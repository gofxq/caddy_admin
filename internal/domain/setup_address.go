package domain

import (
	"net/netip"
	"strings"
)

// Private entrances are valid; this does not assert public reachability.
func NormalizeSetupAddress(value string) (string, error) {
	ip, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || ip.Zone() != "" || ip.Is4In6() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || !ip.IsGlobalUnicast() {
		return "", Invalid("请明确填写可访问的 Caddy IPv4 或 IPv6 地址，不含协议或端口；不能使用未指定、回环、链路本地或组播地址")
	}
	return ip.String(), nil
}
