# 完整观测实施计划

> **For agentic workers:** 使用 superpowers:executing-plans，当前会话直接实施。用户已指定 P0–P3 全部并要求继续。

**Goal:** 完成 metrics 提案的指标、流量、服务观测、脱敏日志、诊断和站内告警。

**Architecture:** Caddy 模块提供固定标签 metrics 和受保护脱敏日志读取；应用层负责发布归属、采集增量、查询与告警，独立 SQLite adapter 存储，React 复用既有 API 与组件。

**Tech Stack:** 现有 Go/Gin/SQLite、Caddy v2.11.3、React/TypeScript；Prometheus 标准解析器复用 Caddy 已使用版本，不引入外部基础设施。

**Spec:** `docs/superpowers/specs/2026-10-03-observability-complete-design.md`

## Global Constraints

- 指标、日志、告警、周期检查默认关闭，设置保存草稿后显式发布。
- 15 秒采集，五级聚合；独立 observability.db，0600，256 MiB，不能进入 readiness。
- 固定服务 ID/Host，拒绝未知 Host、控制台指标，最多 500 服务、30000 series、8 MiB 响应、1000 查询点。
- 写前日志脱敏，源文件 16 MiB+一份轮转、7 天/100000 条索引；访问日志不含原始 query/header/body/error。
- 外部采集只读，缺少模块显示不可用；站内通知；TCP 只使用已发布安全快照。

## Review Focus

- 并发发布、重启、重载、断采不得串接 Counter 或归属新服务 ID。
- 未知 HTTPS Host、方法和状态不能制造无限标签。
- 文件/数据库写入失败、队列满、日志游标落后不能拖慢代理或核心操作。
- 空窗口/低流量/不完整数据不能成为正常或触发指标告警。
- 管理接口需会话、首次改密、Origin/CSRF；外部缺少模块必须明确反馈。

## Task 1：Caddy 模块和生成器

**Files:** `deploy/caddy/observability/`、`deploy/caddy/main.go`、`Dockerfile`、`internal/domain/generate.go`、`settings.go`、`internal/application/service.go`。

**Interfaces:** 固定 Prometheus families `caddy_admin_*`；GET `/caddy-admin/observability/logs`，epoch/sequence 游标。

- [x] 写安全标签、记录字节/时长、脱敏、错误分类、队列/轮转与配置生成测试；核心新增用例先观察失败，再补实现。
- [x] 实现 App/Middleware/AdminRouter/安全编码器及设置草稿映射，确保 validate 不写真实日志。
- [x] 运行 root domain/application 测试和 Caddy module tests。

## Task 2：观测数据与采集

**Files:** `internal/domain/observability.go`、`internal/application/observability*.go`、`internal/adapter/observability/`、`internal/adapter/caddy/observability.go`、`cmd/manager/main.go`。

**Interfaces:** `ObservationSource.Metrics/Logs`、`ObservationStore.Write/Traffic/Logs/Close`；Service 启动/关闭生命周期。

- [x] 写 Counter/Histogram、重启/断采/发布归属、聚合、去重、retention、数据库容量故障测试；核心新增用例先观察失败，再补实现。
- [x] 实现标准 metrics 解析器、有限采集、数据库与查询，核心服务无观测依赖故障。
- [x] 运行对应包测试，确认数据保留与权限。

## Task 3：诊断、告警和 API

**Files:** `internal/application/diagnostics.go`、`alerts.go`、`internal/adapter/caddy/diagnostics.go`、`internal/adapter/httpapi/observability.go`。

**Interfaces:** GET `/traffic`、`/logs`、`/alerts`、`/observability`；POST `/diagnostics`、`/alerts/:id/acknowledge`。

- [x] 写低流量/缺口、指标规则、证书、后台 TCP、安全 DNS/CAA 目标、鉴权与 CSRF 测试；核心新增用例先观察失败，再补实现。
- [x] 实现站内状态、手动诊断和可选周期检查。
- [x] 运行对应包与 API 测试。

## Task 4：界面、mock、文档和验收

**Files:** `web/src/pages/Traffic.tsx`、`components/TrafficSummary.tsx`、`pages/Overview.tsx`、`ServiceDetail.tsx`、`Settings.tsx`、`model.ts`、`main.tsx`、`web/mock-api.ts`；使用/API/运维/roadmap 文档。

- [x] 写趋势、空/错误/缺口、过滤、诊断与告警确认交互测试。
- [x] 实现响应式流量页、站内告警、服务统计/日志、设置开关；同步 mock。
- [x] 运行 `make test`、`make check`、`make build`、`make integration`，检查真实 Caddy 指标/日志，并记录未覆盖部署边界（容器验收已尝试但受依赖下载阻断）。
- [x] 自审完整差异，修复发现；交付不提交、不合并、不操作生产部署。

## 实施与验证记录

- 完成上述四组工作；界面统计组件集中于 `web/src/components/TrafficWorkspace.tsx`，概览、流量页和服务详情复用；mock 实现在 `web/src/demo/mock-api.ts`，由 `web/mock-api.ts` 导出。
- P0–P3 使用默认关闭的四个设置开关，包含设置 Diff、JSON 导入导出与显式发布流程；开启任一观测开关时限制 500 服务、64 登记域名。
- `make test`：根模块 Go race、Caddy 观测模块 race、前端 25 个文件 / 160 个测试通过。
- `make check`：两个模块静态检查与依赖完整性检查通过；`make build`：Manager、严格 TypeScript 和 Vite 构建通过。Vite 仍提示主 JS chunk 超过 500 kB。
- `make integration`：当前源码真实 Caddy 的配置校验、加载失败保留、引导 TLS、secret 文件及观测集成通过。观测测试验证已完成请求的字节/Histogram、写前脱敏、未知 Host 不增加标签和重载 epoch。等待内部测试证书就绪后，观测集成连续重复 5 次通过。
- `docker compose config --quiet` 通过。`make container-test` 已执行，但在 Dockerfile 的 `go mod download` 阶段因 `goproxy.cn` TLS handshake timeout 失败，未进入容器验收，不能视为部署已通过。
- 独立只读代码审查发现并修复时间范围外 bucket 计数、首次源端截断漏报、重启后的历史日志归属以及服务筛选选项收窄；全局查询超限时追加当前已发布服务作为选择兜底。相应回归测试通过。
- 追加容量上限失败后历史仍可读取、保留期限/未知 schema、日志游标重启/去重、确认状态并发合并、源文件轮转/权限/写盘失败、编码器隐私、告警运行版本绑定以及七天日志请求延时边界测试。
- 实现选择：用固定服务标签 `caddy_admin_*` 取代不安全的原生 `per_host`；使用 SVG 趋势与数值表格，未新增 ECharts 或监控基础设施。独立派生数据库保持主业务 schema v7 不变。
- 未验证真实浏览器/移动端、长期负载、真实 ACME/Cloudflare、宿主机磁盘耗尽以及目标外部 Caddy 部署；没有提交、合并或修改真实运行数据。
