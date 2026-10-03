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
	Domains             []ManagedDomain
	PreviousAdminDomain string
	AdminDomain         string
	UpstreamCIDRs       []string
	AllowedNames        []string
	ReservedIPs         []string
	DeniedIPs           []string
	SystemEndpoints     []string
	Resolver            TargetResolver
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
	if len(settings.Domains) == 0 {
		return fmt.Errorf("至少登记一个域名")
	}
	ids := map[string]bool{}
	adminFound, previousFound := false, settings.PreviousAdminDomain == ""
	for i, d := range settings.Domains {
		if d.ID == "" || ids[d.ID] || !ValidDomain(d.Name) {
			return fmt.Errorf("无效或重复域名")
		}
		ids[d.ID] = true
		if d.Access != nil && *d.Access != "trusted" && *d.Access != "internet" {
			return fmt.Errorf("无效访问范围")
		}
		if d.Access != nil && *d.Access == "trusted" && len(settings.LAN) == 0 {
			return fmt.Errorf("仅可信网络的域名需要可信网段")
		}
		for _, other := range settings.Domains[:i] {
			if d.Name == other.Name || strings.HasSuffix(d.Name, "."+other.Name) || strings.HasSuffix(other.Name, "."+d.Name) {
				return fmt.Errorf("域名重复或重叠")
			}
		}
		adminFound = adminFound || OneLevel(settings.AdminDomain, d.Name)
		previousFound = previousFound || OneLevel(settings.PreviousAdminDomain, d.Name)
	}
	if !adminFound || !previousFound {
		return fmt.Errorf("控制台必须是登记域名的一层子域")
	}
	validateOrigin := func(origin, host string) bool {
		parsed, err := url.Parse(origin)
		return err == nil && parsed.Scheme == "https" && parsed.Hostname() == host && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == "" && (parsed.Port() == "" || validPort(parsed.Port()))
	}
	if !validateOrigin(settings.Origin, settings.AdminDomain) {
		return fmt.Errorf("origin must be the HTTPS admin origin")
	}
	if (settings.PreviousAdminDomain == "") != (settings.PreviousOrigin == "") || (settings.PreviousAdminDomain != "" && (!validateOrigin(settings.PreviousOrigin, settings.PreviousAdminDomain) || settings.PreviousAdminDomain == settings.AdminDomain)) {
		return fmt.Errorf("无效控制台交接地址")
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
	if settings.ConsoleLANOnly && len(settings.LAN) == 0 {
		return fmt.Errorf("控制台来源限制需要可信网段")
	}
	for _, values := range [][]string{settings.LAN, settings.UpstreamCIDRs} {

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
	base := ""
	for _, d := range policy.Domains {
		if d.ID == service.DomainID {
			base = d.Name
			break
		}
	}
	if base == "" || !OneLevel(service.Hostname, base) || service.Hostname == policy.AdminDomain || (policy.PreviousAdminDomain != "" && service.Hostname == policy.PreviousAdminDomain) {
		return service, Invalid("域名必须为所选域名的一层子域且不能占用控制台")
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
	if service.Host == policy.AdminDomain || (policy.PreviousAdminDomain != "" && service.Host == policy.PreviousAdminDomain) {
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
