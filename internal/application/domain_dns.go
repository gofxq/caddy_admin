package application

import (
	"context"
	"github.com/gofxq/caddy_admin/internal/domain"
	"time"
)

func (service *Service) PreviewDomainDNS(ctx context.Context, id, address string) (SetupDNSPlan, error) {
	if service.externalCaddy() || service.options.TestTLS || service.dnsAdapter(service.dnsProvider()) == nil {
		return SetupDNSPlan{}, domain.Invalid("当前模式请自行配置 DNS")
	}
	draft, err := service.repository.Draft(ctx)
	if err != nil {
		return SetupDNSPlan{}, err
	}
	name := ""
	for _, d := range draft.Settings.Domains {
		if d.ID == id {
			name = d.Name
		}
	}
	if name == "" {
		return SetupDNSPlan{}, domain.NotFound("域名不存在，请先保存域名草稿")
	}
	token, err := service.readDNSToken(service.dnsProvider())
	if err != nil {
		return SetupDNSPlan{}, err
	}
	plan, e := service.dnsAdapter(service.dnsProvider()).Preview(ctx, token, name, address)
	plan.Provider = service.dnsProvider()
	return plan, e
}

func (service *Service) ConfirmDomainDNS(ctx context.Context, id, address, fingerprint string, confirm bool, actor string) (SetupDNSPlan, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if !confirm || fingerprint == "" {
		return SetupDNSPlan{}, domain.Invalid("请明确确认 DNS 变更")
	}
	plan, err := service.PreviewDomainDNS(ctx, id, address)
	if err != nil {
		return plan, err
	}
	if service.setupIntent == nil {
		return plan, domain.Invalid("DNS 操作意图存储不可用")
	}
	intent, err := service.setupIntent.Read()
	if err != nil {
		return plan, err
	}
	if plan.Fingerprint != fingerprint && !setupDNSRecovered(intent, plan, fingerprint) {
		return plan, domain.Conflict("DNS 记录已变化，请重新预览并确认")
	}
	if err = checkDNSWriteCooldown(intent, plan); err != nil {
		return plan, err
	}
	if plan.Action != "reuse" {
		if err = service.setupIntent.Write(SetupIntent{Plan: plan, DispatchedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
			return plan, err
		}
		token, e := service.readDNSToken(service.dnsProvider())
		if e != nil {
			return plan, e
		}
		if err = service.dnsAdapter(service.dnsProvider()).Apply(ctx, token, plan); err != nil {
			return plan, service.dnsApplyError(plan, err)
		}
		if plan.Provider == domain.DNSProviderDNSPod {
			verified, e := service.PreviewDomainDNS(ctx, id, address)
			if e != nil {
				return plan, e
			}
			if verified.Action != "reuse" {
				return plan, &domain.AppError{Status: 503, Code: "dnspod_result_uncertain", Message: "DNSPod 已接收变更，记录列表尚未确认；请重新预览核对后继续"}
			}
			plan = verified
		}
	}
	if err = service.repository.Audit(ctx, actor, "domain.dns", id, "success", 0, 0); err != nil {
		return plan, err
	}
	if err = service.setupIntent.Remove(); err != nil {
		return plan, err
	}
	return plan, nil
}
