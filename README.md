# Caddy Web Admin

单容器部署的中文 Caddy 反向代理控制台，支持自动 HTTPS、服务草稿、配置校验、确认发布和在线回滚。

## 快速部署

需要 Docker Compose；初始化前将 80/443 限制为可信 LAN/VPN 可达。

```bash
mkdir -p caddy-admin
cd caddy-admin
curl -fsSL https://raw.githubusercontent.com/gofxq/caddy_admin/HEAD/compose.yaml -o compose.yaml
docker compose up -d
```

打开 `https://服务器IP/setup` 完成初始化，再在设置页提交 Cloudflare Token。首次自签名证书提示属于预期；数据保存在当前目录的 `.run/`。

默认使用 `ghcr.io/gofxq/caddy-admin:latest`，无需 clone 源码或本地构建。

## 使用流程
![workflow](docs/static/caddy_admin.excalidraw.svg)

保存草稿不会影响线上，确认发布后才生效。

## 技术栈

- 后端：Go、Gin、GORM、SQLite。
- 前端：React、TypeScript、Vite、Tailwind CSS。
- 代理与证书：Caddy、Cloudflare DNS 模块。
- 构建与部署：Docker Compose、GitHub Actions、GHCR。

## 文档

- [使用指南](docs/usage.md) · [完整流程与汇总图](docs/product-user-flow-review.md)：服务管理、发布、回滚和配置导入导出。
- [部署与维护](docs/operations.md)：DNS、完整配置、源码构建、外部 Caddy、升级与备份。
- [从零测试](docs/testing.md) · [验证边界](docs/verification.md) · [API](docs/api.md)。
- [开发与贡献](CONTRIBUTING.md) · [安全报告](SECURITY.md) · [变更记录](CHANGELOG.md)。

[Apache-2.0](LICENSE)
