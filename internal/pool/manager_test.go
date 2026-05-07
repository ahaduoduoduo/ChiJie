package pool

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"chijie/internal/dialer"
	"chijie/internal/util"
)

func TestSetNodeEnabledControlsSelection(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("static", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "node-1", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "US"},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	manager.pools["static"] = pool

	if _, err := manager.SelectEgress("US", "random", false); err != nil {
		t.Fatalf("expected enabled node to be selectable: %v", err)
	}

	if err := manager.SetNodeEnabled("static", "node-1", false); err != nil {
		t.Fatalf("disable node: %v", err)
	}
	if _, err := manager.SelectEgress("US", "random", false); err == nil {
		t.Fatalf("expected disabled node to be excluded")
	}

	status := manager.GetPoolStatus()
	if len(status) != 1 || len(status[0].Nodes) != 1 {
		t.Fatalf("unexpected status shape: %#v", status)
	}
	if status[0].Nodes[0].Enabled {
		t.Fatalf("status should expose disabled node")
	}

	if err := manager.SetNodeEnabled("static", "node-1", true); err != nil {
		t.Fatalf("enable node: %v", err)
	}
	if _, err := manager.SelectEgress("US", "random", false); err != nil {
		t.Fatalf("expected re-enabled node to be selectable: %v", err)
	}
}

func TestLoadFromFileReplacesPools(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.yaml")

	firstConfig := []byte(`
node_pools:
  static:
    source: static
    nodes:
      - name: node-1
        type: direct
  direct:
    source: direct
`)
	if err := os.WriteFile(path, firstConfig, 0644); err != nil {
		t.Fatalf("write first config: %v", err)
	}

	manager := NewManager()
	if err := manager.LoadFromFile(path); err != nil {
		t.Fatalf("load first config: %v", err)
	}
	if manager.GetPool("static") == nil || manager.GetPool("direct") == nil {
		t.Fatalf("expected both pools to be loaded")
	}

	secondConfig := []byte(`
node_pools:
  direct:
    source: direct
`)
	if err := os.WriteFile(path, secondConfig, 0644); err != nil {
		t.Fatalf("write second config: %v", err)
	}
	if err := manager.LoadFromFile(path); err != nil {
		t.Fatalf("load second config: %v", err)
	}

	if manager.GetPool("static") != nil {
		t.Fatalf("expected removed pool to be absent from runtime state")
	}
	if manager.GetPool("direct") == nil {
		t.Fatalf("expected remaining pool to stay loaded")
	}
}

func TestSubscriptionPoolLoadsWithFetchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	manager := NewManager()
	pool, err := manager.buildPool("sub", &PoolConfig{
		Source: "subscription",
		URL:    server.URL,
	})
	if err != nil {
		t.Fatalf("subscription fetch errors should not block pool loading: %v", err)
	}
	if pool == nil || pool.Error == "" {
		t.Fatalf("expected pool with recorded error, got %#v", pool)
	}
	if len(pool.Entries) != 0 {
		t.Fatalf("expected no entries on failed subscription fetch")
	}
}

func TestSelectEgressUsesExplicitRegion(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("subscription", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "node-us", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "US"},
			{Name: "node-jp", Type: "socks5", Server: "127.0.0.1", Port: 1081, Region: "JP"},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	manager.pools["subscription"] = pool

	choice, err := manager.SelectEgress("US", "random", false)
	if err != nil {
		t.Fatalf("select by region: %v", err)
	}
	if choice.NodeName != "node-us" || choice.Region != "US" || choice.Group != "US" {
		t.Fatalf("unexpected US choice: %#v", choice)
	}
}

func TestSelectEgressSeparatesResidentialGroups(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("subscription", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-ordinary", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "US"},
			{Name: "us-res", Type: "socks5", Server: "127.0.0.1", Port: 1081, Region: "US", Residential: true},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	manager.pools["subscription"] = pool

	ordinaryChoice, err := manager.SelectEgress("US", "random", false)
	if err != nil {
		t.Fatalf("select ordinary node: %v", err)
	}
	if ordinaryChoice.NodeName != "us-ordinary" || ordinaryChoice.Group != "US" {
		t.Fatalf("unexpected ordinary choice: %#v", ordinaryChoice)
	}

	residentialChoice, err := manager.SelectEgress("US", "random", true)
	if err != nil {
		t.Fatalf("select residential node: %v", err)
	}
	if residentialChoice.NodeName != "us-res" || residentialChoice.Group != "US-RES" {
		t.Fatalf("unexpected residential choice: %#v", residentialChoice)
	}

	status := manager.GetPoolStatus()
	if len(status) != 1 || len(status[0].Nodes) != 2 {
		t.Fatalf("unexpected status shape: %#v", status)
	}
	groups := map[string]RegionGroupStatus{}
	for _, group := range status[0].RegionGroups {
		groups[group.Group] = group
	}
	if groups["US"].Residential || groups["US"].Count != 1 {
		t.Fatalf("unexpected ordinary group: %#v", groups["US"])
	}
	if !groups["US-RES"].Residential || groups["US-RES"].Count != 1 {
		t.Fatalf("unexpected residential group: %#v", groups["US-RES"])
	}
}

func TestSelectEgressRoundRobinAcrossPools(t *testing.T) {
	manager := NewManager()

	first, err := manager.buildPool("sub-a", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-a", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "US"},
		},
	})
	if err != nil {
		t.Fatalf("build first pool: %v", err)
	}
	first.Config.Source = "subscription"

	second, err := manager.buildPool("sub-b", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-b", Type: "socks5", Server: "127.0.0.1", Port: 1081, Region: "US"},
			{Name: "jp-b", Type: "socks5", Server: "127.0.0.1", Port: 1082, Region: "JP"},
		},
	})
	if err != nil {
		t.Fatalf("build second pool: %v", err)
	}
	second.Config.Source = "subscription"

	manager.pools["sub-a"] = first
	manager.pools["sub-b"] = second

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		choice, err := manager.SelectEgress("US", "round-robin", false)
		if err != nil {
			t.Fatalf("select across subscriptions: %v", err)
		}
		seen[choice.NodeName] = true
	}

	if !seen["us-a"] || !seen["us-b"] {
		t.Fatalf("expected US nodes from both subscriptions, got %#v", seen)
	}
	if seen["jp-b"] {
		t.Fatalf("unexpected JP node in US global group")
	}
}

func TestSelectEgressUsesTemplateForColdRegion(t *testing.T) {
	manager := NewManager()
	template, err := manager.buildPool("brightdata", &PoolConfig{
		Source:           "template",
		Type:             "socks5",
		Server:           "proxy.example.com",
		Port:             22225,
		UsernameTemplate: "user-country-{region}",
		Password:         "secret",
	})
	if err != nil {
		t.Fatalf("build template pool: %v", err)
	}
	resTemplate, err := manager.buildPool("brightdata-res", &PoolConfig{
		Source:           "template",
		Residential:      true,
		Type:             "socks5",
		Server:           "res.example.com",
		Port:             22225,
		UsernameTemplate: "user-country-{REGION}",
		Password:         "secret",
	})
	if err != nil {
		t.Fatalf("build residential template pool: %v", err)
	}
	manager.pools["brightdata"] = template
	manager.pools["brightdata-res"] = resTemplate

	ordinaryChoice, err := manager.SelectEgress("NG", "random", false)
	if err != nil {
		t.Fatalf("select ordinary template: %v", err)
	}
	if !ordinaryChoice.Template || ordinaryChoice.PoolName != "brightdata" || ordinaryChoice.Group != "NG" {
		t.Fatalf("unexpected ordinary template choice: %#v", ordinaryChoice)
	}

	residentialChoice, err := manager.SelectEgress("NG", "random", true)
	if err != nil {
		t.Fatalf("select residential template: %v", err)
	}
	if !residentialChoice.Template || residentialChoice.PoolName != "brightdata-res" || residentialChoice.Group != "NG-RES" {
		t.Fatalf("unexpected residential template choice: %#v", residentialChoice)
	}
}

func TestSelectAnyEgressUsesLatencyThreshold(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("subscription", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "local-direct", Type: "direct", Region: "US"},
			{Name: "us-node", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "US"},
			{Name: "hk-node", Type: "socks5", Server: "127.0.0.1", Port: 1081, Region: "HK"},
			{Name: "jp-node", Type: "socks5", Server: "127.0.0.1", Port: 1082, Region: "JP"},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	pool.Entries[0].Latency = 5 * time.Millisecond
	pool.Entries[1].Latency = 120 * time.Millisecond
	pool.Entries[2].Latency = 50 * time.Millisecond
	manager.pools["subscription"] = pool

	choice, err := manager.SelectAnyEgress("least-latency", false, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("select any egress: %v", err)
	}
	if choice.NodeName != "hk-node" || choice.Group != "ANY" || choice.Region != "HK" {
		t.Fatalf("unexpected any choice: %#v", choice)
	}

	if _, err := manager.SelectAnyEgress("random", false, 40*time.Millisecond); err == nil {
		t.Fatalf("expected no node below latency threshold")
	}
}

func TestSelectAnyEgressSeparatesResidential(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("subscription", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-ordinary", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "US"},
			{Name: "us-res", Type: "socks5", Server: "127.0.0.1", Port: 1081, Region: "US", Residential: true},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	manager.pools["subscription"] = pool

	choice, err := manager.SelectAnyEgress("least-latency", true, 0)
	if err != nil {
		t.Fatalf("select residential any egress: %v", err)
	}
	if choice.NodeName != "us-res" || choice.Group != "ANY-RES" || !choice.Residential {
		t.Fatalf("unexpected residential any choice: %#v", choice)
	}
}

func TestNodeMetadataCanMapByServerKey(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("subscription", &PoolConfig{
		Source: "static",
		NodeServerRegions: map[string]string{
			"proxy.example.com:1080": "SG",
		},
		NodeServerAliases: map[string]string{
			"proxy.example.com:1080": "sg-primary",
		},
		NodeServerTags: map[string][]string{
			"proxy.example.com:1080": {"residential", "streaming"},
		},
		Nodes: []dialer.Node{
			{Name: "provider-name", Type: "socks5", Server: "proxy.example.com", Port: 1080},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	manager.pools["subscription"] = pool

	entry := pool.Entries[0]
	if entry.Region != "SG" || entry.Alias != "sg-primary" || !entry.Residential {
		t.Fatalf("unexpected server-key metadata: %#v", entry)
	}
	if !util.ContainsString(entry.Tags, "streaming") {
		t.Fatalf("expected server-key tags, got %#v", entry.Tags)
	}
}

func TestRejectRegexFiltersSubscriptionNodes(t *testing.T) {
	nodes := []dialer.Node{
		{Name: "套餐流量剩余", Type: "socks5", Server: "127.0.0.1", Port: 1080},
		{Name: "🇺🇸美国节点", Type: "socks5", Server: "127.0.0.1", Port: 1081},
	}

	filtered, err := applyRejectRegexes(nodes, []string{"流量|套餐"})
	if err != nil {
		t.Fatalf("apply reject regex: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Name != "🇺🇸美国节点" {
		t.Fatalf("unexpected filtered nodes: %#v", filtered)
	}
}

func TestRegionDetectionAvoidsPlainWordFalsePositives(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{name: "direct", want: "UN"},
		{name: "brightdata", want: "UN"},
		{name: "hk-socks", want: "HK"},
		{name: "US01 premium", want: "US"},
		{name: "UK-01", want: "GB"},
	}

	for _, tc := range cases {
		if got := detectRegionFromNodeName(tc.name); got != tc.want {
			t.Fatalf("detectRegionFromNodeName(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}
