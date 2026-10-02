# 参与贡献

感谢你改进 Caddy Web Admin。本项目优先保证普通用户能安全完成反向代理管理，不以功能数量扩大复杂度。

## 开始之前

- 先阅读 [README](README.md)、[工程约定](AGENTS.md) 和 [验证说明](docs/verification.md)。
- 缺陷与功能建议请提交 Issue；安全问题不要公开披露，按 [安全策略](SECURITY.md) 报告。
- 未经讨论不要扩展多用户、多实例、主动健康检查、多上游、路径路由、流量图表或任意配置编辑器。

## 本地开发

工具链以两个 `go.mod`、`web/package.json` 和 Dockerfile 为准。前端依赖必须使用锁文件安装：

```bash
GOPROXY=https://goproxy.cn,direct go mod download
cd web
corepack pnpm install --frozen-lockfile
cd ..
make test
make check
make build
```

只查看演示界面可运行 `make web`，地址为 `http://127.0.0.1:5177`，演示数据重启后重置。工具链、依赖与构建阶段以两个 `go.mod`、`web/package.json`、锁文件及 Dockerfile 为准；源码容器部署见[部署方式](docs/operations.md#部署方式)。不要把真实 Token、生产数据库、生产证书或生产 Caddy 用于测试；`TEST_TLS=true` 仅用于隔离测试。

## 提交变更

1. 先为行为变更添加能够复现问题的测试，再实现最小修复。
2. 复用核心业务层，不在 UI、API 和 CLI 中复制安全规则。
3. 用户可见行为、配置或部署方式变化时同步更新文档和示例。
4. PR 说明应包含问题、方案、验证命令、未验证范围和兼容性影响。
5. 不提交本地 `.env*`、`secrets/`、`.run/`、旧 `run/`、备份、配置导出、私钥、本地工具目录或构建产物。仅显式的 `.env.example` / `.env.*.example` 可作为无凭据模板；Git 忽略规则不影响已经跟踪的文件，发布前仍须核对提交内容。Docker 构建也必须排除上述本地数据。

维护者会重点检查默认安全、失败恢复、数据保留、权限边界和是否超出当前产品范围。
