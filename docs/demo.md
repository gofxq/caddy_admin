# 纯静态功能演示

在项目根目录执行：

```bash
make static-web
```

命令使用锁文件安装前端依赖，构建静态产物，再于 `http://127.0.0.1:5178` 启动静态预览；按 Ctrl+C 停止。需要项目要求的 Node.js/Corepack 工具链，首次安装需要依赖仓库访问。无需 Go、Manager、Caddy、Docker、数据库或真实 Cloudflare Token。

## 可体验功能

复用正式产品的概览、服务及详情、发布、证书、审计、设置、初始化与登录页面。默认打开已登录的示例控制台，可以新增、编辑、启停和删除服务，保存设置、查看 Diff 与只读 JSON、校验、确认发布和在线回滚，也可导入导出当前格式的业务配置。

默认预置照片库、监控面板、智能家居、影音库、文件管理和下载中心六个演示服务，覆盖已发布、未发布修改、待首次发布、停用及 HTTPS 上游。已有标签页会保留原数据，可点击「添加演示服务」补充缺少的样例；样例使用当前登记域名，仅追加草稿，不覆盖现有服务或改变已发布版本。重复添加不会生成相同域名的服务。

填写格式正确的配置并完成界面确认后，DNS、上游检查、证书启用、校验与发布均由浏览器模拟成功；不调用业务后端、Cloudflare、浏览器 DoH 或上游服务。发布立即完成。回滚改变模拟已发布版本并保留草稿。域名交接也在当前 Demo 内完成，不跳转到填写的控制台域名。模拟成功不代表真实部署检查通过。

顶部「体验初始化」在明确确认后清除演示数据，打开三步向导。示例密码为 `demo-password`，Token 为 `demo-token`，DNS 目标为 `192.168.1.5`，可直接体验。初始化完成后的服务列表为空，显式导入的服务仅保存为草稿。「重置演示」恢复预置服务与历史发布。默认账号 `admin`；体验初始化时可更改用户名。登出或修改密码后，用当前演示用户名和任意非空密码重新登录，密码维护只模拟流程。

演示数据保存在当前标签页的 `sessionStorage` 中，刷新或站内跳转后仍可继续；关闭标签页后不承诺保留。存储不可用时退回页面内存。密码与 Token 不持久化，仅保留配置状态。请使用示例凭据；演示会话不提供真实鉴权。

已登录控制台的演示横幅位于主内容栏顶部，避开固定的左侧导航；移动端位于导航下方。登录与初始化页面的横幅仍位于页面顶部。

## 构建与 Vercel 部署

只生成静态文件：

```bash
cd web
corepack pnpm install --frozen-lockfile
corepack pnpm build:demo
```

产物为 `web/dist-demo/`，独立于正式产品的 `web/dist/`。可将该目录托管到支持 SPA 路由回退的静态服务。

在 Vercel 导入仓库，将 **Root Directory 设置为 `web`**。该目录下的 `vercel.json` 已配置 Vite、锁文件安装、`build:demo`、输出目录 `dist-demo` 和 SPA 回退，直接访问或刷新 `/services/photos`、`/deployments`、`/settings`、`/setup` 等路径均返回入口页面。无需配置后端地址或密钥，也不使用 Functions。

具体步骤：先将本地改动提交并推送到 GitHub，然后在 Vercel 点击「Add New → Project」，导入该仓库；选择包含 Demo 改动的分支，将 Root Directory 改为 `web`，点击 Deploy。若手动填写构建选项，安装命令为 `corepack pnpm install --frozen-lockfile`，构建命令为 `corepack pnpm build:demo`，输出目录为 `dist-demo`。部署成功后打开 Vercel 提供的网址，并验证 `/services` 页面刷新仍可正常访问。

`make web` 仍为 `127.0.0.1:5177` 的开发服务器演示；`make static-web` 预览的是构建好的静态文件。`corepack pnpm build` 保持正式产品构建，调用真实 Manager API。

## 验证

```bash
cd web
corepack pnpm test
corepack pnpm build
corepack pnpm build:demo
```

静态演示测试覆盖无后端请求、初始化与 DNS、证书启用、草稿及发布状态、回滚保留草稿、刷新恢复、重置，以及密码和 Token 不进入浏览器持久化。上述模拟测试不替代真实 Caddy 集成或容器部署验收。
