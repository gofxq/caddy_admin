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
	DNSProvider           domain.DNSProvider      `json:"dns_provider,omitempty"`
	UseConfiguredPassword bool                    `json:"use_configured_password,omitempty"`
	UseConfiguredToken    bool                    `json:"use_configured_token,omitempty"`
	SetupID               string                  `json:"setup_id,omitempty"`
	ImportSettings        *domain.ManagedSettings `json:"import_settings,omitempty"`
	Cloudflare            *SetupCloudflare        `json:"dns,omitempty"`
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

type SetupCredentialStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
type SetupStatus struct {
	DNSProvider           domain.DNSProvider                           `json:"dns_provider"`
	DNSCredentials        map[domain.DNSProvider]SetupCredentialStatus `json:"dns_credentials"`
	Defaults              *SetupDefaults                               `json:"defaults,omitempty"`
	ConfigurationError    string                                       `json:"configuration_error,omitempty"`
	Automatic             SetupProgress                                `json:"automatic"`
	AdminPasswordStatus   string                                       `json:"admin_password_status"`
	CloudflareTokenStatus string                                       `json:"dns_token_status"`
	AdminPasswordError    string                                       `json:"admin_password_error,omitempty"`
	CloudflareTokenError  string                                       `json:"dns_token_error,omitempty"`
	SetupID               string                                       `json:"setup_id"`
	TestTLS               bool                                         `json:"test_tls"`
	Initialized           bool                                         `json:"initialized"`
	Resolvers             []string                                     `json:"resolver_suggestions"`
	ExternalCaddy         bool                                         `json:"external_caddy"`
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
		Domains: []domain.ManagedDomain{{ID: name, Name: name, Access: domain.DomainAccess("internet")}},
		LAN:     []string{"0.0.0.0/0", "::/0"}, UpstreamCIDRs: []string{"0.0.0.0/0", "::/0"},
		AllowedNames: []string{"*"}, DeniedIPs: []string{}, Resolvers: resolvers}
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

func (service *Service) ConfiguredSetupToken(setupID string) (string, error) {
	return service.configuredProviderToken(service.dnsProvider(), "", true, setupID)
}

func (service *Service) SetupStatus(ctx context.Context) (SetupStatus, error) {
	if progress := service.AutomaticSetupProgress(); progress.Status == "running" {
		return SetupStatus{Automatic: progress}, nil
	}
	service.setupMu.Lock()
	defer service.setupMu.Unlock()
	initialized, err := service.repository.IsInitialized(ctx)
	resolvers := []string{}
	if service.resolverSuggestions != nil {
		resolvers = service.resolverSuggestions()
	}
	status := SetupStatus{Initialized: initialized, Resolvers: resolvers, ExternalCaddy: service.externalCaddy(), TestTLS: service.options.TestTLS, SetupID: service.setupID}
	status.Automatic = service.AutomaticSetupProgress()
	if !initialized && err == nil {
		defaults, configurationErr := service.setupDefaults()
		status.Defaults = &defaults
		if configurationErr != nil {
			status.ConfigurationError = configurationErr.Error()
		}
	}
	status.AdminPasswordStatus, status.AdminPasswordError = credentialStatus(service.options.SetupPassword, domain.ValidatePassword)
	status.DNSProvider = service.dnsProvider()
	if _, providerErr := domain.NormalizeDNSProvider(status.DNSProvider); providerErr != nil {
		status.ConfigurationError = providerErr.Error()
	}
	status.DNSCredentials = map[domain.DNSProvider]SetupCredentialStatus{}
	for _, p := range []domain.DNSProvider{domain.DNSProviderCloudflare, domain.DNSProviderDNSPod} {
		state, msg := credentialStatus(service.setupToken(p), func(t string) error { return domain.ValidateDNSToken(p, t) })
		status.DNSCredentials[p] = SetupCredentialStatus{state, msg}
	}
	selected := status.DNSCredentials[status.DNSProvider]
	status.CloudflareTokenStatus, status.CloudflareTokenError = selected.Status, selected.Error
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
		result.Checks = append(result.Checks, setupCheck("admin_dns", "info", "服务器 DNS 未返回控制台地址，可能受缓存或内网地址过滤影响；此项仅作提示，不阻止初始化。证书与正式入口就绪状态将在初始化后核对。"))
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
	if service.AutomaticSetupProgress().Status == "running" {
		return "", domain.Conflict("自动初始化正在后台执行，请等待状态核对完成")
	}
	service.setupMu.Lock()
	defer service.setupMu.Unlock()
	if service.AutomaticSetupProgress().Status == "running" {
		return "", domain.Conflict("自动初始化正在后台执行，请等待状态核对完成")
	}
	return service.completeSetupLocked(ctx, request)
}

func (service *Service) completeSetupLocked(ctx context.Context, request SetupRequest) (string, error) {
	if request.Cloudflare != nil {
		cloudflare := *request.Cloudflare
		request.Cloudflare = &cloudflare
	}
	initialized, err := service.repository.IsInitialized(ctx)
	if err != nil {
		return "", err
	}
	if initialized {
		return "", domain.Conflict("系统已经初始化")
	}
	if service.setupUncertain {
		return "", &domain.AppError{Status: 503, Code: "setup_result_uncertain", Message: "初始化保存结果待核对；请保留数据并检查存储，重启后先核对状态，不能重复提交"}
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
	provider, providerErr := domain.NormalizeDNSProvider(request.DNSProvider)
	if providerErr != nil {
		return "", providerErr
	}
	if request.DNSProvider == "" {
		provider = service.dnsProvider()
	}
	if request.Cloudflare != nil {
		provider, err = domain.NormalizeDNSProvider(request.Cloudflare.Provider)
		if err != nil {
			return "", err
		}
		request.Cloudflare.Provider = provider
	}
	options.DNSProvider = provider
	options.RuntimePolicy = setupActiveSettings(settings)
	certificate := domain.CertificateStatus{Mode: domain.CertificateModeBootstrapInternal, ActivationStatus: "idle", PublicStatus: "unknown"}
	if options.AdminURL != "" {
		certificate = domain.CertificateStatus{Mode: domain.CertificateMode(provider), ActivationStatus: "success", PublicStatus: "pending"}
	}
	automaticDNS := !service.externalCaddy() && !options.TestTLS
	if automaticDNS {
		if request.Cloudflare == nil || request.Cloudflare.Fingerprint == "" {
			return "", domain.Invalid("请填写 DNS 凭据并检查 DNS 记录")
		}
		address, addressErr := domain.NormalizeSetupAddress(request.Cloudflare.Address)
		if addressErr != nil {
			return "", addressErr
		}
		request.Cloudflare.Address = address
		if service.dnsAdapter(provider) == nil || service.setupIntent == nil || service.secrets == nil || service.bootstrapTLS == nil {
			return "", domain.Invalid("初始化 DNS 或 secret 存储未配置")
		}
		certificate = domain.CertificateStatus{Mode: domain.CertificateMode(provider), ActivationStatus: "success", PublicStatus: "pending"}
	} else if request.Cloudflare != nil {
		return "", domain.Invalid("当前模式不接受本机 DNS 凭据")
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
		cf.Token, err = service.configuredProviderToken(provider, cf.Token, cf.UseConfiguredToken, cf.SetupID)
		if err != nil {
			return "", err
		}
		dnsContext, cancelDNS := context.WithTimeout(ctx, 30*time.Second)
		defer cancelDNS()
		plan, previewErr := service.PreviewSetupDNSProvider(dnsContext, provider, cf.Token, strings.TrimPrefix(settings.AdminDomain, "caddyadmin."), cf.Address)
		if previewErr != nil {
			return "", previewErr
		}
		if err = checkDNSWriteCooldown(intent, plan); err != nil {
			return "", err
		}
		recovered := cf.Confirmed && setupDNSRecovered(intent, plan, cf.Fingerprint)
		if plan.Fingerprint != cf.Fingerprint && !recovered {
			return "", domain.Conflict("DNS 记录已变化，请重新检查，并按需确认变更")
		}
		if plan.Action != "reuse" {
			return "", domain.Invalid("DNS 记录需要新增或修改，请先预览并明确确认变更，解析生效后再完成初始化")
		}
		if plan.Warning != "" && !cf.Confirmed {
			return "", domain.Invalid("DNS 记录包含需确认的警告；请重新核对记录并明确确认后完成初始化")
		}
		// The wizard checks propagation from the browser via DoH. Server-side
		// resolution may filter private addresses; Cloudflare records and the
		// checked or confirmed change fingerprint remain authoritative for completion.
		if err = service.writeDNSToken(provider, cf.Token); err != nil {
			return "", err
		}
		if err = service.caddy.Validate(ctx, raw); err != nil {
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
	credentials := SetupCredentials{Username: request.Username, Password: request.Password}
	if len(imported) > 0 {
		err = service.repository.CompleteSetupWithDraft(ctx, credentials, settings, certificate, imported)
	} else {
		err = service.repository.CompleteSetup(ctx, credentials, settings, certificate)
	}
	if err != nil {
		// A failed commit response is not proof of rollback. Never remove the
		// startup snapshot before checking the durable initialization state.
		checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		committed, checkErr := service.repository.IsInitialized(checkCtx)
		cancel()
		if committed && checkErr == nil {
			if automaticDNS {
				_ = service.setupIntent.Remove()
			}
			service.setActivePolicy(settings)
			return settings.Origin, nil
		}
		if checkErr != nil {
			service.setupUncertain = true
		}
		if !automaticDNS && checkErr == nil && !committed {
			// This submission wrote a previously absent snapshot. A definitive
			// non-commit in modes without a recovery intent may safely remove
			// that candidate, allowing a retry after storage has been repaired.
			_ = service.snapshot.Remove()
		}
		if automaticDNS {
			return "", setupDNSLocalFailure()
		}
		return "", err
	}
	if automaticDNS {
		_ = service.setupIntent.Remove()
	}
	service.setActivePolicy(options.RuntimePolicy)
	return strings.TrimRight(settings.Origin, "/"), nil
}

func (service *Service) ResolveSetupToken(token string, useConfigured bool, setupID string) (string, error) {
	return service.configuredProviderToken(service.dnsProvider(), token, useConfigured, setupID)
}
