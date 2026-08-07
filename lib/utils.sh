#!/usr/bin/env bash
set -euo pipefail
command_exists() { command -v "$1" >/dev/null 2>&1; }
require_root() { [[ "${EUID:-$(id -u)}" -eq 0 ]] || { echo "Run as root" >&2; exit 1; }; }
json_escape() { jq -Rs . <<<"${1:-}" | tr -d '\n'; }
ensure_dir() { mkdir -p "$1"; }
backup_id() { date -u '+%Y%m%dT%H%M%SZ'; }
load_config() { local f="${VBACKUP_CONFIG:-/etc/vbackup/server.conf}"; [[ -f "$f" ]] && # shellcheck disable=SC1090
  source "$f"; : "${SERVER_ID:=$(hostname -s 2>/dev/null || echo server)}"; : "${RESTIC_REPOSITORY:=rclone:${RCLONE_REMOTE:-gdrive}:VBackup/${SERVER_ID}}"; export SERVER_ID RESTIC_REPOSITORY; }
run_quiet_sensitive() { "$@"; }
