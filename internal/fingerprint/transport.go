package fingerprint

import (
	"context"
	stdtls "crypto/tls"
	"fmt"
	"net"
	"net/http"

	tls "github.com/refraction-networking/utls"
)

// WrapTransport 为 HTTP Transport 注入 uTLS 指纹
// helloID: 预设指纹 ID（如 HelloChrome_Auto），自定义时传 HelloCustom
// spec: 自定义 ClientHelloSpec，预设指纹时传 nil
// serverName: 目标服务器域名（SNI），如为空则从请求 URL 提取
func WrapTransport(transport *http.Transport, helloID *tls.ClientHelloID, spec *tls.ClientHelloSpec, serverName string) {
	WrapTransportWithDialContext(transport, helloID, spec, serverName, nil)
}

// WrapTransportWithDialContext 使用指定拨号函数注入 uTLS 指纹。
// dialContext 应传入已选出口的 DialContext，避免 TLS 握手绕过当前出口。
func WrapTransportWithDialContext(transport *http.Transport, helloID *tls.ClientHelloID, spec *tls.ClientHelloSpec, serverName string, dialContext func(context.Context, string, string) (net.Conn, error)) {
	if transport == nil || helloID == nil {
		return
	}

	baseDial := dialContext
	if baseDial == nil {
		baseDial = transport.DialContext
	}
	if baseDial == nil {
		baseDial = dialContextDefault
	}

	if dialContext != nil {
		transport.Proxy = nil
		transport.DialContext = baseDial
	}
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *stdtls.Conn) http.RoundTripper{}

	transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return DialTLSWithDialContext(ctx, network, addr, helloID, spec, serverName, baseDial)
	}
}

// DialTLSWithDialContext 通过指定出口建立带 uTLS 指纹的 TLS 连接，适用于需要自己完成握手的调用方。
func DialTLSWithDialContext(ctx context.Context, network, addr string, helloID *tls.ClientHelloID, spec *tls.ClientHelloSpec, serverName string, dialContext func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	if helloID == nil {
		return nil, fmt.Errorf("tls hello id is required")
	}
	baseDial := dialContext
	if baseDial == nil {
		baseDial = dialContextDefault
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	sni := serverName
	if sni == "" {
		sni = host
	}

	rawConn, err := baseDial(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("dial tls target: %w", err)
	}

	tlsConfig := &tls.Config{
		ServerName:   sni,
		NextProtos:   []string{"http/1.1"},
		OmitEmptyPsk: true,
	}

	selectedHelloID := *helloID
	selectedSpec := spec
	if selectedSpec == nil {
		if presetSpec, err := tls.UTLSIdToSpec(*helloID); err == nil {
			preferHTTP1ALPN(&presetSpec)
			selectedSpec = &presetSpec
			selectedHelloID = tls.HelloCustom
		}
	}

	uconn := tls.UClient(rawConn, tlsConfig, selectedHelloID)
	if selectedSpec != nil {
		preferHTTP1ALPN(selectedSpec)
		if err := uconn.ApplyPreset(selectedSpec); err != nil {
			rawConn.Close()
			return nil, fmt.Errorf("apply tls spec: %w", err)
		}
	}

	if err := uconn.HandshakeContext(ctx); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("tls handshake: %w", err)
	}

	return uconn, nil
}

func preferHTTP1ALPN(spec *tls.ClientHelloSpec) {
	if spec == nil {
		return
	}
	for _, ext := range spec.Extensions {
		if alpn, ok := ext.(*tls.ALPNExtension); ok {
			alpn.AlpnProtocols = []string{"http/1.1"}
		}
	}
}

// dialContextDefault 建立 TCP 连接，支持 context 取消
func dialContextDefault(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, network, addr)
}
