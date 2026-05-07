package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"chijie/internal/dialer"
	"chijie/internal/fingerprint"
	"chijie/internal/netguard"
	"chijie/internal/traffic"
	"chijie/internal/util"

	"github.com/gorilla/websocket"
)

// TunnelRequest WebSocket 隧道首帧请求
type TunnelRequest struct {
	URL           string            `json:"url"`
	Method        string            `json:"method,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Payload       string            `json:"payload,omitempty"`
	Egress        EgressOptions     `json:"egress,omitempty"`
	Authorization string            `json:"authorization,omitempty"`
}

// upgrader 默认拒绝跨站浏览器发起的 WebSocket 升级。
// 无 Origin header（服务端客户端，如 CF Workers / Go / Node.js）直接放行；
// 有 Origin header 时必须与请求 Host 同源，否则视为可疑跨站请求。
var upgrader = websocket.Upgrader{
	CheckOrigin: checkSameOriginOrEmpty,
}

// checkSameOriginOrEmpty 校验 WebSocket Origin header。
// 规则：无 Origin 放行；有 Origin 且 host 等于请求 Host 放行；其余拒绝。
func checkSameOriginOrEmpty(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

// handleTunnel 处理 WebSocket 隧道请求
// 流程: WS 升级 → 读取首帧(JSON) → 认证 → 出口选择 → 拨号目标 → 双向转发
func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request) {
	preAuthorized := s.auth.VerifyRequest(r)

	// 升级为 WebSocket
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		util.Warnf("tunnel: ws upgrade failed: %v", err)
		return
	}
	defer wsConn.Close()

	// 读取首帧：隧道目标信息
	_ = wsConn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, msg, err := wsConn.ReadMessage()
	if err != nil {
		util.Warnf("tunnel: read init frame failed: %v", err)
		return
	}
	_ = wsConn.SetReadDeadline(time.Time{})

	var req TunnelRequest
	if err := json.Unmarshal(msg, &req); err != nil {
		wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"invalid init frame"}`))
		return
	}

	if !preAuthorized && !s.auth.VerifyAuthorization(req.Authorization) {
		wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"unauthorized"}`))
		return
	}

	if req.URL == "" {
		wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"url required"}`))
		return
	}
	if err := s.validateTargetURL(r.Context(), req.URL); err != nil {
		wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"invalid target"}`))
		util.Warnf("tunnel: target rejected: %v", err)
		return
	}
	if req.Method == "" {
		req.Method = "GET"
	}

	routeResult, err := s.resolveEgress(req.Egress)
	if err != nil {
		wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"egress failed"}`))
		util.Warnf("tunnel: egress resolve error: %v", err)
		return
	}

	// 获取 dialer
	d, err := s.getDialer(routeResult)
	if err != nil {
		wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"get dialer failed"}`))
		util.Warnf("tunnel: get dialer error: %v", err)
		return
	}

	targetURL, err := url.Parse(req.URL)
	if err != nil {
		wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"invalid target url"}`))
		return
	}
	if targetURL.Scheme == "ws" || targetURL.Scheme == "wss" {
		s.handleWebSocketTunnel(r.Context(), wsConn, &req, routeResult, d)
		return
	}

	s.handleRawTunnel(r.Context(), wsConn, &req, routeResult, d)
}

func (s *Server) handleRawTunnel(ctx context.Context, wsConn *websocket.Conn, req *TunnelRequest, routeResult *egressRoute, d dialer.Dialer) {
	targetAddr, err := extractHostPort(req.URL)
	if err != nil {
		wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"invalid target url"}`))
		return
	}

	util.Debugf("[tunnel] %s → group:%s via %s", req.URL, routeResult.Group, d.Name())

	if !s.allowPrivateTargets {
		host, _, splitErr := net.SplitHostPort(targetAddr)
		if splitErr != nil {
			host = targetAddr
		}
		if guardErr := netguard.CheckHost(ctx, host); guardErr != nil {
			wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"target blocked"}`))
			util.Warnf("tunnel: target host %s blocked: %v", host, guardErr)
			s.recordTunnelTrace(req, routeResult, http.StatusForbidden, 0, 0, 0, guardErr.Error())
			return
		}
	}

	targetConn, err := d.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"dial target failed"}`))
		util.Warnf("tunnel: dial %s error: %v", targetAddr, err)
		s.recordTunnelTrace(req, routeResult, http.StatusBadGateway, 0, 0, 0, err.Error())
		return
	}
	defer targetConn.Close()

	wsConn.WriteMessage(websocket.TextMessage, []byte(`{"status":"connected"}`))
	started := time.Now()
	var inboundBytes int64
	var outboundBytes int64
	if s.traffic != nil {
		s.traffic.TunnelOpened()
		defer func() {
			s.traffic.TunnelClosed()
			s.recordTunnelTrace(req, routeResult, http.StatusSwitchingProtocols, atomic.LoadInt64(&inboundBytes), atomic.LoadInt64(&outboundBytes), time.Since(started), "")
		}()
	}

	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			_, data, err := wsConn.ReadMessage()
			if err != nil {
				return
			}
			atomic.AddInt64(&inboundBytes, int64(len(data)))
			if _, err := targetConn.Write(data); err != nil {
				return
			}
		}
	}()

	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 32*1024)
		for {
			n, err := targetConn.Read(buf)
			if n > 0 {
				atomic.AddInt64(&outboundBytes, int64(n))
				if writeErr := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	<-done
	_ = targetConn.Close()
	_ = wsConn.Close()
	<-done
	util.Debugf("[tunnel] %s closed", req.URL)
}

func (s *Server) handleWebSocketTunnel(ctx context.Context, wsConn *websocket.Conn, req *TunnelRequest, routeResult *egressRoute, d dialer.Dialer) {
	if !strings.EqualFold(req.Method, http.MethodGet) {
		wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"websocket method must be GET"}`))
		s.recordTunnelTrace(req, routeResult, http.StatusBadRequest, 0, 0, 0, "websocket method must be GET")
		return
	}

	targetURL, err := url.Parse(req.URL)
	if err != nil {
		wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"invalid target url"}`))
		return
	}

	headers := tunnelTargetHeaders(req.Headers)
	wsDialer := &websocket.Dialer{
		NetDialContext:    d.DialContext,
		Proxy:             nil,
		HandshakeTimeout:  30 * time.Second,
		EnableCompression: false,
	}

	if targetURL.Scheme == "wss" && routeResult.TLSFingerprint != "" && s.fpManager != nil {
		fpConfig, fpErr := s.fpManager.ConfigFromValue(routeResult.TLSFingerprint)
		if fpErr != nil {
			wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"tls fingerprint failed"}`))
			s.recordTunnelTrace(req, routeResult, http.StatusBadRequest, 0, 0, 0, fpErr.Error())
			return
		}
		if fpConfig.UserAgent != "" && headers.Get("User-Agent") == "" {
			headers.Set("User-Agent", fpConfig.UserAgent)
		}
		helloID, spec, fpErr := fingerprint.BuildSpecFromConfig(fpConfig)
		if fpErr != nil {
			wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"tls fingerprint failed"}`))
			s.recordTunnelTrace(req, routeResult, http.StatusBadRequest, 0, 0, 0, fpErr.Error())
			return
		}
		wsDialer.NetDialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return fingerprint.DialTLSWithDialContext(ctx, network, addr, helloID, spec, targetURL.Hostname(), d.DialContext)
		}
	}

	upstreamConn, resp, err := wsDialer.DialContext(ctx, req.URL, headers)
	status := http.StatusSwitchingProtocols
	if resp != nil {
		status = resp.StatusCode
		if resp.Body != nil {
			defer resp.Body.Close()
		}
	}
	if err != nil {
		wsConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"dial websocket target failed"}`))
		util.Warnf("tunnel: dial websocket %s error: %v", req.URL, err)
		s.recordTunnelTrace(req, routeResult, status, 0, 0, 0, err.Error())
		return
	}
	defer upstreamConn.Close()

	util.Debugf("[tunnel] websocket %s → group:%s via %s", req.URL, routeResult.Group, d.Name())
	if err := wsConn.WriteMessage(websocket.TextMessage, []byte(`{"status":"connected"}`)); err != nil {
		return
	}

	started := time.Now()
	var inboundBytes int64
	var outboundBytes int64
	if req.Payload != "" {
		atomic.AddInt64(&inboundBytes, int64(len(req.Payload)))
		if err := upstreamConn.WriteMessage(websocket.TextMessage, []byte(req.Payload)); err != nil {
			s.recordTunnelTrace(req, routeResult, http.StatusBadGateway, atomic.LoadInt64(&inboundBytes), 0, time.Since(started), err.Error())
			return
		}
	}

	if s.traffic != nil {
		s.traffic.TunnelOpened()
		defer func() {
			s.traffic.TunnelClosed()
			s.recordTunnelTrace(req, routeResult, http.StatusSwitchingProtocols, atomic.LoadInt64(&inboundBytes), atomic.LoadInt64(&outboundBytes), time.Since(started), "")
		}()
	}

	done := make(chan struct{}, 2)
	go relayWebSocketMessages(wsConn, upstreamConn, &inboundBytes, done)
	go relayWebSocketMessages(upstreamConn, wsConn, &outboundBytes, done)

	<-done
	_ = upstreamConn.Close()
	_ = wsConn.Close()
	<-done
	util.Debugf("[tunnel] websocket %s closed", req.URL)
}

func relayWebSocketMessages(from, to *websocket.Conn, byteCounter *int64, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()
	for {
		messageType, data, err := from.ReadMessage()
		if err != nil {
			return
		}
		atomic.AddInt64(byteCounter, int64(len(data)))
		if err := to.WriteMessage(messageType, data); err != nil {
			return
		}
	}
}

func tunnelTargetHeaders(values map[string]string) http.Header {
	headers := make(http.Header)
	for name, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" || tunnelHandshakeHeader(normalized) {
			continue
		}
		headers.Set(name, value)
	}
	return headers
}

func tunnelHandshakeHeader(name string) bool {
	switch name {
	case "connection", "upgrade", "sec-websocket-key", "sec-websocket-version", "sec-websocket-extensions", "sec-websocket-accept":
		return true
	default:
		return false
	}
}

func (s *Server) recordTunnelTrace(req *TunnelRequest, route *egressRoute, status int, requestBytes, responseBytes int64, elapsed time.Duration, errText string) {
	if s.traffic == nil || req == nil {
		return
	}
	trace := traffic.Trace{
		Kind:          "tunnel",
		Method:        req.Method,
		URL:           req.URL,
		Target:        traceTarget(req.URL),
		Status:        status,
		LatencyMS:     elapsed.Milliseconds(),
		RequestBytes:  requestBytes,
		ResponseBytes: responseBytes,
		Error:         errText,
	}
	applyRouteTrace(&trace, route)
	s.traffic.Record(trace)
}

// extractHostPort 从 URL 提取 host:port
func extractHostPort(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("url has no host")
	}
	port := parsed.Port()
	if port == "" {
		switch parsed.Scheme {
		case "https", "wss":
			port = "443"
		default:
			port = "80"
		}
	}

	return net.JoinHostPort(host, port), nil
}
