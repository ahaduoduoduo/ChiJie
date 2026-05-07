package netguard

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"127.255.255.254", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.254", true},
		{"172.32.0.1", false},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"100.64.0.1", true},
		{"100.127.255.254", true},
		{"100.128.0.1", false},
		{"0.0.0.0", true},
		{"0.1.2.3", true},
		{"240.0.0.1", true},
		{"255.255.255.255", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"::1", true},
		{"fe80::1", true},
		{"fc00::1", true},
		{"fd00::1", true},
		{"2001:4860:4860::8888", false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if got := IsBlockedIP(ip); got != c.blocked {
			t.Errorf("IsBlockedIP(%s) = %v, want %v", c.ip, got, c.blocked)
		}
	}
}

func TestCheckHostLiteralIP(t *testing.T) {
	if err := CheckHost(context.Background(), "8.8.8.8"); err != nil {
		t.Errorf("public IP should pass: %v", err)
	}
	if err := CheckHost(context.Background(), "127.0.0.1"); !errors.Is(err, ErrBlockedAddress) {
		t.Errorf("loopback should be blocked, got %v", err)
	}
}

func TestGuardAllowPrivate(t *testing.T) {
	called := 0
	base := func(ctx context.Context, network, addr string) (net.Conn, error) {
		called++
		return nil, errors.New("ok")
	}
	guarded := Guard(base, true)
	_, _ = guarded(context.Background(), "tcp", "127.0.0.1:9090")
	if called != 1 {
		t.Errorf("allowPrivate=true should bypass guard")
	}
}

func TestGuardBlocksPrivate(t *testing.T) {
	base := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, errors.New("dial happened")
	}
	guarded := Guard(base, false)
	_, err := guarded(context.Background(), "tcp", "127.0.0.1:80")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Errorf("expected ErrBlockedAddress, got %v", err)
	}
}
