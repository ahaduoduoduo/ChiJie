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
- 家宽模板：通过 `residential: true` 单独声明，只服务家宽请求
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
```

`/proxy` 单次请求 body 上限 10 MB，上游响应 body 上限 32 MB；Admin API JSON body 上限 1 MB。

订阅拉取只允许 `http` / `https`，默认拒绝私网/回环/保留地址，单次订阅响应 body 上限 4 MB。

Admin 登录限速的客户端 IP 在 Cloudflare 部署下优先读取 `CF-Connecting-IP` / `True-Client-IP`，随后兼容 `X-Forwarded-For` 与 `X-Real-IP`，无有效 header 时回退到 `RemoteAddr`。

`/tunnel` WebSocket 默认拒绝跨站浏览器升级（无 Origin 放行；有 Origin 必须同源），不影响 Cloudflare Workers / Go / Node.js / Python 等服务端客户端。

外部服务接入 Proxy API 详见 [docs/proxy-client-usage.md](docs/proxy-client-usage.md)。
参数驱动出口模型详见 [docs/parameter-driven-egress.md](docs/parameter-driven-egress.md)。
订阅节点地区识别、节点元数据和模板语义详见 [docs/subscription-routing.md](docs/subscription-routing.md)。
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

`vmess` / `vless` / `trojan` 支持常见 V2Ray 传输参数：TCP、WebSocket、gRPC、HTTP/H2、HTTPUpgrade、QUIC。`vless` 支持 TLS 和 Reality；Reality 和订阅中的 `fp/client-fingerprint` 需要使用 `-tags with_utls` 构建，`./build.sh` 已默认启用。

订阅导入支持 Clash YAML、Base64 URI 列表、未 Base64 包装的纯 URI 列表。单个订阅池可填写多个订阅地址，使用换行、英文逗号或 `|` 分隔；部分订阅地址失败时，已成功解析的节点仍会进入该池。订阅地址必须是 `http` / `https` 公网目标，响应体超过 4 MB 时会拒绝解析。

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
  "egress": {
    "region": "US",
    "strategy": "least-latency",
    "residential": false,
    "tls_fingerprint": "chrome"
  }
}
```

`Authorization: Bearer <proxy_token>` 使用后台 System 页面或 `/api/auth/proxy-token` 生成的 JWT。

字段说明：

- `url`：目标 URL，必填。
- `method`：目标请求方法，缺省为 `GET`。
- `headers`：目标请求 Header。
- `payload`：目标请求 Body。`GET` 和 `HEAD` 请求不会发送 Body。
- `egress.region`：二字母地区码；空字符串表示直连。
- `egress.any`：为 `true` 时不指定地区，选择一个非直连静态/订阅出口；也可将 `region` 传为 `*`、`ANY` 或 `AUTO`。
- `egress.max_latency_ms`：配合 `egress.any` 使用，限制候选出口最近健康检查延迟上限；`0` 表示不限制。
- `egress.strategy`：`random`、`round-robin`、`least-latency`，缺省为 `random`。
- `egress.residential`：是否使用家宽出口。
- `egress.tls_fingerprint`：TLS 指纹名称、预设名、JA3 raw、JA4 raw、Akamai raw 或可解析的配置字符串；测试结果以远端返回的真实指纹信息为准。

响应使用目标服务器的 status code、`Content-Type` 和响应体。

如果选中的非直连出口在建立连接或等待响应头阶段失败（例如 `EOF`、拨号失败、代理断流、TLS 握手失败），`/proxy` 会按当前策略再换一个候选出口重试一次。目标站点已经返回 HTTP 状态码时不会重试，例如真实的 `403`、`404`、`502` 会原样返回给调用方。

### 出口选择

1. `region` 为空且 `any` 不是 `true` 时使用直连出口；如果传入 `tls_fingerprint`，仍应用 TLS 指纹。
2. `any=true` 或 `region` 为 `*`、`ANY`、`AUTO` 时，选择任意非直连静态/订阅出口；设置 `max_latency_ms` 后只选择延迟不超过阈值的节点。
3. `region` 不为空时标准化为大写二字母地区码。
4. `residential=false` 查找普通地区组，例如 `US`。
5. `residential=true` 查找家宽地区组，例如 `US-RES`。
6. 地区组内存在可用静态节点或订阅节点时，按 `strategy` 选择节点。
7. 地区组内没有可用节点时，使用同类型模板节点生成出口。
8. 没有可用节点也没有同类型模板时返回错误。

任意地区出口不会使用 `direct`、`type: direct` 静态节点或模板节点，因为模板节点需要明确地区码来生成代理账号。

模板节点默认支持任意二字母地区码，不支持家宽。家宽模板需要单独配置 `residential: true`。

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

Proxy API 调用 token 由后台生成，使用同一个 `jwt_secret` 签名，但 claim 中只带 `proxy=true`，不能访问 Admin API。JWT 为无状态 token，失效方式是等待过期或更换 `admin.jwt_secret`。

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
| POST | /api/nodes/template/test | 按模板池、地区和可选测试 URL 生成临时节点并测试连通性，返回出口 IP、国家码和 IP 类型信息 |
| POST | /api/nodes/enabled | 启用或禁用节点，并持久化到 `nodes.yaml` |

#### 流量日志

| 方法 | 端点 | 说明 |
|------|------|------|
| GET | /api/traffic?limit=100 | 查看请求记录、分钟级流量序列和聚合指标 |

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
| GET | /api/config/export | 导出当前 YAML 配置快照 |

## 开发计划

详见 [TODO.md](TODO.md)。
