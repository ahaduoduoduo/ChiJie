package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"chijie/internal/dialer"
	"chijie/internal/fingerprint"
	"chijie/internal/netguard"
	"chijie/internal/pool"
	"chijie/internal/traffic"
	"chijie/internal/util"
)

// MaxProxyBodyBytes 是 /proxy 单次请求允许的最大 body 大小。
const MaxProxyBodyBytes = 10 * 1024 * 1024

// MaxProxyResponseBytes 是 /proxy 单次上游响应允许读取的最大 body 大小。
const MaxProxyResponseBytes = 32 * 1024 * 1024

// MaxProxyAttempts 是一次 /proxy 请求在出口执行失败时允许尝试的最大出口数量。
const MaxProxyAttempts = 2

// MaxChijieProxyHops 限制 Chijie 之间互相转发时的最大跳数，防止 A/B 循环。
const MaxChijieProxyHops = 3

const chijieHopHeader = "X-Chijie-Hop"
const chijieErrorHeader = "X-Chijie-Error"

var errProxyResponseTooLarge = errors.New("upstream response body is too large")

type proxyAttemptError struct {
	err error
}

func (e *proxyAttemptError) Error() string {
	return e.err.Error()
}

func (e *proxyAttemptError) Unwrap() error {
	return e.err
}

// Server HTTP 服务器
type Server struct {
	auth                *Auth
	poolManager         *pool.Manager
	fpManager           *fingerprint.Manager
	traffic             *traffic.Store
	httpServer          *http.Server
	allowPrivateTargets bool
	remoteChijieClient  *http.Client
}

// ProxyRequest 客户端发来的代理请求
type ProxyRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Payload string            `json:"payload"`
	Egress  EgressOptions     `json:"egress"`
	Hop     int               `json:"-"`
}

// EgressOptions 由调用方直接声明出口需求。
type EgressOptions struct {
	Region         string `json:"region"`
	Strategy       string `json:"strategy"`
	Residential    bool   `json:"residential"`
	TLSFingerprint string `json:"tls_fingerprint"`
	Any            bool   `json:"any"`
	MaxLatencyMS   int    `json:"max_latency_ms"`
}

type egressRoute struct {
	Direct         bool
	Any            bool
	Region         string
	Group          string
	Strategy       string
	Residential    bool
	TLSFingerprint string
	MaxLatencyMS   int
	Choice         *pool.EgressChoice
	Choices        []*pool.EgressChoice
}

// Config 服务器配置
type Config struct {
	Listen              string
	JWTSecret           string
	TLSCert             string
	TLSKey              string
	AllowPrivateTargets bool
}

var errInvalidRegion = errors.New("region must be an empty string or a two-letter region code")

// NewServer 创建服务器
func NewServer(cfg *Config, poolManager *pool.Manager, fpManager *fingerprint.Manager, trafficStores ...*traffic.Store) *Server {
	var trafficStore *traffic.Store
	if len(trafficStores) > 0 {
		trafficStore = trafficStores[0]
	}
	if trafficStore == nil {
		trafficStore = traffic.NewStore(1000)
	}

	s := &Server{
		auth:                NewAuth(cfg.JWTSecret),
		poolManager:         poolManager,
		fpManager:           fpManager,
		traffic:             trafficStore,
		allowPrivateTargets: cfg.AllowPrivateTargets,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/proxy", s.handleProxy)
	mux.HandleFunc("/tunnel", s.handleTunnel)
	mux.HandleFunc("/health", s.handleHealth)

	s.httpServer = &http.Server{
		Addr:         cfg.Listen,
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

// Start 启动服务器
func (s *Server) Start() error {
	log.Printf("server listening on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// StartTLS 启动 TLS 服务器
func (s *Server) StartTLS(certFile, keyFile string) error {
	log.Printf("server listening on %s (TLS)", s.httpServer.Addr)
	return s.httpServer.ListenAndServeTLS(certFile, keyFile)
}

// Shutdown 优雅关闭服务器，等待在途请求完成。
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

// handleHealth 健康检查
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// handleProxy 处理代理请求
func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	requestStarted := time.Now()

	// 仅允许同源请求或无 Origin 的服务端客户端。CORS 通配符放任浏览器跨站滥用网关。
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		if parsed, err := url.Parse(origin); err == nil && strings.EqualFold(parsed.Host, r.Host) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
	}

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		writeProxyError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	if !s.auth.VerifyRequest(r) {
		writeProxyError(w, http.StatusForbidden, "unauthorized", "")
		return
	}
	proxyHop := chijieHopFromRequest(r)
	if proxyHop >= MaxChijieProxyHops {
		writeProxyError(w, http.StatusLoopDetected, "proxy loop detected", fmt.Sprintf("max %s is %d", chijieHopHeader, MaxChijieProxyHops))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxProxyBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeProxyError(w, http.StatusBadRequest, "read body failed", err.Error())
		return
	}
	defer r.Body.Close()

	var req ProxyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeProxyError(w, http.StatusBadRequest, "invalid json", err.Error())
		return
	}
	if req.URL == "" {
		writeProxyError(w, http.StatusBadRequest, "url required", "")
		return
	}
	if err := s.validateTargetURL(r.Context(), req.URL); err != nil {
		writeProxyError(w, http.StatusBadRequest, "invalid target", err.Error())
		return
	}
	if req.Method == "" {
		req.Method = "GET"
	}
	if req.Headers == nil {
		req.Headers = map[string]string{}
	}
	req.Hop = proxyHop

	requestBytes := int64(len(body))
	route, err := s.resolveEgress(req.Egress)
	if err != nil {
		util.Warnf("egress resolve error: %v", err)
		s.recordProxyTrace(&req, nil, http.StatusBadRequest, requestBytes, 0, time.Since(requestStarted), err.Error())
		writeProxyError(w, http.StatusBadRequest, "egress failed", err.Error())
		return
	}

	util.Debugf("[egress] %s %s → group:%s strategy:%s residential:%t",
		req.Method, req.URL, route.Group, route.Strategy, route.Residential)

	respBody, contentType, statusCode, finalRoute, err := s.doProxyWithRetry(r.Context(), &req, route)
	if err != nil {
		util.Warnf("[egress] proxy failed: %v", err)
		s.recordProxyTrace(&req, finalRoute, http.StatusBadGateway, requestBytes, 0, time.Since(requestStarted), err.Error())
		writeProxyError(w, http.StatusBadGateway, "proxy request failed", err.Error())
		return
	}

	s.recordProxyTrace(&req, finalRoute, statusCode, requestBytes, int64(len(respBody)), time.Since(requestStarted), "")
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(statusCode)
	w.Write(respBody)
}

func writeProxyError(w http.ResponseWriter, status int, code string, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(chijieErrorHeader, code)
	w.WriteHeader(status)
	payload := map[string]string{"error": code}
	if detail != "" {
		payload["detail"] = detail
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) recordProxyTrace(req *ProxyRequest, route *egressRoute, status int, requestBytes, responseBytes int64, elapsed time.Duration, errText string) {
	if s.traffic == nil || req == nil {
		return
	}

	trace := traffic.Trace{
		Kind:          "proxy",
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

func applyRouteTrace(trace *traffic.Trace, route *egressRoute) {
	if trace == nil || route == nil {
		return
	}
	trace.EgressGroup = route.Group
	trace.Region = route.Region
	trace.Strategy = route.Strategy
	trace.Residential = route.Residential
	trace.TLSFingerprint = route.TLSFingerprint
	if route.Choice != nil {
		trace.EgressPool = route.Choice.PoolName
		trace.EgressNode = route.Choice.NodeName
		trace.EgressSource = route.Choice.Source
		trace.EgressTemplate = route.Choice.Template
	}
}

func traceTarget(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return rawURL
	}
	if parsed.Path == "" {
		return parsed.Host
	}
	return parsed.Host + parsed.Path
}

func chijieHopFromRequest(r *http.Request) int {
	if r == nil {
		return 0
	}
	hop, _ := strconv.Atoi(strings.TrimSpace(r.Header.Get(chijieHopHeader)))
	if hop < 0 {
		return 0
	}
	return hop
}

func (s *Server) doProxyWithRetry(ctx context.Context, req *ProxyRequest, route *egressRoute) ([]byte, string, int, *egressRoute, error) {
	attempts := proxyAttemptRoutes(route, proxyAttemptLimit(route))
	var attemptErrors []string
	var lastRoute *egressRoute
	var lastErr error

	for idx, attemptRoute := range attempts {
		lastRoute = attemptRoute
		respBody, contentType, statusCode, err := s.doProxy(ctx, req, attemptRoute)
		if err == nil {
			return respBody, contentType, statusCode, attemptRoute, nil
		}
		lastErr = err
		attemptErrors = append(attemptErrors, fmt.Sprintf("%s: %v", routeAttemptName(attemptRoute), err))
		if idx+1 >= len(attempts) || !isRetryableProxyAttempt(ctx, err) {
			break
		}
		util.Warnf("[egress] attempt via %s failed, retrying next candidate: %v", routeAttemptName(attemptRoute), err)
	}

	if len(attemptErrors) > 1 {
		return nil, "", 0, lastRoute, errors.New(strings.Join(attemptErrors, "; "))
	}
	if lastErr != nil {
		return nil, "", 0, lastRoute, lastErr
	}
	return nil, "", 0, lastRoute, fmt.Errorf("proxy request failed")
}

func proxyAttemptLimit(route *egressRoute) int {
	if route == nil || len(route.Choices) == 0 {
		return MaxProxyAttempts
	}
	for _, choice := range route.Choices {
		if choice != nil && choice.Template {
			return len(route.Choices)
		}
	}
	return MaxProxyAttempts
}

func proxyAttemptRoutes(route *egressRoute, maxAttempts int) []*egressRoute {
	if route == nil {
		return nil
	}
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	if len(route.Choices) == 0 {
		return []*egressRoute{route}
	}
	limit := len(route.Choices)
	if limit > maxAttempts {
		limit = maxAttempts
	}
	routes := make([]*egressRoute, 0, limit)
	for _, choice := range route.Choices[:limit] {
		attemptRoute := *route
		attemptRoute.Choice = choice
		attemptRoute.Region = choice.Region
		attemptRoute.Group = choice.Group
		routes = append(routes, &attemptRoute)
	}
	return routes
}

func routeAttemptName(route *egressRoute) string {
	if route == nil {
		return "unknown"
	}
	if route.Choice != nil {
		if route.Choice.NodeName != "" {
			return route.Choice.NodeName
		}
		if route.Choice.PoolName != "" {
			return route.Choice.PoolName
		}
	}
	return route.Group
}

func isRetryableProxyAttempt(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	var attemptErr *proxyAttemptError
	return errors.As(err, &attemptErr)
}

func (s *Server) doProxy(ctx context.Context, req *ProxyRequest, route *egressRoute) ([]byte, string, int, error) {
	if isChijieTemplateRoute(route) {
		return s.doRemoteChijieProxy(ctx, req, route)
	}

	d, err := s.getDialer(route)
	if err != nil {
		return nil, "", 0, fmt.Errorf("get dialer: %w", err)
	}

	var bodyReader io.Reader
	if req.Payload != "" && req.Method != "GET" && req.Method != "HEAD" {
		bodyReader = strings.NewReader(req.Payload)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bodyReader)
	if err != nil {
		return nil, "", 0, fmt.Errorf("create request: %w", err)
	}

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	transport := d.GetHTTPTransport()
	if route.TLSFingerprint != "" && s.fpManager != nil {
		fpConfig, err := s.fpManager.ConfigFromValue(route.TLSFingerprint)
		if err != nil {
			return nil, "", 0, fmt.Errorf("build tls fingerprint: %w", err)
		}
		helloID, spec, err := fingerprint.BuildSpecFromConfig(fpConfig)
		if err != nil {
			return nil, "", 0, fmt.Errorf("build tls fingerprint: %w", err)
		}
		fpConfig.ApplyRequestDefaults(httpReq)
		if fpConfig.WantsHTTP2() {
			transport = nil
		} else {
			fingerprint.WrapTransportWithDialContext(transport, helloID, spec, "", d.DialContext)
		}

		if fpConfig.WantsHTTP2() {
			client := &http.Client{
				Transport: fingerprint.NewHTTP2RoundTripperWithResponseLimit(helloID, spec, "", d.DialContext, fpConfig, MaxProxyResponseBytes),
				Timeout:   30 * time.Second,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}

			startTime := time.Now()
			resp, err := client.Do(httpReq)
			elapsed := time.Since(startTime)
			if err != nil {
				return nil, "", 0, &proxyAttemptError{err: fmt.Errorf("do request via %s: %w", d.Name(), err)}
			}
			defer resp.Body.Close()

			respBody, err := readProxyResponseBody(resp, MaxProxyResponseBytes)
			if err != nil {
				return nil, "", 0, fmt.Errorf("read response: %w", err)
			}

			contentType := resp.Header.Get("Content-Type")
			util.Debugf("[egress] %dms via %s → %d (%d bytes)", elapsed.Milliseconds(), d.Name(), resp.StatusCode, len(respBody))
			return respBody, contentType, resp.StatusCode, nil
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	startTime := time.Now()
	resp, err := client.Do(httpReq)
	elapsed := time.Since(startTime)
	if err != nil {
		return nil, "", 0, &proxyAttemptError{err: fmt.Errorf("do request via %s: %w", d.Name(), err)}
	}
	defer resp.Body.Close()

	respBody, err := readProxyResponseBody(resp, MaxProxyResponseBytes)
	if err != nil {
		return nil, "", 0, fmt.Errorf("read response: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	util.Debugf("[egress] %dms via %s → %d (%d bytes)", elapsed.Milliseconds(), d.Name(), resp.StatusCode, len(respBody))
	return respBody, contentType, resp.StatusCode, nil
}

func isChijieTemplateRoute(route *egressRoute) bool {
	return route != nil && route.Choice != nil && route.Choice.Template && pool.NormalizeTemplateType(route.Choice.TemplateType) == "chijie"
}

func (s *Server) doRemoteChijieProxy(ctx context.Context, req *ProxyRequest, route *egressRoute) ([]byte, string, int, error) {
	if req == nil || route == nil || route.Choice == nil {
		return nil, "", 0, fmt.Errorf("remote chijie route is incomplete")
	}
	if strings.TrimSpace(route.Choice.BearerToken) == "" {
		return nil, "", 0, &proxyAttemptError{err: fmt.Errorf("remote chijie %s has no bearer token", routeAttemptName(route))}
	}
	proxyURL, err := pool.ChijieProxyURL(route.Choice.Endpoint, 0)
	if err != nil {
		return nil, "", 0, &proxyAttemptError{err: fmt.Errorf("remote chijie %s endpoint: %w", routeAttemptName(route), err)}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, "", 0, fmt.Errorf("marshal remote chijie request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, proxyURL, bytes.NewReader(body))
	if err != nil {
		return nil, "", 0, fmt.Errorf("create remote chijie request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(route.Choice.BearerToken))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "*/*")
	httpReq.Header.Set(chijieHopHeader, strconv.Itoa(req.Hop+1))

	client := s.remoteChijieClient
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}

	startTime := time.Now()
	resp, err := client.Do(httpReq)
	elapsed := time.Since(startTime)
	if err != nil {
		return nil, "", 0, &proxyAttemptError{err: fmt.Errorf("do request via remote chijie %s: %w", routeAttemptName(route), err)}
	}
	defer resp.Body.Close()

	respBody, err := readProxyResponseBody(resp, MaxProxyResponseBytes)
	if err != nil {
		return nil, "", 0, fmt.Errorf("read remote chijie response: %w", err)
	}
	if resp.Header.Get(chijieErrorHeader) != "" {
		detail := strings.TrimSpace(string(respBody))
		if len(detail) > 240 {
			detail = detail[:240] + "..."
		}
		return nil, "", 0, &proxyAttemptError{err: fmt.Errorf("remote chijie %s returned gateway error %d: %s", routeAttemptName(route), resp.StatusCode, detail)}
	}

	contentType := resp.Header.Get("Content-Type")
	util.Debugf("[egress] %dms via remote chijie %s → %d (%d bytes)", elapsed.Milliseconds(), routeAttemptName(route), resp.StatusCode, len(respBody))
	return respBody, contentType, resp.StatusCode, nil
}

func readProxyResponseBody(resp *http.Response, limit int64) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	if limit <= 0 {
		return io.ReadAll(resp.Body)
	}
	if resp.ContentLength > limit {
		return nil, errProxyResponseTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errProxyResponseTooLarge
	}
	return body, nil
}

func (s *Server) getDialer(route *egressRoute) (dialer.Dialer, error) {
	if route.Direct {
		base := dialer.NewDirectDialer()
		if s.allowPrivateTargets {
			return base, nil
		}
		return &guardedDialer{Dialer: base, allowPrivate: false}, nil
	}
	if route.Choice == nil || route.Choice.Dialer == nil {
		return nil, fmt.Errorf("no egress choice")
	}
	return route.Choice.Dialer, nil
}

// validateTargetURL 校验调用方声明的目标 URL 协议合法且解析后的主机不命中私网黑名单。
// allow_private_targets=true 时跳过 IP 校验。
func (s *Server) validateTargetURL(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "ws", "wss":
	default:
		return fmt.Errorf("unsupported scheme: %s", parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("url has no host")
	}
	if s.allowPrivateTargets {
		return nil
	}
	host := parsed.Hostname()
	if err := netguard.CheckHost(ctx, host); err != nil {
		return err
	}
	return nil
}

func (s *Server) resolveEgress(options EgressOptions) (*egressRoute, error) {
	strategy := pool.NormalizeStrategy(options.Strategy)
	fingerprintValue := strings.TrimSpace(options.TLSFingerprint)
	region := strings.TrimSpace(options.Region)
	maxLatencyMS := options.MaxLatencyMS
	if maxLatencyMS < 0 {
		maxLatencyMS = 0
	}

	if region == "" {
		if options.Any {
			choices, err := s.poolManager.SelectAnyEgressCandidates(strategy, options.Residential, time.Duration(maxLatencyMS)*time.Millisecond)
			if err != nil {
				return nil, err
			}
			choice := choices[0]
			return &egressRoute{
				Any:            true,
				Region:         choice.Region,
				Group:          pool.AnyEgressGroup(options.Residential),
				Strategy:       strategy,
				Residential:    options.Residential,
				TLSFingerprint: fingerprintValue,
				MaxLatencyMS:   maxLatencyMS,
				Choice:         choice,
				Choices:        choices,
			}, nil
		}
		return &egressRoute{
			Direct:         true,
			Group:          "DIRECT",
			Strategy:       strategy,
			TLSFingerprint: fingerprintValue,
		}, nil
	}

	if isAnyRegion(region) {
		choices, err := s.poolManager.SelectAnyEgressCandidates(strategy, options.Residential, time.Duration(maxLatencyMS)*time.Millisecond)
		if err != nil {
			return nil, err
		}
		choice := choices[0]
		return &egressRoute{
			Any:            true,
			Region:         choice.Region,
			Group:          pool.AnyEgressGroup(options.Residential),
			Strategy:       strategy,
			Residential:    options.Residential,
			TLSFingerprint: fingerprintValue,
			MaxLatencyMS:   maxLatencyMS,
			Choice:         choice,
			Choices:        choices,
		}, nil
	}

	region = pool.NormalizeRegionCode(region)
	if region == "" {
		return nil, errInvalidRegion
	}

	choices, err := s.poolManager.SelectEgressCandidates(region, strategy, options.Residential)
	if err != nil {
		return nil, err
	}
	choice := choices[0]

	return &egressRoute{
		Region:         region,
		Group:          pool.EgressGroup(region, options.Residential),
		Strategy:       strategy,
		Residential:    options.Residential,
		TLSFingerprint: fingerprintValue,
		Choice:         choice,
		Choices:        choices,
	}, nil
}

func isAnyRegion(region string) bool {
	switch strings.ToUpper(strings.TrimSpace(region)) {
	case "*", "ANY", "AUTO":
		return true
	default:
		return false
	}
}
