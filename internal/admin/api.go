package admin

import (
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"chijie/internal/auth"
	"chijie/internal/dialer"
	"chijie/internal/fingerprint"
	"chijie/internal/netguard"
	"chijie/internal/pool"
	proxyserver "chijie/internal/server"
	"chijie/internal/traffic"
	"chijie/internal/util"

	"github.com/andybalholm/brotli"
	"github.com/golang-jwt/jwt/v5"
	"github.com/klauspost/compress/zstd"
)

// MaxAdminBodyBytes 是 Admin API 单次请求允许的最大 JSON body 大小。
const MaxAdminBodyBytes = 1 * 1024 * 1024

//go:embed all:dist
var webFS embed.FS

// Server Admin API 服务器
type Server struct {
	poolManager   *pool.Manager
	healthChecker *pool.HealthChecker
	fpManager     *fingerprint.Manager
	traffic       *traffic.Store
	configDir     string
	startTime     time.Time
	httpServer    *http.Server
	fileMu        sync.Mutex    // 保护配置文件写入
	password      string        // 管理员密码
	jwtSecret     string        // JWT 签名密钥
	jwtExpire     time.Duration // JWT 过期时间
	loginLimiter  *loginLimiter // 登录速率限制
	runtimeMu     sync.RWMutex
	runtimeInfo   RuntimeInfo
	proxyRuntime  ProxySettingsRuntime
}

// RuntimeInfo 是 Admin System 页面展示的运行时配置摘要。
type RuntimeInfo struct {
	ProxyListen string `json:"proxy_listen"`
	ProxyTLS    bool   `json:"proxy_tls"`
	AdminListen string `json:"admin_listen"`
	AuthEnabled bool   `json:"auth_enabled"`
	LogLevel    string `json:"log_level"`
	LogOutput   string `json:"log_output"`
	Version     string `json:"version"`
	GoVersion   string `json:"go_version"`
}

// ProxySettingsRuntime 是 Admin API 用来读取和更新 /proxy 运行时配置的最小接口。
type ProxySettingsRuntime interface {
	ProxySettings() proxyserver.ProxySettings
	UpdateProxySettings(proxyserver.ProxySettings)
}

// LoginLimitConfig 登录失败速率限制配置。
type LoginLimitConfig struct {
	MaxFailures int
	Window      time.Duration
	Lockout     time.Duration
}

// NewServer 创建 Admin API 服务器。
//
// loginLimit 为可选项；为 nil 时使用默认值 (5 次失败 / 60 秒窗口 / 5 分钟锁定)。
func NewServer(listen string, poolManager *pool.Manager, fpManager *fingerprint.Manager, configDir string, password string, jwtSecret string, jwtExpire string, loginLimit *LoginLimitConfig, trafficStores ...*traffic.Store) *Server {
	// 解析过期时间
	expire, err := time.ParseDuration(jwtExpire)
	if err != nil || expire == 0 {
		expire = 24 * time.Hour // 默认 24 小时
	}
	var trafficStore *traffic.Store
	if len(trafficStores) > 0 {
		trafficStore = trafficStores[0]
	}
	if trafficStore == nil {
		trafficStore = traffic.NewStore(1000)
	}

	maxFailures, window, lockout := 5, 60*time.Second, 5*time.Minute
	if loginLimit != nil {
		if loginLimit.MaxFailures > 0 {
			maxFailures = loginLimit.MaxFailures
		}
		if loginLimit.Window > 0 {
			window = loginLimit.Window
		}
		if loginLimit.Lockout > 0 {
			lockout = loginLimit.Lockout
		}
	}

	s := &Server{
		poolManager:  poolManager,
		fpManager:    fpManager,
		traffic:      trafficStore,
		configDir:    configDir,
		startTime:    time.Now(),
		password:     password,
		jwtSecret:    jwtSecret,
		jwtExpire:    expire,
		loginLimiter: newLoginLimiter(maxFailures, window, lockout),
		runtimeInfo: RuntimeInfo{
			AdminListen: listen,
			AuthEnabled: password != "",
			LogLevel:    util.LogLevelName(),
			LogOutput:   "stdout",
			Version:     "chijie 1.0.0",
			GoVersion:   runtime.Version(),
		},
	}

	mux := http.NewServeMux()

	// 登录端点（不需要鉴权）
	mux.HandleFunc("/api/auth/login", s.handleLogin)

	// 其他端点（需要鉴权）
	mux.HandleFunc("/api/auth/proxy-token", s.authMiddleware(s.handleProxyToken))
	mux.HandleFunc("/api/nodes", s.authMiddleware(s.handleNodes))
	mux.HandleFunc("/api/nodes/pool", s.authMiddleware(s.handleNodePoolConfig))
	mux.HandleFunc("/api/nodes/node", s.authMiddleware(s.handleNodeConfig))
	mux.HandleFunc("/api/nodes/subscription/node", s.authMiddleware(s.handleSubscriptionNodeConfig))
	mux.HandleFunc("/api/nodes/refresh", s.authMiddleware(s.handleNodesRefresh))
	mux.HandleFunc("/api/nodes/test", s.authMiddleware(s.handleNodeTest))
	mux.HandleFunc("/api/nodes/template/test", s.authMiddleware(s.handleTemplateTest))
	mux.HandleFunc("/api/nodes/enabled", s.authMiddleware(s.handleNodeEnabled))
	mux.HandleFunc("/api/fingerprints", s.authMiddleware(s.handleFingerprints))
	mux.HandleFunc("/api/fingerprints/", s.authMiddleware(s.handleFingerprintByName))
	mux.HandleFunc("/api/fingerprints/test", s.authMiddleware(s.handleFingerprintTest))
	mux.HandleFunc("/api/reload", s.authMiddleware(s.handleReload))
	mux.HandleFunc("/api/stats", s.authMiddleware(s.handleStats))
	mux.HandleFunc("/api/traffic", s.authMiddleware(s.handleTraffic))
	mux.HandleFunc("/api/traffic/grouping-rules", s.authMiddleware(s.handleTrafficGroupingRules))
	mux.HandleFunc("/api/traffic/success-folding-rules", s.authMiddleware(s.handleTrafficSuccessFoldingRules))
	mux.HandleFunc("/api/traffic/settings", s.authMiddleware(s.handleTrafficSettings))
	mux.HandleFunc("/api/system/logging", s.authMiddleware(s.handleLogging))
	mux.HandleFunc("/api/system/health-check", s.authMiddleware(s.handleHealthCheckSettings))
	mux.HandleFunc("/api/system/proxy", s.authMiddleware(s.handleProxySettings))
	mux.HandleFunc("/api/config/export", s.authMiddleware(s.handleConfigExport))

	// 前端静态文件（SPA fallback）
	distFS, err := fs.Sub(webFS, "dist")
	if err != nil {
		log.Printf("admin: failed to load web assets: %v", err)
	} else {
		fileServer := http.FileServer(http.FS(distFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// 尝试提供静态文件
			path := r.URL.Path
			if path == "/" {
				fileServer.ServeHTTP(w, r)
				return
			}
			// 检查文件是否存在
			f, err := distFS.Open(strings.TrimPrefix(path, "/"))
			if err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
			// 文件不存在，返回 index.html（SPA fallback）
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
		})
	}

	s.httpServer = &http.Server{
		Addr:         listen,
		Handler:      withBodyLimit(mux, MaxAdminBodyBytes),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return s
}

// SetRuntimeInfo 设置由主进程传入的运行时配置摘要。
func (s *Server) SetRuntimeInfo(info RuntimeInfo) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if info.AdminListen == "" {
		info.AdminListen = s.httpServer.Addr
	}
	if info.LogLevel == "" {
		info.LogLevel = util.LogLevelName()
	}
	if info.LogOutput == "" {
		info.LogOutput = "stdout"
	}
	if info.Version == "" {
		info.Version = "chijie 1.0.0"
	}
	if info.GoVersion == "" {
		info.GoVersion = runtime.Version()
	}
	info.AuthEnabled = s.password != ""
	s.runtimeInfo = info
}

func (s *Server) SetHealthChecker(checker *pool.HealthChecker) {
	s.healthChecker = checker
}

func (s *Server) SetProxySettingsRuntime(runtime ProxySettingsRuntime) {
	s.proxyRuntime = runtime
}

func (s *Server) runtimeSnapshot() RuntimeInfo {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	info := s.runtimeInfo
	info.LogLevel = util.LogLevelName()
	info.AuthEnabled = s.password != ""
	if info.GoVersion == "" {
		info.GoVersion = runtime.Version()
	}
	return info
}

// withBodyLimit 给所有 admin handler 套上 body 大小限制，避免恶意大 JSON 耗尽内存。
func withBodyLimit(next http.Handler, max int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, max)
		}
		next.ServeHTTP(w, r)
	})
}

// Start 启动 Admin API 服务器
func (s *Server) Start() error {
	log.Printf("admin api listening on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown 优雅关闭 Admin API 服务器。
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

// handleNodes GET 查看节点池状态 / POST 添加节点池
func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		s.getNodes(w)
	case "POST":
		s.addNodePool(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// getNodes 返回所有节点池
func (s *Server) getNodes(w http.ResponseWriter) {
	pools := s.poolManager.GetPoolStatus()
	writeJSON(w, http.StatusOK, map[string]any{
		"count": len(pools),
		"pools": pools,
	})
}

// addNodePool 添加节点池
func (s *Server) addNodePool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string           `json:"name"`
		Config *pool.PoolConfig `json:"config"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid request body: " + err.Error(),
		})
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "pool name is required",
		})
		return
	}

	if req.Config == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "pool config is required",
		})
		return
	}

	// 验证配置
	if err := validatePoolConfig(req.Config); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
		})
		return
	}

	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	nodesPath := filepath.Join(s.configDir, "nodes.yaml")
	config, err := loadNodesConfig(nodesPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to load nodes: " + err.Error(),
		})
		return
	}

	// 检查是否已存在
	if _, exists := config.NodePools[req.Name]; exists {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "pool already exists",
		})
		return
	}

	// 添加新节点池
	config.NodePools[req.Name] = req.Config

	// 保存到文件
	if err := s.saveNodesToFile(nodesPath, config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to save nodes: " + err.Error(),
		})
		return
	}

	// 重新加载节点管理器
	if err := s.poolManager.LoadFromFile(nodesPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to reload nodes: " + err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"message": "pool added successfully",
		"name":    req.Name,
	})
}

// handleNodePoolConfig PUT 更新节点池 / DELETE 删除节点池
func (s *Server) handleNodePoolConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "PUT":
		s.updateNodePool(w, r)
	case "DELETE":
		s.deleteNodePool(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// updateNodePool 更新节点池配置
func (s *Server) updateNodePool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string           `json:"name"`
		NewName string           `json:"new_name"`
		Config  *pool.PoolConfig `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid request body: " + err.Error(),
		})
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.NewName = strings.TrimSpace(req.NewName)
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "pool name is required",
		})
		return
	}
	if req.Config == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "pool config is required",
		})
		return
	}
	if err := validatePoolConfig(req.Config); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
		})
		return
	}

	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	nodesPath := filepath.Join(s.configDir, "nodes.yaml")
	config, err := loadNodesConfig(nodesPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to load nodes: " + err.Error(),
		})
		return
	}

	if _, exists := config.NodePools[req.Name]; !exists {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "pool not found",
		})
		return
	}

	targetName := req.Name
	if req.NewName != "" && req.NewName != req.Name {
		if _, exists := config.NodePools[req.NewName]; exists {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "pool name already exists",
			})
			return
		}
		delete(config.NodePools, req.Name)
		targetName = req.NewName
	}

	config.NodePools[targetName] = req.Config
	if err := s.saveAndReloadNodes(nodesPath, config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "pool updated successfully",
		"name":    targetName,
	})
}

// deleteNodePool 删除节点池配置
func (s *Server) deleteNodePool(w http.ResponseWriter, r *http.Request) {
	poolName := strings.TrimSpace(r.URL.Query().Get("name"))
	if poolName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "pool name is required",
		})
		return
	}

	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	nodesPath := filepath.Join(s.configDir, "nodes.yaml")
	config, err := loadNodesConfig(nodesPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to load nodes: " + err.Error(),
		})
		return
	}

	if _, exists := config.NodePools[poolName]; !exists {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "pool not found",
		})
		return
	}

	delete(config.NodePools, poolName)
	if err := s.saveAndReloadNodes(nodesPath, config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "pool deleted successfully",
		"name":    poolName,
	})
}

// handleNodeConfig PUT 更新静态节点 / DELETE 删除静态节点
func (s *Server) handleNodeConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "PUT":
		s.updateNode(w, r)
	case "DELETE":
		s.deleteNode(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleSubscriptionNodeConfig PUT 更新订阅节点元数据
func (s *Server) handleSubscriptionNodeConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Pool        string   `json:"pool"`
		Node        string   `json:"node"`
		Server      string   `json:"server"`
		Port        int      `json:"port"`
		Region      string   `json:"region"`
		Alias       string   `json:"alias"`
		Tags        []string `json:"tags"`
		Residential *bool    `json:"residential"`
		Premium     *bool    `json:"premium"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid request body: " + err.Error(),
		})
		return
	}

	req.Pool = strings.TrimSpace(req.Pool)
	req.Node = strings.TrimSpace(req.Node)
	req.Server = strings.TrimSpace(req.Server)
	req.Region = strings.ToUpper(strings.TrimSpace(req.Region))
	req.Alias = strings.TrimSpace(req.Alias)
	req.Tags = cleanStringSlice(req.Tags)
	if req.Residential != nil {
		req.Tags = setSpecialTag(req.Tags, "residential", *req.Residential)
	}
	if req.Premium != nil {
		req.Tags = setSpecialTag(req.Tags, "premium", *req.Premium)
	}
	if req.Pool == "" || req.Node == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "pool and node are required",
		})
		return
	}

	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	nodesPath := filepath.Join(s.configDir, "nodes.yaml")
	config, err := loadNodesConfig(nodesPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to load nodes: " + err.Error(),
		})
		return
	}

	poolCfg := config.NodePools[req.Pool]
	if poolCfg == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "pool not found",
		})
		return
	}
	if poolCfg.Source != "subscription" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "subscription node metadata is only supported for subscription pools",
		})
		return
	}

	serverKey := pool.ServerKey(req.Server, req.Port)
	if serverKey == "" {
		serverKey = s.findNodeServerKey(req.Pool, req.Node)
	}

	if req.Region == "" {
		delete(poolCfg.NodeRegions, req.Node)
		delete(poolCfg.NodeServerRegions, serverKey)
	} else {
		if poolCfg.NodeRegions == nil {
			poolCfg.NodeRegions = make(map[string]string)
		}
		poolCfg.NodeRegions[req.Node] = req.Region
		if serverKey != "" {
			if poolCfg.NodeServerRegions == nil {
				poolCfg.NodeServerRegions = make(map[string]string)
			}
			poolCfg.NodeServerRegions[serverKey] = req.Region
		}
	}

	if poolCfg.NodeAliases == nil {
		poolCfg.NodeAliases = make(map[string]string)
	}
	if poolCfg.NodeServerAliases == nil {
		poolCfg.NodeServerAliases = make(map[string]string)
	}
	if req.Alias != "" {
		if existing, exists := poolCfg.NodeAliases[req.Alias]; exists && existing != req.Node {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "alias already points to another node",
			})
			return
		}
		for key, alias := range poolCfg.NodeServerAliases {
			if alias == req.Alias && key != serverKey {
				writeJSON(w, http.StatusConflict, map[string]any{
					"error": "alias already points to another server",
				})
				return
			}
		}
	}
	for alias, target := range poolCfg.NodeAliases {
		if target == req.Node {
			delete(poolCfg.NodeAliases, alias)
		}
	}
	if serverKey != "" {
		delete(poolCfg.NodeServerAliases, serverKey)
	}
	if req.Alias != "" {
		poolCfg.NodeAliases[req.Alias] = req.Node
		if serverKey != "" {
			poolCfg.NodeServerAliases[serverKey] = req.Alias
		}
	}

	if len(req.Tags) == 0 {
		delete(poolCfg.NodeTags, req.Node)
		delete(poolCfg.NodeServerTags, serverKey)
	} else {
		if poolCfg.NodeTags == nil {
			poolCfg.NodeTags = make(map[string][]string)
		}
		poolCfg.NodeTags[req.Node] = req.Tags
		if serverKey != "" {
			if poolCfg.NodeServerTags == nil {
				poolCfg.NodeServerTags = make(map[string][]string)
			}
			poolCfg.NodeServerTags[serverKey] = req.Tags
		}
	}

	if err := s.saveAndReloadNodes(nodesPath, config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":     "subscription node metadata updated",
		"pool":        req.Pool,
		"node":        req.Node,
		"region":      req.Region,
		"alias":       req.Alias,
		"tags":        req.Tags,
		"residential": util.ContainsString(req.Tags, "residential"),
		"premium":     util.ContainsString(req.Tags, "premium"),
		"server_key":  serverKey,
	})
}

func (s *Server) findNodeServerKey(poolName string, nodeName string) string {
	for _, ps := range s.poolManager.GetPoolStatus() {
		if ps.Name != poolName {
			continue
		}
		for _, node := range ps.Nodes {
			if node.Name == nodeName {
				return pool.ServerKey(node.Server, node.Port)
			}
		}
	}
	return ""
}

func cleanStringSlice(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func setSpecialTag(tags []string, tag string, enabled bool) []string {
	tag = strings.TrimSpace(strings.ToLower(tag))
	if tag == "" {
		return tags
	}
	result := make([]string, 0, len(tags)+1)
	found := false
	for _, value := range tags {
		if strings.EqualFold(strings.TrimSpace(value), tag) {
			found = true
			if enabled {
				result = append(result, tag)
			}
			continue
		}
		result = append(result, value)
	}
	if enabled && !found {
		result = append(result, tag)
	}
	return cleanStringSlice(result)
}

// updateNode 更新 static 节点池中的单个节点
func (s *Server) updateNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pool        string       `json:"pool"`
		Node        string       `json:"node"`
		UpdatedNode *dialer.Node `json:"updated_node"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid request body: " + err.Error(),
		})
		return
	}

	req.Pool = strings.TrimSpace(req.Pool)
	req.Node = strings.TrimSpace(req.Node)
	if req.Pool == "" || req.Node == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "pool and node are required",
		})
		return
	}
	if req.UpdatedNode == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "updated_node is required",
		})
		return
	}
	if err := validateNodeConfig(req.UpdatedNode); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
		})
		return
	}

	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	nodesPath := filepath.Join(s.configDir, "nodes.yaml")
	config, err := loadNodesConfig(nodesPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to load nodes: " + err.Error(),
		})
		return
	}

	poolCfg := config.NodePools[req.Pool]
	if poolCfg == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "pool not found",
		})
		return
	}
	if poolCfg.Source != "static" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "node editing is only supported for static pools",
		})
		return
	}

	for _, existing := range poolCfg.Nodes {
		if existing.Name == req.UpdatedNode.Name && existing.Name != req.Node {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "node name already exists",
			})
			return
		}
	}

	found := false
	for i := range poolCfg.Nodes {
		if poolCfg.Nodes[i].Name == req.Node {
			poolCfg.Nodes[i] = *req.UpdatedNode
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "node not found",
		})
		return
	}
	if req.Node != req.UpdatedNode.Name {
		poolCfg.DisabledNodes = util.RemoveString(poolCfg.DisabledNodes, req.Node)
	}

	if err := s.saveAndReloadNodes(nodesPath, config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "node updated successfully",
		"pool":    req.Pool,
		"node":    req.UpdatedNode.Name,
	})
}

// deleteNode 删除 static 节点池中的单个节点
func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request) {
	poolName := strings.TrimSpace(r.URL.Query().Get("pool"))
	nodeName := strings.TrimSpace(r.URL.Query().Get("node"))
	if poolName == "" || nodeName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "pool and node are required",
		})
		return
	}

	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	nodesPath := filepath.Join(s.configDir, "nodes.yaml")
	config, err := loadNodesConfig(nodesPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to load nodes: " + err.Error(),
		})
		return
	}

	poolCfg := config.NodePools[poolName]
	if poolCfg == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "pool not found",
		})
		return
	}
	if poolCfg.Source != "static" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "node deletion is only supported for static pools",
		})
		return
	}

	nodes := make([]dialer.Node, 0, len(poolCfg.Nodes))
	found := false
	for _, node := range poolCfg.Nodes {
		if node.Name == nodeName {
			found = true
			continue
		}
		nodes = append(nodes, node)
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "node not found",
		})
		return
	}

	poolCfg.Nodes = nodes
	poolCfg.DisabledNodes = util.RemoveString(poolCfg.DisabledNodes, nodeName)
	if err := s.saveAndReloadNodes(nodesPath, config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "node deleted successfully",
		"pool":    poolName,
		"node":    nodeName,
	})
}

// handleNodesRefresh POST 刷新订阅池
func (s *Server) handleNodesRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	poolName := r.URL.Query().Get("pool")
	if poolName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "pool parameter required",
		})
		return
	}

	if err := s.poolManager.RefreshSubscription(poolName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": err.Error(),
		})
		return
	}

	log.Printf("admin: pool %s refreshed", poolName)
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "pool refreshed",
		"pool":    poolName,
	})
}

// handleReload POST 热重载全部配置
func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var errors []string

	// 重载指纹库
	if s.fpManager != nil {
		fpPath := s.configDir + "/fingerprints.yaml"
		if _, err := os.Stat(fpPath); stderrors.Is(err, os.ErrNotExist) {
			log.Printf("admin: fingerprints config not found, skipping: %s", fpPath)
		} else if err := s.fpManager.LoadFromFile(fpPath); err != nil {
			errors = append(errors, "fingerprints: "+err.Error())
		}
	}
	if s.poolManager != nil {
		nodesPath := filepath.Join(s.configDir, "nodes.yaml")
		if err := s.poolManager.LoadFromFile(nodesPath); err != nil {
			errors = append(errors, "nodes: "+err.Error())
		} else {
			s.poolManager.StartSubscriptionUpdater()
		}
	}
	if s.healthChecker != nil {
		defaults, err := s.loadHealthCheckSettings()
		if err != nil {
			errors = append(errors, "health_check: "+err.Error())
		} else {
			s.healthChecker.UpdateDefaults(defaults)
		}
	}
	if s.proxyRuntime != nil {
		settings, err := s.loadProxySettings()
		if err != nil {
			errors = append(errors, "proxy: "+err.Error())
		} else {
			s.proxyRuntime.UpdateProxySettings(settings)
		}
	}
	if s.traffic != nil {
		trafficCfg, err := s.loadTrafficConfig()
		if err != nil {
			errors = append(errors, "traffic: "+err.Error())
		} else {
			s.traffic.UpdateConfig(trafficCfg)
		}
	}

	if len(errors) > 0 {
		log.Printf("admin: reload partial failure: %v", errors)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"message": "reload completed with errors",
			"errors":  errors,
		})
		return
	}

	log.Printf("admin: all configs reloaded")
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "all configs reloaded",
	})
}

// handleStats GET 基础统计信息
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	uptime := time.Since(s.startTime)

	writeJSON(w, http.StatusOK, map[string]any{
		"uptime":       uptime.String(),
		"uptime_sec":   int(uptime.Seconds()),
		"pools_count":  len(s.poolManager.ListPools()),
		"fingerprints": len(s.fpManager.List()),
		"traffic":      s.traffic.Metrics(),
		"runtime":      s.runtimeSnapshot(),
		"health_check": s.healthCheckSettingsSnapshot(),
		"proxy":        s.proxySettingsSnapshot(),
	})
}

func (s *Server) handleLogging(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, http.StatusOK, s.runtimeSnapshot())
	case "PUT":
		var req struct {
			Level string `json:"level"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
			return
		}
		util.SetLogLevel(req.Level)
		if err := s.persistLogLevel(util.LogLevelName()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		info := s.runtimeSnapshot()
		writeJSON(w, http.StatusOK, info)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) persistLogLevel(level string) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	path := filepath.Join(s.configDir, "gateway.yaml")
	var cfg map[string]any
	if err := loadYAML(path, &cfg); err != nil {
		return err
	}
	logCfg, ok := cfg["log"].(map[string]any)
	if !ok || logCfg == nil {
		logCfg = map[string]any{}
		cfg["log"] = logCfg
	}
	logCfg["level"] = level
	if _, exists := logCfg["file"]; !exists {
		logCfg["file"] = ""
	}
	if err := atomicWriteYAML(path, cfg); err != nil {
		return err
	}

	s.runtimeMu.Lock()
	info := s.runtimeInfo
	info.LogLevel = level
	s.runtimeInfo = info
	s.runtimeMu.Unlock()
	return nil
}

func (s *Server) handleHealthCheckSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, http.StatusOK, s.healthCheckSettingsSnapshot())
	case "PUT":
		var req pool.HealthCheckConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
			return
		}
		defaults, err := pool.ParseHealthCheckDefaults(&req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		cfg := pool.HealthCheckDefaultsConfig(defaults)
		if err := s.persistHealthCheckSettings(cfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if s.healthChecker != nil {
			s.healthChecker.UpdateDefaults(defaults)
		}
		writeJSON(w, http.StatusOK, s.healthCheckSettingsSnapshot())
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) healthCheckSettingsSnapshot() map[string]any {
	defaults := pool.DefaultHealthCheckDefaults()
	if s.healthChecker != nil {
		defaults = s.healthChecker.Defaults()
	}
	cfg := pool.HealthCheckDefaultsConfig(defaults)
	return map[string]any{
		"interval": cfg.Interval,
		"timeout":  cfg.Timeout,
		"url":      cfg.URL,
		"max_fail": cfg.MaxFail,
	}
}

func (s *Server) persistHealthCheckSettings(healthCfg *pool.HealthCheckConfig) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	path := filepath.Join(s.configDir, "gateway.yaml")
	var cfg map[string]any
	if err := loadYAML(path, &cfg); err != nil {
		return err
	}
	cfg["health_check"] = map[string]any{
		"interval": healthCfg.Interval,
		"timeout":  healthCfg.Timeout,
		"url":      healthCfg.URL,
		"max_fail": healthCfg.MaxFail,
	}
	if err := atomicWriteYAML(path, cfg); err != nil {
		return err
	}
	return nil
}

func (s *Server) loadHealthCheckSettings() (pool.HealthCheckDefaults, error) {
	path := filepath.Join(s.configDir, "gateway.yaml")
	var cfg struct {
		HealthCheck *pool.HealthCheckConfig `yaml:"health_check"`
	}
	if err := loadYAML(path, &cfg); err != nil {
		return pool.DefaultHealthCheckDefaults(), err
	}
	return pool.ParseHealthCheckDefaults(cfg.HealthCheck)
}

func (s *Server) handleProxySettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, http.StatusOK, s.proxySettingsSnapshot())
	case "PUT":
		var req proxyserver.ProxySettingsConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
			return
		}
		settings, err := proxyserver.ParseProxySettings(&req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if err := s.persistProxySettings(settings); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if s.proxyRuntime != nil {
			s.proxyRuntime.UpdateProxySettings(settings)
		}
		writeJSON(w, http.StatusOK, s.proxySettingsSnapshot())
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) proxySettingsSnapshot() map[string]any {
	settings := proxyserver.DefaultProxySettings()
	if s.proxyRuntime != nil {
		settings = s.proxyRuntime.ProxySettings()
	} else if loaded, err := s.loadProxySettings(); err == nil {
		settings = loaded
	}
	return map[string]any{
		"max_attempts":                     settings.MaxAttempts,
		"max_redirects":                    settings.MaxRedirects,
		"template_fallback_after_attempts": settings.TemplateFallbackAfterAttempts,
		"response_header_timeout":          settings.ResponseHeaderTimeout.String(),
		"total_timeout":                    settings.TotalTimeout.String(),
	}
}

func (s *Server) persistProxySettings(settings proxyserver.ProxySettings) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	path := filepath.Join(s.configDir, "gateway.yaml")
	var cfg map[string]any
	if err := loadYAML(path, &cfg); err != nil {
		return err
	}
	cfg["proxy"] = map[string]any{
		"max_attempts":                     settings.MaxAttempts,
		"max_redirects":                    settings.MaxRedirects,
		"template_fallback_after_attempts": settings.TemplateFallbackAfterAttempts,
		"response_header_timeout":          settings.ResponseHeaderTimeout.String(),
		"total_timeout":                    settings.TotalTimeout.String(),
	}
	return atomicWriteYAML(path, cfg)
}

func (s *Server) loadProxySettings() (proxyserver.ProxySettings, error) {
	path := filepath.Join(s.configDir, "gateway.yaml")
	var cfg struct {
		Proxy *proxyserver.ProxySettingsConfig `yaml:"proxy"`
	}
	if err := loadYAML(path, &cfg); err != nil {
		return proxyserver.DefaultProxySettings(), err
	}
	return proxyserver.ParseProxySettings(cfg.Proxy)
}

func (s *Server) handleConfigExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	readText := func(name string) string {
		data, err := os.ReadFile(filepath.Join(s.configDir, name))
		if err != nil {
			return ""
		}
		return string(data)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"exported_at":       time.Now().UTC().Format(time.RFC3339),
		"gateway_yaml":      readText("gateway.yaml"),
		"nodes_yaml":        readText("nodes.yaml"),
		"fingerprints_yaml": readText("fingerprints.yaml"),
	})
}

// writeJSON 写入 JSON 响应
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// handleNodeTest POST 测试节点连通性
func (s *Server) handleNodeTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Pool string `json:"pool"`
		Node string `json:"node"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid request body",
		})
		return
	}

	if req.Pool == "" || req.Node == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "pool and node are required",
		})
		return
	}

	result, err := s.poolManager.TestNodeConnectivity(req.Pool, req.Node, "", 0)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleTemplateTest POST 即时测试模板池指定地区的连通性
func (s *Server) handleTemplateTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Pool   string `json:"pool"`
		Region string `json:"region"`
		URL    string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid request body",
		})
		return
	}
	req.Pool = strings.TrimSpace(req.Pool)
	req.Region = strings.ToUpper(strings.TrimSpace(req.Region))
	if req.Pool == "" || req.Region == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "pool and region are required",
		})
		return
	}

	result, err := s.poolManager.TestTemplateConnectivity(req.Pool, req.Region, strings.TrimSpace(req.URL), 0)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleNodeEnabled POST 持久化更新节点启停状态
func (s *Server) handleNodeEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Pool    string `json:"pool"`
		Node    string `json:"node"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid request body",
		})
		return
	}
	if req.Pool == "" || req.Node == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "pool and node are required",
		})
		return
	}

	if err := s.poolManager.SetNodeEnabled(req.Pool, req.Node, req.Enabled); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": err.Error(),
		})
		return
	}

	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	nodesPath := filepath.Join(s.configDir, "nodes.yaml")
	config, err := loadNodesConfig(nodesPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to load nodes: " + err.Error(),
		})
		return
	}

	poolCfg := config.NodePools[req.Pool]
	if poolCfg == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "pool not found",
		})
		return
	}
	updatePoolConfigNodeEnabled(poolCfg, req.Node, req.Enabled)

	if err := s.saveNodesToFile(nodesPath, config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to save nodes: " + err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "node state updated",
		"pool":    req.Pool,
		"node":    req.Node,
		"enabled": req.Enabled,
	})
}

func updatePoolConfigNodeEnabled(poolCfg *pool.PoolConfig, nodeName string, enabled bool) {
	for i := range poolCfg.Nodes {
		if poolCfg.Nodes[i].Name == nodeName {
			value := enabled
			poolCfg.Nodes[i].Enabled = &value
			return
		}
	}

	if enabled {
		poolCfg.DisabledNodes = util.RemoveString(poolCfg.DisabledNodes, nodeName)
	} else if !util.ContainsString(poolCfg.DisabledNodes, nodeName) {
		poolCfg.DisabledNodes = append(poolCfg.DisabledNodes, nodeName)
	}
}

func loadNodesConfig(path string) (*pool.NodesFileConfig, error) {
	var config pool.NodesFileConfig
	if err := loadYAML(path, &config); err != nil {
		return nil, err
	}
	if config.NodePools == nil {
		config.NodePools = make(map[string]*pool.PoolConfig)
	}
	return &config, nil
}

func (s *Server) saveAndReloadNodes(path string, config *pool.NodesFileConfig) error {
	if err := s.saveNodesToFile(path, config); err != nil {
		return fmt.Errorf("failed to save nodes: %w", err)
	}
	if err := s.poolManager.LoadFromFile(path); err != nil {
		return fmt.Errorf("failed to reload nodes: %w", err)
	}
	s.poolManager.StartSubscriptionUpdater()
	return nil
}

func validatePoolConfig(config *pool.PoolConfig) error {
	config.Source = strings.TrimSpace(config.Source)
	for _, pattern := range config.RejectRegex {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("invalid reject regex %q: %w", pattern, err)
		}
	}
	switch config.Source {
	case "direct":
		return nil
	case "subscription":
		if strings.TrimSpace(config.URL) == "" {
			return fmt.Errorf("subscription url is required")
		}
		config.UpdateInterval = strings.TrimSpace(config.UpdateInterval)
		if config.UpdateInterval != "" {
			if _, err := pool.ParseDurationWithDays(config.UpdateInterval); err != nil {
				return fmt.Errorf("invalid update_interval: %w", err)
			}
		}
		return nil
	case "template":
		config.TemplateType = pool.NormalizeTemplateType(config.TemplateType)
		switch config.TemplateType {
		case "chijie":
			if _, err := pool.ChijieProxyURL(config.Endpoint, config.Port); err != nil {
				return fmt.Errorf("invalid chijie template endpoint: %w", err)
			}
			if strings.TrimSpace(config.BearerToken) == "" {
				return fmt.Errorf("chijie template bearer_token is required")
			}
		case "proxy":
			node := &dialer.Node{
				Name:   "template",
				Type:   config.Type,
				Server: config.Server,
				Port:   config.Port,
			}
			if err := validateNodeConfig(node); err != nil {
				return fmt.Errorf("invalid template config: %w", err)
			}
		default:
			return fmt.Errorf("unsupported template_type: %s", config.TemplateType)
		}
		config.Coverage = pool.NormalizeTemplateCoverage(config.Coverage, config.Residential, config.TemplateType)
		switch config.Coverage {
		case "normal", "residential", "both":
		default:
			return fmt.Errorf("invalid coverage: %s", config.Coverage)
		}
		return nil
	case "static":
		seen := make(map[string]struct{}, len(config.Nodes))
		for i := range config.Nodes {
			if err := validateNodeConfig(&config.Nodes[i]); err != nil {
				return fmt.Errorf("invalid static node %d: %w", i+1, err)
			}
			if _, exists := seen[config.Nodes[i].Name]; exists {
				return fmt.Errorf("duplicate node name: %s", config.Nodes[i].Name)
			}
			seen[config.Nodes[i].Name] = struct{}{}
		}
		return nil
	default:
		return fmt.Errorf("pool source is required (static/subscription/template/direct)")
	}
}

func validateNodeConfig(node *dialer.Node) error {
	node.Name = strings.TrimSpace(node.Name)
	node.Type = strings.TrimSpace(node.Type)
	node.Server = strings.TrimSpace(node.Server)

	if node.Name == "" {
		return fmt.Errorf("node name is required")
	}
	if !supportedNodeType(node.Type) {
		return fmt.Errorf("unsupported node type: %s", node.Type)
	}
	if node.Type == "direct" {
		return nil
	}
	if node.Server == "" {
		return fmt.Errorf("node server is required")
	}
	if node.Port <= 0 || node.Port > 65535 {
		return fmt.Errorf("node port must be between 1 and 65535")
	}
	return nil
}

func supportedNodeType(nodeType string) bool {
	switch nodeType {
	case "direct", "socks5", "http_proxy", "http", "ss", "shadowsocks", "vmess", "trojan", "vless", "hysteria2", "hy2":
		return true
	default:
		return false
	}
}

// saveNodesToFile 保存节点配置到 YAML 文件（保留兼容签名）。
func (s *Server) saveNodesToFile(path string, config *pool.NodesFileConfig) error {
	return atomicWriteYAML(path, config)
}

// handleFingerprints GET 查看指纹列表 / POST 添加指纹
func (s *Server) handleFingerprints(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		s.getFingerprints(w)
	case "POST":
		s.addFingerprint(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// getFingerprints 返回所有指纹
func (s *Server) getFingerprints(w http.ResponseWriter) {
	fps := s.fpManager.List()

	type fingerprintView struct {
		Name   string                         `json:"name"`
		Type   string                         `json:"type"`
		Config *fingerprint.FingerprintConfig `json:"config,omitempty"`
		Preset string                         `json:"preset,omitempty"`
		JA3    string                         `json:"ja3,omitempty"`
		JA4    string                         `json:"ja4,omitempty"`
		Akamai string                         `json:"akamai,omitempty"`
	}

	result := make([]fingerprintView, 0, len(fps))
	for _, name := range fps {
		spec, ok := s.fpManager.Get(name)
		if !ok {
			continue
		}

		view := fingerprintView{
			Name:   name,
			Config: spec.Canonical(),
		}

		// 判断是预设还是自定义
		if spec.Preset != "" {
			view.Type = "preset"
			view.Preset = spec.Preset
		} else {
			view.Type = "custom"
			view.JA3 = spec.JA3
		}
		view.JA4 = spec.JA4
		view.Akamai = spec.Akamai

		result = append(result, view)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":        len(result),
		"fingerprints": result,
	})
}

// addFingerprint 添加指纹
func (s *Server) addFingerprint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string                         `json:"name"`
		Config     *fingerprint.FingerprintConfig `json:"config"`
		ConfigText string                         `json:"config_text"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid request body: " + err.Error(),
		})
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "fingerprint name is required",
		})
		return
	}

	if req.Config == nil && strings.TrimSpace(req.ConfigText) != "" {
		parsed, err := fingerprint.ParseConfigText(req.ConfigText)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "failed to parse fingerprint config: " + err.Error(),
			})
			return
		}
		req.Config = parsed
	}

	if req.Config == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "fingerprint config is required",
		})
		return
	}

	req.Config = req.Config.Canonical()

	// 验证配置：必须有 preset、ja3 或 ja4 raw。Akamai/HTTP2 raw 是补充字段。
	if req.Config.Preset == "" && req.Config.JA3 == "" && req.Config.JA4 == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "fingerprint must have preset, ja3, or ja4 raw",
		})
		return
	}

	if _, _, err := fingerprint.BuildSpecFromConfig(req.Config); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid fingerprint config: " + err.Error(),
		})
		return
	}

	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	// 读取现有指纹配置
	fpPath := filepath.Join(s.configDir, "fingerprints.yaml")
	config, err := loadFingerprintsConfig(fpPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to load fingerprints: " + err.Error(),
		})
		return
	}

	// 检查是否已存在
	if _, exists := config.Fingerprints[req.Name]; exists {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "fingerprint already exists",
		})
		return
	}

	// 添加新指纹
	config.Fingerprints[req.Name] = req.Config

	// 保存到文件
	if err := s.saveFingerprintsToFile(fpPath, config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to save fingerprints: " + err.Error(),
		})
		return
	}

	// 重新加载指纹管理器
	if err := s.fpManager.LoadFromFile(fpPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to reload fingerprints: " + err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"message": "fingerprint added successfully",
		"name":    req.Name,
	})
}

// handleFingerprintByName DELETE 删除指纹
func (s *Server) handleFingerprintByName(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// 提取指纹名称
	path := strings.TrimPrefix(r.URL.Path, "/api/fingerprints/")
	if path == "" || path == "test" {
		http.Error(w, `{"error":"fingerprint name required"}`, http.StatusBadRequest)
		return
	}
	fpName := path

	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	// 读取现有指纹配置
	fpPath := filepath.Join(s.configDir, "fingerprints.yaml")
	config, err := loadFingerprintsConfig(fpPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to load fingerprints: " + err.Error(),
		})
		return
	}

	// 检查指纹是否存在
	if _, exists := config.Fingerprints[fpName]; !exists {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "fingerprint not found",
		})
		return
	}

	// 删除指纹
	delete(config.Fingerprints, fpName)

	// 保存到文件
	if err := s.saveFingerprintsToFile(fpPath, config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to save fingerprints: " + err.Error(),
		})
		return
	}

	// 重新加载指纹管理器
	if err := s.fpManager.LoadFromFile(fpPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to reload fingerprints: " + err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "fingerprint deleted successfully",
	})
}

// saveFingerprintsToFile 保存指纹到 YAML 文件（保留兼容签名）。
func (s *Server) saveFingerprintsToFile(path string, config *fingerprint.FileConfig) error {
	return atomicWriteYAML(path, config)
}

func loadFingerprintsConfig(path string) (*fingerprint.FileConfig, error) {
	var config fingerprint.FileConfig
	if err := loadYAML(path, &config); err != nil {
		if !stderrors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if config.Fingerprints == nil {
		config.Fingerprints = make(map[string]*fingerprint.FingerprintConfig)
	}
	return &config, nil
}

// handleFingerprintTest POST 测试指纹
func (s *Server) handleFingerprintTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Fingerprint string `json:"fingerprint"`
		URL         string `json:"url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid request body",
		})
		return
	}

	if req.Fingerprint == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "fingerprint name is required",
		})
		return
	}

	if req.URL == "" {
		req.URL = "https://tls.browserleaks.com/json"
	}
	targetURL, err := url.Parse(req.URL)
	if err != nil || targetURL.Scheme != "https" || targetURL.Host == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "target URL must be an absolute https URL",
		})
		return
	}
	if guardErr := netguard.CheckHost(r.Context(), targetURL.Hostname()); guardErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "target host is not allowed: " + guardErr.Error(),
		})
		return
	}

	// 检查指纹是否存在
	spec, ok := s.fpManager.Get(req.Fingerprint)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "fingerprint not found",
		})
		return
	}

	// 构建 TLS 配置
	helloID, helloSpec, err := s.fpManager.BuildSpec(req.Fingerprint)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to build fingerprint spec: " + err.Error(),
		})
		return
	}
	fingerprintInfo := map[string]any{}
	if helloSpec != nil {
		fingerprintInfo["type"] = "custom"
	} else if helloID != nil {
		fingerprintInfo["type"] = "preset"
		fingerprintInfo["preset"] = spec.Preset
	}
	if spec.WantsHTTP2() {
		fingerprintInfo["http_version"] = "h2"
	}

	transport := &http.Transport{
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	var roundTripper http.RoundTripper = transport
	if spec.WantsHTTP2() {
		roundTripper = fingerprint.NewHTTP2RoundTripper(helloID, helloSpec, targetURL.Hostname(), nil, spec)
	} else {
		fingerprint.WrapTransport(transport, helloID, helloSpec, targetURL.Hostname())
	}
	client := &http.Client{
		Transport: roundTripper,
		Timeout:   20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	started := time.Now()
	method := spec.EffectiveMethod(http.MethodGet)
	httpReq, err := http.NewRequestWithContext(r.Context(), method, targetURL.String(), nil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "failed to create test request: " + err.Error(),
		})
		return
	}
	httpReq.Header.Set("Accept", "application/json,text/plain,*/*")
	spec.ApplyRequestDefaults(httpReq)
	if httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", "Chijie-TLS-Probe/1.0")
	}

	resp, err := client.Do(httpReq)
	latency := time.Since(started)
	if err != nil {
		result := map[string]any{
			"fingerprint": req.Fingerprint,
			"url":         targetURL.String(),
			"status":      "failed",
			"latency_ms":  latency.Milliseconds(),
			"error":       err.Error(),
		}
		for key, value := range fingerprintInfo {
			result[key] = value
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	defer resp.Body.Close()

	body, readErr := readFingerprintTestBody(resp, 128*1024)
	if readErr != nil {
		result := map[string]any{
			"fingerprint":  req.Fingerprint,
			"url":          targetURL.String(),
			"status":       "failed",
			"http_status":  resp.StatusCode,
			"latency_ms":   latency.Milliseconds(),
			"content_type": resp.Header.Get("Content-Type"),
			"error":        "read response: " + readErr.Error(),
		}
		for key, value := range fingerprintInfo {
			result[key] = value
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	result := map[string]any{
		"fingerprint": req.Fingerprint,
		"url":         targetURL.String(),
		"status":      "ok",
		"http_status": resp.StatusCode,
		"latency_ms":  latency.Milliseconds(),
		"body_bytes":  len(body),
		"http_proto":  resp.Proto,
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		result["content_type"] = contentType
	}
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" {
		result["content_encoding"] = encoding
	}
	for key, value := range fingerprintInfo {
		result[key] = value
	}

	var observed map[string]any
	if err := json.Unmarshal(body, &observed); err == nil {
		result["observed"] = observed
	} else if len(body) > 0 {
		sample := string(body)
		if len(sample) > 4096 {
			sample = sample[:4096]
		}
		result["body_sample"] = sample
	}

	writeJSON(w, http.StatusOK, result)
}

func readFingerprintTestBody(resp *http.Response, limit int64) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}

	reader, closeFn, err := decodedBodyReader(resp)
	if err != nil {
		return nil, err
	}
	if closeFn != nil {
		defer closeFn()
	}
	return io.ReadAll(io.LimitReader(reader, limit))
}

func decodedBodyReader(resp *http.Response) (io.Reader, func(), error) {
	encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	encoding = strings.Split(encoding, ",")[0]
	switch encoding {
	case "", "identity":
		return resp.Body, nil, nil
	case "gzip":
		reader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("decode gzip response: %w", err)
		}
		return reader, func() { reader.Close() }, nil
	case "deflate":
		reader, err := zlib.NewReader(resp.Body)
		if err == nil {
			return reader, func() { reader.Close() }, nil
		}
		flateReader := flate.NewReader(resp.Body)
		return flateReader, func() { flateReader.Close() }, nil
	case "br":
		return brotli.NewReader(resp.Body), nil, nil
	case "zstd":
		reader, err := zstd.NewReader(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("decode zstd response: %w", err)
		}
		return reader, func() { reader.Close() }, nil
	default:
		return resp.Body, nil, nil
	}
}

// LoginRequest 登录请求
type LoginRequest struct {
	Password string `json:"password"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
}

// ProxyTokenRequest 代理调用 token 生成请求
type ProxyTokenRequest struct {
	Name     string `json:"name"`
	Duration string `json:"duration"`
}

// ProxyTokenResponse 代理调用 token 生成响应
type ProxyTokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
	ExpiresAt string `json:"expires_at"`
	Name      string `json:"name"`
}

// handleLogin 处理登录请求
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	clientIP := clientIPFromRequest(r)
	if s.loginLimiter != nil {
		if allowed, retryAfter := s.loginLimiter.allow(clientIP); !allowed {
			seconds := int(retryAfter.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error":       "too many failed attempts, try again later",
				"retry_after": seconds,
			})
			return
		}
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid request body",
		})
		return
	}

	// 如果未配置密码，拒绝登录
	if s.password == "" {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "authentication not configured",
		})
		return
	}

	// 常量时间比较，防止时序侧信道
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(s.password)) != 1 {
		if s.loginLimiter != nil {
			s.loginLimiter.recordFailure(clientIP)
		}
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "invalid password",
		})
		return
	}

	if s.loginLimiter != nil {
		s.loginLimiter.recordSuccess(clientIP)
	}

	// 生成 JWT token
	now := time.Now()
	claims := &auth.Claims{
		Admin: true,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.jwtExpire)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to generate token",
		})
		return
	}

	writeJSON(w, http.StatusOK, LoginResponse{
		Token:     tokenString,
		ExpiresIn: int64(s.jwtExpire.Seconds()),
	})
}

// clientIPFromRequest 从请求中提取客户端 IP。
// Cloudflare 部署优先使用 CF-Connecting-IP / True-Client-IP；其后兼容常见反代 Header，最后回退到 RemoteAddr。
func clientIPFromRequest(r *http.Request) string {
	if cfIP := firstHeaderIP(r.Header.Get("CF-Connecting-IP")); cfIP != "" {
		return cfIP
	}
	if trueClientIP := firstHeaderIP(r.Header.Get("True-Client-IP")); trueClientIP != "" {
		return trueClientIP
	}
	if xff := firstHeaderIP(r.Header.Get("X-Forwarded-For")); xff != "" {
		return xff
	}
	if real := firstHeaderIP(r.Header.Get("X-Real-IP")); real != "" {
		return real
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func firstHeaderIP(value string) string {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if host, _, err := net.SplitHostPort(part); err == nil {
			part = host
		}
		part = strings.TrimPrefix(strings.TrimSuffix(part, "]"), "[")
		if ip := net.ParseIP(part); ip != nil {
			return ip.String()
		}
	}
	return ""
}

// handleProxyToken 生成只用于代理调用的 Bearer token。
func (s *Server) handleProxyToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req ProxyTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid request body",
		})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "proxy-api"
	}
	duration, err := parseTokenDuration(req.Duration, s.jwtExpire)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": err.Error(),
		})
		return
	}

	now := time.Now()
	expiresAt := now.Add(duration)
	claims := &auth.Claims{
		Proxy: true,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   name,
			Audience:  []string{"proxy"},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "failed to generate token",
		})
		return
	}

	writeJSON(w, http.StatusOK, ProxyTokenResponse{
		Token:     tokenString,
		ExpiresIn: int64(duration.Seconds()),
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
		Name:      name,
	})
}

func parseTokenDuration(raw string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return fallback, nil
	}
	var duration time.Duration
	var err error
	if strings.HasSuffix(value, "d") {
		days, parseErr := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if parseErr != nil {
			return 0, fmt.Errorf("invalid token duration")
		}
		duration = time.Duration(days) * 24 * time.Hour
	} else {
		duration, err = time.ParseDuration(value)
		if err != nil {
			return 0, fmt.Errorf("invalid token duration")
		}
	}
	if duration <= 0 {
		return 0, fmt.Errorf("token duration must be positive")
	}
	if duration > 365*24*time.Hour {
		return 0, fmt.Errorf("token duration must be 365d or less")
	}
	return duration, nil
}

// authMiddleware JWT 认证中间件
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 如果未配置密码，直接放行
		if s.password == "" {
			next(w, r)
			return
		}

		// 提取 Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": "missing authorization header",
			})
			return
		}

		// 验证 Bearer token
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": "invalid authorization header format",
			})
			return
		}

		tokenString := parts[1]

		// 解析 JWT
		token, err := jwt.ParseWithClaims(tokenString, &auth.Claims{}, func(token *jwt.Token) (interface{}, error) {
			// 验证签名方法
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(s.jwtSecret), nil
		})

		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": "invalid token: " + err.Error(),
			})
			return
		}

		// 验证 claims
		if claims, ok := token.Claims.(*auth.Claims); ok && token.Valid {
			if !claims.Admin {
				writeJSON(w, http.StatusForbidden, map[string]any{
					"error": "insufficient permissions",
				})
				return
			}
			// 验证通过，继续处理
			next(w, r)
		} else {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": "invalid token claims",
			})
		}
	}
}
