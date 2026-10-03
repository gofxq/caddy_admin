package domain_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestGeneratedCaddyJSONContract(t *testing.T) {
	config := domain.CaddyConfig{
		AdminDomain: "caddyadmin.home.example", Domains: []domain.ManagedDomain{{ID: "home", Name: "home.example", Access: domain.DomainAccess("trusted")}},
		LAN: []string{"10.0.0.0/8"}, ManagerDial: "manager:8080", StaticRoot: "/srv/web",
		Socket: "/run/caddy/admin.sock", CaddyStorage: "/data/caddy", Resolvers: []string{"1.1.1.1"},
		CertificateMode: domain.CertificateModeCloudflare, HTTPPort: "80", HTTPSPort: "443",
	}
	raw, err := domain.Generate(config, []domain.Service{{ID: "one", Name: "Photos", DomainID: "home", Hostname: "photos.home.example", Scheme: "http", Host: "10.77.0.8", Port: 2283, Dial: "10.77.0.8:2283", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"unix//run/caddy/admin.sock"`, `"photos.home.example"`, `"dial": "10.77.0.8:2283"`, `"status_code": 404`, `"{env.CLOUDFLARE_API_TOKEN}"`} {
		if !strings.Contains(string(raw), required) {
			t.Errorf("generated JSON missing %s", required)
		}
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
}

func TestSetupIPEntryOnlyExistsInEmbeddedConfigsAndPreservesCertificateLoaders(t *testing.T) {
	config := domain.CaddyConfig{Socket: "/tmp/admin.sock", AdminDomain: "caddyadmin.home.example", Domains: []domain.ManagedDomain{{ID: "home", Name: "home.example", Access: domain.DomainAccess("trusted")}}, LAN: []string{"10.0.0.0/8"}, ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443", SetupCertPath: "/tmp/setup.crt", SetupKeyPath: "/tmp/setup.key", TemporaryAdminCertPath: "/tmp/admin.crt", TemporaryAdminKeyPath: "/tmp/admin.key", CertificateMode: domain.CertificateModeCloudflare, TemporaryAdminCertificate: true}
	raw, err := domain.Generate(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`127.0.0.1:8083`, domain.SetupCertificateTag, `default_sni`, `setup_redirect`} {
		if !strings.Contains(string(raw), required) {
			t.Fatalf("missing %q", required)
		}
	}
	removed, changed, err := domain.RemoveTemporaryAdminCertificate(raw, config)
	if err != nil || !changed || !strings.Contains(string(removed), domain.SetupCertificateTag) || strings.Contains(string(removed), domain.TemporaryAdminCertificateTag) {
		t.Fatalf("remove admin loader = %v %v %s", changed, err, removed)
	}
	config.AdminURL = "http://external:2019"
	raw, err = domain.Generate(config, nil)
	if err != nil || strings.Contains(string(raw), "127.0.0.1:8083") || strings.Contains(string(raw), domain.SetupCertificateTag) {
		t.Fatalf("external config exposes setup bridge: %v %s", err, raw)
	}
}

func TestDomainHasNoOuterLayerImports(t *testing.T) {
	command := exec.Command("go", "list", "-json", "github.com/gofxq/caddy_admin/internal/domain")
	raw, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"internal/config", "gin-gonic", "gorm.io", "modernc.org", "libtnb/sqlite"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("domain depends on outer layer %q", forbidden)
		}
	}
}
