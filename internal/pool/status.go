package pool

import (
	"sort"
	"strings"
	"time"
)

func (p *Pool) recordSubscriptionRefreshFailure(message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Error = message
	p.LastRefreshAt = time.Now().UTC()
	p.LastRefreshFailed = true
}

func (p *Pool) recordSubscriptionRefreshSuccess(entries []*NodeEntry, warning string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Entries = entries
	p.Error = warning
	p.LastRefreshAt = time.Now().UTC()
	p.LastRefreshFailed = false
}

func (p *Pool) restoreSubscriptionEntriesAfterFailure(entries []*NodeEntry, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Entries = entries
	p.Error = message
	p.LastRefreshAt = time.Now().UTC()
	p.LastRefreshFailed = true
}

// PoolStatus 节点池状态（供 Admin API 使用）
type PoolStatus struct {
	Name              string              `json:"name"`
	Source            string              `json:"source"`
	Config            *PoolConfig         `json:"config,omitempty"`
	Error             string              `json:"error,omitempty"`
	LastUpdated       string              `json:"last_updated,omitempty"`
	LastRefreshFailed bool                `json:"last_refresh_failed"`
	RegionGroups      []RegionGroupStatus `json:"region_groups,omitempty"`
	Nodes             []NodeStatus        `json:"nodes"`
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
	Premium     bool              `json:"premium,omitempty"`
	Alias       string            `json:"alias,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Enabled     bool              `json:"enabled"`
	Alive       bool              `json:"alive"`
	Latency     string            `json:"latency"`
	FailCount   int               `json:"fail_count"`
}

// GetPoolStatus 返回所有池的详细状态。
func (m *Manager) GetPoolStatus() []PoolStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]PoolStatus, 0, len(m.pools))
	for name, pool := range m.pools {
		poolStatus := PoolStatus{
			Name:   name,
			Source: pool.Config.Source,
			Config: pool.Config,
		}

		pool.mu.RLock()
		poolStatus.Error = pool.Error
		if !pool.LastRefreshAt.IsZero() {
			poolStatus.LastUpdated = pool.LastRefreshAt.Format(time.RFC3339Nano)
		}
		poolStatus.LastRefreshFailed = pool.LastRefreshFailed
		groupMap := make(map[string]*RegionGroupStatus)
		for _, entry := range pool.Entries {
			nodeStatus := NodeStatus{
				Name:        entry.Node.Name,
				Type:        entry.Node.Type,
				Server:      entry.Node.Server,
				Port:        entry.Node.Port,
				Username:    entry.Node.Username,
				Password:    entry.Node.Password,
				Extra:       entry.Node.Extra,
				Region:      entry.Region,
				RegionGroup: EgressGroup(entry.Region, entry.Residential),
				Residential: entry.Residential,
				Premium:     entry.Premium,
				Alias:       entry.Alias,
				Tags:        entry.Tags,
				Enabled:     entry.Enabled,
				Alive:       entry.Alive,
				FailCount:   entry.FailCount,
			}
			if entry.Latency > 0 {
				nodeStatus.Latency = entry.Latency.String()
			}
			poolStatus.Nodes = append(poolStatus.Nodes, nodeStatus)

			groupCode := EgressGroup(entry.Region, entry.Residential)
			group := groupMap[groupCode]
			if group == nil {
				group = &RegionGroupStatus{
					Group:       groupCode,
					Region:      entry.Region,
					Name:        groupNameForRegion(entry.Region, EgressSelector{Residential: entry.Residential}, pool.Config),
					Residential: entry.Residential,
				}
				groupMap[groupCode] = group
			}
			group.Count++
			if entry.Enabled && entry.Alive {
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
			selector := EgressSelector{Residential: pool.Config.Residential}
			groupCode := EgressGroupFor(normalized, selector)
			if groupMap[groupCode] == nil {
				groupMap[groupCode] = &RegionGroupStatus{
					Group:       groupCode,
					Region:      normalized,
					Name:        groupName,
					Residential: pool.Config.Residential,
				}
			}
		}
		poolStatus.RegionGroups = sortedRegionGroups(groupMap)
		pool.mu.RUnlock()

		result = append(result, poolStatus)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Source != result[j].Source {
			return result[i].Source < result[j].Source
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func groupNameForRegion(region string, selector EgressSelector, cfg *PoolConfig) string {
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
		if selector.Residential {
			return name + "-RES"
		}
		return name
	}
	if selector.Residential {
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
