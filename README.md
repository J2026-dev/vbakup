# VBackup DR

VBackup DR（VPS Backup & Disaster Recovery）是一个面向 Ubuntu / Debian VPS 的 Bash 自动备份与灾难恢复框架。它会自动发现 Docker、数据库、网站、systemd 服务与网络配置，生成 `manifest.json`，并通过 Restic + rclone 将加密增量备份上传到每台服务器独立的 Google Drive Repository。


## 一键安装

在全新的 Ubuntu / Debian VPS 上，可以直接执行以下命令从 GitHub 仓库下载安装向导：

```bash
curl -fsSL https://raw.githubusercontent.com/J2026-dev/vbakup/main/install.sh | sudo bash
```

如果仓库默认分支不是 `main`，可以指定分支，例如：

```bash
curl -fsSL https://raw.githubusercontent.com/J2026-dev/vbakup/main/install.sh | sudo VBACKUP_BRANCH=master bash
```

安装向导会自动下载 `https://github.com/J2026-dev/vbakup` 的源码、安装依赖、创建 `/usr/local/bin/vbackup` 命令、配置 systemd timer，并引导你填写 Google Drive、Restic 与 Telegram 配置。

> 一键安装命令通过管道执行脚本，但安装器会从 `/dev/tty` 读取交互输入；看到 `Google Drive Client Secret` 或 `Refresh Token` 时输入不会显示，这是正常的，直接按回车即可跳过可选项。
> 如果看到 `failed to create oauth client: empty token found`，说明已有的 `gdrive` remote token 无效。请执行 `rclone config reconnect gdrive:` 修复，或重新运行安装器并填写 Google Drive Refresh Token。

## 中文安装步骤

### 1. 准备系统

支持的系统：

- Ubuntu 20.04 / 22.04 / 24.04
- Debian 11 / 12

建议使用 root 用户或具备 sudo 权限的用户执行安装：

```bash
sudo -i
```

### 2. 下载或进入 VBackup 目录

如果你已经在源码目录中，直接进入项目目录：

```bash
cd /path/to/vbackup
```

如果是从 Git 仓库安装，请先克隆项目并进入目录：

```bash
git clone https://github.com/J2026-dev/vbakup vbackup
cd vbackup
```

### 3. 运行安装向导

执行安装脚本：

```bash
sudo ./install.sh
```

安装向导会要求填写：

- Server Name：当前 VPS 的唯一名称，例如 `hk-vps-01`
- Google Drive rclone remote：rclone 中配置的 Google Drive remote 名称，默认 `gdrive`
- Google Drive Client ID：Google Drive OAuth Client ID；如果 remote 已配置可留空
- Google Drive Client Secret：Google Drive OAuth Client Secret；如果 remote 已配置可留空
- Google Drive Refresh Token：Google Drive Refresh Token；如果 remote 已配置可留空
- Restic Password：Restic 仓库加密密码
- Telegram Token：Telegram Bot Token，可留空
- Telegram Chat ID：Telegram 接收通知的 Chat ID，可留空

安装脚本会自动：

- 安装依赖：`restic`、`rclone`、`jq`、`curl`、`tar`、`zstd`
- 将程序安装到 `/opt/vbackup`
- 写入配置到 `/etc/vbackup/server.conf`
- 创建 `/usr/local/bin/vbackup` 命令
- 安装并启用 systemd timer
- 初始化 Restic Repository
- 执行环境测试
- 执行首次备份

### 4. 配置 Google Drive / rclone

安装前或安装后都可以配置 rclone。推荐先配置：

```bash
rclone config
```

创建 Google Drive remote 时，请确保 remote 名称与安装向导中填写的一致，例如：

```text
gdrive
```

VBackup 会将当前 VPS 的 Restic 仓库放在：

```text
rclone:gdrive:VBackup/<SERVER_ID>
```

例如：

```text
rclone:gdrive:VBackup/hk-vps-01
```

> 注意：每台 VPS 必须使用独立的 `SERVER_ID`，禁止多台服务器共用同一个 Restic Repository。

### 5. 检查定时备份

VBackup 默认使用 systemd timer，不使用 crontab。查看下一次执行时间：

```bash
vbackup schedule
```

查看 timer 状态：

```bash
vbackup status
```

### 6. 手动执行备份

```bash
vbackup backup
```

备份完成后可以查看 Restic 快照：

```bash
vbackup snapshots
```

### 7. 执行恢复

在新 VPS 或灾难恢复环境中安装 VBackup 后，执行：

```bash
vbackup restore
```

恢复向导会让你选择快照；也可以使用自动模式恢复指定快照：

```bash
vbackup restore auto latest
```

恢复流程会按固定顺序执行：网络、系统文件、数据库、Docker、systemd、网站、SSL、验证和 Telegram 通知。

### 8. 执行恢复验证

```bash
vbackup verify
```

验证结果会写入：

```text
reports/restore-result.json
```

### 9. 查看日志

```bash
vbackup logs
```

日志默认按日期写入：

```text
/var/log/vbackup/YYYY-MM-DD.log
```

## Quick start

```bash
sudo ./install.sh
vbackup backup
vbackup restore
vbackup verify
```

See `docs/` for installation, restore, Google Drive, security and troubleshooting notes.
