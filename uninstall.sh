#!/usr/bin/env bash
set -euo pipefail
systemctl disable --now vbackup-backup.timer 2>/dev/null || true
rm -f /etc/systemd/system/vbackup-backup.service /etc/systemd/system/vbackup-backup.timer /usr/local/bin/vbackup
systemctl daemon-reload 2>/dev/null || true
echo "VBackup removed. Backups and /etc/vbackup are preserved."
