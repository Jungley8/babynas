package cover

import (
	"fmt"
	"net/http"
)

// 柔和、适合婴幼儿的高明度配色（成对：起始色→结束色）。
var palettes = [][2]string{
	{"#FFD3A5", "#FD6585"}, {"#A1FFCE", "#FAFFD1"}, {"#A18CD1", "#FBC2EB"},
	{"#FAD0C4", "#FFD1FF"}, {"#FFECD2", "#FCB69F"}, {"#84FAB0", "#8FD3F4"},
	{"#FFF1EB", "#ACE0F9"}, {"#C2E9FB", "#A1C4FD"}, {"#FDCBF1", "#E6DEE9"},
	{"#F6D365", "#FDA085"}, {"#96FBC4", "#F9F586"}, {"#D4FC79", "#96E6A1"},
}

// 婴幼儿友好的图标集，按分类选取。
var iconsByCat = map[string][]string{
	"audio": {"🎵", "🎶", "🎤", "🎸", "🎹", "🥁", "🎺", "🪕", "🔔", "⭐"},
	"video": {"🎬", "🎞️", "📺", "🐘", "🦁", "🐳", "🌈", "🚀", "🌻", "🎠"},
}

// SVG 生成确定性封面（由 seed 决定渐变与图标，同一文件始终一致）。
// 用 SVG 而非位图：体积极小、矢量清晰、零图像库依赖，浏览器直接渲染。
func SVG(seed int64, category, title string) string {
	p := palettes[uint64(seed)%uint64(len(palettes))]
	icons := iconsByCat[category]
	if len(icons) == 0 {
		icons = iconsByCat["audio"]
	}
	icon := icons[uint64(seed/7)%uint64(len(icons))]
	angle := int(seed%360)

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 300">
<defs><linearGradient id="g" gradientTransform="rotate(%d 0.5 0.5)">
<stop offset="0%%" stop-color="%s"/><stop offset="100%%" stop-color="%s"/>
</linearGradient></defs>
<rect width="300" height="300" rx="32" fill="url(#g)"/>
<circle cx="150" cy="125" r="62" fill="rgba(255,255,255,.35)"/>
<text x="150" y="148" font-size="74" text-anchor="middle" dominant-baseline="middle">%s</text>
</svg>`, angle, p[0], p[1], icon)
}

// Handler 输出 SVG 封面，带长缓存（封面由 seed 决定，永不变化）。
func Handler(w http.ResponseWriter, seed int64, category, title string) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	fmt.Fprint(w, SVG(seed, category, title))
}
