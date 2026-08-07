# VBackup DR

Production-oriented Bash backup and disaster recovery scaffold for Ubuntu/Debian VPS hosts. It discovers Docker, databases, web servers, systemd units and network state, writes a manifest, stores encrypted incremental Restic snapshots in per-server Google Drive repositories, and restores through an ordered workflow.

## Quick start

```bash
sudo ./install.sh
vbackup backup
vbackup restore
vbackup verify
```

See `docs/` for installation, restore, Google Drive, security and troubleshooting notes.
