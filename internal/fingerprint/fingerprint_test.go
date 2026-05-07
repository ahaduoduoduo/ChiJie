package fingerprint

import (
	"strings"
	"testing"

	tls "github.com/refraction-networking/utls"
)

const chromeJA3 = "771,4865-4866-4867-49196-49195-52393-49200-49199-52392-49162-49161-49172-49171,0-23-65281-10-11-16-5-13-18-51-45-43-27-21,29-23-24-25,0"
const chromePQJA3 = "771,4865-4866-4867-49195-49199-49196-49200-52393-52392-49171-49172-156-157-47-53,11-23-43-16-5-13-0-65037-51-65281-17613-35-45-27-18-10-41,4588-29-23-24,0"
const chromeJA4Raw = "t13d2014h2_000a,002f,0035,009c,009d,1301,1302,1303,c008,c009,c00a,c012,c013,c014,c02b,c02c,c02f,c030,cca8,cca9_0000,0005,000a,000b,000d,0012,0015,0017,001b,002b,002d,0033,ff01_0403,0804,0401,0503,0805,0805,0501,0806,0601,0201"
const chromeAkamaiRaw = "HEADER_TABLE_SIZE=65536;ENABLE_PUSH=0;INITIAL_WINDOW_SIZE=6291456;MAX_HEADER_LIST_SIZE=262144|15663105|method,authority,scheme,path"

func TestBuildSpecFromValueSupportsCurlCFFIExtraFP(t *testing.T) {
	manager := NewManager()
	value := `ja3: "` + chromeJA3 + `"
extra_fp:
  tls_signature_algorithms:
    - rsa_pkcs1_sha256
  tls_cert_compression: zlib
  tls_grease: true`

	helloID, spec, err := manager.BuildSpecFromValue(value)
	if err != nil {
		t.Fatalf("BuildSpecFromValue returned error: %v", err)
	}
	if helloID == nil || helloID.Client != tls.HelloCustom.Client {
		t.Fatalf("expected custom hello id, got %#v", helloID)
	}
	if spec == nil {
		t.Fatal("expected custom ClientHelloSpec")
	}

	var hasGREASE bool
	var hasCompression bool
	var hasSignatureOverride bool
	var paddingCount int
	var compressionCount int
	for _, ext := range spec.Extensions {
		switch typed := ext.(type) {
		case *tls.UtlsGREASEExtension:
			hasGREASE = true
		case *tls.UtlsCompressCertExtension:
			compressionCount++
			for _, algo := range typed.Algorithms {
				if algo == tls.CertCompressionZlib {
					hasCompression = true
				}
			}
		case *tls.SignatureAlgorithmsExtension:
			hasSignatureOverride = len(typed.SupportedSignatureAlgorithms) == 1 &&
				typed.SupportedSignatureAlgorithms[0] == tls.PKCS1WithSHA256
		case *tls.UtlsPaddingExtension:
			paddingCount++
		}
	}
	if !hasGREASE {
		t.Fatal("expected GREASE extension from extra_fp.tls_grease")
	}
	if !hasCompression {
		t.Fatal("expected zlib certificate compression from extra_fp.tls_cert_compression")
	}
	if !hasSignatureOverride {
		t.Fatal("expected signature algorithm override from extra_fp.tls_signature_algorithms")
	}
	if paddingCount != 1 {
		t.Fatalf("expected exactly one padding extension, got %d", paddingCount)
	}
	if compressionCount != 1 {
		t.Fatalf("expected exactly one certificate compression extension, got %d", compressionCount)
	}
}

func TestJA3SupportedVersionsAllowsTLS13(t *testing.T) {
	spec, err := ja3ToSpec(chromeJA3, nil)
	if err != nil {
		t.Fatalf("ja3ToSpec returned error: %v", err)
	}
	if spec.TLSVersMin != tls.VersionTLS12 {
		t.Fatalf("expected TLS min 1.2 for TLS 1.3 JA3, got %#x", spec.TLSVersMin)
	}
	if spec.TLSVersMax != tls.VersionTLS13 {
		t.Fatalf("expected TLS max 1.3 for TLS 1.3 JA3, got %#x", spec.TLSVersMax)
	}
}

func TestChromePQJA3UsesStructuredExtensions(t *testing.T) {
	spec, err := ja3ToSpec(chromePQJA3, nil)
	if err != nil {
		t.Fatalf("ja3ToSpec returned error: %v", err)
	}

	var hasECH bool
	var hasALPS bool
	var hasPSK bool
	var paddingIndex = -1
	var pskIndex = -1
	var keyShares []tls.KeyShare
	for idx, ext := range spec.Extensions {
		switch typed := ext.(type) {
		case *tls.GREASEEncryptedClientHelloExtension:
			hasECH = true
		case *tls.ApplicationSettingsExtensionNew:
			hasALPS = true
		case *tls.UtlsPreSharedKeyExtension:
			hasPSK = true
			pskIndex = idx
		case *tls.UtlsPaddingExtension:
			paddingIndex = idx
		case *tls.KeyShareExtension:
			keyShares = typed.KeyShares
		}
	}
	if !hasECH {
		t.Fatal("expected GREASE ECH extension for extension 65037")
	}
	if !hasALPS {
		t.Fatal("expected ApplicationSettingsExtensionNew for extension 17613")
	}
	if !hasPSK {
		t.Fatal("expected UtlsPreSharedKeyExtension for extension 41")
	}
	if paddingIndex == -1 || pskIndex == -1 || paddingIndex > pskIndex {
		t.Fatalf("expected padding before PSK, padding=%d psk=%d", paddingIndex, pskIndex)
	}
	if len(keyShares) < 2 || keyShares[0].Group != tls.X25519MLKEM768 || keyShares[1].Group != tls.X25519 {
		t.Fatalf("unexpected key shares: %#v", keyShares)
	}
}

func TestCanonicalConvertsExtraFP(t *testing.T) {
	config := (&FingerprintConfig{
		JA3:    chromeJA3,
		JA4:    chromeJA4Raw,
		Akamai: " " + chromeAkamaiRaw + " ",
		TLS: &TLSDetailsConfig{
			Protocols: []string{" h2 ", "http/1.1"},
			Ciphers:   []string{" TLS_AES_128_GCM_SHA256 "},
		},
		HTTP2: &HTTP2DetailsConfig{
			Settings:     []string{" HEADER_TABLE_SIZE = 65536 "},
			WindowUpdate: " 15663105 ",
			Headers:      []string{" method ", "authority"},
		},
		ExtraFP: &ExtraConfig{
			TLSSignatureAlgorithms: []string{"rsa_pkcs1_sha256"},
			TLSCertCompression:     "zlib",
			TLSGREASE:              true,
		},
	}).Canonical()

	if config.ExtraFP != nil {
		t.Fatal("expected canonical config to drop extra_fp")
	}
	if config.JA4 != chromeJA4Raw {
		t.Fatalf("unexpected ja4: %s", config.JA4)
	}
	if config.Akamai != chromeAkamaiRaw {
		t.Fatalf("unexpected akamai: %s", config.Akamai)
	}
	if config.TLS == nil || strings.Join(config.TLS.Protocols, ",") != "h2,http/1.1" {
		t.Fatalf("unexpected tls details: %#v", config.TLS)
	}
	if config.HTTP2 == nil || config.HTTP2.WindowUpdate != "15663105" {
		t.Fatalf("unexpected http2 details: %#v", config.HTTP2)
	}
	if config.Extra == nil {
		t.Fatal("expected canonical config to include extra")
	}
	if got := strings.Join(config.Extra.SignatureAlgorithms, ","); got != "rsa_pkcs1_sha256" {
		t.Fatalf("unexpected signature algorithms: %s", got)
	}
	if config.Extra.CertCompression != "zlib" {
		t.Fatalf("unexpected cert compression: %s", config.Extra.CertCompression)
	}
	if !config.Extra.GREASE {
		t.Fatal("expected grease to be true")
	}
}

func TestParseConfigTextCapturedJSON(t *testing.T) {
	config, err := ParseConfigText(`{
  "http_version": "h2",
  "method": "GET",
  "user_agent": "Mozilla/5.0 Test",
  "tls": {
    "ciphers": ["TLS_AES_128_GCM_SHA256"],
    "extensions": [
      {"name": "application_layer_protocol_negotiation (16)", "protocols": ["h2", "http/1.1"]},
      {"name": "supported_versions (43)", "versions": ["TLS 1.3", "TLS 1.2"]},
      {"name": "supported_groups (10)", "supported_groups": ["X25519 (29)", "P-256 (23)"]},
      {"name": "signature_algorithms (13)", "signature_algorithms": ["ecdsa_secp256r1_sha256"]}
    ],
    "tls_version_negotiated": "772",
    "ja3": "` + chromePQJA3 + `",
    "ja4_r": "` + chromeJA4Raw + `",
    "ja4": "t13d2014h2_a09f3c656075_8c54e8d281eb"
  },
  "http2": {
    "akamai_fingerprint": "1:65536;2:0;4:6291456;6:262144|15663105|0|m,a,s,p",
    "sent_frames": [
      {"frame_type": "SETTINGS", "settings": ["HEADER_TABLE_SIZE = 65536", "ENABLE_PUSH = 0"]},
      {"frame_type": "WINDOW_UPDATE", "increment": 15663105},
      {"frame_type": "HEADERS", "headers": [":method: GET", ":authority: get.ja3.zone", ":scheme: https", ":path: /", "user-agent: Mozilla/5.0 Test"], "priority": {"weight": 220, "depends_on": 0, "exclusive": 1}}
    ]
  }
}`)
	if err != nil {
		t.Fatalf("ParseConfigText returned error: %v", err)
	}
	if config.JA3 != chromePQJA3 {
		t.Fatalf("unexpected ja3: %s", config.JA3)
	}
	if config.JA4 != chromeJA4Raw {
		t.Fatalf("unexpected ja4: %s", config.JA4)
	}
	if config.Akamai != "1:65536;2:0;4:6291456;6:262144|15663105|0|m,a,s,p" {
		t.Fatalf("unexpected akamai: %s", config.Akamai)
	}
	if config.HTTPVersion != "h2" || !config.WantsHTTP2() {
		t.Fatalf("expected h2 config, got %#v", config)
	}
	if config.HTTP2 == nil || config.HTTP2.WindowUpdate != "15663105" || len(config.HTTP2.HeaderLines) != 5 {
		t.Fatalf("unexpected http2 details: %#v", config.HTTP2)
	}
	if config.HTTP2.Priority == nil || config.HTTP2.Priority.Weight != 220 || !config.HTTP2.Priority.Exclusive {
		t.Fatalf("unexpected priority: %#v", config.HTTP2.Priority)
	}
	if config.TLS == nil || strings.Join(config.TLS.Protocols, ",") != "h2,http/1.1" {
		t.Fatalf("unexpected tls details: %#v", config.TLS)
	}
}

func TestParseAkamaiRawNumericFormat(t *testing.T) {
	settings, windowUpdate, order := parseAkamaiRaw("1:65536;2:0;4:6291456;6:262144|15663105|0|m,a,s,p")
	if len(settings) != 4 {
		t.Fatalf("expected 4 settings, got %d", len(settings))
	}
	if settings[0].ID != 1 || settings[0].Val != 65536 {
		t.Fatalf("unexpected first setting: %#v", settings[0])
	}
	if windowUpdate != 15663105 {
		t.Fatalf("unexpected window update: %d", windowUpdate)
	}
	if strings.Join(order, ",") != "method,authority,scheme,path" {
		t.Fatalf("unexpected order: %v", order)
	}
}

func TestBuildSpecFromConfigAllowsJA3WithJA4Raw(t *testing.T) {
	helloID, spec, err := BuildSpecFromConfig(&FingerprintConfig{
		JA3:    chromeJA3,
		JA4:    chromeJA4Raw,
		Akamai: chromeAkamaiRaw,
	})
	if err != nil {
		t.Fatalf("BuildSpecFromConfig returned error: %v", err)
	}
	if helloID == nil || helloID.Client != tls.HelloCustom.Client {
		t.Fatalf("expected custom hello id, got %#v", helloID)
	}
	if spec == nil || spec.TLSVersMax != tls.VersionTLS13 {
		t.Fatalf("expected TLS 1.3 custom spec, got %#v", spec)
	}
}

func TestBuildSpecFromValueJA4RawBuildsSpec(t *testing.T) {
	manager := NewManager()
	helloID, spec, err := manager.BuildSpecFromValue("ja4:" + chromeJA4Raw)
	if err != nil {
		t.Fatalf("BuildSpecFromValue returned error: %v", err)
	}
	if helloID == nil || helloID.Client != tls.HelloCustom.Client {
		t.Fatalf("expected custom hello id, got %#v", helloID)
	}
	if spec == nil {
		t.Fatal("expected custom ClientHelloSpec")
	}
	if len(spec.CipherSuites) != 20 {
		t.Fatalf("expected 20 cipher suites, got %d", len(spec.CipherSuites))
	}
	if spec.TLSVersMax != tls.VersionTLS13 {
		t.Fatalf("expected TLS max 1.3, got %#x", spec.TLSVersMax)
	}
}

func TestBuildSpecFromValueJA4CompactStringIsNotRaw(t *testing.T) {
	manager := NewManager()
	_, _, err := manager.BuildSpecFromValue("ja4:t13d2014h2_a09f3c656075_8c54e8d281eb")
	if err == nil || !strings.Contains(err.Error(), "invalid JA4") {
		t.Fatalf("expected invalid JA4 raw error, got %v", err)
	}
}
