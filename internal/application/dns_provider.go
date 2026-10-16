package application

import "errors"
import "github.com/gofxq/caddy_admin/internal/domain"

type SetupDNSCredentials = SetupCloudflare

func (s *Service) dnsAdapter(p domain.DNSProvider) SetupDNS {
	p, _ = domain.NormalizeDNSProvider(p)
	if a := s.setupDNSProviders[p]; a != nil {
		return a
	}
	if p == domain.DNSProviderCloudflare {
		return s.setupDNS
	}
	return nil
}
func (s *Service) dnsProvider() domain.DNSProvider {
	if domain.IsPublicCertificateMode(s.options.CertificateMode) {
		return domain.DNSProvider(s.options.CertificateMode)
	}
	p, err := domain.NormalizeDNSProvider(s.options.DNSProvider)
	if err != nil {
		return s.options.DNSProvider
	}
	return p
}
func (s *Service) setupToken(p domain.DNSProvider) string {
	if t := s.options.SetupTokens[p]; t != "" {
		return t
	}
	if p == s.dnsProvider() {
		return s.options.SetupToken
	}
	return ""
}
func (s *Service) configuredProviderToken(p domain.DNSProvider, token string, use bool, id string) (string, error) {
	p, e := domain.NormalizeDNSProvider(p)
	if e != nil {
		return "", e
	}
	if use {
		if id != s.setupID || s.setupToken(p) == "" {
			return "", domain.Conflict("预配置凭据确认已失效，请重新读取初始化状态并确认")
		}
		if token != "" {
			return "", domain.Invalid("已选择预配置 Token，不接受重复输入")
		}
		token = s.setupToken(p)
	} else if s.setupToken(p) != "" && !s.externalCaddy() && !s.options.TestTLS {
		return "", domain.Invalid("请确认使用已配置的 DNS Token")
	}
	return token, domain.ValidateDNSToken(p, token)
}
func (s *Service) ResolveSetupProviderToken(p domain.DNSProvider, token string, use bool, id string) (string, error) {
	return s.configuredProviderToken(p, token, use, id)
}
func (s *Service) writeDNSToken(p domain.DNSProvider, token string) error {
	if w, ok := s.secrets.(interface {
		WriteDNSToken(domain.DNSProvider, string) error
	}); ok {
		return w.WriteDNSToken(p, token)
	}
	if p == domain.DNSProviderCloudflare {
		return s.secrets.WriteCloudflareToken(token)
	}
	return domain.Invalid("DNS 凭据存储不可用")
}
func (s *Service) readDNSToken(p domain.DNSProvider) (string, error) {
	if r, ok := s.secrets.(interface {
		ReadDNSToken(domain.DNSProvider) (string, error)
	}); ok {
		return r.ReadDNSToken(p)
	}
	if p == domain.DNSProviderCloudflare {
		if r, ok := s.secrets.(interface{ ReadCloudflareToken() (string, error) }); ok {
			return r.ReadCloudflareToken()
		}
	}
	return "", domain.Invalid("DNS Token 尚未配置")
}

func checkDNSWriteCooldown(intent SetupIntent, plan SetupDNSPlan) error {
	if intent.Plan.Provider != domain.DNSProviderDNSPod || intent.DispatchedAt == "" {
		return nil
	}
	original := intent.Plan
	matches := plan.Provider == original.Provider && plan.Name == original.Name && plan.Type == original.Type && plan.Address == original.Address && plan.ZoneID == original.ZoneID && plan.ContextFingerprint == original.ContextFingerprint
	if !matches {
		return domain.Conflict("已有 DNSPod DNS 变更待核对，请先恢复原账户和域名核对结果")
	}
	if plan.Action == "reuse" {
		return nil
	}
	return &domain.AppError{Status: 503, Code: "dnspod_result_uncertain", Message: "DNSPod 写入结果待核对；请重新预览等待记录可见，未核对前不能重复写入。若记录持续缺失，请核对 DNSPod 控制台与操作意图后维护处理"}
}

// A provider may prove that an operation was rejected before any remote write.
// All other failures retain the durable dispatch marker for reconciliation.
func (s *Service) dnsApplyError(plan SetupDNSPlan, applyErr error) error {
	if plan.Provider != domain.DNSProviderDNSPod {
		return applyErr
	}
	var rejection interface{ DNSWriteRejected() bool }
	if !errors.As(applyErr, &rejection) || !rejection.DNSWriteRejected() {
		return applyErr
	}
	intent, err := s.setupIntent.Read()
	if err != nil {
		return err
	}
	if intent.Plan.Provider != plan.Provider || intent.Plan.Fingerprint != plan.Fingerprint || intent.Plan.ContextFingerprint != plan.ContextFingerprint {
		return applyErr
	}
	intent.DispatchedAt = ""
	if err = s.setupIntent.Write(intent); err != nil {
		return err
	}
	return applyErr
}
