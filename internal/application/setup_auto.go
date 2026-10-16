package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

// SetupDefaults is available only through the existing Setup interface. Public
// progress contains no deployment values, resolver addresses or credentials.
type SetupDefaults struct {
	Domain    string   `json:"domain,omitempty"`
	Username  string   `json:"username,omitempty"`
	Address   string   `json:"address,omitempty"`
	Resolvers []string `json:"resolvers,omitempty"`
}
type SetupProgress struct {
	Status     string               `json:"status"`
	Stage      string               `json:"stage"`
	Message    string               `json:"message,omitempty"`
	Checks     []SetupProgressCheck `json:"checks,omitempty"`
	CanRecheck bool                 `json:"can_recheck,omitempty"`
}

func (service *Service) AutomaticSetupProgress() SetupProgress {
	service.autoMu.RLock()
	defer service.autoMu.RUnlock()
	progress := service.autoProgress
	progress.Checks = append([]SetupProgressCheck(nil), progress.Checks...)
	progress.CanRecheck = (progress.Status == "failed" || progress.Status == "action_required") && (progress.Stage == "doh" || progress.Stage == "dns" || progress.Stage == "preflight")
	return progress
}
func (service *Service) setSetupProgress(status, stage, message string, checks ...SetupCheck) {
	service.autoMu.Lock()
	defer service.autoMu.Unlock()
	service.autoProgress = SetupProgress{Status: status, Stage: stage, Message: message, Checks: publicSetupChecks(checks)}
}

// Invalid nonempty values are reported without echoing their contents. Valid
// defaults may still be used in the manual wizard after a partial configuration.
func (service *Service) setupDefaults() (SetupDefaults, error) {
	input := service.options.SetupDefaults
	result := SetupDefaults{Username: "admin", Resolvers: []string{"1.1.1.1"}}
	var problem error
	if input.Username != "" {
		if len(input.Username) > 64 {
			problem = domain.Invalid("SETUP_ADMIN_USERNAME 必须为 1–64 字节")
		} else {
			result.Username = input.Username
		}
	}
	if strings.TrimSpace(input.Domain) != "" {
		settings, err := ManagedSettingsForSetup(SetupSettings{Domain: input.Domain})
		if err != nil {
			problem = domain.Invalid("SETUP_DOMAIN 无效，请修正部署配置或在向导填写有效域名")
		} else {
			result.Domain = settings.AdminBase()
		}
	}
	if strings.TrimSpace(input.Address) != "" {
		address, err := domain.NormalizeSetupAddress(input.Address)
		if err != nil {
			problem = domain.Invalid("SETUP_CADDY_IP 无效，请明确填写可访问的 IPv4 或 IPv6 地址")
		} else {
			result.Address = address
		}
	}
	if len(input.Resolvers) > 0 {
		// Validate resolvers using the same managed settings rules as manual setup.
		_, err := ManagedSettingsForSetup(SetupSettings{Domain: "setup.example.com", Resolvers: input.Resolvers})
		if err != nil {
			problem = domain.Invalid("SETUP_RESOLVERS 无效，请填写逗号分隔的服务器 DNS IP 或 IP:端口")
		} else {
			result.Resolvers = append([]string{}, input.Resolvers...)
		}
	}
	return result, problem
}

// StartAutomaticSetup runs at most once per process. Page reads cannot call it.
// Holding setupMu for the whole task also protects DNS intent and snapshot writes
// from concurrent manual submission; once paused, this task never resumes itself.
func (service *Service) StartAutomaticSetup(ctx context.Context, done func()) <-chan struct{} {
	service.autoOnce.Do(func() {
		service.setupMu.Lock()
		service.setSetupProgress("running", "configuration", "")
		go func() {
			defer close(service.autoDone)
			defer service.setupMu.Unlock()
			taskCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			initialized, err := service.repository.IsInitialized(taskCtx)
			if err != nil {
				service.setSetupProgress("uncertain", "configuration", "无法核对实例状态；请保留数据并检查存储，不能重复初始化")
				return
			}
			if initialized || service.externalCaddy() || service.options.TestTLS {
				service.setSetupProgress("manual", "configuration", "")
				return
			}
			defaults, err := service.setupDefaults()
			complete := strings.TrimSpace(service.options.SetupPassword) != "" && strings.TrimSpace(service.setupToken(service.dnsProvider())) != "" && strings.TrimSpace(service.options.SetupDefaults.Domain) != "" && strings.TrimSpace(service.options.SetupDefaults.Address) != ""
			if !complete {
				service.setSetupProgress("manual", "configuration", "")
				return
			}
			if err != nil {
				service.setSetupProgress("failed", "configuration", err.Error())
				return
			}
			if domain.ValidatePassword(service.options.SetupPassword) != nil || domain.ValidateDNSToken(service.dnsProvider(), service.setupToken(service.dnsProvider())) != nil {
				service.setSetupProgress("failed", "configuration", "预配置密码或 DNS 服务商 Token 格式无效，请修正部署配置后重建容器")
				return
			}
			if service.snapshot == nil || service.setupIntent == nil || service.dnsAdapter(service.dnsProvider()) == nil || service.setupDoH == nil {
				service.setSetupProgress("failed", "configuration", "初始化存储或 DNS 检查不可用，请检查部署配置")
				return
			}
			intent, err := service.setupIntent.Read()
			exists, snapshotErr := service.snapshot.Exists()
			if err != nil || snapshotErr != nil || exists || intent.Plan.Fingerprint != "" || intent.ConfigurationHash != "" || intent.SnapshotHash != "" {
				service.setSetupProgress("uncertain", "configuration", "检测到已有快照或初始化意图，需先核对持久化状态；请保留数据，按运维文档检查后通过向导接续")
				return
			}
			plan, preflight, ready := service.checkAutomaticSetup(taskCtx, defaults)
			if !ready {
				return
			}
			settings := SetupSettings{Domain: defaults.Domain, Resolvers: defaults.Resolvers}
			service.setSetupProgress("running", "saving", "", preflight.Checks...)
			_, err = service.completeSetupLocked(taskCtx, SetupRequest{Username: defaults.Username, UseConfiguredPassword: true, UseConfiguredToken: true, SetupID: service.setupID, Settings: settings, Cloudflare: &SetupCloudflare{Provider: service.dnsProvider(), Address: defaults.Address, Fingerprint: plan.Fingerprint}})
			if err != nil {
				var appErr *domain.AppError
				if errors.As(err, &appErr) && (appErr.Code == "setup_local_persistence" || appErr.Code == "existing_snapshot" || appErr.Code == "setup_result_uncertain") {
					service.setSetupProgress("uncertain", "saving", "本地初始化保存结果需核对；请保留数据并检查存储，已生效 DNS 不会自动撤销，不能盲目重试")
				} else {
					service.setSetupProgress("action_required", "preflight", "初始化未完成；DNS、预检或 Caddy 校验未通过，请在向导重新核对后提交")
				}
				return
			}
			service.setSetupProgress("saved", "restarting", "初始化已保存，正在重启服务；DNS、证书与正式入口就绪状态将在交接页面分别核对", preflight.Checks...)
			if done != nil {
				done()
			}
		}()
	})
	return service.autoDone
}
func safeAutomaticSetupError(err error) string {
	// Messages originate from our adapters; never forward arbitrary transport or
	// module errors, whose text may contain secrets or internal filesystem paths.
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		switch appErr.Code {
		case "cloudflare_unavailable", "cloudflare_api":
			return "DNS 服务商 暂时不可达或操作未完成；请检查网络和 Token 权限后通过向导重试"
		case "invalid":
			return "DNS 服务商 凭据或 DNS 记录无法复用；请检查 Zone Read、DNS Edit 权限与记录冲突后通过向导接续"
		}
	}
	return "凭据或 DNS 检查失败；请检查 DNS 服务商 连接、Token 权限和记录冲突后通过向导重试"
}
