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
- [x] `/proxy` 在非直连出口建立连接或等待响应头失败时最多重试一次，第二次使用下一个候选出口
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
