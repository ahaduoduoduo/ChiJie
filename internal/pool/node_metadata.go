package pool

import (
	"fmt"
	"sort"
	"strings"

	"chijie/internal/dialer"
	"chijie/internal/util"
)

func residentialForNode(node *dialer.Node, cfg *PoolConfig, tags []string) bool {
	return node.Residential || (cfg != nil && cfg.Residential) || util.ContainsString(tags, "residential")
}

func premiumForNode(node *dialer.Node, cfg *PoolConfig, tags []string) bool {
	return node.Premium || (cfg != nil && cfg.Premium) || util.ContainsString(tags, "premium") || util.ContainsString(tags, "high-end")
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
	tags := make([]string, 0, 2)
	if strings.Contains(name, "家宽") ||
		strings.Contains(name, "住宅") ||
		strings.Contains(name, "家庭宽带") ||
		strings.Contains(lower, "residential") ||
		strings.Contains(lower, "resi") ||
		strings.Contains(lower, "home-broadband") ||
		strings.Contains(lower, "home broadband") {
		tags = append(tags, "residential")
	}
	if strings.Contains(name, "高端") ||
		strings.Contains(lower, "premium") ||
		strings.Contains(lower, "high-end") ||
		strings.Contains(lower, "high end") {
		tags = append(tags, "premium")
	}
	return tags
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
