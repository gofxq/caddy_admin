package application

import (
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type Dependencies struct {
	UpstreamProbe        UpstreamProbe
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
	upstreamProbe       UpstreamProbe
	upstreamMu          sync.Mutex
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
	policyMu            sync.RWMutex
	setupMu             sync.Mutex
	owned               map[string]struct{}
	activePolicy        atomic.Pointer[domain.ManagedSettings]
	setupID             string
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
	service := &Service{
		options: options, repository: repository, caddy: caddy, resolver: resolver,
		upstreamProbe: dependencies.UpstreamProbe,
		setupDNS:      dependencies.SetupDNS, setupIntent: dependencies.SetupIntent,
		certificates: dependencies.Certificates, snapshot: dependencies.Snapshot,
		secrets: dependencies.Secrets, bootstrapTLS: dependencies.BootstrapCertificate,
		localAddresses: localAddresses, owned: map[string]struct{}{},
		setupProbe: dependencies.SetupProbe, resolverSuggestions: dependencies.ResolverSuggestions,
		setupID: domain.ID(),
	}
	service.setActivePolicy(options.RuntimePolicy)
	return service
}

func (service *Service) caddyConfig() domain.CaddyConfig {
	return service.caddyConfigFor(service.activeSettings())
}

func (service *Service) caddyConfigFor(policy domain.ManagedSettings) domain.CaddyConfig {
	options := service.options
	options.RuntimePolicy = policy
	return caddyConfigForOptions(options)
}

func (service *Service) setActivePolicy(policy domain.ManagedSettings) {
	service.activePolicy.Store(&policy)
}
func (service *Service) activeSettings() domain.ManagedSettings { return *service.activePolicy.Load() }

func caddyConfigForOptions(options Options) domain.CaddyConfig {
	policy := options.RuntimePolicy
	return domain.CaddyConfig{
		Socket: options.Socket, AdminURL: options.AdminURL,
		Domains: policy.Domains, ConsoleLANOnly: policy.ConsoleLANOnly,
		PreviousAdminDomain: policy.PreviousAdminDomain,
		AdminDomain:         policy.AdminDomain, LAN: policy.LAN, Resolvers: policy.Resolvers,
		ManagerDial: options.ManagerDial, StaticRoot: options.StaticRoot,
		CaddyStorage: options.CaddyStorage, CertificateMode: options.CertificateMode,
		TemporaryAdminCertPath: filepath.Join(options.DataDir, "secrets", "admin_temporary_tls.crt"),
		TemporaryAdminKeyPath:  filepath.Join(options.DataDir, "secrets", "admin_temporary_tls.key"),
		SetupCertPath:          filepath.Join(options.DataDir, "secrets", "setup_tls.crt"), SetupKeyPath: filepath.Join(options.DataDir, "secrets", "setup_tls.key"),
		TestTLS: options.TestTLS, HTTPPort: options.HTTPPort, HTTPSPort: options.HTTPSPort,
	}
}

func (service *Service) externalCaddy() bool { return service.options.AdminURL != "" }
