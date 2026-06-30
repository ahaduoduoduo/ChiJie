# 高端出口节点

日期：2026-07-01

## 目标

高端出口用于访问开启 Cloudflare 防护、会阻挡普通代理节点的网站。调用方在 `/proxy` 或 `/tunnel` 的 `egress` 中传入 `premium: true` 后，网关只从高端节点集合里选择出口。

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

- `premium=true` 且未指定 `region` 时，会选择任意高端静态/订阅出口，不走直连。
- 指定 `region` 时，只在该地区的高端节点或高端模板中选择。
- `strategy` 仍只在高端候选集合内生效。
- `residential=true` 与 `premium=true` 可组合，组合后选择高端家宽组，例如 `US-RES-PREM`。
- 普通请求不会自动使用高端节点，避免高成本节点被默认流量消耗。

## 地区组

地区组后缀：

- 普通：`US`
- 家宽：`US-RES`
- 高端：`US-PREM`
- 高端家宽：`US-RES-PREM`
- 任意高端：`ANY-PREM`

失败流量合并、Traffic 展示和 Admin 区域组展示都会使用这些组名。
