package dialer

import (
	"reflect"
	"testing"
)

func TestNormalizeHysteriaServerPorts(t *testing.T) {
	got := normalizeHysteriaServerPorts("16001-17000, 18001:19000|443")
	want := []string{"16001:17000", "18001:19000", "443:443"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestNewSingBoxDialerSupportsCommonProtocols(t *testing.T) {
	nodes := []*Node{
		{
			Name:     "ss-node",
			Type:     "ss",
			Server:   "ss.example.com",
			Port:     8388,
			Password: "pass",
			Extra: map[string]string{
				"cipher": "chacha20-ietf-poly1305",
			},
		},
		{
			Name:   "vmess-ws",
			Type:   "vmess",
			Server: "vmess.example.com",
			Port:   443,
			Extra: map[string]string{
				"uuid":     "11111111-1111-1111-1111-111111111111",
				"security": "auto",
				"tls":      "tls",
				"sni":      "vmess.example.com",
				"network":  "ws",
				"path":     "/ws",
				"host":     "cdn.example.com",
			},
		},
		{
			Name:   "vless-grpc",
			Type:   "vless",
			Server: "vless.example.com",
			Port:   443,
			Extra: map[string]string{
				"uuid":         "11111111-1111-1111-1111-111111111111",
				"tls":          "tls",
				"sni":          "vless.example.com",
				"network":      "grpc",
				"service_name": "tun",
			},
		},
		{
			Name:     "trojan-ws",
			Type:     "trojan",
			Server:   "trojan.example.com",
			Port:     443,
			Password: "secret",
			Extra: map[string]string{
				"sni":     "trojan.example.com",
				"network": "ws",
				"path":    "/edge",
				"host":    "cdn.example.com",
			},
		},
		{
			Name:     "hy2-node",
			Type:     "hysteria2",
			Server:   "hy.example.com",
			Port:     443,
			Password: "hy-pass",
			Extra: map[string]string{
				"sni":           "hy.example.com",
				"skip_verify":   "true",
				"obfs":          "salamander",
				"obfs_password": "obfs-pass",
				"up_mbps":       "20",
				"down_mbps":     "100",
				"ports":         "16001-17000",
			},
		},
		{
			Name:     "anytls-node",
			Type:     "anytls",
			Server:   "any.example.com",
			Port:     443,
			Password: "any-pass",
			Extra: map[string]string{
				"sni": "any.example.com",
			},
		},
		{
			Name:     "tuic-node",
			Type:     "tuic",
			Server:   "tuic.example.com",
			Port:     443,
			Password: "tuic-pass",
			Extra: map[string]string{
				"uuid":               "11111111-1111-1111-1111-111111111111",
				"sni":                "tuic.example.com",
				"congestion_control": "bbr",
				"udp_relay_mode":     "native",
				"alpn":               "h3",
			},
		},
	}

	for _, node := range nodes {
		t.Run(node.Name, func(t *testing.T) {
			d, err := NewDialer(node)
			if err != nil {
				t.Fatalf("create dialer: %v", err)
			}
			if d == nil {
				t.Fatal("dialer is nil")
			}
		})
	}
}
