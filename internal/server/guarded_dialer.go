package server

import (
	"context"
	"net"
	"net/http"

	"chijie/internal/dialer"
	"chijie/internal/netguard"
)

// guardedDialer 是 dialer.Dialer 的防 SSRF 包装。
// 仅作用于直连出口；通过代理出口时，目标解析在远端代理上完成。
type guardedDialer struct {
	dialer.Dialer
	allowPrivate bool
}

func (g *guardedDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if !g.allowPrivate {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		if err := netguard.CheckHost(ctx, host); err != nil {
			return nil, err
		}
	}
	return g.Dialer.DialContext(ctx, network, addr)
}

func (g *guardedDialer) GetHTTPTransport() *http.Transport {
	transport := g.Dialer.GetHTTPTransport()
	if g.allowPrivate || transport == nil {
		return transport
	}
	transport.DialContext = g.DialContext
	return transport
}

func (g *guardedDialer) Name() string {
	return g.Dialer.Name()
}

// 确保实现了 Dialer 接口
var _ dialer.Dialer = (*guardedDialer)(nil)
