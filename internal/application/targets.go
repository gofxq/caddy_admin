package application

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func (service *Service) endpointIPs(ctx context.Context, endpoint string) ([]netip.Addr, error) {
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		return nil, &domain.AppError{Status: 503, Code: "system_resolution", Message: "系统目标地址无效"}
	}
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		return []netip.Addr{ip.Unmap()}, nil
	}
	lookupContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	addresses, err := service.resolver.LookupNetIP(lookupContext, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, &domain.AppError{Status: 503, Code: "system_resolution", Message: "无法确认系统目标地址，请检查 Manager/Caddy DNS"}
	}
	for index := range addresses {
		if !addresses[index].IsValid() {
			return nil, &domain.AppError{Status: 503, Code: "system_resolution", Message: "系统目标解析无效"}
		}
		addresses[index] = addresses[index].Unmap()
	}
	return addresses, nil
}

func (service *Service) targetPolicy(ctx context.Context) (domain.TargetPolicy, error) {
	return service.targetPolicyForSettings(ctx, service.options.RuntimePolicy)
}

func (service *Service) targetPolicyForSettings(ctx context.Context, settings domain.ManagedSettings) (domain.TargetPolicy, error) {
	reserved := []string{}
	addresses, err := service.localAddresses()
	if err != nil {
		return domain.TargetPolicy{}, &domain.AppError{Status: 503, Code: "system_resolution", Message: "无法确认本机系统地址"}
	}
	for _, address := range addresses {
		ip, _, parseErr := net.ParseCIDR(address.String())
		if parseErr != nil {
			return domain.TargetPolicy{}, &domain.AppError{Status: 503, Code: "system_resolution", Message: "无法确认本机系统地址"}
		}
		reserved = append(reserved, ip.String())
	}
	endpoints := []string{service.options.ManagerDial, service.options.ProbeAddress}
	if service.options.AdminURL != "" {
		if parsed, parseErr := url.Parse(service.options.AdminURL); parseErr == nil {
			endpoints = append(endpoints, parsed.Host)
		}
	}
	for _, endpoint := range endpoints {
		addresses, resolveErr := service.endpointIPs(ctx, endpoint)
		if resolveErr != nil {
			return domain.TargetPolicy{}, resolveErr
		}
		for _, address := range addresses {
			reserved = append(reserved, address.String())
		}
	}
	return domain.TargetPolicy{PublicDomain: settings.PublicDomain, HomelabDomain: settings.HomelabDomain, AdminDomain: settings.AdminDomain, UpstreamCIDRs: settings.UpstreamCIDRs, AllowedNames: settings.AllowedNames, ReservedIPs: reserved, DeniedIPs: settings.DeniedIPs, SystemEndpoints: endpoints, Resolver: service.resolver}, nil
}

func (service *Service) ClientAddress(ctx context.Context, remoteAddress, forwardedAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return "unknown"
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	forwarded, err := netip.ParseAddr(forwardedAddress)
	if err != nil {
		return peer.Unmap().String()
	}
	addresses, err := service.endpointIPs(ctx, service.options.ProbeAddress)
	if err == nil {
		for _, address := range addresses {
			if address.Unmap() == peer.Unmap() {
				return forwarded.Unmap().String()
			}
		}
	}
	return peer.Unmap().String()
}
