# 部署与维护

## 部署方式

默认部署只需下载 `compose.yaml`，命令见[快速部署](../README.md#快速部署)，无需 clone 源码。单容器运行 Caddy 与 Manager，通过私有 Unix Socket 管理，只发布 80 TCP、443 TCP/UDP；首次部署不需要 `.env`、预建数据目录或提前提供 Cloudflare Token。

完整可选项见 [compose.full.yaml](../compose.full.yaml)，用 `docker compose -f compose.full.yaml up -d` 独立运行，不与默认文件同时加载。可选项默认注释，按需启用；替换已有部署时须保持项目名一致，例如加 `-p caddy-admin`，避免同时运行两个 Manager。

本地源码构建使用 [compose.build.yaml](../compose.build.yaml) 覆盖，镜像标签为 `caddy-admin:local`：

```bash
git clone --depth 1 https://github.com/gofxq/caddy_admin.git caddy-admin
cd caddy-admin
docker compose -f compose.yaml -f compose.build.yaml up -d --build
```

后续维护使用相同文件组合；外部模式再追加 `-f compose.external.yaml`。隔离验证见[从零测试](testing.md)。

## 初始化与访问边界

安装命令见 [README](../README.md)。内置模式通过 `https://服务器IP/setup` 初始化，80 仅跳转至 HTTPS；完成后 Caddy 接管 80/443。首次证书为临时自签名证书。

第一个有效提交者成为唯一管理员，请在可信网络立即完成初始化。填写 Homelab 域名后，控制台地址为 `https://caddyadmin.<Homelab域名>`；Public 域名初始为空，当前不能在界面中添加。

Setup 只接受私有、回环或链路本地 TCP 对端，以及同类 IP Host 或本机 `localhost`。HTTPS `Origin` 必须与请求 Host/端口精确一致，直连 Setup 不信任转发来源头。Docker/NAT 可能将公网来源表现为私有网关，初始化前必须用宿主防火墙限制为可信 LAN/VPN。

向导的「DNS」步骤按 Homelab 域名生成 `*.<Homelab域名>` 的 A/AAAA 记录指引。内置模式建议使用当前访问的私网 IP；通过 localhost、回环地址访问或使用外部模式时，生成记录指引需手动填写实际 Caddy 地址。该地址仅用于页面指引，不写入部署配置，也不会自动修改 Cloudflare。选择「仅 DNS（灰云）」；内网 IP 仍只对能到达该网络的设备可用。已有单独子域记录优先于通配符，需核对其目标。DNS-01 签发域名证书无需向公网开放入站端口。

重新检查会使用填写的解析器确认控制台域名有解析结果，不证明它指向建议 IP，也不证明浏览器可达或 HTTPS 已就绪。检查失败时保留输入，可以重试。

第一个有效提交原子创建管理员，并发输家返回 409。网络/DNS 警告需明确确认，不能绕过字段与安全校验。密码只通过 HTTPS Setup 或维护终端输入；Cloudflare Token 只通过设置页写入受限 secret 文件，不放入环境、命令行或版本控制。

初始化后的 IP HTTPS 入口提供交接状态；内置模式还允许受 LAN、认证、Origin 和 CSRF 保护的登录与管理操作。Caddy 经容器回环桥接转发并覆盖客户端地址头，未知域名返回 404。浏览器至少读到一次正式入口就绪后，临时入口保留五分钟再关闭。外部模式的交接入口只提供静态页面和只读状态。

提交响应丢失或重启期间刷新时，保留输入并查询交接状态；不要重放初始化请求。临时入口失联时使用页面中的正式地址，并检查容器、DNS、证书、反代和防火墙。

## 配置迁移

在设置页导出当前服务草稿与域名、网络策略。下载文件名带 UTC 时间戳，例如 `caddy-admin-config-2026-10-02T12-34-56-789Z.json`。现有实例上传后先预览，再明确确认替换全部草稿；文件中的域名和网络策略不会覆盖实例设置，服务按当前策略重新校验。空服务列表会清空草稿；并发修改返回 409，须重新预览。导入不改变线上版本、启动快照或历史记录，之后仍需校验与发布。

新实例可在初始化的管理员步骤上传并采用配置，核对向导中的域名、网络、DNS 和服务列表后确认。服务以草稿保存；管理员密码和 Cloudflare Token 必须重新配置。当前向导仅支持标准 Homelab 控制台地址，不支持导入 Public 域名、自定义控制台标签或 Origin 端口，遇到这类文件会明确拒绝。

JSON 文件最大 60 KiB，仅包含本项目的业务配置，不接受 Caddyfile 或原始 Caddy JSON。它包含内网地址等信息，请妥善保存；没有密码、会话、Token、证书或发布历史，不能代替下面的完整备份。

## 持久化

| 目录 | 内容 |
| --- | --- |
| `.run/manager` | SQLite、WAL、实例锁、受限 secret |
| `.run/snapshots` | 内置 Caddy 启动快照 `active.json` |
| `.run/caddy-data` | 证书、私钥、ACME 状态 |
| `.run/caddy-config` | Caddy 配置数据 |
| 容器 `/run/caddy` | 运行时 Socket；可选挂载 `.run/caddy-socket`，不纳入备份 |

Compose 仅启用必要项；可选配置以 `# 配置项 # 说明` 形式列出，先取消配置行开头的注释，再设置对应环境变量。运行时 Socket 默认留在容器内，不需要宿主挂载。

Token 文件统一默认为 `${DATA_DIR}/secrets/cloudflare_token`（默认 `/var/lib/manager/secrets/cloudflare_token`），保存、校验和启动均使用此路径。仅自定义文件位置时配置 `CLOUDFLARE_API_TOKEN_FILE`，并保证目录持久化且 `10001:10001` 可读写；完整示例中同时取消 `environment` 及该配置行的注释。Token 本身仍只通过设置页提交。

服务进程以 `10001:10001` 运行。Compose 自动创建上述宿主目录，仅挂载当前数据目录，不读取旧 `run/`。初始化和业务设置保存在 SQLite，不再从旧域名、网络环境变量导入；没有目录迁移或手工补建启动快照命令。

## 备份、恢复与升级

当前没有项目级备份、恢复或离线救援命令。先停止容器，对上述四个持久目录取得一致、加密的宿主机快照，并在隔离副本验证完整恢复。仅复制 SQLite 不够；外部 Caddy 数据需单独保护。

升级前完成备份，预构建镜像部署执行：

```bash
docker compose pull
docker compose up -d
```

源码部署更新源码后使用 `docker compose -f compose.yaml -f compose.build.yaml up -d --build`；外部模式追加对应文件。维护期间始终沿用原项目名、文件组合和数据目录。需要固定镜像时将 `image` 改为发布的 `sha-完整提交SHA` 标签或镜像 digest；`latest` 会随默认分支的新发布更新。

只接受空库或结构完整的当前 v6 数据库，不支持旧版本升级或自动兼容导入。管理员与设置必须同时存在或同时为空；部分实例、不支持的版本及损坏结构会明确报错，保留数据。不要同时运行两个 Manager，也不要删除数据库、锁或快照来绕过失败。更新不主动改写历史发布 JSON 或启动快照。

故障恢复优先在 Web 中回滚成功版本；回滚按当前策略和 DNS 重新生成、校验、发布，保留草稿。若管理入口不可用，停止写入、保留数据，从已验证快照恢复或在副本诊断。不要手工写 Admin API、数据库或启动 JSON 来宣称恢复成功。

## 密码与历史维护

CLI 与 Manager 共用实例锁，维护前停止容器：

```bash
docker compose stop caddy
docker compose run --rm --no-deps caddy manager reset-password
docker compose start caddy
```

密码通过终端隐藏读取，重置后撤销旧会话。只读查询发布历史时，将 `reset-password` 替换为 `history`。

## 外部 Caddy 模式

使用 `compose.yaml` 与 `compose.external.yaml`。在已有 `compose.yaml` 的部署目录中执行，将示例主机名替换为实际地址，并限制 Manager 与 Admin API 的网络边界：

```bash
curl -fL https://raw.githubusercontent.com/gofxq/caddy_admin/HEAD/compose.external.yaml -o compose.external.yaml
CADDY_ADMIN_URL=https://caddy.internal:2019 \
MANAGER_DIAL=manager-host.internal:8081 \
MANAGER_BIND=0.0.0.0:8081 \
docker compose -f compose.yaml -f compose.external.yaml up -d
```

外部实例必须专供本项目管理，不能并行使用其他配置入口。

| 参数 | 用途 |
| --- | --- |
| `CADDY_ADMIN_URL` | 完整 HTTP(S) Admin API 地址，含端口 |
| `MANAGER_DIAL` | 外部 Caddy 可达的 Manager 地址；经宿主映射时须使用宿主发布端口 |
| `MANAGER_BIND` | Manager 宿主绑定，默认 `127.0.0.1:8081`，仅允许外部 Caddy 访问 |
| `SETUP_BIND` | HTTPS Setup 宿主绑定，默认 `0.0.0.0:8080-8099`，仅允许可信 LAN/VPN |
| `CADDY_PROBE_ADDRESS` | 可选；默认 Admin API 主机名加 443，覆盖时先取消外部 Compose 对应行的注释 |

查询 Setup 地址：`docker compose -f compose.yaml -f compose.external.yaml port caddy 8082`。宿主 80/443 由外部 Caddy 所有。HTTPS Admin API 验证系统信任链且不跟随重定向；不可达时不回退到内置实例。

首次 Setup 只写本地状态和基线快照。外部 Caddy 需预先将控制台域名反代到 `MANAGER_DIAL`，自行配置 Cloudflare 凭据。正式入口就绪后登录、校验并确认首次发布。发布保留远端实际 `admin`/`storage`，其余业务由本项目完整管理；本地校验不能证明远端模块、凭据或存储可用。

内外模式切换前停服并保护两端数据，核对启动快照与目标实例兼容性；项目不会自动迁移快照，也不会自动发布到另一实例。

## 自定义端口与验证

默认 `compose.yaml` 的宿主绑定直接修改 `80:80`、`443:443` 和可选的 UDP 映射，例如 `192.168.1.100:8080:80`、`192.168.1.100:8443:443`；将示例 IP 替换为服务器实际内网地址。完整示例 `compose.full.yaml` 支持 `HTTP_BIND`/`HTTPS_BIND`。重建容器后生效，非标准 HTTPS 端口需显式访问，例如 `https://服务器IP:8443/setup`；HTTP 默认跳转仍指向 443。本地 `.env` 若存在仍会被 Compose 自动读取，避免其中残留旧覆盖或凭据。

例如在默认文件中替换端口映射：

```yaml
- '192.168.1.100:8080:80'
- '192.168.1.100:8443:443'
- '192.168.1.100:8443:443/udp'
```

修改后执行 `docker compose up -d`；访问 `https://实际内网IP:8443/setup`。

隔离检查和手工验证入口见 [验证说明](verification.md)。测试数据与真实运行数据分开，不使用真实 Token 或生产 Caddy。

## 镜像发布

GitHub Actions 的 `verify` 工作流在默认分支 push 或手动运行时，先完成检查和本地镜像构建，再发布 `ghcr.io/gofxq/caddy-admin:latest` 及 `sha-完整提交SHA`，构建目标为 `app`，平台为 `linux/amd64` 和 `linux/arm64`。PR、其他分支和 fork 不发布该镜像；使用工作流自身的 `GITHUB_TOKEN` 和 `packages: write` 权限，无需额外 registry 密钥。

维护者首次发布后，将 GitHub Packages 中的 `caddy-admin` 包设为 Public，使用户可匿名拉取。若同名包已经存在，确认它关联到 `gofxq/caddy_admin` 并允许该仓库工作流写入。首次发布与公开设置完成前，安装命令无法拉取镜像，可使用源码构建覆盖文件。
