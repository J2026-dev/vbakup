#!/usr/bin/env bash
set -euo pipefail
restic_init() { restic snapshots >/dev/null 2>&1 || restic init; }
restic_backup_dir() { local dir="$1"; restic backup "$dir" --tag vbackup; restic forget --keep-last "${KEEP_LAST:-3}" --prune; }
restic_restore_snapshot() { local snapshot="${1:-latest}" target="$2"; restic restore "$snapshot" --target "$target"; }
restic_snapshots() { restic snapshots --json; }
