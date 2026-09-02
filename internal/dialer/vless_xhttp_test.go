package dialer

import (
	"strings"
	"testing"

	"github.com/justinwoo280/sing-xhttp/xhttp"
)

func TestBuildXHTTPOptionsSupportsRequiredModes(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want string
	}{
		{name: "default", mode: "", want: xhttp.ModePacketUp},
		{name: "auto", mode: "auto", want: xhttp.ModePacketUp},
		{name: "packet-up", mode: "packet-up", want: xhttp.ModePacketUp},
		{name: "stream-up", mode: "stream-up", want: xhttp.ModeStreamUp},
		{name: "stream-one", mode: "stream-one", want: xhttp.ModeStreamOne},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			options, err := buildXHTTPOptions(&Node{Extra: map[string]string{
				"xhttp_mode": tc.mode,
				"xhttp_path": "/xhttp",
			}})
			if err != nil {
				t.Fatalf("build xhttp options: %v", err)
			}
			if options.Mode != tc.want {
				t.Fatalf("mode = %q, want %q", options.Mode, tc.want)
			}
		})
	}
}

func TestBuildXHTTPOptionsAutoUsesStreamOneForReality(t *testing.T) {
	options, err := buildXHTTPOptions(&Node{Extra: map[string]string{
		"security":   "reality",
		"public_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"xhttp_mode": "auto",
	}})
	if err != nil {
		t.Fatalf("build xhttp options: %v", err)
	}
	if options.Mode != xhttp.ModeStreamOne {
		t.Fatalf("mode = %q, want %q", options.Mode, xhttp.ModeStreamOne)
	}
}

func TestBuildXHTTPOptionsRejectsUnsupportedMode(t *testing.T) {
	_, err := buildXHTTPOptions(&Node{Extra: map[string]string{"xhttp_mode": "stream-sideways"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported xhttp mode: stream-sideways") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildXHTTPOptionsRejectsDownloadSettings(t *testing.T) {
	tests := []map[string]string{
		{"xhttp_download_settings": "true"},
		{"xhttp_download_server": "download.example.com"},
	}
	for _, extra := range tests {
		_, err := buildXHTTPOptions(&Node{Extra: extra})
		if err == nil || !strings.Contains(err.Error(), "unsupported xhttp downloadSettings") {
			t.Fatalf("unexpected error for %#v: %v", extra, err)
		}
	}
}

func TestBuildXHTTPOptionsRejectsHTTP3(t *testing.T) {
	_, err := buildXHTTPOptions(&Node{Extra: map[string]string{"alpn": "h3,h2"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported xhttp HTTP/3") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewVLESSXHTTPDialerSupportsStreamOneAndAuto(t *testing.T) {
	for _, mode := range []string{xhttp.ModeStreamOne, xhttp.ModeAuto} {
		t.Run(mode, func(t *testing.T) {
			dialer, err := NewVLESSXHTTPDialer(&Node{
				Name:   "vless-xhttp-" + mode,
				Type:   "vless",
				Server: "xhttp.example.com",
				Port:   443,
				Extra: map[string]string{
					"uuid":       "11111111-1111-1111-1111-111111111111",
					"security":   "tls",
					"sni":        "xhttp.example.com",
					"network":    "xhttp",
					"xhttp_mode": mode,
					"xhttp_path": "/xhttp",
				},
			})
			if err != nil {
				t.Fatalf("create dialer: %v", err)
			}
			if dialer == nil {
				t.Fatal("dialer is nil")
			}
		})
	}
}
