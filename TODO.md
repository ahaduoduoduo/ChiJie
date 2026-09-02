# Chijie 开发计划

## P1 — 参数驱动代理网关后端 ✅
日期：2026-05-04

- [x] `POST /proxy` 使用 `url`、`method`、`headers`、`payload` 和 `egress` 作为请求协议
- [x] `egress.region` 为空时使用直连出口，并允许继续应用 TLS 指纹
- [x] `egress.region` 非空时按二字母地区码选择出口
- [x] `egress.strategy` 支持 `random`、`round-robin`、`least-latency`
- [x] `egress.residential` 区分普通地区组和家宽地区组
- [x] `egress.tls_fingerprint` 支持命名指纹、预设指纹、JA3 和配置字符串
- [x] 删除后端规则引擎和规则配置文件
- [x] 删除后端规则 Admin API
- [x] 删除旧节点选择器接口，统一使用 `pool.SelectEgress`
- [x] HTTP 请求和 WebSocket 隧道共用同一套出口选择逻辑

## P2 — 节点池与地区组 ✅
日期：2026-05-04

- [x] `nodes.yaml` 作为唯一出口配置文件
- [x] 静态节点支持 `region`、`residential`、`enabled`、`tags`
- [x] 订阅节点支持自动地区识别
- [x] 订阅节点支持 `node_regions` 手动修正地区
- [x] 订阅节点支持 `node_aliases` 和 `node_tags`
- [x] 订阅节点支持 `reject_regex` 屏蔽无效节点名
- [x] 地区组返回 `US`、`HK`、`JP` 等普通组
- [x] 家宽地区组返回 `US-RES`、`HK-RES`、`JP-RES`
- [x] 普通模板节点默认支持任意二字母地区码
- [x] 家宽模板节点通过 `residential: true` 单独声明
- [x] 普通模板不服务家宽请求，家宽模板不服务普通请求
- [x] 静态节点和订阅节点优先，模板节点作为同类型兜底
- [x] 健康检查结果参与可用性判断和最低延迟策略

## P3 — Admin API ✅
日期：2026-05-04

- [x] `POST /api/auth/login` 密码登录
- [x] `GET /api/nodes` 返回节点池、节点状态和地区组状态
- [x] `POST /api/nodes` 添加节点池
- [x] `PUT /api/nodes/pool` 更新节点池
- [x] `DELETE /api/nodes/pool?name=xxx` 删除节点池
- [x] `PUT /api/nodes/node` 更新 static 节点
- [x] `DELETE /api/nodes/node?pool=xxx&node=xxx` 删除 static 节点
- [x] `PUT /api/nodes/subscription/node` 更新订阅节点元数据
- [x] `POST /api/nodes/refresh?pool=xxx` 刷新订阅池
- [x] `POST /api/nodes/test` 测试节点连通性
- [x] `POST /api/nodes/enabled` 启用或禁用节点并写回 `nodes.yaml`
- [x] `GET /api/fingerprints` 查看 TLS 指纹
- [x] `POST /api/fingerprints` 添加 TLS 指纹
- [x] `DELETE /api/fingerprints/:name` 删除 TLS 指纹
- [x] `POST /api/fingerprints/test` 测试 TLS 指纹配置
- [x] `GET /api/traffic?limit=100` 查看请求记录和聚合指标
- [x] `POST /api/reload` 重载节点池和 TLS 指纹配置
- [x] `GET /api/stats` 查看运行统计

## P4 — 文档 ✅
日期：2026-05-04

- [x] 新增 `docs/parameter-driven-egress.md`
- [x] 重写 `README.md`，以参数驱动出口模型为当前项目说明
- [x] 重写 `DETAILS.md`，移除已删除后端模块说明
- [x] 重写 `docs/subscription-routing.md`，说明订阅节点、地区组和模板节点
- [x] 删除已过期的规则模式文档

## P5 — 前端重构 ✅
日期：2026-05-04

- [x] 新增 `docs/admin-frontend.md`，说明 Admin 前端接入、构建方式和接口边界
- [x] 移除历史规则编辑页面
- [x] 移除前端对 `/api/rules` 的调用
- [x] 重做 Overview：运行状态、可用地区组、模板可用性、近期错误
- [x] 重做 Egress：地区组、普通节点、家宽节点、静态节点、订阅节点
- [x] 新增 Templates：普通模板和家宽模板管理
- [x] 重做 Subscriptions：订阅源、刷新、屏蔽正则、地区修正、节点别名、节点标签
- [x] 保留 TLS Profiles：预设和自定义 TLS 指纹管理
- [x] 重做 Traffic：展示地区组、策略、出口池、出口节点、模板标识和错误信息
- [x] 重新构建并复制前端产物到 `internal/admin/dist`

## P6 — 后续验证 ⏳
日期：2026-05-04

- [x] `go test ./...`
- [ ] 使用真实 SOCKS5 / HTTP Proxy 节点验证 `POST /proxy`
- [ ] 使用普通模板节点验证冷门地区出口
- [ ] 使用家宽模板节点验证 `residential=true`
- [ ] 使用真实订阅源验证地区组生成和订阅刷新
- [ ] 使用真实 JA3 和命名指纹验证 TLS 指纹行为

## P7 — TLS 指纹测试与 extra_fp 兼容 ✅
日期：2026-05-05

- [x] `POST /api/fingerprints/test` 改为对 HTTPS 目标发起真实 TLS 握手测试
- [x] 测试结果返回 HTTP 状态、耗时和检测端 JSON 摘要
- [x] 支持 curl_cffi 风格 `extra_fp.tls_signature_algorithms`
- [x] 支持 curl_cffi 风格 `extra_fp.tls_cert_compression`
- [x] 支持 curl_cffi 风格 `extra_fp.tls_grease`
- [x] 前端 Add TLS profile 支持 JSON/YAML 配置输入
- [x] uTLS 请求包装复用当前出口的 `DialContext`
- [x] 支持 JA4/Akamai raw 导入；测试结果只展示远端真实观测字段

## P8 — 模板测试诊断增强 ✅
日期：2026-05-05

- [x] 模板地区测试支持传入自定义测试 URL
- [x] 模板测试结果返回探测阶段：代理 TCP、代理 CONNECT、TLS 握手或目标 HTTP
- [x] 模板测试结果返回目标 HTTP 状态码
- [x] 前端模板测试支持切换 `api.ipify.org`、`httpbin.org/ip`、Google `generate_204` 和自定义 URL
- [x] 前端对代理 CONNECT 阶段 403 给出明确含义说明
- [x] 模板测试结果显示出口 IP、请求地区码和实际国家码
- [x] 节点和模板测试结果返回 IP 类型、ASN、ISP、组织和代理/VPN/hosting 标记

## P9 — HTTP/2 指纹请求层 ✅
日期：2026-05-05

- [x] TLS Profiles 支持检测 JSON 导入，提取 `tls.ja3`、`tls.ja4_r` 和 `http2.akamai_fingerprint`
- [x] 保存 `http_version`、`method`、`user_agent`、HTTP/2 SETTINGS、WINDOW_UPDATE、HEADERS 和 priority 明细
- [x] `POST /api/fingerprints/test` 支持通过 uTLS 协商 `h2` 并写入 Akamai HTTP/2 raw
- [x] `POST /proxy` 在指纹显式要求 HTTP/2 时使用 HTTP/2 指纹 transport
- [x] TLS 测试结果显示实际响应协议、出口 IP、地区码、远端 JA3/JA4/Akamai 观测值
- [x] 使用 `https://tls.browserleaks.com/json` 验证 HTTP/2 探测返回 `HTTP/2.0` 和 Akamai raw

## P10 — Admin 移动端布局与路由修复 ✅
日期：2026-05-05

- [x] 登录成功后将地址栏从 `/login` 替换为当前控制台页面路径
- [x] 控制台导航支持浏览器返回按钮和页面路径同步
- [x] Egress 节点 More 按钮打开节点详情抽屉
- [x] 移动端改为顶部横向导航、单列页面头、可横向滚动数据表、全宽抽屉和自适应弹窗
- [x] 验证 390px 移动视口下主页面无 body 横向溢出

## P11 — 调用鉴权与地区无关出口 ✅
日期：2026-05-05

- [x] `/proxy` 支持 `Authorization: Bearer <proxy_token>` 鉴权
- [x] 移除旧版分钟 MD5 鉴权配置和实现
- [x] `/tunnel` 支持首帧 JSON 携带 `authorization`
- [x] `egress.any=true` 支持选择任意非直连静态/订阅出口
- [x] `egress.max_latency_ms` 支持任意地区出口延迟阈值
- [x] `region` 支持 `*`、`ANY`、`AUTO` 作为地区无关出口别名

## P12 — Proxy token 与节点元数据编辑 ✅
日期：2026-05-05

- [x] 新增 `/api/auth/proxy-token`，生成只用于代理调用的 Bearer token
- [x] System 页面新增 Proxy token 生成与复制入口
- [x] Egress 节点详情抽屉接入 metadata 编辑
- [x] 订阅节点 metadata 支持按 `server:port` 保存地区、别名和标签
- [x] 静态节点 metadata 支持改名、地区、家宽标识和标签

## P14 — 代码质量与可维护性 ✅
日期：2026-05-05

- [x] 抽取 `internal/util/strings.go` 公共工具：`FirstNonEmpty` / `ContainsString` / `RemoveString` / `ParseInt` / `SplitList`
- [x] 删除 `dialer/singbox.go`、`pool/manager.go`、`pool/subscription.go`、`admin/api.go`、`fingerprint/http2_transport.go` 中的同名重复定义
- [x] 抽取 `internal/auth/claims.go`：admin 和 server 共享同一份 JWT Claims 结构
- [x] 抽取 `internal/admin/yaml_io.go`：`loadYAML` / `atomicWriteYAML` 统一 admin handler 的 YAML 读写
- [x] 合并 `dialer.go` 中 5 个 sing-box 协议的 case
- [x] `tunnel.go` 双向转发改为等待两个 goroutine 都退出，关闭对端连接传播退出信号
- [x] `/proxy` 的 `http.NewRequest` 改 `http.NewRequestWithContext`，客户端断开时级联取消上游请求
- [x] admin handler 不再手动构造 map，直接 marshal `NodeTestResult` / `TemplateTestResult` 结构体
- [x] `NodeTestResult` 与 `TemplateTestResult` 提取共有字段到 `connectivityCommon` 嵌入结构（JSON 输出保持字段平铺，行为不变）
- [x] 地区映射改为单一权威表 `internal/pool/regions.go`：合并 `defaultRegionGroupNames` 与 `regionNameAliases`，扩充至 80+ 常见地区代码（覆盖大中华 / 东南亚 / 南亚 / 北美 / 拉美 / 欧洲全境 / 中东 / 大洋洲 / 非洲）
- [x] 实现 `log.level` 分级日志：新增 `internal/util/logger.go` 的 `Debugf/Infof/Warnf/Errorf`，迁移 `/proxy`、`/tunnel` 等高频日志到 Debug 级
- [x] 新增 `util/strings_test.go`、`util/logger_test.go` 单元测试
- [x] 全量 `go vet` 与 `go test` 通过

## P13 — 安全加固 ✅
日期：2026-05-05

- [x] 配置文件治理：`configs/*.yaml` 加入 `.gitignore`，仓库提供 `*.example` 模板
- [x] 启动时校验 `admin.jwt_secret`：非空、长度 ≥ 16、非占位字符串
- [x] 启动时校验 `admin.password`：为空时强制 listen 为 127.0.0.1/localhost
- [x] 新增 `internal/netguard`：私网/回环/CGNAT/保留段 IP 黑名单（IPv4 + IPv6）
- [x] `/proxy` 加 SSRF 防护：URL scheme 校验 + 目标 host 解析后黑名单校验
- [x] `/tunnel` 加 SSRF 防护：首帧 URL 校验 + 拨号阶段二次校验
- [x] 直连出口包装 `guardedDialer`：DialContext 时再次校验防 DNS rebinding
- [x] `gateway.yaml` 新增 `server.allow_private_targets`（默认 false）
- [x] `/api/fingerprints/test` 加目标 host 黑名单校验
- [x] 登录端点 `/api/auth/login` 加按 IP 速率限制（5 次失败 / 60s 窗口 / 5 分钟锁定）
- [x] 密码比较改用 `crypto/subtle.ConstantTimeCompare`
- [x] WebSocket `/tunnel` 加 Origin 校验（无 Origin 放行 + 同源放行 + 跨站拒绝）
- [x] `/proxy` body 大小限制 10 MB；Admin API JSON body 限制 1 MB
- [x] CORS：`/proxy` 仅在同源 Origin 时回显 ACAO
- [x] 配置文件写入权限改为 0600
- [x] 优雅关闭：信号驱动 → server.Shutdown → admin.Shutdown → healthChecker.Stop → StopSubscriptionUpdater
- [x] 修复订阅自动更新协程在 reload 后泄漏的问题（StopSubscriptionUpdater + ctx 取消）

## P15 — 安全修复与未完成项完成 ✅
日期：2026-05-06

- [x] `go.mod` 升级到 `go 1.25.9`，`govulncheck ./...` 不再报告已调用的 Go 标准库漏洞
- [x] `/proxy` 上游响应 body 加 32 MB 上限，普通 HTTP transport 和 HTTP/2 指纹 transport 均生效
- [x] Admin 登录限速的客户端 IP 提取优先使用 `CF-Connecting-IP` / `True-Client-IP`，并跳过无效 header 值
- [x] `/tunnel` 对 `ws://` / `wss://` 目标执行真实上游 WebSocket 握手，首帧 `headers` 与 `payload` 已参与上游请求
- [x] `/tunnel` 的 `wss://` 上游握手可通过当前出口应用请求级 TLS 指纹
- [x] 订阅拉取限制为 `http` / `https`，拒绝私网目标，并限制单次响应 body 为 4 MB
- [x] 后台健康检查读取每个池的 `health_check` 配置，状态写入使用池锁
- [x] System 页面接入运行时信息、日志级别保存和配置导出
- [x] Egress 新增静态节点表单写入 `username` / `password`
- [x] 清理静态检查命中的冗余 `CanonicalHeaderKey` 和已废弃 `UtlsPreSharedKeyExtension.OmitEmptyPsk` 用法
- [x] `go test ./...`、`go test -race ./...`、`go vet ./...`、`staticcheck ./...` 通过

## P16 — Docker 部署 ✅
日期：2026-05-06

- [x] 新增多阶段 `Dockerfile`：前端构建、Go 构建、运行镜像分离
- [x] 新增 `.dockerignore`，排除本地二进制、构建产物和 `configs/*.yaml`
- [x] 新增 `docker-compose.yml`，Proxy API 和 Admin 默认只绑定宿主机本机地址
- [x] Compose 宿主机端口和配置目录改为 `.env` 可配置
- [x] 新增 `docker-compose.amd64.yml`，支持 ARM 机器构建 X86 镜像
- [x] 新增 `configs/gateway.docker.yaml.example`
- [x] 新增 `docs/docker-deployment.md`

## P17 — Docker Hub 自动发布 ✅
日期：2026-05-07

- [x] 新增 `.github/workflows/dockerhub.yml`，推送 `main` 后自动构建并发布 Docker Hub 镜像
- [x] GitHub Actions 使用 `DOCKERHUB_USERNAME` 和 `DOCKERHUB_TOKEN` Secrets 登录 Docker Hub
- [x] 镜像标签包含 `latest` 和当前 Git commit SHA
- [x] 新增 `docker-compose.prod.yml`，VPS 生产环境只拉取镜像运行
- [x] `.env.example` 新增 `CHIJIE_IMAGE`，默认指向 `ahaduoduoduo/chijie:latest`
- [x] Docker 默认宿主机端口改为 `127.0.0.1:18080` 和 `127.0.0.1:19090`，便于 nginx 反向代理
- [x] 新增 `docs/dockerhub-release.md`，说明 Docker Hub 仓库、GitHub Secrets 和 VPS 更新命令

## P18 — 订阅源编辑补齐 ✅
日期：2026-05-07

- [x] Subscriptions 编辑抽屉新增 Source 页签，支持修改订阅 URL、刷新间隔和家宽标识
- [x] Subscriptions Source 页签支持修改订阅池展示名称
- [x] Subscriptions 编辑抽屉新增删除订阅池入口
- [x] 保存或删除节点池后重启订阅自动刷新协程，避免旧 interval 或已删除订阅继续执行
- [x] 订阅拉取使用指定 Chrome User-Agent，并保留 fetch 底层错误原因且不暴露 URL query token
- [x] 前端地区名称和旗帜映射补齐到后端地区表；英国 `UK` / `GB` 展示统一使用 `GB` 标准码
- [x] 新增 `configs/fingerprints.yaml.example`，Admin reload 和指纹新增在缺少 `fingerprints.yaml` 时不再报错

## P19 — Proxy API 调用文档 ✅
日期：2026-05-07

- [x] 新增 `docs/proxy-client-usage.md`，面向外部服务、脚本、Cloudflare Workers 和 AI Agent 说明 Proxy API 调用方式
- [x] 文档覆盖 `/health`、`/proxy`、`/tunnel`、Bearer 鉴权、出口参数、错误响应、限制和最小 AI Agent 调用说明
- [x] `README.md` 和 `DETAILS.md` 补充 Proxy API 使用文档入口

## P20 — Subscriptions 窄屏布局修正 ✅
日期：2026-05-07

- [x] Subscriptions 订阅卡片改用专用响应式 class，避免移动端继承通用卡片横向滚动
- [x] 窄屏下订阅摘要从四列改为单列，订阅 URL 使用自动换行，不再撑宽卡片
- [x] 订阅卡片头部操作区在窄屏下分行展示，刷新时间、Refresh、Edit 和启停开关不再挤压
- [x] 节点列表仅在自身条带内横向滚动，卡片主体保持视口宽度内排版

## P21 — Overview 近期请求列表修正 ✅
日期：2026-05-07

- [x] Overview `Recent errors` 的错误筛选改为与 Traffic 页一致，覆盖 `status >= 400`、`status == 0` 和有错误 detail 的请求
- [x] Overview `Recent errors` 在目标站点返回 4xx 且无后端错误 detail 时显示目标 URL
- [x] Overview 新增 `Recent success`，展示最近 5 条成功请求

## P22 — 出口执行失败自动换节点重试 ✅
日期：2026-05-09

- [x] `pool.Manager` 新增按策略排序的出口候选列表，`least-latency` 按延迟升序，`round-robin` 从当前轮询节点开始，`random` 返回随机顺序
- [x] `/proxy` 在非直连出口建立连接或等待响应头失败时使用下一个候选出口，后续由 P35 扩展为可配置次数
- [x] 源站已经返回 HTTP 状态码时不重试，避免把真实 `403` / `404` / `502` 误判为代理失败
- [x] 补充出口候选排序和代理失败重试的单元测试

## P23 — Overview 请求详情抽屉补齐 ✅
日期：2026-05-09

- [x] Overview `Recent success` 和 `Recent errors` 行点击打开请求详情抽屉
- [x] Traffic 和 Overview 共用同一个请求详情组件，避免两处详情展示不一致
- [x] 请求详情抽屉新增 UTC+8 请求时间

## P24 — Shadowsocks SIP002 订阅兼容 ✅
日期：2026-05-10

- [x] 兼容 Base64 包装的 CRLF 多行 URI 订阅
- [x] 兼容 Shadowsocks `ss://userinfo@host:port/?plugin=...#name` 中 `host:port` 后的空路径 `/`
- [x] 补充脱敏单元测试，覆盖 simple-obfs plugin 参数解析

## P25 — Shadowsocks simple-obfs 插件名兼容 ✅
日期：2026-05-10

- [x] URI 订阅中的 `plugin=simple-obfs` 规范化为 `obfs-local`
- [x] Clash YAML 订阅中的 `plugin: simple-obfs` 规范化为 `obfs-local`
- [x] 补充 URI 和 Clash YAML 两种入口的单元测试

## P26 — 远端 Chijie 模板与优先级 fallback ✅
日期：2026-05-10

- [x] 新增 `template_type: chijie`，只允许 HTTPS endpoint，并使用远端 Proxy token 转发请求
- [x] 模板候选按 `priority` 降序尝试，支持 Chijie、BrightData、Lumi 等多模板顺序 fallback
- [x] Chijie 模板转发时保持原 `/proxy` body，只替换远端 Bearer
- [x] 新增 `X-Chijie-Hop` 循环保护和 `X-Chijie-Error` 网关错误识别
- [x] Admin Templates 支持 Provider、Priority、Coverage、Chijie endpoint 和 Bearer 配置
- [x] 补充后端选择、远端转发、HTTPS 校验和 fallback 行为测试

## P27 — Chijie 模板测试复用 Test region ✅
日期：2026-05-11

- [x] `/api/nodes/template/test` 支持 `template_type: chijie`
- [x] Chijie 模板测试通过远端 `/proxy` 请求 `https://api.ipify.org?format=json`
- [x] 测试请求固定使用 `least-latency` 和界面输入的地区码
- [x] 前端 Templates 页面复用原 `Test region` 按钮和结果面板
- [x] 补充远端 Chijie 成功和网关错误测试

## P28 — 前端地区旗帜自动生成 ✅
日期：2026-05-11

- [x] Region pill 对任意 ISO-2 地区码自动生成 flag emoji
- [x] `UK` 兼容显示为 `GB` 旗帜
- [x] 保留 `UN` 的兜底白旗显示

## P29 — 普通地区出口降级到同地区家宽 ✅
日期：2026-05-12

- [x] `residential=false` 请求在普通节点和普通模板均不可用时，尝试同地区家宽节点
- [x] 同地区家宽节点不可用时，继续尝试家宽模板
- [x] Traffic 记录实际使用的 `*-RES` 组和 `residential=true`
- [x] 远端 Chijie 模板 fallback 时同步修正转发请求的 `egress.residential`

## P30 — Traffic Request log 加载更多 ✅
日期：2026-05-12

- [x] Traffic 页面支持按 200 条递增加载更多请求日志
- [x] 最高展示后端内存窗口中的最近 1000 条
- [x] 保持后端内存存储模型不变

## P31 — Traffic 请求详情抽屉溢出修正 ✅
日期：2026-06-03

- [x] 请求详情抽屉限制自身和内容区横向宽度，避免长文本撑开抽屉
- [x] 两列详情布局右侧内容列使用 `minmax(0, 1fr)` 和 `min-width: 0`
- [x] URL、Node、Error 和 replay payload 使用断行策略，错误请求不再因连续长字符串溢出

## P32 — AnyTLS / TUIC 订阅导入兼容 ✅
日期：2026-06-14

- [x] Base64 TXT 订阅入口复用弹性 Base64 解码，支持 URL-safe 和无 padding 编码
- [x] URI 订阅新增 `anytls://` 和 `tuic://` 解析
- [x] 出口拨号新增 sing-box AnyTLS 和 TUIC outbound
- [x] 补充订阅解析和拨号器单元测试

## P33 — 健康检查与订阅刷新策略 ✅
日期：2026-06-18

- [x] System 页面支持配置全局健康检查 `interval`、`timeout`、`url`、`max_fail`
- [x] 订阅刷新周期支持手动更新、小时输入和天输入
- [x] 订阅刷新或配置重载失败时保留上一次成功拉取的运行时节点
- [x] 订阅池新增 `try_offline`，唯一离线地区节点可在模板前再尝试一次

## P34 — Admin 站点图标资源 ✅
日期：2026-06-22

- [x] 基于现有 `.brand-mark` 视觉导出透明圆角 favicon、Apple Touch 和 PNG 图标
- [x] `web/index.html` 增加 favicon、Apple Touch 和 PNG 图标引用
- [x] `web/scripts/build.mjs` 构建时复制站点图标到 `web/dist`

## P35 — Proxy 重试与管理页稳定性 ✅
日期：2026-06-25

- [x] `/proxy` 默认最多尝试 5 个可用静态/订阅节点，并支持 System 页面配置
- [x] `/proxy` 出口执行失败后立即将对应静态/订阅节点标记为 `Alive=false`
- [x] 显式地区请求支持可用节点失败后继续尝试同地区同类型模板节点
- [x] `/api/nodes` 和 `/api/fingerprints` 返回固定顺序，避免自动刷新后列表跳动
- [x] Subscriptions 节点条带的 `+N` 改为可展开按钮

## P36 — Docker 远程构建修复 ✅
日期：2026-06-25

- [x] `Dockerfile` 的 `web-builder` 阶段复制 favicon 和 PNG 图标，匹配 `web/scripts/build.mjs` 的构建输入

## P37 — Shadowrocket Hysteria2 订阅兼容 ✅
日期：2026-06-25

- [x] `hysteria2` 订阅节点支持 `mport=16001-17000` 端口范围写法，并转换为 sing-box 接受的 `16001:17000`
- [x] 补充 Hysteria2 端口跳跃范围的 dialer 单元测试

## P38 — 订阅解析失败原因可见性 ✅
日期：2026-06-25

- [x] Clash YAML 识别支持 `mixed-port`、`dns` 等头部配置后再出现 `proxies:` 的订阅内容
- [x] 订阅解析成功但节点因不支持的传输协议被跳过时，在订阅池状态中展示跳过原因
- [x] 标记 `xhttp` / `splithttp` 为当前不支持的 V2Ray transport，避免订阅页只显示 0 且没有原因

## P39 — 订阅拉取 User-Agent 调整 ✅
日期：2026-06-25

- [x] 订阅拉取 UA 从 Chrome 类 UA 改为 `clash-verge/v2.0.0`，避免部分订阅源只返回精简节点集合
- [x] 对比 `clash-verge/v2.0.0`、Shadowrocket 和 Chrome UA 在同一订阅链接下的节点数量差异

## P40 — Clash 订阅兼容性修复 ✅
日期：2026-06-25

- [x] Clash YAML 的 `port: "443"` 字符串端口支持解析为整数端口
- [x] Clash YAML 的 `fingerprint` 与 `client-fingerprint` 分离处理，证书 SHA-256 pinning 指纹不再误传给 uTLS
- [x] 订阅中的未知 uTLS 指纹值不再导致节点被跳过
- [x] 补充字符串端口和未知 uTLS 指纹的单元测试

## P41 — VLESS XHTTP 订阅兼容 ✅
日期：2026-06-25

- [x] 新增 `vless+xhttp` 专用拨号器，支持 `packet-up` 和 TLS `stream-up`
- [x] Clash YAML 的 `xhttp-opts` 支持解析 `path`、`mode` 和 `download-settings`
- [x] VLESS URI 支持读取 `mode`、`path` 和 `extra` 中的 XHTTP 参数

## P42 — 内置 DNS 解析兜底 ✅
日期：2026-06-25

- [x] 出口节点拨号、订阅拉取校验和 sing-box DNSRouter 使用内置公共 DNS resolver
- [x] 内置 DNS 服务器使用 `1.1.1.1:53` 和 `8.8.8.8:53`，避免 Docker `127.0.0.11` 解析失败影响节点

## P43 — Proxy 超时配置 ✅
日期：2026-06-26

- [x] `/proxy` 单个出口等待目标响应头超时改为 `proxy.response_header_timeout` 配置，默认 `3s`
- [x] `/proxy` 单个出口完整请求总超时改为 `proxy.total_timeout` 配置，默认 `30s`
- [x] System 页面支持查看和保存 Proxy response header timeout 与 total timeout
- [x] `/api/system/proxy` 持久化响应头等待超时、完整请求总超时、重试次数和模板兜底设置

## P44 — Proxy token 文档澄清 ✅
日期：2026-06-27

- [x] README 明确 Proxy token 是无状态 JWT，只在生成时显示一次，后台不保存历史 token 与到期时间
- [x] `docs/proxy-client-usage.md` 补充生产环境保存 token 与失效方式说明

## P45 — Proxy Cookie 响应头透出 ✅
日期：2026-06-28

- [x] `/proxy` 将目标站点返回的多个 `Set-Cookie` 响应头逐条写入网关响应
- [x] 远端 Chijie 模板响应保留 `Set-Cookie`，支持多级转发场景
- [x] README、DETAILS 和 `docs/proxy-client-usage.md` 补充 Cookie 请求与响应边界说明

## P46 — Traffic 失败合并统计 ✅
日期：2026-06-29

- [x] Traffic 指标区分 raw 请求和有效请求，重复失败按 `kind + url/target + egress_group` 合并为一次错误
- [x] 延迟指标只统计最终成功的请求，避免目标站点超时失败污染平均延迟和 P95
- [x] `/api/traffic` 返回 `display_traces`，Admin Traffic 页面将可合并错误显示为一条并通过 `xN` 展开原始请求
- [x] README 和 DETAILS 补充 Traffic 统计口径说明

## P47 — Traffic 分钟级 P95 零值修复 ✅
日期：2026-06-30

- [x] 前端保留后端返回的 `bucket.p95_latency_ms = 0`，避免失败-only 分钟桶被全局 P95 覆盖

## P48 — Proxy 按请求跟随 Redirect ✅
日期：2026-07-01

- [x] `POST /proxy` 新增 `follow_redirects` 请求字段，默认保持不自动跟随
- [x] `follow_redirects=true` 时返回最终页面响应，并通过 `X-Chijie-*` 响应头返回最终 URL 与跳转明细
- [x] 新增 `proxy.max_redirects` 全局配置，默认 `5`，Admin System 页面支持保存
- [x] 每次跳转目标继续执行目标 URL 安全校验，达到最大跳转次数时返回最后一个 3xx 响应

## P49 — Traffic URL 参数忽略规则编辑 ✅
日期：2026-07-01

- [x] 新增 `traffic.failure_grouping.url_normalization.rules`，失败合并前可按 `host_pattern` / `path_pattern` 删除指定 query key
- [x] `/api/traffic/grouping-rules` 支持从 Admin 保存 URL 规范化规则并立即更新运行时 Traffic Store
- [x] 请求详情增加 `Ignore URL params` 编辑器，Host 和 Path 片段可切换为 `*`，Query 参数可勾选加入 `drop_keys`
- [x] README、DETAILS、`docs/traffic-url-grouping.md` 和示例配置补充规则说明

## P50 — 高端出口节点 ✅
日期：2026-07-01

- [x] 节点、订阅池和订阅节点标签支持 `premium` 高端标识
- [x] `/proxy` 和 `/tunnel` 支持 `egress.premium=true`，未指定地区时自动选择任意非直连出口并优先尝试高端节点
- [x] 地区组不按高端拆分，高端只作为节点标记和选择偏好
- [x] Admin Egress 和 Subscriptions 页面支持查看、筛选和编辑高端标识
- [x] README、DETAILS、`docs/premium-egress.md`、参数驱动文档和订阅路由文档补充高端出口说明

## P51 — 订阅池允许本地地址 ✅
日期：2026-07-26

- [x] subscription 节点池新增 `allow_private_host`，默认保持私网、回环、CGNAT 和保留地址拦截
- [x] 开启后支持直接填写本地 IP，以及使用解析到本地地址的域名
- [x] 初次拉取、手动刷新、自动刷新、HTTP 重定向和实际拨号使用同一池级设置
- [x] Admin 新增与编辑订阅界面增加 `Allow local addresses` 开关，并在订阅卡片显示 `local source`
- [x] 补充后端测试、示例配置、README、DETAILS 和订阅路由文档

## P52 — 订阅最近拉取状态 ✅
日期：2026-07-26

- [x] subscription 节点池记录最近一次拉取尝试完成时间和成功状态
- [x] 初次拉取、手动刷新、自动刷新和配置重载使用同一状态记录
- [x] `GET /api/nodes` 返回 `last_updated` 和 `last_refresh_failed`
- [x] Admin Subscriptions 页面显示相对时间，成功为灰色，失败为红色
- [x] 部分节点被跳过的警告与拉取失败使用独立判断
- [x] 补充后端测试、README、DETAILS 和订阅状态文档

## P53 — 运行数据持久化与成功请求折叠 ✅
日期：2026-09-02

- [x] 订阅成功结果使用 URL 哈希写入 `.runtime/subscriptions.json`，容器重启后首次拉取失败时恢复缓存节点
- [x] Traffic 原始 trace 按日写入 JSONL，默认保留 7 天，保留天数支持 Admin 和 YAML 配置
- [x] 启动时恢复最近 1000 条 Traffic trace，过期日期文件自动清理
- [x] 新增 Host + Path 的 200 折叠规则，命中请求保留日志但排除出有效统计
- [x] Traffic 请求详情支持创建成功折叠规则，列表展示 `xN` 折叠结果
- [x] 补充订阅缓存、Traffic 持久化、Docker 状态目录、配置示例和自动化测试

## P54 — VLESS XHTTP stream-one 与 auto ✅
日期：2026-09-02

- [x] 升级 `sing-xhttp`，接入 `stream-one` 双向流式传输
- [x] 支持 Xray `auto` 模式，Reality 节点自动使用 `stream-one`
- [x] 保留 VLESS XHTTP 初始化的具体失败原因，不再统一显示为整个 XHTTP 传输不支持
- [x] 保留 `downloadSettings` 解析结果但继续使用主地址下载，避免旧版可用节点因该字段被跳过
- [x] 补充 TLS、Reality、订阅池模式和底层 XHTTP 端到端测试

## P55 — Admin 移动端与禁用订阅行为修正 ✅
日期：2026-09-02

- [x] 请求日志保留天数设置从 Traffic 页面迁移到 System 的 Logging 卡片
- [x] Traffic 底部加载按钮在移动端保持单行固定高度
- [x] 禁用订阅在启动、配置重载、手动刷新和定时更新阶段不发起请求
- [x] viewport 禁止页面缩放，输入控件保持紧凑字号且不触发 iOS Safari 聚焦自动放大
