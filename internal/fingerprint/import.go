package fingerprint

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseConfigText 支持普通 FingerprintConfig，也支持从检测站复制出的 JSON。
func ParseConfigText(value string) (*FingerprintConfig, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("fingerprint config text is required")
	}

	if config, ok, err := parseCapturedJSON(value); ok || err != nil {
		return config, err
	}

	var config FingerprintConfig
	if err := yaml.Unmarshal([]byte(value), &config); err != nil {
		return nil, err
	}
	return config.Canonical(), nil
}

type capturedFingerprintJSON struct {
	HTTPVersion string             `json:"http_version"`
	Method      string             `json:"method"`
	UserAgent   string             `json:"user_agent"`
	TLS         *capturedTLSJSON   `json:"tls"`
	HTTP2       *capturedHTTP2JSON `json:"http2"`
}

type capturedTLSJSON struct {
	Ciphers              []string               `json:"ciphers"`
	Extensions           []capturedTLSExtension `json:"extensions"`
	TLSVersionRecord     string                 `json:"tls_version_record"`
	TLSVersionNegotiated string                 `json:"tls_version_negotiated"`
	JA3                  string                 `json:"ja3"`
	JA4                  string                 `json:"ja4"`
	JA4R                 string                 `json:"ja4_r"`
	Peetprint            string                 `json:"peetprint"`
}

type capturedTLSExtension struct {
	Name                     string   `json:"name"`
	Protocols                []string `json:"protocols"`
	Versions                 []string `json:"versions"`
	SignatureAlgorithms      []string `json:"signature_algorithms"`
	SupportedGroups          []string `json:"supported_groups"`
	EllipticCurvePointFormat []string `json:"elliptic_curves_point_formats"`
}

type capturedHTTP2JSON struct {
	AkamaiFingerprint string               `json:"akamai_fingerprint"`
	SentFrames        []capturedHTTP2Frame `json:"sent_frames"`
}

type capturedHTTP2Frame struct {
	FrameType string                 `json:"frame_type"`
	Settings  []string               `json:"settings"`
	Increment json.Number            `json:"increment"`
	Headers   []string               `json:"headers"`
	Priority  *capturedHTTP2Priority `json:"priority"`
}

type capturedHTTP2Priority struct {
	Weight    json.Number `json:"weight"`
	DependsOn json.Number `json:"depends_on"`
	Exclusive any         `json:"exclusive"`
}

func parseCapturedJSON(value string) (*FingerprintConfig, bool, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()

	var captured capturedFingerprintJSON
	if err := decoder.Decode(&captured); err != nil {
		return nil, false, nil
	}
	if captured.TLS == nil && captured.HTTP2 == nil && captured.HTTPVersion == "" && captured.Method == "" && captured.UserAgent == "" {
		return nil, false, nil
	}

	config := &FingerprintConfig{
		HTTPVersion: captured.HTTPVersion,
		Method:      captured.Method,
		UserAgent:   captured.UserAgent,
	}

	if captured.TLS != nil {
		config.JA3 = strings.TrimSpace(captured.TLS.JA3)
		config.JA4 = strings.TrimSpace(captured.TLS.JA4R)
		if config.JA4 == "" {
			if _, err := parseJA4Material(captured.TLS.JA4); err == nil {
				config.JA4 = strings.TrimSpace(captured.TLS.JA4)
			}
		}

		config.TLS = &TLSDetailsConfig{
			TLSUsed:    firstNonEmpty(captured.TLS.TLSVersionNegotiated, captured.TLS.TLSVersionRecord),
			Ciphers:    captured.TLS.Ciphers,
			Extensions: extensionNames(captured.TLS.Extensions),
		}

		for _, ext := range captured.TLS.Extensions {
			name := strings.ToLower(ext.Name)
			switch {
			case strings.Contains(name, "application_layer_protocol_negotiation"):
				config.TLS.Protocols = append(config.TLS.Protocols, ext.Protocols...)
			case strings.Contains(name, "application_settings") && len(config.TLS.Protocols) == 0:
				config.TLS.Protocols = append(config.TLS.Protocols, ext.Protocols...)
			case strings.Contains(name, "supported_versions"):
				config.TLS.SupportedVersions = append(config.TLS.SupportedVersions, ext.Versions...)
			case strings.Contains(name, "supported_groups"):
				config.TLS.Curves = append(config.TLS.Curves, ext.SupportedGroups...)
			case strings.Contains(name, "signature_algorithms"):
				config.TLS.SignatureAlgorithms = append(config.TLS.SignatureAlgorithms, ext.SignatureAlgorithms...)
			}
		}
	}

	if captured.HTTP2 != nil {
		config.Akamai = strings.TrimSpace(captured.HTTP2.AkamaiFingerprint)
		config.HTTP2 = &HTTP2DetailsConfig{}
		for _, frame := range captured.HTTP2.SentFrames {
			switch strings.ToUpper(strings.TrimSpace(frame.FrameType)) {
			case "SETTINGS":
				config.HTTP2.Settings = append(config.HTTP2.Settings, frame.Settings...)
			case "WINDOW_UPDATE":
				if frame.Increment != "" {
					config.HTTP2.WindowUpdate = frame.Increment.String()
				}
			case "HEADERS":
				config.HTTP2.HeaderLines = append(config.HTTP2.HeaderLines, frame.Headers...)
				config.HTTP2.Headers = append(config.HTTP2.Headers, headerNamesFromLines(frame.Headers)...)
				if frame.Priority != nil {
					config.HTTP2.Priority = convertCapturedPriority(frame.Priority)
				}
			}
		}
	}

	config = config.Canonical()
	if config.Preset == "" && config.JA3 == "" && config.JA4 == "" && config.Akamai == "" && config.TLS == nil && config.HTTP2 == nil {
		return nil, true, fmt.Errorf("captured fingerprint JSON has no usable fingerprint fields")
	}
	return config, true, nil
}

func extensionNames(extensions []capturedTLSExtension) []string {
	names := make([]string, 0, len(extensions))
	for _, ext := range extensions {
		if name := strings.TrimSpace(ext.Name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func headerNamesFromLines(lines []string) []string {
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		name, _, ok := splitHTTP2HeaderLine(line)
		if !ok {
			continue
		}
		name = normalizeHTTP2OrderName(name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func splitHTTP2HeaderLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	if strings.HasPrefix(line, ":") {
		idx := strings.Index(line[1:], ":")
		if idx < 0 {
			return "", "", false
		}
		idx++
		return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
	}
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}

func normalizeHTTP2OrderName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimPrefix(name, ":")
	switch name {
	case "m":
		return "method"
	case "a":
		return "authority"
	case "s":
		return "scheme"
	case "p":
		return "path"
	default:
		return name
	}
}

func convertCapturedPriority(priority *capturedHTTP2Priority) *HTTP2PriorityConfig {
	if priority == nil {
		return nil
	}
	weight64, _ := strconv.ParseUint(priority.Weight.String(), 10, 8)
	depends64, _ := strconv.ParseUint(priority.DependsOn.String(), 10, 32)
	return &HTTP2PriorityConfig{
		Weight:    uint8(weight64),
		DependsOn: uint32(depends64),
		Exclusive: parseFlexibleBool(priority.Exclusive),
	}
}

func parseFlexibleBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case json.Number:
		parsed, _ := strconv.ParseInt(typed.String(), 10, 64)
		return parsed != 0
	case float64:
		return typed != 0
	case string:
		typed = strings.ToLower(strings.TrimSpace(typed))
		return typed == "1" || typed == "true" || typed == "yes"
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
