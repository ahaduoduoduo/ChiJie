# Traffic 持久化与请求折叠

更新：2026-09-02

## 持久化和轮换

Traffic 原始 trace 以 JSON Lines 格式写入配置目录下的 `.runtime/traffic/traffic-YYYY-MM-DD.jsonl`。写入使用单个进程内缓冲写入器，不依赖数据库或外部日志服务；日期变化时切换文件，启动和切换文件时清理过期日期。

```yaml
traffic:
  persistence:
    enabled: true
    retention_days: 7
```

- `enabled` 默认 `true`。
- `retention_days` 默认 `7`，范围为 1–3650。
- Admin System 页面的 Logging 卡片可修改保留天数；Traffic 页面只负责日志查看、加载和导出。
- 内存最多保留并向 Admin 提供最近 1000 条原始 trace；磁盘文件按配置保留完整日期范围。
- Docker 默认将 `/config` 映射到宿主机配置目录，因此 `.runtime` 会随同配置目录跨容器重启保留。

## 失败 URL 参数忽略

Traffic 失败合并默认按 `kind + url/target + egress_group` 生成分组键。对于带签名、时间戳或一次性 token 的 URL，可以在生成分组键前删除指定 query 参数，让同类失败请求合并到同一条展示记录。

规则只影响 Admin Traffic 统计和展示，不改变真实代理请求，也不修改原始 trace。展开 `xN` 后仍能看到每条真实 URL。

### Admin 编辑器

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

### YAML 结构

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

### 匹配规则

- `host_pattern` 按 `.` 拆分片段，`*` 只匹配一个域名片段。
- `path_pattern` 按 `/` 拆分片段，`*` 只匹配一个路径片段。
- `drop_keys` 删除精确同名 query key。
- 多条命中的规则会合并它们的 `drop_keys`。
- 删除参数后 query 会排序，避免参数顺序不同导致分组失败。

### 边界

- 未命中成功折叠规则的成功请求不合并。
- 未删除的 query 参数仍参与分组。
- 不建议把所有 query 参数都删除；`id`、`lb`、`region` 等稳定参数可能代表不同资源或线路。
- 规则保存时，同一 `host_pattern + path_pattern` 会合并 `drop_keys`，不会重复创建相同范围的规则。

## 200 成功请求折叠

对高频且通常成功的请求，可以在某条 200 请求详情中选择 `Fold successful path`，按 Host 和 Path 生成规则。Host / Path 片段支持与失败规则相同的 `*` 单片段通配。

```yaml
traffic:
  success_folding:
    enabled: true
    rules:
      - name: "frequent-search"
        match:
          host_pattern: "api.example.com"
          path_pattern: "/v1/search"
```

规则只匹配 `status == 200` 且没有错误文本的请求。命中后：

- 原始 trace 继续写入每日 JSONL 文件，Admin 中按 `kind + host/path + egress_group` 折叠为 `xN` 一行。
- Query 不参与成功折叠键；不同 Query 的相同 Host + Path 会进入同一行。
- 这些 200 请求不计入有效请求数、成功数、成功率、延迟、字节数和分钟序列。
- `raw_requests` 和 `ignored_success` 分别保留原始请求总数与被排除的成功请求数。
- 相同 Host + Path 的失败响应仍按失败规则统计，不受成功折叠规则影响。
