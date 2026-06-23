package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"babynas/internal/cover"
	"babynas/internal/db"
	"babynas/internal/scanner"
)

// Server HTTP 服务。前端与游戏通过 embed.FS 注入（见 main.go）。
type Server struct {
	db   *db.DB
	scan *scanner.Scanner
	web  http.Handler // 嵌入的前端静态资源
	pin  string       // 家长 PIN（保护扫描/管理操作）
}

func New(database *db.DB, sc *scanner.Scanner, web http.Handler, pin string) *Server {
	return &Server{db: database, scan: sc, web: web, pin: pin}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// ── API ──
	mux.HandleFunc("GET /api/subs", s.handleSubs)     // ?cat=audio
	mux.HandleFunc("GET /api/list", s.handleList)     // ?cat=&sub=&page=
	mux.HandleFunc("GET /api/cover/{id}", s.handleCover)
	mux.HandleFunc("GET /api/stream/{id}", s.handleStream)
	mux.HandleFunc("GET /api/scan/status", s.handleScanStatus)
	mux.HandleFunc("POST /api/scan", s.handleScanStart) // 需 PIN

	// ── 前端 + 游戏（静态，由 embed.FS 提供）──
	mux.Handle("/", s.web)

	return logMiddleware(mux)
}

func (s *Server) handleSubs(w http.ResponseWriter, r *http.Request) {
	cat := r.URL.Query().Get("cat")
	subs, err := s.db.Subs(cat)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, subs)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cat := q.Get("cat")
	sub := q.Get("sub")
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 0 {
		page = 0
	}
	const pageSize = 60 // 分页防止一次性返回上万条
	items, err := s.db.List(cat, sub, pageSize, page*pageSize)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"items": items, "page": page, "pageSize": pageSize})
}

func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	m, err := s.db.ByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cover.Handler(w, m.Seed, m.Category, m.Title)
}

// handleStream 直连磁盘文件流式播放。http.ServeContent 原生支持 HTTP Range，
// 拖动进度、边下边播开箱即用，无需把文件读进内存。
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	m, err := s.db.ByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(m.Path)
	if err != nil {
		http.Error(w, "file gone", 410) // 文件已被移动/删除
		return
	}
	defer f.Close()
	http.ServeContent(w, r, m.Title+m.Ext, time.Unix(m.MTime, 0), f)
}

func (s *Server) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	p := &s.scan.Prog
	writeJSON(w, map[string]any{
		"running": p.Running.Load(),
		"scanned": p.Scanned.Load(),
		"added":   p.Added.Load(),
		"removed": p.Removed.Load(),
		"started": p.Started.Load(),
	})
}

func (s *Server) handleScanStart(w http.ResponseWriter, r *http.Request) {
	if s.pin != "" && r.Header.Get("X-Pin") != s.pin {
		http.Error(w, "需要家长 PIN", http.StatusUnauthorized)
		return
	}
	go s.scan.Scan() // 后台异步扫描，立即返回
	writeJSON(w, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}
