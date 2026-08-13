# vBakup

面向 Linux VPS 的主控式灾难备份与恢复系统。主控提供 Web 控制台、计划调度和备份索引；被控 agent 自动发现服务，通过主动出站 HTTPS 获取任务，并把备份直接上传到 WebDAV。被控机不需要开放任何端口。控制台支持节点备注、节点/备份策略选择，备份路径会包含节点关键词，便于识别来源。

项目仓库：[J2026-dev/vbakup](https://github.com/J2026-dev/vbakup)

> 当前为可运行的早期版本。请先在测试 VPS 完成备份与恢复演练，再用于生产。

## 功能

- 一个主控管理所有 VPS，提供节点、WebDAV、计划、备份和恢复视图。
- 一条命令安装 agent，注册后由 systemd 常驻，无需后续手工配对。
- 自动识别 1Panel、Docker、Xray、sing-box、Komari Agent、Cloudreve、MySQL、PostgreSQL、Redis、Nginx 及常用数据目录。
- 数据库优先执行逻辑一致性导出；Docker 保存 inspect、Compose、原工作目录和 `.env` 元数据。
- 支持每小时、每 6 小时、每 12 小时、每天、每周以及 API 自定义 Go duration（最短 15 分钟）。
- 备份上传前记录 SHA-256；恢复下载后强制复验，归档解包防路径穿越。
- 自动合并服务发现与用户附加路径，覆盖应用配置、用户数据、`/srv`、`/opt`、`/var/lib`、`/var/log` 等；排除虚拟文件系统、缓存和运行时目录。
- Docker 备份记录容器原运行状态，短暂停止运行容器后归档持久卷/绑定目录，并在完成后恢复原状态；数据库优先使用官方逻辑导出工具。
- 原 VPS 损坏后，在新 VPS 执行相同安装命令，再从控制台选择备份和新节点恢复。
- WebDAV 密码由主控本地 AES-GCM 主密钥加密；agent token 仅保存 SHA-256 哈希。
- 被控只发起 `443/tcp` 出站请求，可置于 NAT、防火墙或 Cloudflare 代理之后。
- 主控页面每 10 秒静默同步节点、策略和快照状态，切回页面时也会立即刷新，无需手动重新加载。
- 使用站内登录页面和 HttpOnly 会话 Cookie，不再触发浏览器 Basic Auth 弹窗；可在面板退出、修改密码或撤销全部旧会话。
- 节点列表和详情显示 agent 最近一次心跳的来源 IP；经 Cloudflare 时优先读取 `CF-Connecting-IP`，其次使用标准代理转发地址和连接地址。
- 快照 manifest v2 记录识别到的服务、服务管理器、unit、备份前启用/运行状态以及系统快捷命令清单。

## 架构

```text
浏览器 ──HTTPS──> 主控 / 控制台
                    │  调度、索引、命令
                    │
VPS agent ──HTTPS 出站轮询──┘
    │
    ├── 自动发现 / 数据库 dump / tar.gz / SHA-256
    └────────HTTPS────────> WebDAV
```

备份数据不经过主控，因此不会占用主控双倍带宽。主控保存的是任务、备份路径、校验和及加密后的 WebDAV 凭据。

## 三步完成部署

主控和被控都面向 Linux。安装器支持 Debian/Ubuntu、RHEL 系和 Alpine（systemd 或 OpenRC），CPU 支持 `amd64`、`arm64`。

> 仓库维护者首次部署前需完成一次发布：在 GitHub Packages 中把 `ghcr.io/j2026-dev/vbakup` 设为 Public，并推送一个 `v*` tag（例如 `v0.1.0`）。Release workflow 会发布 Linux agent、SHA-256 校验文件和多架构主控镜像。普通使用者无需执行此步骤。

### 1. 准备域名

将一个域名的 A 记录指向主控 VPS，例如 `backup.example.com`。首次安装时如果使用 Cloudflare，请先关闭代理（灰云），并确保 VPS 防火墙允许入站 `80/tcp`、`443/tcp` 和 `443/udp`。

### 2. 一条命令安装主控

在主控 Linux VPS 上运行：

```bash
curl -fsSL https://raw.githubusercontent.com/J2026-dev/vbakup/main/scripts/install-controller.sh \
  | sudo sh -s -- --domain backup.example.com
```

如果 Docker 安装曾在 `docker-model-plugin` 处失败，先更新到最新仓库代码，再直接重新运行上面的同一条命令；安装脚本会复用已有配置并继续安装。Ubuntu 20.04 已停止标准支持，脚本仍可安装 Docker，但生产环境建议升级到 Ubuntu 22.04 或 24.04 LTS。

安装器会自动完成：

- 安装 Docker 与 Compose（若尚未安装）；
- 拉取 `ghcr.io/j2026-dev/vbakup:latest`；
- 生成管理员密码和独立的 agent 注册密钥；
- 启动 controller 与 Caddy，由 Caddy 自动申请 HTTPS 证书；
- 将配置和数据保存到 `/opt/vbakup`。

命令结束时会打印面板地址、管理员密码和注册密钥。访问 `https://backup.example.com` 即可登录。重复运行安装命令会更新容器并保留现有密码和数据。

如果安装器提示域名尚未解析，请等待 DNS 生效后重试。若 `80/443` 已被 Nginx、1Panel 或其他程序占用，请使用“手动部署主控”并接入现有反向代理，不要同时启动内置 Caddy。

### 3. 接入被控 VPS

登录控制台，点击“添加节点”，在每台被控 Linux VPS 执行面板给出的一条命令。约 30 秒后节点上线，不需要开放任何被控端口。

接着在面板添加 WebDAV、创建备份计划，即可开始备份。

主控 VPS 也可以安装 agent 并作为被控节点。agent 运行在宿主机 systemd 中；备份 Docker 时会短暂停止包括主控在内的运行容器，直接把归档上传到 WebDAV，随后恢复原先运行的容器并向主控上报结果。备份窗口内控制台短暂不可访问属于预期行为。

## 手动部署主控

需要自定义网络或已有反向代理时：

```bash
git clone https://github.com/J2026-dev/vbakup.git
cd vbakup
cp .env.example .env
# 编辑 .env
docker compose pull
docker compose up -d
```

本地开发构建使用：

```bash
docker compose -f compose.yaml -f compose.dev.yaml up -d --build
```

## 添加被控 VPS

在控制台点击“添加节点”，复制命令并在被控 VPS 运行。agent 支持 Linux `amd64` 和 `arm64`，安装到 `/usr/local/bin/vbakup-agent`，配置位于 `/etc/vbakup/agent.json`。

```bash
curl -fsSL https://backup.example.com/install.sh | sudo sh -s -- \
  --controller https://backup.example.com \
  --secret YOUR_BOOTSTRAP_SECRET
```

之后不需要打开端口或继续配置。约 30 秒内节点会出现在控制台。
重复执行同一安装命令会更新 agent 二进制并保留现有节点身份，不会在控制台创建重复节点。

一键安装还会部署本地 SSH 管理工具。自动升级默认关闭，可在控制台“节点详情”中开启，也可直接在节点执行：

```bash
vbakup-agentctl status
vbakup-agentctl logs
vbakup-agentctl update
vbakup-agentctl auto-update on
vbakup-agentctl auto-update off
vbakup-agentctl uninstall
```

`uninstall` 会要求输入 `UNINSTALL`，然后删除 agent 服务、节点身份和本地工作数据；WebDAV 备份不会被删除。卸载后可在控制台删除离线节点记录。systemd 节点开启自动升级后每天检查一次最新 Release，并有最多 6 小时的随机延迟，避免所有节点同时下载。

## 创建备份

1. 在“备份空间”添加 WebDAV 地址、用户名、密码和基础目录。
2. 在“备份计划”选择节点、WebDAV 和频率。
3. 保持 Docker 与数据库选项开启。特殊服务可按每行一个绝对路径补充，例如 `/srv/my-app`。
4. 等待计划运行，或点击“立即备份”。完成后可在“灾难恢复”查看大小和校验和。

数据库逻辑导出依赖本机 `mysqldump`、`pg_dumpall`、`redis-cli` 的现有 socket/免密权限。如果导出失败，备份仍会继续、报告警告并归档已发现的数据目录，但运行中数据库的物理文件不等同于一致性备份。生产环境应为备份用户配置最小导出权限，并定期检查备份日志和执行恢复演练。

## VPS 损坏后的恢复

1. 准备同架构、同发行版的大致空白 VPS。
2. 在新 VPS 执行控制台显示的同一条 agent 安装命令。
3. 等待新节点在线，在“灾难恢复”选择目标备份并点击“恢复”。
4. 选择新节点，确认覆盖后下发任务。
5. agent 下载归档、验证 SHA-256、恢复文件和数据库 dump，并尝试重启已识别的 systemd 服务。
6. agent 会把 Compose 元数据恢复到原工作目录，并执行 `docker compose up -d`。
7. 在控制台检查恢复任务结果，以及应用域名、IP 绑定、证书、数据库账号、Docker 网络等警告。

恢复不会自动修改 DNS，也无法可靠重建云厂商安全组、外部密钥、未备份的远端依赖或硬编码旧 IP。完整灾备必须把这些项目纳入演练清单。

恢复时会自动跳过 vBakup agent 自身的二进制、配置、结果缓存和 systemd/OpenRC 单元，避免正在运行的 agent 被覆盖而触发 `text file busy`。旧版本创建的快照也使用相同的跳过规则，可以直接恢复。

新快照会保存 systemd/OpenRC 服务的实际 unit、备份前运行状态和启用状态。恢复时先还原 unit 与数据，再执行 systemd `daemon-reload`，重新启用原本启用的服务，并只重启备份前正在运行的服务；sing-box、Xray、1Panel 等使用同一套恢复流程。旧 manifest 仍按内置服务映射兼容恢复。

agent 还会扫描 `/usr/local/bin`、`/usr/local/sbin`、用户 `.local/bin` 及常见 shell profile，记录自定义可执行文件、符号链接和 alias 名称。处于用户/本地命令目录及 profile 中的内容会随文件归档恢复；清单只记录 alias 名称和来源文件，恢复过程不会主动执行 alias 内容。系统发行版管理的 `/usr/bin` 不作为自定义快捷命令恢复来源。

## Cloudflare 小云朵

主控域名可以打开 Cloudflare 代理：

- 首次安装时保持灰云，等待 `https://你的域名` 可以正常访问后再打开橙云。
- SSL/TLS 模式使用 **Full (strict)**，源站必须有有效证书。
- WebSockets 不是必需项；agent 使用普通 HTTPS 轮询，兼容 Cloudflare CDN。
- 不要为 `/api/agent/*` 和 `/install.sh` 配置缓存。
- 若使用 Cloudflare Access，需为 agent 路径配置 Service Token 或旁路策略；否则 agent 会被登录页阻断。
- 更换连接域名时，先修改 `VBAKUP_PUBLIC_URL` 并重启主控，然后更新 `/etc/vbakup/agent.json` 的 `controller` 字段并重启 agent：

```bash
sudo sed -i 's#https://old.example.com#https://new.example.com#' /etc/vbakup/agent.json
sudo systemctl restart vbakup-agent
```

agent 不允许跳过 TLS 校验。需要改变连接方式时，推荐在主控前增加 Cloudflare Tunnel/Caddy/Nginx，保持 agent 端仍为标准 HTTPS。

## 运维与安全

- 必须备份主控的 `data/state.json` 和 `data/master.key`；缺少主密钥将无法解密 WebDAV 密码。
- 注册命令含 bootstrap secret，不要贴到 issue、日志或聊天群；节点全部接入后可更换该密钥并重启主控。
- 使用专用 WebDAV 账号和独立目录，启用服务端版本控制/对象锁（若提供）。
- 主控登录页必须放在 HTTPS 后；更高安全要求可在前面叠加 Cloudflare Access/OIDC。
- agent 为读取系统数据和执行恢复而以 root 运行。只把主控部署在受信任环境，严格保护主控账户。
- 每月至少做一次隔离恢复演练。只有被实际恢复并验证过的备份才可信。

常用主控运维命令：

```bash
cd /opt/vbakup
docker compose ps
docker compose logs -f
docker compose pull && docker compose up -d
```

一键安装会同时安装 `vbakupctl`：

```bash
vbakupctl status
vbakupctl logs
vbakupctl update
vbakupctl uninstall
vbakupctl uninstall --purge
vbakupctl delete backup BACKUP_ID
vbakupctl delete node NODE_ID
```

`status` 会显示主控容器、Docker 空间、主控数据目录及节点/计划/快照/空间数量。删除命令会提示输入当前管理员密码，并使用与 Web 面板相同的引用保护。
主控 `uninstall` 默认保留 `/opt/vbakup` 中的数据，`--purge` 会二次确认后永久删除主控配置、凭据和索引，但不会删除 WebDAV 上的备份对象。

## 开发

要求 Go 1.22+：

```bash
go test ./...
go run ./cmd/controller
```

环境变量见 `.env.example`。控制台和安装脚本嵌入 controller 二进制，无额外前端构建步骤。

创建 GitHub Release（推荐，无需在本地创建标签）：

1. 打开仓库的 `Actions` 页面并选择 `Release`。
2. 点击 `Run workflow`，输入版本号（例如 `v0.1.0`）后确认。
3. 等待所有任务变为绿色；工作流会自动创建标签、Release、Linux agent 附件和多架构主控镜像。

也可以从终端推送一个以 `v` 开头的新标签：

```bash
git tag v0.1.0
git push origin v0.1.0
```

版本号必须采用 `v0.1.0` 这种格式；`0.0.1` 这类不带 `v` 的标签不会触发发布。等待 Release workflow 全部通过后，再运行主控和 agent 一键安装命令。

## 项目结构

```text
cmd/controller/      主控、API、调度器和嵌入式 Web UI
cmd/agent/           被控 agent 主循环与任务执行
internal/agent/      服务发现、归档、校验和恢复
internal/webdav/     WebDAV 客户端
internal/store/      原子 JSON 状态存储
internal/vault/      AES-GCM 凭据加密
.github/workflows/   CI 与 tag 发布
```

## License

[MIT](LICENSE)
