package config_test

import (
	"reflect"
	"testing"

	"github.com/gofxq/caddy_admin/internal/config"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestConfigMapsEveryManagedField(t *testing.T) {
	settings := domain.ManagedSettings{
		Origin: "https://caddyadmin.home.example", Domains: []domain.ManagedDomain{{ID: "home", Name: "home.example", Access: domain.DomainAccess("trusted")}}, AdminDomain: "caddyadmin.home.example",
		LAN: []string{"10.0.0.0/8"}, UpstreamCIDRs: []string{"10.0.0.0/8"},
		AllowedNames: []string{"photos.internal"}, DeniedIPs: []string{"10.0.0.1"}, Resolvers: []string{"1.1.1.1"},
	}
	value := config.Default()
	if err := config.ApplyManagedSettings(&value, settings); err != nil {
		t.Fatal(err)
	}
	if got := value.ManagedSettings(); !reflect.DeepEqual(got, settings) {
		t.Fatalf("managed settings mapping lost fields:\n got %#v\nwant %#v", got, settings)
	}
	generated := value.CaddyConfig()
	if generated.AdminDomain != settings.AdminDomain || !reflect.DeepEqual(generated.Domains, settings.Domains) || !reflect.DeepEqual(generated.LAN, settings.LAN) || !reflect.DeepEqual(generated.Resolvers, settings.Resolvers) {
		t.Fatalf("Caddy mapping lost managed settings: %#v", generated)
	}
}

func TestLoadUsesOnlyCurrentDeploymentEnvironment(t *testing.T) {
	for _, key := range []string{"CADDY_ADMIN_URL", "MANAGER_DIAL", "LISTEN", "CADDY_PROBE_ADDRESS"} {
		t.Setenv(key, "")
	}
	for _, key := range []string{"ADMIN_ORIGIN", "ADMIN_DOMAIN", "PUBLIC_DOMAIN", "HOMELAB_DOMAIN", "LAN_CIDRS", "UPSTREAM_CIDRS", "ALLOWED_UPSTREAM_NAMES", "DENIED_UPSTREAM_IPS", "DNS_RESOLVERS"} {
		t.Setenv(key, "obsolete-invalid-value")
	}
	value, err := config.Load()
	if err != nil {
		t.Fatalf("removed business environment still affects startup: %v", err)
	}
	if value.Origin != "" || value.AdminDomain != "" || len(value.Domains) != 0 || len(value.LAN) != 0 || len(value.UpstreamCIDRs) != 0 || len(value.AllowedNames) != 0 || len(value.DeniedIPs) != 0 || len(value.Resolvers) != 0 {
		t.Fatalf("removed environment supplied managed settings: %#v", value.ManagedSettings())
	}
	defaults := config.Default()
	if defaults.Listen != "127.0.0.1:8080" || defaults.ManagerDial != "127.0.0.1:8080" || defaults.ProbeAddress != "127.0.0.1:443" {
		t.Fatalf("defaults retain historical aliases: %+v", defaults)
	}
}
