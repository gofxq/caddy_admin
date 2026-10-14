# 前端功能与页面布局检查

检查日期：2026-10-14。以当前工作区（包含已有未提交改动）为准。本次只新增检查报告和截图，不修改产品代码。

目前主要问题是异步操作与页面状态衔接、草稿与线上信息混用、移动端操作入口，以及共享样式影响了不相关的内容。整体视觉体系可继续使用，修复不需要更换组件库或重做页面。

## 检查范围与证据

- 阅读初始化、登录、概览、服务列表与详情、发布与回滚、证书、审计、设置、服务导览和现有流量页面，以及共享组件、API 客户端、业务模型、CSS、相关测试和文档。
- 在独立回环演示服务器上用 Chromium 检查七个管理页面的 320、390、800、1024px 布局；另检查 1440px 下的概览、服务与发布。390px 补查服务详情、编辑弹窗、导览、登录和 Setup。Setup 使用浏览器注入的状态与预检响应，不执行初始化。
- 注入保存响应延迟、读取 503、未发布控制台地址；执行一次演示发布。未接触真实数据库、Caddy、Cloudflare、Token 或运行目录。
- `cd web && corepack pnpm test`：29 个测试文件、222 项测试通过。
- `cd web && corepack pnpm build`：TypeScript 与 Vite 构建通过。主 JS 624.98 kB，gzip 199.76 kB，存在大于 500 kB 的构建警告。
- 基础样例下，上述四种屏宽的七个管理页面没有整页横向溢出；服务表格的局部横向滚动符合当前设计。不能据此证明最长合法内容、手机软键盘、真实证书或真实发布均已验收。

下表共 36 个检查项，包括缺陷、打磨建议和验证缺口。**B** 表示浏览器复现，**C** 表示代码或文档直接确认，**建议**表示体验改进；仅代码确认的竞态或极端布局需补浏览器回归。P1 优先修复数据、状态和核心操作问题；P2 改善核心流程；P3 为一致性和维护优化。本报告不是“所有环境下不存在其他问题”的保证。

截图：[桌面概览](static/frontend-review-2026-10-14/overview-desktop.png)、[移动概览](static/frontend-review-2026-10-14/overview-mobile.png)、[移动发布](static/frontend-review-2026-10-14/deployments-mobile.png)、[移动流量](static/frontend-review-2026-10-14/traffic-mobile.png)。截图为演示数据，其中控制台地址草稿是在检查中修改的虚构地址。

## 功能与交互

| 编号 / 优先级 / 证据 | 问题、触发与影响 | 根因与位置 | 修复方案与验收条件 |
| --- | --- | --- | --- |
| F01 · P1 · B | **保存设置会丢掉等待响应期间的新输入。** 提交 DNS `1.1.1.1`，保存中改成 `8.8.8.8`，响应成功后输入恢复为 `1.1.1.1`。 | [Settings](../web/src/pages/Settings.tsx)：只有按钮禁用，字段仍可编辑；`onSuccess` 无条件 `setDraft(null)`、清空 `rawLists`。 | 最小修复是在保存期间禁用整组相关字段与增删操作。若需要继续编辑，则将提交快照和后续修改分开，只清除已提交内容。验收：延迟响应下不会静默丢失新输入；失败保留原值。 |
| F02 · P1 · B+C | **正式入口提示可能指向尚未发布的控制台域名。** 将草稿 Origin 注入为 `https://unpublished.home.example.com`，顶栏“打开正式入口”直接使用这个地址，即使证书与运行状态对应旧入口。首次任务中心也取草稿 Origin。 | [EntryMigration](../web/src/components/EntryMigration.tsx)、[Overview](../web/src/pages/Overview.tsx) 使用 `settings.config.origin`；[设置接口](../internal/adapter/httpapi/settings.go) 明确区分 `config` 草稿和 `active_config` 生效配置。 | 所有“正式入口已就绪”、当前入口判断和迁移按钮以 `active_config` 为准，待发布地址单独提示。验收：仅保存地址草稿不改变迁移目标；发布后再展示新入口。 |
| F03 · P1 · B+C | **发布成功后短暂沿用旧 Diff 和旧校验。** 演示 v3 成功后仍显示原来的 2 项变更、“真实配置校验通过”，且“确认发布”仍可点击。 | [Deployments](../web/src/pages/Deployments.tsx)：提交成功保留 `validated`；任务终态只失效 deployments/overview/services/settings，没有立即更新 preview，等待最长约 15 秒的轮询。 | 接收发布任务后让校验进入已使用状态；终态立即刷新当前候选，成功/失败后按结果重新要求校验。保留响应丢失时的幂等标识，不能一律生成新标识重发。验收：成功后旧确认入口不可再次提交，Diff 随最新线上版本更新；断联仍按任务核对。服务端校验保护仍存在，本项不等于已证实能重复修改线上。 |
| F04 · P2 · B+C | **读取失败同时显示“没有记录”。** 审计 503 出现“暂无审计记录”；发布历史 503 出现“还没有发布记录”；告警 503 出现“没有待处理告警”。日志也有同类分支。 | [Audit](../web/src/pages/Audit.tsx)、[Deployments](../web/src/pages/Deployments.tsx)、[TrafficWorkspace](../web/src/components/TrafficWorkspace.tsx)：失败后用 `items ?? []` 进入空状态。 | 分开加载、失败、成功为空、成功有数据；有缓存时显示上次数据及“刷新失败”。验收：首次失败只显示失败与重试，不给出无记录/无告警结论；证书成功空列表补明确空状态。 |
| F05 · P2 · C | **后台刷新失败时，旧的绿色状态缺少过期标记。** 概览保留“保持一致”、服务保留“路由已发布”，证书保留“证书有效”；顶部虽有错误，但卡片未统一说明当前状态未重新核对。服务行的“已生效”也只比较服务字段，没有比较域名访问策略。 | [Overview](../web/src/pages/Overview.tsx)、[Services](../web/src/pages/Services.tsx)、[Certificates](../web/src/pages/Certificates.tsx)、[model.serviceState](../web/src/model.ts)；可参考 [ServiceDetail](../web/src/pages/ServiceDetail.tsx) 对刷新失败的处理。 | 保留旧数据与时间，状态旁加“上次结果／当前未确认”；服务列表区分已发布配置与实际运行核对，域名策略待发布单独标识。验收：缓存成功后断网、策略草稿变更、运行漂移时，均不将旧配置一致性当作当前事实。 |
| F06 · P2 · C | **Setup 交接会自动跳转，与页面及运维流程的手动核对约定冲突。** 服务端认为 HTTPS 就绪后 `location.assign`，可能将设备带到本机仍不可达的域名。 | [Setup](../web/src/pages/Setup.tsx) 的 `openedConsole` effect；[SetupHandoff](../web/src/components/SetupHandoff.tsx) 与 [流程文档](product-user-flow-review.md) 要求用户从本机打开入口，服务端探测不代表设备可达。 | 交接页持续显示就绪状态，保留“打开正式控制台”和临时登录入口，由用户点击。验收：服务端变成 ready 时不自动离开；用户可复制地址并在访问失败后继续核对。 |
| F07 · P2 · C | **导入初始化时可编辑首个域名，但没有同步更新导入域名列表。** 任意改成文件中不存在的域名，前端格式检查通过，最终才被服务端拒绝。 | [Setup](../web/src/pages/Setup.tsx)：`form.domain` 可编辑，`importSettings.domains` 仍来自原文件；[ManagedSettingsForSetup](../internal/application/setup.go) 要求首个域名属于导入配置。 | 采用导入后，首个域名使用已导入域名的选择器或只读展示；要改文件外域名时明确取消导入或通过已有专家入口处理。验收：不能产生“控制台首域名不在导入域名中”的提交，保留明确导入策略，不补开放默认值。 |
| F08 · P2 · C | **“放弃修改”立即清空整个设置表单，没有确认。** 与跨页离开的保护不一致，长表单误点难以恢复。 | [Settings](../web/src/pages/Settings.tsx) 的放弃按钮直接清空 draft/rawLists。 | 有真实修改时使用统一放弃确认，或提供可撤销的恢复机制。验收：取消确认保留输入；确认后恢复最新草稿。 |
| F09 · P3 · C | **设置页脏状态误报。** 改字段再改回原值，`draft` 仍存在，离开仍警告且保存仍可用。 | [Settings](../web/src/pages/Settings.tsx) 使用 `!!draft` 判断未保存，而非内容比较。 | 用编辑起点与规范化后的有效设置比较，同时考虑未解析文本；处理服务端修订更新时保留编辑起点。验收：还原所有值后不警告、不提交无变化；真实修改仍被保护。 |
| F10 · P2 · C | **配置导入成功后未刷新设置修订。** 导入替换草稿会增加共享修订，但设置 Query 没有失效，立即编辑设置可能用旧修订提交而产生本可避免的冲突。 | [ConfigurationTransfer](../web/src/components/ConfigurationTransfer.tsx) 只失效 services/preview/overview/audit。 | 同时更新或失效 settings，并在相关数据读取完成前避免用旧修订开始编辑；统一共享修订更新。验收：导入后设置页展示新修订；紧接着保存设置不因本次导入自身产生冲突。 |
| F11 · P2 · C | **域名 DNS 输入会被无关草稿保存清空。** DNS 组件用共享修订作 key，修订变化后整个组件重新挂载；域名、目标 IP、查询结果和错误都被重置。 | [Settings](../web/src/pages/Settings.tsx) 中 `DomainDNS key={d.revision}`，组件输入均为本地 state。 | 保持组件实例；仅相关域名、IP、凭据或指纹变化时作废预览/确认/查询，保留 IP 输入。验收：保存无关观测开关不清空 DNS 输入；相关变更仍强制重新核对。 |
| F12 · P2 · C | **日志时间窗口改变后仍留在旧页码。** 已翻页后从 24 小时切到 1 小时，查询使用新窗口与旧 offset，可能出现空列表。日志实际范围最多约 7 天，但统计可选一年，缺少明确的实际日志起止时间。 | [TrafficWorkspace.LogViewer](../web/src/components/TrafficWorkspace.tsx)：hours 改变未重置 offset；请求对 hours 作七天截断。 | 时间窗口变化重置日志页码，展示日志实际范围及截断说明。验收：翻页再切时间从第 1 页开始；一年统计窗口下日志清晰标明仅最近七天。 |
| F13 · P3 · C+建议 | **分页用“本页条数等于页长”猜测还有下一页。** 恰好 20 条时可进入空白第 2 页，删除或过滤后也缺少越界处理。 | [shared.Pager](../web/src/components/shared.tsx)；日志分页也使用相同方式。 | API 返回 `has_more`，或内部多取一条判断，不必增加昂贵总数查询；条件变化重置页码，越界时返回上一有效页。验收：恰好一页、最后一页、读取失败和记录减少的页码均合理。 |
| F14 · P2 · C | **服务详情“查看发布记录”不能定位对应版本。** 无论点击哪行都只进入发布页面，用户需自己在分页历史中寻找。 | [ServiceDetail](../web/src/pages/ServiceDetail.tsx) 所有记录链接都指向 `/deployments`，未带记录 ID。 | 给发布页增加受校验的详情查询参数，按 ID 读取并打开详情；保留当前历史页与候选模式。验收：点击 vN 打开 vN；无效 ID 展示明确错误，不显示其他版本。 |
| F15 · P2 · C | **任务卡可能长期固定在本标签页的旧任务。** `pinned` 发布后始终为 true，其他标签页的新任务不能替换卡片；翻到历史后 `latest` 读取失败也没有独立反馈。 | [Deployments](../web/src/pages/Deployments.tsx) 中 activeID/pinned/latest/locked 的组合。 | 本地任务进行中固定；终态后允许最新任务接管，或将“我提交的任务”和“全局最新任务”明确分开。最新状态失败标为未知并提供重试。验收：两标签页先后发布、历史翻页、latest 断网，卡片与全局锁定原因一致。 |
| F16 · P2 · C | **输入保护不覆盖 Setup 和改密码表单。** Setup 直接刷新/离开会丢管理员、域名与 DNS 输入；Settings 的离开保护只看设置 draft/Token，不看密码表单。改密码期间当前/确认密码仍能编辑。 | [Setup](../web/src/pages/Setup.tsx)、[Login.PasswordForm](../web/src/pages/Login.tsx)、[Settings](../web/src/pages/Settings.tsx)、[useUnsavedChanges](../web/src/components/useUnsavedChanges.ts)。 | 对有输入的表单增加内存中的离开保护，提交期间禁用相关字段；不把密码/Token 写入浏览器持久化存储。初始化结果未知时继续交接核对，不能将离开保护变成重放提交。验收：离开取消保留输入；成功和用户明确放弃后解除保护。 |

## 页面布局与可访问性

| 编号 / 优先级 / 证据 | 问题、触发与影响 | 根因与位置 | 修复方案与验收条件 |
| --- | --- | --- | --- |
| L01 · P2 · B+C | **流量空状态出现巨大的绿色图标。** 四张无趋势数据卡片各占据很大纵向空间，页面显著拉长。 | [styles.css](../web/src/styles.css) 的 `.trend-card svg { width:100%;height:auto;... }` 命中嵌套的 Empty 图标；同时用了不存在的 `--primary` token，回退为绿色。 | 只给实际图表 SVG 专用类名与尺寸，Empty 图标保持 30px；使用现有 `--color-primary`。验收：无数据与有趋势两种状态分别检查，空状态图标不会被图表规则放大。 |
| L02 · P1 · B+C | **800px 及以下无法登出，也看不到当前管理员。** 七个页面均已复现。 | [styles.css](../web/src/styles.css) 移动断点隐藏 `.sidebar-bottom`，登出只存在于该区域。 | 在移动顶部保留账号菜单或独立登出按钮，复用现有 mutation/error 反馈。验收：320、390、800px 可触控和键盘登出；失败可重试；不增加第二套会话逻辑。 |
| L03 · P2 · B+建议 | **移动导航后部不可直接发现。** 390px 初始只看到前几项，审计/设置需横向滚动；没有明显的“还有导航”提示。 | [main](../web/src/main.tsx) 的七项导航与 [styles.css](../web/src/styles.css) 的横向 overflow；截图可见右侧项目被截断。 | 优先明确核心六项导航；若保留额外入口，使用渐隐/滚动指示或紧凑菜单，确保当前项可见。验收：首次用户能发现设置；键盘聚焦和直达链接后当前项不被隐藏。 |
| L04 · P2 · B+C | **移动状态条中时间挤压主要状态。** 概览状态被压成窄列，而“检查于…”占据同一行的大块空间。 | [styles.css](../web/src/styles.css)：`.notice small` 全局 nowrap、主要 span flex:1；小屏仅修改 small margin，未建立单列结构。 | 对含状态与时间的 notice 明确分区，小屏状态占整行、时间另起一行，内容区 min-width:0。验收：320/390px 下长错误、长域名、时间和重试按钮不会互相挤压。 |
| L05 · P2 · B+C | **移动设置 Diff 的标签和“之前/之后”被拆成零碎短行。** 示例“控制台”断成两行，“之前”也拆开；长域名和 JSON 会进一步恶化。 | [Deployments](../web/src/pages/Deployments.tsx) 与 [styles.css](../web/src/styles.css) 的 `.change-row` 始终横向 flex，没有小屏专用布局。 | 小屏改成“字段标题 → 之前 → 之后”的单列，内容 min-width:0，域名/JSON 可换行；桌面保留紧凑比较。验收：最长合法域名、IPv6、网段列表及较长备注仍可读。 |
| L06 · P2 · B+C | **部分卡片正文贴边，视觉间距不一致。** 告警说明/列表、趋势标题与底部、日志筛选、域名诊断等直接放在 card 下，缺少内容 padding。发布校验提醒也贴满边缘，与 inset 提示不一致。 | [TrafficWorkspace](../web/src/components/TrafficWorkspace.tsx)、[Traffic](../web/src/pages/Traffic.tsx)、[Deployments](../web/src/pages/Deployments.tsx)；`.card` 只有背景、边框和裁切，没有正文内边距。 | 增加统一 card-body 类并显式包裹正文；保留表格满宽，不对所有 card 直接加 padding。验收：桌面与移动标题/正文/筛选留白一致，表格和既有内边距不被重复叠加。 |
| L07 · P2 · B+建议 | **设置第一页同时展开几乎所有专家配置，普通操作成本偏高。** 网段、名称许可、观测、DNS、Token、导入和改密码连续展开；移动端很长。 | [Settings](../web/src/pages/Settings.tsx)：只有禁止目标是 details，其余高级设置常显。 | 默认显示域名、访问范围、当前策略摘要与保存操作；网络许可、观测、服务器 DNS 按需展开；访问放开警告始终可见，错误所在分区自动展开。验收：普通新增域名无需阅读全部专家字段，既有高级能力仍可访问。 |
| L08 · P2 · C | **设置保存反馈远离操作位置。** 域名设置保存后，成功提示出现在部署信息/Token 卡片之后，手机上可能完全不在视口内。 | [Settings](../web/src/pages/Settings.tsx) 的共享 message 在多个大 section 之后渲染。 | 每个操作区显示自己的状态，设置保存成功紧邻保存按钮并给“预览与发布”入口；异步结果用 role=status。验收：当前视口能看到结果，不需到页面中部找反馈，Token 与草稿反馈不混用。 |
| L09 · P2 · C | **证书标题对长域名缺少换行规则，存在被卡片裁切的风险。** `.certificate-card h2` 22px，未设置 overflow-wrap；card overflow:hidden。 | [Certificates](../web/src/pages/Certificates.tsx)、[styles.css](../web/src/styles.css)。基础短样例正常，最长合法内容未实测。 | 标题、SAN、日期/值区域设 min-width:0 和合适的换行；窄屏标签和值按需上下排列。验收：长基础域名、多 SAN、未知/异常长信息在 320px 无裁切。 |
| L10 · P2 · C | **触控尺寸规则只保证高度，部分图标入口宽度不足。** 弹窗关闭按钮 padding 5px + 20px 图标约 30px 宽；help-trigger 只在 <=700px 扩为 44px，701–800px 仍为 28px。 | [dialog](../web/src/components/ui/dialog.tsx)、[HelpTooltip](../web/src/components/HelpTooltip.tsx)、[styles.css](../web/src/styles.css)。 | 小屏交互目标同时设 min-width/min-height:44px，统一断点；不放大图标本身。验收：关闭、帮助、行操作可触控，键盘焦点明显且不被 card overflow 裁切。 |

## 文案、产品边界与信息一致性

| 编号 / 优先级 / 证据 | 问题与位置 | 修复方案与验收条件 |
| --- | --- | --- |
| C01 · P2 · C | **当前导航和功能与本次 AGENTS 范围冲突。** [main](../web/src/main.tsx)、[Settings](../web/src/pages/Settings.tsx)、[TrafficWorkspace](../web/src/components/TrafficWorkspace.tsx) 已提供流量趋势、后台 TCP、告警等；本次约束固定六项核心导航，并将流量图表和主动健康检查排除在未明确要求的范围之外。部分文档又称这些未提供。 | 先核对这些现存功能是否来自已授权需求，统一当前版本范围。本次不擅自删除已有功能或用户改动，也不进一步扩展观测。若以本次约束为准，核心导航及设置默认区须回到六项流程，额外功能另作明确范围决定。 |
| C02 · P2 · C | **UI 规范明显滞后。** [web/UI.md](../web/UI.md) 仍写五步 Setup、来源推断网段、默认不放宽策略、无 Public 的 Homelab 限制、“修改导入设置”等，当前普通 Setup 是三步且默认开放，导入模型也是多域名。 | 以现行代码、测试和本次约束重写对应段落，删除未实现的入口描述；同步使用/Setup/流程文档。验收：每个步骤、默认值和按钮名可在当前页面找到，规划不冒充已实现。 |
| C03 · P2 · B+C | **`make web` 演示与静态 demo 的真实性文案不一致。** mock 模式的发布写“真实配置校验通过／已成功上线”，证书说明写“来自真实 TLS 握手”，但实际 API 是模拟的。 | [request](../web/src/request.ts) 的 isStaticDemo 仅识别 mode=demo，[vite.config](../web/vite.config.ts) 的 mode=mock 也使用模拟 API。统一“是否模拟”的展示标记，网络行为仍保持各自边界；mock/demo 均标记模拟校验、模拟上线、模拟 TLS，并展示演示提示。验收：两种演示均不会被误当真实部署验收，production 不显示模拟状态。 |
| C04 · P3 · C+建议 | **审计主要操作名和结果直接显示机器值。** `service.put`、`deployment.success` 等对普通用户不直观。 | [Audit](../web/src/pages/Audit.tsx) 增加已有操作/结果的中文映射，原始代码在详情保留；未知值安全回退。验收：可识别新增、编辑、启停、删除、校验、发布、回滚等操作，无敏感值回显。 |
| C05 · P2 · C | **外部模式概览仍写 `Admin API · Unix Socket`。** | [Overview](../web/src/pages/Overview.tsx) 根据 external_caddy 展示内置私有 Socket / 外部 Admin API；不展示带凭据 URL。验收：内外两种模式说明正确。 |
| C06 · P3 · C | **多域名证书页标题仍说“一张通配符证书覆盖所有受管服务”。** | [Certificates](../web/src/pages/Certificates.tsx) 改为“查看各登记域名的通配符证书与 TLS 探测结果”，保留一层子域边界。验收：一个或多个域名的说明都成立。 |
| C07 · P3 · C+建议 | **固定 Homelab“工作空间”与侧栏箭头容易暗示可切换；页脚版本硬编码。** | [main](../web/src/main.tsx) 去掉不可操作的箭头/重复空间层级，使用单实例措辞；若显示版本则复用 Manager 版本数据，避免与部署信息冲突。验收：不增加多实例或空间功能，版本信息只有可信来源。 |

## 性能与验证缺口

| 编号 / 优先级 / 证据 | 问题与位置 | 修复方案与验收条件 |
| --- | --- | --- |
| V01 · P2 · C | **现有 Vitest 通过仍未覆盖本次视觉与关键竞态。** jsdom 不做真实布局；F01、F03、F04、L01、L02 已在现有测试全部通过时复现。 | 将保存中输入、发布终态、首次读取失败、草稿 Origin 和失效缓存加入 Vitest。增加少量可复现的浏览器检查入口或人工检查脚本，覆盖核心断点、图标尺寸、登出和弹窗；不把演示测试称为真实 Caddy 验收。 |
| V02 · P3 · 构建证据+建议 | **所有页面进入单一较大的启动包。** [main](../web/src/main.tsx) 静态导入全部页面，构建主包 624.98 kB，gzip 199.76 kB。未测慢网性能，不能据此断言加载不可用。 | 使用现有 Router/React 的路由懒加载，优先拆分 Setup、设置及非核心页面；提供明确加载与加载失败重试。验收：首屏下载减少，直达路由正常，拆包失败可恢复，不引入额外库。 |
| V03 · P3 · C+建议 | **发布页每秒重渲染整个页面；历史持续三秒轮询。** 全局 EntryMigration 还持续轮询 overview/settings。 | [Deployments](../web/src/pages/Deployments.tsx) 将到期计时缩到小组件，或在有效期临近/到期时更新；任务进行中高频轮询，终态降低频率。核对 Query 去重后再优化，避免重复客户端读状态。验收：后台任务仍可恢复、15 分钟到期准确，无任务时减少请求和整页渲染。 |

## 建议的修复顺序

1. **状态正确与输入保留。** 先做 F01、F02、F03、F04、L02；用延迟、503、草稿/生效配置差异和任务终态复现作验收。优先复用现有 Query 与业务规则，保持服务端鉴权、串行发布、幂等和不确定状态保护。
2. **核心流程衔接。** 做 F05–F08、F10–F12、F14–F16；逐项覆盖保存草稿、配置导入、DNS、交接、发布与回滚。需要 API 分页元信息/发布详情路由的改动单独实施，避免与纯样式修复混在一起。
3. **统一布局。** 先处理 L01、L04–L06、L09–L10，再处理导航、设置分区和反馈位置。仅收窄 CSS 作用域、补齐正文间距与响应式，不另建主题或换技术栈。
4. **文档与后续优化。** 先澄清 C01，再同步 C02–C07；最后按收益处理 F09、F13、V02–V03。每项修复都同步实际行为，不把本报告建议写成已实现。

## 修复后的最低验收矩阵

| 场景 | 应验证的结果 |
| --- | --- |
| 320 / 390 / 800 / 1024 / 1440px | 核心导航可发现、登出可用、当前页可识别、按钮触控尺寸足够；局部表格滚动可发现，主页面不溢出。 |
| 长域名、IPv6、多 SAN、长备注、JSON Diff | 内容可读且完整，标签不被挤碎；弹窗滚动和焦点不裁切。 |
| 保存延迟、503、断网、修订冲突 | 输入保留；不会显示无数据/当前健康的错误结论；读取最新状态后才能明确继续。 |
| 设置/域名草稿与线上不同 | 正式入口、证书与线上访问范围按 active_config 展示；草稿明确待发布。 |
| applying / success / failed / uncertain | 任务卡、预览、校验和按钮一致；成功刷新 Diff；响应丢失核对任务；uncertain 阻止新发布。 |
| 初始化普通/导入/外部/预配置 | 三步主流程、导入策略保留、外部不回退、交接不替代设备核对；不保存浏览器凭据。 |
| 设置导入后立即编辑、日志换时间与分页 | 共享修订及时更新；分页回到有效位置；查询范围清晰。 |
| 键盘与读屏 | 表单标签、弹窗焦点、错误 role=alert、结果 role=status、帮助和离开确认均可操作。 |

本次未执行真实 Caddy 集成、容器发布/恢复、真实 DNS 与证书签发，也未验证 Safari、Android/iOS 软键盘或辅助技术。相应行为修复仍应按 [verification.md](verification.md) 选择后端、集成或隔离部署检查。
