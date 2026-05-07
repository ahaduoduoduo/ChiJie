# TLS 指纹配置与测试

日期：2026-05-05

更新：2026-05-06

## 支持范围

TLS 指纹用于 `egress.tls_fingerprint` 和 Admin 的 TLS Profiles。

当前支持：

- 指纹库中的命名配置，例如 `chrome`。
- uTLS 内置预设：`chrome`、`firefox`、`safari`、`ios`、`edge`、`360`、`qq`、`random`。
- JA3 字符串。
- JA4 raw 列表：`JA4_a_cipher-list_extension-list_signature-list`。
- Akamai HTTP/2 raw 字符串。
- TLS / HTTP2 详细 raw 字段，用于保存从检测页复制的明细。
- 从检测站复制出的 JSON：支持 `tls.ja3`、`tls.ja4_r`、`http2.akamai_fingerprint`、`http2.sent_frames`、`http_version`、`method`、`user_agent`。
- YAML 或 JSON 格式的 `FingerprintConfig` 字符串。
- JA3 的额外参数：`extra` 和 curl_cffi 风格的 `extra_fp`。

当前不支持：

- 从 JA4 短格式字符串生成 ClientHello。`ja4` 字段只接受 raw 列表。
- HTTP/2 多路复用和连接池。当前 HTTP/2 指纹请求按单次请求建立连接，写入配置里的 SETTINGS、WINDOW_UPDATE、HEADERS 顺序并读取响应。

## 内置预设来源

`configs/fingerprints.yaml` 只保存 profile 名称和 preset 名称。实际 ClientHello 来自 `github.com/refraction-networking/utls` 的内置预设：

- `chrome` → `HelloChrome_Auto`
- `firefox` → `HelloFirefox_Auto`
- `safari` → `HelloSafari_Auto`
- `ios` → `HelloIOS_Auto`
- `edge` → `HelloEdge_Auto`
- `360` → `Hello360_Auto`
- `qq` → `HelloQQ_Auto`
- `random` → `HelloRandomized`

## extra 与 extra_fp

内部规范字段：

```yaml
ja3: "771,4865-4866-4867,0-23-65281-10-11-13,29-23-24,0"
extra:
  signature_algorithms:
    - ecdsa_secp256r1_sha256
    - rsa_pss_rsae_sha256
  cert_compression: zlib
  grease: true
```

curl_cffi 兼容字段：

```yaml
ja3: "771,4865-4866-4867,0-23-65281-10-11-13,29-23-24,0"
extra_fp:
  tls_signature_algorithms:
    - ecdsa_secp256r1_sha256
    - rsa_pss_rsae_sha256
  tls_cert_compression: zlib
  tls_grease: true
```

保存到 `configs/fingerprints.yaml` 时，`extra_fp` 会转换为内部 `extra` 字段。

## JA4 raw

`ja4` 字段可选。只填写 `ja3` 时按 JA3 构造；同时填写 `ja3` 和 `ja4` 时，握手仍以 JA3 为准，`ja4` 作为 raw 输入保存。测试时只显示远端返回的真实观测结果。

YAML：

```yaml
ja3: "771,4865-4866-4867-49196-49195-52393-49200-49199-52392-49162-49161-49172-49171,0-23-65281-10-11-16-5-13-18-51-45-43-27-21,29-23-24-25,0"
ja4: "t13d2014h2_000a,002f,0035,009c,009d,1301,1302,1303,c008,c009,c00a,c012,c013,c014,c02b,c02c,c02f,c030,cca8,cca9_0000,0005,000a,000b,000d,0012,0015,0017,001b,002b,002d,0033,ff01_0403,0804,0401,0503,0805,0805,0501,0806,0601,0201"
akamai: "HEADER_TABLE_SIZE=65536;ENABLE_PUSH=0;INITIAL_WINDOW_SIZE=6291456;MAX_HEADER_LIST_SIZE=262144|15663105|method,authority,scheme,path"
```

也支持检测站常见的 numeric Akamai raw：

```yaml
http_version: h2
akamai: "1:65536;2:0;4:6291456;6:262144|15663105|0|m,a,s,p"
```

JSON：

```json
{
  "ja3": "771,4865-4866-4867-49196-49195-52393-49200-49199-52392-49162-49161-49172-49171,0-23-65281-10-11-16-5-13-18-51-45-43-27-21,29-23-24-25,0",
  "ja4": "t13d2014h2_000a,002f,0035,009c,009d,1301,1302,1303,c008,c009,c00a,c012,c013,c014,c02b,c02c,c02f,c030,cca8,cca9_0000,0005,000a,000b,000d,0012,0015,0017,001b,002b,002d,0033,ff01_0403,0804,0401,0503,0805,0805,0501,0806,0601,0201",
  "akamai": "HEADER_TABLE_SIZE=65536;ENABLE_PUSH=0;INITIAL_WINDOW_SIZE=6291456;MAX_HEADER_LIST_SIZE=262144|15663105|method,authority,scheme,path"
}
```

只填写 JA4 raw 时，后端会根据 raw 中的 cipher、extension、signature algorithm 构造一个可发起握手的 uTLS spec。JA4 raw 不包含 supported groups、key share payload、padding 长度等所有 ClientHello 细节，这些缺省字段由后端按当前默认值补齐。

Chrome/BoringSSL raw 中常见的 `encrypted_client_hello (65037)`、`application_settings (17513/17613)`、`pre_shared_key (41)` 会映射到 uTLS 的结构化扩展。首次请求没有会话票据时，空 PSK 会被隐藏，避免生成非法 ClientHello。

## 详细 raw 字段

可以保存检测页里的详细字段。字段按 raw 文本保存，不做本地摘要计算：

```yaml
tls:
  protocols: ["h2", "http/1.1"]
  supported_versions: ["TLS 1.3", "TLS 1.2"]
  curves: ["X25519MLKEM768 (4588)", "X25519 (29)", "P-256 (23)", "P-384 (24)"]
  signature_algorithms:
    - ecdsa_secp256r1_sha256
    - rsa_pss_rsae_sha256
  extensions:
    - supported_versions (43)
    - application_layer_protocol_negotiation (16)
    - signature_algorithms (13)
  ciphers:
    - TLS_AES_128_GCM_SHA256
    - TLS_AES_256_GCM_SHA384
http2:
  settings:
    - HEADER_TABLE_SIZE = 65536
    - ENABLE_PUSH = 0
    - INITIAL_WINDOW_SIZE = 6291456
    - MAX_HEADER_LIST_SIZE = 262144
  window_update: "15663105"
  headers: ["method", "authority", "scheme", "path"]
```

也可以直接粘贴检测 JSON。导入时的主要映射关系：

- `tls.ja3` → `ja3`。
- `tls.ja4_r` → `ja4`，`tls.ja4` 短格式只作为远端观测值，不保存为本地配置。
- `http2.akamai_fingerprint` → `akamai`。
- `http2.sent_frames` 中的 SETTINGS、WINDOW_UPDATE、HEADERS → `http2.settings`、`http2.window_update`、`http2.header_lines` 和 `http2.priority`。
- `http_version`、`method`、`user_agent` 会保留，用于测试请求和代理请求的默认值。

## Admin 测试接口

`POST /api/fingerprints/test` 会对目标 HTTPS URL 发起真实 GET 请求，默认目标是：

```text
https://tls.browserleaks.com/json
```

接口返回：

- `status`：`ok` 或 `failed`。
- `http_status`：测试目标返回的 HTTP 状态码。
- `http_proto`：实际响应协议，例如 `HTTP/2.0`。
- `latency_ms`：测试请求耗时。
- `observed`：测试目标返回的 JSON 结果，例如 JA3、JA4、Akamai 或 HTTP/2 指纹字段。
- `error`：握手或请求失败时的错误文本。

这个测试只验证请求实际发出去后远端看到的结果，不返回本地计算的 JA3/JA4/Akamai 摘要。节点和模板出口连通性仍使用 `/api/nodes/test` 和 `/api/nodes/template/test`。

## 请求执行行为

`POST /proxy` 应用 TLS 指纹时，uTLS 握手使用当前选中的出口 `DialContext`。这保证 SOCKS5、HTTP Proxy、sing-box 适配的出口不会因为自定义 TLS 握手而改成直连。

未显式要求 HTTP/2 时，请求层仍使用 HTTP/1.1，并把 ALPN 限制为 `http/1.1`。显式要求 HTTP/2 的条件包括：

- `http_version: h2`。
- 配置了 `akamai`。
- 配置了 `http2` 明细。
- `tls.protocols` 包含 `h2`。
- JA4 raw 的 `JA4_a` 以 `h2` 结尾。

HTTP/2 模式会通过 uTLS 协商 `h2`，然后手写 HTTP/2 preface、SETTINGS、WINDOW_UPDATE 和 HEADERS。测试结果中的 JA3、JA4、Akamai 都来自远端检测页返回，不做本地 hash 计算。

`POST /proxy` 的普通 HTTP transport 和 HTTP/2 指纹 transport 都限制上游响应 body 为 32 MB，超过后返回读取响应失败。

`/tunnel` 访问 `wss://` 上游 WebSocket 目标时，会在网关侧执行上游 TLS 握手，因此可以通过当前出口应用请求级 TLS 指纹。`https://` raw TCP 隧道不接管客户端 TLS 握手。
