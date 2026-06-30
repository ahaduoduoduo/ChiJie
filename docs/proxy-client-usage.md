# Chijie Proxy API 使用文档

日期：2026-05-07

更新：2026-06-28

本文面向接入 Chijie 的业务服务、脚本、Cloudflare Workers 或 AI Agent。调用方不需要访问 Admin API，只需要拿到 `Proxy API` 地址和一个 `proxy_token`。

## 接入信息

服务所有者需要提供：

```bash
CHIJIE_BASE_URL="https://proxy.example.com"
CHIJIE_PROXY_TOKEN="eyJ..."
```

`CHIJIE_BASE_URL` 指向 Proxy API，不是 Admin 管理后台。

`CHIJIE_PROXY_TOKEN` 由 System 页面或 Admin API 的 `/api/auth/proxy-token` 生成。该 token 是无状态 JWT，只在生成时显示一次；后台不保存已创建 token 列表、原文或到期时间。生产环境应将生成结果保存到调用方环境变量或密钥管理系统；失效方式是等待过期，或由服务所有者更换 `admin.jwt_secret` 使全部旧 token 失效。

所有代理请求都使用 Bearer token：

```http
Authorization: Bearer <proxy_token>
```


## 接口总览

| Method | Path | 鉴权 | 用途 |
| --- | --- | --- | --- |
| `GET` | `/health` | 否 | 存活检查 |
| `POST` | `/proxy` | 是 | 发起一次 HTTP / HTTPS 目标请求 |
| `WS` / `WSS` | `/tunnel` | 是 | 建立 WebSocket 隧道或 raw TCP 隧道 |

## 健康检查

```bash
curl "$CHIJIE_BASE_URL/health"
```

正常响应：

```json
{"status":"ok"}
```

该接口只表示 Chijie Proxy API 进程可访问，不代表某个出口节点可用。

## POST /proxy

`/proxy` 用于让 Chijie 代替调用方发起一次目标 HTTP / HTTPS 请求，并按请求里的 `egress` 参数选择出口。

### 请求格式

```http
POST /proxy
Authorization: Bearer <proxy_token>
Content-Type: application/json
```

```json
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
    "tls_fingerprint": "chrome"
  }
}
```

### 字段说明

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `url` | string | 是 | 目标 URL，`/proxy` 建议使用 `http://` 或 `https://` |
| `method` | string | 否 | 目标请求方法，默认 `GET` |
| `headers` | object | 否 | 发送给目标站点的请求 Header；目标站点 Cookie 需要显式写入 `headers.Cookie` |
| `payload` | string | 否 | 发送给目标站点的请求 Body；`GET` / `HEAD` 会忽略该字段 |
| `follow_redirects` | boolean | 否 | 是否由 Chijie 自动跟随 HTTP redirect，默认 `false` |
| `egress.region` | string | 否 | 二字母地区码，例如 `US`、`HK`、`JP`、`GB`；为空表示直连 |
| `egress.any` | boolean | 否 | 为 `true` 时选择任意非直连出口 |
| `egress.max_latency_ms` | number | 否 | 配合 `any=true` 使用，按最近健康检查延迟过滤候选出口 |
| `egress.strategy` | string | 否 | `random`、`round-robin`、`least-latency`，默认 `random` |
| `egress.residential` | boolean | 否 | 是否要求家宽出口；为 `false` 时优先普通出口，普通出口不可用时可降级使用同地区家宽出口 |
| `egress.tls_fingerprint` | string | 否 | TLS 指纹名称或指纹配置字符串，例如 `chrome` |

地区码会标准化为大写二字母代码。英国使用标准码 `GB`；传入 `UK` 时也会归一为 `GB`。

### 响应格式

成功时，Chijie 返回目标服务器的 HTTP status code、`Content-Type` 和响应体。目标站点返回的 `Set-Cookie` 会逐条写入 `/proxy` 响应头。响应体不是固定 JSON 包装，而是目标站点原始响应体。

Chijie 不保存 cookie，不会自动把本次响应里的 `Set-Cookie` 转成下一次请求的 `Cookie`。需要会话保持时，调用方负责保存目标站点返回的 cookie，并在后续请求的 `headers.Cookie` 中带回。浏览器环境还会受到浏览器对 `Set-Cookie`、`Domain`、`Path` 和 JavaScript 可读性的限制。

例如目标站点返回 `200 application/json`：

```json
{"ip":"203.0.113.10"}
```

目标站点返回 `302` 时，Chijie 默认不自动跟随跳转，而是把 `302` 响应返回给调用方，并转发目标站点的 `Location` 响应头。

请求中传入 `"follow_redirects": true` 时，Chijie 会自动跟随 HTTP redirect，最终返回最后页面的原始 status code、`Content-Type` 和响应体。响应头会附带跳转细节：

| 响应头 | 含义 |
| --- | --- |
| `X-Chijie-Final-URL` | 最终响应对应的 URL |
| `X-Chijie-Redirect-Count` | 已跟随的跳转次数 |
| `X-Chijie-Max-Redirects` | 当前全局最大跳转次数配置 |
| `X-Chijie-Redirects` | JSON 数组，包含每次跳转的 `status_code`、`from_url`、`to_url`、`from_method`、`to_method` 和原始 `location` |
| `X-Chijie-Redirect-Limit-Reached` | 达到最大跳转次数时为 `true` |

`follow_redirects=true` 时，每个跳转目标都会继续执行目标 URL 安全校验。超过 `proxy.max_redirects` 后，Chijie 返回最后一个未继续跟随的 3xx 响应。

失败时，Chijie 返回 JSON：

```json
{
  "error": "proxy request failed",
  "detail": "do request via US-01: context deadline exceeded"
}
```

常见错误：

| HTTP 状态 | `error` | 含义 |
| --- | --- | --- |
| `400` | `invalid json` | 请求体不是合法 JSON |
| `400` | `url required` | 缺少 `url` |
| `400` | `invalid target` | 目标 URL 不合法，或被私网 / 回环地址防护拦截 |
| `400` | `egress failed` | 出口参数无效，或没有可用出口 |
| `403` | `unauthorized` | token 缺失、错误或过期 |
| `405` | `method not allowed` | `/proxy` 只接受 `POST` |
| `502` | `proxy request failed` | 已选择出口，但目标请求失败 |

### 出口失败自动重试

`/proxy` 通过单个出口访问目标站点时使用 `gateway.yaml` 的 `proxy.response_header_timeout` 控制等待响应头的超时，默认 `3s`。目标站点开始返回响应后，读取 body 继续受 `proxy.total_timeout` 约束，完整请求总超时默认 `30s`。`follow_redirects=true` 时按 `proxy.max_redirects` 限制最大跳转次数，默认最多 5 次。在非直连出口执行失败时会按 `proxy.max_attempts` 继续尝试候选静态/订阅节点，默认最多 5 个。失败节点会立即标记为 `Alive=false`，后台健康检查成功后可恢复。开启 `proxy.template_fallback_after_attempts` 时，可用节点都失败后继续尝试同地区同类型模板节点。

- `least-latency`：第一次使用最低延迟候选，失败后使用下一个延迟候选。
- `random`：候选顺序随机，失败后使用下一个随机候选。
- `round-robin`：第一次使用当前轮询候选，失败后使用下一个候选。

目标站点已经返回 HTTP 状态码时不会重试，例如真实的 `403`、`404`、`502` 会原样返回给调用方。非幂等请求（例如会创建订单的 `POST`）如果第一次请求已经到达源站但连接中途断开，重试可能让源站收到第二次请求；调用方应使用业务幂等键控制重复提交风险。

### 调用示例：直连请求

```bash
curl -sS "$CHIJIE_BASE_URL/proxy" \
  -H "Authorization: Bearer $CHIJIE_PROXY_TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary '{
    "url": "https://api.ipify.org?format=json",
    "method": "GET"
  }'
```

不传 `egress.region` 且 `egress.any` 不为 `true` 时使用直连出口。

### 调用示例：指定美国出口

```bash
curl -sS "$CHIJIE_BASE_URL/proxy" \
  -H "Authorization: Bearer $CHIJIE_PROXY_TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary '{
    "url": "https://api.ipify.org?format=json",
    "method": "GET",
    "egress": {
      "region": "US",
      "strategy": "least-latency"
    }
  }'
```

### 调用示例：POST JSON 到目标服务

```bash
curl -sS "$CHIJIE_BASE_URL/proxy" \
  -H "Authorization: Bearer $CHIJIE_PROXY_TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary '{
    "url": "https://target.example/api/orders",
    "method": "POST",
    "headers": {
      "Content-Type": "application/json",
      "Accept": "application/json"
    },
    "payload": "{\"order_id\":\"demo-001\"}",
    "egress": {
      "region": "JP",
      "strategy": "round-robin",
      "tls_fingerprint": "chrome"
    }
  }'
```

### 调用示例：携带目标站点 Cookie

```bash
curl -i "$CHIJIE_BASE_URL/proxy" \
  -H "Authorization: Bearer $CHIJIE_PROXY_TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary '{
    "url": "https://target.example/account",
    "method": "GET",
    "headers": {
      "Cookie": "session=abc; uid=123",
      "User-Agent": "Mozilla/5.0"
    }
  }'
```

如果目标站点响应中包含 `Set-Cookie`，`curl -i` 会在 `/proxy` 的响应头中看到对应的 `Set-Cookie`。后续请求仍需要调用方自行把有效 cookie 拼成 `headers.Cookie`。

### Node.js 示例

```js
const baseURL = process.env.CHIJIE_BASE_URL;
const token = process.env.CHIJIE_PROXY_TOKEN;

async function callViaChijie(targetURL, options = {}) {
  const response = await fetch(`${baseURL}/proxy`, {
    method: "POST",
    headers: {
      "Authorization": `Bearer ${token}`,
      "Content-Type": "application/json"
    },
    body: JSON.stringify({
      url: targetURL,
      method: options.method || "GET",
      headers: options.headers || {},
      payload: options.payload || "",
      egress: options.egress || {}
    })
  });

  const contentType = response.headers.get("content-type") || "";
  const body = await response.text();

  if (!response.ok && contentType.includes("application/json")) {
    throw new Error(`Chijie proxy failed: ${body}`);
  }

  return {
    status: response.status,
    contentType,
    body
  };
}

const result = await callViaChijie("https://api.ipify.org?format=json", {
  egress: { region: "US", strategy: "least-latency" }
});

console.log(result.status, result.body);
```

### Python 示例

```python
import os
import requests

base_url = os.environ["CHIJIE_BASE_URL"]
token = os.environ["CHIJIE_PROXY_TOKEN"]

resp = requests.post(
    f"{base_url}/proxy",
    headers={
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
    },
    json={
        "url": "https://api.ipify.org?format=json",
        "method": "GET",
        "egress": {
            "region": "US",
            "strategy": "least-latency",
        },
    },
    timeout=60,
)

print(resp.status_code, resp.text)
```

## WebSocket /tunnel

`/tunnel` 用于长连接场景。连接建立后，调用方发送第一条 JSON 初始化帧，Chijie 根据该帧完成认证、出口选择和目标连接。

### 连接地址

```text
wss://proxy.example.com/tunnel
```

本地或内网测试也可以使用：

```text
ws://127.0.0.1:18080/tunnel
```

### 首帧格式

```json
{
  "url": "wss://target.example/ws",
  "authorization": "Bearer <proxy_token>",
  "method": "GET",
  "headers": {
    "Authorization": "Bearer target-site-token"
  },
  "payload": "",
  "egress": {
    "region": "US",
    "strategy": "round-robin",
    "residential": false,
    "tls_fingerprint": "chrome"
  }
}
```

非浏览器客户端也可以在 WebSocket 握手 Header 里带：

```http
Authorization: Bearer <proxy_token>
```

浏览器 WebSocket API 不能设置自定义握手 Header，因此浏览器客户端需要把 `authorization` 放在首帧 JSON 里。注意：不要把长期有效的 `proxy_token` 放到公开网页前端代码中。

### 隧道模式

| `url` scheme | 行为 |
| --- | --- |
| `ws://` / `wss://` | Chijie 对上游目标执行 WebSocket 握手，后续 WebSocket 消息双向转发 |
| `http://` / `https://` | Chijie 连接目标主机端口，后续数据按 raw TCP 双向转发 |

连接成功后，Chijie 会先发一条文本消息：

```json
{"status":"connected"}
```

之后进入双向转发。

失败时，Chijie 会返回错误文本帧：

```json
{"error":"unauthorized"}
```

常见错误包括：

- `invalid init frame`
- `unauthorized`
- `url required`
- `invalid target`
- `egress failed`
- `get dialer failed`
- `dial target failed`
- `dial websocket target failed`
- `tls fingerprint failed`

## 出口选择规则

### 直连

```json
{
  "egress": {}
}
```

或不传 `egress`。适合只需要 Chijie 代发请求，但不需要代理出口的场景。

### 指定地区

```json
{
  "egress": {
    "region": "SG",
    "strategy": "least-latency"
  }
}
```

指定地区时，Chijie 会选择对应地区组里的可用节点；如果配置了模板节点，也可以按地区动态生成出口。

### 任意非直连出口

```json
{
  "egress": {
    "any": true,
    "strategy": "least-latency",
    "max_latency_ms": 800
  }
}
```

也可以写：

```json
{
  "egress": {
    "region": "ANY"
  }
}
```

`any=true` 不会选择直连出口，也不会选择必须依赖明确地区码的模板节点。

### 家宽出口

```json
{
  "egress": {
    "region": "US",
    "residential": true,
    "strategy": "least-latency"
  }
}
```

家宽出口会查找 `US-RES` 这类地区组。是否可用取决于服务所有者是否配置了家宽节点或家宽模板。

### TLS 指纹

```json
{
  "egress": {
    "region": "US",
    "tls_fingerprint": "chrome"
  }
}
```

`tls_fingerprint` 可使用 Chijie 后台配置的指纹名，也可使用支持的预设或指纹字符串。调用方没有特殊需求时可以不传。

## 限制与安全行为

- `/proxy` 请求 JSON body 上限为 `10 MB`。
- `/proxy` 上游响应 body 上限为 `32 MB`。
- `/proxy` 单个出口等待目标响应头的超时时间由 `proxy.response_header_timeout` 控制，默认 `3s`；完整请求总时长由 `proxy.total_timeout` 控制，默认 `30s`。
- `/proxy` 默认不自动跟随 HTTP redirect；请求传入 `follow_redirects=true` 后最多跟随 `proxy.max_redirects` 次，默认 `5`。

## 给 AI Agent 的最小说明

把下面这段提供给需要调用 Chijie 的 AI Agent：

```text
你可以通过 Chijie Proxy API 代发 HTTP 请求。

Base URL: <CHIJIE_BASE_URL>
Auth Header: Authorization: Bearer <CHIJIE_PROXY_TOKEN>

接口：
POST <CHIJIE_BASE_URL>/proxy
Content-Type: application/json

请求 JSON：
{
  "url": "https://target.example/path",
  "method": "GET",
  "headers": {},
  "payload": "",
  "follow_redirects": false,
  "egress": {
    "region": "US",
    "strategy": "least-latency",
    "residential": false,
    "tls_fingerprint": "chrome"
  }
}

规则：
- 不需要代理时省略 egress 或传空对象。
- 指定地区时使用二字母地区码；英国使用 GB，UK 也会被归一为 GB。
- 任意代理出口用 {"any": true}。
- 成功响应是目标站点的原始 status code 和响应体。
- Chijie 自身错误会返回 JSON：{"error":"...","detail":"..."}。
- 不要访问内网、回环或 metadata 地址；默认会被拒绝。
```
