package dialer

import (
	"context"
	"net"
	"time"

	"chijie/internal/dnsresolver"

	M "github.com/sagernet/sing/common/metadata"
)

type dnsNetworkDialer struct {
	dialer *net.Dialer
}

func newDNSNetworkDialer(timeout time.Duration) *dnsNetworkDialer {
	return &dnsNetworkDialer{dialer: dnsresolver.NewDialer(timeout)}
}

func (d *dnsNetworkDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	return d.dialer.DialContext(ctx, network, destination.String())
}

func (d *dnsNetworkDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	var listenConfig net.ListenConfig
	return listenConfig.ListenPacket(ctx, "udp", "")
}
