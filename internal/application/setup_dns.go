package application

import (
	"context"
	"net/netip"
	"strings"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type SetupDNSPlan struct {
	ContextFingerprint string `json:"context_fingerprint"`
	ZoneID             string `json:"zone_id"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	Address            string `json:"address"`
	Action             string `json:"action"`
	RecordID           string `json:"record_id,omitempty"`
	OldAddress         string `json:"old_address,omitempty"`
	OldProxied         bool   `json:"old_proxied,omitempty"`
	Warning            string `json:"warning,omitempty"`
	Fingerprint        string `json:"fingerprint"`
}

type SetupDNS interface {
	Preview(context.Context, string, string, string) (SetupDNSPlan, error)
	Apply(context.Context, string, SetupDNSPlan) error
}

type SetupCloudflare struct {
	UseConfiguredToken bool   `json:"use_configured_token,omitempty"`
	SetupID            string `json:"setup_id,omitempty"`
	Token              string `json:"token"`
	Address            string `json:"address"`
	Fingerprint        string `json:"fingerprint"`
	Confirmed          bool   `json:"confirmed"`
}

type SetupIntent struct {
	Plan              SetupDNSPlan `json:"plan"`
	ConfigurationHash string       `json:"configuration_hash"`
	SnapshotHash      string       `json:"snapshot_hash"`
}
type SetupIntentStore interface {
	Read() (SetupIntent, error)
	Write(SetupIntent) error
	Remove() error
}

func (service *Service) PreviewSetupDNS(ctx context.Context, token, hostname, address string) (SetupDNSPlan, error) {
	initialized, err := service.repository.IsInitialized(ctx)
	if err != nil {
		return SetupDNSPlan{}, err
	}
	if initialized {
		return SetupDNSPlan{}, domain.Conflict("系统已经初始化")
	}
	if service.externalCaddy() || service.options.TestTLS || service.setupDNS == nil {
		return SetupDNSPlan{}, domain.Invalid("当前模式不支持本机自动配置 Cloudflare DNS")
	}
	return service.setupDNS.Preview(ctx, token, hostname, address)
}

func setupDNSLocalFailure() error {
	return &domain.AppError{Status: 503, Code: "setup_local_persistence", Message: "DNS 已配置，但本地初始化未完成；请保留数据并检查存储后重新预览提交，已生效记录会复用，不会自动撤销 DNS 变更"}
}

// ConfirmSetupDNS persists the intent before any remote write. Retrying reconciles
// the original plan against the actual Cloudflare record, including lost responses.
func (service *Service) ConfirmSetupDNS(ctx context.Context, settings SetupSettings, cf SetupCloudflare) (SetupDNSPlan, error) {
	service.setupMu.Lock()
	defer service.setupMu.Unlock()
	managed, err := ManagedSettingsForSetup(settings)
	if err != nil {
		return SetupDNSPlan{}, err
	}
	if !cf.Confirmed || cf.Fingerprint == "" {
		return SetupDNSPlan{}, domain.Invalid("请明确确认 DNS 变更")
	}
	cf.Token, err = service.configuredToken(cf.Token, cf.UseConfiguredToken, cf.SetupID)
	if err != nil {
		return SetupDNSPlan{}, err
	}
	plan, err := service.PreviewSetupDNS(ctx, cf.Token, strings.TrimPrefix(managed.AdminDomain, "caddyadmin."), cf.Address)
	if err != nil {
		return plan, err
	}
	if service.setupIntent == nil || service.secrets == nil {
		return plan, domain.Invalid("初始化 DNS 或 secret 存储未配置")
	}
	intent, err := service.setupIntent.Read()
	if err != nil {
		return plan, err
	}
	if plan.Fingerprint != cf.Fingerprint && !setupDNSRecovered(intent, plan, cf.Fingerprint) {
		return plan, domain.Conflict("DNS 记录已变化，请重新预览并确认")
	}
	if err = service.secrets.WriteCloudflareToken(cf.Token); err != nil {
		return plan, err
	}
	// Keep snapshot ownership when retrying an interrupted local completion.
	if !setupDNSRecovered(intent, plan, cf.Fingerprint) {
		if setupDNSIntentMatches(intent, plan) {
			intent.Plan = plan
		} else {
			intent = SetupIntent{Plan: plan}
		}
	}
	if err = service.setupIntent.Write(intent); err != nil {
		return plan, err
	}
	dnsContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = service.setupDNS.Apply(dnsContext, cf.Token, plan); err != nil {
		return plan, err
	}
	return plan, nil
}

func setupDNSRecovered(intent SetupIntent, plan SetupDNSPlan, fingerprint string) bool {
	return intent.Plan.Fingerprint == fingerprint && setupDNSIntentMatches(intent, plan)
}

func setupDNSIntentMatches(intent SetupIntent, plan SetupDNSPlan) bool {
	return intent.Plan.Name == plan.Name && intent.Plan.Type == plan.Type && intent.Plan.Address == plan.Address && intent.Plan.ZoneID == plan.ZoneID && intent.Plan.ContextFingerprint == plan.ContextFingerprint && plan.Action == "reuse"
}

// SetupDNSReport contains all checks, including unsuccessful DNS responses.
type SetupDNSReport struct {
	Verified bool            `json:"verified"`
	Queries  []SetupDNSQuery `json:"queries"`
}
type SetupDNSQuery struct {
	Name      string   `json:"name"`
	Resolver  string   `json:"resolver"`
	Addresses []string `json:"addresses"`
	Status    string   `json:"status"`
	Message   string   `json:"message"`
}

func (service *Service) CheckSetupDNS(ctx context.Context, settings SetupSettings, address string) (SetupDNSReport, error) {
	initialized, err := service.repository.IsInitialized(ctx)
	if err != nil {
		return SetupDNSReport{}, err
	}
	if initialized {
		return SetupDNSReport{}, domain.Conflict("系统已经初始化")
	}
	managed, err := ManagedSettingsForSetup(settings)
	if err != nil {
		return SetupDNSReport{}, domain.Invalid(err.Error())
	}
	address = strings.TrimSpace(address)
	if address == "" && !service.externalCaddy() && !service.options.TestTLS {
		return SetupDNSReport{}, domain.Invalid("请填写 DNS 目标 IP")
	}
	if address != "" {
		if _, err := netip.ParseAddr(address); err != nil {
			return SetupDNSReport{}, domain.Invalid("请填写有效的 DNS 目标 IP")
		}
	}
	if service.setupProbe == nil {
		return SetupDNSReport{}, domain.Invalid("DNS 检查未配置")
	}
	probeContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return service.setupProbe.DNSResults(probeContext, strings.TrimPrefix(managed.AdminDomain, "caddyadmin."), address, managed.Resolvers)
}
