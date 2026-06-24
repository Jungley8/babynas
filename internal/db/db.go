package db

import (
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// DB 封装 SQLite 连接。使用 modernc 纯 Go 驱动，便于交叉编译到 NAS（ARM/amd64 无需 CGO）。
type DB struct {
	*sql.DB
}

// Media 一条媒体记录。
type Media struct {
	ID       int64  `json:"id"`
	Path     string `json:"-"`     // 绝对路径，不暴露给前端
	Rel      string `json:"rel"`   // 相对扫描根的路径
	Category string `json:"category"` // audio | video
	Sub      string `json:"sub"`      // 子分类：儿歌/故事/古诗/纪录片...（取一级目录名）
	Title    string `json:"title"`
	Ext      string `json:"ext"`
	Size     int64  `json:"size"`
	MTime    int64  `json:"mtime"`
	Seed     int64  `json:"seed"` // 封面随机种子（稳定）
	AddedAt  int64  `json:"addedAt"`
}

// Open 打开/初始化数据库，开启 WAL 提升并发与写入性能。
func Open(path string) (*DB, error) {
	sdb, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	// 大量文件场景的性能 pragma
	for _, p := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA cache_size=-20000", // ~20MB 页缓存
	} {
		if _, err := sdb.Exec(p); err != nil {
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	d := &DB{sdb}
	if err := d.migrate(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *DB) migrate() error {
	_, err := d.Exec(`
CREATE TABLE IF NOT EXISTS media (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  path      TEXT NOT NULL UNIQUE,
  rel       TEXT NOT NULL,
  category  TEXT NOT NULL,
  sub       TEXT NOT NULL DEFAULT '',
  title     TEXT NOT NULL,
  ext       TEXT NOT NULL,
  size      INTEGER NOT NULL,
  mtime     INTEGER NOT NULL,
  seed      INTEGER NOT NULL,
  added_at  INTEGER NOT NULL,
  gen       INTEGER NOT NULL DEFAULT 0   -- 扫描代次，用于检测删除
);
CREATE INDEX IF NOT EXISTS idx_media_cat  ON media(category, sub);
CREATE INDEX IF NOT EXISTS idx_media_sub  ON media(sub);

CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT);

-- 收藏：media_id 外键到 media，media 被 Prune 删除时连带清理（ON DELETE CASCADE 需 PRAGMA，
-- 这里靠 JOIN 时 media 不存在自然过滤；额外提供清理）。按 media.category 天然分音/视频。
CREATE TABLE IF NOT EXISTS favorites (
  media_id  INTEGER PRIMARY KEY,
  added_at  INTEGER NOT NULL
);

-- 歌单
CREATE TABLE IF NOT EXISTS playlists (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  created_at  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS playlist_items (
  playlist_id INTEGER NOT NULL,
  media_id    INTEGER NOT NULL,
  pos         INTEGER NOT NULL,
  PRIMARY KEY(playlist_id, media_id)
);
CREATE INDEX IF NOT EXISTS idx_pli_pl ON playlist_items(playlist_id, pos);
`)
	return err
}

// Upsert 插入或更新（仅当 size/mtime 变化），返回是否为新增/变更。
// gen 标记本次扫描代次，用于后续清理已删除文件。
func (d *DB) Upsert(tx *sql.Tx, m *Media, gen int64) error {
	_, err := tx.Exec(`
INSERT INTO media(path, rel, category, sub, title, ext, size, mtime, seed, added_at, gen)
VALUES(?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(path) DO UPDATE SET
  rel=excluded.rel, category=excluded.category, sub=excluded.sub,
  title=excluded.title, ext=excluded.ext, size=excluded.size,
  mtime=excluded.mtime, gen=excluded.gen`,
		m.Path, m.Rel, m.Category, m.Sub, m.Title, m.Ext,
		m.Size, m.MTime, m.Seed, m.AddedAt, gen)
	return err
}

// Touch 仅更新未变化文件的 gen（标记"仍存在"），避免被当作删除清理。
func (d *DB) Touch(tx *sql.Tx, path string, gen int64) error {
	_, err := tx.Exec(`UPDATE media SET gen=? WHERE path=?`, gen, path)
	return err
}

// Existing 返回 path -> (size,mtime) 映射，供扫描时判断是否跳过。
func (d *DB) Existing() (map[string][2]int64, error) {
	rows, err := d.Query(`SELECT path, size, mtime FROM media`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string][2]int64, 4096)
	for rows.Next() {
		var p string
		var s, mt int64
		if err := rows.Scan(&p, &s, &mt); err != nil {
			return nil, err
		}
		m[p] = [2]int64{s, mt}
	}
	return m, rows.Err()
}

// Prune 删除 gen 不等于当前代次的记录（即本次未扫到 = 已从磁盘删除）。
func (d *DB) Prune(gen int64) (int64, error) {
	r, err := d.Exec(`DELETE FROM media WHERE gen<>?`, gen)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

// Subs 返回某分类下的子分类及计数。
func (d *DB) Subs(category string) ([]struct {
	Sub   string `json:"sub"`
	Count int    `json:"count"`
}, error) {
	rows, err := d.Query(`SELECT sub, COUNT(*) FROM media WHERE category=? GROUP BY sub ORDER BY sub`, category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		Sub   string `json:"sub"`
		Count int    `json:"count"`
	}
	for rows.Next() {
		var s struct {
			Sub   string `json:"sub"`
			Count int    `json:"count"`
		}
		if err := rows.Scan(&s.Sub, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// List 分页列出媒体（性能：LIMIT/OFFSET + 索引）。
func (d *DB) List(category, sub string, limit, offset int) ([]Media, error) {
	q := `SELECT id, path, rel, category, sub, title, ext, size, mtime, seed, added_at FROM media WHERE category=?`
	args := []any{category}
	if sub != "" {
		q += ` AND sub=?`
		args = append(args, sub)
	}
	q += ` ORDER BY title LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Media
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID, &m.Path, &m.Rel, &m.Category, &m.Sub,
			&m.Title, &m.Ext, &m.Size, &m.MTime, &m.Seed, &m.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Folder 浏览时的子文件夹条目（含递归文件计数）。
type Folder struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

const mediaCols = `id, path, rel, category, sub, title, ext, size, mtime, seed, added_at`

// mediaColsM 为带 m. 前缀的列，用于与 favorites/playlist_items JOIN（避免 added_at 等列名歧义）。
const mediaColsM = `m.id, m.path, m.rel, m.category, m.sub, m.title, m.ext, m.size, m.mtime, m.seed, m.added_at`

func scanMedia(rows *sql.Rows) ([]Media, error) {
	var out []Media
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID, &m.Path, &m.Rel, &m.Category, &m.Sub,
			&m.Title, &m.Ext, &m.Size, &m.MTime, &m.Seed, &m.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// escapeLike 转义 LIKE 通配符，配合 ESCAPE '\' 使用，避免文件夹名含 % _ 时误匹配。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Browse 浏览 category 下某一层目录 path（"" 为根），文件分页返回。
// 子文件夹仅在首页(offset==0)返回；文件按 title 排序分页（大目录如儿歌 2574 首防止旧设备卡顿）。
// 基于 rel 前缀匹配，命中 rel 的 UNIQUE 索引；substr/instr 按字符计数，兼容中文目录名。
func (d *DB) Browse(category, path string, limit, offset int) ([]Folder, []Media, error) {
	prefix := ""
	if path != "" {
		prefix = path + "/"
	}
	like := escapeLike(prefix) + "%"
	startPos := utf8.RuneCountInString(prefix) + 1 // SQLite substr 为 1 起始、按字符

	var folders []Folder
	if offset == 0 { // 子文件夹只在首页取一次，翻页不重复
		fRows, err := d.Query(`
SELECT seg, COUNT(*) FROM (
  SELECT substr(rem, 1, instr(rem, '/') - 1) AS seg FROM (
    SELECT substr(rel, ?) AS rem FROM media
    WHERE category=? AND rel LIKE ? ESCAPE '\'
  ) WHERE instr(rem, '/') > 0
) GROUP BY seg ORDER BY seg`, startPos, category, like)
		if err != nil {
			return nil, nil, err
		}
		defer fRows.Close()
		for fRows.Next() {
			var f Folder
			if err := fRows.Scan(&f.Name, &f.Count); err != nil {
				return nil, nil, err
			}
			folders = append(folders, f)
		}
		if err := fRows.Err(); err != nil {
			return nil, nil, err
		}
	}

	// 当前层直接文件：剩余路径中不再含 '/'，分页
	mRows, err := d.Query(`
SELECT `+mediaCols+` FROM media
WHERE category=? AND rel LIKE ? ESCAPE '\' AND instr(substr(rel, ?), '/') = 0
ORDER BY title LIMIT ? OFFSET ?`, category, like, startPos, limit, offset)
	if err != nil {
		return nil, nil, err
	}
	defer mRows.Close()
	files, err := scanMedia(mRows)
	return folders, files, err
}

// ByID 取单条（播放/封面时用）。
func (d *DB) ByID(id int64) (*Media, error) {
	var m Media
	err := d.QueryRow(`SELECT id, path, rel, category, sub, title, ext, size, mtime, seed, added_at FROM media WHERE id=?`, id).
		Scan(&m.ID, &m.Path, &m.Rel, &m.Category, &m.Sub, &m.Title, &m.Ext, &m.Size, &m.MTime, &m.Seed, &m.AddedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// QueueUnder 返回 category 下 path 目录内（含子目录，递归）的全部文件，
// 用于"整个目录播放/临时歌单"。按 rel 排序保持目录顺序；上限防超大目录拖垮内存。
func (d *DB) QueueUnder(category, path string, limit int) ([]Media, error) {
	q := `SELECT ` + mediaCols + ` FROM media WHERE category=?`
	args := []any{category}
	if path != "" {
		q += ` AND rel LIKE ? ESCAPE '\'`
		args = append(args, escapeLike(path+"/")+"%")
	}
	q += ` ORDER BY rel LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMedia(rows)
}

// ── 收藏 ──

// FavToggle 切换收藏，返回切换后是否为已收藏。
func (d *DB) FavToggle(mediaID, now int64) (bool, error) {
	r, err := d.Exec(`DELETE FROM favorites WHERE media_id=?`, mediaID)
	if err != nil {
		return false, err
	}
	if n, _ := r.RowsAffected(); n > 0 {
		return false, nil // 原本已收藏 → 取消
	}
	_, err = d.Exec(`INSERT INTO favorites(media_id, added_at) VALUES(?,?)`, mediaID, now)
	return err == nil, err
}

// FavIDs 返回某分类下所有收藏的 media_id（前端用来标记红心）。
func (d *DB) FavIDs(category string) ([]int64, error) {
	rows, err := d.Query(`
SELECT f.media_id FROM favorites f JOIN media m ON m.id=f.media_id
WHERE m.category=? ORDER BY f.added_at DESC`, category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Favs 列出某分类的收藏（按收藏时间倒序），media 已删除的自动排除。
func (d *DB) Favs(category string) ([]Media, error) {
	rows, err := d.Query(`
SELECT `+mediaColsM+` FROM media m JOIN favorites f ON m.id=f.media_id
WHERE m.category=? ORDER BY f.added_at DESC`, category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMedia(rows)
}

// ── 歌单 ──

// PlaylistInfo 歌单概要（含曲目数）。
type PlaylistInfo struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Count   int    `json:"count"`
	Created int64  `json:"created"`
}

func (d *DB) PlaylistCreate(name string, now int64) (int64, error) {
	r, err := d.Exec(`INSERT INTO playlists(name, created_at) VALUES(?,?)`, name, now)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (d *DB) PlaylistDelete(id int64) error {
	if _, err := d.Exec(`DELETE FROM playlist_items WHERE playlist_id=?`, id); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM playlists WHERE id=?`, id)
	return err
}

func (d *DB) PlaylistRename(id int64, name string) error {
	_, err := d.Exec(`UPDATE playlists SET name=? WHERE id=?`, name, id)
	return err
}

// Playlists 列出全部歌单及曲目数。
func (d *DB) Playlists() ([]PlaylistInfo, error) {
	rows, err := d.Query(`
SELECT p.id, p.name, p.created_at, COUNT(i.media_id)
FROM playlists p LEFT JOIN playlist_items i ON i.playlist_id=p.id
GROUP BY p.id ORDER BY p.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlaylistInfo
	for rows.Next() {
		var p PlaylistInfo
		if err := rows.Scan(&p.ID, &p.Name, &p.Created, &p.Count); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PlaylistAdd 添加曲目到歌单末尾（已存在则忽略）。
func (d *DB) PlaylistAdd(plID, mediaID int64) error {
	var pos int64
	d.QueryRow(`SELECT COALESCE(MAX(pos),0)+1 FROM playlist_items WHERE playlist_id=?`, plID).Scan(&pos)
	_, err := d.Exec(`INSERT OR IGNORE INTO playlist_items(playlist_id, media_id, pos) VALUES(?,?,?)`, plID, mediaID, pos)
	return err
}

func (d *DB) PlaylistRemove(plID, mediaID int64) error {
	_, err := d.Exec(`DELETE FROM playlist_items WHERE playlist_id=? AND media_id=?`, plID, mediaID)
	return err
}

// PlaylistItems 按加入顺序返回歌单曲目，media 已删除的自动排除。
func (d *DB) PlaylistItems(plID int64) ([]Media, error) {
	rows, err := d.Query(`
SELECT `+mediaColsM+` FROM media m JOIN playlist_items i ON m.id=i.media_id
WHERE i.playlist_id=? ORDER BY i.pos`, plID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMedia(rows)
}
