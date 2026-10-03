package application

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func sameSettings(a, b domain.ManagedSettings) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func (service *Service) RuntimeSettings(context.Context) (domain.ManagedSettings, error) {
	return service.activeSettings(), nil
}

// ControlAccess is also used for the temporary entry; never trust arbitrary forwarded headers.
func (service *Service) ControlAccess(ctx context.Context, address string) error {
	service.policyMu.RLock()
	defer service.policyMu.RUnlock()
	settings := service.activeSettings()
	if pending, err := service.repository.Pending(ctx); err == nil {
		if settings.ConsoleLANOnly != pending.Settings.ConsoleLANOnly || settings.Origin != pending.Settings.Origin || (settings.ConsoleLANOnly && !sameSettings(settings, pending.Settings)) {
			return &domain.AppError{Status: 503, Code: "policy_pending", Message: "访问策略正在发布或等待核对，请稍后重试"}
		}
	} else if !domain.IsMissing(err) {
		return err
	}
	if !settings.ConsoleLANOnly {
		return nil
	}
	ip, err := netip.ParseAddr(address)
	if err == nil {
		for _, cidr := range settings.LAN {
			prefix, parseErr := netip.ParsePrefix(cidr)
			if parseErr == nil && prefix.Contains(ip.Unmap()) {
				return nil
			}
		}
	}
	return &domain.AppError{Status: 403, Code: "network_restricted", Message: "控制台仅允许配置的 LAN/VPN 网络访问"}
}

func (service *Service) SaveSettings(ctx context.Context, revision int64, candidate domain.ManagedSettings, confirmExposure bool, actor, clientAddress string) (domain.Draft, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if _, err := service.repository.Pending(ctx); err == nil {
		return domain.Draft{}, domain.Conflict("已有发布待完成或核对")
	} else if !domain.IsMissing(err) {
		return domain.Draft{}, err
	}
	draft, err := service.repository.Draft(ctx)
	if err != nil {
		return domain.Draft{}, err
	}
	if draft.Revision != revision {
		return domain.Draft{}, domain.Conflict("草稿已变化，请刷新设置")
	}
	active := service.activeSettings()
	candidate.AdminDomain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(candidate.AdminDomain), "."))
	if candidate.AdminDomain != active.AdminDomain {
		if active.PreviousAdminDomain != "" {
			return domain.Draft{}, domain.Conflict("请先完成当前控制台地址交接")
		}
		candidate.Origin = "https://" + candidate.AdminDomain
		candidate.PreviousAdminDomain, candidate.PreviousOrigin = active.AdminDomain, active.Origin
	} else {
		candidate.Origin = active.Origin
		candidate.PreviousAdminDomain, candidate.PreviousOrigin = active.PreviousAdminDomain, active.PreviousOrigin
		if draft.Settings.AdminDomain == active.AdminDomain {
			candidate.PreviousAdminDomain, candidate.PreviousOrigin = draft.Settings.PreviousAdminDomain, draft.Settings.PreviousOrigin
		}
	}
	for i := range candidate.Domains {
		candidate.Domains[i].Name = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(candidate.Domains[i].Name), "*."), "."))
		if candidate.Domains[i].ID == "" {
			candidate.Domains[i].ID = domain.ID()
		}
	}
	if err = domain.ValidateManagedSettings(candidate); err != nil {
		return domain.Draft{}, domain.Invalid("设置无效：" + err.Error())
	}
	exposes := trustedNetworksWiden(active, candidate) || (active.ConsoleLANOnly && !candidate.ConsoleLANOnly)
	for _, current := range candidate.Domains {
		if current.Access == nil || *current.Access != "internet" {
			continue
		}
		found := false
		for _, old := range active.Domains {
			if old.ID == current.ID {
				found = true
			}
			if old.ID == current.ID && (old.Access == nil || *old.Access != "internet") {
				exposes = true
			}
		}
		if !found {
			exposes = true
		}
	}
	if exposes && !confirmExposure {
		return domain.Draft{}, domain.Invalid("请明确确认放开访问范围")
	}
	if candidate.ConsoleLANOnly {
		ip, parseErr := netip.ParseAddr(clientAddress)
		allowed := false
		for _, cidr := range candidate.LAN {
			if p, e := netip.ParsePrefix(cidr); e == nil && parseErr == nil && p.Contains(ip.Unmap()) {
				allowed = true
			}
		}
		if !allowed {
			return domain.Draft{}, domain.Invalid("新规则会阻止当前设备访问控制台，请从目标网络登录后再启用")
		}
	}
	_, published, err := service.expected(ctx)
	if err != nil {
		return domain.Draft{}, err
	}
	for _, list := range [][]domain.Service{draft.Services, published} {
		for _, item := range list {
			oldName, newName := "", ""
			for _, d := range active.Domains {
				if d.ID == item.DomainID {
					oldName = d.Name
				}
			}
			for _, d := range draft.Settings.Domains {
				if d.ID == item.DomainID {
					oldName = d.Name
				}
			}
			for _, d := range candidate.Domains {
				if d.ID == item.DomainID {
					newName = d.Name
				}
			}
			if newName == "" || oldName != newName {
				return domain.Draft{}, domain.Invalid("域名仍被草稿或已发布服务引用：" + item.Hostname)
			}
			if item.Hostname == candidate.AdminDomain || item.Hostname == candidate.PreviousAdminDomain {
				return domain.Draft{}, domain.Invalid("控制台地址已被业务服务使用")
			}
		}
	}
	return service.repository.SaveSettings(ctx, revision, candidate, actor)
}

func (service *Service) CompleteConsoleTransition(ctx context.Context, revision int64, confirm bool, origin, actor string) (domain.Draft, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	active := service.activeSettings()
	if !confirm || active.PreviousAdminDomain == "" || origin != active.Origin {
		return domain.Draft{}, domain.Invalid("请在新控制台登录并明确确认完成交接")
	}
	if service.setupProbe == nil || !service.setupProbe.ExternalConsole(ctx, active.Origin, service.options.ProbeAddress) {
		return domain.Draft{}, domain.Conflict("新控制台 HTTPS 尚未通过验证")
	}
	if _, err := service.repository.Pending(ctx); err == nil {
		return domain.Draft{}, domain.Conflict("已有发布待完成或核对")
	} else if !domain.IsMissing(err) {
		return domain.Draft{}, err
	}
	draft, err := service.repository.Draft(ctx)
	if err != nil {
		return domain.Draft{}, err
	}
	if draft.Revision != revision {
		return domain.Draft{}, domain.Conflict("草稿已变化，请重新确认")
	}
	if draft.Settings.AdminDomain != active.AdminDomain || draft.Settings.Origin != active.Origin {
		return domain.Draft{}, domain.Conflict("控制台草稿地址已变化，请先核对当前交接")
	}
	draft.Settings.PreviousAdminDomain, draft.Settings.PreviousOrigin = "", ""
	return service.repository.SaveSettings(ctx, revision, draft.Settings, actor)
}

func consoleSourceAllowed(settings domain.ManagedSettings, address string) bool {
	if !settings.ConsoleLANOnly {
		return true
	}
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return false
	}
	for _, cidr := range settings.LAN {
		if p, e := netip.ParsePrefix(cidr); e == nil && p.Contains(ip.Unmap()) {
			return true
		}
	}
	return false
}

func widensAccess(before, after domain.ManagedSettings, oldServices, newServices []domain.Service) bool {
	if trustedNetworksWiden(before, after) {
		return true
	}
	if before.ConsoleLANOnly && !after.ConsoleLANOnly {
		return true
	}
	for _, d := range after.Domains {
		if d.Access == nil || *d.Access != "internet" {
			continue
		}
		old, found := before.Domain(d.ID)
		if !found || old.Access == nil || *old.Access != "internet" {
			return true
		}
		for _, item := range newServices {
			if !item.Enabled || item.DomainID != d.ID {
				continue
			}
			unchanged := false
			for _, oldItem := range oldServices {
				if oldItem.ID == item.ID && oldItem.Enabled && oldItem.DomainID == item.DomainID && oldItem.Hostname == item.Hostname {
					unchanged = true
				}
			}
			if !unchanged {
				return true
			}
		}
	}
	return false
}

func trustedNetworksWiden(before, after domain.ManagedSettings) bool {
	restricted := before.ConsoleLANOnly && after.ConsoleLANOnly
	for _, old := range before.Domains {
		if old.Access != nil && *old.Access == "trusted" {
			if next, exists := after.Domain(old.ID); exists && next.Access != nil && *next.Access == "trusted" {
				restricted = true
			}
		}
	}
	if !restricted {
		return false
	}
	for _, next := range after.LAN {
		p, err := netip.ParsePrefix(next)
		if err != nil {
			continue
		}
		contained := false
		for _, prev := range before.LAN {
			q, e := netip.ParsePrefix(prev)
			if e == nil && q.Addr().BitLen() == p.Addr().BitLen() && q.Bits() <= p.Bits() && q.Contains(p.Masked().Addr()) {
				contained = true
			}
		}
		if !contained {
			return true
		}
	}
	return false
}
