package db

import (
	"database/sql"
	"fmt"

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
