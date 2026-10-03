package application

import (
	"context"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func timestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (service *Service) Draft(ctx context.Context) (domain.Draft, error) {
	return service.repository.Draft(ctx)
}

func (service *Service) DraftRevision(ctx context.Context, revision int64) (domain.Draft, error) {
	return service.repository.DraftRevision(ctx, revision)
}

func (service *Service) Published(ctx context.Context) ([]domain.Service, error) {
	_, published, err := service.expected(ctx)
	return published, err
}

func (service *Service) SaveService(ctx context.Context, revision int64, value domain.Service, remove bool, actor string) (domain.Draft, error) {
	draft, err := service.repository.Draft(ctx)
	if err != nil {
		return domain.Draft{}, err
	}
	if value.ID == "" {
		if remove {
			return domain.Draft{}, domain.NotFound("服务不存在")
		}
		value.ID = domain.ID()
	} else {
		found := false
		for _, existing := range draft.Services {
			if existing.ID == value.ID {
				found = true
				if remove {
					value = existing
				}
				break
			}
		}
		if !found {
			return domain.Draft{}, domain.NotFound("服务不存在")
		}
	}
	if !remove {
		policy, policyErr := service.targetPolicyForSettings(ctx, draft.Settings)
		if policyErr != nil {
			return domain.Draft{}, policyErr
		}
		value, err = domain.NormalizeService(ctx, policy, value)
		if err != nil {
			return domain.Draft{}, err
		}
		value.UpdatedAt = timestamp()
	}
	return service.repository.SaveService(ctx, revision, value, remove, actor)
}

func (service *Service) Deployment(ctx context.Context, id string) (domain.Deployment, error) {
	return service.repository.Deployment(ctx, id)
}

func (service *Service) Deployments(ctx context.Context, offset, limit int) ([]domain.Deployment, error) {
	return service.repository.Deployments(ctx, offset, limit)
}

func (service *Service) Audits(ctx context.Context, offset, limit int) ([]domain.AuditEvent, error) {
	return service.repository.Audits(ctx, offset, limit)
}
