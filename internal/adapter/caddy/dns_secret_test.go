package caddy

import (
	"context"
	"github.com/gofxq/caddy_admin/internal/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDNSPodSecretPersistsPrivatelyWithoutOverwritingCloudflare(t *testing.T) {
	dir := t.TempDir()
	secret := &SecretFile{Path: filepath.Join(dir, "cloudflare_token"), DNSPodPath: filepath.Join(dir, "dnspod_token")}
	cf := strings.Repeat("a", 40)
	if err := secret.WriteCloudflareToken(cf); err != nil {
		t.Fatal(err)
	}
	token := "12345," + strings.Repeat("b", 32)
	if err := secret.WriteDNSToken(domain.DNSProviderDNSPod, token); err != nil {
		t.Fatal(err)
	}
	if got, err := secret.ReadDNSToken(domain.DNSProviderDNSPod); err != nil || got != token {
		t.Fatal("DNSPod secret did not round trip")
	}
	if got, err := secret.ReadCloudflareToken(); err != nil || got != cf {
		t.Fatal("Cloudflare secret was overwritten")
	}
	info, err := os.Stat(secret.DNSPodPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("DNSPod token permissions must be 0600")
	}
	if err := secret.WriteDNSToken(domain.DNSProviderDNSPod, "bad-token"); err == nil {
		t.Fatal("invalid DNSPod token accepted")
	}
	if got, _ := secret.ReadDNSToken(domain.DNSProviderDNSPod); got != token {
		t.Fatal("failed update replaced valid token")
	}
}

func TestDNSPodValidationUsesPersistedCredential(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "secrets", "dnspod_token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("12345,persisted-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DNSPOD_TOKEN_FILE", tokenPath)
	t.Setenv("DNSPOD_TOKEN", "12345,stale-token")
	binary := filepath.Join(dir, "caddy")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n[ \"$DNSPOD_TOKEN\" = '12345,persisted-token' ]\n"), 0700); err != nil {
		t.Fatal(err)
	}
	client := NewClient(Options{CaddyBinary: binary, DataDir: dir})
	if err := client.Validate(context.Background(), []byte(`{"apps":{}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestBuildInfoChecksSelectedDNSModule(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "caddy")
	script := "#!/bin/sh\ncase \"$1\" in version) echo v2.test;; list-modules) echo dns.providers.cloudflare;; esac\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	client := NewClient(Options{CaddyBinary: binary})
	if _, found := client.DNSBuildInfo(context.Background(), domain.DNSProviderDNSPod); found {
		t.Fatal("Cloudflare module incorrectly proves DNSPod support")
	}
	if _, found := client.DNSBuildInfo(context.Background(), domain.DNSProviderCloudflare); !found {
		t.Fatal("missing Cloudflare module")
	}
}
