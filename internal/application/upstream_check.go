package application

import (
	"context"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func (service *Service) CheckUpstream(ctx context.Context, id, expectedHash, expectedDeploymentID string) (domain.UpstreamCheck, error) {
	if !service.upstreamMu.TryLock() {
		return domain.UpstreamCheck{}, &domain.AppError{Status: 429, Code: "rate_limited", Message: "已有上游检查正在执行，请稍后重试"}
	}
	defer service.upstreamMu.Unlock()
	detail, err := service.ServiceDetail(ctx, id)
	if err != nil {
		return domain.UpstreamCheck{}, err
	}
	if detail.Published == nil || !detail.Published.Enabled {
		return domain.UpstreamCheck{}, domain.Conflict("只能检查已发布且启用的服务上游")
	}
	if detail.Runtime.Status != "matched" || expectedHash == "" || expectedHash != detail.Runtime.ExpectedHash || expectedDeploymentID == "" || expectedDeploymentID != detail.Runtime.DeploymentID {
		return domain.UpstreamCheck{}, domain.Conflict("运行配置或发布版本未确认，请刷新详情后重试")
	}
	if service.upstreamProbe == nil {
		return domain.UpstreamCheck{}, &domain.AppError{Status: 503, Code: "unavailable", Message: "上游检查暂不可用"}
	}
	policy, err := service.targetPolicy(ctx)
	if err != nil {
		return domain.UpstreamCheck{}, err
	}
	if err = domain.ValidateServiceDial(policy, *detail.Published); err != nil {
		return domain.UpstreamCheck{}, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	result := service.upstreamProbe.Check(probeCtx, detail.Published.Dial)
	cancel()
	result.Target = detail.Published.Dial
	result.ExpectedHash = expectedHash
	result.DeploymentID = expectedDeploymentID
	result.Vantage = "manager"
	result.CheckedAt = timestamp()
	after, afterErr := service.ServiceDetail(ctx, id)
	if afterErr != nil || after.Runtime.Status != "matched" || after.Runtime.ExpectedHash != expectedHash || after.Runtime.DeploymentID != expectedDeploymentID {
		result.Status = "unknown"
		result.Message = "检查期间发布版本或运行状态变化，结果已失效，请刷新详情后重试"
	}
	return result, nil
}
