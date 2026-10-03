package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	caddyadapter "github.com/gofxq/caddy_admin/internal/adapter/caddy"
	"github.com/gofxq/caddy_admin/internal/adapter/gormstore"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/config"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestContainerCommandsSelectExternalOrEmbeddedCaddy(t *testing.T) {
	c := config.Default()
	t.Setenv("CLOUDFLARE_API_TOKEN", "activation-fixture-token")
	commands := containerCommands(c, "/usr/bin/manager", true)
	if len(commands) != 2 || commands[0].Path != c.CaddyBinary || commands[1].Args[1] != "serve" {
		t.Fatal("embedded mode must start Caddy and Manager")
	}
	if commands[0].Env != nil {
		t.Fatal("supervised Caddy must inherit the manager run environment, including the persisted runtime secret")
	}
	if !strings.Contains(strings.Join(commands[1].Env, "\n"), "CLOUDFLARE_API_TOKEN=activation-fixture-token") {
		t.Fatal("supervised Manager did not inherit the persisted runtime Cloudflare secret")
	}
	c.AdminURL = "http://10.77.0.6:2019"
	commands = containerCommands(c, "/usr/bin/manager", true)
	if len(commands) != 1 || commands[0].Path != "/usr/bin/manager" || commands[0].Args[1] != "serve" {
		t.Fatal("external mode started a local Caddy")
	}
}

func TestCloudflareTokenRequiredOnlyForActiveEmbeddedIssuer(t *testing.T) {
	c := config.Default()
	if needsCloudflareToken(c, domain.CertificateModeBootstrapInternal) {
		t.Fatal("bootstrap mode incorrectly requires Cloudflare token")
	}
	if !needsCloudflareToken(c, domain.CertificateModeCloudflare) {
		t.Fatal("active embedded Cloudflare issuer must require its secret")
	}
	c.AdminURL = "https://external.example.com:2019"
	if needsCloudflareToken(c, domain.CertificateModeCloudflare) {
		t.Fatal("external Caddy must not require a local token")
	}
}

func TestCertificateActivationRecoveryFollowsBootSnapshot(t *testing.T) {
	for _, test := range []struct {
		name           string
		snapshotMode   domain.CertificateMode
		wantMode       domain.CertificateMode
		wantActivation string
	}{
		{name: "committed candidate", snapshotMode: domain.CertificateModeCloudflare, wantMode: domain.CertificateModeCloudflare, wantActivation: "success"},
		{name: "old bootstrap snapshot", snapshotMode: domain.CertificateModeBootstrapInternal, wantMode: domain.CertificateModeBootstrapInternal, wantActivation: "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := config.Default()
			c.DataDir = t.TempDir()
			c.SnapshotDir = t.TempDir()
			c.Origin = "https://admin.home.example.com"
			c.Domains = []domain.ManagedDomain{{ID: "home", Name: "home.example.com", Access: domain.DomainAccess("trusted")}}
			c.AdminDomain = "admin.home.example.com"
			c.LAN = []string{"10.0.0.0/8"}
			c.UpstreamCIDRs = []string{"10.0.0.0/8"}
			c.Resolvers = []string{"9.9.9.9"}
			store, err := gormstore.Open(filepath.Join(c.DataDir, "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			c.CertificateMode = domain.CertificateModeBootstrapInternal
			internal, err := domain.Generate(c.CaddyConfig(), nil)
			if err != nil {
				t.Fatal(err)
			}
			c.CertificateMode = domain.CertificateModeCloudflare
			cloudflare, err := domain.Generate(c.CaddyConfig(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.SetCertificateStatus(context.Background(), domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, ActivationStatus: "uncertain", PublicStatus: "pending", BeforeHash: domain.Fingerprint(internal), CandidateHash: domain.Fingerprint(cloudflare)}); err != nil {
				t.Fatal(err)
			}
			snapshot := internal
			if test.snapshotMode == domain.CertificateModeCloudflare {
				snapshot = cloudflare
			}
			if err = caddyadapter.AtomicWrite(c.ActivePath(), snapshot); err != nil {
				t.Fatal(err)
			}
			if err = reconcileCertificateActivation(store, c); err != nil {
				t.Fatal(err)
			}
			got, err := store.CertificateStatus(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got.Mode != test.wantMode || got.ActivationStatus != test.wantActivation {
				t.Fatalf("recovered state %#v", got)
			}
		})
	}
}

func TestEmbeddedStartupRejectsExternalSnapshot(t *testing.T) {
	c := config.Default()
	c.Origin = "https://admin.home.example.com"
	c.Domains = []domain.ManagedDomain{{ID: "home", Name: "home.example.com", Access: domain.DomainAccess("trusted")}}
	c.AdminDomain = "admin.home.example.com"
	c.LAN = []string{"10.0.0.0/8"}
	c.UpstreamCIDRs = []string{"10.0.0.0/8"}
	c.Resolvers = []string{"9.9.9.9"}
	c.DataDir = t.TempDir()
	c.SnapshotDir = t.TempDir()
	c.TestTLS = true
	c.CaddyBinary = "/must-not-start-caddy"
	c.AdminURL = "http://external:2019"
	raw, err := domain.Generate(c.CaddyConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(c.ActivePath(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	c.AdminURL = ""
	store, err := gormstore.Open(c.DataDir + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CompleteSetup(context.Background(), application.SetupCredentials{Username: "admin", Password: "test-password-long"}, c.ManagedSettings(), domain.CertificateStatus{Mode: domain.CertificateModeBootstrapInternal, ActivationStatus: "idle", PublicStatus: "unknown"}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	err = runContainer(c)
	if err == nil || !strings.Contains(err.Error(), "incompatible embedded snapshot") {
		t.Fatal("external snapshot was not rejected before process startup", err)
	}
}

func TestEmbeddedStartupNeverRecreatesMissingSnapshot(t *testing.T) {
	c := config.Default()
	c.Domains = []domain.ManagedDomain{{ID: "home", Name: "home.example.test", Access: domain.DomainAccess("trusted")}}
	c.AdminDomain = "caddyadmin.home.example.test"
	c.Origin = "https://" + c.AdminDomain
	c.LAN = []string{"10.0.0.0/8"}
	c.UpstreamCIDRs = []string{"10.0.0.0/8"}
	c.Resolvers = []string{"10.0.0.53"}
	c.DataDir = t.TempDir()
	c.SnapshotDir = t.TempDir()
	c.CaddyBinary = "/must-not-start-caddy"
	store, err := gormstore.Open(filepath.Join(c.DataDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CompleteSetup(context.Background(), application.SetupCredentials{Username: "admin", Password: "test-password-long"}, c.ManagedSettings(), domain.CertificateStatus{Mode: domain.CertificateModeBootstrapInternal, ActivationStatus: "idle", PublicStatus: "unknown"}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if err = runContainer(c); err == nil || !strings.Contains(err.Error(), "read embedded snapshot") {
		t.Fatalf("missing snapshot startup: %v", err)
	}
	if _, err = os.Stat(c.ActivePath()); !os.IsNotExist(err) {
		t.Fatalf("snapshot was recreated: %v", err)
	}
}

func TestCertificateActivationWithoutFingerprintsRemainsUncertain(t *testing.T) {
	c := config.Default()
	c.DataDir = t.TempDir()
	c.SnapshotDir = t.TempDir()
	c.Domains = []domain.ManagedDomain{{ID: "home", Name: "home.example.test", Access: domain.DomainAccess("trusted")}}
	c.AdminDomain = "caddyadmin.home.example.test"
	c.Origin = "https://" + c.AdminDomain
	c.LAN = []string{"10.0.0.0/8"}
	c.Resolvers = []string{"10.0.0.53"}
	c.CertificateMode = domain.CertificateModeCloudflare
	raw, err := domain.Generate(c.CaddyConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = caddyadapter.AtomicWrite(c.ActivePath(), raw); err != nil {
		t.Fatal(err)
	}
	store, err := gormstore.Open(filepath.Join(c.DataDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.SetCertificateStatus(context.Background(), domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, ActivationStatus: "uncertain", PublicStatus: "pending"}); err != nil {
		t.Fatal(err)
	}
	if err = reconcileCertificateActivation(store, c); err != nil {
		t.Fatal(err)
	}
	got, err := store.CertificateStatus(context.Background())
	if err != nil || got.ActivationStatus != "uncertain" {
		t.Fatalf("unbound snapshot declared activation successful: %#v %v", got, err)
	}
	retained, err := os.ReadFile(c.ActivePath())
	if err != nil || string(retained) != string(raw) {
		t.Fatalf("reconciliation rewrote snapshot: %v", err)
	}
}
