package dialer

import (
	"context"
	stdjson "encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"chijie/internal/util"

	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	sbconstant "github.com/sagernet/sing-box/constant"
	sblog "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	sbhysteria2 "github.com/sagernet/sing-box/protocol/hysteria2"
	sbshadowsocks "github.com/sagernet/sing-box/protocol/shadowsocks"
	sbtrojan "github.com/sagernet/sing-box/protocol/trojan"
	sbvless "github.com/sagernet/sing-box/protocol/vless"
	sbvmess "github.com/sagernet/sing-box/protocol/vmess"
	singjson "github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
)

// SingBoxDialer adapts sing-box outbound implementations to the gateway Dialer.
type SingBoxDialer struct {
	node     *Node
	outbound adapter.Outbound
}

func NewSingBoxDialer(node *Node) (*SingBoxDialer, error) {
	if node.Extra == nil {
		node.Extra = map[string]string{}
	}

	ctx := newSingBoxContext(context.Background())
	logger := sblog.NewNOPFactory().Logger()
	tag := node.Name
	if tag == "" {
		tag = fmt.Sprintf("%s-%s:%d", node.Type, node.Server, node.Port)
	}

	outbound, err := buildSingBoxOutbound(ctx, logger, tag, node)
	if err != nil {
		return nil, err
	}

	return &SingBoxDialer{
		node:     node,
		outbound: outbound,
	}, nil
}

func newSingBoxContext(ctx context.Context) context.Context {
	ctx = service.ContextWith[adapter.DNSTransportManager](ctx, noopDNSTransportManager{})
	ctx = service.ContextWith[adapter.DNSRouter](ctx, systemDNSRouter{})
	return ctx
}

type noopDNSTransportManager struct{}

func (noopDNSTransportManager) Start(stage adapter.StartStage) error { return nil }
func (noopDNSTransportManager) Close() error                         { return nil }
func (noopDNSTransportManager) Transports() []adapter.DNSTransport   { return nil }
func (noopDNSTransportManager) Transport(tag string) (adapter.DNSTransport, bool) {
	return nil, false
}
func (noopDNSTransportManager) Default() adapter.DNSTransport { return nil }
func (noopDNSTransportManager) FakeIP() adapter.FakeIPTransport {
	return nil
}
func (noopDNSTransportManager) Remove(tag string) error { return nil }
func (noopDNSTransportManager) Create(ctx context.Context, logger sblog.ContextLogger, tag string, outboundType string, options any) error {
	return nil
}

type systemDNSRouter struct{}

func (systemDNSRouter) Start(stage adapter.StartStage) error { return nil }
func (systemDNSRouter) Close() error                         { return nil }
func (systemDNSRouter) Exchange(ctx context.Context, message *dns.Msg, options adapter.DNSQueryOptions) (*dns.Msg, error) {
	return nil, fmt.Errorf("dns exchange is not supported")
}
func (systemDNSRouter) Lookup(ctx context.Context, domain string, options adapter.DNSQueryOptions) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", domain)
}
func (systemDNSRouter) ClearCache() {}
func (systemDNSRouter) LookupReverseMapping(ip netip.Addr) (string, bool) {
	return "", false
}
func (systemDNSRouter) ResetNetwork() {}

func (d *SingBoxDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.outbound.DialContext(ctx, network, M.ParseSocksaddr(addr))
}

func (d *SingBoxDialer) GetHTTPTransport() *http.Transport {
	return &http.Transport{
		DialContext:           d.DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func (d *SingBoxDialer) Name() string {
	return fmt.Sprintf("%s://%s", normalizeNodeType(d.node.Type), d.node.Name)
}

func buildSingBoxOutbound(ctx context.Context, logger sblog.ContextLogger, tag string, node *Node) (adapter.Outbound, error) {
	switch normalizeNodeType(node.Type) {
	case "shadowsocks":
		var options option.ShadowsocksOutboundOptions
		if err := decodeSingBoxOptions(buildShadowsocksOptions(node), &options); err != nil {
			return nil, err
		}
		return sbshadowsocks.NewOutbound(ctx, nil, logger, tag, options)
	case "vmess":
		var options option.VMessOutboundOptions
		if err := decodeSingBoxOptions(buildVMessOptions(node), &options); err != nil {
			return nil, err
		}
		return sbvmess.NewOutbound(ctx, nil, logger, tag, options)
	case "vless":
		var options option.VLESSOutboundOptions
		if err := decodeSingBoxOptions(buildVLESSOptions(node), &options); err != nil {
			return nil, err
		}
		return sbvless.NewOutbound(ctx, nil, logger, tag, options)
	case "trojan":
		var options option.TrojanOutboundOptions
		if err := decodeSingBoxOptions(buildTrojanOptions(node), &options); err != nil {
			return nil, err
		}
		return sbtrojan.NewOutbound(ctx, nil, logger, tag, options)
	case "hysteria2":
		var options option.Hysteria2OutboundOptions
		if err := decodeSingBoxOptions(buildHysteria2Options(node), &options); err != nil {
			return nil, err
		}
		return sbhysteria2.NewOutbound(ctx, nil, logger, tag, options)
	default:
		return nil, fmt.Errorf("unsupported sing-box node type: %s", node.Type)
	}
}

func decodeSingBoxOptions(raw map[string]any, target any) error {
	data, err := stdjson.Marshal(raw)
	if err != nil {
		return err
	}
	return singjson.Unmarshal(data, target)
}

func buildBaseOptions(node *Node) map[string]any {
	return map[string]any{
		"server":      node.Server,
		"server_port": node.Port,
	}
}

func buildShadowsocksOptions(node *Node) map[string]any {
	options := buildBaseOptions(node)
	options["method"] = util.FirstNonEmpty(node.Extra["cipher"], node.Extra["method"], "aes-256-gcm")
	options["password"] = node.Password
	if node.Extra["plugin"] != "" {
		options["plugin"] = node.Extra["plugin"]
	}
	if pluginOptions := util.FirstNonEmpty(node.Extra["plugin_opts"], node.Extra["plugin_options"]); pluginOptions != "" {
		options["plugin_opts"] = pluginOptions
	}
	return options
}

func buildVMessOptions(node *Node) map[string]any {
	options := buildBaseOptions(node)
	options["uuid"] = util.FirstNonEmpty(node.Extra["uuid"], node.Username)
	options["security"] = util.FirstNonEmpty(node.Extra["security"], node.Extra["cipher"], "auto")
	if alterID := util.ParseInt(util.FirstNonEmpty(node.Extra["alter_id"], node.Extra["alterId"], node.Extra["aid"])); alterID > 0 {
		options["alter_id"] = alterID
	}
	if packetEncoding := node.Extra["packet_encoding"]; packetEncoding != "" {
		options["packet_encoding"] = packetEncoding
	}
	attachTLSAndTransport(options, node, false)
	return options
}

func buildVLESSOptions(node *Node) map[string]any {
	options := buildBaseOptions(node)
	options["uuid"] = util.FirstNonEmpty(node.Extra["uuid"], node.Username)
	if flow := node.Extra["flow"]; flow != "" {
		options["flow"] = flow
	}
	if packetEncoding := node.Extra["packet_encoding"]; packetEncoding != "" {
		options["packet_encoding"] = packetEncoding
	}
	attachTLSAndTransport(options, node, false)
	return options
}

func buildTrojanOptions(node *Node) map[string]any {
	options := buildBaseOptions(node)
	options["password"] = node.Password
	attachTLSAndTransport(options, node, true)
	return options
}

func buildHysteria2Options(node *Node) map[string]any {
	options := buildBaseOptions(node)
	options["password"] = util.FirstNonEmpty(node.Password, node.Extra["password"], node.Extra["auth"], node.Extra["auth_str"])
	if upMbps := util.ParseInt(util.FirstNonEmpty(node.Extra["up_mbps"], node.Extra["up"], node.Extra["upload"])); upMbps > 0 {
		options["up_mbps"] = upMbps
	}
	if downMbps := util.ParseInt(util.FirstNonEmpty(node.Extra["down_mbps"], node.Extra["down"], node.Extra["download"])); downMbps > 0 {
		options["down_mbps"] = downMbps
	}
	if ports := util.FirstNonEmpty(node.Extra["server_ports"], node.Extra["ports"]); ports != "" {
		options["server_ports"] = util.SplitList(ports)
	}
	if hopInterval := util.FirstNonEmpty(node.Extra["hop_interval"], node.Extra["hop-interval"]); hopInterval != "" {
		options["hop_interval"] = hopInterval
	}
	if obfsPassword := util.FirstNonEmpty(node.Extra["obfs_password"], node.Extra["obfs-password"]); obfsPassword != "" {
		options["obfs"] = map[string]any{
			"type":     util.FirstNonEmpty(node.Extra["obfs"], "salamander"),
			"password": obfsPassword,
		}
	}
	options["tls"] = buildTLSOptions(node, true)
	return options
}

func attachTLSAndTransport(options map[string]any, node *Node, defaultTLS bool) {
	if tlsOptions := buildTLSOptions(node, defaultTLS); tlsOptions != nil {
		options["tls"] = tlsOptions
	}
	if transport := buildV2RayTransport(node); transport != nil {
		options["transport"] = transport
	}
}

func buildTLSOptions(node *Node, defaultEnabled bool) map[string]any {
	security := strings.ToLower(util.FirstNonEmpty(node.Extra["security"], node.Extra["tls"]))
	enabled := defaultEnabled || truthy(node.Extra["tls"]) || security == "tls" || security == "reality"
	if explicitlyFalse(node.Extra["tls"]) || security == "none" {
		enabled = false
	}
	if !enabled {
		return nil
	}

	tlsOptions := map[string]any{
		"enabled": true,
	}
	serverName := util.FirstNonEmpty(
		node.Extra["server_name"],
		node.Extra["servername"],
		node.Extra["tls_server_name"],
		node.Extra["sni"],
		node.Extra["peer"],
	)
	if serverName != "" {
		tlsOptions["server_name"] = serverName
	}
	if truthy(util.FirstNonEmpty(node.Extra["insecure"], node.Extra["allowInsecure"], node.Extra["skip_verify"], node.Extra["skip-cert-verify"])) {
		tlsOptions["insecure"] = true
	}
	if alpn := util.FirstNonEmpty(node.Extra["alpn"], node.Extra["alpns"]); alpn != "" {
		tlsOptions["alpn"] = util.SplitList(alpn)
	}
	realityEnabled := security == "reality" || node.Extra["reality"] == "true" || node.Extra["public_key"] != "" || node.Extra["pbk"] != ""
	fingerprint := util.FirstNonEmpty(node.Extra["fingerprint"], node.Extra["client_fingerprint"], node.Extra["client-fingerprint"], node.Extra["fp"])
	if realityEnabled && fingerprint == "" {
		fingerprint = "chrome"
	}
	if fingerprint != "" {
		tlsOptions["utls"] = map[string]any{
			"enabled":     true,
			"fingerprint": fingerprint,
		}
	}
	if realityEnabled {
		tlsOptions["reality"] = map[string]any{
			"enabled":    true,
			"public_key": util.FirstNonEmpty(node.Extra["public_key"], node.Extra["public-key"], node.Extra["pbk"]),
			"short_id":   util.FirstNonEmpty(node.Extra["short_id"], node.Extra["short-id"], node.Extra["sid"]),
		}
	}
	return tlsOptions
}

func buildV2RayTransport(node *Node) map[string]any {
	network := strings.ToLower(util.FirstNonEmpty(node.Extra["network"], node.Extra["transport"], node.Extra["transport_type"]))
	switch network {
	case "", "tcp", "raw":
		return nil
	case "ws", "websocket":
		options := map[string]any{
			"type": sbconstant.V2RayTransportTypeWebsocket,
		}
		if path := node.Extra["path"]; path != "" {
			options["path"] = path
		}
		headers := parseHeaderOptions(util.FirstNonEmpty(node.Extra["headers"], node.Extra["ws_headers"]))
		if host := node.Extra["host"]; host != "" {
			headers["Host"] = host
		}
		if len(headers) > 0 {
			options["headers"] = headers
		}
		return options
	case "grpc":
		options := map[string]any{
			"type": sbconstant.V2RayTransportTypeGRPC,
		}
		if serviceName := util.FirstNonEmpty(node.Extra["service_name"], node.Extra["serviceName"], node.Extra["grpc_service_name"]); serviceName != "" {
			options["service_name"] = serviceName
		}
		return options
	case "h2", "http":
		options := map[string]any{
			"type": sbconstant.V2RayTransportTypeHTTP,
		}
		if path := node.Extra["path"]; path != "" {
			options["path"] = path
		}
		if host := node.Extra["host"]; host != "" {
			options["host"] = util.SplitList(host)
		}
		if headers := parseHeaderOptions(node.Extra["headers"]); len(headers) > 0 {
			options["headers"] = headers
		}
		return options
	case "httpupgrade", "http_upgrade":
		options := map[string]any{
			"type": sbconstant.V2RayTransportTypeHTTPUpgrade,
		}
		if path := node.Extra["path"]; path != "" {
			options["path"] = path
		}
		if host := node.Extra["host"]; host != "" {
			options["host"] = host
		}
		if headers := parseHeaderOptions(node.Extra["headers"]); len(headers) > 0 {
			options["headers"] = headers
		}
		return options
	case "quic":
		return map[string]any{
			"type": sbconstant.V2RayTransportTypeQUIC,
		}
	default:
		return map[string]any{
			"type": network,
		}
	}
}

func parseHeaderOptions(raw string) map[string]string {
	headers := make(map[string]string)
	if raw == "" {
		return headers
	}

	var decoded map[string]string
	if err := stdjson.Unmarshal([]byte(raw), &decoded); err == nil {
		for key, value := range decoded {
			if key != "" {
				headers[key] = value
			}
		}
		return headers
	}

	for _, item := range util.SplitList(raw) {
		name, value, ok := strings.Cut(item, ":")
		if ok && strings.TrimSpace(name) != "" {
			headers[strings.TrimSpace(name)] = strings.TrimSpace(value)
		}
	}
	return headers
}

func normalizeNodeType(nodeType string) string {
	switch strings.ToLower(nodeType) {
	case "ss":
		return "shadowsocks"
	case "hy2":
		return "hysteria2"
	default:
		return strings.ToLower(nodeType)
	}
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on", "tls", "reality":
		return true
	default:
		return false
	}
}

func explicitlyFalse(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "n", "off", "none", "disabled":
		return true
	default:
		return false
	}
}
