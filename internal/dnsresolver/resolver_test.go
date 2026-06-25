package dnsresolver

import (
	"testing"
	"time"
)

func TestNewDialerUsesPublicResolver(t *testing.T) {
	dialer := NewDialer(10 * time.Second)
	if dialer.Resolver != Resolver() {
		t.Fatalf("expected dialer to use public resolver")
	}
	if dialer.Timeout != 10*time.Second {
		t.Fatalf("expected custom timeout, got %s", dialer.Timeout)
	}
}

func TestNewDialerDefaultsTimeout(t *testing.T) {
	dialer := NewDialer(0)
	if dialer.Timeout != 30*time.Second {
		t.Fatalf("expected default timeout, got %s", dialer.Timeout)
	}
}
