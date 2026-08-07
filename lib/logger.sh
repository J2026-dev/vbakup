#!/usr/bin/env bash
set -euo pipefail
: "${VBACKUP_LOG_DIR:=/var/log/vbackup}"
: "${VBACKUP_NO_LOG:=0}"
logger_init() { if [[ "$VBACKUP_NO_LOG" != "1" ]]; then mkdir -p "$VBACKUP_LOG_DIR" 2>/dev/null || true; fi; }
_log_write() { local level="$1"; shift; local msg="$*" ts file; ts="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"; printf '[%s] [%s] %s\n' "$ts" "$level" "$msg"; if [[ "$VBACKUP_NO_LOG" != "1" ]]; then file="$VBACKUP_LOG_DIR/$(date -u '+%Y-%m-%d').log"; printf '[%s] [%s] %s\n' "$ts" "$level" "$msg" >>"$file" 2>/dev/null || true; fi; }
log_info() { _log_write INFO "$@"; }
log_warn() { _log_write WARN "$@"; }
log_error() { _log_write ERROR "$@"; }
log_success() { _log_write SUCCESS "$@"; }
