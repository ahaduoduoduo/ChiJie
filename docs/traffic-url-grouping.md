# Traffic URL 参数忽略规则

Traffic 失败合并默认按 `kind + url/target + egress_group` 生成分组键。对于带签名、时间戳或一次性 token 的 URL，可以在生成分组键前删除指定 query 参数，让同类失败请求合并到同一条展示记录。

规则只影响 Admin Traffic 统计和展示，不改变真实代理请求，也不修改原始 trace。展开 `xN` 后仍能看到每条真实 URL。

## Admin 编辑器

在错误请求详情中点击 `Ignore URL params`：

1. Host 和 Path 会拆成多个片段按钮。
2. 点击片段按钮后，该片段变成 `*`，表示任意片段。
3. Query 参数列表中勾选需要忽略的 key。
4. 保存后规则写入 `gateway.yaml`，并立即更新运行时 Traffic Store。

示例：

```text
host: *.pipecdn.vip
path: /ppot/_definst_/*/lvod/*/chunklist.m3u8
drop_keys: vendtime, vhash
```

该规则可以覆盖同业务结构下不同 CDN 前缀、不同视频文件名和不同签名参数的 HLS 播放列表请求。

## YAML 结构

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

## 匹配规则

- `host_pattern` 按 `.` 拆分片段，`*` 只匹配一个域名片段。
- `path_pattern` 按 `/` 拆分片段，`*` 只匹配一个路径片段。
- `drop_keys` 删除精确同名 query key。
- 多条命中的规则会合并它们的 `drop_keys`。
- 删除参数后 query 会排序，避免参数顺序不同导致分组失败。

## 边界

- 成功请求不合并。
- 未删除的 query 参数仍参与分组。
- 不建议把所有 query 参数都删除；`id`、`lb`、`region` 等稳定参数可能代表不同资源或线路。
- 规则保存时，同一 `host_pattern + path_pattern` 会合并 `drop_keys`，不会重复创建相同范围的规则。
