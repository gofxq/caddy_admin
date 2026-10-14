package application

import (
	"context"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

// RecheckAutomaticSetup only reads deployment settings, DNS and preflight probes.
// It never writes DNS, secrets, snapshots or the database, even when all checks
// pass. An interrupted submission must be reconciled through the manual flow.
func (service *Service) RecheckAutomaticSetup(ctx context.Context, setupID string) (SetupProgress, error) {
	if setupID != service.setupID {
		return SetupProgress{}, domain.Conflict("初始化状态已变化，请重新读取状态")
	}
	if !service.setupMu.TryLock() {
		return SetupProgress{}, domain.Conflict("初始化操作正在执行，请等待后再重新检查")
	}
	defer service.setupMu.Unlock()
	if !service.AutomaticSetupProgress().CanRecheck || service.setupUncertain || service.externalCaddy() || service.options.TestTLS {
		return SetupProgress{}, domain.Conflict("当前状态不能重新检查，请先核对实例结果或通过向导处理")
	}
	initialized, err := service.repository.IsInitialized(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return SetupProgress{}, &domain.AppError{Status: 503, Code: "setup_check_interrupted", Message: "重新检查已中断，本次未保存初始化数据；请读取状态后重试"}
		}
		service.setSetupProgress("uncertain", "configuration", "无法核对实例状态，请保留数据并检查存储")
		return SetupProgress{}, domain.Conflict("无法核对实例状态，不能重试初始化")
	}
	if initialized {
		return SetupProgress{}, domain.Conflict("系统已经初始化，请通过交接页面进入控制台")
	}
	intent, err := service.setupIntent.Read()
	exists, snapshotErr := service.snapshot.Exists()
	if err != nil || snapshotErr != nil || exists || intent.Plan.Fingerprint != "" || intent.ConfigurationHash != "" || intent.SnapshotHash != "" {
		service.setSetupProgress("uncertain", "configuration", "检测到已有快照或初始化意图，请先核对结果；本次未重放任何保存操作")
		return SetupProgress{}, domain.Conflict("持久化结果需先核对，不能通过重新检查重放初始化")
	}
	defaults, err := service.setupDefaults()
	if err != nil {
		return SetupProgress{}, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	_, preflight, ready := service.checkAutomaticSetup(checkCtx, defaults)
	if ready {
		service.setSetupProgress("action_required", "preflight", "DoH、Cloudflare 记录与服务器预检已满足初始化条件；控制台服务器 DNS 未解析仅作提示。本次只重新检查，未保存初始化数据。请进入向导核对并明确确认完成初始化。", preflight.Checks...)
	}
	return service.AutomaticSetupProgress(), nil
}

// Both startup and explicit rechecks share the same DoH -> Cloudflare read-only
// preview -> server preflight order. Only the original startup caller may save.
func (service *Service) checkAutomaticSetup(ctx context.Context, defaults SetupDefaults) (SetupDNSPlan, SetupPreflight, bool) {
	service.setSetupProgress("running", "doh", "")
	report, err := service.setupDoH.Check(ctx, defaults.Domain, defaults.Address)
	dohCheck := setupCheck("setup_doh", "pass", "")
	if err != nil || !report.Verified {
		dohCheck.Status = "warning"
	}
	service.setSetupProgress("running", "dns", "", dohCheck)
	plan, err := service.setupDNS.Preview(ctx, service.options.SetupToken, defaults.Domain, defaults.Address)
	if err != nil {
		service.setSetupProgress("failed", "dns", safeAutomaticSetupError(err), dohCheck)
		return plan, SetupPreflight{}, false
	}
	if plan.Action != "reuse" || plan.Warning != "" {
		service.setSetupProgress("action_required", "dns", "DNS 需要变更或存在需确认的记录；请进入向导预览并明确确认，自动流程未修改 DNS", dohCheck)
		return plan, SetupPreflight{}, false
	}
	if dohCheck.Status != "pass" {
		service.setSetupProgress("action_required", "doh", "Cloudflare 记录已匹配，但 DoH 解析未通过；请检查传播、缓存或 DoH 网络后重新检查，尚未保存初始化数据", dohCheck)
		return plan, SetupPreflight{}, false
	}
	service.setSetupProgress("running", "preflight", "", dohCheck)
	preflight := service.PreflightSetup(ctx, SetupSettings{Domain: defaults.Domain, Resolvers: defaults.Resolvers})
	preflight.Checks = append([]SetupCheck{dohCheck}, preflight.Checks...)
	if !preflight.CanComplete || preflight.RequiresAcknowledgement {
		service.setSetupProgress("action_required", "preflight", "服务器预检有阻断或警告，详情如下；尚未保存初始化数据", preflight.Checks...)
		return plan, preflight, false
	}
	return plan, preflight, true
}
