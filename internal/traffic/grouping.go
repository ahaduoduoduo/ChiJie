package traffic

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Config controls how traffic traces are reduced for admin metrics and display.
type Config struct {
	FailureGrouping FailureGroupingConfig `yaml:"failure_grouping,omitempty" json:"failure_grouping,omitempty"`
}

type FailureGroupingConfig struct {
	Enabled          *bool                  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	URLNormalization URLNormalizationConfig `yaml:"url_normalization,omitempty" json:"url_normalization,omitempty"`
}

type URLNormalizationConfig struct {
	Enabled *bool                  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Rules   []URLNormalizationRule `yaml:"rules,omitempty" json:"rules,omitempty"`
}

type URLNormalizationRule struct {
	Name  string                   `yaml:"name,omitempty" json:"name,omitempty"`
	Match URLNormalizationMatch    `yaml:"match,omitempty" json:"match,omitempty"`
	Query QueryNormalizationConfig `yaml:"query,omitempty" json:"query,omitempty"`
}

type URLNormalizationMatch struct {
	HostPattern string `yaml:"host_pattern,omitempty" json:"host_pattern,omitempty"`
	PathPattern string `yaml:"path_pattern,omitempty" json:"path_pattern,omitempty"`
}

type QueryNormalizationConfig struct {
	DropKeys []string `yaml:"drop_keys,omitempty" json:"drop_keys,omitempty"`
	Sort     *bool    `yaml:"sort,omitempty" json:"sort,omitempty"`
}

func DefaultConfig() Config {
	groupingEnabled := true
	normalizationEnabled := false
	return Config{
		FailureGrouping: FailureGroupingConfig{
			Enabled: &groupingEnabled,
			URLNormalization: URLNormalizationConfig{
				Enabled: &normalizationEnabled,
			},
		},
	}
}

func NormalizeConfig(cfg Config) Config {
	groupingEnabled := true
	if cfg.FailureGrouping.Enabled != nil {
		groupingEnabled = *cfg.FailureGrouping.Enabled
	}

	normalizationEnabled := len(cfg.FailureGrouping.URLNormalization.Rules) > 0
	if cfg.FailureGrouping.URLNormalization.Enabled != nil {
		normalizationEnabled = *cfg.FailureGrouping.URLNormalization.Enabled
	}

	rules := make([]URLNormalizationRule, 0, len(cfg.FailureGrouping.URLNormalization.Rules))
	for _, rule := range cfg.FailureGrouping.URLNormalization.Rules {
		normalized := normalizeRule(rule)
		if len(normalized.Query.DropKeys) == 0 {
			continue
		}
		rules = append(rules, normalized)
	}

	return Config{
		FailureGrouping: FailureGroupingConfig{
			Enabled: &groupingEnabled,
			URLNormalization: URLNormalizationConfig{
				Enabled: &normalizationEnabled,
				Rules:   rules,
			},
		},
	}
}

func MergeURLNormalizationRule(cfg Config, rule URLNormalizationRule) (Config, URLNormalizationRule, error) {
	normalizedRule := normalizeRule(rule)
	if err := validateRule(normalizedRule); err != nil {
		return Config{}, URLNormalizationRule{}, err
	}

	next := NormalizeConfig(cfg)
	groupingEnabled := true
	normalizationEnabled := true
	next.FailureGrouping.Enabled = &groupingEnabled
	next.FailureGrouping.URLNormalization.Enabled = &normalizationEnabled

	for i := range next.FailureGrouping.URLNormalization.Rules {
		existing := &next.FailureGrouping.URLNormalization.Rules[i]
		if existing.Match.HostPattern == normalizedRule.Match.HostPattern &&
			existing.Match.PathPattern == normalizedRule.Match.PathPattern {
			existing.Query.DropKeys = mergeStrings(existing.Query.DropKeys, normalizedRule.Query.DropKeys)
			if existing.Query.Sort == nil {
				sortQuery := true
				existing.Query.Sort = &sortQuery
			}
			if strings.TrimSpace(existing.Name) == "" {
				existing.Name = normalizedRule.Name
			}
			return next, *existing, nil
		}
	}

	next.FailureGrouping.URLNormalization.Rules = append(next.FailureGrouping.URLNormalization.Rules, normalizedRule)
	return next, normalizedRule, nil
}

func groupingEnabled(cfg Config) bool {
	if cfg.FailureGrouping.Enabled == nil {
		return true
	}
	return *cfg.FailureGrouping.Enabled
}

func urlNormalizationEnabled(cfg Config) bool {
	urlCfg := cfg.FailureGrouping.URLNormalization
	if urlCfg.Enabled == nil {
		return len(urlCfg.Rules) > 0
	}
	return *urlCfg.Enabled
}

func failureGroupKey(trace Trace, cfg Config) string {
	if !groupingEnabled(cfg) {
		return ""
	}
	target := failureGroupTarget(trace, cfg)
	if target == "" {
		return ""
	}
	return strings.Join([]string{
		nonEmpty(trace.Kind, "proxy"),
		target,
		targetArea(trace),
	}, "\x00")
}

func failureGroupTarget(trace Trace, cfg Config) string {
	target := strings.TrimSpace(trace.URL)
	if target != "" {
		return normalizeFailureURL(target, cfg)
	}
	return strings.TrimSpace(trace.Target)
}

func normalizeFailureURL(raw string, cfg Config) string {
	if !urlNormalizationEnabled(cfg) {
		return raw
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}

	dropKeys := map[string]struct{}{}
	sortQuery := false
	for _, rule := range cfg.FailureGrouping.URLNormalization.Rules {
		if !urlRuleMatches(rule, parsed) {
			continue
		}
		for _, key := range rule.Query.DropKeys {
			dropKeys[key] = struct{}{}
		}
		if boolValue(rule.Query.Sort, true) {
			sortQuery = true
		}
	}
	if len(dropKeys) == 0 {
		return raw
	}

	query := parsed.Query()
	changed := false
	for key := range query {
		if _, ok := dropKeys[key]; ok {
			query.Del(key)
			changed = true
		}
	}
	if !changed && !sortQuery {
		return raw
	}

	normalized := *parsed
	normalized.Host = strings.ToLower(normalized.Host)
	normalized.Fragment = ""
	normalized.RawQuery = query.Encode()
	return normalized.String()
}

func urlRuleMatches(rule URLNormalizationRule, parsed *url.URL) bool {
	host := strings.ToLower(parsed.Hostname())
	if !matchHostPattern(host, rule.Match.HostPattern) {
		return false
	}
	return matchPathPattern(parsed.EscapedPath(), rule.Match.PathPattern)
}

func matchHostPattern(host, pattern string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return true
	}
	hostParts := splitHostSegments(host)
	patternParts := splitHostSegments(pattern)
	if len(hostParts) != len(patternParts) {
		return false
	}
	for i := range patternParts {
		if !wildcardMatch(patternParts[i], hostParts[i]) {
			return false
		}
	}
	return true
}

func matchPathPattern(value, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return true
	}
	valueParts := splitPathSegments(value)
	patternParts := splitPathSegments(pattern)
	if len(valueParts) != len(patternParts) {
		return false
	}
	for i := range patternParts {
		if !wildcardMatch(patternParts[i], valueParts[i]) {
			return false
		}
	}
	return true
}

func wildcardMatch(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == value
	}

	parts := strings.Split(pattern, "*")
	index := 0
	if parts[0] != "" {
		if !strings.HasPrefix(value, parts[0]) {
			return false
		}
		index = len(parts[0])
	}
	for _, part := range parts[1 : len(parts)-1] {
		if part == "" {
			continue
		}
		found := strings.Index(value[index:], part)
		if found < 0 {
			return false
		}
		index += found + len(part)
	}
	last := parts[len(parts)-1]
	if last == "" {
		return true
	}
	return strings.HasSuffix(value[index:], last)
}

func targetArea(trace Trace) string {
	if trace.EgressGroup != "" {
		return trace.EgressGroup
	}
	if trace.Region != "" {
		region := trace.Region
		if trace.Residential && !strings.Contains(region, "-RES") {
			region += "-RES"
		}
		return region
	}
	return "DIRECT"
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func normalizeRule(rule URLNormalizationRule) URLNormalizationRule {
	rule.Name = strings.TrimSpace(rule.Name)
	rule.Match.HostPattern = strings.ToLower(strings.TrimSpace(rule.Match.HostPattern))
	rule.Match.PathPattern = cleanPathPattern(strings.TrimSpace(rule.Match.PathPattern))
	rule.Query.DropKeys = uniqueNonEmpty(rule.Query.DropKeys)
	if rule.Query.Sort == nil {
		sortQuery := true
		rule.Query.Sort = &sortQuery
	}
	if rule.Name == "" {
		rule.Name = defaultRuleName(rule)
	}
	return rule
}

func validateRule(rule URLNormalizationRule) error {
	if len(rule.Query.DropKeys) == 0 {
		return fmt.Errorf("drop_keys is required")
	}
	if rule.Match.HostPattern == "" && rule.Match.PathPattern == "" {
		return fmt.Errorf("host_pattern or path_pattern is required")
	}
	for _, key := range rule.Query.DropKeys {
		if strings.ContainsAny(key, "&=?#") {
			return fmt.Errorf("invalid query key %q", key)
		}
	}
	return nil
}

func defaultRuleName(rule URLNormalizationRule) string {
	parts := []string{"url-rule"}
	if rule.Match.HostPattern != "" {
		parts = append(parts, rule.Match.HostPattern)
	}
	if rule.Match.PathPattern != "" {
		segments := splitPathSegments(rule.Match.PathPattern)
		if len(segments) > 0 {
			parts = append(parts, segments[len(segments)-1])
		}
	}
	name := strings.Join(parts, "-")
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	if name == "" {
		return "url-rule"
	}
	return name
}

func cleanPathPattern(value string) string {
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		return "/" + value
	}
	return value
}

func splitHostSegments(value string) []string {
	value = strings.Trim(value, ".")
	if value == "" {
		return nil
	}
	return strings.Split(value, ".")
}

func splitPathSegments(value string) []string {
	value = strings.Trim(value, "/")
	if value == "" {
		return nil
	}
	return strings.Split(value, "/")
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func mergeStrings(a, b []string) []string {
	return uniqueNonEmpty(append(append([]string{}, a...), b...))
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
