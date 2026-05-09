package server

import (
	"context"
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

	respBody, contentType, statusCode, finalRoute, err := (&Server{}).doProxyWithRetry(context.Background(), &ProxyRequest{
		URL:    target.URL,
		Method: http.MethodGet,
	}, route)
	if err != nil {
		t.Fatalf("proxy with retry: %v", err)
	}
	if string(respBody) != "ok" || statusCode != http.StatusOK || contentType != "text/plain" {
		t.Fatalf("unexpected response: body=%q status=%d contentType=%q", respBody, statusCode, contentType)
	}
	if finalRoute == nil || finalRoute.Choice == nil || finalRoute.Choice.NodeName != "second" {
		t.Fatalf("expected second candidate, got %#v", finalRoute)
	}
	if firstAttempts != 1 {
		t.Fatalf("first candidate attempts: got %d want 1", firstAttempts)
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

	_, _, statusCode, finalRoute, err := (&Server{}).doProxyWithRetry(context.Background(), &ProxyRequest{
		URL:    target.URL,
		Method: http.MethodGet,
	}, route)
	if err != nil {
		t.Fatalf("proxy should return upstream response without retry error: %v", err)
	}
	if statusCode != http.StatusForbidden {
		t.Fatalf("status: got %d want %d", statusCode, http.StatusForbidden)
	}
	if finalRoute == nil || finalRoute.Choice == nil || finalRoute.Choice.NodeName != "direct" {
		t.Fatalf("expected first candidate, got %#v", finalRoute)
	}
	if secondAttempts != 0 {
		t.Fatalf("second candidate should not be called, got %d attempts", secondAttempts)
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
