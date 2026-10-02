package caddy

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestSetupProbeUsesFixedAddressHostAndTrustedTLS(t *testing.T) {
	var host, path string
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		host, path = request.Host, request.URL.Path
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	probe := &SetupProbe{RootCAs: roots}
	address := strings.TrimPrefix(server.URL, "https://")
	if !probe.ExternalConsole(context.Background(), "https://example.com", address) {
		t.Fatal("trusted fixed-address console probe failed")
	}
	if host != "example.com" || path != "/api/v1/auth/session" {
		t.Fatalf("request host/path = %q %q", host, path)
	}
}

func TestSetupProbeRejectsRedirectsAndUntrustedTLS(t *testing.T) {
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", "https://elsewhere.example.test")
		writer.WriteHeader(http.StatusFound)
	}))
	defer redirect.Close()
	roots := x509.NewCertPool()
	roots.AddCert(redirect.Certificate())
	address := strings.TrimPrefix(redirect.URL, "https://")
	if (&SetupProbe{RootCAs: roots}).ExternalConsole(context.Background(), "https://example.com", address) {
		t.Fatal("redirect was accepted")
	}
	if (&SetupProbe{}).ExternalConsole(context.Background(), "https://example.com", address) {
		t.Fatal("untrusted certificate was accepted")
	}
}

func TestResolverSuggestionsFiltersAndLimitsNameservers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(path, []byte("nameserver 10.0.0.53\nnameserver invalid\nnameserver 2001:db8::53\nnameserver 1.1.1.1\nnameserver 9.9.9.9\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := ResolverSuggestions(path)
	want := []string{"10.0.0.53", "2001:db8::53", "1.1.1.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("suggestions=%v want=%v", got, want)
	}
}

func TestResolverEndpointPreservesExplicitPort(t *testing.T) {
	for input, want := range map[string]string{"10.0.0.53": "10.0.0.53:53", "10.0.0.53:5353": "10.0.0.53:5353", "2001:db8::53": "[2001:db8::53]:53", "[2001:db8::53]:5353": "[2001:db8::53]:5353"} {
		if got := resolverEndpoint(input); got != want {
			t.Errorf("resolverEndpoint(%q)=%q want %q", input, got, want)
		}
	}
}

func TestResolverLookupChecksHealthySecondaryWhilePrimaryTimesOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	want := netip.MustParseAddr("10.0.0.10")
	found, responded := lookupResolvers(ctx, "console.example.test", []string{"10.0.0.53", "10.0.0.54:5353"}, func(ctx context.Context, _, address string) ([]netip.Addr, error) {
		if address == "10.0.0.53" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return []netip.Addr{want}, nil
	})
	if !responded || len(found) != 1 || found[0] != want {
		t.Fatalf("healthy secondary result = %v, %v", found, responded)
	}
}

func TestPreserveExternalSettings(t *testing.T) {
	candidate := []byte(`{"admin":{"listen":"local"},"storage":{"root":"local"},"apps":{"http":{}}}`)
	running := []byte(`{"admin":{"listen":":2019","credential":"private"},"storage":{"module":"custom","root":"remote"},"logging":{}}`)
	got, err := PreserveExternalSettings(candidate, running)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]json.RawMessage
	if err = json.Unmarshal(got, &config); err != nil {
		t.Fatal(err)
	}
	if domain.Fingerprint(config["admin"]) != domain.Fingerprint(json.RawMessage(`{"listen":":2019","credential":"private"}`)) || domain.Fingerprint(config["storage"]) != domain.Fingerprint(json.RawMessage(`{"module":"custom","root":"remote"}`)) {
		t.Fatalf("external infrastructure was not preserved: %s", got)
	}
	if _, exists := config["logging"]; exists {
		t.Fatal("unmanaged running configuration leaked into candidate")
	}
}

func TestSnapshotUsesPrivateAtomicFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshots", "active.json")
	snapshot := &Snapshot{Path: path}
	if exists, err := snapshot.Exists(); err != nil || exists {
		t.Fatalf("Exists() = %v, %v", exists, err)
	}
	if err := snapshot.Write([]byte(`{"version":1}`)); err != nil {
		t.Fatal(err)
	}
	if raw, err := snapshot.Read(); err != nil || string(raw) != `{"version":1}` {
		t.Fatalf("Read() = %q, %v", raw, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("snapshot mode = %v, %v", info.Mode().Perm(), err)
	}
	if err = snapshot.Write([]byte(`{"version":2}`)); err != nil {
		t.Fatal(err)
	}
	if raw, _ := snapshot.Read(); string(raw) != `{"version":2}` {
		t.Fatalf("replacement = %q", raw)
	}
	if err = snapshot.Remove(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureBootstrapCertificateCoversOriginAndReusesValidPair(t *testing.T) {
	for _, origin := range []string{"https://127.0.0.1:8080", "https://[::1]:8080", "https://setup.home.example.test:8080"} {
		t.Run(origin, func(t *testing.T) {
			dir := t.TempDir()
			certPath, keyPath := filepath.Join(dir, "setup.crt"), filepath.Join(dir, "setup.key")
			if err := EnsureBootstrapCertificate(certPath, keyPath, origin); err != nil {
				t.Fatal(err)
			}
			pair, err := tls.LoadX509KeyPair(certPath, keyPath)
			if err != nil {
				t.Fatal(err)
			}
			leaf, err := x509.ParseCertificate(pair.Certificate[0])
			if err != nil {
				t.Fatal(err)
			}
			parsed, _ := url.Parse(origin)
			if err = leaf.VerifyHostname(parsed.Hostname()); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(certPath)
			if err = EnsureBootstrapCertificate(certPath, keyPath, origin); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(certPath)
			if sha256.Sum256(before) != sha256.Sum256(after) {
				t.Fatal("valid certificate was unexpectedly rotated")
			}
			for _, path := range []string{certPath, keyPath} {
				info, statErr := os.Stat(path)
				if statErr != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("%s mode = %v, %v", path, info.Mode().Perm(), statErr)
				}
			}
		})
	}
}

func TestEnsureBootstrapCertificateRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "setup.crt"), filepath.Join(dir, "setup.key")
	if err := EnsureBootstrapCertificate(certPath, keyPath, "https://setup.home.example.test:8080"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, keyPath); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBootstrapCertificate(certPath, keyPath, "https://setup.home.example.test:8080"); err == nil {
		t.Fatal("symlink was accepted")
	}
	if raw, _ := os.ReadFile(target); string(raw) != "keep" {
		t.Fatalf("symlink target changed: %q", raw)
	}
}

func TestProbeBootstrapModeNeverReportsPublicTrust(t *testing.T) {
	query := application.CertificateQuery{Domains: []string{"example.test", "", "home.example.test"}, ProbeAddress: "127.0.0.1:1", Mode: domain.CertificateModeBootstrapInternal}
	certificates := (&Probe{}).Certificates(context.Background(), query)
	if len(certificates) != 2 {
		t.Fatalf("certificates = %#v", certificates)
	}
	for _, certificate := range certificates {
		if certificate.Status != "unknown" || certificate.Message != "当前使用内部引导证书，尚未启用公网可信证书" {
			t.Fatalf("certificate = %#v", certificate)
		}
	}
	if (&Probe{}).PublicReady(context.Background(), query) {
		t.Fatal("bootstrap certificates reported publicly ready")
	}
}

func TestSecretFileValidatesAndPersistsPrivateToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "cloudflare_token")
	secret := &SecretFile{Path: path}
	if err := secret.WriteCloudflareToken("not-a-token"); err == nil {
		t.Fatal("invalid token accepted")
	}
	token := "cfat_abcdefghijklmnopqrstuvwxyz123456"
	if err := secret.WriteCloudflareToken(token); err != nil {
		t.Fatal(err)
	}
	if !secret.CloudflareTokenConfigured() {
		t.Fatal("valid token not detected")
	}
	raw, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if !reflect.DeepEqual(raw, []byte(token+"\n")) || info.Mode().Perm() != 0600 {
		t.Fatalf("secret = %q mode=%v", raw, info.Mode().Perm())
	}
}

func TestSetupIntentPersistsOnlyNonSecretMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "setup_dns_intent.json")
	store := &SetupIntentFile{Path: path}
	intent := application.SetupIntent{Plan: application.SetupDNSPlan{Name: "*.h.example.com", Address: "192.168.1.6", Fingerprint: "before"}, ConfigurationHash: "settings", SnapshotHash: "snapshot"}
	if err := store.Write(intent); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("intent permissions", err)
	}
	reopened := &SetupIntentFile{Path: path}
	after, err := reopened.Read()
	if err != nil || after != intent {
		t.Fatal("intent did not survive restart", after, err)
	}
	if err = reopened.Remove(); err != nil {
		t.Fatal(err)
	}
	after, err = reopened.Read()
	if err != nil || after.Plan.Fingerprint != "" {
		t.Fatal(after, err)
	}
}
