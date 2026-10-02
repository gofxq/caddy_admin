package application

import (
	"context"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func (service *Service) Overview(ctx context.Context) (domain.Overview, error) {
	overview := domain.Overview{CheckedAt: timestamp()}
	draft, err := service.repository.Draft(ctx)
	if err != nil {
		return overview, err
	}
	overview.DraftRevision = draft.Revision
	var published []domain.Service
	overview.ExpectedHash, published, err = service.expected(ctx)
	if err != nil {
		return overview, err
	}
	overview.Unpublished = len(domain.Diff(published, draft.Services)) > 0
	for _, item := range published {
		if item.Enabled {
			overview.Enabled++
		}
	}
	latest, latestErr := service.repository.Latest(ctx)
	if latestErr == nil {
		overview.Version = latest.Version
	} else if !domain.IsMissing(latestErr) {
		return overview, latestErr
	}
	overview.Recent, err = service.repository.Deployments(ctx, 0, 5)
	if err != nil {
		return overview, err
	}
	runtime, err := service.caddy.Read(ctx)
	if err != nil {
		overview.Message = "Caddy 不可达，运行配置未知"
		return overview, nil
	}
	overview.Reachable = true
	overview.RuntimeHash = domain.Fingerprint(runtime)
	overview.Drift = overview.RuntimeHash != overview.ExpectedHash
	if overview.Drift {
		overview.Message = "检测到外部配置漂移，发布前必须重新确认"
	} else {
		overview.Message = "运行配置与已发布版本一致"
	}
	if len(overview.Recent) > 0 && overview.Recent[0].Status != "success" {
		overview.Message += "；最近发布状态：" + overview.Recent[0].Status
	}
	return overview, nil
}

func (service *Service) Ready(ctx context.Context) error {
	if err := service.repository.Ping(ctx); err != nil {
		return err
	}
	_, err := service.caddy.Read(ctx)
	return err
}
