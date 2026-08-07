#!/usr/bin/env bash
set -euo pipefail
gdrive_test() { rclone lsd "${RCLONE_REMOTE:-gdrive}:" >/dev/null; }
gdrive_list_servers() { rclone lsf "${RCLONE_REMOTE:-gdrive}:VBackup" --dirs-only 2>/dev/null | sed 's#/$##'; }
