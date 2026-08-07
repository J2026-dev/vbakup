#!/usr/bin/env bash
set -euo pipefail
notify_telegram() { local status="$1" snapshot="${2:-n/a}" duration="${3:-n/a}" size="${4:-n/a}"; [[ -n "${TELEGRAM_TOKEN:-}" && -n "${TELEGRAM_CHAT_ID:-}" ]] || return 0; curl -fsS -X POST "https://api.telegram.org/bot${TELEGRAM_TOKEN}/sendMessage" --data-urlencode "chat_id=${TELEGRAM_CHAT_ID}" --data-urlencode "text=Server: ${SERVER_ID:-unknown}
Status: ${status}
Snapshot: ${snapshot}
Duration: ${duration}
Size: ${size}" >/dev/null || true; }
notify_test() { notify_telegram "TEST"; }
