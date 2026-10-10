package application

import (
	"context"
	"github.com/gofxq/caddy_admin/internal/domain"
	"time"
)

func (s *Service) Diagnose(ctx context.Context, domainID string) (domain.Diagnostics, error) {
	result := domain.Diagnostics{DomainID: domainID, CheckedAt: timestamp(), Vantage: "manager", Checks: []domain.DiagnosticCheck{}, Events: []domain.AccessEvent{}}
	settings := s.activeSettings()
	d, ok := settings.Domain(domainID)
	if !ok {
		return result, domain.NotFound("只能诊断已生效的登记域名")
	}
	if s.observer == nil || s.observer.diagnostics == nil {
		return result, &domain.AppError{Status: 503, Code: "unavailable", Message: "诊断暂不可用"}
	}
	if !s.observer.diagnosticsMu.TryLock() {
		return result, &domain.AppError{Status: 429, Code: "rate_limited", Message: "已有域名诊断正在进行，请稍后重试"}
	}
	defer s.observer.diagnosticsMu.Unlock()
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result.Checks = s.observer.diagnostics.Diagnose(bounded, settings, d)
	for _, cert := range s.Certificates(bounded) {
		if cert.Subject == "*."+d.Name {
			status := "unknown"
			if cert.Status == "valid" || cert.Status == "warning" {
				status = "pass"
			} else if cert.Status == "invalid" {
				status = "fail"
			}
			result.Checks = append(result.Checks, domain.DiagnosticCheck{Name: "固定 Caddy 地址 TLS", Status: status, Message: cert.Message, Values: []string{cert.Subject}})
		}
	}
	if s.observer.store != nil {
		events, err := s.observer.store.Logs(bounded, domain.LogQuery{From: time.Now().Add(-24 * time.Hour), To: time.Now(), Kind: "diagnostic", Limit: 20})
		if err == nil {
			result.Events = events
		}
	}
	result.Checks = append(result.Checks, domain.DiagnosticCheck{Name: "ACME 签发线索", Status: "unknown", Message: "下方仅为全实例脱敏签发事件；不能据此认定当前域名根因。429 应等待 CA 限速窗口，不反复重试。", Values: []string{}})
	return result, nil
}
