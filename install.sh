#!/usr/bin/env bash
set -euo pipefail

VBACKUP_REPO_URL="${VBACKUP_REPO_URL:-https://github.com/J2026-dev/vbakup}"
VBACKUP_BRANCH="${VBACKUP_BRANCH:-main}"
PREFIX="${PREFIX:-/opt/vbackup}"
ETC="${ETC:-/etc/vbackup}"

require_root() {
  [[ "${EUID:-$(id -u)}" -eq 0 ]] || { echo "Run as root or use: curl -fsSL https://raw.githubusercontent.com/J2026-dev/vbakup/main/install.sh | sudo bash" >&2; exit 1; }
}

prompt_secret() {
  local name="$1" var
  read -r -s -p "$name: " var
  echo
  printf '%s' "$var"
}

prompt_default() {
  local prompt="$1" default="$2" value
  read -r -p "$prompt [$default]: " value
  printf '%s' "${value:-$default}"
}

bootstrap_from_github_if_needed() {
  [[ -f "./VERSION" && -d "./lib" && -f "./backup.sh" ]] && return 0
  local tmp archive primary fallback
  tmp="$(mktemp -d)"
  archive="$tmp/vbackup.tar.gz"
  primary="${VBACKUP_REPO_URL}/archive/refs/heads/${VBACKUP_BRANCH}.tar.gz"
  fallback="${VBACKUP_REPO_URL}/archive/refs/heads/master.tar.gz"
  echo "Downloading VBackup DR from ${VBACKUP_REPO_URL} (${VBACKUP_BRANCH})..."
  if ! curl -fsSL "$primary" -o "$archive"; then
    echo "Branch '${VBACKUP_BRANCH}' not available, trying master..."
    curl -fsSL "$fallback" -o "$archive"
  fi
  tar -xzf "$archive" -C "$tmp"
  local src
  src="$(find "$tmp" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
  cd "$src"
  exec bash ./install.sh
}

install_dependencies() {
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y restic rclone jq curl tar zstd
}

install_files() {
  mkdir -p "$PREFIX"
  tar --exclude='.git' --exclude='reports' -cf - . | tar -xf - -C "$PREFIX"
  chmod +x "$PREFIX"/{backup.sh,restore.sh,verify.sh,vbackup,update.sh,uninstall.sh,install.sh}
  ln -sf "$PREFIX/vbackup" /usr/local/bin/vbackup
}

configure_rclone_google_drive() {
  local remote="$1" client_id="$2" client_secret="$3" refresh_token="$4"
  if rclone listremotes 2>/dev/null | sed 's/:$//' | grep -Fxq "$remote"; then
    echo "rclone remote '${remote}' already exists; keeping existing configuration."
    return 0
  fi
  if [[ -z "$refresh_token" ]]; then
    echo "No refresh token provided and rclone remote '${remote}' does not exist."
    echo "Run 'rclone config' first, or rerun this installer with Google Drive OAuth values."
    return 1
  fi
  mkdir -p /root/.config/rclone
  umask 077
  rclone config create "$remote" drive client_id "$client_id" client_secret "$client_secret" token "{\"access_token\":\"\",\"token_type\":\"Bearer\",\"refresh_token\":\"$refresh_token\",\"expiry\":\"2000-01-01T00:00:00Z\"}" >/dev/null
}

write_config() {
  local server_id="$1" remote="$2" restic_password="$3" telegram_token="$4" telegram_chat_id="$5"
  mkdir -p "$ETC" /var/log/vbackup
  umask 077
  cat >"$ETC/server.conf" <<CONF
SERVER_ID="$server_id"
RCLONE_REMOTE="$remote"
RESTIC_REPOSITORY="rclone:${remote}:VBackup/${server_id}"
RESTIC_PASSWORD="$restic_password"
TELEGRAM_TOKEN="$telegram_token"
TELEGRAM_CHAT_ID="$telegram_chat_id"
KEEP_LAST="3"
CONF
}

install_systemd() {
  cp "$PREFIX/systemd/vbackup-backup.service" /etc/systemd/system/
  cp "$PREFIX/systemd/vbackup-backup.timer" /etc/systemd/system/
  systemctl daemon-reload
  systemctl enable --now vbackup-backup.timer
}

main() {
  require_root
  bootstrap_from_github_if_needed
  echo "=== VBackup DR 一键安装向导 ==="
  install_dependencies
  install_files

  local server_id remote client_id client_secret refresh_token restic_password telegram_token telegram_chat_id
  server_id="$(prompt_default "Server Name / 当前 VPS 唯一名称" "$(hostname -s 2>/dev/null || echo vps-01)")"
  remote="$(prompt_default "Google Drive rclone remote 名称" "gdrive")"
  echo "如果 '${remote}' 已通过 rclone config 配置，下面三个 Google OAuth 字段可直接回车跳过。"
  read -r -p "Google Drive Client ID: " client_id
  client_secret="$(prompt_secret "Google Drive Client Secret")"
  refresh_token="$(prompt_secret "Google Drive Refresh Token")"
  restic_password="$(prompt_secret "Restic Password / Restic 加密密码")"
  read -r -p "Telegram Token (optional): " telegram_token
  read -r -p "Telegram Chat ID (optional): " telegram_chat_id

  configure_rclone_google_drive "$remote" "$client_id" "$client_secret" "$refresh_token"
  write_config "$server_id" "$remote" "$restic_password" "$telegram_token" "$telegram_chat_id"
  install_systemd

  if RESTIC_PASSWORD="$restic_password" RESTIC_REPOSITORY="rclone:${remote}:VBackup/${server_id}" restic init; then
    vbackup test || true
    vbackup backup
    echo "VBackup DR installed successfully. Use 'vbackup schedule' to view the next run."
  else
    echo "Restic initialization failed. Check rclone remote '${remote}' and rerun: vbackup backup" >&2
    exit 1
  fi
}

main "$@"
