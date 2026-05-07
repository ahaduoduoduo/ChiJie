package pool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"chijie/internal/dialer"
	"chijie/internal/netguard"
	"chijie/internal/util"

	"gopkg.in/yaml.v3"
)

// SubscriptionParser 订阅解析器
type SubscriptionParser struct {
	client *http.Client
}

const MaxSubscriptionBodyBytes = 4 * 1024 * 1024

func NewSubscriptionParser() *SubscriptionParser {
	return &SubscriptionParser{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Fetch 拉取并解析订阅链接
func (p *SubscriptionParser) Fetch(subURL string) ([]dialer.Node, error) {
	urls := splitSubscriptionURLs(subURL)
	if len(urls) == 0 {
		return nil, fmt.Errorf("subscription url is empty")
	}
	if len(urls) == 1 {
		return p.fetchOne(urls[0])
	}

	var allNodes []dialer.Node
	var failures []string
	for _, item := range urls {
		nodes, err := p.fetchOne(item)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", redactSubscriptionURL(item), err))
			continue
		}
		allNodes = append(allNodes, nodes...)
	}
	if len(allNodes) > 0 {
		if len(failures) > 0 {
			log.Printf("subscription partial failure: %s", strings.Join(failures, "; "))
		}
		return allNodes, nil
	}
	if len(failures) > 0 {
		return nil, fmt.Errorf("all subscription urls failed: %s", strings.Join(failures, "; "))
	}
	return nil, fmt.Errorf("subscription returned no nodes")
}

func (p *SubscriptionParser) fetchOne(subURL string) ([]dialer.Node, error) {
	if err := validateSubscriptionURL(context.Background(), subURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, subURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create subscription request: %w", err)
	}
	req.Header.Set("User-Agent", "Chijie/1.0")
	req.Header.Set("Accept", "text/plain, application/yaml, application/x-yaml, application/json, */*")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, subscriptionFetchError(subURL, err)
	}
	defer resp.Body.Close()

	body, err := readSubscriptionBody(resp, MaxSubscriptionBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read subscription body: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("subscription returned %d", resp.StatusCode)
	}

	content := strings.TrimSpace(string(body))

	// 尝试 Clash YAML 格式
	if strings.HasPrefix(content, "proxies:") || strings.Contains(content[:min(200, len(content))], "proxies:") {
		return p.parseClashYAML([]byte(content))
	}

	// 兼容未 Base64 包装的 URI 列表
	if strings.Contains(content, "://") {
		return p.parseURIList(content)
	}

	// 尝试 Base64 解码
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		// 尝试 URL-safe Base64
		decoded, err = base64.URLEncoding.DecodeString(content)
		if err != nil {
			// 尝试无 padding 的 Base64
			decoded, err = base64.RawStdEncoding.DecodeString(content)
			if err != nil {
				return nil, fmt.Errorf("cannot decode subscription: not Clash YAML or Base64")
			}
		}
	}

	return p.parseURIList(string(decoded))
}

func subscriptionFetchError(raw string, err error) error {
	redacted := redactSubscriptionURL(raw)
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return fmt.Errorf("fetch %s failed: %w", redacted, urlErr.Err)
	}
	message := strings.ReplaceAll(err.Error(), raw, redacted)
	return fmt.Errorf("fetch %s failed: %s", redacted, message)
}

func validateSubscriptionURL(ctx context.Context, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse subscription url: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("subscription url must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("subscription url has no host")
	}
	if err := netguard.CheckHost(ctx, parsed.Hostname()); err != nil {
		return fmt.Errorf("subscription host is not allowed: %w", err)
	}
	return nil
}

func readSubscriptionBody(resp *http.Response, limit int64) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	if limit <= 0 {
		return io.ReadAll(resp.Body)
	}
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("subscription body exceeds %d bytes", limit)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("subscription body exceeds %d bytes", limit)
	}
	return body, nil
}

func splitSubscriptionURLs(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == '|'
	})

	urls := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			urls = append(urls, item)
		}
	}
	return urls
}

func redactSubscriptionURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "subscription-url"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// parseClashYAML 解析 Clash 格式的 YAML 订阅
func (p *SubscriptionParser) parseClashYAML(data []byte) ([]dialer.Node, error) {
	var config struct {
		Proxies []ClashProxy `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse clash yaml: %w", err)
	}

	nodes := make([]dialer.Node, 0, len(config.Proxies))
	for _, proxy := range config.Proxies {
		node, err := clashProxyToNode(&proxy)
		if err != nil {
			log.Printf("skip proxy %s: %v", proxy.Name, err)
			continue
		}
		nodes = append(nodes, *node)
	}

	return nodes, nil
}

// ClashProxy Clash 代理配置
type ClashProxy struct {
	Name              string `yaml:"name"`
	Type              string `yaml:"type"`
	Server            string `yaml:"server"`
	Port              int    `yaml:"port"`
	Cipher            string `yaml:"cipher"`
	Password          string `yaml:"password"`
	UUID              string `yaml:"uuid"`
	AlterID           int    `yaml:"alterId"`
	Security          string `yaml:"security"`
	TLS               bool   `yaml:"tls"`
	SkipCertVerify    bool   `yaml:"skip-cert-verify"`
	SNI               string `yaml:"sni"`
	ServerName        string `yaml:"servername"`
	ClientFingerprint string `yaml:"client-fingerprint"`
	Fingerprint       string `yaml:"fingerprint"`
	ALPN              any    `yaml:"alpn"`
	Network           string `yaml:"network"`
	Flow              string `yaml:"flow"`
	Username          string `yaml:"username"`
	UDP               bool   `yaml:"udp"`
	Plugin            string `yaml:"plugin"`
	PluginOpts        string `yaml:"plugin-opts"`
	Auth              string `yaml:"auth"`
	AuthStr           string `yaml:"auth-str"`
	Up                any    `yaml:"up"`
	Down              any    `yaml:"down"`
	Ports             any    `yaml:"ports"`
	HopInterval       string `yaml:"hop-interval"`
	Obfs              string `yaml:"obfs"`
	ObfsPassword      string `yaml:"obfs-password"`
	WSOpts            *struct {
		Path    string            `yaml:"path"`
		Headers map[string]string `yaml:"headers"`
	} `yaml:"ws-opts"`
	GrpcOpts *struct {
		GrpcServiceName string `yaml:"grpc-service-name"`
	} `yaml:"grpc-opts"`
	RealityOpts *struct {
		PublicKey string `yaml:"public-key"`
		ShortID   string `yaml:"short-id"`
	} `yaml:"reality-opts"`
}

func clashProxyToNode(proxy *ClashProxy) (*dialer.Node, error) {
	node := &dialer.Node{
		Name:   proxy.Name,
		Server: proxy.Server,
		Port:   proxy.Port,
		Extra:  make(map[string]string),
	}

	switch proxy.Type {
	case "ss":
		node.Type = "ss"
		node.Password = proxy.Password
		node.Extra["cipher"] = proxy.Cipher
		setExtra(node.Extra, "plugin", proxy.Plugin)
		setExtra(node.Extra, "plugin_opts", proxy.PluginOpts)

	case "vmess":
		node.Type = "vmess"
		node.Extra["uuid"] = proxy.UUID
		node.Extra["security"] = proxy.Security
		node.Extra["alter_id"] = strconv.Itoa(proxy.AlterID)
		applyClashCommonExtras(node, proxy)

	case "trojan":
		node.Type = "trojan"
		node.Password = proxy.Password
		applyClashCommonExtras(node, proxy)

	case "vless":
		node.Type = "vless"
		node.Extra["uuid"] = proxy.UUID
		node.Extra["flow"] = proxy.Flow
		applyClashCommonExtras(node, proxy)

	case "hysteria2", "hy2":
		node.Type = "hysteria2"
		node.Password = util.FirstNonEmpty(proxy.Password, proxy.Auth, proxy.AuthStr)
		applyClashCommonExtras(node, proxy)
		setExtra(node.Extra, "up_mbps", scalarToString(proxy.Up))
		setExtra(node.Extra, "down_mbps", scalarToString(proxy.Down))
		setExtra(node.Extra, "ports", stringListToCSV(proxy.Ports))
		setExtra(node.Extra, "hop_interval", proxy.HopInterval)
		setExtra(node.Extra, "obfs", proxy.Obfs)
		setExtra(node.Extra, "obfs_password", proxy.ObfsPassword)

	case "socks5":
		node.Type = "socks5"
		node.Username = proxy.Username
		node.Password = proxy.Password

	case "http":
		node.Type = "http_proxy"
		node.Username = proxy.Username
		node.Password = proxy.Password

	default:
		return nil, fmt.Errorf("unsupported type: %s", proxy.Type)
	}

	return node, nil
}

func applyClashCommonExtras(node *dialer.Node, proxy *ClashProxy) {
	setExtra(node.Extra, "network", proxy.Network)
	if proxy.TLS {
		node.Extra["tls"] = "true"
	}
	setExtra(node.Extra, "sni", util.FirstNonEmpty(proxy.SNI, proxy.ServerName))
	if proxy.SkipCertVerify {
		node.Extra["skip_verify"] = "true"
	}
	setExtra(node.Extra, "client_fingerprint", util.FirstNonEmpty(proxy.ClientFingerprint, proxy.Fingerprint))
	setExtra(node.Extra, "alpn", stringListToCSV(proxy.ALPN))

	if proxy.WSOpts != nil {
		setExtra(node.Extra, "path", proxy.WSOpts.Path)
		if len(proxy.WSOpts.Headers) > 0 {
			if host := util.FirstNonEmpty(proxy.WSOpts.Headers["Host"], proxy.WSOpts.Headers["host"]); host != "" {
				node.Extra["host"] = host
			}
			if encoded, err := json.Marshal(proxy.WSOpts.Headers); err == nil {
				node.Extra["headers"] = string(encoded)
			}
		}
	}
	if proxy.GrpcOpts != nil {
		setExtra(node.Extra, "service_name", proxy.GrpcOpts.GrpcServiceName)
	}
	if proxy.RealityOpts != nil {
		node.Extra["security"] = "reality"
		setExtra(node.Extra, "public_key", proxy.RealityOpts.PublicKey)
		setExtra(node.Extra, "short_id", proxy.RealityOpts.ShortID)
	}
}

// parseURIList 解析 Base64 解码后的 URI 列表（每行一个 URI）
func (p *SubscriptionParser) parseURIList(content string) ([]dialer.Node, error) {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	nodes := make([]dialer.Node, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		node, err := parseURI(line)
		if err != nil {
			log.Printf("skip uri: %v", err)
			continue
		}
		nodes = append(nodes, *node)
	}

	return nodes, nil
}

// parseURI 解析单个代理 URI
func parseURI(uri string) (*dialer.Node, error) {
	lowerURI := strings.ToLower(uri)
	if strings.HasPrefix(lowerURI, "ss://") {
		return parseSSURI(uri)
	}
	if strings.HasPrefix(lowerURI, "vmess://") {
		return parseVMessURI(uri)
	}
	if strings.HasPrefix(lowerURI, "trojan://") {
		return parseTrojanURI(uri)
	}
	if strings.HasPrefix(lowerURI, "vless://") {
		return parseVLESSURI(uri)
	}
	if strings.HasPrefix(lowerURI, "hysteria2://") || strings.HasPrefix(lowerURI, "hy2://") {
		return parseHysteria2URI(uri)
	}
	return nil, fmt.Errorf("unsupported uri scheme: %s", uri[:min(10, len(uri))])
}

// parseSSURI 解析 ss:// URI
// 格式: ss://base64(method:password)@server:port#name
// 或:   ss://base64(method:password@server:port)#name
func parseSSURI(uri string) (*dialer.Node, error) {
	uri = strings.TrimPrefix(uri, "ss://")

	// 提取 fragment（节点名称）
	name := ""
	if idx := strings.Index(uri, "#"); idx != -1 {
		name, _ = url.QueryUnescape(uri[idx+1:])
		uri = uri[:idx]
	}
	query := url.Values{}
	if idx := strings.Index(uri, "?"); idx != -1 {
		query, _ = url.ParseQuery(uri[idx+1:])
		uri = uri[:idx]
	}

	var server, cipher, password string
	var port int

	if idx := strings.Index(uri, "@"); idx != -1 {
		// 格式: base64(method:password)@server:port
		userInfo := uri[:idx]
		decoded, err := decodeBase64Flexible(userInfo)
		if err != nil || !strings.Contains(decoded, ":") {
			decoded, err = url.QueryUnescape(userInfo)
			if err != nil {
				return nil, fmt.Errorf("decode ss userinfo: %w", err)
			}
		}
		parts := strings.SplitN(decoded, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid ss userinfo: %s", decoded)
		}
		cipher = parts[0]
		password = parts[1]

		hostPort := uri[idx+1:]
		host, portStr, err := net.SplitHostPort(hostPort)
		if err != nil {
			return nil, fmt.Errorf("parse ss host:port: %w", err)
		}
		server = host
		port, _ = strconv.Atoi(portStr)
	} else {
		// 格式: base64(method:password@server:port)
		decoded, err := decodeBase64Flexible(uri)
		if err != nil {
			return nil, fmt.Errorf("decode ss uri: %w", err)
		}
		parts := strings.SplitN(decoded, "@", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid ss uri: %s", decoded)
		}
		methodPass := strings.SplitN(parts[0], ":", 2)
		if len(methodPass) != 2 {
			return nil, fmt.Errorf("invalid ss method:password")
		}
		cipher = methodPass[0]
		password = methodPass[1]

		host, portStr, err := net.SplitHostPort(parts[1])
		if err != nil {
			return nil, fmt.Errorf("parse ss host:port: %w", err)
		}
		server = host
		port, _ = strconv.Atoi(portStr)
	}

	if name == "" {
		name = fmt.Sprintf("%s:%d", server, port)
	}

	node := &dialer.Node{
		Name:     name,
		Type:     "ss",
		Server:   server,
		Port:     port,
		Password: password,
		Extra:    map[string]string{"cipher": cipher},
	}
	return node, addSSQueryOptions(query, node)
}

func addSSQueryOptions(query url.Values, node *dialer.Node) error {
	if plugin := query.Get("plugin"); plugin != "" {
		plugin, _ = url.QueryUnescape(plugin)
		parts := strings.SplitN(plugin, ";", 2)
		node.Extra["plugin"] = parts[0]
		if len(parts) > 1 {
			node.Extra["plugin_opts"] = parts[1]
		}
	}
	return nil
}

// parseVMessURI 解析 vmess:// URI（V2RayN 格式，Base64 JSON）
func parseVMessURI(uri string) (*dialer.Node, error) {
	uri = strings.TrimPrefix(uri, "vmess://")

	decoded, err := decodeBase64Flexible(uri)
	if err != nil {
		return nil, fmt.Errorf("decode vmess: %w", err)
	}

	var config struct {
		V    interface{} `json:"v"`
		PS   string      `json:"ps"`
		Add  string      `json:"add"`
		Port interface{} `json:"port"`
		ID   string      `json:"id"`
		Aid  interface{} `json:"aid"`
		Scy  string      `json:"scy"`
		Net  string      `json:"net"`
		Type string      `json:"type"`
		Host string      `json:"host"`
		Path string      `json:"path"`
		TLS  string      `json:"tls"`
		SNI  string      `json:"sni"`
		FP   string      `json:"fp"`
		ALPN string      `json:"alpn"`
	}

	if err := json.Unmarshal([]byte(decoded), &config); err != nil {
		return nil, fmt.Errorf("parse vmess json: %w", err)
	}

	port := 0
	switch v := config.Port.(type) {
	case float64:
		port = int(v)
	case string:
		port, _ = strconv.Atoi(v)
	}

	security := config.Scy
	if security == "" {
		security = "auto"
	}

	name := config.PS
	if name == "" {
		name = fmt.Sprintf("%s:%d", config.Add, port)
	}

	return &dialer.Node{
		Name:   name,
		Type:   "vmess",
		Server: config.Add,
		Port:   port,
		Extra: map[string]string{
			"uuid":        config.ID,
			"security":    security,
			"network":     config.Net,
			"tls":         config.TLS,
			"sni":         config.SNI,
			"fingerprint": config.FP,
			"alpn":        config.ALPN,
			"host":        config.Host,
			"path":        config.Path,
			"alter_id":    strconv.Itoa(parseInterfaceInt(config.Aid)),
		},
	}, nil
}

// parseTrojanURI 解析 trojan:// URI
// 格式: trojan://password@server:port?sni=xxx#name
func parseTrojanURI(uri string) (*dialer.Node, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("parse trojan uri: %w", err)
	}

	password := parsed.User.Username()
	server := parsed.Hostname()
	port, _ := strconv.Atoi(parsed.Port())

	name, _ := url.QueryUnescape(parsed.Fragment)
	if name == "" {
		name = fmt.Sprintf("%s:%d", server, port)
	}

	query := parsed.Query()
	sni := query.Get("sni")
	if sni == "" {
		sni = server
	}

	return &dialer.Node{
		Name:     name,
		Type:     "trojan",
		Server:   server,
		Port:     port,
		Password: password,
		Extra: map[string]string{
			"tls":          util.FirstNonEmpty(query.Get("security"), "tls"),
			"sni":          sni,
			"skip_verify":  util.FirstNonEmpty(query.Get("allowInsecure"), query.Get("insecure")),
			"network":      query.Get("type"),
			"host":         query.Get("host"),
			"path":         query.Get("path"),
			"service_name": util.FirstNonEmpty(query.Get("serviceName"), query.Get("service_name")),
			"alpn":         query.Get("alpn"),
			"fingerprint":  util.FirstNonEmpty(query.Get("fp"), query.Get("fingerprint")),
		},
	}, nil
}

// parseVLESSURI 解析 vless:// URI
// 格式: vless://uuid@server:port?type=tcp&security=tls&sni=xxx&flow=xxx#name
func parseVLESSURI(uri string) (*dialer.Node, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("parse vless uri: %w", err)
	}

	uuid := parsed.User.Username()
	server := parsed.Hostname()
	port, _ := strconv.Atoi(parsed.Port())

	name, _ := url.QueryUnescape(parsed.Fragment)
	if name == "" {
		name = fmt.Sprintf("%s:%d", server, port)
	}

	query := parsed.Query()

	return &dialer.Node{
		Name:   name,
		Type:   "vless",
		Server: server,
		Port:   port,
		Extra: map[string]string{
			"uuid":         uuid,
			"flow":         query.Get("flow"),
			"security":     query.Get("security"),
			"sni":          util.FirstNonEmpty(query.Get("sni"), query.Get("servername")),
			"network":      query.Get("type"),
			"host":         query.Get("host"),
			"path":         query.Get("path"),
			"service_name": util.FirstNonEmpty(query.Get("serviceName"), query.Get("service_name")),
			"alpn":         query.Get("alpn"),
			"fingerprint":  util.FirstNonEmpty(query.Get("fp"), query.Get("fingerprint")),
			"skip_verify":  util.FirstNonEmpty(query.Get("allowInsecure"), query.Get("insecure")),
			"public_key":   util.FirstNonEmpty(query.Get("pbk"), query.Get("publicKey"), query.Get("public_key")),
			"short_id":     util.FirstNonEmpty(query.Get("sid"), query.Get("shortId"), query.Get("short_id")),
		},
	}, nil
}

// parseHysteria2URI 解析 hysteria2:// / hy2:// URI
// 常见格式: hysteria2://password@server:port?sni=example.com&insecure=1&obfs=salamander&obfs-password=xxx#name
func parseHysteria2URI(uri string) (*dialer.Node, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("parse hysteria2 uri: %w", err)
	}

	password := parsed.User.Username()
	server := parsed.Hostname()
	port, _ := strconv.Atoi(parsed.Port())

	name, _ := url.QueryUnescape(parsed.Fragment)
	if name == "" {
		name = fmt.Sprintf("%s:%d", server, port)
	}

	query := parsed.Query()
	return &dialer.Node{
		Name:     name,
		Type:     "hysteria2",
		Server:   server,
		Port:     port,
		Password: password,
		Extra: map[string]string{
			"tls":           "true",
			"sni":           util.FirstNonEmpty(query.Get("sni"), query.Get("peer")),
			"skip_verify":   util.FirstNonEmpty(query.Get("insecure"), query.Get("allowInsecure")),
			"alpn":          query.Get("alpn"),
			"obfs":          query.Get("obfs"),
			"obfs_password": util.FirstNonEmpty(query.Get("obfs-password"), query.Get("obfs_password")),
			"up_mbps":       util.FirstNonEmpty(query.Get("upmbps"), query.Get("up_mbps"), query.Get("up")),
			"down_mbps":     util.FirstNonEmpty(query.Get("downmbps"), query.Get("down_mbps"), query.Get("down")),
			"ports":         util.FirstNonEmpty(query.Get("mport"), query.Get("ports")),
			"hop_interval":  util.FirstNonEmpty(query.Get("hop_interval"), query.Get("hop-interval")),
		},
	}, nil
}

// decodeBase64Flexible 灵活解码 Base64（支持标准/URL-safe/有无 padding）
func decodeBase64Flexible(s string) (string, error) {
	// 先尝试标准 Base64
	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
		return string(decoded), nil
	}
	// URL-safe
	if decoded, err := base64.URLEncoding.DecodeString(s); err == nil {
		return string(decoded), nil
	}
	// 无 padding 标准
	if decoded, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return string(decoded), nil
	}
	// 无 padding URL-safe
	if decoded, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return string(decoded), nil
	}
	return "", fmt.Errorf("base64 decode failed")
}

func setExtra(extra map[string]string, key, value string) {
	if strings.TrimSpace(value) != "" {
		extra[key] = value
	}
}

func parseInterfaceInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		result, _ := strconv.Atoi(typed)
		return result
	default:
		return 0
	}
}

func scalarToString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		if typed == float64(int(typed)) {
			return strconv.Itoa(int(typed))
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(typed)
	}
}

func stringListToCSV(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []string:
		return strings.Join(typed, ",")
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := scalarToString(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ",")
	default:
		return scalarToString(typed)
	}
}
