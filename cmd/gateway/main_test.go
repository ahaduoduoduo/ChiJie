package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"chijie/internal/fingerprint"
	"chijie/internal/pool"
	"chijie/internal/server"

	"github.com/golang-jwt/jwt/v5"
)

func generateTestBearerToken(t *testing.T, secret string) string {
	t.Helper()
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"proxy": true,
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
	})
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tokenString
}

func setupTestServer(t *testing.T, allowPrivateTargets ...bool) (*server.Config, string) {
	t.Helper()

	// 加载节点池
	poolMgr := pool.NewManager()
	nodesPath := t.TempDir() + "/nodes.yaml"
	nodesConfig := []byte("node_pools:\n  direct:\n    source: direct\n")
	if err := os.WriteFile(nodesPath, nodesConfig, 0600); err != nil {
		t.Fatalf("write nodes config: %v", err)
	}
	if err := poolMgr.LoadFromFile(nodesPath); err != nil {
		t.Fatalf("load nodes: %v", err)
	}

	cfg := &server.Config{
		Listen:              freeTestListenAddr(t),
		JWTSecret:           "jwt-secret",
		AllowPrivateTargets: len(allowPrivateTargets) > 0 && allowPrivateTargets[0],
	}

	srv := server.NewServer(cfg, poolMgr, fingerprint.NewManager())

	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			t.Logf("server error: %v", err)
		}
	}()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	return cfg, "http://" + cfg.Listen
}

func freeTestListenAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate test port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close test listener: %v", err)
	}
	return addr
}

func TestHealthEndpoint(t *testing.T) {
	_, baseURL := setupTestServer(t)

	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "ok") {
		t.Fatalf("unexpected body: %s", body)
	}
	t.Logf("health check passed: %s", body)
}

func TestAuthRequired(t *testing.T) {
	_, baseURL := setupTestServer(t)

	// 无 token
	resp, err := http.Post(baseURL+"/proxy", "application/json", strings.NewReader(`{"url":"https://httpbin.org/get"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 403 {
		t.Fatalf("expected 403 without token, got %d", resp.StatusCode)
	}
	t.Logf("auth check passed: no token → 403")

	// 错误 token
	req, _ := http.NewRequest("POST", baseURL+"/proxy", strings.NewReader(`{"url":"https://httpbin.org/get"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != 403 {
		t.Fatalf("expected 403 with wrong token, got %d", resp2.StatusCode)
	}
	t.Logf("auth check passed: wrong token → 403")
}

func TestProxyDirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"user_agent":"`+r.UserAgent()+`"}`)
	}))
	defer target.Close()

	cfg, baseURL := setupTestServer(t, true)

	token := generateTestBearerToken(t, cfg.JWTSecret)

	// 通过直连代理请求本地目标，避免测试依赖外部服务可用性。
	body := `{"url":"` + target.URL + `","method":"GET","headers":{"User-Agent":"Chijie-Test"},"egress":{}}`
	req, _ := http.NewRequest("POST", baseURL+"/proxy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d, body: %s", resp.StatusCode, respBody)
	}

	if !strings.Contains(string(respBody), "Chijie-Test") {
		t.Fatalf("response doesn't contain expected user-agent: %s", respBody)
	}

	t.Logf("direct proxy passed: %d bytes", len(respBody))
}
