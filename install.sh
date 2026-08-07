#!/usr/bin/env bash
set -euo pipefail
PREFIX="${PREFIX:-/opt/vbackup}"; ETC="${ETC:-/etc/vbackup}"
require_root() { [[ "${EUID:-$(id -u)}" -eq 0 ]] || { echo "Run as root" >&2; exit 1; }; }
prompt_secret() { local name="$1" var; read -r -s -p "$name: " var; echo; printf '%s' "$var"; }
main() { require_root; apt-get update; DEBIAN_FRONTEND=noninteractive apt-get install -y restic rclone jq curl tar zstd; mkdir -p "$PREFIX" "$ETC" /var/log/vbackup; cp -a . "$PREFIX/"; chmod +x "$PREFIX"/{backup.sh,restore.sh,verify.sh,vbackup,update.sh,uninstall.sh}; ln -sf "$PREFIX/vbackup" /usr/local/bin/vbackup; read -r -p "Server Name: " SERVER_ID; read -r -p "Google Drive rclone remote [gdrive]: " RCLONE_REMOTE; RCLONE_REMOTE="${RCLONE_REMOTE:-gdrive}"; RESTIC_PASSWORD="$(prompt_secret "Restic Password")"; read -r -p "Telegram Token (optional): " TELEGRAM_TOKEN; read -r -p "Telegram Chat ID (optional): " TELEGRAM_CHAT_ID; umask 077; cat >"$ETC/server.conf" <<CONF
SERVER_ID="$SERVER_ID"
RCLONE_REMOTE="$RCLONE_REMOTE"
RESTIC_REPOSITORY="rclone:${RCLONE_REMOTE}:VBackup/${SERVER_ID}"
RESTIC_PASSWORD="$RESTIC_PASSWORD"
TELEGRAM_TOKEN="$TELEGRAM_TOKEN"
TELEGRAM_CHAT_ID="$TELEGRAM_CHAT_ID"
KEEP_LAST="3"
CONF
cp "$PREFIX/systemd/vbackup-backup.service" /etc/systemd/system/; cp "$PREFIX/systemd/vbackup-backup.timer" /etc/systemd/system/; systemctl daemon-reload; systemctl enable --now vbackup-backup.timer; RESTIC_PASSWORD="$RESTIC_PASSWORD" RESTIC_REPOSITORY="rclone:${RCLONE_REMOTE}:VBackup/${SERVER_ID}" restic init || true; vbackup test || true; vbackup backup; }
main "$@"
