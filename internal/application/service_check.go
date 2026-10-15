package application

import (
	"context"
	"encoding/json"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type ServiceConfigurationCheck struct {
	Revision  int64                  `json:"revision"`
	Service   domain.Service         `json:"service"`
	Effective domain.ServiceAdvanced `json:"effective"`
	Config    json.RawMessage        `json:"config"`
}

// CheckService validates an unsaved candidate without storing a validation token
// or touching drafts, deployments, the running instance or its startup snapshot.
func (s *Service) CheckService(ctx context.Context, revision int64, value domain.Service) (ServiceConfigurationCheck, error) {
	if !s.mu.TryLock() {
		return ServiceConfigurationCheck{}, domain.Conflict("配置操作正在执行，请稍后重试检查")
	}
	defer s.mu.Unlock()
	if _, err := s.repository.Pending(ctx); err == nil {
		return ServiceConfigurationCheck{}, domain.Conflict("已有发布待完成或核对")
	} else if !domain.IsMissing(err) {
		return ServiceConfigurationCheck{}, err
	}
	draft, err := s.repository.Draft(ctx)
	if err != nil {
		return ServiceConfigurationCheck{}, err
	}
	if draft.Revision != revision {
		return ServiceConfigurationCheck{}, domain.Conflict("草稿已变化，请读取最新草稿后重新检查")
	}
	policy, err := s.targetPolicyForSettings(ctx, draft.Settings)
	if err != nil {
		return ServiceConfigurationCheck{}, err
	}
	value, err = domain.NormalizeService(ctx, policy, value)
	if err != nil {
		return ServiceConfigurationCheck{}, err
	}
	list := make([]domain.Service, 0, len(draft.Services)+1)
	found := value.ID == ""
	for _, item := range draft.Services {
		if item.ID == value.ID {
			found = true
			continue
		}
		normalized, e := domain.NormalizeService(ctx, policy, item)
		if e != nil {
			return ServiceConfigurationCheck{}, e
		}
		list = append(list, normalized)
	}
	if !found {
		return ServiceConfigurationCheck{}, domain.NotFound("服务已从草稿删除，请读取最新草稿")
	}
	candidate := value
	candidate.Enabled = true
	if candidate.ID == "" {
		candidate.ID = "service-check"
	}
	list = append(list, candidate)
	raw, err := domain.Generate(s.caddyConfigFor(draft.Settings), list)
	if err != nil {
		return ServiceConfigurationCheck{}, err
	}
	if err = s.caddy.Validate(ctx, raw); err != nil {
		return ServiceConfigurationCheck{}, err
	}
	latest, err := s.repository.Draft(ctx)
	if err != nil {
		return ServiceConfigurationCheck{}, err
	}
	if latest.Revision != revision {
		return ServiceConfigurationCheck{}, domain.Conflict("检查期间草稿已变化，请重新检查")
	}
	effective, err := domain.EffectiveServiceAdvanced(value)
	if err != nil {
		return ServiceConfigurationCheck{}, err
	}
	return ServiceConfigurationCheck{Revision: revision, Service: value, Effective: effective, Config: raw}, nil
}
