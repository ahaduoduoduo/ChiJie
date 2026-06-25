# Admin 前端接入说明

日期：2026-05-04

更新：2026-06-25

`web/` 目录是当前 Admin 控制台原型源码。页面使用 React UMD 与 Babel standalone，以静态文件方式运行；`npm run build` 不做打包压缩，只复制 `index.html` 和 `src/` 到 `web/dist`，再由根目录 `build.sh` 复制到 `internal/admin/dist` 供 Go `embed` 打包。

## 数据来源

- `POST /api/auth/login`：密码登录，返回 JWT。
- `POST /api/auth/proxy-token`：生成只用于代理调用的 Bearer token。
- `GET /api/nodes`：节点池、节点状态、地区组状态。
- `GET /api/fingerprints`：TLS 指纹列表。
- `GET /api/traffic?limit=200`：请求日志、分钟级序列和聚合指标；Traffic 页面可加载更多，最高展示内存窗口中的最近 1000 条。
- `GET /api/stats`：运行时长、节点池数量、指纹数量和流量指标。
- `PUT /api/system/logging`：修改日志级别，并写入 `gateway.yaml`。
- `GET / PUT /api/system/health-check`：查看或保存全局健康检查默认参数。
- `GET / PUT /api/system/proxy`：查看或保存 `/proxy` 重试次数和模板兜底设置。
- `GET /api/config/export`：导出当前配置目录里的 YAML 配置快照。
- `POST /api/nodes/template/test`：按模板池、地区和可选测试 URL 即时探测连通性。普通代理模板会生成临时节点；Chijie 模板会请求远端 `/proxy`，使用 `least-latency` 和指定地区访问测试 URL。返回阶段、HTTP 状态、出口 IP、实际国家码、IP 类型、ASN/ISP 信息和错误文本。
- `POST /api/fingerprints/test`：对 HTTPS 目标发起真实 TLS 指纹测试。

前端入口 `web/src/app.jsx` 启动后读取上述接口；如果接口返回 `401`，显示登录页。JWT 存储在 `localStorage`，后续请求使用 `Authorization: Bearer <token>`。

字段转换集中在 `web/src/api.jsx`：

- 后端 `PoolStatus.config` 会展开为页面所需的池字段。
- 后端节点名会转换成稳定 `id`：`<pool>:<node>`。
- Go duration 字符串会转换成毫秒数用于延迟展示。
- 后端 `traffic.Trace` 会转换成 Traffic 和 Overview 近期请求列表共用的 request 行。

## 已接入交互

- Egress：节点启停、节点状态测试、静态节点添加、静态节点删除、metadata 编辑、状态刷新；静态节点新增表单会提交用户名和密码字段。
- Subscriptions：订阅池新增、刷新、启停、节点地区修正、别名、标签、`reject_regex` 保存。
- Templates：模板池新增、编辑、启停、删除、优先级和覆盖范围配置；普通代理模板和 Chijie 模板均支持按地区真实探测，Chijie 模板测试会通过远端 Proxy API 请求 `https://api.ipify.org?format=json`。
- TLS Profiles：指纹新增、JSON/YAML 配置输入、删除、真实 HTTPS 指纹测试。
- Overview：最近成功和最近错误请求可打开与 Traffic 一致的请求详情抽屉，详情时间按 UTC+8 展示。
- Traffic：真实请求日志展示、详情抽屉、CSV 导出。
- System：运行统计展示、Proxy token 生成、配置重载、日志级别保存、健康检查默认值、Proxy 重试设置、当前配置导出。

## 接口边界

当前后端没有触发全量健康检查的 Admin API。Egress 页的刷新按钮读取当前后端状态，不发起新的健康检查任务；单节点和模板地区测试由各自测试接口执行即时探测。

## 布局边界

桌面端保持左侧固定导航和宽表格信息密度。移动端使用顶部横向导航，页面头、表单、统计和弹窗切换为单列；数据表保留列结构并在卡片内横向滚动，避免主页面横向溢出。
