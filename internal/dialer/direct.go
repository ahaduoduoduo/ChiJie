package dialer

import (
	"context"
	"net"
	"net/http"
	"time"

	"chijie/internal/dnsresolver"
)

// DirectDialer 直连
type DirectDialer struct {
	dialer *net.Dialer
}

func NewDirectDialer() *DirectDialer {
	return &DirectDialer{
		dialer: dnsresolver.NewDialer(30 * time.Second),
	}
}

func (d *DirectDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.dialer.DialContext(ctx, network, addr)
}

func (d *DirectDialer) GetHTTPTransport() *http.Transport {
	return &http.Transport{
		DialContext:           d.DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func (d *DirectDialer) Name() string {
	return "direct"
}
