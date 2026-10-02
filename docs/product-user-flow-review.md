# 产品完整使用流程

更新于 2026-10-02

本文描述已实现的流程；真实浏览器、DNS、证书签发和目标网络仍按[验证边界](verification.md)验收。

## 汇总图

```mermaid
flowchart TD
    START[准备域名与可信 LAN/VPN] --> DEPLOY[拉取预构建镜像或使用源码构建覆盖]
    DEPLOY --> MODE{Caddy 模式}
    MODE -->|内置| BUILTIN[默认 80/443，HTTPS 初始化]
    MODE -->|外部| EXTERNAL[配置 Admin API、Manager 地址与远端反代]
    BUILTIN --> SETUP[创建管理员，配置域名、网络与 DNS]
    EXTERNAL --> SETUP
    SETUP --> HANDOFF[保存初始化结果，查询入口交接状态]
    HANDOFF --> LOGIN[从可用的控制台入口登录]

    LOGIN --> DRAFT[新建、修改、启停或删除服务草稿]
    LOGIN --> CERT{证书准备}
    CERT -->|内置| TOKEN[设置页提交 Token，等待可信证书]
    CERT -->|外部| REMOTE[在远端管理凭据与证书，核对连通性]
    TOKEN --> READY[核对模式相关的发布前提]
    REMOTE --> READY
    DRAFT --> DIFF[预览业务 Diff 与只读 JSON]
    DIFF --> READY
    READY --> VALIDATE[真实 Caddy 校验]
    VALIDATE --> CONFIRM[确认影响，必要时确认覆盖漂移]
    CONFIRM --> APPLY[后台加载，核对运行并持久化快照]
    APPLY --> RESULT{发布结果}
    RESULT -->|success| ONLINE[形成成功线上版本]
    RESULT -->|failed| FIX[查看原因，修正后重新预览]
    FIX --> DIFF
    RESULT -->|uncertain| RECONCILE[阻止新发布，保留数据并核对实际状态]
    RECONCILE -->|连接恢复后继续核对| RESULT

    ONLINE --> DAILY[概览、证书、审计与设置]
    DAILY --> DRAFT
    ONLINE --> ROLLBACK[选择成功历史版本，按当前策略生成候选]
    ROLLBACK --> DIFF
    ONLINE --> MAINTAIN[停服取得四目录加密快照，再升级与验收]

    DRAFT --> EXPORT[导出不含凭据的业务配置 JSON]
    EXPORT --> IMPORT{导入到哪里}
    IMPORT -->|现有实例| IMPORTDRAFT[预览并确认替换草稿，沿用实例策略]
    IMPORTDRAFT --> DRAFT
    IMPORT -->|新实例| IMPORTSETUP[初始化时上传域名、网络策略与服务草稿]
    IMPORTSETUP --> SETUP
```

草稿可以在等待证书期间保存，只有发布会改变线上。现有实例导入不改变域名、网络策略或线上版本；回滚也需要重新校验与确认。汇总图中的证书准备按模式处理：内置模式有服务端可信证书门禁，外部模式的证书与凭据由远端负责。

## 部署与首次初始化

| 场景         | 当前入口与职责                                                                                                                      |
| ------------ | ----------------------------------------------------------------------------------------------------------------------------------- |
| 默认内置模式 | `compose.yaml` 拉取 `ghcr.io/gofxq/caddy-admin:latest`；打开 `https://服务器IP/setup`，80 仅跳转 HTTPS                              |
| 本地源码构建 | 追加 `compose.build.yaml`，使用独立的 `caddy-admin:local` 标签                                                                      |
| 外部 Caddy   | 追加 `compose.external.yaml`，配置 `CADDY_ADMIN_URL`、`MANAGER_DIAL`；查询 8082 的实际 HTTPS Setup 映射，宿主 80/443 留给远端 Caddy |
| 隔离验证     | 构建当前源码，使用 `compose.validation.yaml` 的随机回环端口和独立测试卷，不读取真实 `.run/`                                         |

默认内置模式没有随机 Setup 端口。第一次安装不需要提前填写密码、Token 或 `.env`；管理员密码通过 HTTPS 向导设置。第一个有效提交创建唯一管理员，初始化前须将入口限制在可信 LAN/VPN。

```mermaid
flowchart TD
    OPEN[打开 HTTPS Setup] --> ADMIN[创建管理员，可选上传导出的业务配置]
    ADMIN --> DOMAIN[核对 Homelab 域名与派生控制台地址]
    DOMAIN --> NETWORK[填写可信来源、上游范围与可选高级策略]
    NETWORK --> DNS[按指引设置通配符 DNS，选择仅 DNS 灰云]
    DNS --> PRECHECK[重新检查字段、网络与服务端 DNS]
    PRECHECK --> STATE{预检结果}
    STATE -->|block| CORRECT[修正阻断项，保留输入]
    CORRECT --> PRECHECK
    STATE -->|warning| ACK[阅读影响并确认警告]
    STATE -->|pass| SAVE[确认并原子保存初始化]
    ACK --> SAVE
    SAVE --> HANDOFF[查询 Manager、DNS、TLS 与路由状态]
    HANDOFF --> ENTRY{正式入口就绪}
    ENTRY -->|否| WAIT[显示原因并继续查询，不重复提交初始化]
    WAIT --> HANDOFF
    WAIT --> TEMP{内置模式}
    TEMP -->|是| LOGIN[允许从受保护临时入口登录]
    ENTRY -->|是| FORMAL[打开正式控制台，可取消自动跳转]
    FORMAL --> LOGIN
    LOGIN --> TASKS[按概览首次运行任务中心继续处理]
```

DNS 步骤只生成记录指引，不自动修改 Cloudflare。内置模式从当前访问的私网 IP 提供建议；localhost 或外部模式需手工填写实际 Caddy 地址。重新检查只证明服务端能解析控制台域名，不证明记录指向建议 IP、用户设备可达或 TLS 就绪。

初始化默认只配置 Homelab，控制台地址为 `https://caddyadmin.<Homelab域名>`。Public 域名初始为空，当前没有界面配置入口。服务列表默认为空，显式初始化导入除外。

Setup 提交后持续查询交接状态，正式入口就绪才开始可取消的跳转倒计时。内置模式可通过受 LAN、认证、Origin 与 CSRF 保护的临时入口继续登录；外部交接入口只提供静态页面和只读状态。浏览器读到正式入口就绪后，临时入口保留五分钟再关闭。

外部模式须预先准备控制台反代、Admin API 访问边界和远端 DNS 凭据；Setup 不会自动发布到远端实例，外部不可达也不会回退到内置 Caddy。

## 证书准备与日常发布

概览的首次运行任务中心已经按模式展示正式入口、证书或外部连通性、草稿和首次成功发布的状态。内置模式在设置页提交 Token，文件默认位于 `${DATA_DIR}/secrets/cloudflare_token`；只暴露配置状态，不回显凭据。

内置模式须完成 Cloudflare 激活、运行配置核对和真实可信 TLS 探测，才能校验或发布服务；`TEST_TLS=true` 不能绕过该门禁。证书探测失败显示未知，内部测试 CA 不显示为公网可信。外部模式由远端管理证书，Manager 本地校验不证明远端模块、Token、存储或证书可用。

```mermaid
flowchart TD
    EDIT[编辑、新增、启停或删除服务] --> SAVE[服务端校验并保存新草稿修订]
    SAVE --> PREVIEW[比较草稿与线上业务，查看 Diff 和只读 JSON]
    PREVIEW --> GATE{发布前提满足}
    GATE -->|否| FIX[处理证书、连通性或未决发布状态]
    FIX --> PREVIEW
    GATE -->|是| VALIDATE[解析并检查上游，调用真实 Caddy 校验]
    VALIDATE --> CHECK{校验通过}
    CHECK -->|否| EDIT
    CHECK -->|是| BIND[绑定修订、部署策略与运行指纹，15 分钟有效]
    BIND --> FRESH{校验与当前状态仍一致}
    FRESH -->|否| PREVIEW
    FRESH -->|是| CONFIRM[确认影响摘要与受影响域名]
    CONFIRM --> DRIFT{存在运行漂移}
    DRIFT -->|是| ACK[额外确认完整覆盖漂移]
    DRIFT -->|否| BEGIN[持久化候选与发布意图，返回 applying]
    ACK --> BEGIN
    BEGIN --> LOAD[后台加载完整配置并核对实际运行]
    LOAD --> RESULT{核对结果}
    RESULT -->|候选已运行且快照已保存| SUCCESS[success，形成成功线上版本]
    RESULT -->|确定未加载或仍是原版本| FAILED[failed，重新预览与校验]
    RESULT -->|无法确认或快照保存失败| UNCERTAIN[uncertain，阻止新发布并继续核对]
```

新增、修改、启停和删除只改草稿；删除确认会说明原路由在再次发布前仍在线上。当前服务表单仍输入完整的一层子域，上游受网段、显式服务名和受保护目标规则限制；HTTPS 上游必须校验证书。

发布确认框已展示新增、修改、启用、停用、删除的数量，以及受影响域名和漂移覆盖确认。当前任务卡追踪任务 ID，显示后台执行状态；关闭页面不会取消发布，`202` 只表示任务已接收。

| 状态        | 含义与下一步                                                                         |
| ----------- | ------------------------------------------------------------------------------------ |
| `applying`  | 正在后台执行，等待核对；不能并行提交另一发布                                         |
| `success`   | 候选已运行且启动快照已持久化，可继续管理或在线回滚                                   |
| `failed`    | 已明确未加载或未完成替换；刷新预览、修正原因并重新校验                               |
| `uncertain` | 实际状态无法确定，或已加载但快照保存未完成；保留数据、恢复连通性并核对，不能重复发布 |

幂等标识用于同一请求的重试，不能用于绕过未决状态或更换请求内容。断联、响应丢失或 Manager 重启后，系统读取实际运行配置继续核对；仍存在未知配置或持久化问题时，应按运维文档诊断，不能手工写快照来判定成功。

## 在线回滚

```mermaid
flowchart TD
    SELECT[选择成功历史版本] --> MODEL[读取历史业务模型]
    MODEL --> REBUILD[按当前 DNS、安全策略与部署模式生成候选]
    REBUILD --> PREVIEW[查看回滚业务 Diff、历史配置与当前候选]
    PREVIEW --> VALIDATE[真实校验，并检查有效期与运行指纹]
    VALIDATE --> CONFIRM[明确确认回滚，必要时确认漂移覆盖]
    CONFIRM --> PUBLISH[走同一串行后台发布链路]
    PUBLISH --> RESULT{结果}
    RESULT -->|success| VERSION[形成新的成功发布版本]
    RESULT -->|failed 或 uncertain| CHECK[按发布状态处理，不直接重试]
    VERSION --> KEEP[保留当前未发布草稿]
```

回滚重新发布历史业务模型，不直接加载历史 JSON。历史业务不符合当前策略或 DNS 校验时不能继续；只有成功版本可作为回滚目标。回滚后的线上配置与保留的草稿可能不同，用户仍须明确决定下一次发布的内容。

## 配置导入导出

```mermaid
flowchart TD
    EXPORT[设置页导出域名、网络策略与当前服务草稿] --> FILE[带 UTC 时间戳的业务 JSON，最大 60 KiB]
    FILE --> TARGET{目标实例}
    TARGET -->|已初始化| UPLOAD[上传，按当前实例策略预览替换全部草稿]
    UPLOAD --> CONFIRM[确认覆盖，提交绑定的草稿修订]
    CONFIRM --> CONFLICT{修订仍一致}
    CONFLICT -->|否| UPLOAD
    CONFLICT -->|是| DRAFT[原子替换草稿并记录审计]
    DRAFT --> PRESERVE[域名、网络策略与线上版本保持不变]
    PRESERVE --> PUBLISH[需要上线时另行预览、校验和确认发布]
    TARGET -->|新实例| SETUP[初始化管理员步骤上传并采用配置]
    SETUP --> REVIEW[核对域名、网络、DNS 与服务，设置新密码]
    REVIEW --> INIT[确认初始化，服务仅保存为草稿]
    INIT --> TOKEN[重新配置 Token，完成对应模式的发布前提]
    TOKEN --> PUBLISH
```

空服务列表经确认后会清空现有草稿；文件无效或修订冲突不会部分覆盖。新实例导入只接受当前初始化支持的 Homelab、标准控制台地址与业务模型，不支持 Public 域名、自定义控制台标签或 Origin 端口。

导出没有密码、会话、Token、证书、发布历史、内部服务 ID 或解析地址快照；它包含内网地址等业务信息，应妥善保存，不能代替完整备份。

## 数据维护与当前边界

| 操作       | 当前做法                                                                                             |
| ---------- | ---------------------------------------------------------------------------------------------------- |
| 日常运行   | SQLite 保存草稿、修订、发布与审计；内置 Caddy 使用 `.run/snapshots/active.json` 启动，正常发布热加载 |
| 密码修改   | 设置页修改后撤销旧会话；CLI 重置须先停止 Manager，密码通过维护终端输入                               |
| 升级与保护 | 停服取得 Manager、快照、Caddy 数据与配置四目录的一致加密快照；外部 Caddy 数据另行保护                |
| 故障恢复   | 控制台可用时优先回滚成功版本；否则保留数据，在隔离副本诊断或恢复已验证的宿主快照                     |

当前只支持空库或完整的当前 v6 数据库，没有旧设置、旧目录、旧 CLI 的兼容入口，也没有项目级备份、恢复或离线救援命令。业务配置导入、在线回滚和宿主快照分别解决不同问题，不能相互替代。

已实现的交互包括分步预检、DNS 记录复制与重新检查、可恢复交接、首次运行任务中心、发布四态任务卡和影响摘要。尚未提供多用户、多实例、多上游、路径路由、主动上游健康检查、流量图表或任意配置编辑器；Public 域名运行时配置也仍未提供。

下一步主要是实际环境验收：浏览器与移动端、真实 DNS/Cloudflare 签发和续期、目标防火墙/NAT/LAN/VPN，以及加密宿主快照的隔离恢复演练。自动测试和演示界面不能替代这些验收。

## 对照入口

- [快速部署](../README.md#快速部署)、[使用指南](usage.md)、[部署与维护](operations.md)。
- [API 与发布协议](api.md#发布协议)、[从零测试](testing.md)、[验证边界](verification.md)、[当前待办](roadmap.md)。
- 实现：[Setup](../web/src/pages/Setup.tsx)、[首次运行任务](../web/src/pages/Overview.tsx)、[发布页面](../web/src/pages/Deployments.tsx)、[发布与核对](../internal/application/deploy.go)、[配置导入导出](../internal/application/configuration.go)。
