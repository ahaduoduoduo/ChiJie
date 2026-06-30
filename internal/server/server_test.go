package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"chijie/internal/pool"
)

type testDialer struct {
	name string
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (d *testDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.dial(ctx, network, addr)
}

func (d *testDialer) GetHTTPTransport() *http.Transport {
	return &http.Transport{
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     false,
		ResponseHeaderTimeout: 5 * time.Second,
	}
}

func (d *testDialer) Name() string {
	return d.name
}

func TestParseProxySettingsDefaultsResponseHeaderTimeout(t *testing.T) {
	settings, err := ParseProxySettings(nil)
	if err != nil {
		t.Fatalf("parse default proxy settings: %v", err)
	}
	if settings.ResponseHeaderTimeout != 3*time.Second {
		t.Fatalf("response header timeout = %s, want 3s", settings.ResponseHeaderTimeout)
	}
	if settings.TotalTimeout != 30*time.Second {
		t.Fatalf("total timeout = %s, want 30s", settings.TotalTimeout)
	}
	if settings.MaxRedirects != 5 {
		t.Fatalf("max redirects = %d, want 5", settings.MaxRedirects)
	}
}

func TestParseProxySettingsAcceptsTimeouts(t *testing.T) {
	settings, err := ParseProxySettings(&ProxySettingsConfig{
		MaxAttempts:           2,
		MaxRedirects:          4,
		ResponseHeaderTimeout: "750ms",
		TotalTimeout:          "45s",
	})
	if err != nil {
		t.Fatalf("parse proxy settings: %v", err)
	}
	if settings.ResponseHeaderTimeout != 750*time.Millisecond {
		t.Fatalf("response header timeout = %s, want 750ms", settings.ResponseHeaderTimeout)
	}
	if settings.TotalTimeout != 45*time.Second {
		t.Fatalf("total timeout = %s, want 45s", settings.TotalTimeout)
	}
	if settings.MaxRedirects != 4 {
		t.Fatalf("max redirects = %d, want 4", settings.MaxRedirects)
	}
}

func TestParseProxySettingsAcceptsLegacyRequestTimeout(t *testing.T) {
	settings, err := ParseProxySettings(&ProxySettingsConfig{RequestTimeout: "900ms"})
	if err != nil {
		t.Fatalf("parse legacy proxy settings: %v", err)
	}
	if settings.TotalTimeout != 900*time.Millisecond {
		t.Fatalf("total timeout = %s, want 900ms", settings.TotalTimeout)
	}
}

func TestParseProxySettingsRejectsInvalidResponseHeaderTimeout(t *testing.T) {
	_, err := ParseProxySettings(&ProxySettingsConfig{ResponseHeaderTimeout: "0s"})
	if err == nil {
		t.Fatalf("expected invalid response header timeout error")
	}
}

func TestParseProxySettingsRejectsInvalidTotalTimeout(t *testing.T) {
	_, err := ParseProxySettings(&ProxySettingsConfig{TotalTimeout: "0s"})
	if err == nil {
		t.Fatalf("expected invalid total timeout error")
	}
}

func TestParseProxySettingsRejectsInvalidMaxRedirects(t *testing.T) {
	_, err := ParseProxySettings(&ProxySettingsConfig{MaxRedirects: 51})
	if err == nil {
		t.Fatalf("expected invalid max redirects error")
	}
}

func TestDoProxyTotalTimeoutCoversResponseBodyRead(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("late body"))
	}))
	defer target.Close()

	s := &Server{
		allowPrivateTargets: true,
		proxySettings: ProxySettings{
			MaxAttempts:                   1,
			TemplateFallbackAfterAttempts: true,
			ResponseHeaderTimeout:         time.Second,
			TotalTimeout:                  50 * time.Millisecond,
		},
	}
	started := time.Now()
	_, err := s.doProxy(context.Background(), &ProxyRequest{
		URL:    target.URL,
		Method: http.MethodGet,
	}, &egressRoute{Direct: true, Group: "DIRECT"})
	if err == nil {
		t.Fatalf("expected total timeout while reading response body")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("total timeout took too long: %s", elapsed)
	}
	if !strings.Contains(err.Error(), "read response") {
		t.Fatalf("expected body read timeout, got: %v", err)
	}
}

func TestHandleProxyForwardsSetCookieHeaders(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Add("Set-Cookie", "session=abc; Path=/; HttpOnly")
		w.Header().Add("Set-Cookie", "pref=dark; Path=/")
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()

	jwtSecret := "jwt-secret"
	s := &Server{
		auth:                NewAuth(jwtSecret),
		allowPrivateTargets: true,
		proxySettings:       DefaultProxySettings(),
	}
	gateway := httptest.NewServer(http.HandlerFunc(s.handleProxy))
	defer gateway.Close()

	body, err := json.Marshal(ProxyRequest{URL: target.URL, Method: http.MethodGet})
	if err != nil {
		t.Fatalf("marshal proxy request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, gateway.URL, strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("create proxy request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+signedProxyToken(t, jwtSecret))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(respBody) != "ok" {
		t.Fatalf("unexpected response: status=%d body=%q", resp.StatusCode, respBody)
	}

	got := resp.Header.Values("Set-Cookie")
	want := []string{"session=abc; Path=/; HttpOnly", "pref=dark; Path=/"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("set-cookie headers: got %v want %v", got, want)
	}
}

func TestHandleProxyDoesNotFollowRedirectsByDefault(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("final"))
	}))
	defer target.Close()

	jwtSecret := "jwt-secret"
	s := &Server{
		auth:                NewAuth(jwtSecret),
		allowPrivateTargets: true,
		proxySettings:       DefaultProxySettings(),
	}
	gateway := httptest.NewServer(http.HandlerFunc(s.handleProxy))
	defer gateway.Close()

	body, err := json.Marshal(ProxyRequest{URL: target.URL + "/start", Method: http.MethodGet})
	if err != nil {
		t.Fatalf("marshal proxy request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, gateway.URL, strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("create proxy request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+signedProxyToken(t, jwtSecret))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if got, want := resp.Header.Get("Location"), "/final"; got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}
	if got := resp.Header.Get(chijieRedirectCountHeader); got != "" {
		t.Fatalf("redirect count header should be empty, got %q", got)
	}
}

func TestHandleProxyFollowsRedirectsWhenRequested(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("final"))
	}))
	defer target.Close()

	jwtSecret := "jwt-secret"
	s := &Server{
		auth:                NewAuth(jwtSecret),
		allowPrivateTargets: true,
		proxySettings:       DefaultProxySettings(),
	}
	gateway := httptest.NewServer(http.HandlerFunc(s.handleProxy))
	defer gateway.Close()

	body, err := json.Marshal(ProxyRequest{URL: target.URL + "/start", Method: http.MethodGet, FollowRedirects: true})
	if err != nil {
		t.Fatalf("marshal proxy request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, gateway.URL, strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("create proxy request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+signedProxyToken(t, jwtSecret))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(respBody) != "final" {
		t.Fatalf("unexpected response: status=%d body=%q", resp.StatusCode, respBody)
	}
	if got, want := resp.Header.Get(chijieFinalURLHeader), target.URL+"/final"; got != want {
		t.Fatalf("final url header = %q, want %q", got, want)
	}
	if got := resp.Header.Get(chijieRedirectCountHeader); got != "1" {
		t.Fatalf("redirect count header = %q, want 1", got)
	}
	var redirects []proxyRedirect
	if err := json.Unmarshal([]byte(resp.Header.Get(chijieRedirectsHeader)), &redirects); err != nil {
		t.Fatalf("decode redirects header: %v", err)
	}
	if len(redirects) != 1 || redirects[0].StatusCode != http.StatusFound || redirects[0].ToURL != target.URL+"/final" {
		t.Fatalf("unexpected redirects: %#v", redirects)
	}
}

func TestHandleProxyStopsAtConfiguredMaxRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/middle", http.StatusFound)
		case "/middle":
			http.Redirect(w, r, "/final", http.StatusFound)
		default:
			_, _ = w.Write([]byte("final"))
		}
	}))
	defer target.Close()

	settings := DefaultProxySettings()
	settings.MaxRedirects = 1
	jwtSecret := "jwt-secret"
	s := &Server{
		auth:                NewAuth(jwtSecret),
		allowPrivateTargets: true,
		proxySettings:       settings,
	}
	gateway := httptest.NewServer(http.HandlerFunc(s.handleProxy))
	defer gateway.Close()

	body, err := json.Marshal(ProxyRequest{URL: target.URL + "/start", Method: http.MethodGet, FollowRedirects: true})
	if err != nil {
		t.Fatalf("marshal proxy request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, gateway.URL, strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("create proxy request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+signedProxyToken(t, jwtSecret))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if got, want := resp.Header.Get(chijieFinalURLHeader), target.URL+"/middle"; got != want {
		t.Fatalf("final url header = %q, want %q", got, want)
	}
	if got := resp.Header.Get(chijieRedirectCountHeader); got != "1" {
		t.Fatalf("redirect count header = %q, want 1", got)
	}
	if got := resp.Header.Get(chijieRedirectLimitReachedHeader); got != "true" {
		t.Fatalf("redirect limit header = %q, want true", got)
	}
	if got, want := resp.Header.Get("Location"), "/final"; got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}
}

func TestResolveEgressUsesTemplateWhenRegionGroupMissing(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.yaml")
	data := []byte(`
node_pools:
  primary:
    source: static
    nodes:
      - name: us-node
        type: direct
        region: US
  brightdata:
    source: template
    type: direct
    username_template: "country-{region}"
`)
	if err := os.WriteFile(nodesPath, data, 0644); err != nil {
		t.Fatalf("write nodes: %v", err)
	}

	manager := pool.NewManager()
	if err := manager.LoadFromFile(nodesPath); err != nil {
		t.Fatalf("load nodes: %v", err)
	}

	s := &Server{poolManager: manager}
	route, err := s.resolveEgress(EgressOptions{Region: "JP", Strategy: "random"})
	if err != nil {
		t.Fatalf("resolve egress with fallback template: %v", err)
	}
	if route.Choice == nil || !route.Choice.Template {
		t.Fatalf("expected template choice, got %#v", route.Choice)
	}
	if d, err := s.getDialer(route); err != nil || d.Name() != "direct" {
		t.Fatalf("unexpected fallback dialer: %v %v", d, err)
	}
}

func TestResolveEgressEmptyRegionUsesDirect(t *testing.T) {
	s := &Server{}
	route, err := s.resolveEgress(EgressOptions{})
	if err != nil {
		t.Fatalf("resolve direct egress: %v", err)
	}
	if !route.Direct || route.Group != "DIRECT" {
		t.Fatalf("expected direct route, got %#v", route)
	}
}

func TestResolveEgressPremiumWithoutRegionUsesPremiumNode(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.yaml")
	data := []byte(`
node_pools:
  primary:
    source: static
    nodes:
      - name: us-normal
        type: socks5
        server: 127.0.0.1
        port: 1080
        region: US
      - name: hk-premium
        type: socks5
        server: 127.0.0.1
        port: 1081
        region: HK
        premium: true
      - name: us-res
        type: socks5
        server: 127.0.0.1
        port: 1082
        region: US
        residential: true
`)
	if err := os.WriteFile(nodesPath, data, 0644); err != nil {
		t.Fatalf("write nodes: %v", err)
	}

	manager := pool.NewManager()
	if err := manager.LoadFromFile(nodesPath); err != nil {
		t.Fatalf("load nodes: %v", err)
	}

	s := &Server{poolManager: manager}
	route, err := s.resolveEgress(EgressOptions{Premium: true, Strategy: "least-latency"})
	if err != nil {
		t.Fatalf("resolve premium egress: %v", err)
	}
	if route.Direct || route.Group != "ANY" || !route.Premium || route.Choice == nil || route.Choice.NodeName != "hk-premium" || !route.Choice.Premium {
		t.Fatalf("unexpected premium route: %#v", route)
	}
	if len(route.Choices) != 3 || route.Choices[2].NodeName != "us-res" || !route.Choices[2].Residential {
		t.Fatalf("expected residential fallback to remain available: %#v", route.Choices)
	}
}

func TestResolveEgressPremiumWithoutRegionFallsBackToPremiumResidential(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.yaml")
	data := []byte(`
node_pools:
  primary:
    source: static
    nodes:
      - name: us-premium-res
        type: socks5
        server: 127.0.0.1
        port: 1081
        region: US
        residential: true
        premium: true
`)
	if err := os.WriteFile(nodesPath, data, 0644); err != nil {
		t.Fatalf("write nodes: %v", err)
	}

	manager := pool.NewManager()
	if err := manager.LoadFromFile(nodesPath); err != nil {
		t.Fatalf("load nodes: %v", err)
	}

	s := &Server{poolManager: manager}
	route, err := s.resolveEgress(EgressOptions{Premium: true, Strategy: "least-latency"})
	if err != nil {
		t.Fatalf("resolve premium residential fallback egress: %v", err)
	}
	if route.Group != "ANY-RES" || !route.Premium || !route.Residential || route.Choice == nil || route.Choice.NodeName != "us-premium-res" || !route.Choice.Premium {
		t.Fatalf("unexpected premium residential fallback route: %#v", route)
	}
}

func TestResolveEgressUsesResidentialFallbackRoute(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.yaml")
	data := []byte(`
node_pools:
  primary:
    source: static
    nodes:
      - name: mo-res
        type: direct
        region: MO
        residential: true
`)
	if err := os.WriteFile(nodesPath, data, 0644); err != nil {
		t.Fatalf("write nodes: %v", err)
	}

	manager := pool.NewManager()
	if err := manager.LoadFromFile(nodesPath); err != nil {
		t.Fatalf("load nodes: %v", err)
	}

	s := &Server{poolManager: manager}
	route, err := s.resolveEgress(EgressOptions{Region: "MO", Strategy: "random", Residential: false})
	if err != nil {
		t.Fatalf("resolve egress with residential fallback: %v", err)
	}
	if route.Group != "MO-RES" || !route.Residential || route.Choice == nil || route.Choice.NodeName != "mo-res" {
		t.Fatalf("unexpected residential fallback route: %#v", route)
	}
}

func TestResolveEgressAnyUsesLatencyLimitedNode(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.yaml")
	data := []byte(`
node_pools:
  primary:
    source: static
    nodes:
      - name: us-node
        type: socks5
        server: 127.0.0.1
        port: 1080
        region: US
      - name: hk-node
        type: socks5
        server: 127.0.0.1
        port: 1081
        region: HK
`)
	if err := os.WriteFile(nodesPath, data, 0644); err != nil {
		t.Fatalf("write nodes: %v", err)
	}

	manager := pool.NewManager()
	if err := manager.LoadFromFile(nodesPath); err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	entries := manager.GetPool("primary").Entries
	entries[0].Latency = 150 * time.Millisecond
	entries[1].Latency = 30 * time.Millisecond

	s := &Server{poolManager: manager}
	route, err := s.resolveEgress(EgressOptions{Any: true, Strategy: "least-latency", MaxLatencyMS: 100})
	if err != nil {
		t.Fatalf("resolve any egress: %v", err)
	}
	if route.Direct || !route.Any || route.Group != "ANY" || route.Choice == nil || route.Choice.NodeName != "hk-node" {
		t.Fatalf("unexpected any route: %#v", route)
	}
}

func TestResolveEgressAnyRegionAlias(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.yaml")
	data := []byte(`
node_pools:
  primary:
    source: static
    nodes:
      - name: us-node
        type: socks5
        server: 127.0.0.1
        port: 1080
        region: US
`)
	if err := os.WriteFile(nodesPath, data, 0644); err != nil {
		t.Fatalf("write nodes: %v", err)
	}

	manager := pool.NewManager()
	if err := manager.LoadFromFile(nodesPath); err != nil {
		t.Fatalf("load nodes: %v", err)
	}

	s := &Server{poolManager: manager}
	route, err := s.resolveEgress(EgressOptions{Region: "*", Strategy: "random"})
	if err != nil {
		t.Fatalf("resolve wildcard egress: %v", err)
	}
	if !route.Any || route.Group != "ANY" || route.Choice == nil || route.Choice.NodeName != "us-node" {
		t.Fatalf("unexpected wildcard route: %#v", route)
	}
}

func TestDoProxyWithRetryUsesNextCandidateOnTransportError(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()

	var firstAttempts int
	first := &testDialer{
		name: "first",
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			firstAttempts++
			return nil, errors.New("simulated EOF")
		},
	}
	second := &testDialer{
		name: "second",
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	route := &egressRoute{
		Region:   "US",
		Group:    "US",
		Strategy: "least-latency",
		Choices: []*pool.EgressChoice{
			{Dialer: first, PoolName: "pool", NodeName: "first", Source: "static", Region: "US", Group: "US"},
			{Dialer: second, PoolName: "pool", NodeName: "second", Source: "static", Region: "US", Group: "US"},
		},
	}

	resp, finalRoute, err := (&Server{}).doProxyWithRetry(context.Background(), &ProxyRequest{
		URL:    target.URL,
		Method: http.MethodGet,
	}, route)
	if err != nil {
		t.Fatalf("proxy with retry: %v", err)
	}
	if string(resp.Body) != "ok" || resp.StatusCode != http.StatusOK || resp.ContentType != "text/plain" {
		t.Fatalf("unexpected response: body=%q status=%d contentType=%q", resp.Body, resp.StatusCode, resp.ContentType)
	}
	if finalRoute == nil || finalRoute.Choice == nil || finalRoute.Choice.NodeName != "second" {
		t.Fatalf("expected second candidate, got %#v", finalRoute)
	}
	if firstAttempts != 1 {
		t.Fatalf("first candidate attempts: got %d want 1", firstAttempts)
	}
}

func TestProxyAttemptRoutesUseConfiguredNodeAttemptsBeforeTemplate(t *testing.T) {
	choices := []*pool.EgressChoice{
		{PoolName: "pool", NodeName: "node-1", Source: "static", Region: "US", Group: "US"},
		{PoolName: "pool", NodeName: "node-2", Source: "static", Region: "US", Group: "US"},
		{PoolName: "pool", NodeName: "node-3", Source: "static", Region: "US", Group: "US"},
		{PoolName: "pool", NodeName: "node-4", Source: "static", Region: "US", Group: "US"},
		{PoolName: "pool", NodeName: "node-5", Source: "static", Region: "US", Group: "US"},
		{PoolName: "pool", NodeName: "node-6", Source: "static", Region: "US", Group: "US"},
		{PoolName: "tpl", NodeName: "tpl-us", Source: "template", Template: true, Region: "US", Group: "US"},
	}
	route := &egressRoute{Region: "US", Group: "US", Choices: choices}

	routes := proxyAttemptRoutes(route, ProxySettings{MaxAttempts: 5, TemplateFallbackAfterAttempts: true})
	var got []string
	for _, item := range routes {
		got = append(got, item.Choice.NodeName)
	}
	want := []string{"node-1", "node-2", "node-3", "node-4", "node-5", "tpl-us"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("attempt route order: got %v want %v", got, want)
	}
}

func TestProxyAttemptRoutesKeepPremiumTemplateBeforeOrdinaryFallback(t *testing.T) {
	choices := []*pool.EgressChoice{
		{PoolName: "premium-tpl", NodeName: "premium-tpl-us", Source: "template", Template: true, Premium: true, Region: "US", Group: "US"},
		{PoolName: "pool", NodeName: "node-1", Source: "static", Region: "US", Group: "US"},
	}
	route := &egressRoute{Region: "US", Group: "US", Premium: true, Choices: choices}

	routes := proxyAttemptRoutes(route, ProxySettings{MaxAttempts: 5, TemplateFallbackAfterAttempts: false})
	var got []string
	for _, item := range routes {
		got = append(got, item.Choice.NodeName)
	}
	want := []string{"premium-tpl-us", "node-1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("attempt route order: got %v want %v", got, want)
	}
}

func TestDoProxyWithRetryMarksFailedNodeUnavailable(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()

	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.yaml")
	data := []byte(`
node_pools:
  primary:
    source: static
    nodes:
      - name: us-fail
        type: socks5
        server: 127.0.0.1
        port: 1080
        region: US
      - name: us-ok
        type: socks5
        server: 127.0.0.1
        port: 1081
        region: US
`)
	if err := os.WriteFile(nodesPath, data, 0644); err != nil {
		t.Fatalf("write nodes: %v", err)
	}
	manager := pool.NewManager()
	if err := manager.LoadFromFile(nodesPath); err != nil {
		t.Fatalf("load nodes: %v", err)
	}

	first := &testDialer{
		name: "first",
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return nil, errors.New("simulated timeout")
		},
	}
	second := &testDialer{
		name: "second",
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	route := &egressRoute{
		Region: "US",
		Group:  "US",
		Choices: []*pool.EgressChoice{
			{Dialer: first, PoolName: "primary", NodeName: "us-fail", Source: "static", Region: "US", Group: "US"},
			{Dialer: second, PoolName: "primary", NodeName: "us-ok", Source: "static", Region: "US", Group: "US"},
		},
	}

	_, finalRoute, err := (&Server{poolManager: manager, proxySettings: DefaultProxySettings()}).doProxyWithRetry(context.Background(), &ProxyRequest{
		URL:    target.URL,
		Method: http.MethodGet,
	}, route)
	if err != nil {
		t.Fatalf("proxy with retry: %v", err)
	}
	if finalRoute == nil || finalRoute.Choice.NodeName != "us-ok" {
		t.Fatalf("expected second node to succeed, got %#v", finalRoute)
	}
	entries := manager.GetPool("primary").Entries
	if entries[0].Alive || entries[0].FailCount == 0 {
		t.Fatalf("failed node should be marked unavailable: %#v", entries[0])
	}
	if !entries[1].Alive {
		t.Fatalf("successful node should remain alive")
	}
}

func TestDoProxyWithRetryDoesNotRetryResponseStatus(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer target.Close()

	var secondAttempts int
	direct := &testDialer{
		name: "direct",
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	unused := &testDialer{
		name: "unused",
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			secondAttempts++
			return nil, errors.New("should not be called")
		},
	}
	route := &egressRoute{
		Region:   "US",
		Group:    "US",
		Strategy: "least-latency",
		Choices: []*pool.EgressChoice{
			{Dialer: direct, PoolName: "pool", NodeName: "direct", Source: "static", Region: "US", Group: "US"},
			{Dialer: unused, PoolName: "pool", NodeName: "unused", Source: "static", Region: "US", Group: "US"},
		},
	}

	resp, finalRoute, err := (&Server{}).doProxyWithRetry(context.Background(), &ProxyRequest{
		URL:    target.URL,
		Method: http.MethodGet,
	}, route)
	if err != nil {
		t.Fatalf("proxy should return upstream response without retry error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusForbidden)
	}
	if finalRoute == nil || finalRoute.Choice == nil || finalRoute.Choice.NodeName != "direct" {
		t.Fatalf("expected first candidate, got %#v", finalRoute)
	}
	if secondAttempts != 0 {
		t.Fatalf("second candidate should not be called, got %d attempts", secondAttempts)
	}
}

func TestDoProxyWithRetryDoesNotRetryBlockedRedirectTarget(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/blocked", http.StatusFound)
	}))
	defer target.Close()

	var secondAttempts int
	direct := &testDialer{
		name: "direct",
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	unused := &testDialer{
		name: "unused",
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			secondAttempts++
			return nil, errors.New("should not be called")
		},
	}
	route := &egressRoute{
		Region:   "US",
		Group:    "US",
		Strategy: "least-latency",
		Choices: []*pool.EgressChoice{
			{Dialer: direct, PoolName: "pool", NodeName: "direct", Source: "static", Region: "US", Group: "US"},
			{Dialer: unused, PoolName: "pool", NodeName: "unused", Source: "static", Region: "US", Group: "US"},
		},
	}

	_, _, err := (&Server{proxySettings: DefaultProxySettings()}).doProxyWithRetry(context.Background(), &ProxyRequest{
		URL:             target.URL,
		Method:          http.MethodGet,
		FollowRedirects: true,
	}, route)
	if err == nil {
		t.Fatalf("expected blocked redirect target error")
	}
	if !strings.Contains(err.Error(), "target address is in a blocked range") {
		t.Fatalf("unexpected error: %v", err)
	}
	if secondAttempts != 0 {
		t.Fatalf("blocked redirect target should not retry another candidate, got %d attempts", secondAttempts)
	}
}

func TestRemoteChijieTemplateForwardsRequestWithBearerAndHop(t *testing.T) {
	var seen ProxyRequest
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy" {
			t.Fatalf("path: got %q want /proxy", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer remote-token" {
			t.Fatalf("authorization: got %q", got)
		}
		if got := r.Header.Get(chijieHopHeader); got != "2" {
			t.Fatalf("hop: got %q want 2", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode forwarded body: %v", err)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Add("Set-Cookie", "remote_session=abc; Path=/; HttpOnly")
		w.Header().Add("Set-Cookie", "remote_pref=dark; Path=/")
		_, _ = w.Write([]byte("remote-ok"))
	}))
	defer remote.Close()

	route := &egressRoute{
		Region:      "NG",
		Group:       "NG-RES",
		Residential: true,
		Choices: []*pool.EgressChoice{
			{PoolName: "chijie-b", NodeName: "chijie-b-ng", Source: "template", Template: true, TemplateType: "chijie", Region: "NG", Group: "NG-RES", Residential: true, Endpoint: remote.URL, BearerToken: "remote-token"},
		},
	}
	req := &ProxyRequest{
		URL:     "https://target.example/data",
		Method:  http.MethodPost,
		Headers: map[string]string{"X-Test": "yes"},
		Payload: "payload",
		Egress:  EgressOptions{Region: "NG", Strategy: "least-latency"},
		Hop:     1,
	}

	resp, finalRoute, err := (&Server{remoteChijieClient: remote.Client()}).doProxyWithRetry(context.Background(), req, route)
	if err != nil {
		t.Fatalf("remote chijie proxy: %v", err)
	}
	if string(resp.Body) != "remote-ok" || resp.StatusCode != http.StatusOK || resp.ContentType != "text/plain" {
		t.Fatalf("unexpected response: body=%q status=%d contentType=%q", resp.Body, resp.StatusCode, resp.ContentType)
	}
	wantCookies := []string{"remote_session=abc; Path=/; HttpOnly", "remote_pref=dark; Path=/"}
	if strings.Join(resp.SetCookies, "\n") != strings.Join(wantCookies, "\n") {
		t.Fatalf("set-cookie headers: got %v want %v", resp.SetCookies, wantCookies)
	}
	if finalRoute == nil || finalRoute.Choice == nil || finalRoute.Choice.PoolName != "chijie-b" {
		t.Fatalf("unexpected final route: %#v", finalRoute)
	}
	if seen.URL != req.URL || seen.Method != req.Method || seen.Payload != req.Payload || seen.Headers["X-Test"] != "yes" {
		t.Fatalf("request was not forwarded as proxy payload: %#v", seen)
	}
	if seen.Egress.Region != "NG" || seen.Egress.Strategy != "least-latency" || !seen.Egress.Residential {
		t.Fatalf("egress was not forwarded: %#v", seen.Egress)
	}
}

func TestRemoteChijieGatewayErrorFallsBackToNextTemplate(t *testing.T) {
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(chijieErrorHeader, "egress failed")
		writeProxyError(w, http.StatusBadGateway, "proxy request failed", "remote has no node")
	}))
	defer remote.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fallback-ok"))
	}))
	defer target.Close()

	fallback := &testDialer{
		name: "fallback",
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	route := &egressRoute{
		Region: "NG",
		Group:  "NG",
		Choices: []*pool.EgressChoice{
			{PoolName: "chijie-b", NodeName: "chijie-b-ng", Source: "template", Template: true, TemplateType: "chijie", Region: "NG", Group: "NG", Endpoint: remote.URL, BearerToken: "remote-token"},
			{Dialer: fallback, PoolName: "brightdata", NodeName: "brightdata-ng", Source: "template", Template: true, TemplateType: "proxy", Region: "NG", Group: "NG"},
		},
	}

	resp, finalRoute, err := (&Server{remoteChijieClient: remote.Client()}).doProxyWithRetry(context.Background(), &ProxyRequest{
		URL:    target.URL,
		Method: http.MethodGet,
	}, route)
	if err != nil {
		t.Fatalf("proxy with template fallback: %v", err)
	}
	if string(resp.Body) != "fallback-ok" || resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected fallback response: body=%q status=%d", resp.Body, resp.StatusCode)
	}
	if finalRoute == nil || finalRoute.Choice == nil || finalRoute.Choice.PoolName != "brightdata" {
		t.Fatalf("expected brightdata fallback, got %#v", finalRoute)
	}
}

func TestRemoteChijieSourceStatusDoesNotFallback(t *testing.T) {
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden by target", http.StatusForbidden)
	}))
	defer remote.Close()

	var fallbackAttempts int
	fallback := &testDialer{
		name: "unused",
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			fallbackAttempts++
			return nil, errors.New("should not be called")
		},
	}
	route := &egressRoute{
		Region: "NG",
		Group:  "NG",
		Choices: []*pool.EgressChoice{
			{PoolName: "chijie-b", NodeName: "chijie-b-ng", Source: "template", Template: true, TemplateType: "chijie", Region: "NG", Group: "NG", Endpoint: remote.URL, BearerToken: "remote-token"},
			{Dialer: fallback, PoolName: "brightdata", NodeName: "brightdata-ng", Source: "template", Template: true, TemplateType: "proxy", Region: "NG", Group: "NG"},
		},
	}

	resp, finalRoute, err := (&Server{remoteChijieClient: remote.Client()}).doProxyWithRetry(context.Background(), &ProxyRequest{
		URL:    "https://target.example/data",
		Method: http.MethodGet,
	}, route)
	if err != nil {
		t.Fatalf("remote target status should be returned without retry error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusForbidden)
	}
	if finalRoute == nil || finalRoute.Choice == nil || finalRoute.Choice.PoolName != "chijie-b" {
		t.Fatalf("expected remote chijie final route, got %#v", finalRoute)
	}
	if fallbackAttempts != 0 {
		t.Fatalf("fallback should not be called, got %d attempts", fallbackAttempts)
	}
}

func TestReadProxyResponseBodyRejectsContentLengthOverLimit(t *testing.T) {
	resp := &http.Response{
		Body:          io.NopCloser(strings.NewReader("abcd")),
		ContentLength: 4,
	}
	_, err := readProxyResponseBody(resp, 3)
	if !errors.Is(err, errProxyResponseTooLarge) {
		t.Fatalf("expected response size error, got %v", err)
	}
}

func TestReadProxyResponseBodyRejectsStreamOverLimit(t *testing.T) {
	resp := &http.Response{
		Body:          io.NopCloser(strings.NewReader("abcd")),
		ContentLength: -1,
	}
	_, err := readProxyResponseBody(resp, 3)
	if !errors.Is(err, errProxyResponseTooLarge) {
		t.Fatalf("expected response size error, got %v", err)
	}
}
