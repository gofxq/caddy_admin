# 默认开放访问策略实施计划

> **For agentic workers:** 使用 superpowers:executing-plans 逐项实施；用户已要求按现有目标完整实现。

**Goal:** 新实例和新增域名默认采用最开放许可，保留鉴权、目标保护、明确发布和用户配置。

**Architecture:** 初始化在 `ManagedSettingsForSetup` 统一生成 `internet`、双栈全网段与 `allowed_names=["*"]`。领域策略只新增独立 `*` 的许可语义，仍校验名称及每个解析 IP。前端和演示展示同样默认值，历史与导入配置不补默认。

**Tech Stack:** 项目现有 Go/Gin/SQLite、React/TypeScript/Vite，不增加依赖。

**Spec:** [域名与访问策略](../../domain-access-policy.md)

## Global Constraints

- `console_lan_only=false`；`lan_cidrs` 与 `upstream_cidrs` 均为 `["0.0.0.0/0", "::/0"]`。
- 独立 `*` 许可合法名称，精确名称继续支持，空名称许可或空网段仍拒绝；不支持其他通配形式。
- 保留安全地址、HTTPS 验证、DNS 重校验、会话/Origin/CSRF、发布确认及待核对保护。
- 不自动改写已有实例、显式导入、历史发布或启动快照；不迁移 schema，不发布真实实例。

## Review Focus

- 名称返回安全与危险混合 IP 时整体拒绝，不能只检查选中的地址。
- 默认可信全网段下启用开关不会收紧，页面须解释如何替换为实际网段。
- 上游名称许可非空、网段空时仍不能添加服务，首用引导不得误判完成。
- 导入 `null`/空列表及已有设置的读取不得补上新的开放值。
- 默认公开服务仍要求发布暴露确认，响应丢失/崩溃流程保留现有保护。

## Task 1: 后端默认与服务名许可

**Files:** `internal/domain/policy.go`、`internal/application/setup.go` 及对应策略、初始化、自动初始化、配置导入导出测试。

**Interfaces:** `ValidateManagedNetwork(ManagedSettings) error` 接受独立 `*`；`NormalizeService(context.Context, TargetPolicy, Service) (Service,error)` 继续保护全部地址；`ManagedSettingsForSetup(SetupSettings) (ManagedSettings,error)` 仅在无导入时产生新默认。

- [x] 写出默认值、显式导入不覆盖、全部名称许可/精确许可/空许可、混合危险 IP 的失败测试，运行对应 Go 测试确认失败。
- [x] 更新默认构造与名称许可判断，不改变保护和发布实现。
- [x] 验证自动初始化持久化与仅控制台快照、配置 v2 导入导出 `*`、已有实例与收紧策略。

## Task 2: 前端、演示与交互

**Files:** `web/src/model.ts`、`pages/Settings.tsx`、`pages/Overview.tsx`、`components/SetupConfigurationSummary.tsx`、`demo/mock-api.ts` 及配置、设置、首用和演示测试。

**Interfaces:** 配置 JSON 字段不变；前端读取真实许可；新建域名默认 `internet`，导入保留原值。

- [x] 新增摘要、开放默认直接添加服务、新增域名确认、全可信网段提示与 `*` 导入导出的失败测试，运行 Vitest 确认失败。
- [x] 同步界面与演示默认值，说明全部名称和收紧方式，空网段任务继续未完成。
- [x] 运行前端测试与严格 TypeScript/Vite 构建。

## Task 3: 文档、验证与审查

**Files:** `AGENTS.md`、`README.md`、`docs/{setup,usage,api,operations,verification,roadmap,domain-access-policy,product-user-flow-review}.md`、`CHANGELOG.md`，必要时调整隔离容器测试默认值断言。

- [x] 将现行默认文档与代码同步，旧设计标明被本方案取代；记录已有实例和导入策略不变。
- [x] 运行 `make test`、`make check`、`make build`、`make integration`，按可用环境运行隔离容器测试。
- [x] 检查实际差异、链接和格式，完成独立代码审查，修复影响需求的发现，记录验证结果与未验证边界。

## 执行记录

- 用户已授权完整实现现有目标；在共享工作区保留现有文档修改，不提交或发布，不操作真实数据。
- Task 1 与 Task 2 通过相同 JSON 字段衔接；没有新增模式字段或 schema 迁移。

- 后端默认值与独立 `*` 测试已观察到修改前失败、修改后通过；全网段边界另补有限广播保护测试并完成失败/通过验证。
- 默认摘要、新增域名确认、首用引导、配置 v2 及演示初始化测试已观察到修改前失败、修改后通过；演示导入额外域名和控制台限制仅进入草稿，补齐与真实 Setup 的一致性。
- 独立审查未发现重要缺陷，补充审查确认广播保护仍允许私网/ULA，以及隔离脚本的会话/CSRF 使用。

- 验证通过：`make test`（Go race + 前端 29 个文件、222 项测试）、`make check`、`make build`、`make integration`、`make container-test`，默认 Compose 配置检查及 Demo 构建。构建保留现有的 bundle 大小提示。
- 容器验收使用临时卷/网络和占位凭据，覆盖内置及外部模式、自动初始化的全网段与 `*` 默认、服务名草稿、缺少 CSRF 拒绝、登录、配置导入导出、发布及重启。真实 Token、公网 ACME 签发、移动端和用户网络的可达性未做验收。
