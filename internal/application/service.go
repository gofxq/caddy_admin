package application

import (
	"net"
	"path/filepath"
	"sync"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type Dependencies struct {
	SetupDNS             SetupDNS
	SetupIntent          SetupIntentStore
	Resolver             Resolver
	Certificates         CertificateProbe
	Snapshot             SnapshotStore
	Secrets              SecretStore
	BootstrapCertificate BootstrapCertificate
	LocalAddresses       func() ([]net.Addr, error)
	SetupProbe           SetupProbe
	ResolverSuggestions  func() []string
}

type Service struct {
	setupDNS            SetupDNS
	setupIntent         SetupIntentStore
	options             Options
	repository          Repository
	caddy               CaddyPort
	resolver            Resolver
	certificates        CertificateProbe
	snapshot            SnapshotStore
	secrets             SecretStore
	bootstrapTLS        BootstrapCertificate
	localAddresses      func() ([]net.Addr, error)
	setupProbe          SetupProbe
	resolverSuggestions func() []string
	mu                  sync.Mutex
	setupMu             sync.Mutex
	owned               map[string]struct{}
}

func New(options Options, repository Repository, caddy CaddyPort, dependencies Dependencies) *Service {
	resolver := dependencies.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	localAddresses := dependencies.LocalAddresses
	if localAddresses == nil {
		localAddresses = net.InterfaceAddrs
	}
	return &Service{
		options: options, repository: repository, caddy: caddy, resolver: resolver,
		setupDNS: dependencies.SetupDNS, setupIntent: dependencies.SetupIntent,
		certificates: dependencies.Certificates, snapshot: dependencies.Snapshot,
		secrets: dependencies.Secrets, bootstrapTLS: dependencies.BootstrapCertificate,
		localAddresses: localAddresses, owned: map[string]struct{}{},
		setupProbe: dependencies.SetupProbe, resolverSuggestions: dependencies.ResolverSuggestions,
	}
}

func (service *Service) caddyConfig() domain.CaddyConfig {
	return caddyConfigForOptions(service.options)
}

func caddyConfigForOptions(options Options) domain.CaddyConfig {
	policy := options.RuntimePolicy
	return domain.CaddyConfig{
		Socket: options.Socket, AdminURL: options.AdminURL,
		PublicDomain: policy.PublicDomain, HomelabDomain: policy.HomelabDomain,
		AdminDomain: policy.AdminDomain, LAN: policy.LAN, Resolvers: policy.Resolvers,
		ManagerDial: options.ManagerDial, StaticRoot: options.StaticRoot,
		CaddyStorage: options.CaddyStorage, CertificateMode: options.CertificateMode,
		TemporaryAdminCertPath: filepath.Join(options.DataDir, "secrets", "admin_temporary_tls.crt"),
		TemporaryAdminKeyPath:  filepath.Join(options.DataDir, "secrets", "admin_temporary_tls.key"),
		SetupCertPath:          filepath.Join(options.DataDir, "secrets", "setup_tls.crt"), SetupKeyPath: filepath.Join(options.DataDir, "secrets", "setup_tls.key"),
		TestTLS: options.TestTLS, HTTPPort: options.HTTPPort, HTTPSPort: options.HTTPSPort,
	}
}

func (service *Service) externalCaddy() bool { return service.options.AdminURL != "" }
