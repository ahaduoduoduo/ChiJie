package fingerprint

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	tls "github.com/refraction-networking/utls"
	"gopkg.in/yaml.v3"
)

// FingerprintConfig 单个指纹配置
type FingerprintConfig struct {
	Preset      string              `yaml:"preset,omitempty" json:"preset,omitempty"`             // 预设名：chrome, safari, firefox, ios, edge, 360, qq
	JA3         string              `yaml:"ja3,omitempty" json:"ja3,omitempty"`                   // JA3 raw 字符串
	JA4         string              `yaml:"ja4,omitempty" json:"ja4,omitempty"`                   // JA4 raw 列表
	Akamai      string              `yaml:"akamai,omitempty" json:"akamai,omitempty"`             // Akamai HTTP/2 raw 字符串
	HTTPVersion string              `yaml:"http_version,omitempty" json:"http_version,omitempty"` // h2 或 http/1.1
	Method      string              `yaml:"method,omitempty" json:"method,omitempty"`             // 导入检测数据时保留的请求方法
	UserAgent   string              `yaml:"user_agent,omitempty" json:"user_agent,omitempty"`     // 导入检测数据时保留的 UA
	TLS         *TLSDetailsConfig   `yaml:"tls,omitempty" json:"tls,omitempty"`                   // TLS 详细 raw 字段
	HTTP2       *HTTP2DetailsConfig `yaml:"http2,omitempty" json:"http2,omitempty"`               // HTTP/2 详细 raw 字段
	Extra       *ExtraConfig        `yaml:"extra,omitempty" json:"extra,omitempty"`               // 额外 TLS 参数
	ExtraFP     *ExtraConfig        `yaml:"extra_fp,omitempty" json:"extra_fp,omitempty"`         // curl_cffi extra_fp 兼容字段
}

// TLSDetailsConfig 保存 TLS ClientHello 细节的 raw 输入。
type TLSDetailsConfig struct {
	TLSUsed             string   `yaml:"tls_used,omitempty" json:"tls_used,omitempty"`
	Protocols           []string `yaml:"protocols,omitempty" json:"protocols,omitempty"`
	SupportedVersions   []string `yaml:"supported_versions,omitempty" json:"supported_versions,omitempty"`
	Curves              []string `yaml:"curves,omitempty" json:"curves,omitempty"`
	SignatureAlgorithms []string `yaml:"signature_algorithms,omitempty" json:"signature_algorithms,omitempty"`
	Extensions          []string `yaml:"extensions,omitempty" json:"extensions,omitempty"`
	Ciphers             []string `yaml:"ciphers,omitempty" json:"ciphers,omitempty"`
}

// HTTP2DetailsConfig 保存 HTTP/2 指纹细节的 raw 输入。
type HTTP2DetailsConfig struct {
	Settings     []string             `yaml:"settings,omitempty" json:"settings,omitempty"`
	WindowUpdate string               `yaml:"window_update,omitempty" json:"window_update,omitempty"`
	Headers      []string             `yaml:"headers,omitempty" json:"headers,omitempty"`
	HeaderLines  []string             `yaml:"header_lines,omitempty" json:"header_lines,omitempty"`
	Priority     *HTTP2PriorityConfig `yaml:"priority,omitempty" json:"priority,omitempty"`
}

// HTTP2PriorityConfig 保存 HEADERS frame 的优先级字段。
type HTTP2PriorityConfig struct {
	Weight    uint8  `yaml:"weight,omitempty" json:"weight,omitempty"`
	DependsOn uint32 `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	Exclusive bool   `yaml:"exclusive,omitempty" json:"exclusive,omitempty"`
}

// ExtraConfig JA3 之外的额外指纹参数
type ExtraConfig struct {
	SignatureAlgorithms    []string `yaml:"signature_algorithms,omitempty" json:"signature_algorithms,omitempty"`
	CertCompression        string   `yaml:"cert_compression,omitempty" json:"cert_compression,omitempty"` // zlib, brotli, zstd
	GREASE                 bool     `yaml:"grease,omitempty" json:"grease,omitempty"`
	TLSSignatureAlgorithms []string `yaml:"tls_signature_algorithms,omitempty" json:"tls_signature_algorithms,omitempty"`
	TLSCertCompression     string   `yaml:"tls_cert_compression,omitempty" json:"tls_cert_compression,omitempty"`
	TLSGREASE              bool     `yaml:"tls_grease,omitempty" json:"tls_grease,omitempty"`
}

// FileConfig fingerprints.yaml 顶层结构
type FileConfig struct {
	Fingerprints map[string]*FingerprintConfig `yaml:"fingerprints"`
}

// Manager 指纹管理器
type Manager struct {
	fingerprints map[string]*FingerprintConfig
}

// NewManager 创建指纹管理器
func NewManager() *Manager {
	return &Manager{
		fingerprints: make(map[string]*FingerprintConfig),
	}
}

// LoadFromFile 从 YAML 文件加载指纹配置
func (m *Manager) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read fingerprints config: %w", err)
	}

	var config FileConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parse fingerprints config: %w", err)
	}

	m.fingerprints = config.Fingerprints
	if m.fingerprints == nil {
		m.fingerprints = make(map[string]*FingerprintConfig)
	}

	return nil
}

// Get 按名字获取指纹配置
func (m *Manager) Get(name string) (*FingerprintConfig, bool) {
	fp, ok := m.fingerprints[name]
	return fp, ok
}

// List 返回所有指纹名称
func (m *Manager) List() []string {
	names := make([]string, 0, len(m.fingerprints))
	for name := range m.fingerprints {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// BuildSpec 根据指纹配置构建 uTLS ClientHelloID 或 ClientHelloSpec
// 返回 (helloID, spec, error)
// 如果是预设指纹，返回 helloID + nil spec
// 如果是 JA3/JA4 raw 自定义指纹，返回 HelloCustom + spec
func (m *Manager) BuildSpec(name string) (*tls.ClientHelloID, *tls.ClientHelloSpec, error) {
	fp, ok := m.fingerprints[name]
	if !ok {
		return nil, nil, fmt.Errorf("fingerprint not found: %s", name)
	}

	helloID, spec, err := BuildSpecFromConfig(fp)
	if err != nil && fp.JA3 != "" {
		return nil, nil, fmt.Errorf("parse JA3 for %s: %w", name, err)
	}
	return helloID, spec, err
}

// BuildSpecFromConfig 根据单个配置构建 uTLS ClientHelloID 或 ClientHelloSpec。
func BuildSpecFromConfig(fp *FingerprintConfig) (*tls.ClientHelloID, *tls.ClientHelloSpec, error) {
	if fp == nil {
		return nil, nil, fmt.Errorf("fingerprint config is required")
	}

	if fp.JA4 != "" {
		if _, err := parseJA4Material(fp.JA4); err != nil {
			return nil, nil, err
		}
	}

	// 预设指纹
	if fp.Preset != "" {
		id, err := presetToHelloID(fp.Preset)
		if err != nil {
			return nil, nil, err
		}
		return id, nil, nil
	}

	// JA3 自定义指纹
	if fp.JA3 != "" {
		spec, err := ja3ToSpec(fp.JA3, fp.EffectiveExtra())
		if err != nil {
			return nil, nil, err
		}
		id := tls.HelloCustom
		return &id, spec, nil
	}

	if fp.JA4 != "" {
		spec, err := ja4ToSpec(fp.JA4, fp.EffectiveExtra())
		if err != nil {
			return nil, nil, err
		}
		id := tls.HelloCustom
		return &id, spec, nil
	}

	return nil, nil, fmt.Errorf("fingerprint has no preset, ja3, or ja4 raw")
}

// BuildSpecFromValue 根据请求传入的指纹字符串构建 uTLS 配置。
// value 可以是后台指纹名称、预设名称、JA3/JA4 raw 字符串，或 JSON/YAML 格式的 FingerprintConfig。
func (m *Manager) BuildSpecFromValue(value string) (*tls.ClientHelloID, *tls.ClientHelloSpec, error) {
	config, err := m.ConfigFromValue(value)
	if err != nil {
		return nil, nil, err
	}
	return BuildSpecFromConfig(config)
}

// ConfigFromValue 根据请求传入的指纹字符串解析为规范配置。
func (m *Manager) ConfigFromValue(value string) (*FingerprintConfig, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("fingerprint value is required")
	}

	if fp, ok := m.fingerprints[value]; ok {
		return fp.Canonical(), nil
	}

	if id, err := presetToHelloID(value); err == nil {
		_ = id
		return (&FingerprintConfig{Preset: value}).Canonical(), nil
	}

	if looksLikeJA3(value) {
		return (&FingerprintConfig{JA3: value}).Canonical(), nil
	}

	if _, err := parseJA4Material(value); err == nil {
		return (&FingerprintConfig{JA4: value}).Canonical(), nil
	}

	if config, err := ParseConfigText(value); err == nil && (config.Preset != "" || config.JA3 != "" || config.JA4 != "") {
		return config, nil
	}

	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "ja3:") {
		ja3 := strings.TrimSpace(value[len("ja3:"):])
		if looksLikeJA3(ja3) {
			return (&FingerprintConfig{JA3: ja3}).Canonical(), nil
		}
	}

	if strings.HasPrefix(lower, "ja4:") {
		ja4 := strings.TrimSpace(value[len("ja4:"):])
		if _, err := parseJA4Material(ja4); err != nil {
			return nil, err
		}
		return (&FingerprintConfig{JA4: ja4}).Canonical(), nil
	}

	return nil, fmt.Errorf("unsupported fingerprint value")
}

// EffectiveExtra 返回内部 canonical extra，并兼容 curl_cffi 的 extra_fp/tls_* 字段。
func (fp *FingerprintConfig) EffectiveExtra() *ExtraConfig {
	if fp == nil {
		return nil
	}

	var merged ExtraConfig
	has := false
	applyExtra := func(extra *ExtraConfig) {
		if extra == nil {
			return
		}
		has = true
		if len(extra.SignatureAlgorithms) > 0 {
			merged.SignatureAlgorithms = append([]string(nil), extra.SignatureAlgorithms...)
		}
		if len(extra.TLSSignatureAlgorithms) > 0 {
			merged.SignatureAlgorithms = append([]string(nil), extra.TLSSignatureAlgorithms...)
		}
		if extra.CertCompression != "" {
			merged.CertCompression = extra.CertCompression
		}
		if extra.TLSCertCompression != "" {
			merged.CertCompression = extra.TLSCertCompression
		}
		if extra.GREASE || extra.TLSGREASE {
			merged.GREASE = true
		}
	}

	applyExtra(fp.Extra)
	applyExtra(fp.ExtraFP)
	if !has || merged.isZero() {
		return nil
	}
	return &merged
}

// Canonical 返回保存到 fingerprints.yaml 的规范结构，避免把兼容字段原样写回。
func (fp *FingerprintConfig) Canonical() *FingerprintConfig {
	if fp == nil {
		return nil
	}
	return &FingerprintConfig{
		Preset:      strings.TrimSpace(fp.Preset),
		JA3:         strings.TrimSpace(fp.JA3),
		JA4:         strings.TrimSpace(fp.JA4),
		Akamai:      strings.TrimSpace(fp.Akamai),
		HTTPVersion: strings.TrimSpace(fp.HTTPVersion),
		Method:      strings.ToUpper(strings.TrimSpace(fp.Method)),
		UserAgent:   strings.TrimSpace(fp.UserAgent),
		TLS:         canonicalTLSDetails(fp.TLS),
		HTTP2:       canonicalHTTP2Details(fp.HTTP2),
		Extra:       fp.EffectiveExtra(),
	}
}

func (extra *ExtraConfig) isZero() bool {
	if extra == nil {
		return true
	}
	return len(extra.SignatureAlgorithms) == 0 && extra.CertCompression == "" && !extra.GREASE
}

func canonicalTLSDetails(details *TLSDetailsConfig) *TLSDetailsConfig {
	if details == nil {
		return nil
	}
	canonical := &TLSDetailsConfig{
		TLSUsed:             strings.TrimSpace(details.TLSUsed),
		Protocols:           trimStringSlice(details.Protocols),
		SupportedVersions:   trimStringSlice(details.SupportedVersions),
		Curves:              trimStringSlice(details.Curves),
		SignatureAlgorithms: trimStringSlice(details.SignatureAlgorithms),
		Extensions:          trimStringSlice(details.Extensions),
		Ciphers:             trimStringSlice(details.Ciphers),
	}
	if canonical.TLSUsed == "" &&
		len(canonical.Protocols) == 0 &&
		len(canonical.SupportedVersions) == 0 &&
		len(canonical.Curves) == 0 &&
		len(canonical.SignatureAlgorithms) == 0 &&
		len(canonical.Extensions) == 0 &&
		len(canonical.Ciphers) == 0 {
		return nil
	}
	return canonical
}

func canonicalHTTP2Details(details *HTTP2DetailsConfig) *HTTP2DetailsConfig {
	if details == nil {
		return nil
	}
	canonical := &HTTP2DetailsConfig{
		Settings:     trimStringSlice(details.Settings),
		WindowUpdate: strings.TrimSpace(details.WindowUpdate),
		Headers:      trimStringSlice(details.Headers),
		HeaderLines:  trimStringSlice(details.HeaderLines),
		Priority:     canonicalHTTP2Priority(details.Priority),
	}
	if len(canonical.Settings) == 0 && canonical.WindowUpdate == "" && len(canonical.Headers) == 0 && len(canonical.HeaderLines) == 0 && canonical.Priority == nil {
		return nil
	}
	return canonical
}

func canonicalHTTP2Priority(priority *HTTP2PriorityConfig) *HTTP2PriorityConfig {
	if priority == nil {
		return nil
	}
	if priority.Weight == 0 && priority.DependsOn == 0 && !priority.Exclusive {
		return nil
	}
	return &HTTP2PriorityConfig{
		Weight:    priority.Weight,
		DependsOn: priority.DependsOn,
		Exclusive: priority.Exclusive,
	}
}

func trimStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func looksLikeJA3(value string) bool {
	parts := strings.Split(value, ",")
	if len(parts) != 5 {
		return false
	}
	_, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 16)
	return err == nil
}

// presetToHelloID 预设名称 → uTLS ClientHelloID
func presetToHelloID(preset string) (*tls.ClientHelloID, error) {
	switch strings.ToLower(preset) {
	case "chrome":
		return &tls.HelloChrome_Auto, nil
	case "firefox":
		return &tls.HelloFirefox_Auto, nil
	case "safari":
		return &tls.HelloSafari_Auto, nil
	case "ios":
		return &tls.HelloIOS_Auto, nil
	case "edge":
		return &tls.HelloEdge_Auto, nil
	case "360":
		return &tls.Hello360_Auto, nil
	case "qq":
		return &tls.HelloQQ_Auto, nil
	case "random":
		return &tls.HelloRandomized, nil
	default:
		return nil, fmt.Errorf("unknown preset: %s", preset)
	}
}

// ja3ToSpec 解析 JA3 字符串为 ClientHelloSpec
// JA3 格式: TLSVersion,CipherSuites,Extensions,EllipticCurves,EllipticCurvePointFormats
// 示例: 771,4865-4866-4867-49196-49195,0-23-65281-10-11-16-5-13-18-51-45-43-27-21,29-23-24-25,0
func ja3ToSpec(ja3 string, extra *ExtraConfig) (*tls.ClientHelloSpec, error) {
	parts := strings.Split(ja3, ",")
	if len(parts) != 5 {
		return nil, fmt.Errorf("invalid JA3 format: expected 5 parts, got %d", len(parts))
	}

	// 解析 TLS 版本
	tlsVersion, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid TLS version: %w", err)
	}

	// 解析 Cipher Suites
	cipherSuites, err := parseUint16List(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid cipher suites: %w", err)
	}

	// 解析 Extensions
	extensionIDs, err := parseUint16List(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid extensions: %w", err)
	}

	// 解析 Elliptic Curves
	curves, err := parseUint16List(parts[3])
	if err != nil {
		return nil, fmt.Errorf("invalid curves: %w", err)
	}

	// 解析 Point Formats
	pointFormats, err := parseUint8List(parts[4])
	if err != nil {
		return nil, fmt.Errorf("invalid point formats: %w", err)
	}

	// 构建 Extensions 列表
	extensions := buildExtensions(extensionIDs, curves, pointFormats, extra)

	// 确定 TLS 版本范围
	var tlsVersMin, tlsVersMax uint16
	switch {
	case containsUint16(extensionIDs, 43):
		tlsVersMin = tls.VersionTLS12
		tlsVersMax = tls.VersionTLS13
	case uint16(tlsVersion) == tls.VersionTLS13:
		tlsVersMin = tls.VersionTLS12
		tlsVersMax = tls.VersionTLS13
	case uint16(tlsVersion) == tls.VersionTLS12:
		tlsVersMin = tls.VersionTLS10
		tlsVersMax = tls.VersionTLS12
	default:
		tlsVersMin = tls.VersionTLS10
		tlsVersMax = uint16(tlsVersion)
	}

	spec := &tls.ClientHelloSpec{
		TLSVersMin:         tlsVersMin,
		TLSVersMax:         tlsVersMax,
		CipherSuites:       cipherSuites,
		CompressionMethods: []uint8{0}, // null compression
		Extensions:         extensions,
	}

	return spec, nil
}

// buildExtensions 根据 extension ID 列表构建 TLSExtension 切片
func buildExtensions(extIDs []uint16, curves []uint16, pointFormats []uint8, extra *ExtraConfig) []tls.TLSExtension {
	sigAlgs := defaultSignatureAlgorithms()
	if extra != nil && len(extra.SignatureAlgorithms) > 0 {
		sigAlgs = parseSignatureAlgorithms(extra.SignatureAlgorithms)
	}
	return buildExtensionsWithSignatureAlgorithms(extIDs, curves, pointFormats, extra, sigAlgs)
}

func buildExtensionsWithSignatureAlgorithms(extIDs []uint16, curves []uint16, pointFormats []uint8, extra *ExtraConfig, sigAlgs []tls.SignatureScheme) []tls.TLSExtension {
	extensions := make([]tls.TLSExtension, 0, len(extIDs)+2)

	if len(sigAlgs) == 0 {
		sigAlgs = defaultSignatureAlgorithms()
	}
	certCompressionAlgos := certCompressionAlgorithms(extra)
	hasPadding := containsUint16(extIDs, 21)
	hasCertCompression := containsUint16(extIDs, 27)

	// 构建 curves 列表
	curveIDs := make([]tls.CurveID, len(curves))
	for i, c := range curves {
		curveIDs[i] = tls.CurveID(c)
	}

	for _, id := range extIDs {
		ext := mapExtension(id, curveIDs, pointFormats, sigAlgs, certCompressionAlgos)
		extensions = append(extensions, ext)
	}

	// GREASE
	if extra != nil && extra.GREASE {
		extensions = append([]tls.TLSExtension{&tls.UtlsGREASEExtension{}}, extensions...)
		extensions = append(extensions, &tls.UtlsGREASEExtension{})
	}

	// 证书压缩
	if len(certCompressionAlgos) > 0 && !hasCertCompression {
		extensions = append(extensions, &tls.UtlsCompressCertExtension{
			Algorithms: certCompressionAlgos,
		})
	}

	if !hasPadding {
		extensions = appendPaddingExtension(extensions)
	}

	return extensions
}

func appendPaddingExtension(extensions []tls.TLSExtension) []tls.TLSExtension {
	padding := &tls.UtlsPaddingExtension{GetPaddingLen: tls.BoringPaddingStyle}
	for idx, ext := range extensions {
		if _, ok := ext.(*tls.UtlsPreSharedKeyExtension); ok {
			extensions = append(extensions[:idx], append([]tls.TLSExtension{padding}, extensions[idx:]...)...)
			return extensions
		}
	}
	return append(extensions, padding)
}

// mapExtension 将 extension ID 映射到具体的 TLSExtension
func mapExtension(id uint16, curves []tls.CurveID, pointFormats []uint8, sigAlgs []tls.SignatureScheme, certCompressionAlgos []tls.CertCompressionAlgo) tls.TLSExtension {
	switch id {
	case 0: // server_name (SNI)
		return &tls.SNIExtension{}
	case 5: // status_request (OCSP)
		return &tls.StatusRequestExtension{}
	case 10: // supported_groups (elliptic_curves)
		return &tls.SupportedCurvesExtension{Curves: curves}
	case 11: // ec_point_formats
		return &tls.SupportedPointsExtension{SupportedPoints: pointFormats}
	case 13: // signature_algorithms
		return &tls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: sigAlgs}
	case 16: // ALPN
		return &tls.ALPNExtension{AlpnProtocols: []string{"h2", "http/1.1"}}
	case 17: // status_request_v2
		return &tls.StatusRequestV2Extension{}
	case 18: // signed_certificate_timestamp (SCT)
		return &tls.SCTExtension{}
	case 21: // padding
		return &tls.UtlsPaddingExtension{GetPaddingLen: tls.BoringPaddingStyle}
	case 22: // encrypt_then_mac
		return &tls.GenericExtension{Id: 22}
	case 23: // extended_master_secret
		return &tls.ExtendedMasterSecretExtension{}
	case 27: // compress_certificate
		if len(certCompressionAlgos) > 0 {
			return &tls.UtlsCompressCertExtension{
				Algorithms: certCompressionAlgos,
			}
		}
		return &tls.UtlsCompressCertExtension{
			Algorithms: []tls.CertCompressionAlgo{tls.CertCompressionBrotli},
		}
	case 35: // session_ticket
		return &tls.SessionTicketExtension{}
	case 41: // pre_shared_key
		return &tls.UtlsPreSharedKeyExtension{}
	case 43: // supported_versions
		return &tls.SupportedVersionsExtension{Versions: []uint16{
			tls.VersionTLS13, tls.VersionTLS12,
		}}
	case 45: // psk_key_exchange_modes
		return &tls.PSKKeyExchangeModesExtension{Modes: []uint8{tls.PskModeDHE}}
	case 51: // key_share
		return &tls.KeyShareExtension{KeyShares: keySharesFromCurves(curves)}
	case 17513: // application_settings (ALPS, old codepoint)
		return &tls.ApplicationSettingsExtension{SupportedProtocols: []string{"h2"}}
	case 17613: // application_settings (ALPS, new codepoint)
		return &tls.ApplicationSettingsExtensionNew{SupportedProtocols: []string{"h2"}}
	case 65037: // encrypted_client_hello GREASE
		return tls.BoringGREASEECH()
	case 65281: // renegotiation_info
		return &tls.RenegotiationInfoExtension{Renegotiation: tls.RenegotiateOnceAsClient}
	default:
		// 未识别的 extension，使用 GenericExtension 保留原始数据
		return &tls.GenericExtension{Id: id}
	}
}

func keySharesFromCurves(curves []tls.CurveID) []tls.KeyShare {
	if len(curves) == 0 {
		return []tls.KeyShare{{Group: tls.X25519}, {Group: tls.CurveP256}}
	}

	hasX25519 := false
	for _, curve := range curves {
		if curve == tls.X25519 {
			hasX25519 = true
			break
		}
	}

	var keyShares []tls.KeyShare
	for _, curve := range curves {
		switch curve {
		case tls.X25519MLKEM768:
			keyShares = append(keyShares, tls.KeyShare{Group: curve})
			if hasX25519 {
				keyShares = append(keyShares, tls.KeyShare{Group: tls.X25519})
			}
			return keyShares
		case tls.X25519, tls.CurveP256, tls.CurveP384:
			keyShares = append(keyShares, tls.KeyShare{Group: curve})
			if len(keyShares) >= 2 {
				return keyShares
			}
		}
	}
	if len(keyShares) == 0 {
		return []tls.KeyShare{{Group: tls.X25519}, {Group: tls.CurveP256}}
	}
	return keyShares
}

// parseSignatureAlgorithms 解析签名算法列表
func parseSignatureAlgorithms(names []string) []tls.SignatureScheme {
	nameMap := map[string]tls.SignatureScheme{
		"ecdsa_secp256r1_sha256": tls.ECDSAWithP256AndSHA256,
		"ecdsa_secp384r1_sha384": tls.ECDSAWithP384AndSHA384,
		"ecdsa_secp521r1_sha512": tls.ECDSAWithP521AndSHA512,
		"rsa_pss_rsae_sha256":    tls.PSSWithSHA256,
		"rsa_pss_rsae_sha384":    tls.PSSWithSHA384,
		"rsa_pss_rsae_sha512":    tls.PSSWithSHA512,
		"rsa_pkcs1_sha256":       tls.PKCS1WithSHA256,
		"rsa_pkcs1_sha384":       tls.PKCS1WithSHA384,
		"rsa_pkcs1_sha512":       tls.PKCS1WithSHA512,
		"rsa_pkcs1_sha1":         tls.PKCS1WithSHA1,
	}

	var result []tls.SignatureScheme
	for _, name := range names {
		if scheme, ok := nameMap[strings.ToLower(name)]; ok {
			result = append(result, scheme)
		}
	}

	if len(result) == 0 {
		return defaultSignatureAlgorithms()
	}
	return result
}

func certCompressionAlgorithms(extra *ExtraConfig) []tls.CertCompressionAlgo {
	if extra == nil || extra.CertCompression == "" {
		return nil
	}
	switch strings.ToLower(extra.CertCompression) {
	case "zlib":
		return []tls.CertCompressionAlgo{tls.CertCompressionZlib}
	case "brotli":
		return []tls.CertCompressionAlgo{tls.CertCompressionBrotli}
	case "zstd":
		return []tls.CertCompressionAlgo{tls.CertCompressionZstd}
	default:
		return nil
	}
}

func containsUint16(values []uint16, target uint16) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// defaultSignatureAlgorithms 默认签名算法列表
func defaultSignatureAlgorithms() []tls.SignatureScheme {
	return []tls.SignatureScheme{
		tls.ECDSAWithP256AndSHA256,
		tls.PSSWithSHA256,
		tls.PKCS1WithSHA256,
		tls.ECDSAWithP384AndSHA384,
		tls.PSSWithSHA384,
		tls.PKCS1WithSHA384,
		tls.PSSWithSHA512,
		tls.PKCS1WithSHA512,
		tls.PKCS1WithSHA1,
	}
}

// parseUint16List 解析 "-" 分隔的 uint16 列表
func parseUint16List(s string) ([]uint16, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, "-")
	result := make([]uint16, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid uint16: %s", p)
		}
		result = append(result, uint16(v))
	}
	return result, nil
}

// parseUint8List 解析 "-" 分隔的 uint8 列表
func parseUint8List(s string) ([]uint8, error) {
	if s == "" {
		return []uint8{0}, nil // 默认 uncompressed
	}
	parts := strings.Split(s, "-")
	result := make([]uint8, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseUint(p, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid uint8: %s", p)
		}
		result = append(result, uint8(v))
	}
	return result, nil
}
