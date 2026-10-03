# 多域名与轻量 Setup 实施计划

**执行授权：** 用户已审阅并迭代设计，明确要求“按照这个实现”。在当前功能分支连续执行，不再重复请求实施授权，不提交、推送或部署。

**Spec:** `docs/superpowers/specs/2026-10-03-managed-domains-design.md`

**技术栈：** 沿用 Go/Gin/SQLite、React/TypeScript/Vite，不引入依赖。

## 接口约定

- `domain.ManagedDomain { ID, Name string; Access *string }`，JSON `id/name/access`；`null` 为待配置。
- `ManagedSettings` 删除两组域名，新增 `Domains []ManagedDomain` (`domains`)、`ConsoleLANOnly bool` (`console_lan_only`)；保留 `origin/admin_domain/lan_cidrs/upstream_cidrs/allowed_names/denied_ips/resolvers`。控制台交接另保留 `previous_admin_domain`、`previous_origin` 可选字段。
- `Service.Group` 改 `DomainID string` (`domain_id`)；所有引用按 ID 校验。`Draft.Settings`、`Deployment.Settings`、`Preview.Settings/ActiveSettings` 保存/显示策略，设置和服务共用草稿修订。新增 `Preview.SettingsChanged bool`。
- 新 schema v7、新导出格式 v2；明确拒绝旧版，保留文件，不自动兼容或删除。
- 仓储新增 `SaveSettings(ctx, revision, ManagedSettings, actor) (Draft,error)`；Setup 初始化当前/草稿设置，服务保存保留策略历史；发布成功事务同步当前设置，不改写草稿。
- `GET /settings` 返回 `config`（候选策略 + test_tls）、`active_config`、`revision` 及现有部署状态；`PUT /settings` body `{revision,settings,confirm_exposure}` 保存策略草稿，服务端校验并确认放开访问。业务发布沿用现有接口。
- Setup 输入改 `domain`，无普通网络配置字段；保留高级 `resolvers` 和显式导入 `import_settings`。状态新增 `admin_password_status/cloudflare_token_status` (`missing/ready/invalid`)、不敏感错误及 `setup_id`；来源为 `ADMIN_PASSWORD/CLOUDFLARE_API_TOKEN`。请求支持 `use_configured_password/use_configured_token/setup_id`，只读取服务端值。
- 控制台域名交接：设置保存新地址时保留旧地址与 Origin，发布两入口；新入口认证后明确确认完成交接，产生清除旧入口的草稿，发布成功撤销旧会话。不接受任意 Origin。

## 工作分工与测试

- [x] 1. 领域、部署配置和仓储：多域名/未配置访问属性、独立可选控制台 ACL、空上游 deny-all、完整 JSON、schema v7、不可变策略历史和成功发布策略提交。负责 `internal/domain/`、`internal/config/`、`internal/adapter/gormstore/`；先写关键行为失败测试再实现，更新该范围夹具。
- [x] 2. 前端：统一模型、三步 Setup、预配置确认、域名与策略表单、Diff、发布、可选控制台 ACL、首次登录引导、导入导出和 mock。负责 `web/`；增加行为测试并运行 Vitest、TypeScript/Vite。
- [x] 3. 应用与 HTTP/CLI：使用候选策略预览、绑定校验与 DNS 快照、发布/恢复同步当前策略、并发期间从严、动态 ACL/Origin、交接、轻量 Setup 和公网入口、预配置凭据持久化优先。负责 `internal/application/`、`internal/adapter/httpapi/`、`cmd/manager/`、Caddy/Cloudflare 适配器；补权限、断联、崩溃、预配置和访问回归测试。
- [x] 4. 部署与文档：Compose 明文环境项/插值、运行 secret 优先、容器夹具、README/usage/api/operations/verification/AGENTS 当前约束同步；不操作生产部署。
- [x] 5. 统一审查与验证：`make test`、`make check`、`make build`、`make integration`、Compose 配置与隔离容器（工具可用时），修复所有回归；完成独立审查，明确未验证范围。

## 审查重点

来源限制默认关闭但认证/CSRF/精确 Origin 保留；可信代理头不能伪造。草稿不会更改运行策略；`uncertain` 不重试覆盖。新增域名不因尚未签发证书形成循环门禁；未知域名 404。无配置上游时不放开目标。重启不以环境密码/旧 Token 覆盖已保存数据。

## 决策与进度

Ruling: 当前分支保留设计文档，在独立功能分支增量实施；不建立会丢失未提交设计文档的额外 checkout。
Ruling: 并行任务只编辑指定目录，应用层接口由主代理统一协调；完成各模块后进行统一复核。


### 实施与验收记录（2026-10-03）

已实现上述接口和设计边界。策略和服务共享不可变修订；成功提交与 Manager 的当前访问策略切换同步，待核对状态不提前放开访问。扩大控制台或业务可信网段、公开域名及启用公网服务须明确确认；发布时另检查当前管理来源，防止收紧规则造成自锁。控制台完成交接的草稿意图不会因普通设置编辑而被撤销，移除旧入口与会话撤销在同一数据库事务完成。

独立审查发现并修复可信网段扩大遗漏、成功提交访问规则切换窗口、前后端确认基线不一致，以及交接完成草稿被普通保存覆盖的问题。最后修正 Setup 导入中手动选择文件内另一个域名时误改原域名的问题，输入和下拉两条路径均保留原域名 ID/名称；前端最终 119 个测试通过。新增集成测试覆盖公网认证、伪造来源头、来源收紧、访问扩大确认、策略草稿与当前状态分离、快照失败后恢复、交接后会话撤销、新域名先签证书再上线业务服务。

验证：完整 Go race / Vitest、静态检查与两模块依赖校验、Manager / TypeScript / Vite 构建、真实 Caddy 集成校验，以及默认/完整/外部/源码 Compose 配置检查通过。内置与外部隔离容器验收通过，临时测试资源已清理。

容器测试工具镜像的 curl 下载遇到 Alpine 软件源 TLS 错误。未修改产品 Dockerfile，使用 `/tmp` 临时 Dockerfile 从最新源码构建测试镜像，仅复制已有测试镜像的 curl 和依赖库；随后完整运行 `CONTAINER_TEST_IMAGE=caddy-admin:managed-domains-current-smoke python3 test/container_smoke.py`。测试镜像与普通 Compose 源码构建的 Manager SHA256 相同。最终烟测还实际验证 Compose `$$` 到容器字面 `$` 的传递、预配置凭据状态不泄露、仅提交密码使用确认、初始化后登录及重启认证。此结果是完整容器烟测，原 `make container-test` 的软件源下载问题仍存在；真实 Cloudflare 签发与浏览器端到端验收未验证。

未提交、推送或操作真实部署。schema v6 与导出 v1 明确拒绝，现有数据文件保留；需按运维文档在独立实例采用当前格式。
