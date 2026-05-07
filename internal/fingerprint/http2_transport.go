package fingerprint

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"chijie/internal/util"

	tls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// HTTP2RoundTripper 使用 uTLS 握手后手写 HTTP/2 首帧，保留 Akamai raw 里的顺序和值。
type HTTP2RoundTripper struct {
	helloID     *tls.ClientHelloID
	spec        *tls.ClientHelloSpec
	serverName  string
	dialContext func(context.Context, string, string) (net.Conn, error)
	config      *FingerprintConfig
	maxBody     int64
}

const defaultHTTP2ResponseBodyLimit = 32 * 1024 * 1024

// NewHTTP2RoundTripper 创建带 TLS 与 HTTP/2 指纹的 RoundTripper。
func NewHTTP2RoundTripper(helloID *tls.ClientHelloID, spec *tls.ClientHelloSpec, serverName string, dialContext func(context.Context, string, string) (net.Conn, error), config *FingerprintConfig) http.RoundTripper {
	return NewHTTP2RoundTripperWithResponseLimit(helloID, spec, serverName, dialContext, config, defaultHTTP2ResponseBodyLimit)
}

// NewHTTP2RoundTripperWithResponseLimit 创建带响应体上限的 HTTP/2 指纹 RoundTripper。
func NewHTTP2RoundTripperWithResponseLimit(helloID *tls.ClientHelloID, spec *tls.ClientHelloSpec, serverName string, dialContext func(context.Context, string, string) (net.Conn, error), config *FingerprintConfig, maxBody int64) http.RoundTripper {
	if maxBody <= 0 {
		maxBody = defaultHTTP2ResponseBodyLimit
	}
	if config == nil {
		config = &FingerprintConfig{}
	}
	return &HTTP2RoundTripper{
		helloID:     helloID,
		spec:        spec,
		serverName:  serverName,
		dialContext: dialContext,
		config:      config.Canonical(),
		maxBody:     maxBody,
	}
}

func (rt *HTTP2RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("request is required")
	}
	if req.URL.Scheme != "https" {
		return nil, fmt.Errorf("HTTP/2 fingerprint transport requires https URL")
	}
	if rt.helloID == nil {
		return nil, fmt.Errorf("tls hello id is required")
	}

	conn, err := rt.dialTLS(req.Context(), req)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if deadline, ok := req.Context().Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	framer := http2.NewFramer(conn, conn)
	if _, err := io.WriteString(conn, http2.ClientPreface); err != nil {
		return nil, fmt.Errorf("write http2 preface: %w", err)
	}

	settings, windowUpdate := rt.http2FrameConfig()
	if err := framer.WriteSettings(settings...); err != nil {
		return nil, fmt.Errorf("write http2 settings: %w", err)
	}
	if windowUpdate > 0 {
		if err := framer.WriteWindowUpdate(0, windowUpdate); err != nil {
			return nil, fmt.Errorf("write http2 window update: %w", err)
		}
	}

	body, err := readRequestBody(req)
	if err != nil {
		return nil, err
	}
	headerBlock, err := encodeHTTP2Headers(req, rt.config)
	if err != nil {
		return nil, err
	}

	if err := framer.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      1,
		BlockFragment: headerBlock,
		EndHeaders:    true,
		EndStream:     len(body) == 0,
		Priority:      http2Priority(rt.config),
	}); err != nil {
		return nil, fmt.Errorf("write http2 headers: %w", err)
	}

	if len(body) > 0 {
		if err := writeHTTP2Body(framer, body); err != nil {
			return nil, err
		}
	}

	resp, err := readHTTP2Response(framer, req, rt.maxBody)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (rt *HTTP2RoundTripper) dialTLS(ctx context.Context, req *http.Request) (net.Conn, error) {
	addr := canonicalHTTPSAddr(req.URL.Host)
	baseDial := rt.dialContext
	if baseDial == nil {
		baseDial = dialContextDefault
	}

	rawConn, err := baseDial(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial tls target: %w", err)
	}

	sni := rt.serverName
	if sni == "" {
		sni = req.URL.Hostname()
	}
	protocols := rt.alpnProtocols()
	tlsConfig := &tls.Config{
		ServerName:   sni,
		NextProtos:   protocols,
		OmitEmptyPsk: true,
	}

	selectedHelloID := *rt.helloID
	selectedSpec := rt.spec
	if selectedSpec == nil {
		if presetSpec, err := tls.UTLSIdToSpec(*rt.helloID); err == nil {
			preferHTTP2ALPN(&presetSpec, protocols)
			selectedSpec = &presetSpec
			selectedHelloID = tls.HelloCustom
		}
	}

	uconn := tls.UClient(rawConn, tlsConfig, selectedHelloID)
	if selectedSpec != nil {
		preferHTTP2ALPN(selectedSpec, protocols)
		if err := uconn.ApplyPreset(selectedSpec); err != nil {
			rawConn.Close()
			return nil, fmt.Errorf("apply tls spec: %w", err)
		}
	}

	if err := uconn.HandshakeContext(ctx); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("tls handshake: %w", err)
	}
	if negotiated := uconn.ConnectionState().NegotiatedProtocol; negotiated != "h2" {
		rawConn.Close()
		return nil, fmt.Errorf("http2 alpn was not negotiated: %s", negotiated)
	}
	return uconn, nil
}

func (rt *HTTP2RoundTripper) alpnProtocols() []string {
	if rt.config != nil && rt.config.TLS != nil && len(rt.config.TLS.Protocols) > 0 {
		protocols := make([]string, 0, len(rt.config.TLS.Protocols)+1)
		hasH2 := false
		for _, protocol := range rt.config.TLS.Protocols {
			protocol = strings.TrimSpace(protocol)
			if protocol == "" {
				continue
			}
			if protocol == "h2" {
				hasH2 = true
			}
			protocols = append(protocols, protocol)
		}
		if hasH2 {
			return protocols
		}
	}
	return []string{"h2", "http/1.1"}
}

func preferHTTP2ALPN(spec *tls.ClientHelloSpec, protocols []string) {
	if spec == nil {
		return
	}
	if len(protocols) == 0 {
		protocols = []string{"h2", "http/1.1"}
	}
	hasALPN := false
	for _, ext := range spec.Extensions {
		switch typed := ext.(type) {
		case *tls.ALPNExtension:
			typed.AlpnProtocols = append([]string(nil), protocols...)
			hasALPN = true
		case *tls.ApplicationSettingsExtension:
			if util.ContainsString(protocols, "h2") {
				typed.SupportedProtocols = []string{"h2"}
			}
		case *tls.ApplicationSettingsExtensionNew:
			if util.ContainsString(protocols, "h2") {
				typed.SupportedProtocols = []string{"h2"}
			}
		}
	}
	if !hasALPN {
		spec.Extensions = append(spec.Extensions, &tls.ALPNExtension{AlpnProtocols: append([]string(nil), protocols...)})
	}
}

func (rt *HTTP2RoundTripper) http2FrameConfig() ([]http2.Setting, uint32) {
	settings := parseHTTP2Settings(nil)
	windowUpdate := uint32(0)

	if rt.config != nil {
		if rt.config.Akamai != "" {
			akamaiSettings, akamaiWindow, _ := parseAkamaiRaw(rt.config.Akamai)
			if len(akamaiSettings) > 0 {
				settings = akamaiSettings
			}
			if akamaiWindow > 0 {
				windowUpdate = akamaiWindow
			}
		}
		if rt.config.HTTP2 != nil {
			if parsed := parseHTTP2Settings(rt.config.HTTP2.Settings); len(parsed) > 0 {
				settings = parsed
			}
			if parsed := parseUint32String(rt.config.HTTP2.WindowUpdate); parsed > 0 {
				windowUpdate = parsed
			}
		}
	}

	if len(settings) == 0 {
		settings = defaultHTTP2Settings()
	}
	return settings, windowUpdate
}

func parseAkamaiRaw(value string) ([]http2.Setting, uint32, []string) {
	parts := strings.Split(strings.TrimSpace(value), "|")
	if len(parts) == 0 {
		return nil, 0, nil
	}
	settings := parseHTTP2Settings(strings.Split(parts[0], ";"))
	windowUpdate := uint32(0)
	if len(parts) > 1 {
		windowUpdate = parseUint32String(parts[1])
	}
	orderPart := ""
	if len(parts) == 3 {
		orderPart = parts[2]
	} else if len(parts) >= 4 {
		orderPart = parts[3]
	}
	return settings, windowUpdate, parseHTTP2HeaderOrder(orderPart)
}

func parseHTTP2Settings(values []string) []http2.Setting {
	settings := make([]http2.Setting, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key, rawVal, ok := splitHTTP2Setting(value)
		if !ok {
			continue
		}
		id, ok := http2SettingID(key)
		if !ok {
			continue
		}
		parsed := parseUint32String(rawVal)
		settings = append(settings, http2.Setting{ID: id, Val: parsed})
	}
	return settings
}

func splitHTTP2Setting(value string) (string, string, bool) {
	if idx := strings.Index(value, "="); idx >= 0 {
		return strings.TrimSpace(value[:idx]), strings.TrimSpace(value[idx+1:]), true
	}
	if idx := strings.Index(value, ":"); idx >= 0 {
		return strings.TrimSpace(value[:idx]), strings.TrimSpace(value[idx+1:]), true
	}
	return "", "", false
}

func http2SettingID(key string) (http2.SettingID, bool) {
	key = strings.ToUpper(strings.TrimSpace(key))
	key = strings.TrimPrefix(key, "SETTINGS_")
	switch key {
	case "1", "HEADER_TABLE_SIZE":
		return http2.SettingHeaderTableSize, true
	case "2", "ENABLE_PUSH":
		return http2.SettingEnablePush, true
	case "3", "MAX_CONCURRENT_STREAMS":
		return http2.SettingMaxConcurrentStreams, true
	case "4", "INITIAL_WINDOW_SIZE":
		return http2.SettingInitialWindowSize, true
	case "5", "MAX_FRAME_SIZE":
		return http2.SettingMaxFrameSize, true
	case "6", "MAX_HEADER_LIST_SIZE":
		return http2.SettingMaxHeaderListSize, true
	default:
		return 0, false
	}
}

func defaultHTTP2Settings() []http2.Setting {
	return []http2.Setting{
		{ID: http2.SettingHeaderTableSize, Val: 65536},
		{ID: http2.SettingEnablePush, Val: 0},
		{ID: http2.SettingInitialWindowSize, Val: 6291456},
		{ID: http2.SettingMaxHeaderListSize, Val: 262144},
	}
}

func encodeHTTP2Headers(req *http.Request, config *FingerprintConfig) ([]byte, error) {
	var headerBuf bytes.Buffer
	encoder := hpack.NewEncoder(&headerBuf)
	for _, field := range buildHTTP2HeaderFields(req, config) {
		if err := encoder.WriteField(field); err != nil {
			return nil, fmt.Errorf("encode http2 header %s: %w", field.Name, err)
		}
	}
	return headerBuf.Bytes(), nil
}

func buildHTTP2HeaderFields(req *http.Request, config *FingerprintConfig) []hpack.HeaderField {
	pseudoValues := map[string]string{
		"method":    req.Method,
		"authority": requestAuthority(req),
		"scheme":    req.URL.Scheme,
		"path":      requestPath(req),
	}

	fields := make([]hpack.HeaderField, 0, 16)
	seen := make(map[string]bool)
	add := func(name, value string) {
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		if name == "" || value == "" || forbiddenHTTP2Header(name) {
			return
		}
		fields = append(fields, hpack.HeaderField{Name: name, Value: value})
		seen[name] = true
	}
	addPseudo := func(name string) {
		name = normalizeHTTP2OrderName(name)
		if value := pseudoValues[name]; value != "" {
			add(":"+name, value)
		}
	}

	headerLines := []string(nil)
	if config != nil && config.HTTP2 != nil {
		headerLines = config.HTTP2.HeaderLines
	}
	if len(headerLines) > 0 {
		for _, line := range headerLines {
			name, value, ok := splitHTTP2HeaderLine(line)
			if !ok {
				continue
			}
			normalized := normalizeHTTP2OrderName(name)
			if _, ok := pseudoValues[normalized]; ok {
				addPseudo(normalized)
				continue
			}
			values := requestHeaderValues(req, normalized)
			if len(values) == 0 {
				add(normalized, value)
				continue
			}
			for _, headerValue := range values {
				add(normalized, headerValue)
			}
		}
		for _, name := range []string{"method", "authority", "scheme", "path"} {
			if !seen[":"+name] {
				addPseudo(name)
			}
		}
	} else {
		for _, name := range http2PseudoOrder(config) {
			addPseudo(name)
		}
	}

	if config != nil && config.UserAgent != "" && len(requestHeaderValues(req, "user-agent")) == 0 {
		add("user-agent", config.UserAgent)
	}
	for name, values := range req.Header {
		normalized := strings.ToLower(name)
		if seen[normalized] {
			continue
		}
		for _, value := range values {
			add(normalized, value)
		}
	}
	return fields
}

func http2PseudoOrder(config *FingerprintConfig) []string {
	if config != nil {
		if config.HTTP2 != nil && len(config.HTTP2.Headers) > 0 {
			return config.HTTP2.Headers
		}
		if config.Akamai != "" {
			_, _, order := parseAkamaiRaw(config.Akamai)
			if len(order) > 0 {
				return order
			}
		}
	}
	return []string{"method", "authority", "scheme", "path"}
}

func parseHTTP2HeaderOrder(value string) []string {
	parts := strings.Split(value, ",")
	order := make([]string, 0, len(parts))
	for _, part := range parts {
		name := normalizeHTTP2OrderName(part)
		if name != "" && name != "0" {
			order = append(order, name)
		}
	}
	return order
}

func http2Priority(config *FingerprintConfig) http2.PriorityParam {
	if config == nil || config.HTTP2 == nil || config.HTTP2.Priority == nil {
		return http2.PriorityParam{}
	}
	weight := config.HTTP2.Priority.Weight
	if weight > 0 {
		weight--
	}
	return http2.PriorityParam{
		StreamDep: config.HTTP2.Priority.DependsOn,
		Exclusive: config.HTTP2.Priority.Exclusive,
		Weight:    weight,
	}
}

func readRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	return body, nil
}

func writeHTTP2Body(framer *http2.Framer, body []byte) error {
	const maxFramePayload = 16 * 1024
	for len(body) > 0 {
		chunk := body
		if len(chunk) > maxFramePayload {
			chunk = body[:maxFramePayload]
		}
		body = body[len(chunk):]
		if err := framer.WriteData(1, len(body) == 0, chunk); err != nil {
			return fmt.Errorf("write http2 data: %w", err)
		}
	}
	return nil
}

func readHTTP2Response(framer *http2.Framer, req *http.Request, maxBody int64) (*http.Response, error) {
	var body bytes.Buffer
	var headerBlock bytes.Buffer
	var headers http.Header
	statusCode := 0
	streamEnded := false
	headersEnded := false
	if maxBody <= 0 {
		maxBody = defaultHTTP2ResponseBodyLimit
	}

	for {
		frame, err := framer.ReadFrame()
		if err != nil {
			return nil, fmt.Errorf("read http2 frame: %w", err)
		}

		switch typed := frame.(type) {
		case *http2.SettingsFrame:
			if !typed.IsAck() {
				if err := framer.WriteSettingsAck(); err != nil {
					return nil, fmt.Errorf("write http2 settings ack: %w", err)
				}
			}
		case *http2.HeadersFrame:
			if typed.StreamID != 1 {
				continue
			}
			headerBlock.Write(typed.HeaderBlockFragment())
			if typed.HeadersEnded() {
				parsed, code, err := decodeHTTP2Headers(headerBlock.Bytes())
				if err != nil {
					return nil, err
				}
				headers = parsed
				statusCode = code
				headersEnded = true
				headerBlock.Reset()
			}
			if typed.StreamEnded() {
				streamEnded = true
			}
		case *http2.ContinuationFrame:
			if typed.StreamID != 1 {
				continue
			}
			headerBlock.Write(typed.HeaderBlockFragment())
			if typed.HeadersEnded() {
				parsed, code, err := decodeHTTP2Headers(headerBlock.Bytes())
				if err != nil {
					return nil, err
				}
				headers = parsed
				statusCode = code
				headersEnded = true
				headerBlock.Reset()
			}
		case *http2.DataFrame:
			if typed.StreamID != 1 {
				continue
			}
			if int64(body.Len()+len(typed.Data())) > maxBody {
				return nil, fmt.Errorf("http2 response body exceeds %d bytes", maxBody)
			}
			body.Write(typed.Data())
			if typed.StreamEnded() {
				streamEnded = true
			}
		case *http2.RSTStreamFrame:
			if typed.StreamID == 1 {
				return nil, fmt.Errorf("http2 stream reset: %s", typed.ErrCode)
			}
		case *http2.GoAwayFrame:
			if typed.ErrCode == http2.ErrCodeNo {
				continue
			}
			return nil, fmt.Errorf("http2 goaway: %s", typed.ErrCode)
		}

		if headersEnded && streamEnded {
			break
		}
	}

	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	if headers == nil {
		headers = make(http.Header)
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		StatusCode:    statusCode,
		Proto:         "HTTP/2.0",
		ProtoMajor:    2,
		ProtoMinor:    0,
		Header:        headers,
		Body:          io.NopCloser(bytes.NewReader(body.Bytes())),
		ContentLength: int64(body.Len()),
		Request:       req,
	}, nil
}

func decodeHTTP2Headers(block []byte) (http.Header, int, error) {
	fields, err := hpack.NewDecoder(4096, nil).DecodeFull(block)
	if err != nil {
		return nil, 0, fmt.Errorf("decode http2 headers: %w", err)
	}
	headers := make(http.Header)
	statusCode := 0
	for _, field := range fields {
		if field.Name == ":status" {
			parsed, _ := strconv.Atoi(field.Value)
			statusCode = parsed
			continue
		}
		if strings.HasPrefix(field.Name, ":") {
			continue
		}
		headers.Add(field.Name, field.Value)
	}
	return headers, statusCode, nil
}

func requestHeaderValues(req *http.Request, name string) []string {
	name = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), ":"))
	if name == "user-agent" && req.UserAgent() != "" {
		return []string{req.UserAgent()}
	}
	return req.Header.Values(http.CanonicalHeaderKey(name))
}

func requestAuthority(req *http.Request) string {
	if req.Host != "" {
		return req.Host
	}
	return req.URL.Host
}

func requestPath(req *http.Request) string {
	path := req.URL.RequestURI()
	if path == "" {
		return "/"
	}
	return path
}

func canonicalHTTPSAddr(host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(host, "443")
}

func forbiddenHTTP2Header(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "connection", "proxy-connection", "keep-alive", "transfer-encoding", "upgrade", "host":
		return true
	default:
		return false
	}
}

func parseUint32String(value string) uint32 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(parsed)
}

// WantsHTTP2 判断此指纹是否显式要求 HTTP/2 请求层。
func (fp *FingerprintConfig) WantsHTTP2() bool {
	if fp == nil {
		return false
	}
	version := strings.ToLower(strings.TrimSpace(fp.HTTPVersion))
	if version == "h2" || version == "http/2" || version == "http/2.0" {
		return true
	}
	if fp.Akamai != "" || fp.HTTP2 != nil {
		return true
	}
	if fp.TLS != nil {
		for _, protocol := range fp.TLS.Protocols {
			if strings.TrimSpace(protocol) == "h2" {
				return true
			}
		}
	}
	if fp.JA4 != "" {
		parts := strings.SplitN(strings.TrimSpace(fp.JA4), "_", 2)
		if len(parts) > 0 && strings.HasSuffix(strings.ToLower(parts[0]), "h2") {
			return true
		}
	}
	return false
}

// EffectiveMethod 返回配置内的方法，空值使用 fallback。
func (fp *FingerprintConfig) EffectiveMethod(fallback string) string {
	if fp != nil && strings.TrimSpace(fp.Method) != "" {
		return strings.ToUpper(strings.TrimSpace(fp.Method))
	}
	if fallback == "" {
		return http.MethodGet
	}
	return fallback
}

// ApplyRequestDefaults 把导入配置里保存的请求默认值应用到测试或代理请求。
func (fp *FingerprintConfig) ApplyRequestDefaults(req *http.Request) {
	if fp == nil || req == nil {
		return
	}
	if fp.UserAgent != "" && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", fp.UserAgent)
	}
}
