# 业务 API

统一前缀 `/api/v1`，浏览器通过控制台域名访问。返回 JSON；业务请求体最大 64 KiB。首次初始化开放 Setup 状态、预检、DNS 预览、确认与完成接口；临时交接监听器另开放只读 handoff。完成后除登录外，正式管理 API 均需要有效服务端会话。

## 认证与错误

登录成功设置 `__Host-session` Cookie：`Secure`、`HttpOnly`、`SameSite=Strict`、`Path=/`，有效期 12 小时。会话凭证在数据库只保存哈希。返回的 `csrf` 保存在前端内存；所有认证后的写请求必须同时提供精确 `Origin` 和 `X-CSRF-Token`。登录也校验 `Origin`。

```json
{"error":{"code":"conflict","message":"草稿已被修改，请刷新","request_id":"服务端生成的关联编号"}}
```

| 状态码 | 含义 |
| --- | --- |
| 401 | 未登录、会话过期或密码错误 |
| 403 | Origin/CSRF 不符，或必须先修改初始密码 |
| 404 | 服务、发布记录或接口不存在 |
| 409 | 修订冲突、校验失效、发布未决或未确认漂移 |
| 422 | 字段或配置校验失败 |
| 429 | 登录尝试过多或密码运算并发已满 |
| 503 | Caddy/存储等依赖暂不可用 |

`401 credentials` 表示密码错误，保留当前会话和表单；只有 `401 unauthorized` 表示会话失效，前端清除认证状态及受保护查询/变更缓存。已有会话的后台 503/网络失败保留控制台输入，显示重试提示；首次认证失败显示独立错误页面。非 JSON/空错误响应保留 HTTP 状态，不显示代理 HTML。响应头 `X-Request-ID` 与错误中的 `request_id` 一致，编号由服务端生成。

初始化、修改和 CLI 重置管理员密码共用长度规则：至少 8 个 Unicode 字符，最多 256 字节（UTF-8）；长度无效返回 `422 invalid`。修改或重置密码成功后撤销全部旧会话。

登录限制为每客户端 5 分钟 10 次密码失败、全局 5 分钟 100 次尝试；成功登录只计全局额度。密码运算上限为两次并发，同一客户端的重叠登录返回 429，已完成的密码失败即使请求取消也记账。Caddy 覆盖 `X-Caddy-Client-IP` 为 TCP 客户端地址，Manager 仅在 TCP 对端属于刚确认的 Caddy 地址集合时使用该头；DNS 失败时回退 RemoteAddr，不使用任意 X-Forwarded-For。

## 接口清单

初始化模式仅注册以下接口，业务 API 与登录 API 不存在：

| 方法 | 路径 | 认证 / 行为 |
| --- | --- | --- |
| GET | `/setup/status` | 无认证；返回初始化状态、外部 Caddy 模式、`test_tls`、最多三个非敏感 Resolver 建议和仅来自 TCP 对端的 `client_ip` |
| POST | `/setup/preflight` | 精确同源 `Origin`；返回 `normalized`、固定检查项、`can_complete`、`requires_acknowledgement` 与 `warning_fingerprint` |
| POST | `/setup/dns/preview` | 精确私网 HTTP/HTTPS Origin；`{token,homelab_domain,address}` → DNS 变更预览，不写入 DNS |
| POST | `/setup/dns/confirm` | 精确私网 HTTP/HTTPS Origin；`{settings,cloudflare:{token,address,fingerprint,confirmed:true}}` → 保存恢复意图、立即写入并核对 Cloudflare DNS；成功返回 DNS plan，不等待实际解析传播，失败保留输入重试 |
| POST | `/setup/dns/check` | 精确私网 HTTP/HTTPS Origin；`{settings,address}` → 只读查询，返回 `{verified,queries:[{name,resolver,addresses,status,message}]}`；DNS 未生效或查询失败也返回 200 完整结果 |
| POST | `/setup/complete` | 精确同源 `Origin`；提交管理员账号、设置、`acknowledge_warnings` 和已确认的 `warning_fingerprint`；服务端重新执行相同预检，内置非测试模式另须 `cloudflare` 与明确 DNS 确认，第一个有效提交返回 `202` 后重启 |

有导入服务时预检另含 `imported_services`；`/setup/preflight` 可提交 `{settings,services}`，`/setup/complete` 另提交 `services` 和 `confirm_import:true`。服务重新接受域名、上游和唯一性校验，与管理员及设置事务化保存，仅作为草稿，不自动发布。

常规预检检查 ID 为 `settings_valid`、`network_scope`、`resolver_reachable`、`admin_dns`；外部模式另有 `external_console`。状态固定为 `pass | warning | block`。阻断项不能提交；有警告时必须同时提交 `acknowledge_warnings:true` 及当前预检返回的 `warning_fingerprint`。指纹绑定规范化设置与警告 ID/文案；缺失、不符或最终复检警告变化时，完成接口返回 `409 setup_warning_confirmation_required`，应重新读取预检、向用户展示并确认。无警告时指纹为空。确认警告不放宽字段校验、网络安全策略或快照保护。

`normalized` 包含规范化设置及派生的 `admin_origin`（与 `origin` 相同）。额外的 `network_valid` 和可选 `network_error` 复用服务端网络规则，供网络步骤离开前判断；尚未填写 DNS 时 `can_complete` 仍为 `false`，不会因此允许完成初始化。

临时 Setup/交接监听器另提供 `GET /api/v1/setup/handoff`，返回 `initialized`、`mode`、`admin_origin`、`manager_status`、`dns_status`、`console_status`、`temporary_entry`、`checked_at`；三个状态字段只使用 `ready | pending | error`。该路由不注册到正式管理 API，不返回 Token、Admin API 地址、探测地址、解析 IP、证书或底层错误，并设置 `Cache-Control: no-store`。

`status` 与 `handoff` 的初始化状态读取等待当前初始化操作结束。交接返回 `initialized:false` 且 `manager_status:ready` 才表示确认未完成；`initialized:false` 且 `manager_status:error` 表示暂时无法读取，页面继续核对，不恢复提交按钮，也不自动重放完成请求。

Setup 与交接入口的监听、Host/来源限制和关闭时机见[运维说明](operations.md#初始化与访问边界)。

完成请求的设置字段仅为 `homelab_domain`、`lan_cidrs`、`upstream_cidrs`、`allowed_names`、`denied_ips`、`resolvers`。服务端规范化 Homelab 域名，派生 `admin_domain=caddyadmin.<homelab_domain>`、对应 HTTPS Origin，并把 Public 域名留空；未知的 `origin`、`public_domain` 或 `admin_domain` 字段会被拒绝。内置非测试初始化必须提交 `cloudflare:{token,address,fingerprint,confirmed:true}`；`fingerprint` 来自 DNS 预览。预览返回 `zone_id,name,type,address,action,record_id?,old_address?,old_proxied?,warning?,fingerprint,context_fingerprint`，`action` 为 `create|update|reuse`。调用 `/setup/dns/confirm` 时重新核对记录，立即写入并读取核对结果；已变化返回 409，需重新预览，成功后不会等待传播。独立调用 `/setup/dns/check` 时，服务端直接向每个所选解析器查询控制台域名和随机一层子域的 A 与 AAAA，不使用系统 hosts 或搜索后缀；检查接口为每个名称、解析器返回查询到的 IP 数组、`pass|block` 状态及中文说明；部分失败仍返回全部结果，不输出原始查询错误。`verified` 表示全部查询通过当前检查条件。内置非测试模式必须提供目标 IP，结果必须仅包含目标 IP；外部和 `TEST_TLS=true` 模式可省略 `address`，此时仅检查有解析结果，消息明确说明未核对目标 IP。参数无效返回 422，已初始化返回 409，Origin 不匹配返回 403。向导的「检查 DNS」由浏览器直接调用固定 Cloudflare/Google JSON DoH，检查控制台和随机子域的 A/AAAA，并核对全部返回地址；不调用 `/setup/dns/check`。该后端接口保留为专家诊断入口，使用服务器解析器，与浏览器 DoH 结果可能不同。浏览器 DoH 选择不写入 `settings.resolvers`，后者仍供 Caddy 和服务器诊断使用。完成接口只接受已写入且与确认指纹一致的 Cloudflare 实际记录，并重新校验业务设置、预检警告和快照归属；不再执行普通 DNS 强制传播校验，也不接受或信任浏览器的 `verified` 标记，不会在最终提交时创建或更新 DNS。外部和 `TEST_TLS=true` 模式不接受本机 `cloudflare`。DNS 已成功而本地初始化失败返回 `503 setup_local_persistence`，保留恢复意图，修复后重新预览并提交；不自动撤销外部 DNS。

之后可在认证接口 `POST /settings/cloudflare` 中按 `{ "token": "…", "enable": true }` 启用 Cloudflare；该接口要求登录会话、精确 Origin 与 CSRF Token。密钥只写入受限 secret 文件，响应、日志和状态接口不回显。外部模式不读取或保存本地 Token。

正常模式提供以下已认证管理接口：

| 方法 | 路径 | 输入 / 输出 |
| --- | --- | --- |
| POST | `/auth/login` | `{username,password}` → `{username,csrf,must_change,expires}` |
| GET | `/auth/session` | 当前会话公开信息 |
| POST | `/auth/logout` | `{}`；撤销当前会话 |
| POST | `/auth/password` | `{current,password}`；修改后撤销全部旧会话 |
| GET | `/services` | `{revision,services,published}`，同时返回草稿与已发布业务模型 |
| POST | `/services` | `{revision,service}`；服务 ID 由后端生成 |
| PUT | `/services/{id}` | `{revision,service}`；完整更新服务草稿 |
| DELETE | `/services/{id}` | `{revision}`；仅删除草稿中的服务 |
| GET | `/draft/preview` | 可选 `?rollback=<成功发布ID>`；返回预览 |
| GET | `/draft/revisions/{revision}` | `{revision,services}`；需认证且完成首次改密；缺失的旧版本返回 404 |
| POST | `/draft/validate` | `{revision,rollback_id:""}`；成功返回 `validation_id` 和 RFC3339 `validation_expires_at` |
| POST | `/deployments` | 下述发布请求 → `202` 和发布记录 |
| GET | `/deployments` | 发布历史分页 |
| GET | `/deployments/{id}` | `{deployment,config}`，配置只读 |
| GET | `/overview` | 可达性、漂移、已发布版本、启用数量、近期发布 |
| GET | `/certificates` | `{items}`，证书探测结果；后端缓存 60 秒 |
| GET | `/audit` | 审计记录分页 |
| GET | `/configuration/export` | 导出专用 JSON；`Cache-Control: no-store` |
| POST | `/configuration/preview` | `{configuration}`；按当前策略校验，返回 `{revision,services,changes}` |
| POST | `/configuration/import` | `{configuration,revision,confirm:true}`；原子替换全部草稿并记录审计，修订冲突返回 409 |
| GET | `/settings` | 只读部署信息、版本、模块、密钥配置状态及 `external_caddy`；不暴露外部 Admin URL 或 Manager 地址 |
| POST | `/settings/cloudflare` | `{token,enable:true}`；认证、精确 Origin 与 CSRF 校验后启用内置 DNS-01 |

历史与审计支持 `offset`、`limit`，默认每页 20，最多 100，响应为 `{items,offset,limit}`。服务列表在前端按关键词、分组和启用状态组合筛选。

## 服务模型

```json
{
  "name": "Photos",
  "group": "homelab",
  "hostname": "photo.home.example.com",
  "scheme": "http",
  "host": "10.0.0.8",
  "port": 2283,
  "enabled": true,
  "notes": "仅管理员可见"
}
```

输出还包含 `id`、`dial`、`updated_at`；这些字段由服务器维护。Hostname 转小写、去除首尾空白与末尾点，规范化后全局唯一。名称限制 100 字节、备注 2000 字节。协议只允许 HTTP/HTTPS，端口 1–65535；禁用服务也必须保留合法配置。

## 配置文件

导出结构为 `{format:"caddy-web-admin",version:1,settings,services}`，最大 60 KiB。`settings` 包含 `origin`、`public_domain`、`homelab_domain`、`admin_domain`、`lan_cidrs`、`upstream_cidrs`、`allowed_names`、`denied_ips`、`resolvers`；`services` 仅包含上述服务输入字段，不能包含 `id`、`dial` 或 `updated_at`。未知字段和不支持的版本被拒绝；没有密码、Token、会话、证书或运行快照。

现有实例的导入只取服务列表，按当前实例策略重新校验；不会写入线上配置、快照或上传的设置。空列表清空草稿。预览不保存；确认导入绑定预览修订，保留相同 Hostname 的服务 ID，增加一次草稿修订、不可变历史和 `configuration.import` 审计。响应丢失后不得盲目重放，先核对当前草稿并重新预览。

初始化前端读取同一文件并填入 Setup 字段；当前只接受标准 Homelab 配置，Public 或自定义控制台地址明确拒绝。Setup API 本身接收正常设置和服务输入，不接收原始文件或敏感凭据导出。

## 发布协议

预览包含 `revision`、`services`、`changes`、`config`、`hash`、`runtime_hash`、`expected_hash`、`drift`、`rollback_id`。业务变更类型包括 `added`、`deleted`、`updated`、`enabled`、`disabled`，并附带前后服务模型。

校验通过后，提交：

```json
{
  "validation_id": "校验响应中的 ID",
  "revision": 3,
  "expected_hash": "校验响应中的 runtime_hash",
  "idempotency_key": "客户端为该次确认生成的唯一随机标识",
  "confirm_drift": false
}
```

校验有效期 15 分钟。草稿、部署策略、上游解析或运行指纹变化后必须重新校验。`confirm_drift` 仅确认该次校验绑定的运行指纹，不能用于覆盖之后出现的新变化。幂等标识重复且请求一致时返回原发布记录；同标识不同请求返回 409。

发布状态为 `applying`、`success`、`failed`、`uncertain`。不能将 202 当成成功，须查询发布记录。服务保存后不触发 Caddy 写入；每次发布只通过一次 `POST /load` 加载完整 JSON。

回滚先对成功发布 ID 进行预览及校验，再使用相同发布接口提交，`rollback_id` 从校验记录继承。在线回滚以当前策略、DNS 和安全边界重新生成历史服务模型；预览附带 `rollback_config` 和 `rollback_hash`，供比较原快照和候选。成功回滚产生新版本，不改写现有草稿。当前没有离线救援 API 或 CLI。

服务新增/修改、预览及发布时重新确认 Manager、本机接口和 Caddy 地址；系统解析失败返回 `503 system_resolution`，不会静默缩小保护范围。历史读取和登录不依赖系统目标 DNS 成功。

审计附带 `revision`、`rollback_id` 和安全 `error_class`；详情不包含密码哈希、会话、Token 或私钥。历史修订缺失时返回 404，不根据当前草稿重建。

本地 `GET http://127.0.0.1:8081/ready` 不属于公开管理 API：仅绑定容器回环地址，主 HTTP、SQLite 与 Caddy 均可用才返回 200，否则 503。Compose 的 `manager health` 调用此接口。
