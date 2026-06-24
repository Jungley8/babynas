# babynas 🍼

面向婴幼儿（0-3 岁）的家庭媒体资源管理器。单二进制部署在 NAS 上，直连本地音视频目录，递归增量扫描入库，自动生成封面，提供 **听 / 看 / 玩** 三大入口。

> 数据自主可控，局域网内运行，不依赖第三方 App 的广告与推荐算法。

## ✨ 特性

- **单二进制**：纯 Go（含 SQLite），无 CGO，无外部依赖，一个文件丢上 NAS 即可运行
- **递归增量扫描**：按 `size + mtime` 判重，只处理新增/变更文件，删除自动清理；万级文件秒级增量
- **自动封面**：由文件路径生成稳定的渐变 SVG 封面，矢量、零体积、永不重复变化
- **流式播放**：`http.ServeContent` 原生 HTTP Range，拖动进度、边下边播，不占内存
- **三大入口**
  - 🎵 **听**：儿歌 / 故事 / 古诗（以一级目录自动分类）
  - 🎬 **看**：动画 / 纪录片
  - 🎮 **玩**：内置 0-3 岁因果反馈小游戏（戳泡泡 / 敲敲乐 / 弹琴）
- **护眼设计**：音频优先；连续使用 20 分钟弹出休息提醒
- **家长 PIN**：扫描/管理操作可加 PIN 保护，防止误删 NAS 文件

## 🚀 快速开始

```bash
# 编译
make build

# 运行（指定你的 NAS 音视频目录）
./babynas \
  -audio /volume1/media/音频 \
  -video /volume1/media/视频 \
  -db ./babynas.db \
  -addr :8088 \
  -pin 1234
```

打开 `http://<NAS-IP>:8088` 即可。

### 目录约定

扫描根下的**一级目录名**即子分类：

```
音频/
├── 儿歌/        → 子分类「儿歌」
│   ├── 小星星.mp3
│   └── 两只老虎.mp3
├── 故事/        → 子分类「故事」
└── 古诗/        → 子分类「古诗」
视频/
├── 动画/
└── 纪录片/
```

支持格式：音频 `mp3 m4a aac flac wav ogg opus`，视频 `mp4 mkv webm mov m4v ts`（浏览器直接播放以 `mp4(H.264)` 兼容性最佳）。

## 📦 部署

### 交叉编译（在开发机生成 NAS 可执行文件）

```bash
make dist        # 生成 linux-amd64 / arm64 / armv7 / darwin 到 dist/
```

### 一键安装脚本（Linux systemd，推荐）

自动探测架构、下载对应 Release、安装为 systemd 服务：

```bash
curl -fsSL https://raw.githubusercontent.com/Jungley8/babynas/main/deploy/install.sh -o install.sh
chmod +x install.sh

# 首次安装（指定媒体目录、可选 PIN）
sudo ./install.sh install --audio /volume1/media/音频 --video /volume1/media/视频 --pin 1234

# 升级到最新版（保留配置，仅换二进制并重启）
sudo ./install.sh upgrade

# 指定版本
sudo ./install.sh upgrade --version v0.5.0

# 其他
sudo ./install.sh status      # 查看运行状态
sudo ./install.sh uninstall   # 移除服务（保留媒体与数据库）
```

配置存于 `/opt/babynas/babynas.env`，升级不会覆盖；改完执行 `sudo systemctl restart babynas`。

### 手动 systemd 安装

```bash
sudo install -D -m755 dist/babynas-linux-amd64 /opt/babynas/babynas
sudo cp deploy/babynas.service /etc/systemd/system/
# 创建 /opt/babynas/babynas.env 配置 ADDR/AUDIO/VIDEO/DB/PIN，然后：
sudo systemctl enable --now babynas
```

### Docker

```bash
docker build -t babynas .
docker run -d -p 8088:8088 \
  -v /nas/media:/media \
  -v /nas/babynas:/data \
  babynas -audio /media/音频 -video /media/视频 -db /data/babynas.db
```

## 📱 iOS 锁定为儿童专用（推荐）

1. Safari 打开页面 → 分享 → **添加到主屏幕**（全屏 PWA）
2. 设置 → 辅助功能 → **引导式访问** 打开
3. 启动 App 后三击侧边键锁定 —— 孩子无法划出/退出，按不了 Home

## 🎮 内置小游戏

| 游戏 | 适龄 | 因果机制 |
|------|------|---------|
| 🫧 戳泡泡 | 6m+ | 点→泡泡破裂 + 音效 |
| 🥁 敲敲乐 | 8m+ | 点不同区域→不同乐器 |
| 🎹 弹琴   | 10m+ | 按键→琴音 + 灯光 |

> 2048、连连看等需要策略/配对能力的游戏不适合 0-3 岁，列入后续「大童模式」。

## 🛠 技术栈

- Go 1.25 · `net/http`（Go 1.22+ 路由）· `embed.FS`
- SQLite（`modernc.org/sqlite` 纯 Go 驱动，WAL 模式）
- 前端零框架原生 HTML/CSS/JS，毛玻璃 + 骨架屏

## 📄 License

MIT
