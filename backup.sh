#!/usr/bin/env bash
set -euo pipefail
VBACKUP_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"; export VBACKUP_ROOT
# shellcheck source=lib/logger.sh
source "$VBACKUP_ROOT/lib/logger.sh"; source "$VBACKUP_ROOT/lib/utils.sh"; source "$VBACKUP_ROOT/lib/check.sh"; source "$VBACKUP_ROOT/lib/database.sh"; source "$VBACKUP_ROOT/lib/docker.sh"; source "$VBACKUP_ROOT/lib/systemd.sh"; source "$VBACKUP_ROOT/lib/network.sh"; source "$VBACKUP_ROOT/lib/webserver.sh"; source "$VBACKUP_ROOT/lib/discovery.sh"; source "$VBACKUP_ROOT/lib/metadata.sh"; source "$VBACKUP_ROOT/lib/restic.sh"; source "$VBACKUP_ROOT/lib/notify.sh"
main() { logger_init; load_config; local start id work; start="$(date +%s)"; id="$(backup_id)"; work="${VBACKUP_WORKDIR:-/tmp/vbackup-$id}"; trap 'rm -rf "$work"' EXIT; mkdir -p "$work/metadata"; log_info "Starting backup for $SERVER_ID"; check_supported_os; check_environment || true; discovery_run "$work/metadata"; database_backup "$work"; docker_backup "$work"; systemd_backup "$work"; network_backup "$work"; webserver_backup "$work"; metadata_manifest "$work"; restic_init; restic_backup_dir "$work"; local dur; dur="$(( $(date +%s) - start ))s"; notify_telegram SUCCESS latest "$dur" "$(du -sh "$work" | awk '{print $1}')"; log_success "Backup complete"; }
main "$@"
