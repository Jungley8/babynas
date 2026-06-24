// babynas —— 面向婴幼儿的家庭媒体资源管理器。
// 单二进制部署在 NAS 上，直连本地音视频目录，递归增量扫描入库，
// 自动生成封面，提供音频/视频/游戏三大入口。
package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"babynas/internal/db"
	"babynas/internal/scanner"
	"babynas/internal/server"
)

//go:embed all:web all:games
var assets embed.FS

// version 由构建时 -ldflags 注入（见 Makefile）。
var version = "dev"

func main() {
	var (
		addr     = flag.String("addr", ":8088", "监听地址")
		audioDir = flag.String("audio", "", "音频根目录（递归扫描）")
		videoDir = flag.String("video", "", "视频根目录（递归扫描）")
		dbPath   = flag.String("db", "babynas.db", "SQLite 数据库路径")
		pin      = flag.String("pin", "", "家长 PIN，保护扫描/管理操作（留空则不校验）")
		showVer  = flag.Bool("version", false, "打印版本号并退出")
	)
	flag.Parse()

	if *showVer {
		println(version)
		return
	}

	database, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("打开数据库: %v", err)
	}
	defer database.Close()

	var roots []scanner.Root
	if *audioDir != "" {
		roots = append(roots, scanner.Root{Category: "audio", Dir: *audioDir})
	}
	if *videoDir != "" {
		roots = append(roots, scanner.Root{Category: "video", Dir: *videoDir})
	}
	sc := scanner.New(database, roots)

	// 启动即触发一次后台增量扫描
	if len(roots) > 0 {
		go func() {
			log.Println("开始首次扫描...")
			if err := sc.Scan(); err != nil {
				log.Printf("扫描出错: %v", err)
			} else {
				log.Printf("扫描完成: 遍历 %d，新增/变更 %d，清理 %d",
					sc.Prog.Scanned.Load(), sc.Prog.Added.Load(), sc.Prog.Removed.Load())
			}
		}()
	} else {
		log.Println("提示：未指定 -audio / -video 目录，仅启动前端")
	}

	// embed.FS 子目录作为静态资源根（web/ 在前，games/ 通过 /games/ 访问）
	web, _ := fs.Sub(assets, "web")
	gamesFS, _ := fs.Sub(assets, "games")
	webMux := http.NewServeMux()
	webMux.Handle("/games/", http.StripPrefix("/games/", http.FileServer(http.FS(gamesFS))))
	webMux.Handle("/", spaFileServer(web))

	srv := server.New(database, sc, webMux, *pin)
	log.Printf("babynas %s 启动于 %s", version, *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.Routes()))
}

// spaFileServer 提供前端静态文件，404 回落到 index.html（单页应用路由）。
func spaFileServer(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(root, p); err != nil {
			r.URL.Path = "/" // 回落到 index.html
		}
		fileServer.ServeHTTP(w, r)
	})
}
