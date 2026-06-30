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
	"sync"
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

// DefaultProxyMaxAttempts 是一次 /proxy 请求在普通出口执行失败时默认允许尝试的可用节点数量。
const DefaultProxyMaxAttempts = 5

// DefaultProxyResponseHeaderTimeout 是一次 /proxy 出口 HTTP 请求等待目标响应头的默认超时。
const DefaultProxyResponseHeaderTimeout = 3 * time.Second

// DefaultProxyTotalTimeout 是一次 /proxy 出口 HTTP 请求从开始到响应体读取完成的默认总超时。
const DefaultProxyTotalTimeout = 30 * time.Second

// DefaultProxyMaxRedirects 是 follow_redirects=true 时单次 /proxy 请求允许跟随的默认跳转次数。
const DefaultProxyMaxRedirects = 5

// MaxChijieProxyHops 限制 Chijie 之间互相转发时的最大跳数，防止 A/B 循环。
const MaxChijieProxyHops = 3

const chijieHopHeader = "X-Chijie-Hop"
const chijieErrorHeader = "X-Chijie-Error"
const chijieFinalURLHeader = "X-Chijie-Final-URL"
const chijieRedirectCountHeader = "X-Chijie-Redirect-Count"
const chijieMaxRedirectsHeader = "X-Chijie-Max-Redirects"
const chijieRedirectsHeader = "X-Chijie-Redirects"
const chijieRedirectLimitReachedHeader = "X-Chijie-Redirect-Limit-Reached"

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
	proxySettingsMu     sync.RWMutex
	proxySettings       ProxySettings
}

// ProxySettingsConfig 是 gateway.yaml / Admin API 使用的 /proxy 重试配置。
// TemplateFallbackAfterAttempts 使用指针以区分“未配置”和“显式 false”。
type ProxySettingsConfig struct {
	MaxAttempts                   int    `yaml:"max_attempts" json:"max_attempts"`
	MaxRedirects                  int    `yaml:"max_redirects" json:"max_redirects"`
	TemplateFallbackAfterAttempts *bool  `yaml:"template_fallback_after_attempts" json:"template_fallback_after_attempts"`
	ResponseHeaderTimeout         string `yaml:"response_header_timeout" json:"response_header_timeout"`
	TotalTimeout                  string `yaml:"total_timeout" json:"total_timeout"`
	RequestTimeout                string `yaml:"request_timeout,omitempty" json:"request_timeout,omitempty"`
}

// ProxySettings 是运行时归一化后的 /proxy 重试配置。
type ProxySettings struct {
	MaxAttempts                   int           `json:"max_attempts" yaml:"max_attempts"`
	MaxRedirects                  int           `json:"max_redirects" yaml:"max_redirects"`
	TemplateFallbackAfterAttempts bool          `json:"template_fallback_after_attempts" yaml:"template_fallback_after_attempts"`
	ResponseHeaderTimeout         time.Duration `json:"response_header_timeout" yaml:"response_header_timeout"`
	TotalTimeout                  time.Duration `json:"total_timeout" yaml:"total_timeout"`
}

func DefaultProxySettings() ProxySettings {
	return ProxySettings{
		MaxAttempts:                   DefaultProxyMaxAttempts,
		MaxRedirects:                  DefaultProxyMaxRedirects,
		TemplateFallbackAfterAttempts: true,
		ResponseHeaderTimeout:         DefaultProxyResponseHeaderTimeout,
		TotalTimeout:                  DefaultProxyTotalTimeout,
	}
}

func ParseProxySettings(cfg *ProxySettingsConfig) (ProxySettings, error) {
	settings := DefaultProxySettings()
	if cfg == nil {
		return settings, nil
	}
	if cfg.MaxAttempts > 0 {
		settings.MaxAttempts = cfg.MaxAttempts
	}
	if cfg.MaxRedirects > 0 {
		settings.MaxRedirects = cfg.MaxRedirects
	}
	if cfg.TemplateFallbackAfterAttempts != nil {
		settings.TemplateFallbackAfterAttempts = *cfg.TemplateFallbackAfterAttempts
	}
	headerTimeoutText := strings.TrimSpace(cfg.ResponseHeaderTimeout)
	if headerTimeoutText != "" {
		timeout, err := time.ParseDuration(headerTimeoutText)
		if err != nil || timeout <= 0 {
			return settings, fmt.Errorf("proxy.response_header_timeout must be a positive duration")
		}
		settings.ResponseHeaderTimeout = timeout
	}
	totalTimeoutText := strings.TrimSpace(cfg.TotalTimeout)
	if totalTimeoutText == "" {
		totalTimeoutText = strings.TrimSpace(cfg.RequestTimeout)
	}
	if totalTimeoutText != "" {
		timeout, err := time.ParseDuration(totalTimeoutText)
		if err != nil || timeout <= 0 {
			return settings, fmt.Errorf("proxy.total_timeout must be a positive duration")
		}
		settings.TotalTimeout = timeout
	}
	return ValidateProxySettings(settings)
}

func ValidateProxySettings(settings ProxySettings) (ProxySettings, error) {
	if settings.MaxAttempts <= 0 {
		settings.MaxAttempts = DefaultProxyMaxAttempts
	}
	if settings.MaxAttempts > 50 {
		return settings, fmt.Errorf("proxy.max_attempts must be between 1 and 50")
	}
	if settings.MaxRedirects <= 0 {
		settings.MaxRedirects = DefaultProxyMaxRedirects
	}
	if settings.MaxRedirects > 50 {
		return settings, fmt.Errorf("proxy.max_redirects must be between 1 and 50")
	}
	if settings.ResponseHeaderTimeout <= 0 {
		settings.ResponseHeaderTimeout = DefaultProxyResponseHeaderTimeout
	}
	if settings.TotalTimeout <= 0 {
		settings.TotalTimeout = DefaultProxyTotalTimeout
	}
	return settings, nil
}

func ProxySettingsConfigFromSettings(settings ProxySettings) *ProxySettingsConfig {
	value := settings.TemplateFallbackAfterAttempts
	return &ProxySettingsConfig{
		MaxAttempts:                   settings.MaxAttempts,
		MaxRedirects:                  settings.MaxRedirects,
		TemplateFallbackAfterAttempts: &value,
		ResponseHeaderTimeout:         settings.ResponseHeaderTimeout.String(),
		TotalTimeout:                  settings.TotalTimeout.String(),
	}
}

// ProxyRequest 客户端发来的代理请求
type ProxyRequest struct {
	URL             string            `json:"url"`
	Method          string            `json:"method"`
	Headers         map[string]string `json:"headers"`
	Payload         string            `json:"payload"`
	FollowRedirects bool              `json:"follow_redirects"`
	Egress          EgressOptions     `json:"egress"`
	Hop             int               `json:"-"`
}

type proxyResponse struct {
	Body                 []byte
	ContentType          string
	Location             string
	SetCookies           []string
	StatusCode           int
	FinalURL             string
	Redirects            []proxyRedirect
	RedirectMax          int
	RedirectDetails      bool
	RedirectLimitReached bool
}

// EgressOptions 由调用方直接声明出口需求。
type EgressOptions struct {
	Region         string `json:"region"`
	Strategy       string `json:"strategy"`
	Residential    bool   `json:"residential"`
	Premium        bool   `json:"premium"`
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
	Premium        bool
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
	ProxySettings       *ProxySettings
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
		proxySettings:       DefaultProxySettings(),
	}
	if cfg.ProxySettings != nil {
		settings, err := ValidateProxySettings(*cfg.ProxySettings)
		if err == nil {
			s.proxySettings = settings
		}
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

func (s *Server) ProxySettings() ProxySettings {
	if s == nil {
		return DefaultProxySettings()
	}
	s.proxySettingsMu.RLock()
	defer s.proxySettingsMu.RUnlock()
	settings := s.proxySettings
	if settings.MaxAttempts <= 0 {
		settings = DefaultProxySettings()
	}
	settings, err := ValidateProxySettings(settings)
	if err != nil {
		return DefaultProxySettings()
	}
	return settings
}

func (s *Server) UpdateProxySettings(settings ProxySettings) {
	if s == nil {
		return
	}
	settings, err := ValidateProxySettings(settings)
	if err != nil {
		return
	}
	s.proxySettingsMu.Lock()
	s.proxySettings = settings
	s.proxySettingsMu.Unlock()
}

func (s *Server) proxyResponseHeaderTimeout() time.Duration {
	return s.ProxySettings().ResponseHeaderTimeout
}

func (s *Server) proxyTotalTimeout() time.Duration {
	return s.ProxySettings().TotalTimeout
}

func (s *Server) proxyMaxRedirects() int {
	return s.ProxySettings().MaxRedirects
}

func (s *Server) applyProxyResponseHeaderTimeout(transport *http.Transport) {
	if transport == nil {
		return
	}
	transport.ResponseHeaderTimeout = s.proxyResponseHeaderTimeout()
}

func (s *Server) newRemoteChijieClient() *http.Client {
	transport := &http.Transport{}
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	}
	transport.ResponseHeaderTimeout = s.proxyResponseHeaderTimeout()
	return &http.Client{
		Transport: transport,
		Timeout:   s.proxyTotalTimeout(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
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

	util.Debugf("[egress] %s %s → group:%s strategy:%s residential:%t premium:%t",
		req.Method, req.URL, route.Group, route.Strategy, route.Residential, route.Premium)

	proxyResp, finalRoute, err := s.doProxyWithRetry(r.Context(), &req, route)
	if err != nil {
		util.Warnf("[egress] proxy failed: %v", err)
		s.recordProxyTrace(&req, finalRoute, http.StatusBadGateway, requestBytes, 0, time.Since(requestStarted), err.Error())
		writeProxyError(w, http.StatusBadGateway, "proxy request failed", err.Error())
		return
	}

	s.recordProxyTrace(&req, finalRoute, proxyResp.StatusCode, requestBytes, int64(len(proxyResp.Body)), time.Since(requestStarted), "")
	writeProxyResponse(w, proxyResp)
}

func writeProxyResponse(w http.ResponseWriter, resp *proxyResponse) {
	if resp.ContentType != "" {
		w.Header().Set("Content-Type", resp.ContentType)
	}
	if resp.Location != "" {
		w.Header().Set("Location", resp.Location)
	}
	for _, cookie := range resp.SetCookies {
		if strings.TrimSpace(cookie) != "" {
			w.Header().Add("Set-Cookie", cookie)
		}
	}
	writeRedirectHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	w.Write(resp.Body)
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
	trace.Premium = route.Premium
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

func (s *Server) doProxyWithRetry(ctx context.Context, req *ProxyRequest, route *egressRoute) (*proxyResponse, *egressRoute, error) {
	attempts := proxyAttemptRoutes(route, s.ProxySettings())
	var attemptErrors []string
	var lastRoute *egressRoute
	var lastErr error

	for idx, attemptRoute := range attempts {
		lastRoute = attemptRoute
		resp, err := s.doProxy(ctx, req, attemptRoute)
		if err == nil {
			return resp, attemptRoute, nil
		}
		lastErr = err
		attemptErrors = append(attemptErrors, fmt.Sprintf("%s: %v", routeAttemptName(attemptRoute), err))
		if isRetryableProxyAttempt(ctx, err) {
			s.markAttemptNodeUnavailable(attemptRoute)
		}
		if idx+1 >= len(attempts) || !isRetryableProxyAttempt(ctx, err) {
			break
		}
		util.Warnf("[egress] attempt via %s failed, retrying next candidate: %v", routeAttemptName(attemptRoute), err)
	}

	if len(attemptErrors) > 1 {
		return nil, lastRoute, errors.New(strings.Join(attemptErrors, "; "))
	}
	if lastErr != nil {
		return nil, lastRoute, lastErr
	}
	return nil, lastRoute, fmt.Errorf("proxy request failed")
}

func proxyAttemptRoutes(route *egressRoute, settings ProxySettings) []*egressRoute {
	if route == nil {
		return nil
	}
	if len(route.Choices) == 0 {
		return []*egressRoute{route}
	}
	settings, err := ValidateProxySettings(settings)
	if err != nil {
		settings = DefaultProxySettings()
	}
	hasNodeChoice := false
	for _, choice := range route.Choices {
		if choice != nil && !choice.Template {
			hasNodeChoice = true
			break
		}
	}

	routes := make([]*egressRoute, 0, len(route.Choices))
	nodeAttempts := 0
	for _, choice := range route.Choices {
		if choice == nil {
			continue
		}
		if choice.Template {
			if hasNodeChoice && !settings.TemplateFallbackAfterAttempts {
				continue
			}
		} else {
			if nodeAttempts >= settings.MaxAttempts {
				continue
			}
			nodeAttempts++
		}
		attemptRoute := *route
		attemptRoute.Choice = choice
		attemptRoute.Region = choice.Region
		attemptRoute.Group = choice.Group
		attemptRoute.Residential = choice.Residential
		attemptRoute.Premium = choice.Premium
		routes = append(routes, &attemptRoute)
	}
	return routes
}

func (s *Server) markAttemptNodeUnavailable(route *egressRoute) {
	if s == nil || s.poolManager == nil || route == nil || route.Choice == nil || route.Choice.Template {
		return
	}
	if ok := s.poolManager.MarkNodeUnavailable(route.Choice.PoolName, route.Choice.NodeName); ok {
		util.Warnf("[egress] marked %s unavailable after proxy attempt failure", routeAttemptName(route))
	}
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
	if isProxyRedirectError(err) {
		return false
	}
	var attemptErr *proxyAttemptError
	return errors.As(err, &attemptErr)
}

func (s *Server) doProxy(ctx context.Context, req *ProxyRequest, route *egressRoute) (*proxyResponse, error) {
	if isChijieTemplateRoute(route) {
		return s.doRemoteChijieProxy(ctx, req, route)
	}

	d, err := s.getDialer(route)
	if err != nil {
		return nil, fmt.Errorf("get dialer: %w", err)
	}

	var bodyReader io.Reader
	if req.Payload != "" && req.Method != "GET" && req.Method != "HEAD" {
		bodyReader = strings.NewReader(req.Payload)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	transport := d.GetHTTPTransport()
	s.applyProxyResponseHeaderTimeout(transport)
	if route.TLSFingerprint != "" && s.fpManager != nil {
		fpConfig, err := s.fpManager.ConfigFromValue(route.TLSFingerprint)
		if err != nil {
			return nil, fmt.Errorf("build tls fingerprint: %w", err)
		}
		helloID, spec, err := fingerprint.BuildSpecFromConfig(fpConfig)
		if err != nil {
			return nil, fmt.Errorf("build tls fingerprint: %w", err)
		}
		fpConfig.ApplyRequestDefaults(httpReq)
		if fpConfig.WantsHTTP2() {
			transport = nil
		} else {
			fingerprint.WrapTransportWithDialContext(transport, helloID, spec, "", d.DialContext)
		}

		if fpConfig.WantsHTTP2() {
			client := &http.Client{
				Transport: fingerprint.NewHTTP2RoundTripperWithResponseLimitAndHeaderTimeout(helloID, spec, "", d.DialContext, fpConfig, MaxProxyResponseBytes, s.proxyResponseHeaderTimeout()),
				Timeout:   s.proxyTotalTimeout(),
			}
			redirects := s.newProxyRedirectTracker(req)
			client.CheckRedirect = redirects.CheckRedirect

			startTime := time.Now()
			resp, err := client.Do(httpReq)
			elapsed := time.Since(startTime)
			if err != nil {
				return nil, &proxyAttemptError{err: fmt.Errorf("do request via %s: %w", d.Name(), err)}
			}
			defer resp.Body.Close()

			proxyResp, err := buildProxyResponse(resp)
			if err != nil {
				return nil, fmt.Errorf("read response: %w", err)
			}
			redirects.Apply(proxyResp, resp)

			util.Debugf("[egress] %dms via %s → %d (%d bytes)", elapsed.Milliseconds(), d.Name(), resp.StatusCode, len(proxyResp.Body))
			return proxyResp, nil
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   s.proxyTotalTimeout(),
	}
	redirects := s.newProxyRedirectTracker(req)
	client.CheckRedirect = redirects.CheckRedirect

	startTime := time.Now()
	resp, err := client.Do(httpReq)
	elapsed := time.Since(startTime)
	if err != nil {
		return nil, &proxyAttemptError{err: fmt.Errorf("do request via %s: %w", d.Name(), err)}
	}
	defer resp.Body.Close()

	proxyResp, err := buildProxyResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	redirects.Apply(proxyResp, resp)

	util.Debugf("[egress] %dms via %s → %d (%d bytes)", elapsed.Milliseconds(), d.Name(), resp.StatusCode, len(proxyResp.Body))
	return proxyResp, nil
}

func isChijieTemplateRoute(route *egressRoute) bool {
	return route != nil && route.Choice != nil && route.Choice.Template && pool.NormalizeTemplateType(route.Choice.TemplateType) == "chijie"
}

func (s *Server) doRemoteChijieProxy(ctx context.Context, req *ProxyRequest, route *egressRoute) (*proxyResponse, error) {
	if req == nil || route == nil || route.Choice == nil {
		return nil, fmt.Errorf("remote chijie route is incomplete")
	}
	if strings.TrimSpace(route.Choice.BearerToken) == "" {
		return nil, &proxyAttemptError{err: fmt.Errorf("remote chijie %s has no bearer token", routeAttemptName(route))}
	}
	proxyURL, err := pool.ChijieProxyURL(route.Choice.Endpoint, 0)
	if err != nil {
		return nil, &proxyAttemptError{err: fmt.Errorf("remote chijie %s endpoint: %w", routeAttemptName(route), err)}
	}

	forwardReq := *req
	if route.Region != "" {
		forwardReq.Egress.Region = route.Region
	}
	if route.Strategy != "" {
		forwardReq.Egress.Strategy = route.Strategy
	}
	forwardReq.Egress.Residential = route.Residential || (route.Choice != nil && route.Choice.Residential)
	forwardReq.Egress.Premium = route.Premium || (route.Choice != nil && route.Choice.Premium)
	if route.TLSFingerprint != "" {
		forwardReq.Egress.TLSFingerprint = route.TLSFingerprint
	}
	body, err := json.Marshal(&forwardReq)
	if err != nil {
		return nil, fmt.Errorf("marshal remote chijie request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, proxyURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create remote chijie request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(route.Choice.BearerToken))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "*/*")
	httpReq.Header.Set(chijieHopHeader, strconv.Itoa(req.Hop+1))

	client := s.remoteChijieClient
	if client == nil {
		client = s.newRemoteChijieClient()
	}

	startTime := time.Now()
	resp, err := client.Do(httpReq)
	elapsed := time.Since(startTime)
	if err != nil {
		return nil, &proxyAttemptError{err: fmt.Errorf("do request via remote chijie %s: %w", routeAttemptName(route), err)}
	}
	defer resp.Body.Close()

	proxyResp, err := buildProxyResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("read remote chijie response: %w", err)
	}
	applyRedirectHeadersFromRemote(proxyResp, resp.Header)
	if resp.Header.Get(chijieErrorHeader) != "" {
		detail := strings.TrimSpace(string(proxyResp.Body))
		if len(detail) > 240 {
			detail = detail[:240] + "..."
		}
		return nil, &proxyAttemptError{err: fmt.Errorf("remote chijie %s returned gateway error %d: %s", routeAttemptName(route), resp.StatusCode, detail)}
	}

	util.Debugf("[egress] %dms via remote chijie %s → %d (%d bytes)", elapsed.Milliseconds(), routeAttemptName(route), resp.StatusCode, len(proxyResp.Body))
	return proxyResp, nil
}

func buildProxyResponse(resp *http.Response) (*proxyResponse, error) {
	respBody, err := readProxyResponseBody(resp, MaxProxyResponseBytes)
	if err != nil {
		return nil, err
	}
	return &proxyResponse{
		Body:        respBody,
		ContentType: resp.Header.Get("Content-Type"),
		Location:    resp.Header.Get("Location"),
		SetCookies:  append([]string(nil), resp.Header.Values("Set-Cookie")...),
		StatusCode:  resp.StatusCode,
	}, nil
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
	selector := pool.EgressSelector{Residential: options.Residential, Premium: options.Premium}
	fingerprintValue := strings.TrimSpace(options.TLSFingerprint)
	region := strings.TrimSpace(options.Region)
	maxLatencyMS := options.MaxLatencyMS
	if maxLatencyMS < 0 {
		maxLatencyMS = 0
	}

	if region == "" {
		if options.Any || options.Premium {
			choices, routeSelector, err := s.selectAnyEgressCandidates(strategy, selector, time.Duration(maxLatencyMS)*time.Millisecond)
			if err != nil {
				return nil, err
			}
			choice := choices[0]
			return &egressRoute{
				Any:            true,
				Region:         choice.Region,
				Group:          pool.AnyEgressGroupFor(routeSelector),
				Strategy:       strategy,
				Residential:    routeSelector.Residential,
				Premium:        routeSelector.Premium,
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
		choices, routeSelector, err := s.selectAnyEgressCandidates(strategy, selector, time.Duration(maxLatencyMS)*time.Millisecond)
		if err != nil {
			return nil, err
		}
		choice := choices[0]
		return &egressRoute{
			Any:            true,
			Region:         choice.Region,
			Group:          pool.AnyEgressGroupFor(routeSelector),
			Strategy:       strategy,
			Residential:    routeSelector.Residential,
			Premium:        routeSelector.Premium,
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

	settings := s.ProxySettings()
	var choices []*pool.EgressChoice
	var err error
	if settings.TemplateFallbackAfterAttempts {
		choices, err = s.poolManager.SelectEgressCandidatesWithTemplateFallbackFor(region, strategy, selector)
	} else {
		choices, err = s.poolManager.SelectEgressCandidatesFor(region, strategy, selector)
	}
	if err != nil {
		return nil, err
	}
	choice := choices[0]
	routeResidential := choice.Residential
	routePremium := choice.Premium
	routeGroup := choice.Group
	if routeGroup == "" {
		routeGroup = pool.EgressGroupFor(region, pool.EgressSelector{Residential: routeResidential, Premium: routePremium})
	}

	return &egressRoute{
		Region:         region,
		Group:          routeGroup,
		Strategy:       strategy,
		Residential:    routeResidential,
		Premium:        routePremium,
		TLSFingerprint: fingerprintValue,
		Choice:         choice,
		Choices:        choices,
	}, nil
}

func (s *Server) selectAnyEgressCandidates(strategy string, selector pool.EgressSelector, maxLatency time.Duration) ([]*pool.EgressChoice, pool.EgressSelector, error) {
	choices, err := s.poolManager.SelectAnyEgressCandidatesFor(strategy, selector, maxLatency)
	if err == nil {
		return choices, selector, nil
	}
	if selector.Premium && !selector.Residential {
		fallback := selector
		fallback.Residential = true
		if fallbackChoices, fallbackErr := s.poolManager.SelectAnyEgressCandidatesFor(strategy, fallback, maxLatency); fallbackErr == nil {
			return fallbackChoices, fallback, nil
		}
	}
	return nil, selector, err
}

func isAnyRegion(region string) bool {
	switch strings.ToUpper(strings.TrimSpace(region)) {
	case "*", "ANY", "AUTO":
		return true
	default:
		return false
	}
}
