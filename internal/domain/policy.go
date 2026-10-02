package domain

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type TargetResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type TargetPolicy struct {
	PublicDomain    string
	HomelabDomain   string
	AdminDomain     string
	UpstreamCIDRs   []string
	AllowedNames    []string
	ReservedIPs     []string
	DeniedIPs       []string
	SystemEndpoints []string
	Resolver        TargetResolver
}

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func ValidDomain(value string) bool {
	if len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !labelPattern.MatchString(label) {
			return false
		}
	}
	return value != ""
}

func OneLevel(host, base string) bool {
	return strings.HasSuffix(host, "."+base) && labelPattern.MatchString(strings.TrimSuffix(host, "."+base))
}

func ValidateManagedSettings(settings ManagedSettings) error {
	if err := ValidateManagedNetwork(settings); err != nil {
		return err
	}
	for _, value := range []string{settings.HomelabDomain, settings.AdminDomain} {
		if !ValidDomain(value) {
			return fmt.Errorf("invalid domain %q", value)
		}
	}
	if settings.PublicDomain != "" && (!ValidDomain(settings.PublicDomain) || settings.PublicDomain == settings.HomelabDomain) {
		return fmt.Errorf("invalid Public domain")
	}
	if !OneLevel(settings.AdminDomain, settings.HomelabDomain) {
		return fmt.Errorf("invalid domain groups or admin domain")
	}
	parsed, err := url.Parse(settings.Origin)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != settings.AdminDomain || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("origin must be the HTTPS admin origin")
	}
	if len(settings.Resolvers) == 0 {
		return fmt.Errorf("resolvers must not be empty")
	}
	for _, resolver := range settings.Resolvers {
		if _, err := netip.ParseAddr(resolver); err == nil {
			continue
		}
		host, port, err := net.SplitHostPort(resolver)
		if err != nil || net.ParseIP(host) == nil || !validPort(port) {
			return fmt.Errorf("invalid resolver")
		}
	}
	return nil
}

// ValidateManagedNetwork shares the hard network rules with the staged Setup
// preflight, before the user has entered DNS settings.
func ValidateManagedNetwork(settings ManagedSettings) error {
	for _, values := range [][]string{settings.LAN, settings.UpstreamCIDRs} {
		if len(values) == 0 {
			return fmt.Errorf("network allowlist must not be empty")
		}
		for _, value := range values {
			if _, err := netip.ParsePrefix(value); err != nil {
				return fmt.Errorf("invalid network %q", value)
			}
		}
	}
	for _, value := range settings.DeniedIPs {
		if _, err := netip.ParseAddr(value); err != nil {
			return fmt.Errorf("invalid denied IP")
		}
	}
	for _, name := range settings.AllowedNames {
		if !ValidDomain(name) {
			return fmt.Errorf("invalid allowed upstream name")
		}
	}
	return nil
}

func validPort(value string) bool {
	port, err := strconv.Atoi(value)
	return err == nil && port >= 1 && port <= 65535
}

func NormalizeService(ctx context.Context, policy TargetPolicy, service Service) (Service, error) {
	service.Name = strings.TrimSpace(service.Name)
	service.Hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(service.Hostname), "."))
	service.Host = strings.ToLower(strings.TrimSpace(service.Host))
	service.Dial = ""
	if service.Name == "" || len(service.Name) > 100 || len(service.Notes) > 2000 {
		return service, Invalid("名称必填且不超过 100 字节，备注不超过 2000 字节")
	}
	base := policy.PublicDomain
	if service.Group == "homelab" {
		base = policy.HomelabDomain
	} else if service.Group != "public" {
		return service, Invalid("无效分组")
	} else if base == "" {
		return service, Invalid("Public 域名尚未配置")
	}
	if !OneLevel(service.Hostname, base) || service.Hostname == policy.AdminDomain {
		return service, Invalid("域名必须为分组下的一层子域且不能占用控制台")
	}
	if service.Scheme != "http" && service.Scheme != "https" {
		return service, Invalid("上游协议必须为 http 或 https")
	}
	if service.Port < 1 || service.Port > 65535 {
		return service, Invalid("端口必须为 1–65535")
	}
	for _, endpoint := range policy.SystemEndpoints {
		host, _, err := net.SplitHostPort(endpoint)
		if err == nil && service.Host == strings.ToLower(host) {
			return service, Invalid("上游不能指向管理器或 Caddy 控制入口")
		}
	}
	if service.Host == policy.AdminDomain {
		return service, Invalid("上游不能指向控制台域名")
	}
	var addresses []netip.Addr
	if address, err := netip.ParseAddr(service.Host); err == nil {
		addresses = []netip.Addr{address.Unmap()}
	} else {
		allowed := false
		for _, name := range policy.AllowedNames {
			if name == service.Host {
				allowed = true
				break
			}
		}
		if !allowed || !ValidDomain(service.Host) {
			return service, Invalid("上游服务名未明确许可")
		}
		resolver := policy.Resolver
		if resolver == nil {
			resolver = net.DefaultResolver
		}
		found, err := resolver.LookupNetIP(ctx, "ip", service.Host)
		if err != nil || len(found) == 0 {
			return service, Invalid("无法解析上游服务名")
		}
		addresses = found
	}
	for _, address := range addresses {
		if !allowedIP(policy, address.Unmap()) {
			return service, Invalid("上游地址不在允许范围或属于受保护目标")
		}
	}
	sort.Slice(addresses, func(i, j int) bool { return addresses[i].Compare(addresses[j]) < 0 })
	service.Dial = net.JoinHostPort(addresses[0].Unmap().String(), strconv.Itoa(service.Port))
	return service, nil
}

func ValidateUniqueServices(services []Service) error {
	seen := map[string]bool{}
	for _, service := range services {
		if seen[service.Hostname] {
			return Invalid(fmt.Sprintf("域名 %s 已存在", service.Hostname))
		}
		seen[service.Hostname] = true
	}
	return nil
}

func allowedIP(policy TargetPolicy, address netip.Addr) bool {
	if !address.IsValid() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, value := range []string{"169.254.0.0/16", "100.100.100.200/32", "fd00:ec2::254/128"} {
		if netip.MustParsePrefix(value).Contains(address) {
			return false
		}
	}
	for _, value := range append(append([]string{}, policy.DeniedIPs...), policy.ReservedIPs...) {
		if denied, err := netip.ParseAddr(value); err == nil && denied.Unmap() == address {
			return false
		}
	}
	for _, value := range policy.UpstreamCIDRs {
		if prefix, err := netip.ParsePrefix(value); err == nil && prefix.Contains(address) {
			return true
		}
	}
	return false
}
