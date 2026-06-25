package server

import (
	"html/template"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// ── Lite 模式 ──────────────────────────────────────────────
// 面向 Kindle 体验版浏览器（远古 WebKit）与灰度手机：
//   - 服务端渲染，纯 <a> 整页跳转，零 JS、零 fetch
//   - 纯黑白高对比、大字大按钮，不依赖颜色（灰度屏友好）
//   - 音频用最兼容的原生 <audio controls> + 直连 MP3，无自动播放
// 入口：/lite

// Kindle 安全 CSS：避免 flex/grid/变量/过渡，只用 block/border/padding。
const liteCSS = `
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:sans-serif;background:#fff;color:#000;font-size:20px;line-height:1.5;
  -webkit-text-size-adjust:100%;padding:0 0 40px}
a{color:#000;text-decoration:none}
.top{border-bottom:3px solid #000;padding:14px 16px;font-size:24px;font-weight:bold}
.top a{float:right;font-size:18px;border:2px solid #000;padding:4px 12px}
.wrap{padding:10px 14px}
.row{display:block;border:2px solid #000;border-radius:8px;padding:18px 16px;margin:10px 0;font-size:22px}
.row b{font-weight:bold}
.tag{float:right;border:2px solid #000;border-radius:14px;padding:1px 10px;font-size:15px}
.dir{font-weight:bold}
.dir:before{content:"\1F4C1  "}
.file:before{content:"\25B6  "}
.crumb{padding:8px 14px;font-size:16px;border-bottom:1px solid #999}
.playall{background:#000;color:#fff;text-align:center;font-weight:bold}
.empty{padding:40px 16px;text-align:center;color:#444}
.pbar{padding:16px 14px}
.ptitle{font-size:24px;font-weight:bold;margin-bottom:14px;word-break:break-all}
audio{width:100%;margin:8px 0 20px}
.nav a{display:block;border:2px solid #000;border-radius:8px;padding:16px;margin:10px 0;
  text-align:center;font-size:22px;font-weight:bold}
.nav .disabled{color:#bbb;border-color:#bbb}
.note{font-size:14px;color:#555;padding:10px 16px}
`

var liteTmpl = template.Must(template.New("lite").Parse(`
{{define "head"}}<!DOCTYPE html><html lang="zh-CN"><head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title><style>` + liteCSS + `</style></head><body>{{end}}
{{define "foot"}}</body></html>{{end}}

{{define "home"}}{{template "head" .}}
<div class="top">宝宝乐园 · 轻享版</div>
<div class="wrap">
  <a class="row dir" href="/lite/b?cat=audio">🎵 <b>听</b>　儿歌 · 故事 · 古诗</a>
  <a class="row dir" href="/lite/b?cat=video">🎬 <b>看</b>　动画 · 纪录片</a>
</div>
<div class="note">提示：本页为无彩色、无脚本的轻量模式，适配 Kindle 与灰度屏。<br>Kindle 需连蓝牙音箱/耳机才能出声。</div>
{{template "foot" .}}{{end}}

{{define "browse"}}{{template "head" .}}
<div class="top">{{.CatName}}<a href="/lite">主页</a></div>
{{if .Path}}<div class="crumb">📂 {{.Path}}　<a href="{{.UpHref}}">⬆ 上级</a></div>{{end}}
<div class="wrap">
  {{if .CanPlayAll}}<a class="row playall" href="/lite/p?cat={{.Cat}}&first=1&dir={{.PathEnc}}">▶ 播放本目录全部</a>{{end}}
  {{range .Folders}}<a class="row dir" href="/lite/b?cat={{$.Cat}}&path={{.Href}}">{{.Name}}<span class="tag">{{.Count}}</span></a>{{end}}
  {{range .Files}}<a class="row file" href="/lite/p?id={{.ID}}">{{.Title}}</a>{{end}}
  {{if and (not .Folders) (not .Files)}}<div class="empty">这里还没有内容</div>{{end}}
  {{if .HasMore}}<a class="row" href="/lite/b?cat={{.Cat}}&path={{.PathEnc}}&page={{.NextPage}}">下一页 ▼</a>{{end}}
</div>
{{template "foot" .}}{{end}}

{{define "play"}}{{template "head" .}}
<div class="top">{{.CatName}}<a href="/lite">主页</a></div>
<div class="pbar">
  <div class="ptitle">{{.Item.Title}}</div>
  {{if .IsVideo}}
  <video width="100%" controls preload="auto" src="/api/stream/{{.Item.ID}}"></video>
  {{else}}
  <audio controls preload="auto" src="/api/stream/{{.Item.ID}}"></audio>
  {{end}}
  <div class="nav">
    {{if .PrevID}}<a href="/lite/p?id={{.PrevID}}">⬅ 上一首</a>{{else}}<a class="disabled">⬅ 上一首</a>{{end}}
    {{if .NextID}}<a href="/lite/p?id={{.NextID}}">下一首 ➡</a>{{else}}<a class="disabled">下一首 ➡</a>{{end}}
    <a href="{{.BackHref}}">⬆ 返回目录</a>
  </div>
  <div class="note">若无法播放，多为 Kindle 浏览器限制；可改用灰度模式手机访问同一地址。</div>
</div>
{{template "foot" .}}{{end}}
`))

func catName(cat string) string {
	if cat == "video" {
		return "看一看"
	}
	return "听一听"
}

func (s *Server) liteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /lite", s.liteHome)
	mux.HandleFunc("GET /lite/b", s.liteBrowse)
	mux.HandleFunc("GET /lite/p", s.litePlay)
}

func (s *Server) liteHome(w http.ResponseWriter, r *http.Request) {
	liteTmpl.ExecuteTemplate(w, "home", map[string]any{"Title": "宝宝乐园 · 轻享版"})
}

type liteFolder struct {
	Name  string
	Count int
	Href  string // URL 编码后的子路径
}

func (s *Server) liteBrowse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cat := q.Get("cat")
	if cat != "audio" && cat != "video" {
		cat = "audio"
	}
	p := strings.Trim(q.Get("path"), "/")
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 0 {
		page = 0
	}
	const pageSize = 80
	folders, files, err := s.db.Browse(cat, p, pageSize, page*pageSize)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	lf := make([]liteFolder, 0, len(folders))
	for _, f := range folders {
		child := f.Name
		if p != "" {
			child = p + "/" + f.Name
		}
		lf = append(lf, liteFolder{Name: f.Name, Count: f.Count, Href: child})
	}
	up := "/lite"
	if p != "" {
		parent := path.Dir(p)
		if parent == "." {
			up = "/lite/b?cat=" + cat
		} else {
			up = "/lite/b?cat=" + cat + "&path=" + parent
		}
	}
	liteTmpl.ExecuteTemplate(w, "browse", map[string]any{
		"Title":      catName(cat),
		"CatName":    catName(cat),
		"Cat":        cat,
		"Path":       p,
		"PathEnc":    p,
		"UpHref":     up,
		"Folders":    lf,
		"Files":      files,
		"CanPlayAll": cat == "audio" && len(files) > 0,
		"HasMore":    len(files) == pageSize,
		"NextPage":   page + 1,
	})
}

func (s *Server) litePlay(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// "播放本目录全部"：定位目录首个文件
	if q.Get("first") == "1" {
		cat := q.Get("cat")
		dir := strings.Trim(q.Get("dir"), "/")
		_, files, err := s.db.Browse(cat, dir, 1, 0)
		if err != nil || len(files) == 0 {
			http.Redirect(w, r, "/lite/b?cat="+cat, http.StatusFound)
			return
		}
		http.Redirect(w, r, "/lite/p?id="+strconv.FormatInt(files[0].ID, 10), http.StatusFound)
		return
	}

	id, _ := strconv.ParseInt(q.Get("id"), 10, 64)
	m, err := s.db.ByID(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// 同目录兄弟文件，用于上一首/下一首
	dir := path.Dir(m.Rel)
	if dir == "." {
		dir = ""
	}
	var prevID, nextID int64
	if _, files, err := s.db.Browse(m.Category, dir, 2000, 0); err == nil {
		for i := range files {
			if files[i].ID == m.ID {
				if i > 0 {
					prevID = files[i-1].ID
				}
				if i < len(files)-1 {
					nextID = files[i+1].ID
				}
				break
			}
		}
	}

	back := "/lite/b?cat=" + m.Category
	if dir != "" {
		back += "&path=" + dir
	}
	liteTmpl.ExecuteTemplate(w, "play", map[string]any{
		"Title":   m.Title,
		"CatName": catName(m.Category),
		"Item":    m,
		"IsVideo": m.Category == "video",
		"PrevID":  prevID,
		"NextID":  nextID,
		"BackHref": back,
	})
}
