//go:build with_utls

package dialer

import "testing"

func TestNewSingBoxDialerSupportsUTLSAndReality(t *testing.T) {
	nodes := []*Node{
		{
			Name:   "vmess-utls",
			Type:   "vmess",
			Server: "vmess.example.com",
			Port:   443,
			Extra: map[string]string{
				"uuid":        "11111111-1111-1111-1111-111111111111",
				"security":    "auto",
				"tls":         "tls",
				"sni":         "vmess.example.com",
				"fingerprint": "chrome",
			},
		},
		{
			Name:   "vless-reality",
			Type:   "vless",
			Server: "vless.example.com",
			Port:   443,
			Extra: map[string]string{
				"uuid":       "11111111-1111-1111-1111-111111111111",
				"security":   "reality",
				"sni":        "www.example.com",
				"public_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
				"short_id":   "abcd",
			},
		},
		{
			Name:   "vless-xhttp-reality-auto",
			Type:   "vless",
			Server: "vless.example.com",
			Port:   443,
			Extra: map[string]string{
				"uuid":       "11111111-1111-1111-1111-111111111111",
				"security":   "reality",
				"sni":        "www.example.com",
				"public_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
				"short_id":   "abcd",
				"network":    "xhttp",
				"xhttp_mode": "auto",
				"xhttp_path": "/xhttp",
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
