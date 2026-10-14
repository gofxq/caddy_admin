# 环境预配置与自动初始化实施计划

依据：[设计](../specs/2026-10-14-environment-setup-design.md)。用户已要求完整实施，在当前工作区连续执行，保留所有已有数据，不操作真实部署或凭据。

## 全局约束

仅内置 Cloudflare 正式模式自动初始化。先核对数据库、意图及快照；所有外部写入仍需 DNS 确认。自动任务由启动触发，与手动提交共用串行锁。状态查询只读，公开进度不暴露配置、凭据或内部错误。已有实例忽略首次参数。

## Task 1: 环境输入与入口 IP

- `internal/config/config.go` 读取首次参数但不在 Load 验证，避免阻止已有实例启动；`internal/application/options.go` 传递首次配置。
- 新增统一入口 IP 校验，拒绝空值、未指定、回环、链路本地及组播；允许私网 IPv4/IPv6。手动正式 Setup 同样使用，测试及外部模式保留原有行为。
- 测试配置读取、默认值、无效值延迟验证、IP 边界。运行 config/domain/application 相关测试，预期先失败后通过。

## Task 2: 串行后台初始化与恢复

- `internal/application/setup_auto.go` 管理一次性任务状态（manual/running/action_required/failed/uncertain/saved）和阶段，复用 Preflight、DNS Preview 和 CompleteSetup。
- 完整参数才开始；部分配置仅提供有效预填；非法非空配置明确报错。已初始化、外部、测试模式不自动运行。
- 自动流程只接受 reuse 且无警告的计划；锁内再次核对 DNS/预检，真实 Caddy Validate 后持久化。手动 POST 在自动流程中返回冲突，人工接续后不再自动提交。
- 快照归属不明、已有提交意图或写入失败进入待核对，不盲目重试；数据库提交错误先读取实际状态，保留归属意图及快照。
- 测试成功、缺失、非法值、DNS 变更/冲突/断联、警告变化、并发、启动重入、持久化失败与重启恢复。运行 application race 测试。

## Task 3: 启动和公开状态 API

- `cmd/manager/main.go` 在 Setup 监听器就绪后启动后台任务，成功共用现有重启通知，关闭服务等待任务结束。
- 保留现有 `/setup/status` 作为精确同源 Origin/Referer Setup 预填接口；新增只读 `/setup/progress` 返回脱敏阶段，与 handoff 分工，轮询不占用写入限速配额。
- API 测试状态脱敏、无匿名重试入口、Origin/限速及认证边界；组合根测试首次参数映射。

## Task 4: 页面与演示

- `web/src/pages/Setup.tsx` 显示后台进度，轮询只读状态；人工处理显示原因并接续三步表单，预填只应用一次，重试读取保留输入。
- 不自动勾选凭据、DNS 或警告确认，不因刷新写入；成功进入既有 handoff，断联先核对结果。
- `web/mock-api.ts` 和静态演示同步接口；Vitest 覆盖运行、暂停、失败、预填、刷新与断联，不发送预配置凭据。

## Task 5: 文档和隔离验证

- 同步 README、setup、usage、api、operations、verification、Compose、AGENTS 及设计状态。
- 运行 make test/check/build/integration、Compose 检查和隔离容器验证；增加不使用生产 Cloudflare 的真实 Caddy 自动初始化集成场景及持久化恢复。
- 最后审阅全部 diff；验证无法运行则记录实际原因，不宣称真实部署验收。

## Review Focus

- 状态刷新/断联不能触发第二次初始化；任务暂停后不与用户编辑竞争。
- 首次配置无效不能破坏已初始化实例；持久化 Token 优先。
- DNS Preview 后记录改变与预检警告改变不得沿用旧确认。
- DB 提交返回错误可能已经提交；不得删除有效快照或重建管理员。
- 公共进度不能包含 DNS 列表、路径、Token、密码或底层模块日志。

## 实施记录

- 已完成环境输入、统一 IP 校验、后台串行任务、同源预填/脱敏进度、页面和演示接口、文档及隔离容器场景。
- DNS Edit 无法用现有只读 API 证明；不增加未经确认的外部写入，明确记录权限核对边界。
- 已有意图/快照时暂停人工核对；不自动重放。外部/测试模式明确未提交时只清理本次写入的候选，结果未知保留。
- 独立审查发现的失败后重试和同源预填边界已修复并补充回归测试。
- 初次共享临时目录配额及并发构建影响检查；顺序重跑通过。容器测试补齐生产 restart 策略及隔离 Compose 自建网络。

## Task 6: 统一 DNS 核对与只读重检

用户追加要求并确认自动路径由服务器执行 DoH、手动向导继续由浏览器执行。统一为 DoH → Cloudflare 只读记录核对 → 服务器预检；需要修复时仍须预览、确认、写入、重查传播。新增固定 DoH 适配器与 SetupDoH 端口，覆盖双栈、随机子域、CNAME 链、响应校验和脱敏。

`POST /setup/recheck` 要求精确 Origin、限速、setup_id、串行锁及可重检阶段。复用启动检查但绝不调用 CompleteSetup；通过也只展示最新结果等待手动确认。GET 不触发任务，已有意图/快照和未知提交结果不重放。同步演示 API、文档和隔离 HTTPS DoH/Cloudflare 夹具，补充应用/API/UI 与容器测试。
