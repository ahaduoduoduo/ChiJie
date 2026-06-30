# 参数驱动出口模型

日期：2026-05-04

更新：2026-07-01

## 背景

Chijie 现有模型包含规则匹配、请求级覆盖、动态地区提取、规则级兜底出口等能力。新的业务判断是：代理调用方已经知道本次请求需要哪个出口，不需要网关再根据域名、路径、Header、Body 做自动分流。

当前后端已经简化为“参数驱动出口网关”：

- 代理请求自己携带出口参数。
- 后端只负责认证、出口选择、请求执行、TLS 指纹应用和流量记录。
- 管理后台只维护节点池、地区组、模板节点、TLS 指纹和请求日志。

## 已删除的核心概念

后端不再保留以下能力：

- 域名、路径、Header、Body 规则匹配。
- 规则动作里的 `direct` / `fixed` / `dynamic` 分流模式。
- 从请求字段动态提取地区。
- `route_mode` 的 `auto` / `override` / `force` 三种语义。
- 规则级 `fallback_proxy_group`。
- 规则测试匹配页面。

项目尚未部署，因此没有保留旧接口兼容层。

## 新请求模型

代理请求使用后台生成的 Bearer token：

```http
Authorization: Bearer <proxy_token>
```

代理请求由两部分组成：目标请求内容和出口选择参数。

目标请求内容：

- `url`：目标地址。
- `method`：请求方法，缺省为 `GET`。
- `headers`：目标请求 Header。
- `payload`：目标请求 Body。

出口选择参数：

- `region`：二字母地区码，例如 `US`、`HK`、`JP`、`TW`、`SG`、`NG`。为空表示直连。
- `any`：不关心地区但要求使用非直连出口时设为 `true`。也支持将 `region` 传为 `*`、`ANY` 或 `AUTO`。
- `max_latency_ms`：任意地区出口的延迟上限，依据最近健康检查延迟过滤；`0` 表示不限制。
- `strategy`：节点选择策略，支持 `least-latency`、`round-robin`、`random`。
- `residential`：是否使用家宽出口。
- `premium`：是否优先使用高端出口；用于访问开启 Cloudflare 防护、普通节点容易被阻挡的网站。
- `tls_fingerprint`：TLS 指纹字符串，可以是后台预设名、uTLS 预设名、JA3 raw、JA4 raw，或 YAML/JSON 格式的 `FingerprintConfig` 字符串。配置字符串可包含 Akamai raw、TLS 明细、HTTP/2 明细，也可直接使用检测站复制出的 JSON。

建议请求结构：

```json
{
  "url": "https://target.example/api",
  "method": "POST",
  "headers": {
    "Content-Type": "application/json"
  },
  "payload": "{\"hello\":\"world\"}",
  "egress": {
    "region": "US",
    "strategy": "least-latency",
    "residential": false,
    "premium": false,
    "tls_fingerprint": "chrome"
  }
}
```

地区无关出口示例：

```json
{
  "url": "https://api.ipify.org?format=json",
  "method": "GET",
  "egress": {
    "any": true,
    "strategy": "least-latency",
    "max_latency_ms": 100
  }
}
```

高端出口示例：

```json
{
  "url": "https://target.example",
  "method": "GET",
  "egress": {
    "premium": true,
    "strategy": "least-latency"
  }
}
```

当前后端只围绕 `egress` 设计，不再读取旧 `options` 字段。

## WebSocket 隧道模型

`/tunnel` 仍通过首帧 JSON 使用同一组 `egress` 参数。首帧字段与 `/proxy` 保持接近：

- `url`：目标地址，支持 `ws://`、`wss://`、`http://`、`https://`。
- `authorization`：可选，浏览器客户端无法在握手阶段设置 Header 时使用。
- `headers`：`ws://` / `wss://` 目标的上游 WebSocket 握手 Header。
- `payload`：`ws://` / `wss://` 目标连接成功后发往上游的第一条文本消息。
- `egress`：出口选择参数。

`ws://` / `wss://` 目标会由网关执行上游 WebSocket 握手，后续消息按 WebSocket 帧类型转发。`wss://` 目标支持通过当前出口应用 `tls_fingerprint`。`http://` / `https://` 目标保持 raw TCP 转发，客户端负责后续协议细节。

## 出口选择流程

1. 校验认证。
2. 解析目标请求内容。
3. 解析 `egress` 参数。
4. `region` 为空、`any` 不是 `true` 且 `premium` 不是 `true` 时，使用直连出口；如果提供 `tls_fingerprint`，仍然应用 TLS 指纹。
5. `any=true`、`premium=true` 或 `region` 为 `*`、`ANY`、`AUTO` 时，选择任意非直连静态/订阅出口；`type: direct` 静态节点也会被排除。
6. 任意地区出口设置 `max_latency_ms` 时，只保留最近健康检查延迟大于 0 且不超过阈值的节点。
7. `region` 不为空时，将地区码规范化为大写二字母地区码。
8. `residential=false` 时查找普通地区组，例如 `US`。
9. `residential=true` 时查找家宽地区组，例如 `US-RES`。
10. `premium=true` 时优先选择高端节点和高端模板；高端不可用或尝试失败时继续使用原有候选和 fallback。
11. 地区组内存在可用静态节点或订阅节点时，按 `strategy` 选择具体节点。
12. 开启 `proxy.template_fallback_after_attempts` 时，可用静态/订阅节点按 `proxy.max_attempts` 尝试失败后继续查找同类型模板节点；地区组内没有可用静态节点或订阅节点时也会查找同类型模板节点。
13. 找到模板节点后，按 `priority` 降序尝试；普通代理模板用请求地区码生成实际代理出口，Chijie 模板把原 `/proxy` 请求转发给远端 Chijie。
14. 普通请求没有普通节点和普通模板时，降级尝试同地区家宽节点和家宽模板；高端请求也保留这类原有 fallback。
15. 没有可用节点也没有可用模板时，返回明确错误。
16. 按 `proxy.response_header_timeout` 等待目标响应头，默认单个出口等待 `3s`；按 `proxy.total_timeout` 限制完整请求总时长，默认 `30s`；`follow_redirects=true` 时按 `proxy.max_redirects` 限制最大跳转次数，默认 `5`；并记录选择结果、状态码、耗时、错误和流量。
17. 如果非直连出口在建立连接或等待响应头阶段失败，继续换下一个候选出口；失败的静态/订阅节点立即标记为 `Alive=false`，源站已经返回 HTTP 状态码时不重试。

## 地区组模型

地区组是前端和后端共同使用的出口视图。

普通节点进入普通地区组：

- `US`
- `HK`
- `JP`
- `TW`
- `SG`

家宽节点进入家宽地区组：

- `US-RES`
- `HK-RES`
- `JP-RES`
- `TW-RES`
- `SG-RES`

高端不创建单独地区组。高端普通节点仍进入 `US`、`HK`、`JP`，高端家宽节点仍进入 `US-RES`、`HK-RES`、`JP-RES`。

地区组只由静态节点和订阅节点实际生成。模板节点默认不展开所有可能地区，因为模板节点支持任意二字母地区码，前端不应该显示无限地区列表。

前端展示建议：

- 地区组列表展示已有普通组和家宽组。
- 每个组展示在线节点数、总节点数、最低延迟、来源数量、是否有模板兜底。
- 模板覆盖能力单独展示为“普通模板可用”和“家宽模板可用”。
- 冷门地区请求不需要提前出现在地区组列表中，只要有对应类型模板即可处理。

## 节点来源

### 静态节点

静态节点由后台手动导入，适合固定代理、自建 VPS、本地 SOCKS5 或 HTTP Proxy。

静态节点需要字段：

- `name`
- `type`
- `server`
- `port`
- `username`
- `password`
- `extra`
- `region`
- `residential`
- `premium`
- `enabled`
- `tags`

`region` 可以由节点名自动识别，也可以手动修正。

### 订阅源

订阅源可以导入一个或多个订阅地址。系统解析订阅后生成节点，并按节点名识别地区。

订阅源需要支持：

- 多订阅地址。
- 自动地区识别。
- 地区手动修正。
- 节点别名。
- 节点标签。
- 家宽节点识别。
- 屏蔽正则。
- 刷新失败时保留已有可用节点。
- 错误信息不暴露订阅 token。

### 模板节点

模板节点用于 BrightData 这类按地区动态生成代理账号的出口。

模板节点默认支持任意二字母地区码，默认不支持家宽。

家宽模板需要单独导入，并在导入时声明：

```yaml
residential: true
```

模板节点分为两类：

- 普通模板：服务普通地区请求，例如 `US`、`NG`。
- 家宽模板：服务家宽地区请求，例如 `US-RES`、`NG-RES`。

模板节点用途：

- 地区兜底：例如 `US` 普通节点全部不可用时，使用普通模板生成 `US` 出口。
- 冷门地区动态出口：例如请求 `NG`，没有订阅节点提供尼日利亚，但普通模板可以生成 `NG` 出口。
- 家宽地区兜底：例如 `US-RES` 没有可用家宽节点时，使用家宽模板生成 `US` 家宽出口。
- 家宽冷门地区动态出口：例如请求 `NG` 且 `residential=true`，使用家宽模板生成 `NG` 家宽出口。

模板节点需要字段：

- `name`
- `type`
- `server`
- `port`
- `username_template`
- `password`
- `residential`
- `enabled`
- `tags`

普通模板不会服务家宽请求。家宽模板优先服务家宽请求；普通请求没有普通节点和普通模板时，可降级使用同地区家宽模板。

## 策略语义

`strategy` 只在同一候选集合内生效。

候选集合分两段：

1. 可用静态节点和订阅节点。
2. 同类型模板节点。
3. 普通请求的同地区家宽节点或家宽模板。

选择优先级：

1. 先选择可用静态节点和订阅节点。
2. 如果候选集合为空，再选择同类型模板节点。
3. 普通请求仍没有候选时，再选择同地区家宽出口。

策略说明：

- `random`：随机选择候选节点。
- `round-robin`：按地区组和家宽类型分别轮询。
- `least-latency`：选择最近健康检查延迟最低的节点。

模板节点有多个时，同样使用 `strategy` 在可用模板中选择。

## TLS 指纹语义

TLS 指纹从规则动作配置改为请求参数配置。

请求传入：

- 空字符串：使用默认 TLS。
- 预设名称：从后台指纹库读取，例如 `chrome`、`ios`、`safari`。
- JA3 字符串：后端按 JA3 解析。
- JA4 raw 列表：后端按 raw 中的 cipher、extension、signature algorithm 构造 uTLS spec，并补齐 JA4 不包含的默认字段。
- Akamai raw 字符串：作为 HTTP/2 指纹 raw 配置保存，测试结果以远端返回的真实字段为准。
- 其他指纹参数字符串：后端按约定格式解析，无法解析时返回明确错误。

后台维护：

- 预设指纹。
- 自定义指纹名称。
- 指纹字符串。
- 指纹测试结果。

`region` 为空时仍然可以应用 TLS 指纹。此时请求不走代理节点，只通过直连出口访问目标。

## 管理后台需求

新的后台不再以规则编辑器为中心。

建议页面：

- `Overview`：系统运行状态、可用地区组、模板可用性、请求成功率、近期错误。
- `Egress`：地区组、普通节点、家宽节点、静态节点、订阅节点和模板节点。
- `Subscriptions`：订阅源导入、刷新、屏蔽正则、地区修正、节点别名。
- `Templates`：普通模板和家宽模板管理。
- `TLS Profiles`：预设和自定义 TLS 指纹管理。
- `Traffic`：请求记录、出口选择结果、错误信息、耗时和流量。
- `System`：认证、热重载、配置状态、版本信息。

关键交互：

- 地区组优先展示。
- 家宽和普通节点明确分开。
- 模板能力作为地区组的补充信息展示。
- 节点详情默认隐藏敏感字段。
- 所有删除、禁用、刷新订阅等操作需要站内确认。

## 当前 Admin API

Admin API 围绕节点池和运行状态设计。

当前保留：

- 获取节点池状态。
- 新增、更新、删除节点池。
- 新增、更新、删除静态节点。
- 刷新订阅源。
- 启用或禁用节点。
- 更新订阅节点地区、别名、标签。
- 管理普通模板节点和家宽模板节点。
- 通过 `GET /api/nodes` 获取地区组状态。
- 管理 TLS 指纹。
- 获取流量日志。
- 获取系统统计。

已移除：

- 规则 CRUD。
- 规则测试。
- 规则热加载。

## 错误语义

需要明确区分以下错误：

- `region` 不是二字母地区码。
- 没有可用普通节点，也没有普通模板。
- 没有可用家宽节点，也没有家宽模板。
- TLS 指纹名称不存在。
- TLS 指纹字符串无法解析。
- 节点连接失败。
- 目标请求失败。
- 订阅刷新失败。

错误返回需要包含可展示给后台的短消息，也需要包含便于日志排查的内部原因。

## 当前实现状态

已完成：

1. 新增 `egress` 请求参数，不保留旧 `options`。
2. 后端使用参数驱动出口选择，不再依赖规则引擎。
3. 节点池增加普通模板和家宽模板语义。
4. `GET /api/nodes` 返回地区组和节点状态。
5. 文档更新请求协议、目录说明和开发计划。
6. 规则系统后端代码和 `configs/rules.yaml` 已删除。

待处理：

1. 前端移除历史规则编辑器。
2. 前端改为地区组、节点池、订阅源、模板节点和 TLS 指纹管理。
3. 使用真实代理节点验证普通地区、家宽地区、模板兜底和 TLS 指纹。
