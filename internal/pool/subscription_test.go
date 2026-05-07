package pool

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParsePlainURIListSupportsCommonProtocols(t *testing.T) {
	vmessConfig := `{"v":"2","ps":"vmess-ws","add":"vmess.example.com","port":"443","id":"11111111-1111-1111-1111-111111111111","aid":"0","scy":"auto","net":"ws","host":"cdn.example.com","path":"/ws","tls":"tls","sni":"vmess.example.com","fp":"chrome"}`
	vmessURI := "vmess://" + base64.StdEncoding.EncodeToString([]byte(vmessConfig))

	content := `ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@example.com:8388?plugin=v2ray-plugin%3Bmode%3Dwebsocket#ss-ws
` + vmessURI + `
trojan://secret@trojan.example.com:443?security=tls&sni=trojan.example.com&type=grpc&serviceName=tun#trojan-grpc
vless://11111111-1111-1111-1111-111111111111@vless.example.com:443?security=reality&sni=www.example.com&type=ws&host=cdn.example.com&path=%2Fedge&fp=chrome&pbk=public-key&sid=abcd#vless-reality
hysteria2://hy-pass@hy.example.com:443?sni=hy.example.com&insecure=1&obfs=salamander&obfs-password=obfs-pass&upmbps=20&downmbps=100#hy2`

	nodes, err := NewSubscriptionParser().parseURIList(content)
	if err != nil {
		t.Fatalf("parse uri list: %v", err)
	}
	if len(nodes) != 5 {
		t.Fatalf("expected 5 nodes, got %d", len(nodes))
	}

	assertNode := func(index int, nodeType, name string) {
		t.Helper()
		if nodes[index].Type != nodeType {
			t.Fatalf("node %d type: got %q want %q", index, nodes[index].Type, nodeType)
		}
		if nodes[index].Name != name {
			t.Fatalf("node %d name: got %q want %q", index, nodes[index].Name, name)
		}
	}

	assertNode(0, "ss", "ss-ws")
	if nodes[0].Extra["plugin"] != "v2ray-plugin" || nodes[0].Extra["plugin_opts"] != "mode=websocket" {
		t.Fatalf("ss plugin options not parsed: %#v", nodes[0].Extra)
	}

	assertNode(1, "vmess", "vmess-ws")
	if nodes[1].Extra["network"] != "ws" || nodes[1].Extra["tls"] != "tls" || nodes[1].Extra["fingerprint"] != "chrome" {
		t.Fatalf("vmess extras not parsed: %#v", nodes[1].Extra)
	}

	assertNode(2, "trojan", "trojan-grpc")
	if nodes[2].Extra["network"] != "grpc" || nodes[2].Extra["service_name"] != "tun" {
		t.Fatalf("trojan grpc extras not parsed: %#v", nodes[2].Extra)
	}

	assertNode(3, "vless", "vless-reality")
	if nodes[3].Extra["security"] != "reality" || nodes[3].Extra["public_key"] != "public-key" || nodes[3].Extra["host"] != "cdn.example.com" {
		t.Fatalf("vless reality extras not parsed: %#v", nodes[3].Extra)
	}

	assertNode(4, "hysteria2", "hy2")
	if nodes[4].Extra["obfs_password"] != "obfs-pass" || nodes[4].Extra["up_mbps"] != "20" || nodes[4].Extra["down_mbps"] != "100" {
		t.Fatalf("hysteria2 extras not parsed: %#v", nodes[4].Extra)
	}
}

func TestParseClashYAMLSupportsHysteria2AndReality(t *testing.T) {
	yamlContent := []byte(`
proxies:
  - name: ss-node
    type: ss
    server: ss.example.com
    port: 8388
    cipher: chacha20-ietf-poly1305
    password: pass
    plugin: v2ray-plugin
    plugin-opts: mode=websocket
  - name: vless-reality
    type: vless
    server: vless.example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    network: ws
    servername: www.example.com
    client-fingerprint: chrome
    reality-opts:
      public-key: public-key
      short-id: abcd
    ws-opts:
      path: /edge
      headers:
        Host: cdn.example.com
  - name: hy2-node
    type: hysteria2
    server: hy.example.com
    port: 443
    password: hy-pass
    sni: hy.example.com
    skip-cert-verify: true
    up: 20
    down: 100
    obfs: salamander
    obfs-password: obfs-pass
`)

	nodes, err := NewSubscriptionParser().parseClashYAML(yamlContent)
	if err != nil {
		t.Fatalf("parse clash yaml: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	if nodes[1].Extra["security"] != "reality" || nodes[1].Extra["public_key"] != "public-key" || nodes[1].Extra["host"] != "cdn.example.com" {
		t.Fatalf("vless reality clash extras not parsed: %#v", nodes[1].Extra)
	}
	if nodes[2].Type != "hysteria2" || nodes[2].Extra["skip_verify"] != "true" || nodes[2].Extra["obfs_password"] != "obfs-pass" {
		t.Fatalf("hysteria2 clash extras not parsed: %#v", nodes[2])
	}
}

func TestFetchSupportsMultipleSubscriptionURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fail":
			http.Error(w, "forbidden", http.StatusForbidden)
		case "/ok":
			_, _ = w.Write([]byte("ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@example.com:8388#multi-source"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	parser := NewSubscriptionParser()
	parser.client = subscriptionTestClient(t, server)
	nodes, err := parser.Fetch("http://example.com/fail|http://example.com/ok")
	if err != nil {
		t.Fatalf("fetch multiple subscriptions: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "multi-source" {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
}

func TestFetchReportsAllSubscriptionURLFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	parser := NewSubscriptionParser()
	parser.client = subscriptionTestClient(t, server)
	_, err := parser.Fetch("http://example.com/fail?token=secret\nhttp://example.com/fail?token=secret")
	if err == nil {
		t.Fatalf("expected all subscription urls to fail")
	}
	if strings.Contains(err.Error(), "?") {
		t.Fatalf("subscription error should not expose query strings: %v", err)
	}
}

func TestFetchNetworkErrorKeepsCauseAndRedactsURL(t *testing.T) {
	parser := NewSubscriptionParser()
	parser.client = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("dial tcp: test network failure")
		}),
	}

	_, err := parser.Fetch("https://example.com/sub?token=secret")
	if err == nil {
		t.Fatalf("expected network error")
	}
	if !strings.Contains(err.Error(), "test network failure") {
		t.Fatalf("subscription error should keep network cause: %v", err)
	}
	if strings.Contains(err.Error(), "token=secret") || strings.Contains(err.Error(), "?") {
		t.Fatalf("subscription error should not expose query strings: %v", err)
	}
}

func TestFetchSendsSubscriptionHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != subscriptionUserAgent {
			t.Fatalf("unexpected user agent: %q", got)
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "application/yaml") {
			t.Fatalf("unexpected accept header: %q", got)
		}
		_, _ = w.Write([]byte("ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@example.com:8388#headers"))
	}))
	defer server.Close()

	parser := NewSubscriptionParser()
	parser.client = subscriptionTestClient(t, server)
	nodes, err := parser.Fetch("http://example.com/sub")
	if err != nil {
		t.Fatalf("fetch subscription: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "headers" {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
}

func TestNewSubscriptionParserUsesConservativeHTTPTransport(t *testing.T) {
	parser := NewSubscriptionParser()
	if parser.client.Timeout != 45*time.Second {
		t.Fatalf("unexpected client timeout: %s", parser.client.Timeout)
	}
	transport, ok := parser.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected http transport, got %T", parser.client.Transport)
	}
	if transport.ForceAttemptHTTP2 {
		t.Fatalf("subscription transport should not force HTTP/2")
	}
	if transport.TLSNextProto == nil {
		t.Fatalf("subscription transport should disable automatic HTTP/2")
	}
	if transport.ResponseHeaderTimeout != 30*time.Second {
		t.Fatalf("unexpected response header timeout: %s", transport.ResponseHeaderTimeout)
	}
}

func TestValidateSubscriptionURLRejectsPrivateHosts(t *testing.T) {
	if err := validateSubscriptionURL(context.Background(), "http://127.0.0.1/sub"); err == nil {
		t.Fatalf("expected private subscription host to be rejected")
	}
}

func TestReadSubscriptionBodyRejectsOverLimit(t *testing.T) {
	resp := &http.Response{
		Body:          http.NoBody,
		ContentLength: MaxSubscriptionBodyBytes + 1,
	}
	if _, err := readSubscriptionBody(resp, MaxSubscriptionBodyBytes); err == nil {
		t.Fatalf("expected subscription body size error")
	}
}

func subscriptionTestClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	serverAddr := strings.TrimPrefix(server.URL, "http://")
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.HasPrefix(addr, "example.com:") {
				addr = serverAddr
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	return &http.Client{Transport: transport}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
