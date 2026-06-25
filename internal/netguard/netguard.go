// Package netguard 提供针对 SSRF 的目标地址校验与受控拨号包装。
//
// 默认拒绝拨向以下范围：回环、私有网段（10/8、172.16/12、192.168/16）、
// 链路本地（169.254/16）、CGNAT（100.64/10）、保留段，以及 IPv6 同类。
// 通过 AllowPrivate=true 可关闭防护。
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"chijie/internal/dnsresolver"
)

// ErrBlockedAddress 表示目标命中私网/保留段黑名单。
var ErrBlockedAddress = errors.New("target address is in a blocked range (private, loopback or reserved)")

// IsBlockedIP 判断给定 IP 是否落在被禁止的地址范围内。
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsPrivate() {
		return true
	}
	// CGNAT 100.64.0.0/10
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1]&0xC0 == 64 {
			return true
		}
		// 0.0.0.0/8 保留
		if v4[0] == 0 {
			return true
		}
		// 240.0.0.0/4 保留
		if v4[0] >= 240 {
			return true
		}
	}
	return false
}

// CheckHost 把 host 解析为 IP（若已是 IP 则直接判断）并对所有解析结果做黑名单检查。
// 任意一个 IP 命中黑名单即返回 ErrBlockedAddress，防止 DNS rebinding 绕过。
func CheckHost(ctx context.Context, host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("host is empty")
	}
	if ip := net.ParseIP(host); ip != nil {
		if IsBlockedIP(ip) {
			return ErrBlockedAddress
		}
		return nil
	}
	addrs, err := dnsresolver.Resolver().LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve host: %w", err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("no addresses for host: %s", host)
	}
	for _, addr := range addrs {
		if IsBlockedIP(addr.IP) {
			return ErrBlockedAddress
		}
	}
	return nil
}

// DialContextFunc 是 net.Dialer.DialContext 的兼容签名。
type DialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Guard 把任意 DialContext 包装成"先校验目标 IP，再拨号"的版本。
// allowPrivate=true 时直接透传 base，不做校验。
//
// 注意：本守卫只在直连/直拨场景生效。当出口是 SOCKS5/HTTP Proxy 等代理协议时，
// 真正的目标解析在远端代理上完成，此处只能校验请求声明的目标。
func Guard(base DialContextFunc, allowPrivate bool) DialContextFunc {
	if allowPrivate {
		return base
	}
	if base == nil {
		base = (&net.Dialer{}).DialContext
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		if err := CheckHost(ctx, host); err != nil {
			return nil, err
		}
		return base(ctx, network, addr)
	}
}
