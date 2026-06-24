// Package transcode 提供可选的视频动态转码/重封装，让浏览器不支持的编码（如 H.265）也能播放。
// 依赖系统 ffmpeg/ffprobe；未安装时自动禁用，调用方应回退到原样发送。
package transcode

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"sync"
)

// 浏览器（Chrome/Safari/Edge）可直接解码的编码白名单。
var okVideo = map[string]bool{"h264": true, "vp8": true, "vp9": true, "av1": true}
var okAudio = map[string]bool{"aac": true, "mp3": true, "opus": true, "vorbis": true, "flac": true}

// 这些容器即使编码兼容，浏览器也能直接播，无需换壳。
var okContainer = map[string]bool{".mp4": true, ".m4v": true, ".mov": true, ".webm": true}

// Mode 处理方式。
type Mode int

const (
	Direct         Mode = iota // 原样发送（支持 Range/拖动）
	Remux                      // 仅换容器，编码不变（轻量，但流式无法拖动）
	Transcode                  // 重新编码视频（重）
	TranscodeAudio             // 重新编码音频为 mp3（如 mp2/pcm 假后缀文件）
)

// Transcoder 转码器，持有 ffmpeg 可用性与探测缓存。
type Transcoder struct {
	available bool
	cache     sync.Map // path -> probeResult
}

type probeResult struct {
	vcodec, acodec string
}

// New 检测 ffmpeg/ffprobe 是否可用。
func New() *Transcoder {
	_, e1 := exec.LookPath("ffmpeg")
	_, e2 := exec.LookPath("ffprobe")
	return &Transcoder{available: e1 == nil && e2 == nil}
}

func (t *Transcoder) Available() bool { return t.available }

// Decide 判断文件的处理方式（探测结果带缓存，避免重复 ffprobe）。
func (t *Transcoder) Decide(category, path, ext string) Mode {
	if !t.available {
		return Direct
	}
	pr := t.probe(path)
	if category == "audio" {
		// 音频：编码兼容直发；mp2/pcm/wma 等假后缀文件转码为 mp3
		if okAudio[pr.acodec] {
			return Direct
		}
		return TranscodeAudio
	}
	vOK := okVideo[pr.vcodec]
	aOK := pr.acodec == "" || okAudio[pr.acodec] // 无音轨也算 OK
	switch {
	case vOK && aOK && okContainer[ext]:
		return Direct // 编码和容器都兼容
	case vOK && aOK:
		return Remux // 编码兼容、仅容器需换（如 mkv/ts 里的 h264+aac）
	default:
		return Transcode // 视频或音频编码不兼容
	}
}

func (t *Transcoder) probe(path string) probeResult {
	if v, ok := t.cache.Load(path); ok {
		return v.(probeResult)
	}
	pr := probeResult{vcodec: ffprobe(path, "v:0"), acodec: ffprobe(path, "a:0")}
	t.cache.Store(path, pr)
	return pr
}

func ffprobe(path, stream string) string {
	out, err := exec.Command("ffprobe", "-v", "error",
		"-select_streams", stream,
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Serve 用 ffmpeg 把文件转成分片 MP4 流式输出。Remux 用 -c copy（快），Transcode 重编码视频。
// 流式输出不支持 Range（无法拖动进度）；婴幼儿短视频可接受，长片建议后续做磁盘缓存。
func (t *Transcoder) Serve(w http.ResponseWriter, r *http.Request, path string, mode Mode) {
	// 音频转码：mp2/pcm 等 → mp3，输出 audio/mpeg
	if mode == TranscodeAudio {
		t.serveAudio(w, r, path)
		return
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-i", path}
	if mode == Remux {
		args = append(args, "-c", "copy")
	} else {
		// baseline + 无 B 帧 + zerolatency：保证首个分片即含关键帧与 SPS/PPS，
		// 否则浏览器拿不到视频轨初始化信息（只出声音、画面 0x0）。
		args = append(args,
			"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency",
			"-profile:v", "baseline", "-level", "3.1", "-pix_fmt", "yuv420p",
			"-bf", "0", "-g", "48", "-crf", "23",
			"-c:a", "aac", "-b:a", "128k", "-ac", "2")
	}
	// 分片 MP4：无需可定位的 moov，可边转边发
	args = append(args, "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-f", "mp4", "pipe:1")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "transcode init failed", 500)
		return
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, "ffmpeg start failed", 500)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// 边转边写；客户端断开时 ctx 取消，ffmpeg 被杀
	copyFlush(w, stdout)
	cmd.Wait()
}

// serveAudio 把 mp2/pcm/wma 等不兼容音频转成 mp3 流式输出。
func (t *Transcoder) serveAudio(w http.ResponseWriter, r *http.Request, path string) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error", "-i", path,
		"-vn", "-c:a", "libmp3lame", "-b:a", "128k", "-f", "mp3", "pipe:1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "transcode init failed", 500)
		return
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, "ffmpeg start failed", 500)
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	copyFlush(w, stdout)
	cmd.Wait()
}
