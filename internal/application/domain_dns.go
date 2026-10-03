package application

import (
	"context"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func (service *Service) PreviewDomainDNS(ctx context.Context, id, address string) (SetupDNSPlan, error) {
	if service.externalCaddy() || service.options.TestTLS || service.setupDNS == nil {
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
	reader, ok := service.secrets.(interface{ ReadCloudflareToken() (string, error) })
	if !ok {
		return SetupDNSPlan{}, domain.Invalid("Cloudflare Token 尚未配置")
	}
	token, err := reader.ReadCloudflareToken()
	if err != nil {
		return SetupDNSPlan{}, err
	}
	return service.setupDNS.Preview(ctx, token, name, address)
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
	if plan.Action != "reuse" {
		if err = service.setupIntent.Write(SetupIntent{Plan: plan}); err != nil {
			return plan, err
		}
		reader := service.secrets.(interface{ ReadCloudflareToken() (string, error) })
		token, e := reader.ReadCloudflareToken()
		if e != nil {
			return plan, e
		}
		if err = service.setupDNS.Apply(ctx, token, plan); err != nil {
			return plan, err
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
