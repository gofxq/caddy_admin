# 变更记录

本文件记录面向用户的重要变化，格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本遵循[语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### Added

- 新增无需登录的 `/portal` 服务导览：简洁卡片、搜索与 HTTPS 快速链接，只展示已发布且启用的服务，遵循控制台和业务域名的网络访问限制。

- 可选完整观测：固定服务标签指标与五级历史聚合、流量页和单服务趋势、写前脱敏日志、DNS/CAA/TLS 诊断及站内告警；后台 TCP 限额轮询。开关默认关闭，经校验和明确发布启用；观测故障不阻断核心管理。

- 默认 Compose 使用 `ghcr.io/gofxq/caddy-admin:latest` 预构建镜像，仅下载配置文件即可安装；新增 `compose.build.yaml` 源码构建覆盖和检查通过后的 GitHub 镜像发布流程。

- 设置页支持业务配置 JSON 导入导出；现有实例导入仅替换草稿，新实例初始化可采用域名、网络策略及服务草稿，不携带密钥或自动发布。

- 初始化 DNS 步骤提供可编辑的 Caddy 地址建议、Cloudflare 灰云通配符 A/AAAA 记录与复制操作，并支持按需重新检查 DNS。

- 默认 `docker compose up -d` 可从空目录启动：自动创建持久化目录，内置模式通过 HTTP 80 或 HTTPS 443 初始化，完成后 HTTP 恢复 HTTPS 跳转。
- 内置初始化提交 Cloudflare Token，预览并确认自动配置灰云通配符 DNS，随后申请证书；外部及隔离测试模式不要求本机 Token。Token 输入处提供申请链接，保留临时 HTTPS 入口与可信证书发布门禁。
- 单命令容器安装与 Web 首次配置向导；业务域名和网络策略不再带个人化默认值。
- 使用随机回环端口和独立命名卷的从零验证环境。
- 首次发布三步引导，以及服务和发布页面的可操作空状态。
- 错误影响、下一步建议和安全重试入口。
- Apache-2.0 许可证、贡献指南、安全报告策略和产品改进清单。

### Changed

- 仅支持当前版本：删除旧设置环境变量导入、旧 `run/` 自动迁移、历史地址别名、旧快照启动补写、无指纹证书状态推断及 `manager init` 补建快照命令；当前实例设置由 SQLite 提供。

- 历史需求、设计、评审与验收记录移至 `.archive/`，当前文档集中到安装、运维、API、验证与待办。
- CI 使用当前 Caddy 适配层的真实验证测试，修正已删除包的旧引用。

- 后端从扁平 `internal/core` 拆分为 domain、application、config 及 GORM/Caddy/Gin adapter；Manager 成为唯一组合根，并增加依赖方向架构测试。
- 宿主运行数据目录为 `.run/`，保留 Manager、启动快照、Caddy 数据与配置的独立持久化。
- HTTP 适配层由 chi/ServeMux 统一为 Gin 原生路由与 handler，集中处理严格 JSON、错误响应、请求 ID、恢复、认证及 CSRF，同时保持 `/api/v1` 契约不变。
- 数据持久化统一由 GORM 管理，并使用基于 modernc SQLite 的纯 Go Dialector；继续支持无 CGO 构建和 ARM64 交叉编译。
- 数据库兼容边界收紧为空库或结构完整的当前 v6；v1–v5 数据库会在不写入的情况下拒绝启动，不提供旧版本升级或迁移入口。
- Setup 无需预配置 Origin 或一次性凭证；只允许私网 TCP 对端、IP 字面量 Host 和与当前请求精确一致的 HTTP/HTTPS Origin。第一个有效提交原子成为管理员。
- 首次配置只输入 Homelab 域名；控制台固定派生为 `caddyadmin.<homelab>`，Public 域名初始为空且 Public 服务暂时禁用。
- Cloudflare 签发期间使用临时控制台证书；初始化地址提供受 LAN、认证、Origin 与 CSRF 保护的临时管理入口，正式入口就绪被读回后保留五分钟再关闭。
- 托管业务配置存入 SQLite；当前 v6 数据库启动时只做结构验证，不自动改写历史数据或启动快照。
- 外部 Caddy 初始化改由远端实例管理 Cloudflare Token；Manager 不再要求或读取重复的本地 secret。
- 服务校验与发布需通过实时 TLS 信任探测。
- 登出失败改为页面内反馈，并保留当前会话与未保存输入。

### Removed

- 内置模式的独立 8082 Setup/交接监听与宿主备用映射；默认和隔离验证部署只发布 80 TCP、443 TCP/UDP，外部模式保留独立 Setup 与 Manager 端口。

- 项目 `.env`、宿主安装/启动/维护脚本，以及 Setup Token/固定 Setup Origin 配置。
- 尚未具备 Web 替代方案的项目级备份、恢复和离线救援命令；生产部署需使用已验证的宿主机级加密快照。

## [0.1.0] - 2026-09-22

### Added

- 单实例、单管理员的 Caddy 服务草稿、预览、校验、发布、回滚和审计流程。
- 证书概览、初始化、密码维护、离线救援以及加密备份恢复。
- 内置单容器模式与专用外部 Caddy 模式。

[Unreleased]: https://github.com/gofxq/caddy_admin/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/gofxq/caddy_admin/releases/tag/v0.1.0
