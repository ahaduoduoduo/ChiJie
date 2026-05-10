package pool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"chijie/internal/dialer"
)

// HealthChecker 节点健康检查器
type HealthChecker struct {
	manager  *Manager
	interval time.Duration
	timeout  time.Duration
	testURL  string
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex
	lastRun  map[string]time.Time
}

// connectivityCommon 抽出节点 / 模板探测结果共享的字段。
// 嵌入到具体结果结构后，由于 Go 嵌入字段在 encoding/json 中默认平铺，
// 输出 JSON 与之前保持完全一致。
type connectivityCommon struct {
	Pool        string `json:"pool"`
	Node        string `json:"node"`
	TestURL     string `json:"test_url,omitempty"`
	Phase       string `json:"phase,omitempty"`
	HTTPStatus  int    `json:"http_status,omitempty"`
	ObservedIP  string `json:"observed_ip,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
	IPVersion   string `json:"ip_version,omitempty"`
	IPType      string `json:"ip_type,omitempty"`
	IPISP       string `json:"ip_isp,omitempty"`
	IPOrg       string `json:"ip_org,omitempty"`
	IPASN       int    `json:"ip_asn,omitempty"`
	IPDomain    string `json:"ip_domain,omitempty"`
	IPProxy     bool   `json:"ip_proxy,omitempty"`
	IPVPN       bool   `json:"ip_vpn,omitempty"`
	IPTor       bool   `json:"ip_tor,omitempty"`
	IPHosting   bool   `json:"ip_hosting,omitempty"`
	GeoError    string `json:"geo_error,omitempty"`
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
}

// NodeTestResult 是一次即时节点探测结果。
type NodeTestResult struct {
	connectivityCommon
	Enabled   bool          `json:"enabled"`
	Alive     bool          `json:"alive"`
	Latency   time.Duration `json:"-"`
	LatencyMS int64         `json:"latency_ms"`
	FailCount int           `json:"fail_count"`
}

// TemplateTestResult 是一次模板节点即时探测结果。
type TemplateTestResult struct {
	connectivityCommon
	Region           string        `json:"region"`
	Residential      bool          `json:"residential"`
	TemplateType     string        `json:"template_type,omitempty"`
	Enabled          bool          `json:"enabled"`
	Latency          time.Duration `json:"-"`
	LatencyMS        int64         `json:"latency_ms"`
	ResolvedUsername string        `json:"resolved_username,omitempty"`
}

const (
	defaultTemplateTestURL = "https://api.ipify.org?format=json"
	chijieErrorHeader      = "X-Chijie-Error"
)

var newChijieTemplateHTTPClient = func(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(manager *Manager, interval, timeout time.Duration, testURL string) *HealthChecker {
	if testURL == "" {
		testURL = "https://www.google.com/generate_204"
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if interval == 0 {
		interval = minHealthCheckInterval(manager, 5*time.Minute)
		if interval > 30*time.Second {
			interval = 30 * time.Second
		}
	}

	return &HealthChecker{
		manager:  manager,
		interval: interval,
		timeout:  timeout,
		testURL:  testURL,
		lastRun:  make(map[string]time.Time),
	}
}

// Start 启动后台健康检查
func (hc *HealthChecker) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	hc.cancel = cancel

	hc.wg.Add(1)
	go func() {
		defer hc.wg.Done()

		// 启动后立即检查一次
		hc.checkAll(true)

		ticker := time.NewTicker(hc.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hc.checkAll(false)
			}
		}
	}()

	log.Printf("health checker started (interval: %s)", hc.interval)
}

// Stop 停止健康检查
func (hc *HealthChecker) Stop() {
	if hc.cancel != nil {
		hc.cancel()
	}
	hc.wg.Wait()
}

type healthCheckOptions struct {
	Interval time.Duration
	Timeout  time.Duration
	TestURL  string
	MaxFail  int
}

type healthCheckTarget struct {
	PoolName string
	Pool     *Pool
	Entries  []*NodeEntry
	Options  healthCheckOptions
}

// checkAll 检查所有池中的所有节点
func (hc *HealthChecker) checkAll(force bool) {
	targets := hc.listHealthTargets()
	now := time.Now()

	var wg sync.WaitGroup
	for _, target := range targets {
		if force {
			hc.markPoolRun(target.PoolName, now)
		} else {
			if !hc.shouldRunPool(target.PoolName, target.Options.Interval, now) {
				continue
			}
		}

		for _, entry := range target.Entries {
			wg.Add(1)
			go func(target healthCheckTarget, e *NodeEntry) {
				defer wg.Done()
				latency, err := hc.checkNode(e, target.Options)
				target.Pool.mu.Lock()
				alive, failCount, markedDead := updateNodeHealth(e, latency, err, target.Options.MaxFail)
				nodeName := e.Node.Name
				target.Pool.mu.Unlock()
				if err != nil && markedDead && !alive && failCount >= target.Options.MaxFail {
					log.Printf("[health] %s/%s marked dead: %v", target.PoolName, nodeName, err)
				}
			}(target, entry)
		}
	}
	wg.Wait()
}

// checkNode 检查单个节点的连通性和延迟
func (hc *HealthChecker) checkNode(entry *NodeEntry, options healthCheckOptions) (time.Duration, error) {
	result := probeNodeConnectivity(entry, options.TestURL, options.Timeout)
	return result.Latency, result.Error
}

func (hc *HealthChecker) listHealthTargets() []healthCheckTarget {
	hc.manager.mu.RLock()
	defer hc.manager.mu.RUnlock()

	targets := make([]healthCheckTarget, 0, len(hc.manager.pools))
	for name, nodePool := range hc.manager.pools {
		if nodePool == nil || nodePool.Config == nil || !poolEnabled(nodePool.Config) {
			continue
		}
		options := healthCheckOptionsForPool(nodePool, hc.interval, hc.timeout, hc.testURL)
		target := healthCheckTarget{
			PoolName: name,
			Pool:     nodePool,
			Options:  options,
		}

		nodePool.mu.RLock()
		for _, entry := range nodePool.Entries {
			if entry == nil || entry.Node == nil || entry.Node.Type == "direct" || !entry.Enabled {
				continue
			}
			target.Entries = append(target.Entries, entry)
		}
		nodePool.mu.RUnlock()

		if len(target.Entries) > 0 {
			targets = append(targets, target)
		}
	}
	return targets
}

func (hc *HealthChecker) shouldRunPool(poolName string, interval time.Duration, now time.Time) bool {
	if interval <= 0 {
		interval = hc.interval
	}
	hc.mu.Lock()
	defer hc.mu.Unlock()
	last := hc.lastRun[poolName]
	if !last.IsZero() && now.Sub(last) < interval {
		return false
	}
	hc.lastRun[poolName] = now
	return true
}

func (hc *HealthChecker) markPoolRun(poolName string, now time.Time) {
	hc.mu.Lock()
	hc.lastRun[poolName] = now
	hc.mu.Unlock()
}

func minHealthCheckInterval(manager *Manager, fallback time.Duration) time.Duration {
	if manager == nil {
		return fallback
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	minInterval := fallback
	for _, nodePool := range manager.pools {
		if nodePool == nil || nodePool.Config == nil || nodePool.Config.HealthCheck == nil {
			continue
		}
		if interval, err := time.ParseDuration(strings.TrimSpace(nodePool.Config.HealthCheck.Interval)); err == nil && interval > 0 && interval < minInterval {
			minInterval = interval
		}
	}
	return minInterval
}

func healthCheckOptionsForPool(nodePool *Pool, defaultInterval, defaultTimeout time.Duration, defaultURL string) healthCheckOptions {
	options := healthCheckOptions{
		Interval: defaultInterval,
		Timeout:  defaultTimeout,
		TestURL:  defaultURL,
		MaxFail:  3,
	}
	if options.Interval <= 0 {
		options.Interval = 5 * time.Minute
	}
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Second
	}
	if options.TestURL == "" {
		options.TestURL = "https://www.google.com/generate_204"
	}
	if nodePool == nil || nodePool.Config == nil || nodePool.Config.HealthCheck == nil {
		return options
	}
	cfg := nodePool.Config.HealthCheck
	if interval, err := time.ParseDuration(strings.TrimSpace(cfg.Interval)); err == nil && interval > 0 {
		options.Interval = interval
	}
	if timeout, err := time.ParseDuration(strings.TrimSpace(cfg.Timeout)); err == nil && timeout > 0 {
		options.Timeout = timeout
	}
	if strings.TrimSpace(cfg.URL) != "" {
		options.TestURL = strings.TrimSpace(cfg.URL)
	}
	if cfg.MaxFail > 0 {
		options.MaxFail = cfg.MaxFail
	}
	return options
}

type connectivityProbeResult struct {
	TestURL    string
	Phase      string
	HTTPStatus int
	ObservedIP string
	Latency    time.Duration
	Error      error
}

func probeNodeConnectivity(entry *NodeEntry, testURL string, timeout time.Duration) connectivityProbeResult {
	result := connectivityProbeResult{
		TestURL: testURL,
		Phase:   "prepare",
	}
	if entry.Dialer == nil {
		return result
	}
	if testURL == "" {
		testURL = "https://www.google.com/generate_204"
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	result.TestURL = testURL

	transport := &http.Transport{
		DialContext: entry.Dialer.DialContext,
		// 不复用连接，每次检查都新建
		DisableKeepAlives: true,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	start := time.Now()
	resp, err := client.Get(testURL)
	latency := time.Since(start)
	result.Latency = latency

	if err != nil {
		// 如果是超时，也尝试简单的 TCP 连通性测试
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			result.Phase = "timeout"
			result.Error = err
			return result
		}
		result.Phase = classifyConnectivityError(err)
		result.Error = err
		return result
	}
	defer resp.Body.Close()
	result.HTTPStatus = resp.StatusCode
	result.Phase = "target_http"
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr == nil {
		result.ObservedIP = extractObservedIP(body)
	}

	// 204 或 200 都算成功
	if resp.StatusCode == 204 || resp.StatusCode == 200 {
		return result
	}

	return result // 能连通就算活着
}

func extractObservedIP(body []byte) string {
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		for _, key := range []string{"ip", "origin", "query"} {
			if value, ok := payload[key].(string); ok {
				if ip := firstIP(value); ip != "" {
					return ip
				}
			}
		}
	}
	return firstIP(string(body))
}

func firstIP(value string) string {
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '"' || r == '\''
	}) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if ip := net.ParseIP(part); ip != nil {
			return ip.String()
		}
	}
	return ""
}

type IPInfo struct {
	CountryCode string
	Country     string
	Region      string
	City        string
	IPVersion   string
	IPType      string
	ISP         string
	Org         string
	ASN         int
	Domain      string
	Proxy       bool
	VPN         bool
	Tor         bool
	Hosting     bool
}

func lookupIPInfo(ip string, timeout time.Duration) (IPInfo, error) {
	if ip == "" {
		return IPInfo{}, nil
	}
	if timeout == 0 || timeout > 2*time.Second {
		timeout = 2 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get("https://ipwho.is/" + url.PathEscape(ip))
	if err != nil {
		return IPInfo{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		Success     bool   `json:"success"`
		Type        string `json:"type"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		Region      string `json:"region"`
		City        string `json:"city"`
		Message     string `json:"message"`
		Connection  struct {
			ASN    int    `json:"asn"`
			Org    string `json:"org"`
			ISP    string `json:"isp"`
			Domain string `json:"domain"`
		} `json:"connection"`
		Security struct {
			Anonymous bool `json:"anonymous"`
			Proxy     bool `json:"proxy"`
			VPN       bool `json:"vpn"`
			Tor       bool `json:"tor"`
			Hosting   bool `json:"hosting"`
		} `json:"security"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32*1024)).Decode(&payload); err != nil {
		return IPInfo{}, err
	}
	if !payload.Success && payload.Message != "" {
		return IPInfo{}, fmt.Errorf("%s", payload.Message)
	}
	info := IPInfo{
		CountryCode: strings.ToUpper(strings.TrimSpace(payload.CountryCode)),
		Country:     strings.TrimSpace(payload.Country),
		Region:      strings.TrimSpace(payload.Region),
		City:        strings.TrimSpace(payload.City),
		IPVersion:   strings.TrimSpace(payload.Type),
		ISP:         strings.TrimSpace(payload.Connection.ISP),
		Org:         strings.TrimSpace(payload.Connection.Org),
		ASN:         payload.Connection.ASN,
		Domain:      strings.TrimSpace(payload.Connection.Domain),
		Proxy:       payload.Security.Proxy || payload.Security.Anonymous,
		VPN:         payload.Security.VPN,
		Tor:         payload.Security.Tor,
		Hosting:     payload.Security.Hosting,
	}
	info.IPType = classifyIPType(info)
	return info, nil
}

func classifyIPType(info IPInfo) string {
	switch {
	case info.Tor:
		return "tor"
	case info.VPN:
		return "vpn"
	case info.Proxy:
		return "proxy"
	case info.Hosting:
		return "hosting"
	}
	text := strings.ToLower(strings.Join([]string{info.ISP, info.Org, info.Domain}, " "))
	switch {
	case containsAny(text, "mobile", "wireless", "cellular", "lte", "5g", "t-mobile", "tmobile", "china mobile", "softbank mobile", "sk telecom", "ntt docomo", "kddi", "verizon wireless"):
		return "mobile"
	case containsAny(text, "cloud", "hosting", "host", "data center", "datacenter", "server", "vps", "colo", "colocation", "amazon", "aws", "google cloud", "microsoft", "azure", "digitalocean", "ovh", "hetzner", "linode", "akamai", "cloudflare", "oracle", "leaseweb", "choopa", "vultr", "constant company", "constant.com", "cleardocks"):
		return "datacenter"
	case containsAny(text, "broadband", "fiber", "fibre", "cable", "xfinity", "comcast", "charter", "spectrum", "cox", "frontier", "centurylink", "telefonica", "deutsche telekom", "bt ", "british telecom", "rogers", "bell", "telus", "singtel", "hkt", "netvigator", "pccw"):
		return "residential"
	case containsAny(text, "telecom", "communications", "internet", "isp", "network", "networks", "telekom", "telco"):
		return "isp"
	case text != "":
		return "business"
	default:
		return "unknown"
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func classifyConnectivityError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "proxy returned"),
		strings.Contains(text, "send connect"),
		strings.Contains(text, "read connect response"):
		return "proxy_connect"
	case strings.Contains(text, "connect to proxy"):
		return "proxy_tcp"
	case strings.Contains(text, "tls handshake"):
		return "tls_handshake"
	default:
		return "request"
	}
}

func updateNodeHealth(entry *NodeEntry, latency time.Duration, err error, maxFail int) (bool, int, bool) {
	if maxFail <= 0 {
		maxFail = 3
	}
	if err != nil {
		markedDead := false
		if entry.Alive {
			entry.FailCount++
			if entry.FailCount >= maxFail {
				entry.Alive = false
				markedDead = true
			}
		}
		return entry.Alive, entry.FailCount, markedDead
	}

	entry.Alive = true
	entry.FailCount = 0
	entry.Latency = latency
	return entry.Alive, entry.FailCount, false
}

// TestNodeConnectivity 立即通过指定节点访问测试 URL，并更新运行时健康状态。
func (m *Manager) TestNodeConnectivity(poolName, nodeName, testURL string, timeout time.Duration) (*NodeTestResult, error) {
	m.mu.RLock()
	nodePool, ok := m.pools[poolName]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("pool not found: %s", poolName)
	}

	nodePool.mu.RLock()
	var entry *NodeEntry
	for _, item := range nodePool.Entries {
		if item.Node.Name == nodeName {
			entry = item
			break
		}
	}
	nodePool.mu.RUnlock()
	if entry == nil {
		return nil, fmt.Errorf("node not found in pool %s: %s", poolName, nodeName)
	}

	probe := probeNodeConnectivity(entry, testURL, timeout)

	nodePool.mu.Lock()
	options := healthCheckOptionsForPool(nodePool, 5*time.Minute, 5*time.Second, "https://www.google.com/generate_204")
	alive, failCount, _ := updateNodeHealth(entry, probe.Latency, probe.Error, options.MaxFail)
	result := &NodeTestResult{
		connectivityCommon: connectivityCommon{
			Pool:       poolName,
			Node:       nodeName,
			TestURL:    probe.TestURL,
			Phase:      probe.Phase,
			HTTPStatus: probe.HTTPStatus,
			ObservedIP: probe.ObservedIP,
			OK:         probe.Error == nil,
		},
		Enabled:   entry.Enabled,
		Alive:     alive,
		Latency:   entry.Latency,
		LatencyMS: entry.Latency.Milliseconds(),
		FailCount: failCount,
	}
	if probe.Error != nil {
		result.Error = probe.Error.Error()
	}
	nodePool.mu.Unlock()
	if result.ObservedIP != "" {
		if info, geoErr := lookupIPInfo(result.ObservedIP, timeout); geoErr != nil {
			result.GeoError = geoErr.Error()
		} else {
			applyIPInfo(&result.connectivityCommon, info)
		}
	}

	return result, nil
}

// TestTemplateConnectivity 立即用模板池和地区生成临时节点并探测连通性。
func (m *Manager) TestTemplateConnectivity(poolName, region, testURL string, timeout time.Duration) (*TemplateTestResult, error) {
	region = NormalizeRegionCode(region)
	if region == "" {
		return nil, fmt.Errorf("region must be a two-letter region code")
	}

	m.mu.RLock()
	nodePool, ok := m.pools[poolName]
	if !ok {
		m.mu.RUnlock()
		return nil, fmt.Errorf("pool not found: %s", poolName)
	}
	if nodePool.Config == nil || nodePool.Config.Source != "template" {
		m.mu.RUnlock()
		return nil, fmt.Errorf("pool %s is not a template pool", poolName)
	}
	cfg := *nodePool.Config
	cfg.Tags = append([]string(nil), nodePool.Config.Tags...)
	enabled := poolEnabled(nodePool.Config)
	m.mu.RUnlock()

	if NormalizeTemplateType(cfg.TemplateType) == "chijie" {
		return testChijieTemplateConnectivity(poolName, region, &cfg, enabled, testURL, timeout)
	}

	d, err := m.buildTemplateDialer(poolName, &cfg, region)
	if err != nil {
		return nil, err
	}

	nodeName := fmt.Sprintf("%s-%s", poolName, strings.ToLower(region))
	entry := &NodeEntry{
		Node:    &dialer.Node{Name: nodeName, Type: cfg.Type, Server: cfg.Server, Port: cfg.Port},
		Dialer:  d,
		Enabled: true,
		Alive:   true,
	}
	probe := probeNodeConnectivity(entry, testURL, timeout)
	result := &TemplateTestResult{
		connectivityCommon: connectivityCommon{
			Pool:       poolName,
			Node:       nodeName,
			TestURL:    probe.TestURL,
			Phase:      probe.Phase,
			HTTPStatus: probe.HTTPStatus,
			ObservedIP: probe.ObservedIP,
			OK:         probe.Error == nil,
		},
		Region:           region,
		Residential:      cfg.Residential,
		TemplateType:     "proxy",
		Enabled:          enabled,
		Latency:          probe.Latency,
		LatencyMS:        probe.Latency.Milliseconds(),
		ResolvedUsername: templateUsername(cfg.UsernameTemplate, region),
	}
	if probe.Error != nil {
		result.Error = probe.Error.Error()
	}
	if result.ObservedIP != "" {
		if info, geoErr := lookupIPInfo(result.ObservedIP, timeout); geoErr != nil {
			result.GeoError = geoErr.Error()
		} else {
			applyIPInfo(&result.connectivityCommon, info)
		}
	}
	return result, nil
}

func testChijieTemplateConnectivity(poolName, region string, cfg *PoolConfig, enabled bool, testURL string, timeout time.Duration) (*TemplateTestResult, error) {
	if cfg == nil {
		return nil, fmt.Errorf("template config is required")
	}
	endpoint, err := ChijieProxyURL(cfg.Endpoint, cfg.Port)
	if err != nil {
		return nil, err
	}
	bearer := strings.TrimSpace(cfg.BearerToken)
	if bearer == "" {
		return nil, fmt.Errorf("chijie template bearer_token is required")
	}
	if testURL == "" {
		testURL = defaultTemplateTestURL
	}
	if timeout == 0 {
		timeout = 15 * time.Second
	}

	residential := NormalizeTemplateCoverage(cfg.Coverage, cfg.Residential, cfg.TemplateType) == "residential"
	payload := map[string]any{
		"url":    testURL,
		"method": http.MethodGet,
		"headers": map[string]string{
			"Accept": "application/json",
		},
		"egress": map[string]any{
			"region":      region,
			"strategy":    "least-latency",
			"residential": residential,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal chijie template test request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create chijie template test request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	nodeName := fmt.Sprintf("%s-%s", poolName, strings.ToLower(region))
	result := &TemplateTestResult{
		connectivityCommon: connectivityCommon{
			Pool:    poolName,
			Node:    nodeName,
			TestURL: testURL,
			Phase:   "remote_chijie",
		},
		Region:       region,
		Residential:  residential,
		TemplateType: "chijie",
		Enabled:      enabled,
	}

	start := time.Now()
	resp, err := newChijieTemplateHTTPClient(timeout).Do(req)
	result.Latency = time.Since(start)
	result.LatencyMS = result.Latency.Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	defer resp.Body.Close()

	result.HTTPStatus = resp.StatusCode
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		result.Error = readErr.Error()
		return result, nil
	}
	result.ObservedIP = extractObservedIP(respBody)
	if code := strings.TrimSpace(resp.Header.Get(chijieErrorHeader)); code != "" {
		detail := strings.TrimSpace(string(respBody))
		if detail == "" {
			detail = code
		}
		result.Error = detail
		return result, nil
	}
	result.OK = true
	if result.ObservedIP != "" {
		if info, geoErr := lookupIPInfo(result.ObservedIP, timeout); geoErr != nil {
			result.GeoError = geoErr.Error()
		} else {
			applyIPInfo(&result.connectivityCommon, info)
		}
	}
	return result, nil
}

// applyIPInfo 把 IP 富信息映射到测试结果共有字段。
func applyIPInfo(common *connectivityCommon, info IPInfo) {
	common.CountryCode = info.CountryCode
	common.IPVersion = info.IPVersion
	common.IPType = info.IPType
	common.IPISP = info.ISP
	common.IPOrg = info.Org
	common.IPASN = info.ASN
	common.IPDomain = info.Domain
	common.IPProxy = info.Proxy
	common.IPVPN = info.VPN
	common.IPTor = info.Tor
	common.IPHosting = info.Hosting
}
