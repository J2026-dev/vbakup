#!/usr/bin/env bash
set -euo pipefail

VBACKUP_REPO_URL="${VBACKUP_REPO_URL:-https://github.com/J2026-dev/vbakup}"
VBACKUP_BRANCH="${VBACKUP_BRANCH:-main}"
PREFIX="${PREFIX:-/opt/vbackup}"
ETC="${ETC:-/etc/vbackup}"

require_root() {
  [[ "${EUID:-$(id -u)}" -eq 0 ]] || { echo "Run as root or use: curl -fsSL https://raw.githubusercontent.com/J2026-dev/vbakup/main/install.sh | sudo bash" >&2; exit 1; }
}

require_tty() {
  [[ -r /dev/tty ]] || { echo "This installer needs an interactive TTY for configuration prompts." >&2; exit 1; }
}

prompt_secret() {
  local name="$1" var
  require_tty
  printf '%s: ' "$name" >/dev/tty
  IFS= read -r -s var </dev/tty
  printf '\n' >/dev/tty
  printf '%s' "$var"
}

prompt_input() {
  local prompt="$1" value
  require_tty
  printf '%s: ' "$prompt" >/dev/tty
  IFS= read -r value </dev/tty
  printf '%s' "$value"
}

prompt_default() {
  local prompt="$1" default="$2" value
  require_tty
  printf '%s [%s]: ' "$prompt" "$default" >/dev/tty
  IFS= read -r value </dev/tty
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

rclone_remote_exists() {
  local remote="$1"
  rclone listremotes 2>/dev/null | sed 's/:$//' | grep -Fxq "$remote"
}

rclone_remote_works() {
  local remote="$1"
  rclone lsd "${remote}:" >/dev/null 2>&1
}

create_rclone_google_drive_remote() {
  local remote="$1" client_id="$2" client_secret="$3" refresh_token="$4"
  mkdir -p /root/.config/rclone
  umask 077
  rclone config delete "$remote" >/dev/null 2>&1 || true
  rclone config create "$remote" drive client_id "$client_id" client_secret "$client_secret" token "{\"access_token\":\"\",\"token_type\":\"Bearer\",\"refresh_token\":\"$refresh_token\",\"expiry\":\"2000-01-01T00:00:00Z\"}" >/dev/null
}

configure_rclone_google_drive() {
  local remote="$1" client_id="$2" client_secret="$3" refresh_token="$4"
  if rclone_remote_exists "$remote"; then
    echo "rclone remote '${remote}' already exists; validating access..."
    if rclone_remote_works "$remote"; then
      echo "rclone remote '${remote}' is valid."
      return 0
    fi
    echo "rclone remote '${remote}' exists but cannot access Google Drive."
    echo "This usually means its OAuth token is missing or expired."
    if [[ -n "$refresh_token" ]]; then
      echo "Recreating rclone remote '${remote}' with the provided refresh token..."
      create_rclone_google_drive_remote "$remote" "$client_id" "$client_secret" "$refresh_token"
      rclone_remote_works "$remote" || { echo "rclone remote '${remote}' is still invalid after recreation." >&2; return 1; }
      return 0
    fi
    echo "Fix it with one of these commands, then rerun the installer:" >&2
    echo "  rclone config reconnect ${remote}:" >&2
    echo "  rclone config delete ${remote} && rclone config" >&2
    echo "Or rerun this installer and provide Google Drive Client ID/Secret/Refresh Token." >&2
    return 1
  fi
  if [[ -z "$refresh_token" ]]; then
    echo "No refresh token provided and rclone remote '${remote}' does not exist." >&2
    echo "Run 'rclone config' first, or rerun this installer with Google Drive OAuth values." >&2
    return 1
  fi
  create_rclone_google_drive_remote "$remote" "$client_id" "$client_secret" "$refresh_token"
  rclone_remote_works "$remote" || { echo "rclone remote '${remote}' is invalid after creation." >&2; return 1; }
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
  client_id="$(prompt_input "Google Drive Client ID（可留空）")"
  client_secret="$(prompt_secret "Google Drive Client Secret（可留空，输入时不显示）")"
  refresh_token="$(prompt_secret "Google Drive Refresh Token（可留空，输入时不显示）")"
  restic_password="$(prompt_secret "Restic Password / Restic 加密密码")"
  telegram_token="$(prompt_input "Telegram Token（可留空）")"
  telegram_chat_id="$(prompt_input "Telegram Chat ID（可留空）")"

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
