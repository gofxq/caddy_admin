package application

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type SetupSettings struct {
	Domain         string                  `json:"domain"`
	Resolvers      []string                `json:"resolvers"`
	ImportSettings *domain.ManagedSettings `json:"-"`
}

type SetupRequest struct {
	UseConfiguredPassword bool                    `json:"use_configured_password,omitempty"`
	UseConfiguredToken    bool                    `json:"use_configured_token,omitempty"`
	SetupID               string                  `json:"setup_id,omitempty"`
	ImportSettings        *domain.ManagedSettings `json:"import_settings,omitempty"`
	Cloudflare            *SetupCloudflare        `json:"cloudflare,omitempty"`
	Services              []PortableService       `json:"services,omitempty"`
	ConfirmImport         bool                    `json:"confirm_import,omitempty"`
	Username              string                  `json:"username"`
	Password              string                  `json:"password"`
	Settings              SetupSettings           `json:"settings"`
	AcknowledgeWarnings   bool                    `json:"acknowledge_warnings"`
	WarningFingerprint    string                  `json:"warning_fingerprint,omitempty"`
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
	WarningFingerprint      string                  `json:"warning_fingerprint"`
}

type SetupNormalizedSettings struct {
	domain.ManagedSettings
	AdminOrigin string `json:"admin_origin"`
}

type SetupStatus struct {
	AdminPasswordStatus   string   `json:"admin_password_status"`
	CloudflareTokenStatus string   `json:"cloudflare_token_status"`
	AdminPasswordError    string   `json:"admin_password_error,omitempty"`
	CloudflareTokenError  string   `json:"cloudflare_token_error,omitempty"`
	SetupID               string   `json:"setup_id"`
	TestTLS               bool     `json:"test_tls"`
	Initialized           bool     `json:"initialized"`
	Resolvers             []string `json:"resolver_suggestions"`
	ExternalCaddy         bool     `json:"external_caddy"`
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
	name := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(input.Domain), "*."), "."))
	resolvers := input.Resolvers
	if len(resolvers) == 0 && input.ImportSettings != nil {
		resolvers = input.ImportSettings.Resolvers
	}
	if len(resolvers) == 0 {
		resolvers = []string{"1.1.1.1"}
	}
	settings := domain.ManagedSettings{Origin: "https://caddyadmin." + name, AdminDomain: "caddyadmin." + name,
		Domains: []domain.ManagedDomain{{ID: name, Name: name}}, LAN: []string{}, UpstreamCIDRs: []string{}, AllowedNames: []string{}, DeniedIPs: []string{}, Resolvers: resolvers}
	if input.ImportSettings != nil {
		imported := input.ImportSettings
		if imported.PreviousAdminDomain != "" || imported.PreviousOrigin != "" || imported.AdminDomain != "caddyadmin."+imported.AdminBase() || imported.Origin != "https://"+imported.AdminDomain {
			return settings, domain.Invalid("初始化导入只支持标准 caddyadmin 控制台地址，且不得包含未完成的地址交接")
		}
		settings = *input.ImportSettings
		settings.Domains = append([]domain.ManagedDomain{}, settings.Domains...)
		settings.Resolvers = resolvers
		settings.AdminDomain, settings.Origin = "caddyadmin."+name, "https://caddyadmin."+name
		settings.PreviousAdminDomain, settings.PreviousOrigin = "", ""
		found := false
		for _, d := range settings.Domains {
			if d.Name == name {
				found = true
			}
		}
		if !found {
			return settings, domain.Invalid("首个域名必须属于导入配置")
		}
	}
	if err := domain.ValidateManagedSettings(settings); err != nil {
		return settings, domain.Invalid(err.Error())
	}
	return settings, nil
}

func setupActiveSettings(candidate domain.ManagedSettings) domain.ManagedSettings {
	active := candidate
	active.ConsoleLANOnly = false
	active.PreviousAdminDomain, active.PreviousOrigin = "", ""
	active.Domains = []domain.ManagedDomain{}
	for _, d := range candidate.Domains {
		if domain.OneLevel(candidate.AdminDomain, d.Name) {
			active.Domains = append(active.Domains, d)
			break
		}
	}
	return active
}

func credentialStatus(value string, validate func(string) error) (string, string) {
	if value == "" {
		return "missing", ""
	}
	if err := validate(value); err != nil {
		return "invalid", err.Error()
	}
	return "ready", ""
}

func (service *Service) configuredToken(token string, useConfigured bool, setupID string) (string, error) {
	if useConfigured {
		if setupID != service.setupID || service.options.SetupToken == "" {
			return "", domain.Conflict("预配置凭据确认已失效，请重新读取初始化状态并确认")
		}
		if token != "" {
			return "", domain.Invalid("已选择预配置 Token，不接受重复输入")
		}
		token = service.options.SetupToken
	} else if service.options.SetupToken != "" && !service.externalCaddy() && !service.options.TestTLS {
		return "", domain.Invalid("请确认使用已配置的 Cloudflare Token")
	}
	return token, domain.ValidateCloudflareToken(token)
}

func (service *Service) ConfiguredSetupToken(setupID string) (string, error) {
	return service.configuredToken("", true, setupID)
}

func (service *Service) SetupStatus(ctx context.Context) (SetupStatus, error) {
	service.setupMu.Lock()
	defer service.setupMu.Unlock()
	initialized, err := service.repository.IsInitialized(ctx)
	resolvers := []string{}
	if service.resolverSuggestions != nil {
		resolvers = service.resolverSuggestions()
	}
	status := SetupStatus{Initialized: initialized, Resolvers: resolvers, ExternalCaddy: service.externalCaddy(), TestTLS: service.options.TestTLS, SetupID: service.setupID}
	status.AdminPasswordStatus, status.AdminPasswordError = credentialStatus(service.options.SetupPassword, domain.ValidatePassword)
	status.CloudflareTokenStatus, status.CloudflareTokenError = credentialStatus(service.options.SetupToken, domain.ValidateCloudflareToken)
	return status, err
}

func (service *Service) SetupHandoff(ctx context.Context) SetupHandoff {
	result := SetupHandoff{Mode: "embedded", ManagerStatus: "error", DNSStatus: "pending", ConsoleStatus: "pending", TemporaryEntry: true, CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	if service.externalCaddy() {
		result.Mode = "external"
	}
	service.setupMu.Lock()
	initialized, err := service.repository.IsInitialized(ctx)
	service.setupMu.Unlock()
	if err != nil {
		return result
	}
	result.Initialized = initialized
	result.ManagerStatus = "ready"
	if !initialized {
		return result
	}
	result.ManagerStatus = "error"
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
	result.NetworkValid = true
	settings, err := ManagedSettingsForSetup(input)
	if err != nil {
		result.CanComplete = false
		result.Checks = append(result.Checks, setupCheck("settings_valid", "block", err.Error()))
	} else {
		result.Normalized = SetupNormalizedSettings{ManagedSettings: settings, AdminOrigin: settings.Origin}
		result.Checks = append(result.Checks, setupCheck("settings_valid", "pass", "设置格式和安全边界有效"))
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
		result.Checks = append(result.Checks, setupCheck("resolver_reachable", "pass", "至少一个服务器 DNS 解析器可响应"))
	} else {
		result.Checks = append(result.Checks, setupCheck("resolver_reachable", "warning", "服务器 DNS 暂不可用，可能影响证书 DNS 校验；请检查高级设置中的解析器与网络"))
	}
	if dnsOK {
		result.Checks = append(result.Checks, setupCheck("admin_dns", "pass", "服务器 DNS 已返回控制台地址"))
	} else {
		result.Checks = append(result.Checks, setupCheck("admin_dns", "warning", "服务器 DNS 未返回控制台地址，可能受缓存或内网地址过滤影响；浏览器 DoH 结果独立，请检查服务器解析器"))
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
	result.WarningFingerprint = setupWarningFingerprint(result)
	return result
}

// Bind consent to the settings and the warnings the administrator actually saw.
// No password, token, or probe error is included in this non-sensitive binding.
func setupWarningFingerprint(result SetupPreflight) string {
	warnings := []SetupCheck{}
	for _, check := range result.Checks {
		if check.Status == "warning" {
			warnings = append(warnings, check)
		}
	}
	if len(warnings) == 0 {
		return ""
	}
	raw, _ := json.Marshal(struct {
		Settings domain.ManagedSettings
		Warnings []SetupCheck
	}{result.Normalized.ManagedSettings, warnings})
	return domain.Fingerprint(raw)
}

func (service *Service) CompleteSetup(ctx context.Context, request SetupRequest) (string, error) {
	service.setupMu.Lock()
	defer service.setupMu.Unlock()
	initialized, err := service.repository.IsInitialized(ctx)
	if err != nil {
		return "", err
	}
	if initialized {
		return "", domain.Conflict("系统已经初始化")
	}
	if len(request.Username) == 0 || len(request.Username) > 64 {
		return "", domain.Invalid("用户名必须为 1–64 字节")
	}
	if request.UseConfiguredPassword {
		if request.SetupID != service.setupID || service.options.SetupPassword == "" {
			return "", domain.Conflict("预配置密码确认已失效，请重新读取初始化状态")
		}
		if request.Password != "" {
			return "", domain.Invalid("已选择预配置密码，不接受重复输入")
		}
		request.Password = service.options.SetupPassword
	} else if service.options.SetupPassword != "" {
		return "", domain.Invalid("请确认使用已配置的管理员密码")
	}
	request.Settings.ImportSettings = request.ImportSettings
	if request.Cloudflare != nil && request.UseConfiguredToken {
		request.Cloudflare.UseConfiguredToken, request.Cloudflare.SetupID = true, request.SetupID
	}
	if err := domain.ValidatePassword(request.Password); err != nil {
		return "", err
	}
	preflight := service.PreflightSetupImport(ctx, request.Settings, request.Services)
	if !preflight.CanComplete {
		for _, check := range preflight.Checks {
			if check.Status == "block" {
				return "", domain.Invalid(check.Message)
			}
		}
		return "", domain.Invalid("初始化配置无效")
	}
	if preflight.RequiresAcknowledgement && (!request.AcknowledgeWarnings || request.WarningFingerprint != preflight.WarningFingerprint) {
		return "", &domain.AppError{Status: 409, Code: "setup_warning_confirmation_required", Message: "预检警告尚未确认或已变化；请重新核对并明确确认后再完成初始化"}
	}
	settings := preflight.Normalized.ManagedSettings
	if service.snapshot == nil {
		return "", domain.Invalid("启动快照存储未配置")
	}
	if (len(request.Services) > 0 || request.Settings.ImportSettings != nil) && !request.ConfirmImport {
		return "", domain.Invalid("请确认导入服务仅保存为草稿")
	}
	imported, err := service.normalizeImportedServices(ctx, settings, request.Services, nil)
	if err != nil {
		return "", err
	}
	options := service.options
	options.RuntimePolicy = setupActiveSettings(settings)
	certificate := domain.CertificateStatus{Mode: domain.CertificateModeBootstrapInternal, ActivationStatus: "idle", PublicStatus: "unknown"}
	if options.AdminURL != "" {
		certificate = domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, ActivationStatus: "success", PublicStatus: "pending"}
	}
	automaticDNS := !service.externalCaddy() && !options.TestTLS
	if automaticDNS {
		if request.Cloudflare == nil || !request.Cloudflare.Confirmed || request.Cloudflare.Fingerprint == "" {
			return "", domain.Invalid("请填写 Cloudflare Token、预览 DNS 记录并确认变更")
		}
		if service.setupDNS == nil || service.setupIntent == nil || service.secrets == nil || service.bootstrapTLS == nil {
			return "", domain.Invalid("初始化 DNS 或 secret 存储未配置")
		}
		certificate = domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, ActivationStatus: "success", PublicStatus: "pending"}
	} else if request.Cloudflare != nil {
		return "", domain.Invalid("当前模式不接受本机 Cloudflare 凭据")
	}
	options.CertificateMode = certificate.Mode
	caddyConfig := caddyConfigForOptions(options)
	if automaticDNS {
		caddyConfig.TemporaryAdminCertificate = true
		if err := service.bootstrapTLS.Ensure(caddyConfig.TemporaryAdminCertPath, caddyConfig.TemporaryAdminKeyPath, settings.Origin); err != nil {
			return "", err
		}
	}
	raw, err := domain.Generate(caddyConfig, nil)
	if err != nil {
		return "", err
	}
	exists, err := service.snapshot.Exists()
	if err != nil {
		return "", err
	}
	intent := SetupIntent{}
	if automaticDNS {
		intent, err = service.setupIntent.Read()
		if err != nil {
			return "", err
		}
	}
	bindingRaw, _ := json.Marshal(struct {
		Username string
		Settings domain.ManagedSettings
		Services []PortableService
	}{request.Username, settings, request.Services})
	binding := domain.Fingerprint(bindingRaw)
	ownedSnapshot := false
	if exists && automaticDNS && intent.ConfigurationHash == binding && intent.SnapshotHash == domain.Fingerprint(raw) {
		existing, readErr := service.snapshot.Read()
		ownedSnapshot = readErr == nil && domain.Fingerprint(existing) == intent.SnapshotHash
	}
	if exists && !ownedSnapshot {
		return "", &domain.AppError{Status: 409, Code: "existing_snapshot", Message: "检测到已有启动快照；为保护数据，请先核对实例状态或按运维文档恢复"}
	}
	if automaticDNS {
		cf := request.Cloudflare
		cf.Token, err = service.configuredToken(cf.Token, cf.UseConfiguredToken, cf.SetupID)
		if err != nil {
			return "", err
		}
		dnsContext, cancelDNS := context.WithTimeout(ctx, 30*time.Second)
		defer cancelDNS()
		plan, previewErr := service.setupDNS.Preview(dnsContext, cf.Token, strings.TrimPrefix(settings.AdminDomain, "caddyadmin."), cf.Address)
		if previewErr != nil {
			return "", previewErr
		}
		recovered := setupDNSRecovered(intent, plan, cf.Fingerprint)
		if plan.Fingerprint != cf.Fingerprint && !recovered {
			return "", domain.Conflict("DNS 记录已变化，请重新预览并确认")
		}
		if plan.Action != "reuse" {
			return "", domain.Invalid("请先确认并写入 DNS 配置，解析生效后再完成初始化")
		}
		// The wizard checks propagation from the browser via DoH. Server-side
		// resolution may filter private addresses; Cloudflare records and the
		// confirmed change fingerprint remain authoritative for completion.
		if err = service.secrets.WriteCloudflareToken(cf.Token); err != nil {
			return "", err
		}
		confirmedPlan := plan
		if recovered {
			confirmedPlan = intent.Plan
		}
		intent = SetupIntent{Plan: confirmedPlan, ConfigurationHash: binding, SnapshotHash: domain.Fingerprint(raw)}
		if err = service.setupIntent.Write(intent); err != nil {
			return "", err
		}
	}
	if err = service.snapshot.Write(raw); err != nil {
		if automaticDNS {
			return "", setupDNSLocalFailure()
		}
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
		if automaticDNS {
			return "", setupDNSLocalFailure()
		}
		return "", err
	}
	cleanup = false
	if automaticDNS {
		_ = service.setupIntent.Remove()
	}
	service.setActivePolicy(options.RuntimePolicy)
	return strings.TrimRight(settings.Origin, "/"), nil
}

func (service *Service) ResolveSetupToken(token string, useConfigured bool, setupID string) (string, error) {
	return service.configuredToken(token, useConfigured, setupID)
}
