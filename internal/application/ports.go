package application

import (
	"context"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type ValidationRecord struct {
	ID         string
	Revision   int64
	Config     []byte
	Services   string
	BaseHash   string
	PolicyHash string
	RollbackID string
	Created    int64
}

type SetupCredentials struct {
	Username string
	Password string
}

type AuthRepository interface {
	Login(context.Context, string, string) (domain.Session, error)
	Session(context.Context, string) (domain.Session, error)
	Logout(context.Context, string) error
	ChangePassword(context.Context, string, string) error
	ResetPassword(context.Context, string) error
	LoginAllowed(context.Context, string) bool
	RecordLoginFailure(context.Context, string) error
}

type DraftRepository interface {
	ReplaceDraft(context.Context, int64, []domain.Service, string) (domain.Draft, error)
	Draft(context.Context) (domain.Draft, error)
	DraftRevision(context.Context, int64) (domain.Draft, error)
	SaveService(context.Context, int64, domain.Service, bool, string) (domain.Draft, error)
}

type DeploymentRepository interface {
	Deployment(context.Context, string) (domain.Deployment, error)
	DeploymentByIdempotency(context.Context, string) (domain.Deployment, error)
	Latest(context.Context) (domain.Deployment, error)
	Pending(context.Context) (domain.Deployment, error)
	Deployments(context.Context, int, int) ([]domain.Deployment, error)
	BeginDeployment(context.Context, int64, domain.Deployment) (domain.Deployment, error)
	FinishDeployment(context.Context, domain.Deployment, string, string) error
	SaveValidation(context.Context, ValidationRecord, string) error
	Validation(context.Context, string) (ValidationRecord, error)
}

type AuditRepository interface {
	Audit(context.Context, string, string, string, string, int64, int64) error
	Audits(context.Context, int, int) ([]domain.AuditEvent, error)
	ValidationAudit(context.Context, string, int64, string, error) error
}

type SettingsRepository interface {
	CompleteSetupWithDraft(context.Context, SetupCredentials, domain.ManagedSettings, domain.CertificateStatus, []domain.Service) error
	ManagedSettings(context.Context) (domain.ManagedSettings, error)
	CertificateStatus(context.Context) (domain.CertificateStatus, error)
	SetCertificateStatus(context.Context, domain.CertificateStatus) error
	SetCertificateStatusAudit(context.Context, domain.CertificateStatus, string, string) error
	IsInitialized(context.Context) (bool, error)
	CompleteSetup(context.Context, SetupCredentials, domain.ManagedSettings, domain.CertificateStatus) error
}

type HealthRepository interface {
	Ping(context.Context) error
}

type Repository interface {
	AuthRepository
	DraftRepository
	DeploymentRepository
	AuditRepository
	SettingsRepository
	HealthRepository
}

type CaddyPort interface {
	Read(context.Context) ([]byte, error)
	Validate(context.Context, []byte) error
	Load(context.Context, []byte) error
	BuildInfo(context.Context) (string, bool)
}

type Resolver interface {
	domain.TargetResolver
}

type SetupProbe interface {
	ResolverReachable(context.Context, []string) bool
	AdminDNS(context.Context, string, []string) bool
	ExternalConsole(context.Context, string, string) bool
}

type CertificateQuery struct {
	Domains      []string
	ProbeAddress string
	Mode         domain.CertificateMode
	TestTLS      bool
}

type CertificateProbe interface {
	Certificates(context.Context, CertificateQuery) []domain.Certificate
	PublicReady(context.Context, CertificateQuery) bool
}

type SnapshotStore interface {
	Exists() (bool, error)
	Read() ([]byte, error)
	Write([]byte) error
	Remove() error
}

type SecretStore interface {
	WriteCloudflareToken(string) error
	CloudflareTokenConfigured() bool
}

type BootstrapCertificate interface {
	Ensure(certPath, keyPath, origin string) error
	Remove(certPath, keyPath string) error
}
