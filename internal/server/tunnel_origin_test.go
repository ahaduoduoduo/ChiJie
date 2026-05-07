package server

import (
	"net/http"
	"testing"
)

func TestCheckSameOriginEmptyAllowed(t *testing.T) {
	r, _ := http.NewRequest("GET", "http://example.com/tunnel", nil)
	r.Host = "example.com"
	if !checkSameOriginOrEmpty(r) {
		t.Errorf("empty Origin should be allowed (server-side clients)")
	}
}

func TestCheckSameOriginMatching(t *testing.T) {
	r, _ := http.NewRequest("GET", "http://example.com/tunnel", nil)
	r.Host = "example.com"
	r.Header.Set("Origin", "http://example.com")
	if !checkSameOriginOrEmpty(r) {
		t.Errorf("matching same-origin should be allowed")
	}
}

func TestCheckSameOriginCrossSiteRejected(t *testing.T) {
	r, _ := http.NewRequest("GET", "http://example.com/tunnel", nil)
	r.Host = "example.com"
	r.Header.Set("Origin", "http://evil.com")
	if checkSameOriginOrEmpty(r) {
		t.Errorf("cross-site Origin should be rejected")
	}
}

func TestCheckSameOriginInvalidRejected(t *testing.T) {
	r, _ := http.NewRequest("GET", "http://example.com/tunnel", nil)
	r.Host = "example.com"
	r.Header.Set("Origin", "::not a url::")
	if checkSameOriginOrEmpty(r) {
		t.Errorf("invalid Origin should be rejected")
	}
}
