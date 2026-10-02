package application

import (
	"context"
	"net/netip"
	"strings"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type SetupSettings struct {
	HomelabDomain string   `json:"homelab_domain"`
	LAN           []string `json:"lan_cidrs"`
	UpstreamCIDRs []string `json:"upstream_cidrs"`
	AllowedNames  []string `json:"allowed_names"`
	DeniedIPs     []string `json:"denied_ips"`
	Resolvers     []string `json:"resolvers"`
}

type SetupRequest struct {
	Services            []PortableService `json:"services,omitempty"`
	ConfirmImport       bool              `json:"confirm_import,omitempty"`
	Username            string            `json:"username"`
	Password            string            `json:"password"`
	Settings            SetupSettings     `json:"settings"`
	AcknowledgeWarnings bool              `json:"acknowledge_warnings"`
}

type SetupCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type SetupPreflight struct {
	Normalized              SetupNormalizedSettings `json:"normalized"`
	Checks                  []SetupCheck            `json:"checks"`
	CanComplete             bool                    `json:"can_complete"`
	RequiresAcknowledgement bool                    `json:"requires_acknowledgement"`
	NetworkValid            bool                    `json:"network_valid"`
	NetworkError            string                  `json:"network_error,omitempty"`
}

type SetupNormalizedSettings struct {
	domain.ManagedSettings
	AdminOrigin string `json:"admin_origin"`
}

type SetupStatus struct {
	Initialized   bool     `json:"initialized"`
	Resolvers     []string `json:"resolver_suggestions"`
	ExternalCaddy bool     `json:"external_caddy"`
}

type SetupHandoff struct {
	Initialized    bool   `json:"initialized"`
	Mode           string `json:"mode"`
	AdminOrigin    string `json:"admin_origin"`
	ManagerStatus  string `json:"manager_status"`
	DNSStatus      string `json:"dns_status"`
	ConsoleStatus  string `json:"console_status"`
	TemporaryEntry bool   `json:"temporary_entry"`
	CheckedAt      string `json:"checked_at"`
}

func ManagedSettingsForSetup(input SetupSettings) (domain.ManagedSettings, error) {
	homelab := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(input.HomelabDomain), "."))
	settings := domain.ManagedSettings{Origin: "https://caddyadmin." + homelab, HomelabDomain: homelab, AdminDomain: "caddyadmin." + homelab, LAN: input.LAN, UpstreamCIDRs: input.UpstreamCIDRs, AllowedNames: input.AllowedNames, DeniedIPs: input.DeniedIPs, Resolvers: input.Resolvers}
	if err := domain.ValidateManagedSettings(settings); err != nil {
		return domain.ManagedSettings{}, err
	}
	return settings, nil
}

func (service *Service) SetupStatus(ctx context.Context) (SetupStatus, error) {
	initialized, err := service.repository.IsInitialized(ctx)
	resolvers := []string{}
	if service.resolverSuggestions != nil {
		resolvers = service.resolverSuggestions()
	}
	return SetupStatus{Initialized: initialized, Resolvers: resolvers, ExternalCaddy: service.externalCaddy()}, err
}

func (service *Service) SetupHandoff(ctx context.Context) SetupHandoff {
	result := SetupHandoff{Mode: "embedded", ManagerStatus: "error", DNSStatus: "pending", ConsoleStatus: "pending", TemporaryEntry: true, CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	if service.externalCaddy() {
		result.Mode = "external"
	}
	initialized, err := service.repository.IsInitialized(ctx)
	if err != nil {
		return result
	}
	result.Initialized = initialized
	if !initialized {
		return result
	}
	settings, err := service.repository.ManagedSettings(ctx)
	if err != nil {
		result.DNSStatus = "error"
		result.ConsoleStatus = "error"
		return result
	}
	result.AdminOrigin = strings.TrimRight(settings.Origin, "/")
	if service.repository.Ping(ctx) == nil {
		result.ManagerStatus = "ready"
	}
	if service.setupProbe == nil {
		return result
	}
	probeContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	if service.setupProbe.AdminDNS(probeContext, settings.AdminDomain, settings.Resolvers) {
		result.DNSStatus = "ready"
	}
	cancel()
	probeContext, cancel = context.WithTimeout(ctx, 2*time.Second)
	if service.setupProbe.ExternalConsole(probeContext, settings.Origin, service.options.ProbeAddress) {
		result.ConsoleStatus = "ready"
	}
	cancel()
	return result
}

func setupCheck(id, status, message string) SetupCheck {
	return SetupCheck{ID: id, Status: status, Message: message}
}

func (service *Service) PreflightSetup(ctx context.Context, input SetupSettings) SetupPreflight {
	result := SetupPreflight{CanComplete: true, Checks: make([]SetupCheck, 0, 5)}
	networkErr := domain.ValidateManagedNetwork(domain.ManagedSettings{LAN: input.LAN, UpstreamCIDRs: input.UpstreamCIDRs, AllowedNames: input.AllowedNames, DeniedIPs: input.DeniedIPs})
	result.NetworkValid = networkErr == nil
	if networkErr != nil {
		result.NetworkError = networkErr.Error()
	}
	settings, err := ManagedSettingsForSetup(input)
	if err != nil {
		result.CanComplete = false
		result.Checks = append(result.Checks, setupCheck("settings_valid", "block", err.Error()))
	} else {
		result.Normalized = SetupNormalizedSettings{ManagedSettings: settings, AdminOrigin: settings.Origin}
		result.Checks = append(result.Checks, setupCheck("settings_valid", "pass", "设置格式和安全边界有效"))
	}
	openNetwork := false
	for _, value := range append(append([]string{}, input.LAN...), input.UpstreamCIDRs...) {
		if prefix, parseErr := netip.ParsePrefix(strings.TrimSpace(value)); parseErr == nil && prefix.Bits() == 0 {
			openNetwork = true
		}
	}
	if openNetwork {
		result.Checks = append(result.Checks, setupCheck("network_scope", "warning", "网络范围包含全部地址，请确认这符合预期"))
	} else {
		result.Checks = append(result.Checks, setupCheck("network_scope", "pass", "网络范围未开放到全部地址"))
	}
	resolverOK, dnsOK, consoleOK := false, false, false
	if service.setupProbe != nil && err == nil {
		probeContext, cancel := context.WithTimeout(ctx, 2*time.Second)
		resolverOK = service.setupProbe.ResolverReachable(probeContext, settings.Resolvers)
		cancel()
		probeContext, cancel = context.WithTimeout(ctx, 2*time.Second)
		dnsOK = service.setupProbe.AdminDNS(probeContext, settings.AdminDomain, settings.Resolvers)
		cancel()
		if service.externalCaddy() {
			probeContext, cancel = context.WithTimeout(ctx, 2*time.Second)
			consoleOK = service.setupProbe.ExternalConsole(probeContext, settings.Origin, service.options.ProbeAddress)
			cancel()
		}
	}
	if resolverOK {
		result.Checks = append(result.Checks, setupCheck("resolver_reachable", "pass", "至少一个 DNS 解析器可响应"))
	} else {
		result.Checks = append(result.Checks, setupCheck("resolver_reachable", "warning", "暂时无法确认 DNS 解析器可用"))
	}
	if dnsOK {
		result.Checks = append(result.Checks, setupCheck("admin_dns", "pass", "控制台域名已有解析结果"))
	} else {
		result.Checks = append(result.Checks, setupCheck("admin_dns", "warning", "控制台域名尚无可用解析结果"))
	}
	if service.externalCaddy() {
		if consoleOK {
			result.Checks = append(result.Checks, setupCheck("external_console", "pass", "外部控制台 TLS 与路由探测通过"))
		} else {
			result.Checks = append(result.Checks, setupCheck("external_console", "warning", "外部控制台 TLS 或路由尚未就绪"))
		}
	}
	for _, check := range result.Checks {
		if check.Status == "warning" {
			result.RequiresAcknowledgement = true
		}
	}
	return result
}

func (service *Service) CompleteSetup(ctx context.Context, request SetupRequest) (string, error) {
	service.setupMu.Lock()
	defer service.setupMu.Unlock()
	preflight := service.PreflightSetupImport(ctx, request.Settings, request.Services)
	if !preflight.CanComplete {
		for _, check := range preflight.Checks {
			if check.Status == "block" {
				return "", domain.Invalid(check.Message)
			}
		}
		return "", domain.Invalid("初始化配置无效")
	}
	if preflight.RequiresAcknowledgement && !request.AcknowledgeWarnings {
		return "", &domain.AppError{Status: 409, Code: "setup_warning_confirmation_required", Message: "预检仍有警告；请核对并明确确认后再完成初始化"}
	}
	settings := preflight.Normalized.ManagedSettings
	if service.snapshot == nil {
		return "", domain.Invalid("启动快照存储未配置")
	}
	if len(request.Services) > 0 && !request.ConfirmImport {
		return "", domain.Invalid("请确认导入服务仅保存为草稿")
	}
	imported, err := service.normalizeImportedServices(ctx, settings, request.Services, nil)
	if err != nil {
		return "", err
	}
	options := service.options
	options.RuntimePolicy = settings
	certificate := domain.CertificateStatus{Mode: domain.CertificateModeBootstrapInternal, ActivationStatus: "idle", PublicStatus: "unknown"}
	if options.AdminURL != "" {
		certificate = domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, ActivationStatus: "success", PublicStatus: "pending"}
	}
	options.CertificateMode = certificate.Mode
	raw, err := domain.Generate(caddyConfigForOptions(options), nil)
	if err != nil {
		return "", err
	}
	exists, err := service.snapshot.Exists()
	if err != nil {
		return "", err
	}
	if exists {
		return "", &domain.AppError{Status: 409, Code: "existing_snapshot", Message: "检测到已有启动快照；为保护数据，请先核对实例状态或按运维文档恢复"}
	}
	if err = service.snapshot.Write(raw); err != nil {
		return "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = service.snapshot.Remove()
		}
	}()
	credentials := SetupCredentials{Username: request.Username, Password: request.Password}
	if len(imported) > 0 {
		err = service.repository.CompleteSetupWithDraft(ctx, credentials, settings, certificate, imported)
	} else {
		err = service.repository.CompleteSetup(ctx, credentials, settings, certificate)
	}
	if err != nil {
		return "", err
	}
	cleanup = false
	service.options = options
	return strings.TrimRight(settings.Origin, "/"), nil
}
