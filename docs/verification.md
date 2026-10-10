# 验证与验收边界

按改动范围选择检查；命令成功仅证明对应范围，不等于真实部署或生产验收。

| 命令 | 验证范围 |
| --- | --- |
| `make test` | Go race 测试与前端 Vitest，不启动浏览器 |
| `make check` | Go vet 与两个 Go 模块的依赖校验 |
| `make build` | Manager 构建、严格 TypeScript 与 Vite 构建 |
| `make integration` | 构建固定 Caddy，验证生成、加载、快照恢复和 Cloudflare secret-file |
| `make container-test` | 临时容器中验证内置 HTTP 初始化与 HTTPS 切换、外部 HTTPS 初始化、登录、配置导入导出、发布边界、重启与失联处理；需要 Docker、Python、curl |
| `make web` | `127.0.0.1:5177` 演示界面，仅用于界面检查 |

Compose 配置检查：

```bash
docker compose config --quiet
docker compose -f compose.full.yaml config --quiet
docker compose -f compose.yaml -f compose.build.yaml config --quiet
CADDY_ADMIN_URL=http://caddy.internal:2019 MANAGER_DIAL=manager.internal:8080 \
docker compose -f compose.yaml -f compose.external.yaml config --quiet
docker compose -f compose.yaml -f compose.build.yaml -f compose.validation.yaml config --quiet
```

Setup 自动测试覆盖浏览器 DoH 查询、地址族与通配符、超时与格式错误、无凭据请求、配置/查询状态解耦、连接重试、警告确认指纹与 Cloudflare 记录冲突。DoH 服务的 CORS/CSP、HTTP/HTTPS 实际浏览器访问、真实 DNS 传播和证书签发仍需在专用测试环境验收。

行为变更覆盖主路径、边界、冲突、异常和权限；发布/恢复变更另覆盖断联、崩溃核对及数据保留。测试不用真实 Token、生产数据库或生产 Caddy；`TEST_TLS=true` 仅供隔离夹具，测试 CA 不代表公网信任。

## 从零手工测试

按[从零测试流程](testing.md)完成空白初始化、现有实例导入导出、重启保留数据和新实例初始化导入，再运行自动检查。

验证项目使用独立命名卷和随机宿主端口，不读取真实 `.run/`。验证端口仅绑定宿主回环地址；在宿主浏览器访问或通过受控 SSH 转发访问。`make validation-down` 删除验证项目及其测试卷。内置测试 TLS 模式仍禁止校验与发布，真实证书与业务访问需另行验收。

## 多域名与轻量 Setup 验收

普通 Setup 必须只有三个步骤，不出现网段输入、默认业务访问属性或来源绑定。验证公网/内网来源与公网 Caddy DNS 目标，初始化后默认登录可达但管理 API 必须认证；显式启用控制台来源限制后，Caddy、Manager 和临时入口一致执行，修改业务可信网段不自动启用控制台限制。

使用隔离占位凭据检查密码/Token 两项、单项与均未预配置分支，空值和无效非空值、旧 `setup_id` 拒绝、API 不回显；检查 Compose 明文环境、`.env` 注入及 YAML `$$` 的实际容器传递。重启不得重置密码或以旧环境 Token 覆盖持久化 Token。检查设置草稿和服务共享修订、策略 Diff、未配置访问范围的业务发布阻断、未知 Host 404、仅成功版本回滚保留草稿。schema v6 与 v1 配置文件应明确拒绝并保留原文件。

## 仍需环境验收

- 真实浏览器、移动端、键盘与无障碍，以及五分钟入口关闭流程。
- 隔离真实域名的 Cloudflare DNS-01、续期、Token 轮换及公网信任链。
- 目标宿主防火墙、LAN/VPN、NAT 与 DNS 拓扑。
- 当前 v7 数据重启保留、宿主机级加密快照与完整恢复演练。

历史测试结果见[归档记录](../.archive/docs/verification-history.md)，不替代当前版本的验证。

## 观测验收边界

`make test` 包含根模块 Go race、Caddy 观测模块 race 与前端测试；`make check` 同时检查两个 Go 模块；`make integration` 从当前源码构建真实 Caddy，验证观测配置校验、业务请求 metrics 与脱敏日志、未知 HTTPS Host 不新增标签以及重载 epoch 改变。夹具只使用临时目录、Unix Socket、回环随机端口和内部测试 CA。

自动测试另覆盖 Counter 重置/断采/发布归属、Histogram 插值、五级存储、历史范围边界、服务 ID 不串接、数据库权限、日志去重/持久化游标/首次截断/历史服务恢复、CAA 分类脱敏、低流量和缺口的告警 unknown、确认持久化、会话/CSRF/参数拒绝及前端筛选和诊断交互。

真实浏览器与移动端、长期高负载、宿主磁盘耗尽/写盘卡顿、外部 Caddy 模块安装与网络拓扑、真实 ACME/Cloudflare 签发续期仍需专用环境验收。自动测试不构成这些环境的部署验收。外部通知、原始日志检索和应用 HTTP 健康检查不在本版范围。
