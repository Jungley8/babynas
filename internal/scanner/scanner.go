package scanner

import (
	"hash/fnv"
	"io/fs"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"babynas/internal/db"
)

// 支持的扩展名 → 分类。
var audioExt = map[string]bool{
	".mp3": true, ".m4a": true, ".aac": true, ".flac": true,
	".wav": true, ".ogg": true, ".opus": true, ".wma": true,
}
var videoExt = map[string]bool{
	".mp4": true, ".mkv": true, ".webm": true, ".mov": true,
	".avi": true, ".m4v": true, ".ts": true, ".flv": true,
}

// Root 一个扫描根：分类 + 磁盘目录。
type Root struct {
	Category string // audio | video
	Dir      string // 绝对路径
}

// Progress 扫描进度（原子读写，供 API 查询）。
type Progress struct {
	Running atomic.Bool
	Scanned atomic.Int64 // 已遍历文件
	Added   atomic.Int64 // 新增/变更
	Removed atomic.Int64 // 清理（删除的文件）
	Started atomic.Int64 // unix 秒
}

// Scanner 增量扫描器。
type Scanner struct {
	db    *db.DB
	roots []Root
	gen   int64
	Prog  Progress
}

func New(database *db.DB, roots []Root) *Scanner {
	return &Scanner{db: database, roots: roots}
}

// Scan 执行一次增量扫描。未变化文件（size+mtime 相同）仅 Touch，不重写；
// 新增/变更走 Upsert；扫描结束后 Prune 掉本代次未触达的记录（=已删除）。
func (s *Scanner) Scan() error {
	if !s.Prog.Running.CompareAndSwap(false, true) {
		return nil // 已在运行，幂等
	}
	defer s.Prog.Running.Store(false)
	s.gen++
	gen := s.gen
	s.Prog.Scanned.Store(0)
	s.Prog.Added.Store(0)
	s.Prog.Removed.Store(0)
	s.Prog.Started.Store(time.Now().Unix())

	existing, err := s.db.Existing()
	if err != nil {
		return err
	}

	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	batch := 0
	commit := func() error {
		if err := tx.Commit(); err != nil {
			return err
		}
		tx, err = s.db.Begin()
		return err
	}

	for _, root := range s.roots {
		extSet := audioExt
		if root.Category == "video" {
			extSet = videoExt
		}
		walkErr := filepath.WalkDir(root.Dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // 跳过无法访问的项，不中断整体扫描
			}
			if d.IsDir() {
				name := d.Name()
				if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") {
					return filepath.SkipDir // 跳过隐藏目录与 NAS 系统目录(@eaDir 等)
				}
				return nil
			}
			name := d.Name()
			// 跳过 macOS AppleDouble 资源派生文件(._xxx)与隐藏文件
			if strings.HasPrefix(name, "._") || strings.HasPrefix(name, ".") {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if !extSet[ext] {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			s.Prog.Scanned.Add(1)

			// 增量判断：size+mtime 未变 → 仅标记存在
			if prev, ok := existing[p]; ok && prev[0] == info.Size() && prev[1] == info.ModTime().Unix() {
				if err := s.db.Touch(tx, p, gen); err != nil {
					return err
				}
			} else {
				m := buildMedia(root, p, info.Size(), info.ModTime().Unix(), now)
				if err := s.db.Upsert(tx, m, gen); err != nil {
					return err
				}
				s.Prog.Added.Add(1)
			}

			batch++
			if batch >= 500 { // 每 500 条提交一次，控制事务大小
				batch = 0
				if err := commit(); err != nil {
					return err
				}
			}
			return nil
		})
		if walkErr != nil {
			tx.Rollback()
			return walkErr
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	removed, err := s.db.Prune(gen)
	if err != nil {
		return err
	}
	s.Prog.Removed.Store(removed)
	return nil
}

// buildMedia 从路径推导元数据。子分类 = 相对扫描根的一级目录名。
func buildMedia(root Root, path string, size, mtime, now int64) *db.Media {
	rel, _ := filepath.Rel(root.Dir, path)
	rel = filepath.ToSlash(rel)
	sub := ""
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		sub = rel[:i] // 一级目录作为子分类，如 儿歌/故事/古诗
	}
	ext := strings.ToLower(filepath.Ext(path))
	title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return &db.Media{
		Path:     path,
		Rel:      rel,
		Category: root.Category,
		Sub:      sub,
		Title:    title,
		Ext:      ext,
		Size:     size,
		MTime:    mtime,
		Seed:     seedFor(rel),
		AddedAt:  now,
	}
}

// seedFor 由相对路径生成稳定随机种子，保证同一文件封面不变。
func seedFor(s string) int64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return int64(h.Sum64() & 0x7fffffffffffffff)
}
