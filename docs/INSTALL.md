# VBackup DR Notes

VBackup DR is configured through `/etc/vbackup/server.conf` and operated with the `vbackup` command. Keep Restic and Google Drive credentials secret, test restores regularly, and review `/var/log/vbackup/` plus `reports/restore-result.json` after each recovery.
