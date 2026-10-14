# 部署与维护

## 部署方式

默认部署只需下载 `compose.yaml`，命令见[快速部署](../README.md#快速部署)，无需 clone 源码。单容器运行 Caddy 与 Manager，通过私有 Unix Socket 管理，只发布 80 TCP、443 TCP/UDP；首次部署不需要 `.env`、预建数据目录或提前提供 Cloudflare Token。

完整可选项见 [compose.full.yaml](../compose.full.yaml)，用 `docker compose -f compose.full.yaml up -d` 独立运行，不与默认文件同时加载。可选项默认注释，按需启用；替换已有部署时须保持项目名一致，例如加 `-p caddy-admin`，避免同时运行两个 Manager。

本地源码验证使用 [compose.validation.yaml](../compose.validation.yaml) 覆盖，包含构建配置，镜像标签为 `caddy-admin:local`，数据保存在独立验证卷中：

```bash
git clone --depth 1 https://github.com/gofxq/caddy_admin.git caddy-admin
cd caddy-admin
make up
make status
```

`make down` 停止并删除验证实例及其 volumes；需要清空后重新构建启动时，运行 `make freshup`。管理员、草稿、设置、审计与发布历史、持久化 Token、快照和证书数据将永久删除。此命令仅用于主动重置验证实例，不能作为初始化或恢复失败的修复手段；不会清理正式部署的 `.run/`。Compose 文件、`.env` 与外部 DNS 记录保留，预配置参数仍会生效。

后续维护使用相同项目名和文件组合。源码验证使用独立卷，但端口沿用默认 Compose 映射；与正式实例同宿主运行前须显式调整验证端口。外部模式再追加 `-f compose.external.yaml`。从零验证见[测试流程](testing.md)。正式部署继续使用预构建镜像，不加载验证覆盖。

## 初始化与访问边界

安装命令见 [README](../README.md)。内置模式通过 `http://服务器IP/setup` 初始化，也可使用 `https://服务器IP/setup`。初始化前 80 不强制 HTTPS，完成后 Caddy 接管 80/443，HTTP 恢复 HTTPS 跳转。HTTP 会明文传输手动填写的密码和 Token，建议使用 HTTPS；HTTPS 初始化和证书签发过渡期可能出现临时证书提示。

第一个有效提交者成为唯一管理员。Setup 只有「管理员 → 首个域名与 DNS → 确认」三步，不要求可信网段、上游许可或初始化设备绑定。首个域名默认 `internet`，控制台为 `https://caddyadmin.<首个基础域名>`。普通初始化可信与上游网段均为 `0.0.0.0/0`、`::/0`，服务名许可为 `["*"]`，控制台来源限制默认关闭。登录后可直接添加服务草稿，或收紧业务与上游许可；启用控制台 LAN/VPN 限制前须将全网段替换为实际可信网段，再校验和确认发布。已有实例或显式导入沿用原策略，不因升级或重启变为开放。

Setup 不按 TCP 来源是否私网或 Host 是否私网 IP 拒绝访问，公网 DNS 目标同样支持。HTTP/HTTPS `Origin` 必须与请求 Host/端口精确一致，不信任任意转发来源头。端口映射、监听地址和宿主防火墙决定实际可达性。临时入口及正式登录页默认不增加来源门禁；未认证者不能访问管理 API。

浏览器 DNS 验证使用 DoH，默认 Cloudflare，可明确选择 Google。点击「检查 DNS」才从浏览器向所选服务发送控制台和随机子域的 A/AAAA 查询，不发送密码、Token、LAN 或上游策略，不携带 Cookie 或 Referer。当前只提供已支持 JSON/CORS 的固定服务，不支持任意 DoH URL。DoH 查询不改变持久化设置。服务器 DNS 放在「高级：服务器 DNS」，默认 `1.1.1.1`，仅接受 IP/端口，用于 Caddy 证书 DNS 校验、预检及交接诊断；可采用服务器建议或填写可信内网 DNS。

| 浏览器快捷选择 | DoH 地址 | 官方说明 |
| --- | --- | --- |
| Cloudflare（默认） | `https://cloudflare-dns.com/dns-query` | [JSON DoH](https://developers.cloudflare.com/1.1.1.1/encryption/dns-over-https/make-api-requests/dns-json/) |
| Google | `https://dns.google/resolve` | [JSON DoH](https://developers.google.com/speed/public-dns/docs/doh/json) |

内置模式的「首个域名与 DNS」步骤填写或确认使用已配置 Cloudflare Token 与实际 Caddy IP，先点击「检查 DNS」。服务器读取 Cloudflare 核对 Token 的目标域名访问权限以及 `*.<首个基础域名>` 的 A/AAAA 记录，浏览器核对解析地址；已有记录符合要求且解析通过即可直接继续，不需要 DNS 配置确认，也不写入 DNS。展开「获取 Token 与权限」可查看[创建 Token](https://dash.cloudflare.com/profile/api-tokens)和[官方说明](https://developers.cloudflare.com/fundamentals/api/get-started/create-token/)；只授权目标 Zone 的 Zone Read、DNS Edit。仅需新增或修改记录时才展示 DNS 变更预览；勾选变更影响后点击「确认配置」，立即配置「仅 DNS（灰云）」，这个按钮不等待 DNS 生效。再单独点击「检查 DNS」，结果表展示所选 DoH 查询的控制台和随机一层子域、返回 IP 及检查结果。A 与 AAAA 均须查询成功，没有 AAAA 地址可接受；任一查询失败、返回非目标地址或全部无地址均不通过，保留成功返回的地址。空回答提示可能未配置或被过滤，NXDOMAIN、HTTP/网络失败和超时分别提示。所有返回地址须符合目标 IP；未生效、地址不符或失败时保留输入，可直接重查，无需重复写入。已有记录已核对或变更已确认写入，并且查询通过后才能继续；更换 DoH 只要求重查，不清空已确认的记录；域名、目标 IP 或 Token 改变须重新检查，需要变更时重新预览确认。最终提交由服务器重新预检并核对 Cloudflare 记录与检查或确认指纹，不再通过普通 DNS 解析强制阻断初始化。初始化完成后自动申请证书。当前访问 IP 仅是建议，localhost 与外部模式须填写实际 Caddy 地址；内网 IP 仅对能到达该网络的设备可用。

相同记录直接复用；地址或代理状态不同会显示旧值并要求确认，确认后记录再变化则返回冲突。CNAME、同类型重复记录或覆盖通配符的控制台精确记录不一致时阻断；保留另一地址类型并提示其访问影响，不修改其他记录。外部模式仍提供手动 DNS 指引，远端凭据由外部 Caddy 管理。DNS-01 签发无需向公网开放入站端口。

DNS 与本地初始化不能跨服务原子提交。Token 写入受限 secret 文件，非敏感确认意图写入 `${DATA_DIR}/secrets/setup_dns_intent.json`；响应丢失后先读取实际记录，重复提交复用已完成操作。DNS 成功但本地写入失败会明确说明外部影响，保留数据并修复存储后重新预览提交，不会自动撤销已有 DNS 变更。进程在快照写入与数据库提交之间退出时，只接受与保存意图指纹匹配的候选；其他已有快照仍拒绝覆盖。

浏览器检查控制台和随机一层子域。提供目标 IP 时，全部地址必须与目标一致；外部或测试模式可不填目标 IP，此时只检查是否有解析结果并明确说明未核对目标。外部和测试模式查询可选，不自动写入 DNS。浏览器 DoH 通过不证明设备可达、服务器 DNS 可用或 HTTPS 已就绪；服务器未解析到控制台地址仅作提示，不阻止初始化；解析器不可达等其他警告仍须阅读影响并确认。

第一个有效提交原子创建管理员，并发输家返回 409。预检警告需明确确认，确认绑定规范化设置与具体警告；最终复检出现新增或变化的警告时保留输入、刷新预检并要求重新确认，不能沿用旧勾选。页面不在提交前重复请求预检，可手动「重新预检」。内置自动 DNS 的浏览器目标 IP 查询必须通过才能从页面继续；API 最终完成仍核对实际 Cloudflare 记录、变更指纹、网络安全策略与已有快照归属，不信任浏览器传回的 DNS 结果。密码和 Token 可通过 Setup 手动输入或部署环境预配置确认使用；Token 自动写入受限运行文件，不回显、不记日志，不将真实凭据提交版本控制。

初始化后的 IP HTTPS 入口提供交接状态；内置模式还允许受认证、Origin 和 CSRF 保护；仅显式启用控制台限制时再执行 LAN 校验的登录与管理操作。Caddy 经容器回环桥接转发并覆盖客户端地址头，未知域名返回 404。浏览器实际从正式入口认证访问后，临时入口保留五分钟再关闭。外部模式的交接入口只提供静态页面和只读状态。

提交响应丢失时，当前页面保留输入并核对交接状态；状态读取等待后台初始化操作结束，只有确认未完成才恢复表单，状态暂时不可读时继续核对，不自动重放请求。刷新或重新打开页面不会恢复密码和 Token，两者不写入浏览器持久化存储。HTTP 页面成功提交后进入 HTTPS 交接页；响应丢失时使用页面中的 HTTPS 交接链接，证书签发完成前可能需要接受临时证书提示。交接页区分「初始化已完成」与「正在核对初始化结果」，分别显示 Manager、服务端 DNS、HTTPS 证书与路由状态，并提供重新核对、正式入口及适用的临时登录入口。初始化已保存、Manager 正常且 HTTPS 证书与路由探测通过后自动进入正式控制台（未登录时进入登录页），服务器 DNS 提示不阻止跳转。服务端探测通过仍不证明用户设备可达；跳转后访问失败可返回 IP 交接页检查或使用临时登录入口。临时入口失联时使用页面中的正式地址，并检查容器、DNS、证书、反代和防火墙。初次读取两个状态入口均失败时显示连接失败与重试，不把它当成服务正在重启。

## 凭据预配置

可选：直接将占位值替换并填写在 Compose `environment`，或沿用默认插值从环境变量/`.env` 注入，不要求 secrets 或额外文件挂载：

```yaml
services:
  caddy:
    environment:
      ADMIN_PASSWORD: "replace-with-your-password"
      CLOUDFLARE_API_TOKEN: "replace-with-your-token"
```

默认 Compose 使用 `ADMIN_PASSWORD: ${ADMIN_PASSWORD:-}` 与 `CLOUDFLARE_API_TOKEN: ${CLOUDFLARE_API_TOKEN:-}`；未设置或为空时向导手动输入。Compose YAML 中的字面 `$` 写为 `$$`。不要提交真实凭据，注意本地部署文件和环境变量的访问权限。

普通手动流程中，已配置项在 Setup 只显示可用状态并要求勾选使用；非法非空值明确阻断，修改后重建容器、重新读取状态和确认。重建前页面的 `setup_id` 确认无效。外部与测试模式不使用本地 Token。首次确认使用后应用自动将 Token 原子保存到 `${DATA_DIR}/secrets/cloudflare_token`，密码仅保存 Argon2id 哈希；环境密码只用于未初始化实例，不会重置已有密码。初始化后以持久化 Token 为准，重启不会用旧环境 Token 覆盖设置页更新的值。

完整预配置可让内置正式模式自动初始化：

```yaml
environment:
  ADMIN_PASSWORD: ${ADMIN_PASSWORD:-}
  CLOUDFLARE_API_TOKEN: ${CLOUDFLARE_API_TOKEN:-}
  SETUP_DOMAIN: example.com
  SETUP_CADDY_IP: 203.0.113.10 # 文档示例，必须替换为实际入口 IP；允许私网。
  SETUP_ADMIN_USERNAME: admin
  SETUP_RESOLVERS: 1.1.1.1
```

默认 Compose 保留空值，不要求所有部署者配置自动路径。四个必需项齐全才预检；先检查服务端 DoH，再核对 Cloudflare 记录与服务器解析器；只读复用 DNS、DoH 通过且预检无阻断或需确认的警告时保存并重启。服务器未解析到控制台地址仅作提示；其他不满足条件的情况通过 IP 状态观测页说明暂停原因，由用户点击「进入向导处理」接回手动确认。`SETUP_RESOLVERS` 不改变系统上游解析器。正式规则见[环境预配置](setup.md#环境预配置自动初始化)。

自动初始化失败时保留四个持久化目录。若已有初始化意图或未知归属快照，先核对数据库、受限 `setup_dns_intent.json` 中的快照指纹与 `active.json`，确认管理员是否已创建；状态不可读时修复存储后重启核对，不能删除数据或盲目重复提交。数据库提交响应丢失时先检查实际初始化状态，已保存则进入交接；不能因证书失败重新初始化。DNS 已生效但本地失败时不会自动回滚 DNS，确认原记录后由手动向导复用。重启后的首次参数变化不改写已初始化状态。

## 配置迁移

在设置页导出当前服务草稿与域名、网络策略。下载文件名带 UTC 时间戳，例如 `caddy-admin-config-2026-10-02T12-34-56-789Z.json`。现有实例上传后先预览，再明确确认替换全部草稿；文件中的域名和网络策略不会覆盖实例设置，服务按当前策略重新校验。空服务列表会清空草稿；并发修改返回 409，须重新预览。导入不改变线上版本、启动快照或历史记录，之后仍需校验与发布。

新实例可在管理员步骤采用 v2 业务配置，集中核对多域名、访问属性、策略、DNS 与草稿，仍使用相同三个步骤。域名、当前 Caddy IP 和必要服务器 DNS 可修正；无预配置凭据时手动输入，有预配置时确认使用。非标准控制台标签或 Origin 端口拒绝，不静默转换。服务只保存草稿；文件中的控制台来源限制仅保留待发布，初始化完成时仍关闭。旧 v1 配置文件拒绝并保留原文件。

JSON 文件最大 60 KiB，仅包含本项目的业务配置，不接受 Caddyfile 或原始 Caddy JSON。它包含内网地址等信息，请妥善保存；没有密码、会话、Token、证书或发布历史，不能代替下面的完整备份。

## 持久化

宿主数据目录默认位于 `./.run`。需要自定义时，直接修改所用 Compose 文件中 `services.caddy.volumes` 的宿主路径，例如：

```yaml
volumes:
  - /srv/caddy-admin/manager:/var/lib/manager
  - /srv/caddy-admin/snapshots:/srv/snapshots
  - /srv/caddy-admin/caddy-data:/data
  - /srv/caddy-admin/caddy-config:/config
```

仅修改冒号前的宿主路径；相对路径以 Compose 项目目录为基准。外部模式沿用此设置，源码验证覆盖使用独立命名卷。

修改路径不会自动搬迁数据。已有实例须先停服，将四个持久化子目录完整迁移到新根目录并保留权限，再修改配置启动；指向空目录会创建新实例。

| 目录 | 内容 |
| --- | --- |
| `.run/manager` | SQLite、WAL、实例锁、受限 secret |
| `.run/snapshots` | 内置 Caddy 启动快照 `active.json` |
| `.run/caddy-data` | 证书、私钥、ACME 状态 |
| `.run/caddy-config` | Caddy 配置数据 |
| 容器 `/run/caddy` | 运行时 Socket；可选挂载 `.run/caddy-socket`，不纳入备份 |

Compose 仅启用必要项；可选配置以 `# 配置项 # 说明` 形式列出，先取消配置行开头的注释，再设置对应环境变量。运行时 Socket 默认留在容器内，不需要宿主挂载。

Token 文件统一默认为 `${DATA_DIR}/secrets/cloudflare_token`（默认 `/var/lib/manager/secrets/cloudflare_token`），保存、校验和启动均使用此路径。仅自定义文件位置时配置 `CLOUDFLARE_API_TOKEN_FILE`，并保证目录持久化且 `10001:10001` 可读写；完整示例中取消该配置行的注释。Token 本身通过初始化向导、部署环境预配置确认或设置页提交。

服务进程以 `10001:10001` 运行。Compose 自动创建上述宿主目录，仅挂载当前数据目录，不读取旧 `run/`。初始化和业务设置保存在 SQLite，不再从旧域名、网络环境变量导入；没有目录迁移或手工补建启动快照命令。

## 备份、恢复与升级

当前没有项目级备份、恢复或离线救援命令。先停止容器，对上述四个持久目录取得一致、加密的宿主机快照，并在隔离副本验证完整恢复。仅复制 SQLite 不够；外部 Caddy 数据需单独保护。

升级前完成备份，预构建镜像部署执行：

```bash
docker compose pull
docker compose up -d
```

源码验证更新源码后使用 `make up`；外部模式追加对应文件。维护期间始终沿用原项目名、文件组合和数据目录。需要固定镜像时将 `image` 改为发布的 `sha-完整提交SHA` 标签或镜像 digest；`latest` 会随默认分支的新发布更新。

只接受空库或结构完整的当前 v7 数据库，不支持旧版本升级或自动兼容导入。v6 数据库与 v1 业务配置文件明确拒绝，不提供原地无损升级；先保留旧实例与加密快照，在独立新实例重新配置，勿删除数据绕过拒绝。管理员与设置必须同时存在或同时为空；部分实例、不支持的版本及损坏结构会明确报错，保留数据。不要同时运行两个 Manager，也不要删除数据库、锁或快照来绕过失败。更新不主动改写历史发布 JSON 或启动快照。

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
| `SETUP_BIND` | HTTPS Setup 宿主绑定，默认固定为 `0.0.0.0:8082`，应用默认不限制来源 |
| `CADDY_PROBE_ADDRESS` | 可选；默认 Admin API 主机名加 443，覆盖时先取消外部 Compose 对应行的注释 |

默认 Setup 地址为 `https://服务器IP:8082/setup`。端口被占用时启动报错，不会自动分配其他端口；可通过 `SETUP_BIND` 显式调整绑定地址或端口。查询映射：`docker compose -f compose.yaml -f compose.external.yaml port caddy 8082`。宿主 80/443 由外部 Caddy 所有。HTTPS Admin API 验证系统信任链且不跟随重定向；不可达时不回退到内置实例。

首次 Setup 只写本地状态和基线快照。外部 Caddy 需预先将控制台域名反代到 `MANAGER_DIAL`，自行配置 Cloudflare 凭据。正式入口就绪后登录、校验并确认首次发布。发布保留远端实际 `admin`/`storage`，其余业务由本项目完整管理；本地校验不能证明远端模块、凭据或存储可用。

内外模式切换前停服并保护两端数据，核对启动快照与目标实例兼容性；项目不会自动迁移快照，也不会自动发布到另一实例。

## 自定义端口与验证

默认 `compose.yaml` 的宿主绑定直接修改 `80:80`、`443:443` 和可选的 UDP 映射，例如 `192.168.1.100:8080:80`、`192.168.1.100:8443:443`；将示例 IP 替换为服务器实际内网地址。完整示例 `compose.full.yaml` 支持 `HTTP_BIND`/`HTTPS_BIND`。重建容器后生效，非标准 HTTPS 端口需显式访问，例如 `https://服务器IP:8443/setup`；初始化完成后的 HTTP 默认跳转仍指向 443；非标准宿主 HTTPS 端口请直接从完整 HTTPS 地址初始化。本地 `.env` 若存在仍会被 Compose 自动读取，避免其中残留旧覆盖或凭据。

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

维护者首次发布后，将 GitHub Packages 中的 `caddy-admin` 包设为 Public，使用户可匿名拉取。若同名包已经存在，确认它关联到 `gofxq/caddy_admin` 并允许该仓库工作流写入。首次发布与公开设置完成前，安装命令无法拉取镜像，可使用源码验证覆盖文件在独立卷中验证。

## 可选观测的持久化与维护

观测默认关闭，经设置草稿与显式发布启用。内置镜像编译固定标签指标、写前脱敏访问日志与签发诊断模块；不需要 Prometheus、Grafana 或 Loki。观测数据库及查询失败不参与 readiness，不阻断代理、登录或发布。

| 数据 | 默认位置 / 限额 |
| --- | --- |
| 独立派生数据库 | `.run/manager/observability.db`，文件 0600，主数据库最多 256 MiB；SQLite WAL 自动 checkpoint，journal 保留目标 8 MiB（活跃事务期间可能暂时增长） |
| 指标历史 | 15 秒保留 1 小时；1 分钟 7 天；5 分钟 30 天；1 小时 180 天；1 天 365 天，过期数据分批删除 |
| 访问/签发事件索引 | 最多 7 天、100000 条；游标、丢弃记录与告警持久化在同一观测库 |
| Caddy 脱敏源文件 | 默认 `.run/caddy-data/caddy/admin-observability/access.jsonl`（容器 `/data/caddy/admin-observability/access.jsonl`），0600，16 MiB 加一份轮转 |
| 内存与读取 | 日志队列 1024、源端历史 ring 2000；每 15 秒最多索引 4 页，每页 500 条；采集响应 8 MiB，最多 30000 个样本/series |
| 业务规模与查询 | 开启任一观测开关时最多 500 个服务、64 个登记域名；查询最多 1000 点、200000 原始样本、2 并发，超限提示缩小范围 |

源端写盘是异步的；队列满、磁盘错误、源端重载、索引落后超过 ring 或 Manager 长时间停止可能造成缺口。UI 会显示不可用或缺口，不把丢弃数据补为零。重启通过持久化游标去重，历史服务归属从最近 100 次成功发布恢复（最多 2000 个 ID/hostname）；更早且尚未索引的源事件可能无法归属，并标记缺口。保留期限是时间上限，不保证在容量上限内保存满期数据；服务数量、请求分布和日志量会影响可用历史。数据库容量上限不是磁盘可用空间保证，仍须监控宿主机空间并按停服一致性快照流程保护数据。

观测库是派生历史，不是业务状态来源；它损坏或版本不支持时 Manager 继续提供核心功能，观测显示不可用。处理时先检查空间与权限并重启；确需重建时由管理员停服保护原文件后处理观测库及其 WAL/SHM。不要删除 `manager.db`、启动快照或 Caddy 数据来绕过失败。项目没有自动观测修复、历史导出或新增备份恢复 CLI。

外部 Caddy 需要以 `deploy/caddy` 相同模块构建运行；项目不会自动更换远端二进制。设置开关仅在确认发布时进入生成配置，普通采集只读远端 Admin API。远端无模块、日志不可用或 metrics 不可达显示 unknown/unavailable；HTTPS 信任链和禁止重定向规则不变。外部日志文件保存在外部 Caddy 的 AppDataDir，Manager 通过既有 Admin 连接读取脱敏 ring，不挂载远端磁盘或 Docker Socket。外部证书和日志源另行保护。

周期 TCP 检查来自 Manager，每分钟最多 4 个服务，服务多时每个服务的检查间隔相应增加；只连接已发布、安全校验的地址快照，不重新解析并连接新地址。TCP 成功不代表 HTTP、HTTPS 证书或应用健康。指标告警需要完整 5 分钟和至少 20 个完成请求，日志和签发线索仅供定位，不自动发布、回滚或重试证书签发。
