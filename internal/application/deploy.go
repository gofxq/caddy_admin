package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type definitiveLoadRejection interface {
	error
	Definitive() bool
}

func (service *Service) publicCertificateReady(ctx context.Context) bool {
	if service.externalCaddy() {
		return true
	}
	status, err := service.repository.CertificateStatus(ctx)
	if err != nil || status.Mode != domain.CertificateModeCloudflare || status.ActivationStatus != "success" || service.options.TestTLS {
		return false
	}
	if !service.runtimeMatchesSnapshot(ctx) || service.certificates == nil {
		return false
	}
	return service.certificates.PublicReady(ctx, service.consoleCertificateQuery(status.Mode))
}

func (service *Service) requirePublicCertificate(ctx context.Context) error {
	if !service.publicCertificateReady(ctx) {
		return &domain.AppError{Status: 409, Code: "public_certificate_pending", Message: "请先启用并等待公网可信证书就绪，再校验或发布服务"}
	}
	return nil
}

func (service *Service) runtimeMatchesSnapshot(ctx context.Context) bool {
	if service.snapshot == nil {
		return false
	}
	runtime, err := service.caddy.Read(ctx)
	if err != nil {
		return false
	}
	snapshot, err := service.snapshot.Read()
	return err == nil && domain.Fingerprint(runtime) == domain.Fingerprint(snapshot)
}

func (service *Service) expected(ctx context.Context) (string, []domain.Service, error) {
	deployment, err := service.repository.Latest(ctx)
	if err == nil {
		return deployment.Hash, deployment.Services, nil
	}
	if !domain.IsMissing(err) {
		return "", nil, err
	}
	if service.snapshot == nil {
		return "", nil, fmt.Errorf("snapshot store is not configured")
	}
	raw, err := service.snapshot.Read()
	if err != nil {
		return "", nil, err
	}
	return domain.Fingerprint(raw), []domain.Service{}, nil
}

func (service *Service) Preview(ctx context.Context, rollback string) (domain.Preview, error) {
	draft, err := service.repository.Draft(ctx)
	if err != nil {
		return domain.Preview{}, err
	}
	preview := domain.Preview{Revision: draft.Revision, Services: draft.Services, Settings: draft.Settings, ActiveSettings: service.activeSettings(), RollbackID: rollback}
	if rollback != "" {
		history, historyErr := service.repository.Deployment(ctx, rollback)
		if historyErr != nil {
			return preview, historyErr
		}
		if history.Status != "success" {
			return preview, domain.Invalid("仅可回滚成功版本")
		}
		preview.Services, preview.RollbackConfig, preview.RollbackHash = history.Services, history.Config, history.Hash
		preview.Settings = service.activeSettings()
	}
	policy, err := service.targetPolicyForSettings(ctx, preview.Settings)
	if err != nil {
		return preview, err
	}
	for index, item := range preview.Services {
		preview.Services[index], err = domain.NormalizeService(ctx, policy, item)
		if err != nil {
			return preview, err
		}
	}
	preview.Config, err = domain.Generate(service.caddyConfigFor(preview.Settings), preview.Services)
	if err != nil {
		return preview, err
	}
	preview.SettingsChanged = !sameSettings(preview.Settings, preview.ActiveSettings)
	preview.Hash = domain.Fingerprint(preview.Config)
	var before []domain.Service
	preview.ExpectedHash, before, err = service.expected(ctx)
	if err != nil {
		return preview, err
	}
	preview.Changes = domain.Diff(before, preview.Services)
	runtime, err := service.caddy.Read(ctx)
	if err != nil {
		return preview, err
	}
	if service.externalCaddy() {
		preview.Config, err = domain.PreserveExternalSettings(preview.Config, runtime)
		if err != nil {
			return preview, err
		}
		preview.Hash = domain.Fingerprint(preview.Config)
	}
	preview.RuntimeHash = domain.Fingerprint(runtime)
	preview.Drift = preview.RuntimeHash != preview.ExpectedHash
	return preview, nil
}

func (service *Service) policyHash() string { return service.policyHashFor(service.activeSettings()) }
func (service *Service) policyHashFor(settings domain.ManagedSettings) string {
	raw, _ := json.Marshal(struct {
		Config domain.CaddyConfig     `json:"config"`
		Policy domain.ManagedSettings `json:"policy"`
	}{service.caddyConfigFor(settings), settings})
	return domain.Fingerprint(raw)
}

func (service *Service) Validate(ctx context.Context, revision int64, rollback, actor string) (domain.Preview, error) {
	if err := service.requirePublicCertificate(ctx); err != nil {
		return domain.Preview{}, err
	}
	preview, err := service.validate(ctx, revision, rollback, actor)
	if err != nil {
		auditContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if auditErr := service.repository.ValidationAudit(auditContext, actor, preview.Revision, rollback, err); auditErr != nil {
			return preview, auditErr
		}
	}
	return preview, err
}

func (service *Service) validate(ctx context.Context, revision int64, rollback, actor string) (domain.Preview, error) {
	preview, err := service.Preview(ctx, rollback)
	if err == nil && preview.Revision != revision {
		err = domain.Conflict("草稿版本已变化，请刷新预览")
	}
	if err == nil {
		err = service.requireServiceCertificates(ctx, preview.Settings, preview.Services)
	}
	if err == nil {
		err = service.caddy.Validate(ctx, preview.Config)
	}
	if err != nil {
		return preview, err
	}
	preview.ValidationID = domain.ID()
	created := time.Now().Unix()
	preview.ValidationExpiresAt = time.Unix(created, 0).Add(15 * time.Minute).UTC()
	services, err := json.Marshal(preview.Services)
	if err != nil {
		return preview, err
	}
	err = service.repository.SaveValidation(ctx, ValidationRecord{ID: preview.ValidationID, Revision: preview.Revision, Config: preview.Config, Services: string(services), BaseHash: preview.RuntimeHash, PolicyHash: service.policyHashFor(preview.Settings), Settings: preview.Settings, RollbackID: rollback, Created: created}, actor)
	return preview, err
}

func (service *Service) Begin(ctx context.Context, request domain.PublishRequest, actor string) (domain.Deployment, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.requirePublicCertificate(ctx); err != nil {
		return domain.Deployment{}, err
	}
	if len(request.Idempotency) < 16 || len(request.Idempotency) > 128 {
		return domain.Deployment{}, domain.Invalid("缺少有效的幂等标识")
	}
	requestJSON, _ := json.Marshal(request)
	requestHash := domain.Fingerprint(requestJSON)
	existing, err := service.repository.DeploymentByIdempotency(ctx, request.Idempotency)
	if err == nil {
		if existing.RequestHash != requestHash {
			return existing, domain.Conflict("幂等标识已用于其他请求")
		}
		return existing, nil
	}
	if !domain.IsMissing(err) {
		return domain.Deployment{}, err
	}
	if _, err = service.repository.Pending(ctx); err == nil {
		return domain.Deployment{}, domain.Conflict("已有发布待完成或核对")
	}
	if !domain.IsMissing(err) {
		return domain.Deployment{}, err
	}
	validation, err := service.repository.Validation(ctx, request.ValidationID)
	if err != nil {
		return domain.Deployment{}, domain.Conflict("校验已失效，请重新校验")
	}
	draft, err := service.repository.Draft(ctx)
	if err != nil {
		return domain.Deployment{}, err
	}
	policySettings := draft.Settings
	if validation.RollbackID != "" {
		policySettings = service.activeSettings()
	}
	if validation.Revision != request.Revision || draft.Revision != request.Revision || validation.PolicyHash != service.policyHashFor(policySettings) || validation.Created <= time.Now().Add(-15*time.Minute).Unix() {
		return domain.Deployment{}, domain.Conflict("校验或部署策略已变化，请重新校验")
	}
	var list []domain.Service
	if err = json.Unmarshal([]byte(validation.Services), &list); err != nil {
		return domain.Deployment{}, err
	}
	policy, err := service.targetPolicyForSettings(ctx, validation.Settings)
	if err != nil {
		return domain.Deployment{}, err
	}
	for _, item := range list {
		normalized, normalizeErr := domain.NormalizeService(ctx, policy, item)
		if normalizeErr != nil {
			return domain.Deployment{}, normalizeErr
		}
		if normalized.Dial != item.Dial {
			return domain.Deployment{}, domain.Conflict("上游解析已变化，请重新校验")
		}
	}
	if err := service.requireServiceCertificates(ctx, validation.Settings, list); err != nil {
		return domain.Deployment{}, err
	}
	runtimeConfig, err := service.caddy.Read(ctx)
	if err != nil {
		return domain.Deployment{}, err
	}
	runtimeHash := domain.Fingerprint(runtimeConfig)
	if runtimeHash != validation.BaseHash || runtimeHash != request.ExpectedHash {
		return domain.Deployment{}, domain.Conflict("运行配置已变化，请刷新预览")
	}
	expectedHash, before, err := service.expected(ctx)
	if err != nil {
		return domain.Deployment{}, err
	}
	if runtimeHash != expectedHash && !request.ConfirmDrift {
		return domain.Deployment{}, domain.Conflict("存在外部配置漂移，需要明确确认覆盖")
	}
	active := service.activeSettings()
	if validation.Settings.ConsoleLANOnly && !sameSettings(active, validation.Settings) && !consoleSourceAllowed(validation.Settings, request.ClientAddress) {
		return domain.Deployment{}, domain.Invalid("新规则会阻止当前设备访问控制台，请从目标网络登录后再发布")
	}
	if widensAccess(active, validation.Settings, before, list) && !request.ConfirmExposure {
		return domain.Deployment{}, domain.Invalid("请明确确认放开访问范围")
	}
	deployment := domain.Deployment{ID: domain.ID(), Revision: validation.Revision, Status: "applying", Config: validation.Config, Services: list, Settings: validation.Settings, BaseHash: validation.BaseHash, Hash: domain.Fingerprint(validation.Config), Actor: actor, Created: timestamp(), RollbackID: validation.RollbackID, Changes: domain.Diff(before, list), Idempotency: request.Idempotency, RequestHash: requestHash}
	deployment, err = service.repository.BeginDeployment(ctx, validation.Revision, deployment)
	if err == nil {
		service.owned[deployment.ID] = struct{}{}
	}
	return deployment, err
}

func (service *Service) Apply(id string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	defer delete(service.owned, id)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	deployment, err := service.repository.Deployment(ctx, id)
	if err != nil || deployment.Status != "applying" {
		return
	}
	runtime, err := service.caddy.Read(ctx)
	if err != nil {
		service.recordFinish(context.Background(), deployment, "uncertain", "加载前无法核对 Caddy，等待恢复核对")
		return
	}
	if domain.Fingerprint(runtime) != deployment.BaseHash {
		service.recordFinish(ctx, deployment, "failed", "发布前运行配置变化，未加载")
		return
	}
	err = service.caddy.Load(ctx, deployment.Config)
	var rejected definitiveLoadRejection
	if errors.As(err, &rejected) && rejected.Definitive() {
		service.recordFinish(context.Background(), deployment, "failed", rejected.Error())
		return
	}
	checkContext, checkCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer checkCancel()
	if err = service.reconcile(checkContext, deployment); err != nil {
		slog.Error("deployment_reconcile_failed", "deployment_id", deployment.ID, "revision", deployment.Revision, "stage", "reconcile", "error_class", domain.ErrorClass(err))
	}
}

func (service *Service) recordFinish(ctx context.Context, deployment domain.Deployment, status, message string) {
	if err := service.repository.FinishDeployment(ctx, deployment, status, message); err != nil {
		slog.Error("deployment_finish_failed", "deployment_id", deployment.ID, "revision", deployment.Revision, "stage", status, "error_class", domain.ErrorClass(err))
	}
}

func (service *Service) reconcile(ctx context.Context, deployment domain.Deployment) error {
	runtime, err := service.caddy.Read(ctx)
	if err != nil {
		return service.repository.FinishDeployment(ctx, deployment, "uncertain", "Caddy 不可达，发布结果待核对")
	}
	hash := domain.Fingerprint(runtime)
	if hash == deployment.Hash {
		if service.snapshot == nil {
			service.recordFinish(ctx, deployment, "uncertain", "已应用但启动快照未持久化，需恢复后核对")
			return fmt.Errorf("snapshot store is not configured")
		}
		if err = service.snapshot.Write(deployment.Config); err != nil {
			service.recordFinish(ctx, deployment, "uncertain", "已应用但启动快照未持久化，需恢复后核对")
			return err
		}
		service.policyMu.Lock()
		defer service.policyMu.Unlock()
		if err = service.repository.FinishDeployment(ctx, deployment, "success", ""); err != nil {
			return err
		}
		service.setActivePolicy(deployment.Settings)
		return nil
	}
	if hash == deployment.BaseHash {
		return service.repository.FinishDeployment(ctx, deployment, "failed", "运行配置仍为原版本，发布未完成")
	}
	return service.repository.FinishDeployment(ctx, deployment, "uncertain", "运行配置与候选及原版本均不一致，请使用救援流程")
}

func (service *Service) Recover(ctx context.Context) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	deployment, err := service.repository.Pending(ctx)
	if err == nil {
		if _, owned := service.owned[deployment.ID]; owned {
			return nil
		}
		return service.reconcile(ctx, deployment)
	}
	if !domain.IsMissing(err) {
		return err
	}
	return nil
}
