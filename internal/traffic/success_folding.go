package traffic

import (
	"fmt"
	"net/url"
	"strings"
)

type SuccessFoldingConfig struct {
	Enabled *bool                `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Rules   []SuccessFoldingRule `yaml:"rules,omitempty" json:"rules,omitempty"`
}

type SuccessFoldingRule struct {
	Name  string                `yaml:"name,omitempty" json:"name,omitempty"`
	Match URLNormalizationMatch `yaml:"match,omitempty" json:"match,omitempty"`
}

func normalizeSuccessFoldingConfig(cfg SuccessFoldingConfig) SuccessFoldingConfig {
	enabled := len(cfg.Rules) > 0
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	rules := make([]SuccessFoldingRule, 0, len(cfg.Rules))
	for _, rule := range cfg.Rules {
		normalized := normalizeSuccessFoldingRule(rule)
		if validateSuccessFoldingRule(normalized) != nil {
			continue
		}
		rules = append(rules, normalized)
	}
	return SuccessFoldingConfig{Enabled: &enabled, Rules: rules}
}

func MergeSuccessFoldingRule(cfg Config, rule SuccessFoldingRule) (Config, SuccessFoldingRule, error) {
	normalizedRule := normalizeSuccessFoldingRule(rule)
	if err := validateSuccessFoldingRule(normalizedRule); err != nil {
		return Config{}, SuccessFoldingRule{}, err
	}

	next := NormalizeConfig(cfg)
	enabled := true
	next.SuccessFolding.Enabled = &enabled
	for i := range next.SuccessFolding.Rules {
		existing := &next.SuccessFolding.Rules[i]
		if existing.Match.HostPattern == normalizedRule.Match.HostPattern &&
			existing.Match.PathPattern == normalizedRule.Match.PathPattern {
			if strings.TrimSpace(existing.Name) == "" {
				existing.Name = normalizedRule.Name
			}
			return next, *existing, nil
		}
	}
	next.SuccessFolding.Rules = append(next.SuccessFolding.Rules, normalizedRule)
	return next, normalizedRule, nil
}

func successFoldingEnabled(cfg Config) bool {
	if cfg.SuccessFolding.Enabled == nil {
		return len(cfg.SuccessFolding.Rules) > 0
	}
	return *cfg.SuccessFolding.Enabled
}

func successFoldGroup(trace Trace, cfg Config) (string, string, bool) {
	if !successFoldingEnabled(cfg) || trace.Status != 200 || trace.Error != "" {
		return "", "", false
	}
	parsed, err := url.Parse(strings.TrimSpace(trace.URL))
	if err != nil || parsed.Host == "" {
		return "", "", false
	}

	matched := false
	for _, rule := range cfg.SuccessFolding.Rules {
		if urlRuleMatches(URLNormalizationRule{Match: rule.Match}, parsed) {
			matched = true
			break
		}
	}
	if !matched {
		return "", "", false
	}

	host := strings.ToLower(parsed.Host)
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	target := "//" + host + path
	key := strings.Join([]string{
		nonEmpty(trace.Kind, "proxy"),
		target,
		targetArea(trace),
	}, "\x00")
	return key, target, true
}

func normalizeSuccessFoldingRule(rule SuccessFoldingRule) SuccessFoldingRule {
	rule.Name = strings.TrimSpace(rule.Name)
	rule.Match.HostPattern = strings.ToLower(strings.TrimSpace(rule.Match.HostPattern))
	rule.Match.PathPattern = cleanPathPattern(strings.TrimSpace(rule.Match.PathPattern))
	if rule.Name == "" {
		rule.Name = defaultRuleName(URLNormalizationRule{Match: rule.Match})
	}
	return rule
}

func validateSuccessFoldingRule(rule SuccessFoldingRule) error {
	if rule.Match.HostPattern == "" {
		return fmt.Errorf("host_pattern is required")
	}
	if rule.Match.PathPattern == "" {
		return fmt.Errorf("path_pattern is required")
	}
	return nil
}
