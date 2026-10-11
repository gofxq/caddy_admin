package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofxq/caddy_admin/internal/domain"
)

const Version = "0.1.0"

type Config struct {
	MetricsEnabled            bool                   `json:"metrics_enabled"`
	AccessLogsEnabled         bool                   `json:"access_logs_enabled"`
	AlertsEnabled             bool                   `json:"alerts_enabled"`
	UpstreamChecksEnabled     bool                   `json:"upstream_checks_enabled"`
	DataDir                   string                 `json:"-"`
	SnapshotDir               string                 `json:"-"`
	Socket                    string                 `json:"-"`
	AdminURL                  string                 `json:"-"`
	Listen                    string                 `json:"-"`
	Origin                    string                 `json:"origin"`
	Domains                   []domain.ManagedDomain `json:"domains"`
	ConsoleLANOnly            bool                   `json:"console_lan_only"`
	PreviousAdminDomain       string                 `json:"previous_admin_domain,omitempty"`
	PreviousOrigin            string                 `json:"previous_origin,omitempty"`
	AdminDomain               string                 `json:"admin_domain"`
	LAN                       []string               `json:"lan_cidrs"`
	UpstreamCIDRs             []string               `json:"upstream_cidrs"`
	AllowedNames              []string               `json:"allowed_names"`
	ReservedIPs               []string               `json:"-"`
	DeniedIPs                 []string               `json:"denied_ips"`
	Resolvers                 []string               `json:"resolvers"`
	ManagerDial               string                 `json:"-"`
	StaticRoot                string                 `json:"-"`
	CaddyStorage              string                 `json:"-"`
	CaddyBinary               string                 `json:"-"`
	ProbeAddress              string                 `json:"-"`
	CertificateMode           domain.CertificateMode `json:"-"`
	TemporaryAdminCertificate bool                   `json:"-"`
	TestTLS                   bool                   `json:"test_tls"`
	HTTPPort                  string                 `json:"-"`
	HTTPSPort                 string                 `json:"-"`
}

func Default() Config {
	return Config{DataDir: "/var/lib/manager", SnapshotDir: "/srv/snapshots", Socket: "/run/caddy/admin.sock", Listen: "127.0.0.1:8080", AllowedNames: []string{}, DeniedIPs: []string{}, Resolvers: []string{}, ManagerDial: "127.0.0.1:8080", StaticRoot: "/srv/web", CaddyStorage: "/data/caddy", CaddyBinary: "/usr/bin/caddy", ProbeAddress: "127.0.0.1:443", CertificateMode: domain.CertificateModeCloudflare, HTTPPort: "80", HTTPSPort: "443"}
}

func CloudflareTokenPath(dataDir string) string {
	if path := os.Getenv("CLOUDFLARE_API_TOKEN_FILE"); path != "" {
		return path
	}
	if dataDir == "" {
		dataDir = Default().DataDir
	}
	return filepath.Join(dataDir, "secrets", "cloudflare_token")
}

func (config Config) ManagedSettings() domain.ManagedSettings {
	return domain.ManagedSettings{MetricsEnabled: config.MetricsEnabled, AccessLogsEnabled: config.AccessLogsEnabled, AlertsEnabled: config.AlertsEnabled, UpstreamChecksEnabled: config.UpstreamChecksEnabled, Origin: config.Origin, Domains: config.Domains, ConsoleLANOnly: config.ConsoleLANOnly, PreviousAdminDomain: config.PreviousAdminDomain, PreviousOrigin: config.PreviousOrigin, AdminDomain: config.AdminDomain, LAN: config.LAN, UpstreamCIDRs: config.UpstreamCIDRs, AllowedNames: config.AllowedNames, DeniedIPs: config.DeniedIPs, Resolvers: config.Resolvers}
}

func ApplyManagedSettings(config *Config, settings domain.ManagedSettings) error {
	config.MetricsEnabled, config.AccessLogsEnabled, config.AlertsEnabled, config.UpstreamChecksEnabled = settings.MetricsEnabled, settings.AccessLogsEnabled, settings.AlertsEnabled, settings.UpstreamChecksEnabled
	config.Origin, config.Domains, config.AdminDomain = settings.Origin, settings.Domains, settings.AdminDomain
	config.ConsoleLANOnly, config.PreviousAdminDomain, config.PreviousOrigin = settings.ConsoleLANOnly, settings.PreviousAdminDomain, settings.PreviousOrigin
	config.LAN, config.UpstreamCIDRs, config.AllowedNames, config.DeniedIPs, config.Resolvers = settings.LAN, settings.UpstreamCIDRs, settings.AllowedNames, settings.DeniedIPs, settings.Resolvers
	return domain.ValidateManagedSettings(settings)
}

func Load() (Config, error) {
	config := Default()
	config.AdminURL = strings.TrimRight(os.Getenv("CADDY_ADMIN_URL"), "/")
	if config.AdminURL != "" {
		parsed, err := url.Parse(config.AdminURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || !validPort(parsed.Port()) || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
			return config, fmt.Errorf("CADDY_ADMIN_URL must be an HTTP(S) origin with an explicit port and no credentials")
		}
		if os.Getenv("MANAGER_DIAL") == "" {
			return config, fmt.Errorf("external Caddy requires MANAGER_DIAL reachable from that instance")
		}
		config.Listen, config.ProbeAddress = ":8080", net.JoinHostPort(parsed.Hostname(), "443")
	}
	for key, target := range map[string]*string{"CADDY_STORAGE": &config.CaddyStorage, "DATA_DIR": &config.DataDir, "SNAPSHOT_DIR": &config.SnapshotDir, "CADDY_SOCKET": &config.Socket, "LISTEN": &config.Listen, "MANAGER_DIAL": &config.ManagerDial, "STATIC_ROOT": &config.StaticRoot, "CADDY_BINARY": &config.CaddyBinary, "CADDY_PROBE_ADDRESS": &config.ProbeAddress, "HTTP_PORT": &config.HTTPPort, "HTTPS_PORT": &config.HTTPSPort} {
		if value := os.Getenv(key); value != "" {
			*target = value
		}
	}
	config.TestTLS = os.Getenv("TEST_TLS") == "true"
	if err := validateHost(config); err != nil {
		return config, err
	}
	return config, nil
}

func (config Config) ActivePath() string { return filepath.Join(config.SnapshotDir, "active.json") }
func (config Config) TemporaryAdminCertPath() string {
	return filepath.Join(config.DataDir, "secrets", "admin_temporary_tls.crt")
}
func (config Config) TemporaryAdminKeyPath() string {
	return filepath.Join(config.DataDir, "secrets", "admin_temporary_tls.key")
}

func (config Config) SystemEndpoints() []string {
	endpoints := []string{config.ManagerDial, config.ProbeAddress}
	if config.AdminURL != "" {
		if parsed, err := url.Parse(config.AdminURL); err == nil {
			endpoints = append(endpoints, parsed.Host)
		}
	}
	return endpoints
}

func (config Config) TargetPolicy(resolver domain.TargetResolver, reserved []string) domain.TargetPolicy {
	return domain.TargetPolicy{Domains: config.Domains, PreviousAdminDomain: config.PreviousAdminDomain, AdminDomain: config.AdminDomain, UpstreamCIDRs: config.UpstreamCIDRs, AllowedNames: config.AllowedNames, ReservedIPs: append(append([]string{}, config.ReservedIPs...), reserved...), DeniedIPs: config.DeniedIPs, SystemEndpoints: config.SystemEndpoints(), Resolver: resolver}
}

func (config Config) CaddyConfig() domain.CaddyConfig {
	return domain.CaddyConfig{Socket: config.Socket, AdminURL: config.AdminURL, Domains: config.Domains, ConsoleLANOnly: config.ConsoleLANOnly, PreviousAdminDomain: config.PreviousAdminDomain, AdminDomain: config.AdminDomain, LAN: config.LAN, Resolvers: config.Resolvers, ManagerDial: config.ManagerDial, StaticRoot: config.StaticRoot, CaddyStorage: config.CaddyStorage, CertificateMode: config.CertificateMode, TemporaryAdminCertificate: config.TemporaryAdminCertificate, TemporaryAdminCertPath: config.TemporaryAdminCertPath(), TemporaryAdminKeyPath: config.TemporaryAdminKeyPath(), SetupCertPath: filepath.Join(config.DataDir, "secrets", "setup_tls.crt"), SetupKeyPath: filepath.Join(config.DataDir, "secrets", "setup_tls.key"), TestTLS: config.TestTLS, HTTPPort: config.HTTPPort, HTTPSPort: config.HTTPSPort}
}

func validateHost(config Config) error {
	if _, _, err := net.SplitHostPort(config.ManagerDial); err != nil {
		return fmt.Errorf("invalid MANAGER_DIAL")
	}
	for name, address := range map[string]string{"LISTEN": config.Listen, "MANAGER_DIAL": config.ManagerDial, "CADDY_PROBE_ADDRESS": config.ProbeAddress} {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !validPort(port) || (host != "" && net.ParseIP(host) == nil && !domain.ValidDomain(host)) || (name != "LISTEN" && host == "") {
			return fmt.Errorf("invalid %s", name)
		}
	}
	if !validPort(config.HTTPPort) || !validPort(config.HTTPSPort) || config.HTTPPort == config.HTTPSPort {
		return fmt.Errorf("invalid HTTP_PORT or HTTPS_PORT")
	}
	return nil
}

func validPort(value string) bool {
	port, err := strconv.Atoi(value)
	return err == nil && port >= 1 && port <= 65535
}
