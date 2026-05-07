package dialer

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/proxy"
)

// SOCKS5Dialer SOCKS5 代理
type SOCKS5Dialer struct {
	node   *Node
	dialer proxy.Dialer
}

func NewSOCKS5Dialer(node *Node) (*SOCKS5Dialer, error) {
	addr := fmt.Sprintf("%s:%d", node.Server, node.Port)

	var auth *proxy.Auth
	if node.Username != "" {
		auth = &proxy.Auth{
			User:     node.Username,
			Password: node.Password,
		}
	}

	dialer, err := proxy.SOCKS5("tcp", addr, auth, &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("create socks5 dialer: %w", err)
	}

	return &SOCKS5Dialer{
		node:   node,
		dialer: dialer,
	}, nil
}

func (d *SOCKS5Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.dialer.Dial(network, addr)
}

func (d *SOCKS5Dialer) GetHTTPTransport() *http.Transport {
	return &http.Transport{
		DialContext:           d.DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func (d *SOCKS5Dialer) Name() string {
	return fmt.Sprintf("socks5://%s", d.node.Name)
}
