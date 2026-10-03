package application

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func registeredDomains(settings domain.ManagedSettings) []string {
	values := make([]string, 0, len(settings.Domains))
	for _, item := range settings.Domains {
		values = append(values, item.Name)
	}
	return values
}

func (service *Service) consoleCertificateQuery(mode domain.CertificateMode) CertificateQuery {
	query := service.certificateQuery(mode)
	query.Domains = nil
	settings := service.activeSettings()
	for _, d := range settings.Domains {
		if domain.OneLevel(settings.AdminDomain, d.Name) {
			query.Domains = append(query.Domains, d.Name)
			break
		}
	}
	return query
}

func (service *Service) requireServiceCertificates(ctx context.Context, settings domain.ManagedSettings, list []domain.Service) error {
	if service.externalCaddy() {
		return nil
	}
	active := service.activeSettings()
	checked := map[string]bool{}
	for _, item := range list {
		if !item.Enabled || checked[item.DomainID] {
			continue
		}
		checked[item.DomainID] = true
		name := ""
		for _, d := range settings.Domains {
			if d.ID == item.DomainID {
				name = d.Name
			}
		}
		registered := false
		for _, d := range active.Domains {
			if d.ID == item.DomainID && d.Name == name {
				registered = true
			}
		}
		if !registered {
			return domain.Conflict("请先单独发布域名配置，待证书就绪后再发布其业务服务")
		}
		query := service.certificateQuery(domain.CertificateModeCloudflare)
		query.Domains = []string{name}
		if service.certificates == nil || !service.certificates.PublicReady(ctx, query) {
			return domain.Conflict("域名证书尚未验证可信，请等待签发后重新校验")
		}
	}
	return nil
}

func (service *Service) certificateQuery(mode domain.CertificateMode) CertificateQuery {
	settings := service.activeSettings()
	return CertificateQuery{Domains: registeredDomains(settings), ProbeAddress: service.options.ProbeAddress, Mode: mode, TestTLS: service.options.TestTLS}
}

func (service *Service) Certificates(ctx context.Context) []domain.Certificate {
	if service.certificates == nil {
		return []domain.Certificate{}
	}
	mode := service.options.CertificateMode
	if status, err := service.repository.CertificateStatus(ctx); err == nil {
		mode = status.Mode
	}
	return service.certificates.Certificates(ctx, service.certificateQuery(mode))
}

func (service *Service) CertificateState(ctx context.Context) (domain.CertificateStatus, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	status, err := service.repository.CertificateStatus(ctx)
	if err != nil || status.Mode != domain.CertificateModeCloudflare || service.options.TestTLS {
		return status, err
	}
	publicStatus := "unknown"
	runtimeMatches := service.runtimeMatchesSnapshot(ctx)
	if !runtimeMatches {
		publicStatus = "pending"
	} else if service.certificates != nil && service.certificates.PublicReady(ctx, service.consoleCertificateQuery(status.Mode)) {
		publicStatus = "ready"
	} else {
		for _, certificate := range service.Certificates(ctx) {
			if certificate.Status == "invalid" {
				publicStatus = "error"
				break
			}
		}
	}
	ready := publicStatus == "ready"
	if ready {
		if err = service.finalizeTemporaryAdminCertificate(ctx); err != nil {
			slog.Warn("temporary_admin_certificate_finalize_deferred", "error_class", domain.ErrorClass(err))
			publicStatus, ready = "pending", false
			status.LastErrorClass = domain.ErrorClass(err)
		}
	}
	if status.PublicStatus != publicStatus || (ready && status.ActivationStatus != "success") {
		status.PublicStatus = publicStatus
		if ready {
			status.ActivationStatus, status.LastErrorClass = "success", ""
			status.BeforeHash, status.CandidateHash = "", ""
		}
		if err = service.repository.SetCertificateStatusAudit(ctx, status, "system", publicStatus); err != nil {
			return status, err
		}
	}
	return status, nil
}

func (service *Service) finalizeTemporaryAdminCertificate(ctx context.Context) error {
	if service.snapshot == nil {
		return fmt.Errorf("snapshot store is not configured")
	}
	current, err := service.snapshot.Read()
	if err != nil || !strings.Contains(string(current), domain.TemporaryAdminCertificateTag) {
		return err
	}
	runtime, err := service.caddy.Read(ctx)
	if err != nil {
		return err
	}
	if domain.Fingerprint(runtime) != domain.Fingerprint(current) {
		return domain.Conflict("检测到 Caddy 配置漂移；临时管理证书暂不移除")
	}
	config := service.caddyConfig()
	candidate, changed, err := domain.RemoveTemporaryAdminCertificate(current, config)
	if err != nil || !changed {
		return err
	}
	if err = service.caddy.Validate(ctx, candidate); err != nil {
		return err
	}
	loadErr := service.caddy.Load(ctx, candidate)
	after, readErr := service.caddy.Read(ctx)
	if readErr != nil || domain.Fingerprint(after) != domain.Fingerprint(candidate) {
		if loadErr != nil {
			return loadErr
		}
		return domain.Conflict("公网证书配置切换结果待核对")
	}
	if err = service.snapshot.Write(candidate); err != nil {
		return err
	}
	if service.bootstrapTLS != nil {
		return service.bootstrapTLS.Remove(config.TemporaryAdminCertPath, config.TemporaryAdminKeyPath)
	}
	return nil
}

func (service *Service) ActivateCloudflare(ctx context.Context, token, actor string) (domain.CertificateStatus, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if _, err := service.repository.Pending(ctx); err == nil {
		return domain.CertificateStatus{}, domain.Conflict("已有发布待完成或核对，请稍后更新 Token")
	} else if !domain.IsMissing(err) {
		return domain.CertificateStatus{}, err
	}
	if service.externalCaddy() || service.options.TestTLS {
		return domain.CertificateStatus{}, domain.Invalid("当前部署模式不支持在本机启用 Cloudflare")
	}
	if err := domain.ValidateCloudflareToken(token); err != nil {
		return domain.CertificateStatus{}, err
	}
	if service.secrets == nil || service.snapshot == nil || service.bootstrapTLS == nil {
		return domain.CertificateStatus{}, fmt.Errorf("certificate storage is not configured")
	}
	current, err := service.repository.CertificateStatus(ctx)
	if err != nil {
		return domain.CertificateStatus{}, err
	}
	if current.Mode == domain.CertificateModeCloudflare && current.ActivationStatus == "success" {
		if err = service.secrets.WriteCloudflareToken(token); err != nil {
			return current, err
		}
		if err = service.repository.Audit(ctx, actor, "certificate.token", "cloudflare", "saved", 0, 0); err != nil {
			return current, err
		}
		return current, nil
	}
	_, published, err := service.expected(ctx)
	if err != nil {
		return domain.CertificateStatus{}, err
	}
	config := service.caddyConfig()
	config.CertificateMode, config.TemporaryAdminCertificate = domain.CertificateModeCloudflare, true
	if err = service.bootstrapTLS.Ensure(config.TemporaryAdminCertPath, config.TemporaryAdminKeyPath, service.activeSettings().Origin); err != nil {
		return domain.CertificateStatus{}, err
	}
	candidate, err := domain.Generate(config, published)
	if err != nil {
		return domain.CertificateStatus{}, err
	}
	beforeRuntime, err := service.caddy.Read(ctx)
	if err != nil {
		return domain.CertificateStatus{}, err
	}
	beforeSnapshot, err := service.snapshot.Read()
	if err != nil {
		return domain.CertificateStatus{}, err
	}
	beforeHash, candidateHash := domain.Fingerprint(beforeRuntime), domain.Fingerprint(candidate)
	if beforeHash != domain.Fingerprint(beforeSnapshot) {
		return domain.CertificateStatus{}, domain.Conflict("检测到 Caddy 配置漂移；请先核对并处理运行配置")
	}
	status := domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, ActivationStatus: "applying", PublicStatus: "pending", BeforeHash: beforeHash, CandidateHash: candidateHash}
	persist := func(result string) error {
		return service.repository.SetCertificateStatusAudit(ctx, status, actor, result)
	}
	if err = persist("applying"); err != nil {
		return domain.CertificateStatus{}, err
	}
	if err = service.secrets.WriteCloudflareToken(token); err != nil {
		status.Mode, status.ActivationStatus, status.PublicStatus = domain.CertificateModeBootstrapInternal, "failed", "unknown"
		status.LastErrorClass, status.BeforeHash, status.CandidateHash = "unavailable", "", ""
		_ = persist("failed")
		return domain.CertificateStatus{}, err
	}
	if err = service.caddy.Validate(ctx, candidate); err != nil {
		status.Mode, status.ActivationStatus, status.PublicStatus = domain.CertificateModeBootstrapInternal, "failed", "unknown"
		status.LastErrorClass, status.BeforeHash, status.CandidateHash = "validation", "", ""
		_ = persist("failed")
		return domain.CertificateStatus{}, err
	}
	if err = service.snapshot.Write(candidate); err != nil {
		status.Mode, status.ActivationStatus, status.PublicStatus, status.LastErrorClass = domain.CertificateModeBootstrapInternal, "uncertain", "unknown", "unavailable"
		_ = persist("uncertain")
		return domain.CertificateStatus{}, err
	}
	status.ActivationStatus, status.BeforeHash, status.CandidateHash = "success", "", ""
	if err = persist("success"); err != nil {
		return domain.CertificateStatus{}, err
	}
	return status, nil
}

func (service *Service) BuildInfo(ctx context.Context) (string, bool) {
	return service.caddy.BuildInfo(ctx)
}

func (service *Service) TokenConfigured() bool {
	return service.secrets != nil && service.secrets.CloudflareTokenConfigured()
}
