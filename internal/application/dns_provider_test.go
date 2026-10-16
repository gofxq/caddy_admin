package application

import (
	"context"
	"github.com/gofxq/caddy_admin/internal/domain"
	"testing"
)

type providerSecret struct {
	setupSecret
	provider domain.DNSProvider
	token    string
}

func (s *providerSecret) WriteDNSToken(p domain.DNSProvider, t string) error {
	s.provider = p
	s.token = t
	s.written = true
	return nil
}
func TestProviderConfiguredCredentialsAndDispatch(t *testing.T) {
	cf := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	dp := "123,abcdefghijklmnop0123456789"
	dns := &setupDNSStub{plan: SetupDNSPlan{Action: "reuse", Fingerprint: "checked"}}
	s := New(Options{SetupTokens: map[domain.DNSProvider]string{domain.DNSProviderCloudflare: cf, domain.DNSProviderDNSPod: dp}}, &setupRepository{}, &setupCaddy{}, Dependencies{SetupDNSProviders: map[domain.DNSProvider]SetupDNS{domain.DNSProviderDNSPod: dns}})
	status, err := s.SetupStatus(context.Background())
	if err != nil || status.DNSProvider != domain.DNSProviderCloudflare || status.DNSCredentials[domain.DNSProviderDNSPod].Status != "ready" {
		t.Fatal(status, err)
	}
	token, err := s.ResolveSetupProviderToken(domain.DNSProviderDNSPod, "", true, s.setupID)
	if err != nil || token != dp {
		t.Fatal(token, err)
	}
	if _, err = s.ResolveSetupProviderToken(domain.DNSProviderDNSPod, cf, false, s.setupID); err == nil {
		t.Fatal("credentials crossed providers")
	}
	if _, err = s.ResolveSetupProviderToken(domain.DNSProviderDNSPod, "", true, "old"); err == nil {
		t.Fatal("old setup id accepted")
	}
	plan, err := s.PreviewSetupDNSProvider(context.Background(), domain.DNSProviderDNSPod, dp, "home.example.test", "10.0.0.6")
	if err != nil || plan.Provider != domain.DNSProviderDNSPod {
		t.Fatal(plan, err)
	}
	if _, err = s.PreviewSetupDNSProvider(context.Background(), "other", dp, "home.example.test", "10.0.0.6"); err == nil {
		t.Fatal("unknown provider dispatched")
	}
}
func TestDNSPodSetupPersistsSelectedCertificateMode(t *testing.T) {
	dns := &setupDNSStub{plan: SetupDNSPlan{Name: "*.home.example.com", Address: "10.0.0.6", Type: "A", Action: "reuse", Fingerprint: "checked"}}
	repo := &setupRepository{}
	secret := &providerSecret{}
	snapshot := &setupSnapshot{}
	s := New(Options{ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, repo, &setupCaddy{}, Dependencies{SetupDNSProviders: map[domain.DNSProvider]SetupDNS{domain.DNSProviderDNSPod: dns}, SetupIntent: &setupIntentMemory{}, Secrets: secret, Snapshot: snapshot, BootstrapCertificate: &setupBootstrap{}, SetupProbe: setupProbe{resolver: true, dns: true}})
	_, err := s.CompleteSetup(context.Background(), SetupRequest{Username: "admin", Password: "a-secure-password", Settings: validSetupSettings(), Cloudflare: &SetupCloudflare{Provider: domain.DNSProviderDNSPod, Token: "123,abcdefghijklmnop0123456789", Address: "10.0.0.6", Fingerprint: "checked"}})
	if err != nil || !repo.completed || secret.provider != domain.DNSProviderDNSPod {
		t.Fatal(err, repo.completed, secret.provider)
	}
}
func TestDNSPodUncertainWriteNeverBlindlyRetries(t *testing.T) {
	p := SetupDNSPlan{Provider: domain.DNSProviderDNSPod, Name: "*.example.test", ContextFingerprint: "account", Action: "create"}
	intent := SetupIntent{Plan: p, DispatchedAt: "2020-01-01T00:00:00Z"}
	if checkDNSWriteCooldown(intent, p) == nil {
		t.Fatal("uncertain create retried")
	}
	p.Action = "reuse"
	if err := checkDNSWriteCooldown(intent, p); err != nil {
		t.Fatal(err)
	}
}

func TestDNSPodPendingWriteRejectsUnrelatedReuse(t *testing.T) {
	original := SetupDNSPlan{Provider: domain.DNSProviderDNSPod, Name: "*.a.example.test", Type: "A", Address: "10.0.0.6", ZoneID: "zone", ContextFingerprint: "account", Action: "create"}
	intent := SetupIntent{Plan: original, DispatchedAt: "2020-01-01T00:00:00Z"}
	for _, change := range []func(*SetupDNSPlan){func(p *SetupDNSPlan) { p.Provider = domain.DNSProviderCloudflare }, func(p *SetupDNSPlan) { p.Name = "*.b.example.test" }, func(p *SetupDNSPlan) { p.Type = "AAAA" }, func(p *SetupDNSPlan) { p.Address = "10.0.0.7" }, func(p *SetupDNSPlan) { p.ContextFingerprint = "other" }, func(p *SetupDNSPlan) { p.ZoneID = "other" }} {
		p := original
		p.Action = "reuse"
		change(&p)
		if checkDNSWriteCooldown(intent, p) == nil {
			t.Fatal("unrelated reuse bypassed pending DNS write", p)
		}
	}
	p := original
	p.Action = "reuse"
	if err := checkDNSWriteCooldown(intent, p); err != nil {
		t.Fatal(err)
	}
}
func TestUnknownSetupProviderIsReported(t *testing.T) {
	s := New(Options{DNSProvider: "unknown"}, &setupRepository{}, &setupCaddy{}, Dependencies{})
	status, err := s.SetupStatus(context.Background())
	if err != nil || status.ConfigurationError == "" {
		t.Fatal(status, err)
	}
	if _, err = s.ResolveSetupProviderToken(s.dnsProvider(), "", false, ""); err == nil {
		t.Fatal("unknown ENV provider defaulted")
	}
}

func TestSetupCannotReplacePendingDNSPodIntentWithCloudflareReuse(t *testing.T) {
	original := SetupIntent{Plan: SetupDNSPlan{Provider: domain.DNSProviderDNSPod, Name: "*.original.example.test", Type: "A", Address: "10.0.0.6", ContextFingerprint: "dnspod-account", Fingerprint: "original"}, DispatchedAt: "2020-01-01T00:00:00Z"}
	intent := &setupIntentMemory{intent: original}
	secret := &setupSecret{}
	dns := &setupDNSStub{plan: SetupDNSPlan{Name: "*.home.example.com", Type: "A", Address: "10.0.0.6", Action: "reuse", Fingerprint: "checked"}}
	s := New(Options{}, &setupRepository{}, &setupCaddy{}, Dependencies{SetupDNS: dns, SetupIntent: intent, Secrets: secret})
	_, err := s.ConfirmSetupDNS(context.Background(), validSetupSettings(), SetupCloudflare{Provider: domain.DNSProviderCloudflare, Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Address: "10.0.0.6", Fingerprint: "checked", Confirmed: true})
	if err == nil || intent.intent != original || secret.written || dns.calls != 0 {
		t.Fatal("pending intent replaced", err, intent.intent, secret.written, dns.calls)
	}
}

type rejectedDNSWrite struct{ rejected bool }

func (e rejectedDNSWrite) Error() string          { return "provider rejected" }
func (e rejectedDNSWrite) DNSWriteRejected() bool { return e.rejected }
func TestDNSPodDefiniteRejectionClearsOnlyDispatchMarker(t *testing.T) {
	for _, definite := range []bool{false, true} {
		plan := SetupDNSPlan{Provider: domain.DNSProviderDNSPod, Fingerprint: "plan", ContextFingerprint: "account"}
		original := SetupIntent{Plan: plan, ConfigurationHash: "configuration", SnapshotHash: "snapshot", DispatchedAt: "dispatched"}
		intent := &setupIntentMemory{intent: original}
		s := New(Options{}, &setupRepository{}, &setupCaddy{}, Dependencies{SetupIntent: intent})
		err := s.dnsApplyError(plan, rejectedDNSWrite{definite})
		if err == nil {
			t.Fatal("error lost")
		}
		expected := original
		if definite {
			expected.DispatchedAt = ""
		}
		if intent.intent != expected {
			t.Fatal("wrong intent after rejection", intent.intent, expected)
		}
	}
}
func TestDNSPodUncertainFailureRetainsDispatchMarker(t *testing.T) {
	plan := SetupDNSPlan{Provider: domain.DNSProviderDNSPod, Fingerprint: "plan", ContextFingerprint: "account"}
	original := SetupIntent{Plan: plan, DispatchedAt: "dispatched"}
	intent := &setupIntentMemory{intent: original}
	s := New(Options{}, &setupRepository{}, &setupCaddy{}, Dependencies{SetupIntent: intent})
	_ = s.dnsApplyError(plan, domain.Conflict("response unknown"))
	if intent.intent != original {
		t.Fatal("uncertain intent cleared")
	}
}

func TestSuccessfulDNSPodConfirmationClearsDispatchAndAllowsNewSelection(t *testing.T) {
	dns := &setupDNSStub{plan: SetupDNSPlan{Name: "*.home.example.com", Type: "A", Address: "10.0.0.6", Action: "create", Fingerprint: "checked", ContextFingerprint: "account"}}
	intent := &setupIntentMemory{intent: SetupIntent{ConfigurationHash: "owned-config", SnapshotHash: "owned-snapshot"}}
	secret := &providerSecret{}
	s := New(Options{}, &setupRepository{}, &setupCaddy{}, Dependencies{SetupDNSProviders: map[domain.DNSProvider]SetupDNS{domain.DNSProviderDNSPod: dns}, SetupIntent: intent, Secrets: secret})
	_, err := s.ConfirmSetupDNS(context.Background(), validSetupSettings(), SetupCloudflare{Provider: domain.DNSProviderDNSPod, Token: "123,abcdefghijklmnop0123456789", Address: "10.0.0.6", Fingerprint: "checked", Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if intent.intent.DispatchedAt != "" || intent.intent.Plan.Fingerprint != "checked" {
		t.Fatal("successful dispatch remained uncertain", intent.intent)
	}
	if err = checkDNSWriteCooldown(intent.intent, SetupDNSPlan{Provider: domain.DNSProviderCloudflare, Name: "*.different.example.test", Action: "create"}); err != nil {
		t.Fatal("successful write blocked new selection", err)
	}
}
