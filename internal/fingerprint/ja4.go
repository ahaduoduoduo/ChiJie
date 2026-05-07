package fingerprint

import (
	"fmt"
	"strconv"
	"strings"

	tls "github.com/refraction-networking/utls"
)

type ja4Material struct {
	A            string
	CipherSuites []uint16
	ExtensionIDs []uint16
	SignatureIDs []uint16
}

func ja4ToSpec(value string, extra *ExtraConfig) (*tls.ClientHelloSpec, error) {
	material, err := parseJA4Material(value)
	if err != nil {
		return nil, err
	}

	extIDs := append([]uint16(nil), material.ExtensionIDs...)
	if alpnProtocols := ja4ALPNProtocols(material.A); len(alpnProtocols) > 0 && !containsUint16(extIDs, 16) {
		extIDs = append(extIDs, 16)
	}

	sigAlgs := defaultSignatureAlgorithms()
	if len(material.SignatureIDs) > 0 {
		sigAlgs = make([]tls.SignatureScheme, 0, len(material.SignatureIDs))
		for _, id := range material.SignatureIDs {
			sigAlgs = append(sigAlgs, tls.SignatureScheme(id))
		}
	}

	tlsVersMin, tlsVersMax := ja4TLSVersionRange(material.A)
	extensions := buildExtensionsWithSignatureAlgorithms(extIDs, defaultJA4Curves(), []uint8{0}, extra, sigAlgs)

	return &tls.ClientHelloSpec{
		TLSVersMin:         tlsVersMin,
		TLSVersMax:         tlsVersMax,
		CipherSuites:       material.CipherSuites,
		CompressionMethods: []uint8{0},
		Extensions:         extensions,
	}, nil
}

func parseJA4Material(value string) (*ja4Material, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "ja4:") {
		value = strings.TrimSpace(value[len("ja4:"):])
	}
	if value == "" {
		return nil, fmt.Errorf("JA4 value is required")
	}

	parts := strings.Split(value, "_")
	if len(parts) != 3 && len(parts) != 4 {
		return nil, fmt.Errorf("invalid JA4 raw format: expected JA4_a_cipher-list_extension-list[_signature-list]")
	}

	a := strings.ToLower(strings.TrimSpace(parts[0]))
	if err := validateJA4A(a); err != nil {
		return nil, err
	}

	ciphers, err := parseJA4HexList(parts[1], "cipher suites")
	if err != nil {
		return nil, err
	}
	extensions, err := parseJA4HexList(parts[2], "extensions")
	if err != nil {
		return nil, err
	}
	signatures := []uint16(nil)
	if len(parts) == 4 {
		signatures, err = parseJA4HexList(parts[3], "signature algorithms")
		if err != nil {
			return nil, err
		}
	}

	return &ja4Material{
		A:            a,
		CipherSuites: ciphers,
		ExtensionIDs: extensions,
		SignatureIDs: signatures,
	}, nil
}

func validateJA4A(value string) error {
	if len(value) < 8 {
		return fmt.Errorf("invalid JA4_a: %q", value)
	}
	switch value[0] {
	case 't', 'q', 'd':
	default:
		return fmt.Errorf("invalid JA4 protocol: %q", string(value[0]))
	}
	return nil
}

func parseJA4HexList(value string, label string) ([]uint16, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	items := strings.Split(value, ",")
	result := make([]uint16, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(item), "0x"))
		if item == "" {
			continue
		}
		parsed, err := strconv.ParseUint(item, 16, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid JA4 %s value %q: %w", label, item, err)
		}
		result = append(result, uint16(parsed))
	}
	return result, nil
}

func ja4TLSVersionRange(a string) (uint16, uint16) {
	if len(a) < 3 {
		return tls.VersionTLS10, tls.VersionTLS13
	}
	switch a[1:3] {
	case "13":
		return tls.VersionTLS12, tls.VersionTLS13
	case "12":
		return tls.VersionTLS10, tls.VersionTLS12
	case "11":
		return tls.VersionTLS10, tls.VersionTLS11
	case "10":
		return tls.VersionTLS10, tls.VersionTLS10
	default:
		return tls.VersionTLS10, tls.VersionTLS13
	}
}

func ja4ALPNProtocols(a string) []string {
	if len(a) < 10 {
		return nil
	}
	switch a[8:] {
	case "00":
		return nil
	case "h2":
		return []string{"h2", "http/1.1"}
	case "h1":
		return []string{"http/1.1"}
	default:
		return []string{a[8:]}
	}
}

func defaultJA4Curves() []uint16 {
	return []uint16{29, 23, 24}
}
