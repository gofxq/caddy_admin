# 轻量观测第一阶段实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付服务详情和安全的管理员手动 TCP 检查，并明确后续 metrics 的进入条件。

**Architecture:** 领域层维护详情/检查结果和地址快照校验，应用层组合草稿、成功发布、运行指纹与探测端口。适配器负责 TCP 连接和受认证 HTTP，前端复用既有页面、API 客户端及组件，不新增依赖。

**Tech Stack:** 现有 Go + Gin + SQLite、React + TypeScript + Vite。

**Spec:** `docs/superpowers/specs/2026-10-03-observability-foundation-design.md`

## Global Constraints

- 保留单实例、单管理员、六项导航和既有确认发布流程。
- 本批仅实现阶段 A，不默认开启 metrics、周期探测、访问日志或新增依赖。
- 运行核对和 TCP 检查超时均为 3 秒，全实例同时至多一个手动检查。
- 检查请求只接受 `expected_hash` 与 `expected_deployment_id`；只访问已发布、启用、经过当前生效策略校验的地址快照。
- 最近 20 次全局发布中，最多返回 5 条涉及服务 ID 的发布摘要。
- 不改写启动快照、历史发布、数据库 schema；不使用真实 Token 或真实部署数据。

## Review Focus

- 草稿删除与同域名重建：旧服务详情仍归属旧 ID，未发布的新服务不能探测。
- 断联和待核对：显示未知/待核对，不能伪装成健康，也不丢失配置数据。
- DNS 变化和许可撤销：只探测固定地址，当前策略拒绝即不进行连接。
- 并发发布和探测：探测前后核对版本，旧结果失效，不占用发布锁。
- 非授权请求：详情需会话和首次改密；手动检查还需精确 Origin 和 CSRF。

## Task 1：详情业务模型与安全快照

**Files:** `internal/domain/service_detail.go`、`internal/domain/service_detail_test.go`、`internal/application/service_detail.go`、`internal/application/service_detail_test.go`。

**Interfaces:** `Service.ServiceDetail(ctx context.Context, id string) (domain.ServiceDetail, error)`；`domain.ValidateServiceDial(policy TargetPolicy, service Service) error`。详情含 `id/revision/draft/published/draft_domain/published_domain/runtime/recent_deployments/external_caddy`。

- [x] 写状态、ID 归属、历史过滤、并发版本变化和地址快照安全的失败测试。
- [x] 运行 `go test ./internal/domain ./internal/application -run 'Test(ServiceDetail|ValidateServiceDial)' -count=1`，确认 RED。
- [x] 实现模型、领域校验、3 秒运行核对和有限历史摘要；保留不可达时配置数据。
- [x] 重跑上述测试，确认 GREEN。

## Task 2：手动 TCP 检查与 API

**Files:** `internal/application/ports.go`、`service.go`、`upstream_check.go`、`upstream_check_test.go`；`internal/adapter/caddy/upstream.go` 与测试；`cmd/manager/main.go`；`internal/adapter/httpapi/api.go`、`service_detail.go`、`service_detail_test.go`、`routes_test.go`。

**Interfaces:** `UpstreamProbe.Check(context.Context,string) domain.UpstreamCheck`；`Service.CheckUpstream(ctx context.Context,id,expectedHash,expectedDeploymentID string) (domain.UpstreamCheck,error)`；GET `/services/:id`；POST `/services/:id/check-upstream`。

- [x] 写固定快照、策略拒绝、漂移/未决/停用/过期指纹、检查后版本变化、超时和并发限制失败测试；API 写鉴权、改密、Origin/CSRF、任意目标字段拒绝测试。
- [x] 运行对应包 `-run 'Test(CheckUpstream|TCPUpstream|ServiceDetailAPI)' -count=1` 确认 RED。
- [x] 实现安全检查用例、3 秒 TCP adapter、组合根注入和 HTTP 接口。
- [x] 对应测试确认 GREEN；使用临时 loopback listener 验证真实连接与失败行为。

## Task 3：详情界面与演示接口

**Files:** `web/src/pages/ServiceDetail.tsx`、`web/src/service-detail.test.tsx`、`web/src/model.ts`、`main.tsx`、`pages/Services.tsx`、`styles.css`、`web/mock-api.ts`、`web/src/mock-api.test.ts`。

**Interfaces:** 页面路由 `/services/$id`；`ServiceDetail`、`UpstreamCheck` 类型与后端字段一致。

- [x] 写草稿/已发布分离、待删除、断联保留数据、手动检查重试、版本变化清除旧结果、切换服务清除旧结果的失败测试。
- [x] `cd web && corepack pnpm test src/service-detail.test.tsx src/mock-api.test.ts` 确认 RED。
- [x] 实现详情、列表和待删除入口、动态路由、移动端样式与 mock。
- [x] 同一测试命令确认 GREEN。

## Task 4：文档、全量验证与独立评审

**Files:** `docs/roadmap.md`、`docs/usage.md`、`docs/api.md`、本计划。

- [x] 更新已实现范围和后续优先级，说明 TCP 检查网络视角、状态与限制。
- [x] 运行 `make test`、`make check`、`make build`，逐项读取结果。
- [x] 请求一位独立 reviewer 检查全部工作区差异和本设计，修复重要发现并补回归验证。
- [x] 更新任务完成记录，交付改动和验证结果；不自动提交、合并、发布。

## 执行记录

- 用户已明确要求计划后开始实施，按其授权连续执行，不增加重复确认。
- git 工作区初始干净；worktree 创建因 `.git` 只读失败，按技能的 sandbox fallback 原地实施，不进行 Git 写入。
- 基线：`make test` 通过（Go race 各包通过，前端 17 文件 / 119 测试通过）。
- Task 1：详情及快照安全测试 RED → GREEN，覆盖草稿与成功发布分离、ID 归属、断联/漂移/未决及核对期间版本变化。
- Task 2：检查和 API 测试 RED → GREEN，覆盖固定地址、当前安全策略、旧指纹、并发与发布锁隔离、检查后版本变化、真实本机 TCP、认证与 Origin/CSRF。
- Task 3：前端和 mock 测试 RED → GREEN，2 文件 / 19 测试通过，覆盖配置分离、待删除、手动重试与旧结果失效。

- Final review：3 项 Important，无 Critical/Minor。发布在 Pending 查询期间完成、相同 hash 新发布、后台刷新 503/404 与晚到检查均补回归测试，观察 RED 后修复为 GREEN。
- Ruling：检查绑定配置 hash 与发布 ID，新增 `expected_deployment_id` 请求字段及运行版本元数据，避免相同 hash 的新发布保留旧结果；只影响本批新接口，文档与 mock 同步，无旧接口兼容负担。
- 原始 AGENTS.md 是被 .gitignore 忽略的本地指令文件，不作为交付文件改写；本次显式授权的新增范围写入受版本管理的 roadmap、使用与 API 文档。

- 最终验证：`make test` 通过（Go race 全部包，前端 18 文件 / 134 测试）；`make check` 通过；`make build` 通过；`git diff --check` 通过。Vite 提示主 JS chunk 585.42 kB，超过 500 kB 建议值，本批未引入依赖。
- 验证边界：完成自动测试与临时本机 TCP 连接验证，未进行真实浏览器、移动端或内外模式部署验收；未修改 Caddy 配置生成、数据库 schema 或部署文件。
- 交付：阶段 A 完成；阶段 B–E 按 roadmap 的进入条件继续规划。全部修改留在工作区，未提交、合并或部署。
