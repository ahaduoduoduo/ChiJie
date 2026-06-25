package pool

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"chijie/internal/dialer"
	"chijie/internal/util"

	"gopkg.in/yaml.v3"
)

// PoolConfig 节点池配置
type PoolConfig struct {
	Source            string              `yaml:"source" json:"source"` // direct, static, template, subscription
	Enabled           *bool               `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Residential       bool                `yaml:"residential,omitempty" json:"residential,omitempty"`
	URL               string              `yaml:"url" json:"url"`                                         // 订阅链接（subscription）
	UpdateInterval    string              `yaml:"update_interval" json:"update_interval"`                 // 订阅更新间隔
	Filter            *FilterConfig       `yaml:"filter" json:"filter"`                                   // 节点过滤
	HealthCheck       *HealthCheckConfig  `yaml:"health_check" json:"health_check"`                       // 健康检查配置
	TryOffline        bool                `yaml:"try_offline,omitempty" json:"try_offline,omitempty"`     // 唯一地区节点离线时仍尝试
	Nodes             []dialer.Node       `yaml:"nodes" json:"nodes"`                                     // 静态节点列表
	DisabledNodes     []string            `yaml:"disabled_nodes" json:"disabled_nodes"`                   // 禁用节点名
	TemplateType      string              `yaml:"template_type,omitempty" json:"template_type,omitempty"` // proxy, chijie
	Type              string              `yaml:"type" json:"type"`                                       // 代理模板类型（template）
	Server            string              `yaml:"server" json:"server"`                                   // 模板服务器
	Port              int                 `yaml:"port" json:"port"`                                       // 模板端口
	Endpoint          string              `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`           // 远端 Chijie HTTPS 地址
	BearerToken       string              `yaml:"bearer_token,omitempty" json:"bearer_token,omitempty"`
	UsernameTemplate  string              `yaml:"username_template" json:"username_template"` // 用户名模板
	Password          string              `yaml:"password" json:"password"`                   // 模板密码
	Priority          int                 `yaml:"priority,omitempty" json:"priority,omitempty"`
	Coverage          string              `yaml:"coverage,omitempty" json:"coverage,omitempty"` // normal, residential, both
	Tags              []string            `yaml:"tags,omitempty" json:"tags,omitempty"`         // 节点池标签
	RejectRegex       []string            `yaml:"reject_regex,omitempty" json:"reject_regex,omitempty"`
	NodeRegions       map[string]string   `yaml:"node_regions,omitempty" json:"node_regions,omitempty"`               // 节点名 → 地区代码
	NodeAliases       map[string]string   `yaml:"node_aliases,omitempty" json:"node_aliases,omitempty"`               // 别名 → 节点名
	NodeTags          map[string][]string `yaml:"node_tags,omitempty" json:"node_tags,omitempty"`                     // 节点名 → 标签列表
	NodeServerRegions map[string]string   `yaml:"node_server_regions,omitempty" json:"node_server_regions,omitempty"` // server:port → 地区代码
	NodeServerAliases map[string]string   `yaml:"node_server_aliases,omitempty" json:"node_server_aliases,omitempty"` // server:port → 别名
	NodeServerTags    map[string][]string `yaml:"node_server_tags,omitempty" json:"node_server_tags,omitempty"`       // server:port → 标签列表
	RegionGroupNames  map[string]string   `yaml:"region_group_names,omitempty" json:"region_group_names,omitempty"`   // 地区代码 → 分组展示名
}

type FilterConfig struct {
	Region []string `yaml:"region" json:"region"`
}

type HealthCheckConfig struct {
	Interval string `yaml:"interval" json:"interval"`
	URL      string `yaml:"url" json:"url"`
	Timeout  string `yaml:"timeout" json:"timeout"`
	MaxFail  int    `yaml:"max_fail" json:"max_fail"`
}

// NodeEntry 节点池中的节点条目
type NodeEntry struct {
	Node        *dialer.Node
	Dialer      dialer.Dialer
	Enabled     bool
	Alive       bool
	Latency     time.Duration // 最近一次健康检查延迟
	FailCount   int           // 连续失败次数
	Region      string
	Alias       string
	Residential bool
	Tags        []string
}

// Pool 节点池
type Pool struct {
	Name    string
	Config  *PoolConfig
	Entries []*NodeEntry
	Error   string
	mu      sync.RWMutex
}

// Manager 节点池管理器
type Manager struct {
	pools     map[string]*Pool
	mu        sync.RWMutex
	rrMu      sync.Mutex
	rrByGroup map[string]int

	// 订阅自动更新协程的生命周期管理
	updaterMu     sync.Mutex
	updaterCancel context.CancelFunc
	updaterWG     sync.WaitGroup
}

// NodesFileConfig nodes.yaml 顶层结构
type NodesFileConfig struct {
	NodePools map[string]*PoolConfig `yaml:"node_pools"`
}

// EgressChoice 是参数驱动出口选择的结果。
type EgressChoice struct {
	Dialer       dialer.Dialer
	PoolName     string        `json:"pool"`
	NodeName     string        `json:"node"`
	Source       string        `json:"source"`
	TemplateType string        `json:"template_type,omitempty"`
	Region       string        `json:"region"`
	Group        string        `json:"group"`
	Residential  bool          `json:"residential"`
	Template     bool          `json:"template"`
	Priority     int           `json:"priority,omitempty"`
	Endpoint     string        `json:"endpoint,omitempty"`
	BearerToken  string        `json:"-"`
	Latency      time.Duration `json:"-"`
}

// NewManager 创建节点池管理器
func NewManager() *Manager {
	return &Manager{
		pools:     make(map[string]*Pool),
		rrByGroup: make(map[string]int),
	}
}

// ParseDurationWithDays 兼容 Go duration，并额外支持 d 表示天。
func ParseDurationWithDays(raw string) (time.Duration, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return 0, fmt.Errorf("duration is empty")
	}
	if strings.HasSuffix(value, "d") {
		days, err := strconv.ParseFloat(strings.TrimSuffix(value, "d"), 64)
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid duration: %s", raw)
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	return time.ParseDuration(value)
}

// LoadFromFile 从 YAML 文件加载节点池配置
func (m *Manager) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read nodes config: %w", err)
	}

	var config NodesFileConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parse nodes config: %w", err)
	}

	m.mu.RLock()
	oldPools := make(map[string]*Pool, len(m.pools))
	for name, oldPool := range m.pools {
		oldPools[name] = oldPool
	}
	m.mu.RUnlock()

	newPools := make(map[string]*Pool, len(config.NodePools))
	for name, poolCfg := range config.NodePools {
		pool, err := m.buildPool(name, poolCfg)
		if err != nil {
			return fmt.Errorf("build pool %s: %w", name, err)
		}
		if preserved := preserveSubscriptionEntriesOnError(name, poolCfg, pool, previousSubscriptionPool(name, poolCfg, oldPools)); preserved != nil {
			pool = preserved
		}
		newPools[name] = pool
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.pools = newPools

	return nil
}

// buildPool 根据配置构建节点池
func (m *Manager) buildPool(name string, cfg *PoolConfig) (*Pool, error) {
	pool := &Pool{
		Name:   name,
		Config: cfg,
	}

	switch cfg.Source {
	case "direct":
		node := &dialer.Node{Name: "direct", Type: "direct"}
		d := dialer.NewDirectDialer()
		pool.Entries = []*NodeEntry{newNodeEntry(node, d, cfg)}

	case "static":
		for i := range cfg.Nodes {
			node := &cfg.Nodes[i]
			d, err := dialer.NewDialer(node)
			if err != nil {
				return nil, fmt.Errorf("create dialer for %s: %w", node.Name, err)
			}
			pool.Entries = append(pool.Entries, newNodeEntry(node, d, cfg))
		}

	case "template":
		if NormalizeTemplateType(cfg.TemplateType) == "chijie" {
			if _, err := ChijieProxyURL(cfg.Endpoint, cfg.Port); err != nil {
				return nil, fmt.Errorf("invalid chijie template endpoint: %w", err)
			}
			if strings.TrimSpace(cfg.BearerToken) == "" {
				return nil, fmt.Errorf("chijie template bearer_token is required")
			}
		}
		// 模板池不预创建节点，按需动态生成。这里只保存配置。

	case "subscription":
		parser := NewSubscriptionParser()
		nodes, err := parser.Fetch(cfg.URL)
		if err != nil {
			pool.Error = fmt.Sprintf("fetch subscription: %v", err)
			log.Printf("pool %s: %s", name, pool.Error)
			return pool, nil
		}

		// 按地区过滤
		if cfg.Filter != nil && len(cfg.Filter.Region) > 0 {
			nodes = filterByRegion(nodes, cfg.Filter.Region)
		}
		var filterErr error
		nodes, filterErr = applyRejectRegexes(nodes, cfg.RejectRegex)
		if filterErr != nil {
			pool.Error = filterErr.Error()
			log.Printf("pool %s: %s", name, pool.Error)
			return pool, nil
		}

		entries, warning := buildSubscriptionEntries(nodes, cfg)
		pool.Entries = entries
		pool.Error = warning
		log.Printf("pool %s: loaded %d nodes from subscription", name, len(pool.Entries))

	default:
		return nil, fmt.Errorf("unknown pool source: %s", cfg.Source)
	}

	return pool, nil
}

func previousSubscriptionPool(name string, cfg *PoolConfig, oldPools map[string]*Pool) *Pool {
	if cfg == nil || cfg.Source != "subscription" {
		return nil
	}
	if previous := oldPools[name]; previous != nil && previous.Config != nil && previous.Config.Source == "subscription" {
		return previous
	}
	for _, previous := range oldPools {
		if previous == nil || previous.Config == nil || previous.Config.Source != "subscription" {
			continue
		}
		if strings.TrimSpace(previous.Config.URL) == strings.TrimSpace(cfg.URL) {
			return previous
		}
	}
	return nil
}

func preserveSubscriptionEntriesOnError(name string, cfg *PoolConfig, current *Pool, previous *Pool) *Pool {
	if cfg == nil || cfg.Source != "subscription" || current == nil || current.Error == "" || previous == nil {
		return nil
	}
	previous.mu.RLock()
	defer previous.mu.RUnlock()
	if len(previous.Entries) == 0 {
		return nil
	}
	entries := make([]*NodeEntry, 0, len(previous.Entries))
	for _, entry := range previous.Entries {
		if entry == nil || entry.Node == nil {
			continue
		}
		next := newNodeEntry(entry.Node, entry.Dialer, cfg)
		next.Alive = entry.Alive
		next.Latency = entry.Latency
		next.FailCount = entry.FailCount
		entries = append(entries, next)
	}
	if len(entries) == 0 {
		return nil
	}
	log.Printf("pool %s: keeping %d previous subscription nodes after refresh error: %s", name, len(entries), current.Error)
	return &Pool{
		Name:    name,
		Config:  cfg,
		Entries: entries,
		Error:   current.Error,
	}
}

// SelectEgress 按地区、策略和家宽要求选择出口。普通节点优先，模板节点作为兜底。
func (m *Manager) SelectEgress(region string, strategy string, residential bool) (*EgressChoice, error) {
	choices, err := m.SelectEgressCandidates(region, strategy, residential)
	if err != nil {
		return nil, err
	}
	return choices[0], nil
}

// SelectEgressCandidates 返回按策略排序后的出口候选。请求普通出口时，如果普通节点和模板都不可用，
// 会降级到同地区家宽节点或家宽模板。
func (m *Manager) SelectEgressCandidates(region string, strategy string, residential bool) ([]*EgressChoice, error) {
	region = NormalizeRegionCode(region)
	if region == "" {
		return nil, fmt.Errorf("region must be a two-letter region code")
	}
	strategy = NormalizeStrategy(strategy)

	if choices := m.egressCandidateGroup(region, strategy, residential, false); len(choices) > 0 {
		return choices, nil
	}

	if !residential {
		if choices := m.egressCandidateGroup(region, strategy, true, false); len(choices) > 0 {
			return choices, nil
		}
	}

	if residential {
		return nil, fmt.Errorf("no residential nodes or residential templates available for region %s", region)
	}
	return nil, fmt.Errorf("no nodes or templates available for region %s", region)
}

// SelectEgressCandidatesWithTemplateFallback 返回显式地区出口候选，并在可用节点候选之后追加同类型模板。
// /proxy 会限制普通节点尝试数量；模板候选只在这些节点都失败后继续使用。
func (m *Manager) SelectEgressCandidatesWithTemplateFallback(region string, strategy string, residential bool) ([]*EgressChoice, error) {
	region = NormalizeRegionCode(region)
	if region == "" {
		return nil, fmt.Errorf("region must be a two-letter region code")
	}
	strategy = NormalizeStrategy(strategy)

	if choices := m.egressCandidateGroup(region, strategy, residential, true); len(choices) > 0 {
		return choices, nil
	}

	if !residential {
		if choices := m.egressCandidateGroup(region, strategy, true, true); len(choices) > 0 {
			return choices, nil
		}
	}

	if residential {
		return nil, fmt.Errorf("no residential nodes or residential templates available for region %s", region)
	}
	return nil, fmt.Errorf("no nodes or templates available for region %s", region)
}

func (m *Manager) egressCandidateGroup(region string, strategy string, residential bool, includeTemplatesAfterEntries bool) []*EgressChoice {
	group := EgressGroup(region, residential)
	templates := func() []*EgressChoice {
		return m.templateChoices(region, residential)
	}

	if choices := m.orderEgressChoices(m.entryChoices(region, residential), strategy, group); len(choices) > 0 {
		if includeTemplatesAfterEntries {
			return append(choices, templates()...)
		}
		return choices
	}
	if choices := m.orderEgressChoices(m.tryOfflineEntryChoices(region, residential), strategy, group); len(choices) > 0 {
		if includeTemplatesAfterEntries {
			return append(choices, templates()...)
		}
		return choices
	}
	return templates()
}

// SelectAnyEgress 在不指定地区时选择一个非直连出口。maxLatency 为 0 时不限制延迟。
func (m *Manager) SelectAnyEgress(strategy string, residential bool, maxLatency time.Duration) (*EgressChoice, error) {
	choices, err := m.SelectAnyEgressCandidates(strategy, residential, maxLatency)
	if err != nil {
		return nil, err
	}
	return choices[0], nil
}

// SelectAnyEgressCandidates 在不指定地区时返回按策略排序后的非直连候选。maxLatency 为 0 时不限制延迟。
func (m *Manager) SelectAnyEgressCandidates(strategy string, residential bool, maxLatency time.Duration) ([]*EgressChoice, error) {
	strategy = NormalizeStrategy(strategy)
	group := AnyEgressGroup(residential)
	if choices := m.orderEgressChoices(m.anyEntryChoices(residential, maxLatency), strategy, group); len(choices) > 0 {
		return choices, nil
	}

	if maxLatency > 0 {
		return nil, fmt.Errorf("no %s nodes available under %dms", group, maxLatency.Milliseconds())
	}
	return nil, fmt.Errorf("no %s nodes available", group)
}

func (m *Manager) entryChoices(region string, residential bool) []*EgressChoice {
	m.mu.RLock()
	defer m.mu.RUnlock()

	choices := make([]*EgressChoice, 0)
	for poolName, pool := range m.pools {
		if pool.Config.Source != "static" && pool.Config.Source != "subscription" {
			continue
		}
		if !poolEnabled(pool.Config) {
			continue
		}

		pool.mu.RLock()
		for _, entry := range pool.Entries {
			if !entry.Enabled || !entry.Alive {
				continue
			}
			if entry.Region != region || entry.Residential != residential {
				continue
			}
			choices = append(choices, &EgressChoice{
				Dialer:      entry.Dialer,
				PoolName:    poolName,
				NodeName:    entry.Node.Name,
				Source:      pool.Config.Source,
				Region:      region,
				Group:       EgressGroup(region, residential),
				Residential: residential,
				Latency:     entry.Latency,
			})
		}
		pool.mu.RUnlock()
	}
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].PoolName == choices[j].PoolName {
			return choices[i].NodeName < choices[j].NodeName
		}
		return choices[i].PoolName < choices[j].PoolName
	})
	return choices
}

func (m *Manager) tryOfflineEntryChoices(region string, residential bool) []*EgressChoice {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var allMatching int
	var fallback *EgressChoice
	for poolName, pool := range m.pools {
		if pool.Config == nil || (pool.Config.Source != "static" && pool.Config.Source != "subscription") {
			continue
		}
		if !poolEnabled(pool.Config) {
			continue
		}

		pool.mu.RLock()
		for _, entry := range pool.Entries {
			if entry == nil || entry.Node == nil || !entry.Enabled {
				continue
			}
			if entry.Region != region || entry.Residential != residential {
				continue
			}
			allMatching++
			if pool.Config.Source == "subscription" && pool.Config.TryOffline && !entry.Alive {
				fallback = &EgressChoice{
					Dialer:      entry.Dialer,
					PoolName:    poolName,
					NodeName:    entry.Node.Name,
					Source:      pool.Config.Source,
					Region:      region,
					Group:       EgressGroup(region, residential),
					Residential: residential,
					Latency:     entry.Latency,
				}
			}
		}
		pool.mu.RUnlock()
	}
	if allMatching == 1 && fallback != nil {
		return []*EgressChoice{fallback}
	}
	return nil
}

func (m *Manager) anyEntryChoices(residential bool, maxLatency time.Duration) []*EgressChoice {
	m.mu.RLock()
	defer m.mu.RUnlock()

	group := AnyEgressGroup(residential)
	choices := make([]*EgressChoice, 0)
	for poolName, pool := range m.pools {
		if pool.Config == nil || (pool.Config.Source != "static" && pool.Config.Source != "subscription") {
			continue
		}
		if !poolEnabled(pool.Config) {
			continue
		}

		pool.mu.RLock()
		for _, entry := range pool.Entries {
			if !entry.Enabled || !entry.Alive {
				continue
			}
			if entry.Node != nil && strings.EqualFold(entry.Node.Type, "direct") {
				continue
			}
			if entry.Residential != residential {
				continue
			}
			if maxLatency > 0 && (entry.Latency <= 0 || entry.Latency > maxLatency) {
				continue
			}
			choices = append(choices, &EgressChoice{
				Dialer:      entry.Dialer,
				PoolName:    poolName,
				NodeName:    entry.Node.Name,
				Source:      pool.Config.Source,
				Region:      entry.Region,
				Group:       group,
				Residential: residential,
				Latency:     entry.Latency,
			})
		}
		pool.mu.RUnlock()
	}
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].Latency != choices[j].Latency {
			if choices[i].Latency == 0 {
				return false
			}
			if choices[j].Latency == 0 {
				return true
			}
			return choices[i].Latency < choices[j].Latency
		}
		if choices[i].PoolName == choices[j].PoolName {
			return choices[i].NodeName < choices[j].NodeName
		}
		return choices[i].PoolName < choices[j].PoolName
	})
	return choices
}

func (m *Manager) templateChoices(region string, residential bool) []*EgressChoice {
	m.mu.RLock()
	defer m.mu.RUnlock()

	choices := make([]*EgressChoice, 0)
	for poolName, pool := range m.pools {
		if pool.Config.Source != "template" || !poolEnabled(pool.Config) || !TemplateCoversResidential(pool.Config, residential) {
			continue
		}

		templateType := NormalizeTemplateType(pool.Config.TemplateType)
		endpoint := ""
		var d dialer.Dialer
		if templateType == "chijie" {
			var err error
			endpoint, err = ChijieProxyURL(pool.Config.Endpoint, pool.Config.Port)
			if err != nil {
				log.Printf("template %s unavailable for %s: %v", poolName, region, err)
				continue
			}
		} else {
			var err error
			d, err = m.buildTemplateDialer(poolName, pool.Config, region)
			if err != nil {
				log.Printf("template %s unavailable for %s: %v", poolName, region, err)
				continue
			}
		}
		choices = append(choices, &EgressChoice{
			Dialer:       d,
			PoolName:     poolName,
			NodeName:     fmt.Sprintf("%s-%s", poolName, strings.ToLower(region)),
			Source:       "template",
			TemplateType: templateType,
			Region:       region,
			Group:        EgressGroup(region, residential),
			Residential:  residential,
			Template:     true,
			Priority:     pool.Config.Priority,
			Endpoint:     endpoint,
			BearerToken:  pool.Config.BearerToken,
		})
	}
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].Priority != choices[j].Priority {
			return choices[i].Priority > choices[j].Priority
		}
		return choices[i].PoolName < choices[j].PoolName
	})
	return choices
}

func (m *Manager) buildTemplateDialer(poolName string, cfg *PoolConfig, region string) (dialer.Dialer, error) {
	node := &dialer.Node{
		Name:        fmt.Sprintf("%s-%s", poolName, strings.ToLower(region)),
		Type:        cfg.Type,
		Server:      cfg.Server,
		Port:        cfg.Port,
		Username:    templateUsername(cfg.UsernameTemplate, region),
		Password:    cfg.Password,
		Residential: cfg.Residential,
		Tags:        normalizeTags(cfg.Tags),
	}
	return dialer.NewDialer(node)
}

func templateUsername(usernameTemplate string, region string) string {
	username := strings.ReplaceAll(usernameTemplate, "{region}", strings.ToLower(region))
	username = strings.ReplaceAll(username, "{REGION}", strings.ToUpper(region))
	return username
}

// NormalizeTemplateType 标准化模板提供方类型。空值保持旧配置语义，按普通代理模板处理。
func NormalizeTemplateType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "proxy", "generic", "generic-proxy":
		return "proxy"
	case "chijie":
		return "chijie"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

// NormalizeTemplateCoverage 标准化模板覆盖范围。旧配置没有 coverage 时按 residential 布尔值区分。
func NormalizeTemplateCoverage(value string, residential bool, templateType string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "normal", "residential", "both":
		return strings.ToLower(strings.TrimSpace(value))
	}
	if NormalizeTemplateType(templateType) == "chijie" {
		return "both"
	}
	if residential {
		return "residential"
	}
	return "normal"
}

// TemplateCoversResidential 判断模板是否服务当前普通/家宽请求。
func TemplateCoversResidential(cfg *PoolConfig, residential bool) bool {
	if cfg == nil {
		return false
	}
	switch NormalizeTemplateCoverage(cfg.Coverage, cfg.Residential, cfg.TemplateType) {
	case "both":
		return true
	case "residential":
		return residential
	default:
		return !residential
	}
}

// ChijieProxyURL 返回远端 Chijie 的 /proxy HTTPS 地址。endpoint 可只填写域名。
func ChijieProxyURL(endpoint string, port int) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("chijie endpoint is required")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse chijie endpoint: %w", err)
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("chijie template endpoint must use https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("chijie endpoint has no host")
	}
	if port > 0 && parsed.Port() == "" {
		parsed.Host = net.JoinHostPort(parsed.Hostname(), fmt.Sprintf("%d", port))
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	basePath := strings.TrimRight(parsed.Path, "/")
	if basePath == "" {
		parsed.Path = "/proxy"
	} else if !strings.HasSuffix(basePath, "/proxy") {
		parsed.Path = basePath + "/proxy"
	} else {
		parsed.Path = basePath
	}
	return parsed.String(), nil
}

func (m *Manager) pickEgressChoice(choices []*EgressChoice, strategy string, rrKey string) *EgressChoice {
	ordered := m.orderEgressChoices(choices, strategy, rrKey)
	if len(ordered) == 0 {
		return nil
	}
	return ordered[0]
}

func (m *Manager) orderEgressChoices(choices []*EgressChoice, strategy string, rrKey string) []*EgressChoice {
	if len(choices) == 0 {
		return nil
	}
	ordered := append([]*EgressChoice(nil), choices...)

	switch NormalizeStrategy(strategy) {
	case "round-robin":
		m.rrMu.Lock()
		idx := m.rrByGroup[rrKey] % len(ordered)
		m.rrByGroup[rrKey]++
		m.rrMu.Unlock()
		if idx > 0 {
			ordered = append(ordered[idx:], ordered[:idx]...)
		}
	case "least-latency":
		sort.SliceStable(ordered, func(i, j int) bool {
			if ordered[i].Latency == ordered[j].Latency {
				if ordered[i].PoolName == ordered[j].PoolName {
					return ordered[i].NodeName < ordered[j].NodeName
				}
				return ordered[i].PoolName < ordered[j].PoolName
			}
			if ordered[i].Latency == 0 {
				return false
			}
			if ordered[j].Latency == 0 {
				return true
			}
			return ordered[i].Latency < ordered[j].Latency
		})
	default:
		rand.Shuffle(len(ordered), func(i, j int) {
			ordered[i], ordered[j] = ordered[j], ordered[i]
		})
	}
	return ordered
}

// GetPool 获取指定池
func (m *Manager) GetPool(name string) *Pool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pools[name]
}

// ListPools 列出所有池名称
func (m *Manager) ListPools() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.pools))
	for name := range m.pools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ListPoolEntries 返回所有池的节点条目（供健康检查使用）
func (m *Manager) ListPoolEntries() map[string][]*NodeEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]*NodeEntry)
	for name, pool := range m.pools {
		pool.mu.RLock()
		entries := make([]*NodeEntry, len(pool.Entries))
		copy(entries, pool.Entries)
		pool.mu.RUnlock()
		result[name] = entries
	}
	return result
}

// RefreshSubscription 刷新指定订阅池的节点
func (m *Manager) RefreshSubscription(poolName string) error {
	m.mu.RLock()
	pool, ok := m.pools[poolName]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("pool not found: %s", poolName)
	}
	if pool.Config.Source != "subscription" {
		return fmt.Errorf("pool %s is not a subscription pool", poolName)
	}

	parser := NewSubscriptionParser()
	nodes, err := parser.Fetch(pool.Config.URL)
	if err != nil {
		pool.mu.Lock()
		pool.Error = fmt.Sprintf("fetch subscription: %v", err)
		pool.mu.Unlock()
		return fmt.Errorf("fetch subscription: %w", err)
	}

	if pool.Config.Filter != nil && len(pool.Config.Filter.Region) > 0 {
		nodes = filterByRegion(nodes, pool.Config.Filter.Region)
	}
	nodes, err = applyRejectRegexes(nodes, pool.Config.RejectRegex)
	if err != nil {
		pool.mu.Lock()
		pool.Error = err.Error()
		pool.mu.Unlock()
		return err
	}

	entries, warning := buildSubscriptionEntries(nodes, pool.Config)

	pool.mu.Lock()
	pool.Entries = entries
	pool.Error = warning
	pool.mu.Unlock()

	log.Printf("pool %s: refreshed %d nodes", poolName, len(entries))
	return nil
}

func buildSubscriptionEntries(nodes []dialer.Node, cfg *PoolConfig) ([]*NodeEntry, string) {
	entries := make([]*NodeEntry, 0, len(nodes))
	skipped := make(map[string]int)
	skippedTotal := 0

	for i := range nodes {
		node := nodes[i]
		d, err := dialer.NewDialer(&node)
		if err != nil {
			reason := subscriptionSkipReason(&node, err)
			skipped[reason]++
			skippedTotal++
			log.Printf("skip node %s: %v", node.Name, err)
			continue
		}
		entries = append(entries, newNodeEntry(&node, d, cfg))
	}

	if skippedTotal == 0 {
		return entries, ""
	}
	if len(entries) == 0 && len(nodes) > 0 {
		return entries, fmt.Sprintf("subscription parsed %d nodes but loaded 0 supported nodes: %s", len(nodes), formatSkipReasons(skipped))
	}
	return entries, fmt.Sprintf("subscription skipped %d nodes: %s", skippedTotal, formatSkipReasons(skipped))
}

func subscriptionSkipReason(node *dialer.Node, err error) string {
	if node != nil {
		network := strings.ToLower(util.FirstNonEmpty(node.Extra["network"], node.Extra["transport"], node.Extra["transport_type"]))
		switch network {
		case "xhttp", "splithttp", "split-http":
			return fmt.Sprintf("unsupported v2ray transport %q", network)
		}
	}
	return err.Error()
}

func formatSkipReasons(reasons map[string]int) string {
	if len(reasons) == 0 {
		return ""
	}
	keys := make([]string, 0, len(reasons))
	for reason := range reasons {
		keys = append(keys, reason)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, reason := range keys {
		if reasons[reason] > 1 {
			parts = append(parts, fmt.Sprintf("%s (%d)", reason, reasons[reason]))
			continue
		}
		parts = append(parts, reason)
	}
	return strings.Join(parts, "; ")
}

// StartSubscriptionUpdater 启动后台订阅自动更新。
// 重复调用会先停掉旧协程再启动新协程，避免 reload 后协程泄漏。
func (m *Manager) StartSubscriptionUpdater() {
	m.StopSubscriptionUpdater()

	m.updaterMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	m.updaterCancel = cancel
	m.updaterMu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, pool := range m.pools {
		if pool.Config.Source != "subscription" || pool.Config.UpdateInterval == "" {
			continue
		}

		interval, err := ParseDurationWithDays(pool.Config.UpdateInterval)
		if err != nil {
			log.Printf("invalid update_interval for %s: %v", name, err)
			continue
		}

		poolName := name
		m.updaterWG.Add(1)
		go func() {
			defer m.updaterWG.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := m.RefreshSubscription(poolName); err != nil {
						log.Printf("refresh %s failed: %v", poolName, err)
					}
				}
			}
		}()

		log.Printf("subscription updater started for %s (interval: %s)", name, interval)
	}
}

// StopSubscriptionUpdater 取消所有正在运行的订阅自动更新协程并等待退出。
func (m *Manager) StopSubscriptionUpdater() {
	m.updaterMu.Lock()
	cancel := m.updaterCancel
	m.updaterCancel = nil
	m.updaterMu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.updaterWG.Wait()
}

// PoolStatus 节点池状态（供 Admin API 使用）
type PoolStatus struct {
	Name         string              `json:"name"`
	Source       string              `json:"source"`
	Config       *PoolConfig         `json:"config,omitempty"`
	Error        string              `json:"error,omitempty"`
	RegionGroups []RegionGroupStatus `json:"region_groups,omitempty"`
	Nodes        []NodeStatus        `json:"nodes"`
}

// RegionGroupStatus 订阅节点按地区分组后的状态
type RegionGroupStatus struct {
	Group       string `json:"group"`
	Region      string `json:"region"`
	Name        string `json:"name"`
	Residential bool   `json:"residential"`
	Count       int    `json:"count"`
	Online      int    `json:"online"`
}

// NodeStatus 单个节点状态
type NodeStatus struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Server      string            `json:"server"`
	Port        int               `json:"port"`
	Username    string            `json:"username,omitempty"`
	Password    string            `json:"password,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
	Region      string            `json:"region,omitempty"`
	RegionGroup string            `json:"region_group,omitempty"`
	Residential bool              `json:"residential"`
	Alias       string            `json:"alias,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Enabled     bool              `json:"enabled"`
	Alive       bool              `json:"alive"`
	Latency     string            `json:"latency"`
	FailCount   int               `json:"fail_count"`
}

// GetPoolStatus 返回所有池的详细状态
func (m *Manager) GetPoolStatus() []PoolStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]PoolStatus, 0, len(m.pools))
	for name, pool := range m.pools {
		ps := PoolStatus{
			Name:   name,
			Source: pool.Config.Source,
			Config: pool.Config,
		}

		pool.mu.RLock()
		ps.Error = pool.Error
		groupMap := make(map[string]*RegionGroupStatus)
		for _, e := range pool.Entries {
			ns := NodeStatus{
				Name:        e.Node.Name,
				Type:        e.Node.Type,
				Server:      e.Node.Server,
				Port:        e.Node.Port,
				Username:    e.Node.Username,
				Password:    e.Node.Password,
				Extra:       e.Node.Extra,
				Region:      e.Region,
				RegionGroup: EgressGroup(e.Region, e.Residential),
				Residential: e.Residential,
				Alias:       e.Alias,
				Tags:        e.Tags,
				Enabled:     e.Enabled,
				Alive:       e.Alive,
				FailCount:   e.FailCount,
			}
			if e.Latency > 0 {
				ns.Latency = e.Latency.String()
			}
			ps.Nodes = append(ps.Nodes, ns)
			groupRegion := EgressGroup(e.Region, e.Residential)
			group := groupMap[groupRegion]
			if group == nil {
				group = &RegionGroupStatus{
					Group:       groupRegion,
					Region:      e.Region,
					Name:        groupNameForRegion(e.Region, e.Residential, pool.Config),
					Residential: e.Residential,
				}
				groupMap[groupRegion] = group
			}
			group.Count++
			if e.Enabled && e.Alive {
				group.Online++
			}
		}
		for region, groupName := range pool.Config.RegionGroupNames {
			normalized := normalizeRegionCode(region)
			if normalized == "" {
				normalized = strings.ToUpper(strings.TrimSpace(region))
			}
			if normalized == "" {
				continue
			}
			groupCode := EgressGroup(normalized, pool.Config.Residential)
			if groupMap[groupCode] == nil {
				groupMap[groupCode] = &RegionGroupStatus{
					Group:       groupCode,
					Region:      normalized,
					Name:        groupName,
					Residential: pool.Config.Residential,
				}
			}
		}
		ps.RegionGroups = sortedRegionGroups(groupMap)
		pool.mu.RUnlock()

		result = append(result, ps)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Source != result[j].Source {
			return result[i].Source < result[j].Source
		}
		return result[i].Name < result[j].Name
	})
	return result
}

// SetNodeEnabled 更新运行时节点启停状态。
func (m *Manager) SetNodeEnabled(poolName, nodeName string, enabled bool) error {
	m.mu.RLock()
	pool, ok := m.pools[poolName]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("pool not found: %s", poolName)
	}

	pool.mu.Lock()
	defer pool.mu.Unlock()
	for _, entry := range pool.Entries {
		if entry.Node.Name == nodeName {
			entry.Enabled = enabled
			entry.Node.Enabled = &enabled
			updateConfigNodeEnabled(pool.Config, nodeName, enabled)
			return nil
		}
	}
	return fmt.Errorf("node not found in pool %s: %s", poolName, nodeName)
}

// MarkNodeUnavailable 将一次 /proxy 出口执行失败对应的运行时节点立即标记为不可用。
// 后台健康检查或手动测试后续成功时会再次把 Alive 恢复为 true。
func (m *Manager) MarkNodeUnavailable(poolName, nodeName string) bool {
	if m == nil || strings.TrimSpace(poolName) == "" || strings.TrimSpace(nodeName) == "" {
		return false
	}
	m.mu.RLock()
	pool, ok := m.pools[poolName]
	m.mu.RUnlock()
	if !ok || pool == nil {
		return false
	}

	pool.mu.Lock()
	defer pool.mu.Unlock()
	for _, entry := range pool.Entries {
		if entry == nil || entry.Node == nil || entry.Node.Name != nodeName {
			continue
		}
		if strings.EqualFold(entry.Node.Type, "direct") {
			return false
		}
		entry.Alive = false
		entry.FailCount++
		return true
	}
	return false
}

func updateConfigNodeEnabled(cfg *PoolConfig, nodeName string, enabled bool) {
	for i := range cfg.Nodes {
		if cfg.Nodes[i].Name == nodeName {
			cfg.Nodes[i].Enabled = &enabled
			return
		}
	}
	if enabled {
		cfg.DisabledNodes = util.RemoveString(cfg.DisabledNodes, nodeName)
	} else if !util.ContainsString(cfg.DisabledNodes, nodeName) {
		cfg.DisabledNodes = append(cfg.DisabledNodes, nodeName)
	}
}

func isNodeEnabled(node *dialer.Node, cfg *PoolConfig) bool {
	if node.Enabled != nil {
		return *node.Enabled
	}
	return !util.ContainsString(cfg.DisabledNodes, node.Name)
}

func poolEnabled(cfg *PoolConfig) bool {
	return cfg == nil || cfg.Enabled == nil || *cfg.Enabled
}

func newNodeEntry(node *dialer.Node, d dialer.Dialer, cfg *PoolConfig) *NodeEntry {
	tags := tagsForNode(node, cfg)
	return &NodeEntry{
		Node:        node,
		Dialer:      d,
		Enabled:     isNodeEnabled(node, cfg),
		Alive:       true,
		Region:      regionForNode(node, cfg),
		Alias:       aliasForNode(node, cfg),
		Residential: residentialForNode(node, cfg, tags),
		Tags:        tags,
	}
}

func residentialForNode(node *dialer.Node, cfg *PoolConfig, tags []string) bool {
	return node.Residential || (cfg != nil && cfg.Residential) || util.ContainsString(tags, "residential")
}

func tagsForNode(node *dialer.Node, cfg *PoolConfig) []string {
	tags := make([]string, 0)
	if cfg != nil {
		tags = append(tags, cfg.Tags...)
		if cfg.NodeServerTags != nil {
			tags = append(tags, cfg.NodeServerTags[nodeServerKey(node)]...)
		}
		if cfg.NodeTags != nil {
			tags = append(tags, cfg.NodeTags[node.Name]...)
		}
	}
	tags = append(tags, node.Tags...)
	tags = append(tags, detectTagsFromNodeName(node.Name)...)
	return normalizeTags(tags)
}

func detectTagsFromNodeName(name string) []string {
	lower := strings.ToLower(name)
	if strings.Contains(name, "家宽") ||
		strings.Contains(name, "住宅") ||
		strings.Contains(name, "家庭宽带") ||
		strings.Contains(lower, "residential") ||
		strings.Contains(lower, "resi") ||
		strings.Contains(lower, "home-broadband") ||
		strings.Contains(lower, "home broadband") {
		return []string{"residential"}
	}
	return nil
}

func normalizeTags(tags []string) []string {
	result := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		tag = strings.ReplaceAll(tag, " ", "-")
		tag = strings.ReplaceAll(tag, "_", "-")
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}

// NormalizeRegionCode 对外提供二字母地区码标准化。
func NormalizeRegionCode(value string) string {
	region := normalizeRegionCode(value)
	if len(region) != 2 {
		return ""
	}
	return region
}

// NormalizeStrategy 标准化节点选择策略。
func NormalizeStrategy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "least-latency", "least_latency", "latency":
		return "least-latency"
	case "round-robin", "round_robin", "rr":
		return "round-robin"
	case "random", "":
		return "random"
	default:
		return "random"
	}
}

// EgressGroup 返回前端和日志使用的地区组代码。
func EgressGroup(region string, residential bool) string {
	region = NormalizeRegionCode(region)
	if region == "" {
		return "DIRECT"
	}
	if residential {
		return region + "-RES"
	}
	return region
}

// AnyEgressGroup 返回地区无关出口组代码。
func AnyEgressGroup(residential bool) string {
	if residential {
		return "ANY-RES"
	}
	return "ANY"
}

func regionForNode(node *dialer.Node, cfg *PoolConfig) string {
	if region := NormalizeRegionCode(node.Region); region != "" {
		return region
	}
	if cfg != nil && cfg.NodeServerRegions != nil {
		if region := normalizeRegionCode(cfg.NodeServerRegions[nodeServerKey(node)]); region != "" {
			return region
		}
	}
	if cfg != nil && cfg.NodeRegions != nil {
		if region := normalizeRegionCode(cfg.NodeRegions[node.Name]); region != "" {
			return region
		}
	}
	return detectRegionFromNodeName(node.Name)
}

func aliasForNode(node *dialer.Node, cfg *PoolConfig) string {
	if cfg == nil {
		return ""
	}
	if cfg.NodeServerAliases != nil {
		if alias := strings.TrimSpace(cfg.NodeServerAliases[nodeServerKey(node)]); alias != "" {
			return alias
		}
	}
	for alias, target := range cfg.NodeAliases {
		if target == node.Name {
			return alias
		}
	}
	return ""
}

func nodeServerKey(node *dialer.Node) string {
	if node == nil {
		return ""
	}
	return ServerKey(node.Server, node.Port)
}

// ServerKey 返回节点 server 映射使用的稳定键。
func ServerKey(server string, port int) string {
	server = strings.TrimSpace(strings.ToLower(server))
	if server == "" {
		return ""
	}
	if port > 0 {
		return fmt.Sprintf("%s:%d", server, port)
	}
	return server
}

func groupNameForRegion(region string, residential bool, cfg *PoolConfig) string {
	normalized := normalizeRegionCode(region)
	if normalized == "" {
		normalized = strings.ToUpper(strings.TrimSpace(region))
	}
	if normalized == "" {
		normalized = "UN"
	}
	if cfg != nil && cfg.RegionGroupNames != nil {
		if name := strings.TrimSpace(cfg.RegionGroupNames[normalized]); name != "" {
			return name
		}
	}
	if name := regionCodeToName[normalized]; name != "" {
		if residential {
			return name + "-RES"
		}
		return name
	}
	if residential {
		return normalized + "-RES"
	}
	return normalized
}

func sortedRegionGroups(groups map[string]*RegionGroupStatus) []RegionGroupStatus {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]RegionGroupStatus, 0, len(keys))
	for _, key := range keys {
		result = append(result, *groups[key])
	}
	return result
}

func applyRejectRegexes(nodes []dialer.Node, patterns []string) ([]dialer.Node, error) {
	if len(patterns) == 0 {
		return nodes, nil
	}

	regexes := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid reject regex %q: %w", pattern, err)
		}
		regexes = append(regexes, compiled)
	}
	if len(regexes) == 0 {
		return nodes, nil
	}

	filtered := make([]dialer.Node, 0, len(nodes))
	for _, node := range nodes {
		rejected := false
		for _, regex := range regexes {
			if regex.MatchString(node.Name) {
				rejected = true
				break
			}
		}
		if !rejected {
			filtered = append(filtered, node)
		}
	}
	return filtered, nil
}

// filterByRegion 按地区代码过滤节点（从节点名称中匹配）
func filterByRegion(nodes []dialer.Node, regions []string) []dialer.Node {
	if len(regions) == 0 {
		return nodes
	}

	regionSet := make(map[string]bool)
	for _, c := range regions {
		if region := normalizeRegionCode(c); region != "" {
			regionSet[region] = true
		}
	}

	var filtered []dialer.Node
	for _, node := range nodes {
		if regionSet[detectRegionFromNodeName(node.Name)] {
			filtered = append(filtered, node)
		}
	}

	return filtered
}

func detectRegionFromNodeName(name string) string {
	if region := flagRegionCode(name); region != "" {
		if region == "CN" && containsRegionCodeToken(strings.ToUpper(name), "TW") {
			return "TW"
		}
		return region
	}

	lowerName := strings.ToLower(name)
	for alias, region := range regionAliasToCode {
		if strings.Contains(lowerName, alias) {
			return region
		}
	}

	upperName := strings.ToUpper(name)
	for code := range regionCodeToName {
		if code != "UN" && containsRegionCodeToken(upperName, code) {
			return code
		}
	}
	return "UN"
}

func containsRegionCodeToken(upperName string, region string) bool {
	code := strings.ToUpper(strings.TrimSpace(region))
	if code == "" {
		return false
	}
	for idx := strings.Index(upperName, code); idx >= 0; {
		beforeOK := idx == 0 || !isASCIIUpperLetter(upperName[idx-1])
		afterIdx := idx + len(code)
		for afterIdx < len(upperName) && upperName[afterIdx] >= '0' && upperName[afterIdx] <= '9' {
			afterIdx++
		}
		afterOK := afterIdx == len(upperName) || !isASCIIUpperLetter(upperName[afterIdx])
		if beforeOK && afterOK {
			return true
		}
		nextStart := idx + len(code)
		if nextStart >= len(upperName) {
			break
		}
		next := strings.Index(upperName[nextStart:], code)
		if next < 0 {
			break
		}
		idx = nextStart + next
	}
	return false
}

func isASCIIUpperLetter(b byte) bool {
	return b >= 'A' && b <= 'Z'
}

func flagRegionCode(name string) string {
	runes := []rune(name)
	for i := 0; i < len(runes)-1; i++ {
		first := regionalIndicatorLetter(runes[i])
		second := regionalIndicatorLetter(runes[i+1])
		if first != 0 && second != 0 {
			return string([]rune{first, second})
		}
	}
	return ""
}

func regionalIndicatorLetter(r rune) rune {
	if r < 0x1F1E6 || r > 0x1F1FF {
		return 0
	}
	return 'A' + (r - 0x1F1E6)
}

func normalizeRegionCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if region := regionAliasToCode[strings.ToLower(value)]; region != "" {
		return region
	}
	upper := strings.ToUpper(value)
	if _, ok := regionCodeToName[upper]; ok {
		return upper
	}
	return upper
}
