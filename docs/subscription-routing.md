# 订阅节点、地区组与模板节点

日期：2026-05-04

更新：2026-07-01

## 目标

Chijie 当前只维护节点池。调用方在请求里声明 `egress.region`、`egress.strategy`、`egress.residential`、`egress.premium` 和 `egress.tls_fingerprint`，后端根据这些参数选择出口。

订阅源导入后，系统把节点保留在对应的 subscription 节点池中，并为每个节点计算地区组。普通节点进入 `US`、`HK`、`JP` 这类普通地区组；家宽节点进入 `US-RES`、`HK-RES`、`JP-RES` 这类家宽地区组；高端节点进入 `US-PREM` 这类高端地区组；高端家宽节点进入 `US-RES-PREM` 这类组合地区组。

## subscription 配置字段

`configs/nodes.yaml` 的 subscription 节点池支持：

- `url`：订阅地址。可使用换行、英文逗号或 `|` 填写多个地址。
- `update_interval`：自动刷新间隔，例如 `1h`、`12h`、`3d`；留空表示只手动刷新。
- `try_offline`：某地区只有一个离线订阅节点时，仍允许请求再尝试该节点。
- `reject_regex`：订阅节点屏蔽正则列表。
- `node_regions`：节点名到地区代码的修正表。
- `node_aliases`：别名到节点名的映射，供后台展示和人工识别使用。
- `node_tags`：节点名到标签列表的修正表，例如手动标记 `residential` 或 `premium`。
- `node_server_regions`：`server:port` 到地区代码的修正表，适合订阅节点名称变化但出口地址稳定的场景。
- `node_server_aliases`：`server:port` 到别名的映射。
- `node_server_tags`：`server:port` 到标签列表的映射。
- `region_group_names`：地区代码到分组展示名的映射。
- `tags`：节点池级标签。
- `residential`：池级家宽标识，设置后该池内节点默认进入家宽地区组。
- `premium`：池级高端标识，设置后该池内节点默认进入高端地区组。

订阅地址只允许 `http` / `https`，默认拒绝私网、回环、CGNAT 和保留地址。单次订阅响应 body 上限为 4 MB，超过后该订阅地址按失败处理。运行时已有旧节点时，自动刷新或配置重载的最新一次拉取失败不会清空该池节点，系统保留上一次成功拉取的节点并记录池级错误。

示例：

```yaml
node_pools:
  airport-a:
    source: subscription
    url: |
      https://example.com/sub-a
      https://example.com/sub-b
    update_interval: 1h
    try_offline: true
    reject_regex:
      - 流量|套餐|官网|剩余|到期
    node_regions:
      "US Los Angeles 01": US
    node_aliases:
      openai-us-primary: "US Los Angeles 01"
    node_tags:
      "US Residential 01": [residential]
      "US CF Bypass 01": [premium]
    node_server_aliases:
      "aws-link1.liangxin1.xyz:35248": hk-streaming-primary
    node_server_regions:
      "aws-link1.liangxin1.xyz:35248": HK
    node_server_tags:
      "aws-link1.liangxin1.xyz:35248": [streaming, premium]
    region_group_names:
      US: 美国出口
      JP: 日本出口
```

## 自动地区识别

自动地区识别读取节点名中的地区信息：

- 国旗，例如 `🇺🇸`、`🇯🇵`、`🇭🇰`
- 中文地区名，例如 `美国`、`日本`、`香港`
- 英文地区名或常见城市名，例如 `United States`、`Japan`、`Tokyo`
- 常见地区代码，例如 `US`、`JP`、`HK`

无法识别的节点进入 `UN` 分组。手动修正优先级高于自动识别。

## 家宽识别

节点进入家宽组的条件：

- 节点自身配置 `residential: true`。
- 节点池配置 `residential: true`。
- 节点标签包含 `residential`。
- 节点名包含家宽、住宅、residential、resi 等可识别字样。

普通请求不会选择家宽节点。家宽请求不会选择普通节点。

## 高端识别

节点进入高端组的条件：

- 节点自身配置 `premium: true`。
- 节点池配置 `premium: true`。
- 节点标签包含 `premium` 或 `high-end`。
- 节点名包含高端、premium、high-end 等可识别字样。

普通请求不会选择高端节点。高端请求只选择高端节点；如果同时设置 `residential=true`，则选择高端家宽节点。

## 出口选择

请求示例：

```json
{
  "url": "https://api.example.com/data",
  "method": "GET",
  "egress": {
    "region": "US",
    "strategy": "least-latency",
    "residential": false,
    "premium": false,
    "tls_fingerprint": "chrome"
  }
}
```

选择流程：

1. `region` 为空且 `premium` 不是 `true` 时使用直连出口。
2. `premium=true` 且 `region` 为空时选择任意高端非直连出口。
3. `region` 非空时标准化为大写二字母地区码。
4. `residential=false` 查找普通地区组，例如 `US`。
5. `residential=true` 查找家宽地区组，例如 `US-RES`。
6. `premium=true` 查找高端地区组，例如 `US-PREM` 或 `US-RES-PREM`。
7. 先从可用静态节点和订阅节点中选择。
8. 如果没有可用节点，且订阅池开启 `try_offline`，某地区只有一个离线订阅节点时会在模板前尝试该节点。
9. 地区组内没有可用节点时，使用同类型模板节点。
10. 普通请求没有普通节点和普通模板时，降级尝试同地区家宽节点和家宽模板；高端请求只在高端集合内降级。
11. 没有可用节点也没有可用模板时返回错误。

`strategy` 只在当前候选集合内生效：

- `random`：随机选择。
- `round-robin`：按地区组分别轮询。
- `least-latency`：选择健康检查延迟最低的节点。

## 模板节点

模板节点用于 Bright Data 这类按地区动态生成代理账号的出口。

普通模板示例：

```yaml
node_pools:
  brightdata:
    source: template
    type: http_proxy
    server: brd.superproxy.io
    port: 33335
    username_template: brd-customer-xxx-zone-yyy-country-{region}
    password: secret
```

家宽模板示例：

```yaml
node_pools:
  brightdata-res:
    source: template
    residential: true
    type: http_proxy
    server: brd.superproxy.io
    port: 33335
    username_template: brd-customer-xxx-zone-res-country-{region}
    password: secret
```

模板语义：

- 模板节点默认支持任意二字母地区码。
- 普通模板不服务家宽请求。
- 家宽模板优先服务家宽请求；普通请求没有普通节点和普通模板时，可降级使用同地区家宽模板。
- 静态节点或订阅节点不可用时，模板节点作为同类型兜底。
- 冷门地区不会提前出现在地区组列表中，但有模板时仍可处理。
- `{region}` 会替换成小写地区码，`{REGION}` 会替换成大写地区码。

## Admin API 返回

`GET /api/nodes` 返回节点池状态，其中每个节点包含：

- `region`
- `region_group`
- `residential`
- `premium`
- `alias`
- `tags`
- `enabled`
- `alive`
- `latency`
- `fail_count`

每个节点池还返回 `region_groups`：

- `group`：地区组代码，例如 `US`、`US-RES`、`US-PREM`。
- `region`：二字母地区码。
- `name`：展示名。
- `residential`：是否家宽组。
- `premium`：是否高端组。
- `count`：组内节点数。
- `online`：组内可用节点数。

当前内置 Web 页面仍是历史版本。前端重构后，节点管理页面应以地区组、订阅源和模板节点为中心。
