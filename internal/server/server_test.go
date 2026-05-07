package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"chijie/internal/pool"
)

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
