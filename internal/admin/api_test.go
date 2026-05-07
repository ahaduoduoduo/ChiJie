package admin

import (
	"net/http/httptest"
	"testing"
	"time"
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
