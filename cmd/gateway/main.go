package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"chijie/internal/admin"
	"chijie/internal/fingerprint"
	"chijie/internal/pool"
	"chijie/internal/server"
	"chijie/internal/traffic"
	"chijie/internal/util"

	"gopkg.in/yaml.v3"
)

// GatewayConfig gateway.yaml 顶层结构
type GatewayConfig struct {
	Server struct {
		Listen              string `yaml:"listen"`
		AllowPrivateTargets bool   `yaml:"allow_private_targets"`
		TLS                 struct {
			Cert string `yaml:"cert"`
			Key  string `yaml:"key"`
		} `yaml:"tls"`
	} `yaml:"server"`
	Admin struct {
		Listen           string `yaml:"listen"`
		Password         string `yaml:"password"`
		JWTSecret        string `yaml:"jwt_secret"`
		JWTExpire        string `yaml:"jwt_expire"`
		LoginMaxFailures int    `yaml:"login_max_failures"`
		LoginWindow      string `yaml:"login_window"`
		LoginLockout     string `yaml:"login_lockout"`
	} `yaml:"admin"`
	HealthCheck pool.HealthCheckConfig      `yaml:"health_check"`
	Proxy       *server.ProxySettingsConfig `yaml:"proxy"`
	Traffic     traffic.Config              `yaml:"traffic"`
	Log         struct {
		Level string `yaml:"level"`
		File  string `yaml:"file"`
	} `yaml:"log"`
}

// 已知的占位 jwt_secret 字面量，禁止直接使用。
var placeholderJWTSecrets = map[string]struct{}{
	"": {},
	"your_jwt_secret_key_change_this_in_production": {},
	"change_me":     {},
	"changeme":      {},
	"secret":        {},
	"jwt_secret":    {},
	"please_change": {},
}

func main() {
	configDir := flag.String("config", "./configs", "配置文件目录")
	flag.Parse()

	// 加载主配置
	gatewayConfig, err := loadGatewayConfig(*configDir + "/gateway.yaml")
	if err != nil {
		log.Fatalf("load gateway config: %v", err)
	}
	if err := validateGatewayConfig(gatewayConfig); err != nil {
		log.Fatalf("invalid gateway config: %v", err)
	}
	proxySettings, err := server.ParseProxySettings(gatewayConfig.Proxy)
	if err != nil {
		log.Fatalf("invalid proxy config: %v", err)
	}

	// 设置日志
	if gatewayConfig.Log.File != "" {
		f, err := os.OpenFile(gatewayConfig.Log.File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			log.Fatalf("open log file: %v", err)
		}
		defer f.Close()
		log.SetOutput(f)
	}
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	util.SetLogLevel(gatewayConfig.Log.Level)
	log.Printf("log level: %s", gatewayConfig.Log.Level)

	// 加载节点池
	poolMgr := pool.NewManager()
	if err := poolMgr.LoadFromFile(*configDir + "/nodes.yaml"); err != nil {
		log.Fatalf("load nodes: %v", err)
	}
	log.Printf("loaded %d node pools", len(poolMgr.ListPools()))

	// 启动订阅自动更新
	poolMgr.StartSubscriptionUpdater()

	// 启动健康检查
	healthChecker := pool.NewHealthChecker(poolMgr, 0, 0, "")
	if defaults, err := pool.ParseHealthCheckDefaults(&gatewayConfig.HealthCheck); err == nil {
		healthChecker.UpdateDefaults(defaults)
	} else {
		log.Printf("invalid health_check config, using defaults: %v", err)
	}
	healthChecker.Start()

	// 加载指纹库
	fpManager := fingerprint.NewManager()
	fpConfigPath := *configDir + "/fingerprints.yaml"
	if _, err := os.Stat(fpConfigPath); err == nil {
		if err := fpManager.LoadFromFile(fpConfigPath); err != nil {
			log.Fatalf("load fingerprints: %v", err)
		}
		log.Printf("loaded %d fingerprints", len(fpManager.List()))
	} else {
		log.Printf("fingerprints config not found, skipping: %s", fpConfigPath)
	}

	trafficStore := traffic.NewStore(1000)
	trafficStore.UpdateConfig(gatewayConfig.Traffic)

	// 创建代理服务器，Admin 设置页会复用同一个运行时配置入口。
	srv := server.NewServer(&server.Config{
		Listen:              gatewayConfig.Server.Listen,
		JWTSecret:           gatewayConfig.Admin.JWTSecret,
		TLSCert:             gatewayConfig.Server.TLS.Cert,
		TLSKey:              gatewayConfig.Server.TLS.Key,
		AllowPrivateTargets: gatewayConfig.Server.AllowPrivateTargets,
		ProxySettings:       &proxySettings,
	}, poolMgr, fpManager, trafficStore)

	// 解析登录限速配置
	loginLimit := buildLoginLimit(gatewayConfig)

	// 启动 Admin API 服务器
	var adminSrv *admin.Server
	if gatewayConfig.Admin.Listen != "" {
		adminSrv = admin.NewServer(
			gatewayConfig.Admin.Listen,
			poolMgr, fpManager, *configDir,
			gatewayConfig.Admin.Password,
			gatewayConfig.Admin.JWTSecret,
			gatewayConfig.Admin.JWTExpire,
			loginLimit,
			trafficStore,
		)
		adminSrv.SetHealthChecker(healthChecker)
		adminSrv.SetProxySettingsRuntime(srv)
		adminSrv.SetRuntimeInfo(admin.RuntimeInfo{
			ProxyListen: gatewayConfig.Server.Listen,
			ProxyTLS:    gatewayConfig.Server.TLS.Cert != "" && gatewayConfig.Server.TLS.Key != "",
			AdminListen: gatewayConfig.Admin.Listen,
			LogLevel:    util.LogLevelName(),
			LogOutput:   util.FirstNonEmpty(gatewayConfig.Log.File, "stdout"),
			Version:     "chijie 1.0.0",
		})
		go func() {
			if err := adminSrv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("admin server error: %v", err)
			}
		}()
	}

	// 异步启动主服务器
	srvErrCh := make(chan error, 1)
	go func() {
		var err error
		if gatewayConfig.Server.TLS.Cert != "" && gatewayConfig.Server.TLS.Key != "" {
			err = srv.StartTLS(gatewayConfig.Server.TLS.Cert, gatewayConfig.Server.TLS.Key)
		} else {
			err = srv.Start()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErrCh <- err
			return
		}
		srvErrCh <- nil
	}()

	// 等待信号或主服务器异常退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("received signal: %v, shutting down...", sig)
	case err := <-srvErrCh:
		if err != nil {
			log.Printf("server exited with error: %v", err)
		}
	}

	// 优雅关闭：5 秒超时
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}
	if adminSrv != nil {
		if err := adminSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("admin shutdown error: %v", err)
		}
	}
	healthChecker.Stop()
	poolMgr.StopSubscriptionUpdater()
	log.Printf("shutdown complete")
}

func loadGatewayConfig(path string) (*GatewayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var config GatewayConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// 默认值
	if config.Server.Listen == "" {
		config.Server.Listen = ":8080"
	}

	return &config, nil
}

// validateGatewayConfig 在启动阶段拒绝明显不安全的配置。
//
// 拒绝条件：
//   - jwt_secret 为空、长度 < 16，或命中已知占位字符串。
//   - admin.password 为空且 admin.listen 监听非 127.0.0.1 / localhost。
func validateGatewayConfig(cfg *GatewayConfig) error {
	secret := strings.TrimSpace(cfg.Admin.JWTSecret)
	if _, isPlaceholder := placeholderJWTSecrets[secret]; isPlaceholder {
		return fmt.Errorf("admin.jwt_secret must be set to a non-default value (placeholder detected); generate a strong random string and put it in gateway.yaml")
	}
	if len(secret) < 16 {
		return fmt.Errorf("admin.jwt_secret must be at least 16 characters long, got %d", len(secret))
	}

	listen := strings.TrimSpace(cfg.Admin.Listen)
	if cfg.Admin.Password == "" && listen != "" && !isLocalListen(listen) {
		return fmt.Errorf("admin.password is empty but admin.listen=%q is not a local address; set a password or bind to 127.0.0.1/localhost", listen)
	}

	return nil
}

// isLocalListen 判断 listen 配置是否仅暴露到本机回环。
func isLocalListen(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		host = listen
	}
	host = strings.TrimSpace(strings.ToLower(host))
	switch host {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// buildLoginLimit 把 yaml 字段转成 admin.LoginLimitConfig。
func buildLoginLimit(cfg *GatewayConfig) *admin.LoginLimitConfig {
	limit := &admin.LoginLimitConfig{
		MaxFailures: cfg.Admin.LoginMaxFailures,
	}
	if d, err := time.ParseDuration(cfg.Admin.LoginWindow); err == nil {
		limit.Window = d
	}
	if d, err := time.ParseDuration(cfg.Admin.LoginLockout); err == nil {
		limit.Lockout = d
	}
	return limit
}
