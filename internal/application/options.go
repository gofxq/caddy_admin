package application

import "github.com/gofxq/caddy_admin/internal/domain"

type Options struct {
	SetupPassword   string
	SetupToken      string
	DataDir         string
	SnapshotDir     string
	AdminURL        string
	ManagerDial     string
	ProbeAddress    string
	CaddyStorage    string
	StaticRoot      string
	Socket          string
	CaddyBinary     string
	HTTPPort        string
	HTTPSPort       string
	TestTLS         bool
	CertificateMode domain.CertificateMode
	RuntimePolicy   domain.ManagedSettings
}
