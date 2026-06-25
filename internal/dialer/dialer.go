package dialer

import (
	"context"
	"fmt"
	"net"
	"net/http"
)

// Dialer 统一的出口拨号接口
type Dialer interface {
	// DialContext 建立到目标的连接
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)

	// GetHTTPTransport 返回配置好的 HTTP Transport（用于 HTTP 请求）
	GetHTTPTransport() *http.Transport

	// Name 返回 dialer 名称
	Name() string
}

// Node 节点配置
type Node struct {
	Name        string            `yaml:"name" json:"name"`
	Type        string            `yaml:"type" json:"type"` // direct, socks5, http_proxy, ss, vmess, trojan, vless, hysteria2, anytls, tuic
	Server      string            `yaml:"server" json:"server"`
	Port        int               `yaml:"port" json:"port"`
	Region      string            `yaml:"region,omitempty" json:"region,omitempty"`
	Username    string            `yaml:"username" json:"username,omitempty"`
	Password    string            `yaml:"password" json:"password,omitempty"`
	Enabled     *bool             `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Residential bool              `yaml:"residential,omitempty" json:"residential,omitempty"`
	Tags        []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Extra       map[string]string `yaml:"extra" json:"extra,omitempty"` // 协议特定参数
}

// NewDialer 根据节点配置创建 Dialer
func NewDialer(node *Node) (Dialer, error) {
	switch node.Type {
	case "direct":
		return NewDirectDialer(), nil
	case "socks5":
		return NewSOCKS5Dialer(node)
	case "http_proxy", "http":
		return NewHTTPProxyDialer(node)
	case "vless":
		if isXHTTPTransport(node) {
			return NewVLESSXHTTPDialer(node)
		}
		return NewSingBoxDialer(node)
	case "ss", "shadowsocks", "vmess", "trojan", "hysteria2", "hy2", "anytls", "tuic":
		return NewSingBoxDialer(node)
	default:
		return nil, fmt.Errorf("unsupported node type: %s", node.Type)
	}
}
