package config_test

import (
	"github.com/gofxq/caddy_admin/internal/config"
	"github.com/gofxq/caddy_admin/internal/domain"
	"path/filepath"
	"testing"
)

func TestDNSProviderEnvironmentIsFirstUseOnly(t *testing.T) {
	for _, value := range []string{"", "dnspod", "unknown"} {
		t.Setenv("DNS_PROVIDER", value)
		c, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		want := domain.DNSProvider(value)
		if want == "" {
			want = domain.DNSProviderCloudflare
		}
		if c.DNSProvider != want {
			t.Fatalf("provider=%q want %q", c.DNSProvider, want)
		}
	}
}

func TestDNSTokenPathsAreProviderSpecific(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DNSPOD_TOKEN_FILE", "")
	t.Setenv("CLOUDFLARE_API_TOKEN_FILE", "")
	if got := config.DNSTokenPath(dir, domain.DNSProviderDNSPod); got != filepath.Join(dir, "secrets", "dnspod_token") {
		t.Fatal(got)
	}
	t.Setenv("DNSPOD_TOKEN_FILE", filepath.Join(dir, "custom"))
	if got := config.DNSTokenPath(dir, domain.DNSProviderDNSPod); got != filepath.Join(dir, "custom") {
		t.Fatal(got)
	}
	if config.DNSTokenEnvironment(domain.DNSProviderDNSPod) != "DNSPOD_TOKEN" {
		t.Fatal("wrong DNSPod variable")
	}
	if config.DNSTokenEnvironment(domain.DNSProviderCloudflare) != "CLOUDFLARE_API_TOKEN" {
		t.Fatal("wrong Cloudflare variable")
	}
}
