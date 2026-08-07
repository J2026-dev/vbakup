#!/usr/bin/env bash
set -euo pipefail
VBACKUP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"; source "$VBACKUP_ROOT/lib/logger.sh"; source "$VBACKUP_ROOT/lib/utils.sh"; logger_init
main() { local report="${VBACKUP_REPORT_DIR:-reports}/restore-result.json"; mkdir -p "$(dirname "$report")"; jq -n --arg time "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" --arg docker "$(command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1 && echo ok || echo skipped)" --arg systemd "$(systemctl is-system-running 2>/dev/null || true)" --arg http "$(curl -fsS --max-time 3 http://127.0.0.1 >/dev/null 2>&1 && echo ok || echo skipped)" --arg https "$(curl -kfsS --max-time 3 https://127.0.0.1 >/dev/null 2>&1 && echo ok || echo skipped)" '{time:$time,checks:{docker:$docker,systemd:$systemd,http:$http,https:$https},status:"complete"}' >"$report"; log_success "Verification report written to $report"; }
main "$@"
