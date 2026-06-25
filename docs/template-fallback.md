# 模板 fallback 与远端 Chijie

日期：2026-06-25

## 目标

模板池用于本地静态节点和订阅节点无法覆盖某个地区时的兜底出口，也可在同地区可用节点连续执行失败后作为延后兜底。模板候选按 `priority` 从高到低尝试，适合配置 `Chijie -> BrightData -> Lumi` 这类成本优先级。

## 模板类型

### Generic proxy

`template_type` 为空或为 `proxy` 时，模板按地区动态生成一个普通代理节点。

```yaml
node_pools:
  brightdata:
    source: template
    template_type: proxy
    type: http_proxy
    server: brd.superproxy.io
    port: 33335
    username_template: brd-customer-xxx-zone-yyy-country-{region}
    password: secret
    priority: 50
    coverage: normal
```

### Chijie

`template_type: chijie` 时，本机不会直接请求目标站点，而是把当前 `/proxy` 请求转发给另一个 Chijie 的 Proxy API。远端地址只允许 HTTPS。

```yaml
node_pools:
  chijie-b:
    source: template
    template_type: chijie
    endpoint: https://b.example.com
    bearer_token: remote-proxy-token
    priority: 100
    coverage: both
```

转发时，URL、method、headers、payload、egress 参数保持不变，只替换 HTTP 请求头里的 `Authorization: Bearer <remote-proxy-token>`。

## 字段

- `template_type`：`proxy` 或 `chijie`。空值等同于 `proxy`。
- `priority`：模板优先级，数值越大越先尝试。
- `coverage`：`normal`、`residential` 或 `both`。空值按旧配置的 `residential` 布尔值判断；Chijie 默认 `both`。
- `endpoint`：远端 Chijie HTTPS 地址。可只填域名，系统按 HTTPS 处理。
- `port`：远端 Chijie 端口，可选；标准 HTTPS 反代可不填。
- `bearer_token`：远端 Chijie 的 Proxy token，不是 Admin token。

## 选择顺序

1. 请求指定 `egress.region`。
2. 本地静态节点和订阅节点存在可用候选时，按请求 `strategy` 排序。
3. `/proxy` 先尝试 `proxy.max_attempts` 个可用节点，默认 5 个；失败节点会立即标记为 `Alive=false`。
4. 开启 `proxy.template_fallback_after_attempts` 时，可用节点都失败后进入模板 fallback；本地无可用候选时也直接进入模板 fallback。
5. 模板按 `priority` 降序尝试；同优先级按池名排序。
6. Chijie 模板返回网关错误时尝试下一个模板。
7. 目标站点已经返回 HTTP 状态码时不尝试下一个模板。
8. 普通请求没有普通节点和普通模板时，继续尝试同地区家宽节点和家宽模板。

## 测试行为

Admin 的 `Test region` 按钮使用同一个接口：

- 普通代理模板：本机按地区生成临时代理节点后访问测试 URL。
- Chijie 模板：本机向远端 Chijie 的 `/proxy` 发起测试请求，`egress.region` 使用界面输入的地区，`strategy` 固定为 `least-latency`，默认测试 URL 为 `https://api.ipify.org?format=json`。

Chijie 模板测试只替换远端 Bearer，不会使用 Admin token。

## 循环保护

Chijie 之间转发会添加 `X-Chijie-Hop` 请求头。超过最大跳数时返回 `508`，避免 A 和 B 互相配置后无限转发。
