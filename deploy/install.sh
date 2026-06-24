#!/usr/bin/env bash
# babynas 安装 / 升级 / 卸载脚本（Linux systemd）
#
#   sudo ./install.sh install [选项]    首次安装并启动服务
#   sudo ./install.sh upgrade [选项]    升级二进制（保留配置）
#   sudo ./install.sh uninstall         停止并移除服务（保留媒体与数据库）
#   sudo ./install.sh status            查看服务状态
#
# 选项：
#   --version vX.Y.Z   指定版本（默认拉取最新 Release）
#   --audio  DIR       音频根目录
#   --video  DIR       视频根目录
#   --port   N         监听端口（默认 8088）
#   --pin    NNNN      家长 PIN（默认空，不校验）
#
# 示例：
#   sudo ./install.sh install --audio /volume1/media/音频 --video /volume1/media/视频 --pin 1234
#   sudo ./install.sh upgrade
set -euo pipefail

REPO="Jungley8/babynas"
PREFIX="/opt/babynas"
BIN="$PREFIX/babynas"
ENV_FILE="$PREFIX/babynas.env"
SERVICE="/etc/systemd/system/babynas.service"
SERVICE_NAME="babynas"

# ── 选项默认值 ──
VERSION=""
OPT_AUDIO=""
OPT_VIDEO=""
OPT_PORT=""
OPT_PIN=""

log()  { echo -e "\033[1;36m==>\033[0m $*"; }
warn() { echo -e "\033[1;33m警告:\033[0m $*" >&2; }
die()  { echo -e "\033[1;31m错误:\033[0m $*" >&2; exit 1; }

require_root() { [ "$(id -u)" = "0" ] || die "请用 root 运行：sudo $0 $*"; }

# 探测架构 → Release 产物后缀
detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)        echo "linux-amd64" ;;
    aarch64|arm64)       echo "linux-arm64" ;;
    armv7l|armv7|armhf)  echo "linux-arm7"  ;;
    *) die "不支持的架构：$(uname -m)（仅支持 x86_64 / arm64 / armv7）" ;;
  esac
}

# 查询最新 Release tag（无需 jq）
latest_version() {
  curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'
}

# 下载并解压二进制到 $BIN
download_binary() {
  local ver="$1" arch tmp tarball url
  arch="$(detect_arch)"
  [ -n "$ver" ] || ver="$(latest_version)"
  [ -n "$ver" ] || die "无法获取版本号，请用 --version 指定"
  tarball="babynas-${arch}.tar.gz"
  url="https://github.com/$REPO/releases/download/${ver}/${tarball}"
  log "下载 $ver （$arch）"
  tmp="$(mktemp -d)"
  curl -fSL --progress-bar "$url" -o "$tmp/$tarball" || die "下载失败：$url"
  tar -xzf "$tmp/$tarball" -C "$tmp"
  install -D -m 0755 "$tmp/babynas-${arch}" "$BIN"
  rm -rf "$tmp"
  log "已安装二进制：$BIN （$("$BIN" -version 2>/dev/null || echo "$ver")）"
}

parse_opts() {
  while [ $# -gt 0 ]; do
    case "$1" in
      --version) VERSION="$2"; shift 2 ;;
      --audio)   OPT_AUDIO="$2"; shift 2 ;;
      --video)   OPT_VIDEO="$2"; shift 2 ;;
      --port)    OPT_PORT="$2"; shift 2 ;;
      --pin)     OPT_PIN="$2"; shift 2 ;;
      *) die "未知选项：$1" ;;
    esac
  done
}

write_env() {
  # 首次安装写入配置；已存在则保留（升级不覆盖）
  install -d -m 0755 "$PREFIX"
  cat > "$ENV_FILE" <<EOF
# babynas 运行配置（升级不会覆盖本文件；改完执行 systemctl restart babynas）
ADDR=:${OPT_PORT:-8088}
AUDIO=${OPT_AUDIO}
VIDEO=${OPT_VIDEO}
DB=${PREFIX}/babynas.db
PIN=${OPT_PIN}
EOF
  chmod 0644 "$ENV_FILE"
  log "已写入配置：$ENV_FILE"
}

write_service() {
  cat > "$SERVICE" <<EOF
[Unit]
Description=babynas 婴幼儿家庭媒体服务
After=network.target

[Service]
Type=simple
EnvironmentFile=${ENV_FILE}
ExecStart=${BIN} -addr \${ADDR} -audio \${AUDIO} -video \${VIDEO} -db \${DB} -pin \${PIN}
WorkingDirectory=${PREFIX}
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  log "已写入服务：$SERVICE"
}

cmd_install() {
  require_root install
  parse_opts "$@"
  [ -n "$OPT_AUDIO$OPT_VIDEO" ] || warn "未指定 --audio / --video，可稍后编辑 $ENV_FILE"
  download_binary "$VERSION"
  if [ -f "$ENV_FILE" ]; then
    warn "检测到已有配置 $ENV_FILE，保留不覆盖（如需重配请编辑该文件）"
  else
    write_env
  fi
  write_service
  systemctl enable "$SERVICE_NAME" >/dev/null 2>&1 || true
  systemctl restart "$SERVICE_NAME"
  local ip; ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  log "安装完成 ✅  访问： http://${ip:-<本机IP>}:${OPT_PORT:-8088}"
  systemctl --no-pager --lines=0 status "$SERVICE_NAME" || true
}

cmd_upgrade() {
  require_root upgrade
  parse_opts "$@"
  [ -f "$BIN" ] || die "未检测到已安装的 babynas，请先执行 install"
  local cur; cur="$("$BIN" -version 2>/dev/null || echo unknown)"
  log "当前版本：$cur"
  download_binary "$VERSION"
  # 配置与服务单元保持不变，仅在缺失时补写
  [ -f "$SERVICE" ] || write_service
  systemctl restart "$SERVICE_NAME"
  log "升级完成 ✅  新版本：$("$BIN" -version 2>/dev/null || echo "$VERSION")"
}

cmd_uninstall() {
  require_root uninstall
  systemctl stop "$SERVICE_NAME" 2>/dev/null || true
  systemctl disable "$SERVICE_NAME" 2>/dev/null || true
  rm -f "$SERVICE"
  systemctl daemon-reload
  log "已移除服务。二进制/配置/数据库仍保留在 $PREFIX （媒体文件未动）"
  log "如需彻底删除： sudo rm -rf $PREFIX"
}

cmd_status() {
  systemctl --no-pager status "$SERVICE_NAME"
}

main() {
  local sub="${1:-}"; shift || true
  case "$sub" in
    install)   cmd_install "$@" ;;
    upgrade)   cmd_upgrade "$@" ;;
    uninstall) cmd_uninstall "$@" ;;
    status)    cmd_status "$@" ;;
    *) sed -n '2,30p' "$0"; exit 1 ;;
  esac
}

main "$@"
