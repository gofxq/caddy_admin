package application

import (
	"context"
	"net/netip"
	"sort"
	"strings"

	"github.com/gofxq/caddy_admin/internal/domain"
)

// Portal reads one successful deployment so links and access policy share a version.
// It does not probe Caddy or upstreams and does not expose draft configuration.
func (service *Service) Portal(ctx context.Context, address string) ([]domain.PortalService, error) {
	// Begin, Apply and Recover hold mu; never queue anonymous reads behind a reload.
	if !service.mu.TryLock() {
		return nil, &domain.AppError{Status: 503, Code: "policy_pending", Message: "配置正在更新，请稍后重试"}
	}
	defer service.mu.Unlock()
	service.policyMu.RLock()
	defer service.policyMu.RUnlock()
	if _, err := service.repository.Pending(ctx); err == nil {
		return nil, &domain.AppError{Status: 503, Code: "policy_pending", Message: "配置正在发布或等待核对，请稍后重试"}
	} else if !domain.IsMissing(err) {
		return nil, err
	}
	result := []domain.PortalService{}
	deployment, err := service.repository.Latest(ctx)
	if domain.IsMissing(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	settings := deployment.Settings
	if !consoleSourceAllowed(settings, address) {
		return nil, &domain.AppError{Status: 403, Code: "network_restricted", Message: "控制台仅允许配置的 LAN/VPN 网络访问"}
	}
	trusted := false
	if ip, err := netip.ParseAddr(address); err == nil {
		for _, cidr := range settings.LAN {
			if prefix, err := netip.ParsePrefix(cidr); err == nil && prefix.Contains(ip.Unmap()) {
				trusted = true
				break
			}
		}
	}
	for _, item := range deployment.Services {
		if !item.Enabled {
			continue
		}
		d, found := settings.Domain(item.DomainID)
		if !found || d.Access == nil || !domain.OneLevel(item.Hostname, d.Name) || item.Hostname == settings.AdminDomain || item.Hostname == settings.PreviousAdminDomain {
			continue
		}
		if *d.Access != "internet" && (*d.Access != "trusted" || !trusted) {
			continue
		}
		result = append(result, domain.PortalService{Name: item.Name, Hostname: item.Hostname, URL: "https://" + item.Hostname})
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := strings.ToLower(result[i].Name), strings.ToLower(result[j].Name)
		if a == b {
			return result[i].Hostname < result[j].Hostname
		}
		return a < b
	})
	return result, nil
}
