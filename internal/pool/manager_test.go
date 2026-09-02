package pool

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestParseDurationWithDays(t *testing.T) {
	got, err := ParseDurationWithDays("3d")
	if err != nil {
		t.Fatalf("parse days: %v", err)
	}
	if got != 72*time.Hour {
		t.Fatalf("duration = %s, want 72h", got)
	}
}

func TestLoadFromFileKeepsPreviousSubscriptionEntriesOnFetchError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.yaml")
	data := []byte(`
node_pools:
  sub:
    source: subscription
    url: http://127.0.0.1:9/sub
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	manager := NewManager()
	previous, err := manager.buildPool("sub", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "old-node", Type: "direct", Region: "US"},
		},
	})
	if err != nil {
		t.Fatalf("build previous pool: %v", err)
	}
	previous.Config = &PoolConfig{Source: "subscription", URL: "http://127.0.0.1:9/sub"}
	previous.Entries[0].Alive = false
	previous.Entries[0].FailCount = 2
	previous.Entries[0].Latency = 123 * time.Millisecond
	manager.pools["sub"] = previous

	if err := manager.LoadFromFile(path); err != nil {
		t.Fatalf("load config: %v", err)
	}
	current := manager.GetPool("sub")
	if current == nil || len(current.Entries) != 1 {
		t.Fatalf("expected preserved entry, got %#v", current)
	}
	entry := current.Entries[0]
	if entry.Node.Name != "old-node" || entry.Alive || entry.FailCount != 2 || entry.Latency != 123*time.Millisecond {
		t.Fatalf("unexpected preserved entry: %#v", entry)
	}
	if current.Error == "" {
		t.Fatalf("expected current fetch error to remain visible")
	}
	if current.LastRefreshAt.IsZero() || !current.LastRefreshFailed {
		t.Fatalf("expected failed reload status to be preserved: %#v", current)
	}
}

func TestLoadFromFileRestoresPersistedSubscriptionCacheAfterRestart(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.yaml")
	cachePath := filepath.Join(dir, ".runtime", "subscriptions.json")
	subscriptionURL := "http://127.0.0.1:9/sub?token=cache-secret"
	data := []byte(`
node_pools:
  sub:
    source: subscription
    url: "http://127.0.0.1:9/sub?token=cache-secret"
`)
	if err := os.WriteFile(nodesPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cache := newSubscriptionCache(cachePath)
	if err := cache.save(subscriptionURL, []dialer.Node{
		{Name: "cached-node", Type: "direct", Region: "US"},
	}); err != nil {
		t.Fatalf("save subscription cache: %v", err)
	}

	manager := NewManager()
	manager.SetSubscriptionCachePath(cachePath)
	if err := manager.LoadFromFile(nodesPath); err != nil {
		t.Fatalf("load config: %v", err)
	}

	current := manager.GetPool("sub")
	if current == nil || len(current.Entries) != 1 {
		t.Fatalf("expected one cached entry, got %#v", current)
	}
	if current.Entries[0].Node.Name != "cached-node" {
		t.Fatalf("unexpected cached node: %#v", current.Entries[0].Node)
	}
	if !current.LastRefreshFailed || current.Error == "" {
		t.Fatalf("expected current pull failure to remain visible: %#v", current)
	}

	cacheData, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if strings.Contains(string(cacheData), subscriptionURL) || strings.Contains(string(cacheData), "cache-secret") {
		t.Fatalf("subscription cache must not store source URLs: %s", cacheData)
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat cache: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("cache permissions = %o want 600", got)
	}
}

func TestLoadFromFileDoesNotPreserveStaticEntriesWhenPoolBecomesSubscription(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.yaml")
	data := []byte(`
node_pools:
  sub:
    source: subscription
    url: http://127.0.0.1:9/sub
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	manager := NewManager()
	previous, err := manager.buildPool("sub", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "static-node", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "US"},
		},
	})
	if err != nil {
		t.Fatalf("build previous pool: %v", err)
	}
	manager.pools["sub"] = previous

	if err := manager.LoadFromFile(path); err != nil {
		t.Fatalf("load config: %v", err)
	}
	current := manager.GetPool("sub")
	if current == nil {
		t.Fatalf("expected subscription pool to remain loaded")
	}
	if len(current.Entries) != 0 {
		t.Fatalf("expected static entries not to be preserved, got %d", len(current.Entries))
	}
	if current.Error == "" {
		t.Fatalf("expected current fetch error to remain visible")
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
	if pool.LastRefreshAt.IsZero() || !pool.LastRefreshFailed {
		t.Fatalf("expected failed initial pull status, got %#v", pool)
	}
}

func TestSubscriptionPoolLoadsFromPrivateHostWhenConfigured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@proxy.example.com:1080#Local%20Subscription"))
	}))
	defer server.Close()

	manager := NewManager()
	loadedPool, err := manager.buildPool("local-sub", &PoolConfig{
		Source:           "subscription",
		URL:              server.URL,
		AllowPrivateHost: true,
	})
	if err != nil {
		t.Fatalf("build local subscription pool: %v", err)
	}
	if loadedPool.Error != "" {
		t.Fatalf("unexpected subscription error: %s", loadedPool.Error)
	}
	if len(loadedPool.Entries) != 1 || loadedPool.Entries[0].Node.Name != "Local Subscription" {
		t.Fatalf("unexpected subscription entries: %#v", loadedPool.Entries)
	}
	if loadedPool.LastRefreshAt.IsZero() || loadedPool.LastRefreshFailed {
		t.Fatalf("expected successful initial pull status, got %#v", loadedPool)
	}
}

func TestRefreshSubscriptionExposesLatestPullTimeAndResult(t *testing.T) {
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "temporary failure", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@proxy.example.com:1080#US%20Node"))
	}))
	defer server.Close()

	manager := NewManager()
	loadedPool, err := manager.buildPool("sub", &PoolConfig{
		Source:           "subscription",
		URL:              server.URL,
		AllowPrivateHost: true,
	})
	if err != nil {
		t.Fatalf("build subscription pool: %v", err)
	}
	manager.pools["sub"] = loadedPool

	initial := manager.GetPoolStatus()
	if len(initial) != 1 || initial[0].LastUpdated == "" || initial[0].LastRefreshFailed {
		t.Fatalf("unexpected initial pull status: %#v", initial)
	}
	initialTime, err := time.Parse(time.RFC3339Nano, initial[0].LastUpdated)
	if err != nil {
		t.Fatalf("parse initial last_updated: %v", err)
	}

	fail.Store(true)
	if err := manager.RefreshSubscription("sub"); err == nil {
		t.Fatal("expected refresh failure")
	}
	failed := manager.GetPoolStatus()
	if len(failed) != 1 || !failed[0].LastRefreshFailed || failed[0].LastUpdated == "" {
		t.Fatalf("unexpected failed pull status: %#v", failed)
	}
	failedTime, err := time.Parse(time.RFC3339Nano, failed[0].LastUpdated)
	if err != nil {
		t.Fatalf("parse failed last_updated: %v", err)
	}
	if failedTime.Before(initialTime) {
		t.Fatalf("failed pull time %s is before initial pull time %s", failedTime, initialTime)
	}

	fail.Store(false)
	if err := manager.RefreshSubscription("sub"); err != nil {
		t.Fatalf("refresh subscription after recovery: %v", err)
	}
	recovered := manager.GetPoolStatus()
	if len(recovered) != 1 || recovered[0].LastRefreshFailed || recovered[0].LastUpdated == "" {
		t.Fatalf("unexpected recovered pull status: %#v", recovered)
	}
}

func TestTryOfflineUsesSingleOfflineSubscriptionNode(t *testing.T) {
	manager := NewManager()
	nodePool, err := manager.buildPool("sub", &PoolConfig{
		Source:     "static",
		TryOffline: true,
		Nodes: []dialer.Node{
			{Name: "ng-only", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "NG"},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	nodePool.Config.Source = "subscription"
	nodePool.Entries[0].Alive = false
	manager.pools["sub"] = nodePool

	choice, err := manager.SelectEgress("NG", "random", false)
	if err != nil {
		t.Fatalf("select offline singleton: %v", err)
	}
	if choice.NodeName != "ng-only" || choice.PoolName != "sub" {
		t.Fatalf("unexpected offline singleton choice: %#v", choice)
	}
}

func TestTryOfflineRequiresOnlyOneMatchingNode(t *testing.T) {
	manager := NewManager()
	nodePool, err := manager.buildPool("sub", &PoolConfig{
		Source:     "static",
		TryOffline: true,
		Nodes: []dialer.Node{
			{Name: "ng-a", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "NG"},
			{Name: "ng-b", Type: "socks5", Server: "127.0.0.1", Port: 1081, Region: "NG"},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	nodePool.Config.Source = "subscription"
	nodePool.Entries[0].Alive = false
	nodePool.Entries[1].Alive = false
	manager.pools["sub"] = nodePool

	if _, err := manager.SelectEgress("NG", "random", false); err == nil {
		t.Fatalf("expected multiple offline nodes to stay unavailable")
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

func TestSelectEgressUsesPremiumAsPreference(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("subscription", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-ordinary", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "US"},
			{Name: "us-premium", Type: "socks5", Server: "127.0.0.1", Port: 1081, Region: "US", Premium: true},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	pool.Entries[0].Latency = 20 * time.Millisecond
	pool.Entries[1].Latency = 200 * time.Millisecond
	manager.pools["subscription"] = pool

	ordinaryChoice, err := manager.SelectEgressFor("US", "least-latency", EgressSelector{})
	if err != nil {
		t.Fatalf("select ordinary node: %v", err)
	}
	if ordinaryChoice.NodeName != "us-ordinary" || ordinaryChoice.Group != "US" || ordinaryChoice.Premium {
		t.Fatalf("unexpected ordinary choice: %#v", ordinaryChoice)
	}

	premiumChoice, err := manager.SelectEgressFor("US", "least-latency", EgressSelector{Premium: true})
	if err != nil {
		t.Fatalf("select premium node: %v", err)
	}
	if premiumChoice.NodeName != "us-premium" || premiumChoice.Group != "US" || !premiumChoice.Premium {
		t.Fatalf("unexpected premium choice: %#v", premiumChoice)
	}

	status := manager.GetPoolStatus()
	groups := map[string]RegionGroupStatus{}
	for _, group := range status[0].RegionGroups {
		groups[group.Group] = group
	}
	if groups["US"].Residential || groups["US"].Count != 2 {
		t.Fatalf("unexpected merged group: %#v", groups["US"])
	}
}

func TestSelectEgressFallsBackToOrdinaryWhenPremiumUnavailable(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("subscription", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-ordinary", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "US"},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	manager.pools["subscription"] = pool

	choice, err := manager.SelectEgressFor("US", "least-latency", EgressSelector{Premium: true})
	if err != nil {
		t.Fatalf("select premium fallback node: %v", err)
	}
	if choice.NodeName != "us-ordinary" || choice.Group != "US" || choice.Premium {
		t.Fatalf("unexpected premium fallback choice: %#v", choice)
	}
}

func TestSelectEgressPremiumCandidatesKeepOrdinaryFallback(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("subscription", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-ordinary", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "US"},
			{Name: "us-premium", Type: "socks5", Server: "127.0.0.1", Port: 1081, Region: "US", Premium: true},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	pool.Entries[0].Latency = 20 * time.Millisecond
	pool.Entries[1].Latency = 200 * time.Millisecond
	manager.pools["subscription"] = pool

	choices, err := manager.SelectEgressCandidatesFor("US", "least-latency", EgressSelector{Premium: true})
	if err != nil {
		t.Fatalf("select premium candidates: %v", err)
	}
	if len(choices) != 2 {
		t.Fatalf("unexpected candidate count: %d", len(choices))
	}
	if choices[0].NodeName != "us-premium" || !choices[0].Premium {
		t.Fatalf("premium candidate should be first: %#v", choices[0])
	}
	if choices[1].NodeName != "us-ordinary" || choices[1].Premium {
		t.Fatalf("ordinary candidate should remain as fallback: %#v", choices[1])
	}
}

func TestSelectEgressPremiumCandidatesAppendResidentialFallback(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("subscription", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-premium", Type: "socks5", Server: "127.0.0.1", Port: 1081, Region: "US", Premium: true},
			{Name: "us-res", Type: "socks5", Server: "127.0.0.1", Port: 1082, Region: "US", Residential: true},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	manager.pools["subscription"] = pool

	choices, err := manager.SelectEgressCandidatesFor("US", "least-latency", EgressSelector{Premium: true})
	if err != nil {
		t.Fatalf("select premium candidates: %v", err)
	}
	if len(choices) != 2 {
		t.Fatalf("unexpected candidate count: %d", len(choices))
	}
	if choices[0].NodeName != "us-premium" || !choices[0].Premium || choices[0].Residential {
		t.Fatalf("premium normal candidate should be first: %#v", choices[0])
	}
	if choices[1].NodeName != "us-res" || !choices[1].Residential || choices[1].Group != "US-RES" {
		t.Fatalf("residential fallback should remain available: %#v", choices[1])
	}
}

func TestSelectEgressPremiumPrefersPremiumTemplateOverOrdinaryNode(t *testing.T) {
	manager := NewManager()
	staticPool, err := manager.buildPool("static", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-ordinary", Type: "direct", Region: "US"},
		},
	})
	if err != nil {
		t.Fatalf("build static pool: %v", err)
	}
	templatePool, err := manager.buildPool("premium-template", &PoolConfig{
		Source:           "template",
		Type:             "direct",
		UsernameTemplate: "country-{region}",
		Premium:          true,
		Priority:         10,
	})
	if err != nil {
		t.Fatalf("build template pool: %v", err)
	}
	manager.pools["static"] = staticPool
	manager.pools["premium-template"] = templatePool

	choice, err := manager.SelectEgressFor("US", "least-latency", EgressSelector{Premium: true})
	if err != nil {
		t.Fatalf("select premium egress: %v", err)
	}
	if !choice.Template || choice.PoolName != "premium-template" || !choice.Premium || choice.Group != "US" {
		t.Fatalf("unexpected premium template choice: %#v", choice)
	}

	choices, err := manager.SelectEgressCandidatesWithTemplateFallbackFor("US", "least-latency", EgressSelector{Premium: true})
	if err != nil {
		t.Fatalf("select premium egress candidates: %v", err)
	}
	if len(choices) < 2 || choices[0].PoolName != "premium-template" || choices[1].NodeName != "us-ordinary" {
		t.Fatalf("unexpected premium fallback order: %#v", choices)
	}
}

func TestSelectEgressFallsBackToResidentialRegionWhenNormalUnavailable(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("subscription", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "mo-res", Type: "direct", Region: "MO", Residential: true},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	manager.pools["subscription"] = pool

	choice, err := manager.SelectEgress("MO", "random", false)
	if err != nil {
		t.Fatalf("select fallback residential node: %v", err)
	}
	if choice.NodeName != "mo-res" || choice.Group != "MO-RES" || !choice.Residential {
		t.Fatalf("unexpected residential fallback choice: %#v", choice)
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

func TestSelectEgressCandidatesLeastLatencyOrder(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("static", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-slow", Type: "direct", Region: "US"},
			{Name: "us-fast", Type: "direct", Region: "US"},
			{Name: "us-unknown", Type: "direct", Region: "US"},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	pool.Entries[0].Latency = 80 * time.Millisecond
	pool.Entries[1].Latency = 20 * time.Millisecond
	manager.pools["static"] = pool

	choices, err := manager.SelectEgressCandidates("US", "least-latency", false)
	if err != nil {
		t.Fatalf("select candidates: %v", err)
	}
	got := []string{choices[0].NodeName, choices[1].NodeName, choices[2].NodeName}
	want := []string{"us-fast", "us-slow", "us-unknown"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate order: got %v want %v", got, want)
		}
	}
}

func TestSelectEgressCandidatesRoundRobinStartsAtNextNode(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("static", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-a", Type: "direct", Region: "US"},
			{Name: "us-b", Type: "direct", Region: "US"},
			{Name: "us-c", Type: "direct", Region: "US"},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	manager.pools["static"] = pool

	first, err := manager.SelectEgressCandidates("US", "round-robin", false)
	if err != nil {
		t.Fatalf("select first candidates: %v", err)
	}
	second, err := manager.SelectEgressCandidates("US", "round-robin", false)
	if err != nil {
		t.Fatalf("select second candidates: %v", err)
	}
	if first[0].NodeName != "us-a" || first[1].NodeName != "us-b" {
		t.Fatalf("unexpected first order: %#v", first)
	}
	if second[0].NodeName != "us-b" || second[1].NodeName != "us-c" {
		t.Fatalf("unexpected second order: %#v", second)
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

func TestSelectEgressTemplatesUsePriorityOrder(t *testing.T) {
	manager := NewManager()
	low, err := manager.buildPool("lumi", &PoolConfig{
		Source:           "template",
		Type:             "socks5",
		Server:           "lumi.example.com",
		Port:             22225,
		UsernameTemplate: "lumi-{region}",
		Password:         "secret",
		Priority:         10,
	})
	if err != nil {
		t.Fatalf("build low priority template: %v", err)
	}
	high, err := manager.buildPool("chijie-b", &PoolConfig{
		Source:       "template",
		TemplateType: "chijie",
		Endpoint:     "https://b.example.com",
		BearerToken:  "token",
		Priority:     100,
	})
	if err != nil {
		t.Fatalf("build high priority template: %v", err)
	}
	mid, err := manager.buildPool("brightdata", &PoolConfig{
		Source:           "template",
		Type:             "socks5",
		Server:           "brightdata.example.com",
		Port:             22225,
		UsernameTemplate: "bd-{region}",
		Password:         "secret",
		Priority:         50,
	})
	if err != nil {
		t.Fatalf("build mid priority template: %v", err)
	}
	manager.pools["lumi"] = low
	manager.pools["chijie-b"] = high
	manager.pools["brightdata"] = mid

	choices, err := manager.SelectEgressCandidates("NG", "random", false)
	if err != nil {
		t.Fatalf("select template candidates: %v", err)
	}
	got := []string{choices[0].PoolName, choices[1].PoolName, choices[2].PoolName}
	want := []string{"chijie-b", "brightdata", "lumi"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("template priority order: got %v want %v", got, want)
		}
	}
	if choices[0].TemplateType != "chijie" || choices[0].Endpoint != "https://b.example.com/proxy" {
		t.Fatalf("unexpected chijie choice: %#v", choices[0])
	}
}

func TestSelectEgressCandidatesWithTemplateFallbackAppendsTemplatesAfterNodes(t *testing.T) {
	manager := NewManager()
	staticPool, err := manager.buildPool("static", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-a", Type: "direct", Region: "US"},
			{Name: "us-b", Type: "direct", Region: "US"},
		},
	})
	if err != nil {
		t.Fatalf("build static pool: %v", err)
	}
	templatePool, err := manager.buildPool("brightdata", &PoolConfig{
		Source:           "template",
		Type:             "direct",
		UsernameTemplate: "country-{region}",
		Priority:         100,
	})
	if err != nil {
		t.Fatalf("build template pool: %v", err)
	}
	manager.pools["static"] = staticPool
	manager.pools["brightdata"] = templatePool

	choices, err := manager.SelectEgressCandidatesWithTemplateFallback("US", "least-latency", false)
	if err != nil {
		t.Fatalf("select candidates: %v", err)
	}
	got := []string{choices[0].NodeName, choices[1].NodeName, choices[2].NodeName}
	want := []string{"us-a", "us-b", "brightdata-us"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate order: got %v want %v", got, want)
		}
	}
	if !choices[2].Template {
		t.Fatalf("last choice should be template: %#v", choices[2])
	}
}

func TestMarkNodeUnavailableSetsAliveFalse(t *testing.T) {
	manager := NewManager()
	nodePool, err := manager.buildPool("static", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-a", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "US"},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	manager.pools["static"] = nodePool

	if ok := manager.MarkNodeUnavailable("static", "us-a"); !ok {
		t.Fatalf("expected node to be marked unavailable")
	}
	entry := manager.GetPool("static").Entries[0]
	if entry.Alive || entry.FailCount != 1 {
		t.Fatalf("unexpected marked entry: %#v", entry)
	}
}

func TestChijieTemplateRejectsHTTP(t *testing.T) {
	manager := NewManager()
	_, err := manager.buildPool("remote", &PoolConfig{
		Source:       "template",
		TemplateType: "chijie",
		Endpoint:     "http://b.example.com",
		BearerToken:  "token",
	})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected https-only chijie template error, got %v", err)
	}
}

func TestTemplateConnectivityUsesRemoteChijieProxy(t *testing.T) {
	var seen struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Egress  struct {
			Region      string `json:"region"`
			Strategy    string `json:"strategy"`
			Residential bool   `json:"residential"`
		} `json:"egress"`
	}
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy" {
			t.Fatalf("path: got %q want /proxy", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer remote-token" {
			t.Fatalf("authorization: got %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode proxy body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer remote.Close()

	oldClientFactory := newChijieTemplateHTTPClient
	newChijieTemplateHTTPClient = func(timeout time.Duration) *http.Client {
		client := remote.Client()
		client.Timeout = timeout
		return client
	}
	defer func() { newChijieTemplateHTTPClient = oldClientFactory }()

	manager := NewManager()
	pool, err := manager.buildPool("remote", &PoolConfig{
		Source:       "template",
		TemplateType: "chijie",
		Endpoint:     remote.URL,
		BearerToken:  "remote-token",
		Coverage:     "both",
	})
	if err != nil {
		t.Fatalf("build chijie template: %v", err)
	}
	manager.pools["remote"] = pool

	result, err := manager.TestTemplateConnectivity("remote", "ng", "", 0)
	if err != nil {
		t.Fatalf("test chijie template: %v", err)
	}
	if !result.OK || result.TemplateType != "chijie" || result.Phase != "remote_chijie" || result.HTTPStatus != http.StatusOK {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.TestURL != defaultTemplateTestURL {
		t.Fatalf("test url: got %q want %q", result.TestURL, defaultTemplateTestURL)
	}
	if seen.URL != defaultTemplateTestURL || seen.Method != http.MethodGet {
		t.Fatalf("unexpected remote proxy payload: %#v", seen)
	}
	if seen.Egress.Region != "NG" || seen.Egress.Strategy != "least-latency" || seen.Egress.Residential {
		t.Fatalf("unexpected remote egress payload: %#v", seen.Egress)
	}
}

func TestTemplateConnectivityReportsRemoteChijieGatewayError(t *testing.T) {
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(chijieErrorHeader, "proxy request failed")
		http.Error(w, `{"error":"no nodes"}`, http.StatusBadGateway)
	}))
	defer remote.Close()

	oldClientFactory := newChijieTemplateHTTPClient
	newChijieTemplateHTTPClient = func(timeout time.Duration) *http.Client {
		client := remote.Client()
		client.Timeout = timeout
		return client
	}
	defer func() { newChijieTemplateHTTPClient = oldClientFactory }()

	manager := NewManager()
	pool, err := manager.buildPool("remote", &PoolConfig{
		Source:       "template",
		TemplateType: "chijie",
		Endpoint:     remote.URL,
		BearerToken:  "remote-token",
		Coverage:     "both",
	})
	if err != nil {
		t.Fatalf("build chijie template: %v", err)
	}
	manager.pools["remote"] = pool

	result, err := manager.TestTemplateConnectivity("remote", "US", "https://api.ipify.org?format=json", 0)
	if err != nil {
		t.Fatalf("test chijie template: %v", err)
	}
	if result.OK {
		t.Fatalf("expected remote chijie gateway error, got %#v", result)
	}
	if result.HTTPStatus != http.StatusBadGateway || !strings.Contains(result.Error, "no nodes") {
		t.Fatalf("unexpected remote error result: %#v", result)
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

func TestSelectAnyEgressSeparatesPremium(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("subscription", &PoolConfig{
		Source: "static",
		Nodes: []dialer.Node{
			{Name: "us-ordinary", Type: "socks5", Server: "127.0.0.1", Port: 1080, Region: "US"},
			{Name: "hk-premium", Type: "socks5", Server: "127.0.0.1", Port: 1081, Region: "HK", Premium: true},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	manager.pools["subscription"] = pool

	choice, err := manager.SelectAnyEgressFor("least-latency", EgressSelector{Premium: true}, 0)
	if err != nil {
		t.Fatalf("select premium any egress: %v", err)
	}
	if choice.NodeName != "hk-premium" || choice.Group != "ANY" || !choice.Premium {
		t.Fatalf("unexpected premium any choice: %#v", choice)
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

func TestPremiumCanComeFromPoolAndTags(t *testing.T) {
	manager := NewManager()
	pool, err := manager.buildPool("subscription", &PoolConfig{
		Source:  "static",
		Premium: true,
		Nodes: []dialer.Node{
			{Name: "provider-pool", Type: "socks5", Server: "pool.example.com", Port: 1080, Region: "US"},
		},
	})
	if err != nil {
		t.Fatalf("build pool: %v", err)
	}
	if !pool.Entries[0].Premium {
		t.Fatalf("expected pool-level premium to apply: %#v", pool.Entries[0])
	}

	tagged, err := manager.buildPool("tagged", &PoolConfig{
		Source: "static",
		NodeTags: map[string][]string{
			"provider-tag": {"premium"},
		},
		Nodes: []dialer.Node{
			{Name: "provider-tag", Type: "socks5", Server: "tag.example.com", Port: 1080, Region: "US"},
		},
	})
	if err != nil {
		t.Fatalf("build tagged pool: %v", err)
	}
	if !tagged.Entries[0].Premium {
		t.Fatalf("expected premium tag to apply: %#v", tagged.Entries[0])
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

func TestBuildSubscriptionEntriesReportsUnsupportedTransports(t *testing.T) {
	nodes := []dialer.Node{
		{
			Name:   "vmess-xhttp",
			Type:   "vmess",
			Server: "vmess.example.com",
			Port:   443,
			Extra: map[string]string{
				"uuid":     "11111111-1111-1111-1111-111111111111",
				"security": "tls",
				"network":  "xhttp",
				"sni":      "vless.example.com",
			},
		},
	}

	entries, warning := buildSubscriptionEntries(nodes, &PoolConfig{Source: "subscription"})
	if len(entries) != 0 {
		t.Fatalf("expected unsupported node to be skipped, got %d entries", len(entries))
	}
	if !strings.Contains(warning, "loaded 0 supported nodes") || !strings.Contains(warning, `unsupported v2ray transport "xhttp"`) {
		t.Fatalf("unexpected warning: %q", warning)
	}
}

func TestBuildSubscriptionEntriesLoadsVLESSXHTTP(t *testing.T) {
	for _, mode := range []string{"packet-up", "stream-up", "stream-one", "auto"} {
		t.Run(mode, func(t *testing.T) {
			nodes := []dialer.Node{
				{
					Name:   "vless-xhttp-" + mode,
					Type:   "vless",
					Server: "vless.example.com",
					Port:   443,
					Extra: map[string]string{
						"uuid":       "11111111-1111-1111-1111-111111111111",
						"security":   "tls",
						"sni":        "vless.example.com",
						"network":    "xhttp",
						"xhttp_mode": mode,
						"xhttp_path": "/path",
					},
				},
			}

			entries, warning := buildSubscriptionEntries(nodes, &PoolConfig{Source: "subscription"})
			if warning != "" {
				t.Fatalf("unexpected warning: %q", warning)
			}
			if len(entries) != 1 {
				t.Fatalf("expected xhttp node to load, got %d entries", len(entries))
			}
		})
	}
}

func TestBuildSubscriptionEntriesReportsUnsupportedXHTTPDetail(t *testing.T) {
	nodes := []dialer.Node{
		{
			Name:   "vless-xhttp-download-settings",
			Type:   "vless",
			Server: "vless.example.com",
			Port:   443,
			Extra: map[string]string{
				"uuid":                    "11111111-1111-1111-1111-111111111111",
				"security":                "tls",
				"network":                 "xhttp",
				"xhttp_mode":              "auto",
				"xhttp_download_settings": "true",
			},
		},
	}

	entries, warning := buildSubscriptionEntries(nodes, &PoolConfig{Source: "subscription"})
	if len(entries) != 0 {
		t.Fatalf("expected unsupported node to be skipped, got %d entries", len(entries))
	}
	if !strings.Contains(warning, "unsupported xhttp downloadSettings") {
		t.Fatalf("unexpected warning: %q", warning)
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
		{name: "🇨🇳 TW 01", want: "TW"},
		{name: "🇨🇳 中国 01", want: "CN"},
	}

	for _, tc := range cases {
		if got := detectRegionFromNodeName(tc.name); got != tc.want {
			t.Fatalf("detectRegionFromNodeName(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}
