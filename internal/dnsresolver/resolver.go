package dnsresolver

import (
	"context"
	"errors"
	"net"
	"time"
)

var defaultServers = []string{
	"1.1.1.1:53",
	"8.8.8.8:53",
}

var publicResolver = &net.Resolver{
	PreferGo: true,
	Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		var lastErr error
		for _, server := range defaultServers {
			conn, err := dialer.DialContext(ctx, network, server)
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, errors.New("no dns servers configured")
	},
}

func Resolver() *net.Resolver {
	return publicResolver
}

func NewDialer(timeout time.Duration) *net.Dialer {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		Resolver:  publicResolver,
	}
}
