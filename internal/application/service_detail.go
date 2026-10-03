package application

import (
	"context"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func findService(list []domain.Service, id string) *domain.Service {
	for _, item := range list {
		if item.ID == id {
			return &item
		}
	}
	return nil
}
func findServiceDomain(settings domain.ManagedSettings, item *domain.Service) *domain.ManagedDomain {
	if item == nil {
		return nil
	}
	for _, d := range settings.Domains {
		if d.ID == item.DomainID {
			return &d
		}
	}
	return nil
}

func (service *Service) ServiceDetail(ctx context.Context, id string) (domain.ServiceDetail, error) {
	detail := domain.ServiceDetail{ID: id, ExternalCaddy: service.externalCaddy(), RecentDeployments: []domain.ServiceRelease{}}
	draft, err := service.repository.Draft(ctx)
	if err != nil {
		return detail, err
	}
	detail.Revision = draft.Revision
	detail.Draft = findService(draft.Services, id)
	detail.DraftDomain = findServiceDomain(draft.Settings, detail.Draft)
	latest, err := service.repository.Latest(ctx)
	if err != nil && !domain.IsMissing(err) {
		return detail, err
	}
	if err == nil {
		detail.Published = findService(latest.Services, id)
		detail.PublishedDomain = findServiceDomain(latest.Settings, detail.Published)
		detail.Runtime.ExpectedHash = latest.Hash
		detail.Runtime.DeploymentID = latest.ID
		detail.Runtime.Version = latest.Version
	} else if service.snapshot != nil {
		raw, readErr := service.snapshot.Read()
		if readErr != nil {
			return detail, readErr
		}
		detail.Runtime.ExpectedHash = domain.Fingerprint(raw)
	}
	if detail.Draft == nil && detail.Published == nil {
		return detail, domain.NotFound("服务不存在")
	}
	history, err := service.repository.Deployments(ctx, 0, 20)
	if err != nil {
		return detail, err
	}
	for _, release := range history {
		for _, change := range release.Changes {
			if (change.Before != nil && change.Before.ID == id) || (change.After != nil && change.After.ID == id) {
				detail.RecentDeployments = append(detail.RecentDeployments, domain.ServiceRelease{ID: release.ID, Version: release.Version, Status: release.Status, Created: release.Created, Finished: release.Finished, RollbackID: release.RollbackID})
				break
			}
		}
		if len(detail.RecentDeployments) == 5 {
			break
		}
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	detail.Runtime.Status = "unknown"
	detail.Runtime.Message = "Caddy 不可达，运行配置未知；可检查连接后重试"
	raw, readErr := service.caddy.Read(checkCtx)
	if readErr == nil {
		detail.Runtime.RuntimeHash = domain.Fingerprint(raw)
		if detail.Runtime.RuntimeHash != "" {
			detail.Runtime.Reachable = true
			detail.Runtime.Status = "drift"
			detail.Runtime.Message = "运行配置与已发布版本不一致，请到发布页面核对"
			if detail.Runtime.ExpectedHash != "" && detail.Runtime.RuntimeHash == detail.Runtime.ExpectedHash {
				detail.Runtime.Status = "matched"
				detail.Runtime.Message = "运行配置与已发布版本一致；这不代表上游应用健康"
			}
		}
	}
	pending, pendingErr := service.repository.Pending(ctx)
	if pendingErr != nil && !domain.IsMissing(pendingErr) {
		return detail, pendingErr
	}
	if pendingErr == nil && pending.ID != "" {
		detail.Runtime.Status = "pending"
		detail.Runtime.Message = "发布正在执行或结果待核对，请到发布页面查看进度"
	}
	// A publish may finish while Caddy is being read. Never present the old
	// model as the currently verified version in that case.
	current, currentErr := service.repository.Latest(ctx)
	if currentErr != nil && !domain.IsMissing(currentErr) {
		return detail, currentErr
	}
	if (current.ID != latest.ID || current.Hash != latest.Hash) && detail.Runtime.Status != "pending" {
		detail.Runtime.Status = "unknown"
		detail.Runtime.Message = "核对期间已发布版本变化，请刷新详情后重试"
	}
	detail.Runtime.CheckedAt = timestamp()
	return detail, nil
}
