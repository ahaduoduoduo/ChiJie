package dialer

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"chijie/internal/util"

	"github.com/justinwoo280/sing-xhttp/xhttp"
	sbtls "github.com/sagernet/sing-box/common/tls"
	sblog "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-vmess/vless"
	"github.com/sagernet/sing/common/json/badoption"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type VLESSXHTTPDialer struct {
	node      *Node
	transport *xhttp.Client
	client    *vless.Client
}

func NewVLESSXHTTPDialer(node *Node) (*VLESSXHTTPDialer, error) {
	if node.Extra == nil {
		node.Extra = map[string]string{}
	}

	ctx := newSingBoxContext(context.Background())
	logger := sblog.NewNOPFactory().Logger()
	options, err := buildXHTTPOptions(node)
	if err != nil {
		return nil, err
	}

	var tlsConfig sbtls.Config
	if tlsOptions := buildTLSOptions(node, false); tlsOptions != nil {
		var outboundTLSOptions option.OutboundTLSOptions
		if err := decodeSingBoxOptions(tlsOptions, &outboundTLSOptions); err != nil {
			return nil, err
		}
		tlsConfig, err = sbtls.NewClientWithOptions(sbtls.ClientOptions{
			Context:       ctx,
			Logger:        logger,
			ServerAddress: node.Server,
			Options:       outboundTLSOptions,
		})
		if err != nil {
			return nil, err
		}
	}

	transport, err := xhttp.NewClient(ctx, newDNSNetworkDialer(30*time.Second), M.ParseSocksaddrHostPort(node.Server, uint16(node.Port)), options, tlsConfig)
	if err != nil {
		return nil, err
	}
	client, err := vless.NewClient(util.FirstNonEmpty(node.Extra["uuid"], node.Username), node.Extra["flow"], logger)
	if err != nil {
		return nil, err
	}

	return &VLESSXHTTPDialer{
		node:      node,
		transport: transport,
		client:    client,
	}, nil
}

func (d *VLESSXHTTPDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if N.NetworkName(network) != N.NetworkTCP {
		return nil, fmt.Errorf("vless xhttp only supports tcp dialing")
	}
	conn, err := d.transport.DialContext(ctx)
	if err != nil {
		return nil, err
	}
	return d.client.DialEarlyConn(conn, M.ParseSocksaddr(addr))
}

func (d *VLESSXHTTPDialer) GetHTTPTransport() *http.Transport {
	return &http.Transport{
		DialContext:           d.DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func (d *VLESSXHTTPDialer) Name() string {
	return fmt.Sprintf("vless+xhttp://%s", d.node.Name)
}

func isXHTTPTransport(node *Node) bool {
	if node == nil || node.Extra == nil {
		return false
	}
	switch strings.ToLower(util.FirstNonEmpty(node.Extra["network"], node.Extra["transport"], node.Extra["transport_type"])) {
	case "xhttp", "splithttp", "split-http":
		return true
	default:
		return false
	}
}

func buildXHTTPOptions(node *Node) (xhttp.Options, error) {
	if requestsXHTTP3(node) {
		return xhttp.Options{}, fmt.Errorf("unsupported xhttp HTTP/3")
	}

	mode := resolveXHTTPMode(node)
	switch mode {
	case xhttp.ModePacketUp, xhttp.ModeStreamUp, xhttp.ModeStreamOne:
	default:
		return xhttp.Options{}, fmt.Errorf("unsupported xhttp mode: %s", mode)
	}

	options := xhttp.Options{
		Mode:         mode,
		Path:         util.FirstNonEmpty(node.Extra["xhttp_path"], node.Extra["path"], "/"),
		Host:         util.FirstNonEmpty(node.Extra["xhttp_host"], node.Extra["host"]),
		Method:       util.FirstNonEmpty(node.Extra["xhttp_method"], node.Extra["method"]),
		NoGRPCHeader: truthy(util.FirstNonEmpty(node.Extra["xhttp_no_grpc_header"], node.Extra["no_grpc_header"])),
		NoSSEHeader:  truthy(util.FirstNonEmpty(node.Extra["xhttp_no_sse_header"], node.Extra["no_sse_header"])),
		Headers:      toXHTTPHeaders(parseHeaderOptions(util.FirstNonEmpty(node.Extra["xhttp_headers"], node.Extra["headers"]))),
	}
	return options, nil
}

func resolveXHTTPMode(node *Node) string {
	mode := strings.ToLower(strings.TrimSpace(util.FirstNonEmpty(node.Extra["xhttp_mode"], node.Extra["mode"])))
	if mode != "" && mode != xhttp.ModeAuto {
		return mode
	}
	if usesRealityTLS(node) {
		return xhttp.ModeStreamOne
	}
	return xhttp.ModePacketUp
}

func requestsXHTTP3(node *Node) bool {
	if node == nil || node.Extra == nil {
		return false
	}
	alpns := util.SplitList(util.FirstNonEmpty(node.Extra["alpn"], node.Extra["alpns"]))
	return len(alpns) > 0 && strings.EqualFold(alpns[0], "h3")
}

func toXHTTPHeaders(headers map[string]string) badoption.HTTPHeader {
	if len(headers) == 0 {
		return nil
	}
	result := make(badoption.HTTPHeader, len(headers))
	for name, value := range headers {
		result[name] = badoption.Listable[string]{value}
	}
	return result
}
