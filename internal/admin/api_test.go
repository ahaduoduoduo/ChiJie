package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"chijie/internal/fingerprint"
	"chijie/internal/pool"
)

func TestParseTokenDurationSupportsDays(t *testing.T) {
	got, err := parseTokenDuration("30d", 24*time.Hour)
	if err != nil {
		t.Fatalf("parse duration: %v", err)
	}
	if got != 30*24*time.Hour {
		t.Fatalf("duration = %s, want 720h", got)
	}
}

func TestParseTokenDurationRejectsOverLimit(t *testing.T) {
	if _, err := parseTokenDuration("366d", 24*time.Hour); err == nil {
		t.Fatalf("expected over-limit duration error")
	}
}

func TestClientIPFromRequestPrefersCloudflareHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/auth/login", nil)
	req.RemoteAddr = "10.0.0.10:3456"
	req.Header.Set("CF-Connecting-IP", "203.0.113.9")
	req.Header.Set("X-Forwarded-For", "198.51.100.7")

	if got := clientIPFromRequest(req); got != "203.0.113.9" {
		t.Fatalf("clientIPFromRequest = %q, want Cloudflare source IP", got)
	}
}

func TestClientIPFromRequestSkipsInvalidForwardedValues(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/auth/login", nil)
	req.RemoteAddr = "10.0.0.10:3456"
	req.Header.Set("CF-Connecting-IP", "not-an-ip")
	req.Header.Set("X-Forwarded-For", "bad, 198.51.100.7")

	if got := clientIPFromRequest(req); got != "198.51.100.7" {
		t.Fatalf("clientIPFromRequest = %q, want first valid forwarded IP", got)
	}
}

func TestLoadFingerprintsConfigAllowsMissingFile(t *testing.T) {
	cfg, err := loadFingerprintsConfig(filepath.Join(t.TempDir(), "fingerprints.yaml"))
	if err != nil {
		t.Fatalf("load missing fingerprints config: %v", err)
	}
	if cfg.Fingerprints == nil {
		t.Fatalf("fingerprints map should be initialized")
	}
	if len(cfg.Fingerprints) != 0 {
		t.Fatalf("expected empty fingerprints map, got %d entries", len(cfg.Fingerprints))
	}
}

func TestValidatePoolConfigAcceptsHTTPSChijieTemplate(t *testing.T) {
	err := validatePoolConfig(&pool.PoolConfig{
		Source:       "template",
		TemplateType: "chijie",
		Endpoint:     "https://b.example.com",
		BearerToken:  "token",
		Coverage:     "both",
		Priority:     100,
	})
	if err != nil {
		t.Fatalf("validate chijie template: %v", err)
	}
}

func TestValidatePoolConfigRejectsHTTPChijieTemplate(t *testing.T) {
	err := validatePoolConfig(&pool.PoolConfig{
		Source:       "template",
		TemplateType: "chijie",
		Endpoint:     "http://b.example.com",
		BearerToken:  "token",
	})
	if err == nil {
		t.Fatalf("expected http chijie template to be rejected")
	}
}

func TestValidatePoolConfigAcceptsManualAndDailySubscriptionRefresh(t *testing.T) {
	for _, interval := range []string{"", "3d"} {
		err := validatePoolConfig(&pool.PoolConfig{
			Source:         "subscription",
			URL:            "https://example.com/sub",
			UpdateInterval: interval,
		})
		if err != nil {
			t.Fatalf("validate subscription interval %q: %v", interval, err)
		}
	}
}

func TestHealthCheckSettingsPersistsAndUpdatesChecker(t *testing.T) {
	dir := t.TempDir()
	gatewayPath := filepath.Join(dir, "gateway.yaml")
	if err := os.WriteFile(gatewayPath, []byte(`
server:
  listen: ":8080"
admin:
  jwt_secret: "1234567890123456"
log:
  level: "info"
  file: ""
`), 0600); err != nil {
		t.Fatalf("write gateway config: %v", err)
	}

	manager := pool.NewManager()
	checker := pool.NewHealthChecker(manager, 0, 0, "")
	server := NewServer("127.0.0.1:0", manager, fingerprint.NewManager(), dir, "", "1234567890123456", "24h", nil)
	server.SetHealthChecker(checker)

	reqBody := []byte(`{"interval":"2m","timeout":"9s","url":"https://example.com/health","max_fail":7}`)
	req := httptest.NewRequest(http.MethodPut, "/api/system/health-check", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT health-check status = %d, body: %s", rec.Code, rec.Body.String())
	}

	defaults := checker.Defaults()
	if defaults.Interval != 2*time.Minute || defaults.Timeout != 9*time.Second || defaults.MaxFail != 7 {
		t.Fatalf("checker defaults not updated: %#v", defaults)
	}
	if defaults.TestURL != "https://example.com/health" {
		t.Fatalf("checker test url = %q", defaults.TestURL)
	}

	var stored struct {
		HealthCheck pool.HealthCheckConfig `yaml:"health_check"`
	}
	if err := loadYAML(gatewayPath, &stored); err != nil {
		t.Fatalf("load persisted gateway config: %v", err)
	}
	if stored.HealthCheck.Interval != "2m0s" || stored.HealthCheck.Timeout != "9s" || stored.HealthCheck.URL != "https://example.com/health" || stored.HealthCheck.MaxFail != 7 {
		t.Fatalf("unexpected persisted health_check: %#v", stored.HealthCheck)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/system/health-check", nil)
	getRec := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET health-check status = %d, body: %s", getRec.Code, getRec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode health-check response: %v", err)
	}
	if got["interval"] != "2m0s" || got["timeout"] != "9s" || got["url"] != "https://example.com/health" || got["max_fail"] != float64(7) {
		t.Fatalf("unexpected health-check response: %#v", got)
	}
}
