# Chijie

参数驱动的 HTTP-to-Proxy 网关，部署在 VPS 上，为 Cloudflare Workers 等调用方提供统一出口能力。

## 核心能力

- `POST /proxy`：调用方直接提交目标请求和出口参数，网关执行请求并返回目标响应
- `WS /tunnel`：通过首帧 JSON 声明目标和出口参数，支持 raw TCP 转发和上游 WebSocket 握手转发
- 出口协议：直连、SOCKS5、HTTP Proxy、Shadowsocks、VMess、VLESS、Trojan、Hysteria2
- 节点池管理：静态节点、订阅节点、模板节点和直连池
- 地区组：普通地区组使用 `US` / `HK` / `JP`，家宽地区组使用 `US-RES` / `HK-RES`
- 出口策略：`random`、`round-robin`、`least-latency`
- 模板节点：支持 Bright Data 这类按地区动态生成账号的出口，可作为地区兜底或冷门地区动态出口
- 家宽模板：通过 `residential: true` 单独声明，服务家宽请求；普通请求没有同地区普通出口时可降级使用同地区家宽出口
- 高端出口：节点或订阅池可配置 `premium: true`，请求带 `egress.premium=true` 时优先使用高端节点，高端不可用时回到原有候选和 fallback
- 健康检查：后台探测节点连通性和延迟，供可用性判断和 `least-latency` 使用
- TLS 指纹：支持内置预设、自定义 JA3、配置文件指纹名称和请求级字符串
- Admin API：管理节点池、订阅刷新、TLS 指纹、流量日志和系统重载
- Web 管理页面：当前原型已接入 Admin API，覆盖节点池、地区组、订阅、模板、TLS、流量、日志级别和系统重载
- Admin 鉴权：密码登录 + JWT token 保护管理 API，可关闭

## 快速开始

```bash
# Docker Hub 镜像部署（VPS 推荐）
cp .env.example .env
cp configs/gateway.docker.yaml.example configs/gateway.yaml
cp configs/nodes.yaml.example configs/nodes.yaml
cp configs/fingerprints.yaml.example configs/fingerprints.yaml
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d

# 本机构建 Docker 镜像
docker compose up -d --build

# 完整构建（包含前端）
./build.sh

# 仅编译 Go（with_utls 用于 Reality / uTLS 指纹）
go build -tags with_utls -o chijie ./cmd/gateway/

# 运行（默认读取 ./configs/ 目录）
./chijie

# 指定配置目录
./chijie -config /path/to/configs

# 交叉编译（Mac → Linux x86）
GOOS=linux GOARCH=amd64 go build -tags with_utls -o chijie ./cmd/gateway/
```

Docker 宿主机默认 Admin 地址：`http://127.0.0.1:19090/`。通过 SSH tunnel 访问时，本机浏览器仍可使用 `http://127.0.0.1:9090/`。

## 配置文件

- `configs/gateway.yaml`：服务器端口、TLS、认证密钥、Admin 鉴权和日志
- `configs/nodes.yaml`：节点池配置
- `configs/fingerprints.yaml`：TLS 指纹库

实际配置文件已加入 `.gitignore`，仓库提供 `*.example` 模板。首次部署：

```bash
cp configs/gateway.yaml.example configs/gateway.yaml
cp configs/nodes.yaml.example configs/nodes.yaml
cp configs/fingerprints.yaml.example configs/fingerprints.yaml
# 编辑 gateway.yaml，至少要：
#   - 设置 admin.password（或保持 listen=127.0.0.1 并留空 password）
#   - 设置 admin.jwt_secret（长度 ≥ 16，禁止占位字符串）
```

启动会拒绝以下不安全配置：
- `admin.jwt_secret` 为空、长度 < 16，或为已知占位字符串。
- `admin.password` 为空且 `admin.listen` 监听非 127.0.0.1/localhost。

### 安全相关配置项

```yaml
server:
  # 是否允许 /proxy 和 /tunnel 拨向私网/回环地址。默认 false 防 SSRF。
  allow_private_targets: false

admin:
  # 登录失败速率限制：login_window 内累计 login_max_failures 次失败后锁定 login_lockout。
  login_max_failures: 5
  login_window: "60s"
  login_lockout: "5m"

health_check:
  interval: "30s"
  timeout: "5s"
  url: "https://www.google.com/generate_204"
  max_fail: 3

proxy:
  # 单次 /proxy 通过某个出口访问目标站点时等待响应头的超时。
  response_header_timeout: "3s"
  # 单次 /proxy 通过某个出口访问目标站点的完整请求总超时。
  total_timeout: "30s"
  # 单次 /proxy 出口执行失败时最多尝试的可用静态/订阅节点数量。
  max_attempts: 5
  # follow_redirects=true 时单次请求最多跟随的 HTTP redirect 次数。
  max_redirects: 5
  # 可用节点全部失败后继续尝试同地区同类型模板节点。
  template_fallback_after_attempts: true
```

`/proxy` 单次请求 body 上限 10 MB，上游响应 body 上限 32 MB；Admin API JSON body 上限 1 MB。

订阅拉取只允许 `http` / `https`，默认拒绝私网/回环/保留地址，单次订阅响应 body 上限 4 MB。

出口节点拨号、订阅地址校验和 sing-box DNSRouter 使用内置公共 DNS resolver，默认查询 `1.1.1.1:53` 和 `8.8.8.8:53`，避免 Docker 内置 `127.0.0.11` 解析失败影响节点可用性。

Admin 登录限速的客户端 IP 在 Cloudflare 部署下优先读取 `CF-Connecting-IP` / `True-Client-IP`，随后兼容 `X-Forwarded-For` 与 `X-Real-IP`，无有效 header 时回退到 `RemoteAddr`。

`/tunnel` WebSocket 默认拒绝跨站浏览器升级（无 Origin 放行；有 Origin 必须同源），不影响 Cloudflare Workers / Go / Node.js / Python 等服务端客户端。

外部服务接入 Proxy API 详见 [docs/proxy-client-usage.md](docs/proxy-client-usage.md)。
参数驱动出口模型详见 [docs/parameter-driven-egress.md](docs/parameter-driven-egress.md)。
订阅节点地区识别、节点元数据和模板语义详见 [docs/subscription-routing.md](docs/subscription-routing.md)。
高端出口节点配置和请求语义详见 [docs/premium-egress.md](docs/premium-egress.md)。
模板 fallback、远端 Chijie 和优先级规则详见 [docs/template-fallback.md](docs/template-fallback.md)。
Admin 前端接入和构建说明详见 [docs/admin-frontend.md](docs/admin-frontend.md)。
TLS 指纹配置、`extra_fp` 兼容和测试接口详见 [docs/tls-fingerprints.md](docs/tls-fingerprints.md)。
Docker 部署详见 [docs/docker-deployment.md](docs/docker-deployment.md)。
Docker Hub 自动发布详见 [docs/dockerhub-release.md](docs/dockerhub-release.md)。
目录和模块职责详见 [DETAILS.md](DETAILS.md)。

## 出口协议

静态节点和订阅节点当前支持：

- `direct`
- `socks5`
- `http_proxy` / `http`
- `ss` / `shadowsocks`
- `vmess`
- `vless`
- `trojan`
- `hysteria2` / `hy2`
- `anytls`
- `tuic`

`vmess` / `vless` / `trojan` 支持常见 V2Ray 传输参数：TCP、WebSocket、gRPC、HTTP/H2、HTTPUpgrade、QUIC。`vless` 支持 TLS、Reality 和 `xhttp` / `splithttp` 的 `packet-up`、`stream-up` 模式；`hysteria2` 支持 Shadowrocket 常见的 `mport=16001-17000` 端口跳跃写法；`anytls` / `tuic` 使用 sing-box outbound 并默认启用 TLS。Clash YAML 的 `fingerprint` 会作为证书 SHA-256 pinning 指纹保留，不再当成 uTLS 指纹传给 sing-box；订阅中的未知 uTLS 指纹值会被忽略，避免正常节点被跳过。当前 sing-box 版本不原生支持 `xhttp` / `splithttp`，`vless+xhttp` 由 Chijie 的专用拨号器接入；其他协议的 `xhttp` 节点会被跳过并在订阅池错误信息中展示原因。Reality 和订阅中的 `fp/client-fingerprint` 需要使用 `-tags with_utls` 构建，`./build.sh` 已默认启用。

订阅导入支持 Clash YAML、Base64 URI 列表、未 Base64 包装的纯 URI 列表。URI 列表支持 `ss`、`vmess`、`vless`、`trojan`、`hysteria2` / `hy2`、`anytls` 和 `tuic`。Shadowsocks URI 支持 `ss://userinfo@host:port?plugin=...` 和 SIP002 常见的 `ss://userinfo@host:port/?plugin=...` 写法；`simple-obfs` 插件名会规范化为 sing-box 可识别的 `obfs-local`。订阅拉取使用 `clash-verge/v2.0.0` User-Agent，以兼容会按客户端 UA 返回不同节点集合的订阅服务。单个订阅池可填写多个订阅地址，使用换行、英文逗号或 `|` 分隔；部分订阅地址失败时，已成功解析的节点仍会进入该池。订阅地址必须是 `http` / `https` 公网目标，响应体超过 4 MB 时会拒绝解析。

订阅池的 `update_interval` 支持 `30m`、`12h`、`3d`，留空表示只手动刷新。自动刷新或配置重载时，如果最近一次拉取失败，运行时保留上一次成功拉取的节点并记录池级错误。

## 请求协议

```
POST /proxy
Authorization: Bearer <proxy_token>
Content-Type: application/json

{
  "url": "https://target.example/api/data",
  "method": "POST",
  "headers": {
    "Content-Type": "application/json"
  },
  "payload": "{\"hello\":\"world\"}",
  "follow_redirects": false,
  "egress": {
    "region": "US",
    "strategy": "least-latency",
    "residential": false,
    "premium": false,
    "tls_fingerprint": "chrome"
  }
}
```

`Authorization: Bearer <proxy_token>` 使用后台 System 页面或 `/api/auth/proxy-token` 生成的 JWT。

Proxy token 是无状态 JWT，只在生成弹窗中显示一次；后台不保存 token 清单、原文或到期时间，因此刷新页面或重新登录后不会再展示之前创建的 token。生产环境需要由调用方或密钥管理系统保存生成结果；失效方式是等待 token 过期，或更换 `admin.jwt_secret` 使全部旧 token 失效。

字段说明：

- `url`：目标 URL，必填。
- `method`：目标请求方法，缺省为 `GET`。
- `headers`：目标请求 Header；目标站点 Cookie 需要显式写入 `headers.Cookie`。
- `payload`：目标请求 Body。`GET` 和 `HEAD` 请求不会发送 Body。
- `follow_redirects`：是否由网关自动跟随 HTTP redirect，缺省为 `false`。
- `egress.region`：二字母地区码；空字符串表示直连。
- `egress.any`：为 `true` 时不指定地区，选择一个非直连静态/订阅出口；也可将 `region` 传为 `*`、`ANY` 或 `AUTO`。
- `egress.max_latency_ms`：配合 `egress.any` 使用，限制候选出口最近健康检查延迟上限；`0` 表示不限制。
- `egress.strategy`：`random`、`round-robin`、`least-latency`，缺省为 `random`。
- `egress.residential`：是否使用家宽出口。
- `egress.premium`：是否优先使用高端出口；未指定 `region` 时会自动选择任意非直连出口并优先尝试高端节点。
- `egress.tls_fingerprint`：TLS 指纹名称、预设名、JA3 raw、JA4 raw、Akamai raw 或可解析的配置字符串；测试结果以远端返回的真实指纹信息为准。

响应使用目标服务器的 status code、`Content-Type` 和响应体。目标服务器返回的 `Set-Cookie` 会逐条写入 `/proxy` 响应头；网关不保存 cookie、不自动生成下一次请求的 `Cookie`，也不改写 cookie 的 `Domain` / `Path`。目标服务器返回 3xx 且 `follow_redirects=false` 时，网关保留 3xx 状态并转发 `Location` 响应头。

`follow_redirects=true` 时，网关按 `proxy.max_redirects` 自动跟随跳转，默认最多 5 次。最终响应仍是最终页面的原始 status code、`Content-Type` 和 body，并额外写入 `X-Chijie-Final-URL`、`X-Chijie-Redirect-Count`、`X-Chijie-Max-Redirects`、`X-Chijie-Redirects`。超过次数限制时返回最后一个未继续跟随的 3xx 响应，并写入 `X-Chijie-Redirect-Limit-Reached: true`。每次跳转目标都会继续执行 `/proxy` 的目标 URL 安全校验。

如果选中的非直连出口在建立连接或等待响应头阶段失败（例如 `EOF`、拨号失败、代理断流、TLS 握手失败），`/proxy` 会按当前策略继续换候选出口，默认最多尝试 5 个可用静态/订阅节点。失败的静态/订阅节点会立即标记为 `Alive=false`，后续可由健康检查恢复。目标站点已经返回 HTTP 状态码时不会重试，例如真实的 `403`、`404`、`502` 会原样返回给调用方。

### 出口选择

1. `region` 为空、`any` 不是 `true` 且 `premium` 不是 `true` 时使用直连出口；如果传入 `tls_fingerprint`，仍应用 TLS 指纹。
2. `any=true`、`premium=true` 或 `region` 为 `*`、`ANY`、`AUTO` 时，选择任意非直连静态/订阅出口；设置 `max_latency_ms` 后只选择延迟不超过阈值的节点。
3. `region` 不为空时标准化为大写二字母地区码。
4. `residential=false` 查找普通地区组，例如 `US`。
5. `residential=true` 查找家宽地区组，例如 `US-RES`。
6. `premium=true` 时优先选择高端节点和高端模板；高端不可用或尝试失败时继续使用原有普通、家宽或模板 fallback。
7. 地区组内存在可用静态节点或订阅节点时，按 `strategy` 选择节点。
8. 订阅池开启 `try_offline` 且某地区只有一个离线订阅节点时，在模板 fallback 前允许该节点再尝试一次。
9. 每个出口访问目标站点时使用 `proxy.response_header_timeout` 控制等待响应头的超时，默认 `3s`；使用 `proxy.total_timeout` 控制完整请求总时长，默认 `30s`；`follow_redirects=true` 时最多跟随 `proxy.max_redirects` 次 redirect，默认 5 次；开启 `proxy.template_fallback_after_attempts` 时，地区组内可用节点连续失败达到 `proxy.max_attempts` 后继续尝试同类型模板节点；地区组内没有可用节点时也会直接使用同类型模板节点。
10. 普通请求没有普通节点和普通模板时，降级尝试同地区家宽节点和家宽模板；高端请求也保留这类原有 fallback。
11. 没有可用节点也没有可用模板时返回错误。

任意地区出口不会使用 `direct`、`type: direct` 静态节点或模板节点，因为模板节点需要明确地区码来生成代理账号。

模板节点默认支持任意二字母地区码。家宽模板需要单独配置 `residential: true` 或 `coverage: residential`；`coverage: both` 可同时覆盖普通和家宽请求。

## WebSocket 隧道

```
WS /tunnel

# 首帧发送 JSON：
{
  "url": "wss://target.example/ws",
  "authorization": "Bearer <proxy_token>",
  "method": "GET",
  "headers": {
    "Authorization": "Bearer xxx"
  },
  "payload": "",
  "egress": {
    "region": "US",
    "strategy": "round-robin",
    "residential": false,
    "premium": false,
    "tls_fingerprint": "chrome"
  }
}

# 收到 {"status":"connected"} 后，后续帧进入双向转发
```

`url` 为 `ws://` 或 `wss://` 时，网关会对上游目标执行 WebSocket 握手，首帧 `headers` 会写入上游握手请求，`payload` 会在连接成功后作为第一条文本消息发往上游。`wss://` 上游握手支持通过当前出口应用 `tls_fingerprint`。

`url` 为 `http://` 或 `https://` 时，隧道保持 raw TCP 转发，客户端自行完成 HTTP 或 TLS 协议细节；该模式不会接管客户端 TLS 握手。

浏览器 WebSocket API 不能在握手时设置任意 Header；WebSocket 数据帧也没有 HTTP Header。网关支持在首个 JSON 帧里放 `authorization`。非浏览器客户端也可以在握手 Header 中使用 `Authorization: Bearer <proxy_token>`。

## Admin API

默认监听 `127.0.0.1:9090`，由 `gateway.yaml` 的 `admin.listen` 配置。

### 鉴权配置

```yaml
admin:
  listen: "127.0.0.1:9090"
  password: "your_password"
  jwt_secret: "your_secret_key"
  jwt_expire: "24h"
```

登录后获取 JWT token，后续请求携带 `Authorization: Bearer <token>` Header。`password` 为空时不启用 Admin 鉴权。

Proxy API 调用 token 由后台生成，使用同一个 `jwt_secret` 签名，但 claim 中只带 `proxy=true`，不能访问 Admin API。JWT 为无状态 token，只在生成时返回原文和到期时间；后台不持久化 token 记录，失效方式是等待过期或更换 `admin.jwt_secret`。

### API 端点

#### 认证

| 方法 | 端点 | 说明 |
|------|------|------|
| POST | /api/auth/login | 密码登录，返回 JWT token |
| POST | /api/auth/proxy-token | 生成只用于 `/proxy` 和 `/tunnel` 的 Bearer token |

#### 节点管理

| 方法 | 端点 | 说明 |
|------|------|------|
| GET | /api/nodes | 查看节点池、节点状态和地区组状态 |
| POST | /api/nodes | 添加新节点池 |
| PUT | /api/nodes/pool | 更新节点池配置 |
| DELETE | /api/nodes/pool?name=xxx | 删除节点池配置 |
| PUT | /api/nodes/node | 更新 static 节点池中的单个节点 |
| DELETE | /api/nodes/node?pool=xxx&node=xxx | 删除 static 节点池中的单个节点 |
| PUT | /api/nodes/subscription/node | 更新 subscription 节点地区修正、别名和标签 |
| POST | /api/nodes/refresh?pool=xxx | 手动刷新指定订阅池 |
| POST | /api/nodes/test | 测试节点连通性，返回出口 IP、国家码和 IP 类型信息 |
| POST | /api/nodes/template/test | 按模板池、地区和可选测试 URL 测试连通性；普通代理模板生成临时节点，Chijie 模板请求远端 `/proxy` |
| POST | /api/nodes/enabled | 启用或禁用节点，并持久化到 `nodes.yaml` |

#### 流量日志

| 方法 | 端点 | 说明 |
|------|------|------|
| GET | /api/traffic?limit=100 | 查看请求记录、合并展示记录、分钟级流量序列和聚合指标；内存最多保留最近 1000 条 |
| POST | /api/traffic/grouping-rules | 保存失败 URL 规范化规则，并立即用于 Traffic 合并展示 |

Traffic 指标使用有效请求口径：成功请求逐条计入；失败请求按 `kind + url/target + egress_group` 合并计入，避免同一个目标故障被调用方重复重试后放大错误率。延迟指标只统计最终成功的请求；原始请求数仍通过 `raw_requests` / `raw_failures` 返回给 Admin 页面展示。

失败请求的 URL 可按规则先做规范化再参与合并。Admin 请求详情里的 `Ignore URL params` 可从真实错误 URL 生成规则：Host 和 Path 每一段可点击切换为 `*`，Query 参数可勾选加入 `drop_keys`。规则会保存到 `gateway.yaml`，完整说明见 [docs/traffic-url-grouping.md](docs/traffic-url-grouping.md)：

```yaml
traffic:
  failure_grouping:
    enabled: true
    url_normalization:
      enabled: true
      rules:
        - name: "hls-signed-query"
          match:
            host_pattern: "*.pipecdn.vip"
            path_pattern: "/ppot/_definst_/*/lvod/*/chunklist.m3u8"
          query:
            drop_keys:
              - vendtime
              - vhash
            sort: true
```

#### 指纹管理

| 方法 | 端点 | 说明 |
|------|------|------|
| GET | /api/fingerprints | 查看所有 TLS 指纹 |
| POST | /api/fingerprints | 添加新指纹，支持 raw JA3/JA4/Akamai 和检测 JSON 导入 |
| DELETE | /api/fingerprints/:name | 删除指定指纹 |
| POST | /api/fingerprints/test | 对 HTTPS 目标发起真实 TLS/HTTP2 指纹测试 |

#### 系统管理

| 方法 | 端点 | 说明 |
|------|------|------|
| POST | /api/reload | 热重载节点池和 TLS 指纹配置 |
| GET | /api/stats | 基础统计、节点池数量、指纹数量和流量指标 |
| PUT | /api/system/logging | 修改当前日志级别并写入 `gateway.yaml` |
| GET / PUT | /api/system/health-check | 查看或修改全局健康检查默认参数，并写入 `gateway.yaml` |
| GET / PUT | /api/system/proxy | 查看或修改 `/proxy` 响应头等待超时、完整请求总超时、重试与模板兜底设置，并写入 `gateway.yaml` |
| GET | /api/config/export | 导出当前 YAML 配置快照 |

## 开发计划

详见 [TODO.md](TODO.md)。
