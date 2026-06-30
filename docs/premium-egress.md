# 高端出口节点

日期：2026-07-01

## 目标

高端出口用于访问开启 Cloudflare 防护、会阻挡普通代理节点的网站。`premium` 是节点质量标记，不是新的地区组。调用方在 `/proxy` 或 `/tunnel` 的 `egress` 中传入 `premium: true` 后，网关优先选择高端节点；高端不可用时继续使用原有普通节点、家宽节点或模板 fallback。

## 配置入口

静态节点可直接配置：

```yaml
node_pools:
  us-fleet:
    source: static
    nodes:
      - name: us-premium-01
        type: socks5
        server: premium-us.example.com
        port: 1080
        region: US
        premium: true
```

订阅池可设置池级高端标识，池下节点默认都是高端节点：

```yaml
node_pools:
  premium-airport:
    source: subscription
    url: https://provider.example/sub
    update_interval: 1h
    premium: true
```

订阅节点也可单独通过标签标记：

```yaml
node_pools:
  airport:
    source: subscription
    url: https://provider.example/sub
    node_tags:
      "US CF Bypass 01": [premium]
    node_server_tags:
      "premium-us.example.com:443": [premium]
```

Admin 页面中：

- Egress 节点编辑器可为单个静态节点或订阅节点设置 `Premium`。
- Subscriptions 源配置可为整个订阅池设置 `Premium pool`。
- Tags 页签仍可直接添加 `premium` 标签。

## 请求方式

```json
{
  "url": "https://target.example",
  "method": "GET",
  "egress": {
    "premium": true,
    "strategy": "least-latency",
    "tls_fingerprint": "chrome"
  }
}
```

行为规则：

- `premium=true` 且未指定 `region` 时，会选择任意非直连出口，不走直连，并优先尝试高端节点。
- 指定 `region` 时，在该地区组内优先选择高端节点；没有可用高端节点时继续使用该地区原有候选。
- `strategy` 在当前候选集合内生效；`premium=true` 时高端节点和高端模板排在前面，普通候选保留为 fallback。
- `residential=true` 与 `premium=true` 可组合，表示优先选择高端家宽节点；没有高端家宽时回到普通家宽候选。
- 普通请求也可以使用高端节点。高端节点只是全部节点里更好的一批，不会被普通请求排除。

## 地区组

高端不创建单独地区组：

- 普通：`US`
- 家宽：`US-RES`
- 任意地区：`ANY`

高端节点仍然归入原地区组，例如美国高端普通节点仍在 `US`，美国高端家宽节点仍在 `US-RES`。Admin 只在节点名后显示黄色圆点表示该节点是高端节点。
