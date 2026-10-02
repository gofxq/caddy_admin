package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/gofxq/caddy_admin/internal/domain"
)

const ConfigurationFormat = "caddy-web-admin"
const ConfigurationVersion = 1
const MaxConfigurationBytes = 60 << 10

// PortableService contains editable business data, never identifiers or resolved addresses.
type PortableService struct {
	Name     string `json:"name"`
	Group    string `json:"group"`
	Hostname string `json:"hostname"`
	Scheme   string `json:"scheme"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Enabled  bool   `json:"enabled"`
	Notes    string `json:"notes"`
}

type Configuration struct {
	Format   string                 `json:"format"`
	Version  int                    `json:"version"`
	Settings domain.ManagedSettings `json:"settings"`
	Services []PortableService      `json:"services"`
}

type ConfigurationPreview struct {
	Revision int64            `json:"revision"`
	Services []domain.Service `json:"services"`
	Changes  []domain.Change  `json:"changes"`
}

func portableService(value domain.Service) PortableService {
	return PortableService{Name: value.Name, Group: value.Group, Hostname: value.Hostname, Scheme: value.Scheme, Host: value.Host, Port: value.Port, Enabled: value.Enabled, Notes: value.Notes}
}

func (service *Service) ExportConfiguration(ctx context.Context) (Configuration, error) {
	settings, err := service.repository.ManagedSettings(ctx)
	if err != nil {
		return Configuration{}, err
	}
	draft, err := service.repository.Draft(ctx)
	if err != nil {
		return Configuration{}, err
	}
	settings.LAN = append([]string{}, settings.LAN...)
	settings.UpstreamCIDRs = append([]string{}, settings.UpstreamCIDRs...)
	settings.AllowedNames = append([]string{}, settings.AllowedNames...)
	settings.DeniedIPs = append([]string{}, settings.DeniedIPs...)
	settings.Resolvers = append([]string{}, settings.Resolvers...)
	result := Configuration{Format: ConfigurationFormat, Version: ConfigurationVersion, Settings: settings, Services: make([]PortableService, 0, len(draft.Services))}
	for _, value := range draft.Services {
		result.Services = append(result.Services, portableService(value))
	}
	if err = validateConfiguration(result); err != nil {
		return Configuration{}, err
	}
	return result, nil
}

func validateConfiguration(value Configuration) error {
	if value.Format != ConfigurationFormat || value.Version != ConfigurationVersion {
		return domain.Invalid("不支持的配置文件格式或版本")
	}
	if value.Services == nil {
		return domain.Invalid("配置文件缺少服务列表")
	}
	if err := domain.ValidateManagedSettings(value.Settings); err != nil {
		return domain.Invalid("配置文件的域名或网络策略无效")
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	// Encoder appends a newline; downloads use compact JSON without it.
	if encoded.Len()-1 > MaxConfigurationBytes {
		return domain.Invalid("配置文件不得超过 60 KiB")
	}
	return nil
}

func (service *Service) normalizeImportedServices(ctx context.Context, settings domain.ManagedSettings, values []PortableService, current []domain.Service) ([]domain.Service, error) {
	result := make([]domain.Service, 0, len(values))
	if len(values) == 0 {
		return result, nil
	}
	policy, err := service.targetPolicyForSettings(ctx, settings)
	if err != nil {
		return nil, err
	}
	ids := map[string]string{}
	for _, value := range current {
		ids[value.Hostname] = value.ID
	}
	for index, value := range values {
		candidate, normalizeErr := domain.NormalizeService(ctx, policy, domain.Service{Name: value.Name, Group: value.Group, Hostname: value.Hostname, Scheme: value.Scheme, Host: value.Host, Port: value.Port, Enabled: value.Enabled, Notes: value.Notes})
		if normalizeErr != nil {
			return nil, domain.Invalid(fmt.Sprintf("导入的第 %d 个服务无效：%s", index+1, normalizeErr.Error()))
		}
		candidate.ID = ids[candidate.Hostname]
		if candidate.ID == "" {
			candidate.ID = domain.ID()
		}
		candidate.UpdatedAt = timestamp()
		result = append(result, candidate)
	}
	if err = domain.ValidateUniqueServices(result); err != nil {
		return nil, err
	}
	return result, nil
}

func (service *Service) PreviewConfiguration(ctx context.Context, value Configuration) (ConfigurationPreview, error) {
	if err := validateConfiguration(value); err != nil {
		return ConfigurationPreview{}, err
	}
	draft, err := service.repository.Draft(ctx)
	if err != nil {
		return ConfigurationPreview{}, err
	}
	values, err := service.normalizeImportedServices(ctx, service.options.RuntimePolicy, value.Services, draft.Services)
	if err != nil {
		return ConfigurationPreview{}, err
	}
	return ConfigurationPreview{Revision: draft.Revision, Services: values, Changes: domain.Diff(draft.Services, values)}, nil
}

func (service *Service) ImportConfiguration(ctx context.Context, value Configuration, revision int64, confirm bool, actor string) (domain.Draft, error) {
	if !confirm {
		return domain.Draft{}, domain.Invalid("请明确确认替换当前服务草稿")
	}
	preview, err := service.PreviewConfiguration(ctx, value)
	if err != nil {
		return domain.Draft{}, err
	}
	if preview.Revision != revision {
		return domain.Draft{}, domain.Conflict("草稿已被修改，请重新预览导入")
	}
	return service.repository.ReplaceDraft(ctx, revision, preview.Services, actor)
}

// Setup imports use the submitted policy, and are rechecked again on completion.
func (service *Service) PreflightSetupImport(ctx context.Context, input SetupSettings, values []PortableService) SetupPreflight {
	result := service.PreflightSetup(ctx, input)
	if !result.CanComplete || len(values) == 0 {
		return result
	}
	if _, err := service.normalizeImportedServices(ctx, result.Normalized.ManagedSettings, values, nil); err != nil {
		result.CanComplete = false
		result.Checks = append(result.Checks, setupCheck("imported_services", "block", err.Error()))
	} else {
		result.Checks = append(result.Checks, setupCheck("imported_services", "pass", "导入服务已通过当前域名与上游安全校验"))
	}
	return result
}
