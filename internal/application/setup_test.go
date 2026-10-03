package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type setupRepository struct {
	Repository
	completed bool
}

type setupCaddy struct{ CaddyPort }

func (*setupRepository) IsInitialized(context.Context) (bool, error) { return false, nil }
func (repository *setupRepository) CompleteSetup(context.Context, SetupCredentials, domain.ManagedSettings, domain.CertificateStatus) error {
	repository.completed = true
	return nil
}

type setupSnapshot struct{ written bool }

func (*setupSnapshot) Exists() (bool, error) { return false, nil }
func (*setupSnapshot) Read() ([]byte, error) { return nil, errors.New("missing") }
func (snapshot *setupSnapshot) Write([]byte) error {
	snapshot.written = true
	return nil
}
func (*setupSnapshot) Remove() error { return nil }

type setupProbe struct {
	resolver bool
	dns      bool
	console  bool
}

func (probe setupProbe) ResolverReachable(context.Context, []string) bool { return probe.resolver }
func (probe setupProbe) DNSMatches(context.Context, string, string, []string) error {
	if !probe.dns {
		return domain.Invalid("解析地址不符")
	}
	return nil
}
func (probe setupProbe) AdminDNS(context.Context, string, []string) bool { return probe.dns }
func (probe setupProbe) ExternalConsole(context.Context, string, string) bool {
	return probe.console
}

func validSetupSettings() SetupSettings {
	return SetupSettings{Domain: "Home.Example.com.", Resolvers: []string{"10.0.0.53"}}
}

func TestPreflightSetupNormalizesSettingsAndClassifiesChecks(t *testing.T) {
	service := New(Options{}, &setupRepository{}, &setupCaddy{}, Dependencies{SetupProbe: setupProbe{resolver: true}})
	result := service.PreflightSetup(context.Background(), validSetupSettings())
	if result.Normalized.AdminDomain != "caddyadmin.home.example.com" || result.Normalized.AdminOrigin != "https://caddyadmin.home.example.com" {
		t.Fatalf("normalized settings = %#v", result.Normalized)
	}
	if !result.CanComplete || !result.RequiresAcknowledgement {
		t.Fatalf("preflight flags = complete %v acknowledgement %v", result.CanComplete, result.RequiresAcknowledgement)
	}
	want := map[string]string{"settings_valid": "pass", "resolver_reachable": "pass", "admin_dns": "warning"}
	for _, check := range result.Checks {
		if status, ok := want[check.ID]; ok {
			if check.Status != status {
				t.Errorf("%s=%s, want %s", check.ID, check.Status, status)
			}
			delete(want, check.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing checks: %v", want)
	}
}

func TestPreflightSetupUsesDefaultResolverAndAllowsEmptyPolicies(t *testing.T) {
	service := New(Options{}, &setupRepository{}, &setupCaddy{}, Dependencies{SetupProbe: setupProbe{resolver: true, dns: true}})
	input := validSetupSettings()
	input.Resolvers = nil
	result := service.PreflightSetup(context.Background(), input)
	if !result.CanComplete || len(result.Normalized.Resolvers) != 1 || result.Normalized.Resolvers[0] != "1.1.1.1" {
		t.Fatalf("default resolver missing: %#v", result)
	}
	input.Resolvers = []string{"not-an-ip"}
	result = service.PreflightSetup(context.Background(), input)
	if result.CanComplete {
		t.Fatal("invalid resolver accepted")
	}
}

func TestCompleteSetupRequiresWarningAcknowledgement(t *testing.T) {
	repository := &setupRepository{}
	snapshot := &setupSnapshot{}
	service := New(Options{TestTLS: true, ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, repository, &setupCaddy{}, Dependencies{Snapshot: snapshot, SetupProbe: setupProbe{resolver: false, dns: false}})
	request := SetupRequest{Username: "admin", Password: "a-secure-password", Settings: validSetupSettings()}
	if _, err := service.CompleteSetup(context.Background(), request); err == nil || err.(*domain.AppError).Code != "setup_warning_confirmation_required" {
		t.Fatalf("unacknowledged completion error = %v", err)
	}
	if repository.completed || snapshot.written {
		t.Fatal("setup wrote state before warning acknowledgement")
	}
	request.AcknowledgeWarnings = true
	request.WarningFingerprint = service.PreflightSetup(context.Background(), request.Settings).WarningFingerprint
	if _, err := service.CompleteSetup(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if !repository.completed || !snapshot.written {
		t.Fatal("acknowledged setup did not persist")
	}
}

type setupDNSStub struct {
	plan       SetupDNSPlan
	calls      int
	applyError error
}

func (d *setupDNSStub) Preview(context.Context, string, string, string) (SetupDNSPlan, error) {
	return d.plan, nil
}
func (d *setupDNSStub) Apply(context.Context, string, SetupDNSPlan) error {
	d.calls++
	if d.applyError == nil {
		d.plan.Action = "reuse"
	}
	return d.applyError
}

type setupSecret struct{ written bool }

func (s *setupSecret) WriteCloudflareToken(string) error { s.written = true; return nil }
func (s *setupSecret) CloudflareTokenConfigured() bool   { return s.written }

type setupIntentMemory struct{ intent SetupIntent }

func (s *setupIntentMemory) Read() (SetupIntent, error) { return s.intent, nil }
func (s *setupIntentMemory) Write(v SetupIntent) error  { s.intent = v; return nil }
func (s *setupIntentMemory) Remove() error              { s.intent = SetupIntent{}; return nil }

type setupBootstrap struct{ BootstrapCertificate }

func (*setupBootstrap) Ensure(string, string, string) error { return nil }

func TestSetupDNSRequiresConfirmationBeforeRemoteOrLocalWrites(t *testing.T) {
	dns := &setupDNSStub{plan: SetupDNSPlan{Name: "*.home.example.com", Address: "10.0.0.6", Type: "A", Action: "create", Fingerprint: "preview"}}
	secrets := &setupSecret{}
	snapshot := &setupSnapshot{}
	repo := &setupRepository{}
	svc := New(Options{ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, repo, &setupCaddy{}, Dependencies{SetupDNS: dns, SetupIntent: &setupIntentMemory{}, Secrets: secrets, Snapshot: snapshot, BootstrapCertificate: &setupBootstrap{}, SetupProbe: setupProbe{resolver: true, dns: true}})
	req := SetupRequest{Username: "admin", Password: "a-secure-password", Settings: validSetupSettings(), Cloudflare: &SetupCloudflare{Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Address: "10.0.0.6", Fingerprint: "preview"}}
	if _, err := svc.CompleteSetup(context.Background(), req); err == nil || dns.calls != 0 || secrets.written || repo.completed {
		t.Fatal("unconfirmed DNS caused a write", err)
	}
	req.Cloudflare.Confirmed = true
	if _, err := svc.ConfirmSetupDNS(context.Background(), req.Settings, *req.Cloudflare); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteSetup(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if dns.calls != 1 || !secrets.written || !repo.completed || !snapshot.written {
		t.Fatal("missing setup writes")
	}
}
func TestSetupDNSRejectsInvalidCredentialsBeforeRemoteWrites(t *testing.T) {
	dns := &setupDNSStub{}
	svc := New(Options{}, &setupRepository{}, &setupCaddy{}, Dependencies{SetupDNS: dns})
	req := SetupRequest{Username: "admin", Password: "short", Settings: validSetupSettings(), AcknowledgeWarnings: true, Cloudflare: &SetupCloudflare{Confirmed: true}}
	if _, err := svc.CompleteSetup(context.Background(), req); err == nil || dns.calls != 0 {
		t.Fatal("invalid credentials wrote DNS", err)
	}
}
func TestSetupDNSFailurePreservesIntentAndDoesNotComplete(t *testing.T) {
	dns := &setupDNSStub{plan: SetupDNSPlan{Name: "*.home.example.com", Address: "10.0.0.6", Type: "A", Action: "create", Fingerprint: "preview"}, applyError: errors.New("offline")}
	intent := &setupIntentMemory{}
	repo := &setupRepository{}
	svc := New(Options{ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, repo, &setupCaddy{}, Dependencies{SetupDNS: dns, SetupIntent: intent, Secrets: &setupSecret{}, Snapshot: &setupSnapshot{}, BootstrapCertificate: &setupBootstrap{}, SetupProbe: setupProbe{resolver: true, dns: true}})
	req := SetupRequest{Username: "admin", Password: "a-secure-password", Settings: validSetupSettings(), Cloudflare: &SetupCloudflare{Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Address: "10.0.0.6", Fingerprint: "preview", Confirmed: true}}
	if _, err := svc.ConfirmSetupDNS(context.Background(), req.Settings, *req.Cloudflare); err == nil || repo.completed || intent.intent.Plan.Fingerprint == "" {
		t.Fatal("failed write lost recovery intent", err)
	}
	dns.plan.Action = "reuse"
	dns.plan.Fingerprint = "after"
	dns.applyError = nil
	// Rebuilt service models a process restart; retry the original confirmed request.
	svc = New(svc.options, repo, &setupCaddy{}, Dependencies{SetupDNS: dns, SetupIntent: intent, Secrets: &setupSecret{}, Snapshot: &setupSnapshot{}, BootstrapCertificate: &setupBootstrap{}, SetupProbe: setupProbe{resolver: true, dns: true}})
	if _, err := svc.CompleteSetup(context.Background(), req); err != nil || !repo.completed {
		t.Fatal("retry did not reconcile", err)
	}
}

type failingSetupRepository struct{ setupRepository }

func (*failingSetupRepository) CompleteSetup(context.Context, SetupCredentials, domain.ManagedSettings, domain.CertificateStatus) error {
	return errors.New("database unavailable")
}
func TestSetupLocalFailureExplainsDNSChangesAndKeepsRecoveryIntent(t *testing.T) {
	dns := &setupDNSStub{plan: SetupDNSPlan{Name: "*.home.example.com", Address: "10.0.0.6", Type: "A", Action: "create", Fingerprint: "preview"}}
	intent := &setupIntentMemory{}
	svc := New(Options{ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, &failingSetupRepository{}, &setupCaddy{}, Dependencies{SetupDNS: dns, SetupIntent: intent, Secrets: &setupSecret{}, Snapshot: &setupSnapshot{}, BootstrapCertificate: &setupBootstrap{}, SetupProbe: setupProbe{resolver: true, dns: true}})
	req := SetupRequest{Username: "admin", Password: "a-secure-password", Settings: validSetupSettings(), Cloudflare: &SetupCloudflare{Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Address: "10.0.0.6", Fingerprint: "preview", Confirmed: true}}
	if _, err := svc.ConfirmSetupDNS(context.Background(), req.Settings, *req.Cloudflare); err != nil {
		t.Fatal(err)
	}
	_, err := svc.CompleteSetup(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "DNS 已") || intent.intent.Plan.Fingerprint == "" {
		t.Fatal("failure does not describe external effect", err)
	}
}

func TestSetupRecoveryRejectsUnconfirmedOtherDNSChanges(t *testing.T) {
	dns := &setupDNSStub{plan: SetupDNSPlan{Name: "*.home.example.com", Address: "10.0.0.6", Type: "A", Action: "create", Fingerprint: "preview", ContextFingerprint: "original-context"}, applyError: errors.New("offline")}
	intent := &setupIntentMemory{}
	repo := &setupRepository{}
	svc := New(Options{ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, repo, &setupCaddy{}, Dependencies{SetupDNS: dns, SetupIntent: intent, Secrets: &setupSecret{}, Snapshot: &setupSnapshot{}, BootstrapCertificate: &setupBootstrap{}, SetupProbe: setupProbe{resolver: true, dns: true}})
	req := SetupRequest{Username: "admin", Password: "a-secure-password", Settings: validSetupSettings(), Cloudflare: &SetupCloudflare{Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Address: "10.0.0.6", Fingerprint: "preview", Confirmed: true}}
	_, _ = svc.ConfirmSetupDNS(context.Background(), req.Settings, *req.Cloudflare)
	dns.plan.Action = "reuse"
	dns.plan.Fingerprint = "after"
	dns.plan.ContextFingerprint = "new-AAAA"
	dns.applyError = nil
	if _, err := svc.CompleteSetup(context.Background(), req); err == nil || repo.completed {
		t.Fatal("recovery silently approved another DNS change", err)
	}
}

type crashingSetupSnapshot struct {
	raw   []byte
	crash bool
}

func (s *crashingSetupSnapshot) Exists() (bool, error) { return len(s.raw) > 0, nil }
func (s *crashingSetupSnapshot) Read() ([]byte, error) { return s.raw, nil }
func (s *crashingSetupSnapshot) Write(raw []byte) error {
	s.raw = raw
	if s.crash {
		panic("simulated crash after snapshot rename")
	}
	return nil
}
func (s *crashingSetupSnapshot) Remove() error { s.raw = nil; return nil }
func TestSetupRecoversOwnedSnapshotAfterCrashBeforeDatabaseCommit(t *testing.T) {
	dns := &setupDNSStub{plan: SetupDNSPlan{Name: "*.home.example.com", Address: "10.0.0.6", Type: "A", Action: "create", Fingerprint: "preview", ContextFingerprint: "same"}}
	intent := &setupIntentMemory{}
	repo := &setupRepository{}
	snapshot := &crashingSetupSnapshot{crash: true}
	options := Options{ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}
	deps := Dependencies{SetupDNS: dns, SetupIntent: intent, Secrets: &setupSecret{}, Snapshot: snapshot, BootstrapCertificate: &setupBootstrap{}, SetupProbe: setupProbe{resolver: true, dns: true}}
	req := SetupRequest{Username: "admin", Password: "a-secure-password", Settings: validSetupSettings(), Cloudflare: &SetupCloudflare{Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Address: "10.0.0.6", Fingerprint: "preview", Confirmed: true}}
	if _, err := New(options, repo, &setupCaddy{}, deps).ConfirmSetupDNS(context.Background(), req.Settings, *req.Cloudflare); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("missing simulated crash")
			}
		}()
		_, _ = New(options, repo, &setupCaddy{}, deps).CompleteSetup(context.Background(), req)
	}()
	if repo.completed || len(snapshot.raw) == 0 || intent.intent.SnapshotHash == "" {
		t.Fatal("crash fixture did not preserve recovery state")
	}
	snapshot.crash = false
	dns.plan.Action = "reuse"
	dns.plan.Fingerprint = "after"
	req.Cloudflare.Fingerprint = "after"
	if _, err := New(options, repo, &setupCaddy{}, deps).ConfirmSetupDNS(context.Background(), req.Settings, *req.Cloudflare); err != nil {
		t.Fatal(err)
	}
	if _, err := New(options, repo, &setupCaddy{}, deps).CompleteSetup(context.Background(), req); err != nil || !repo.completed {
		t.Fatal("owned snapshot was not reconciled", err)
	}
}

func TestSetupCompletionUsesCloudflareRecordsDespiteFilteredServerDNS(t *testing.T) {
	dns := &setupDNSStub{plan: SetupDNSPlan{Name: "*.home.example.com", Address: "10.0.0.6", Type: "A", Action: "reuse", Fingerprint: "preview"}}
	repo := &setupRepository{}
	snapshot := &setupSnapshot{}
	svc := New(Options{ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, repo, &setupCaddy{}, Dependencies{SetupDNS: dns, SetupIntent: &setupIntentMemory{}, Secrets: &setupSecret{}, Snapshot: snapshot, BootstrapCertificate: &setupBootstrap{}, SetupProbe: setupProbe{resolver: true, dns: false}})
	req := SetupRequest{Username: "admin", Password: "a-secure-password", Settings: validSetupSettings(), AcknowledgeWarnings: true, Cloudflare: &SetupCloudflare{Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Address: "10.0.0.6", Fingerprint: "preview", Confirmed: true}}
	req.WarningFingerprint = svc.PreflightSetup(context.Background(), req.Settings).WarningFingerprint
	if _, err := svc.CompleteSetup(context.Background(), req); err != nil || !repo.completed || !snapshot.written {
		t.Fatal("confirmed records were blocked by filtered server DNS", err)
	}
}

func TestSetupRejectsWarningAcknowledgementWithoutCurrentFingerprint(t *testing.T) {
	repo := &setupRepository{}
	snapshot := &setupSnapshot{}
	svc := New(Options{TestTLS: true, ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, repo, &setupCaddy{}, Dependencies{Snapshot: snapshot, SetupProbe: setupProbe{resolver: true, dns: false}})
	req := SetupRequest{Username: "admin", Password: "a-secure-password", Settings: validSetupSettings(), AcknowledgeWarnings: true}
	if _, err := svc.CompleteSetup(context.Background(), req); err == nil || repo.completed || snapshot.written {
		t.Fatal("unbound warning acknowledgement was accepted", err)
	}
}

func TestSetupWarningConsentIsBoundToSettingsAndCurrentWarnings(t *testing.T) {
	for _, change := range []string{"settings", "warnings"} {
		t.Run(change, func(t *testing.T) {
			repo := &setupRepository{}
			snapshot := &setupSnapshot{}
			svc := New(Options{TestTLS: true, ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, repo, &setupCaddy{}, Dependencies{Snapshot: snapshot, SetupProbe: setupProbe{resolver: true, dns: false}})
			req := SetupRequest{Username: "admin", Password: "a-secure-password", Settings: validSetupSettings(), AcknowledgeWarnings: true}
			req.WarningFingerprint = svc.PreflightSetup(context.Background(), req.Settings).WarningFingerprint
			if change == "settings" {
				req.Settings.Resolvers = []string{"10.1.0.53"}
			} else {
				svc.setupProbe = setupProbe{resolver: false, dns: true}
			}
			if _, err := svc.CompleteSetup(context.Background(), req); err == nil || repo.completed || snapshot.written {
				t.Fatal("stale warning consent was accepted", err)
			}
			req.WarningFingerprint = svc.PreflightSetup(context.Background(), req.Settings).WarningFingerprint
			if _, err := svc.CompleteSetup(context.Background(), req); err != nil || !repo.completed || !snapshot.written {
				t.Fatal("fresh warning consent did not complete setup", err)
			}
		})
	}
}

type handoffRepository struct {
	setupRepository
	err error
}

func (repository *handoffRepository) IsInitialized(context.Context) (bool, error) {
	return false, repository.err
}

func TestHandoffDistinguishesUnreadableStateFromConfirmedIncompleteSetup(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{{"incomplete", nil, "ready"}, {"unreadable", errors.New("database unavailable"), "error"}} {
		t.Run(tc.name, func(t *testing.T) {
			svc := New(Options{}, &handoffRepository{err: tc.err}, &setupCaddy{}, Dependencies{})
			result := svc.SetupHandoff(context.Background())
			if result.Initialized || result.ManagerStatus != tc.want {
				t.Fatalf("handoff status = %#v", result)
			}
		})
	}
}

func TestSetupStateWaitsForInFlightCompletion(t *testing.T) {
	for _, endpoint := range []string{"status", "handoff"} {
		t.Run(endpoint, func(t *testing.T) {
			svc := New(Options{}, &setupRepository{}, &setupCaddy{}, Dependencies{})
			svc.setupMu.Lock()
			done := make(chan struct{})
			go func() {
				if endpoint == "status" {
					_, _ = svc.SetupStatus(context.Background())
				} else {
					_ = svc.SetupHandoff(context.Background())
				}
				close(done)
			}()
			select {
			case <-done:
				svc.setupMu.Unlock()
				t.Fatal("reported incomplete while setup was still in flight")
			case <-time.After(25 * time.Millisecond):
			}
			svc.setupMu.Unlock()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("did not resume after setup finished")
			}
		})
	}
}

func TestDNSConfirmationDoesNotWaitForPropagation(t *testing.T) {
	dns := &setupDNSStub{plan: SetupDNSPlan{Name: "*.home.example.com", Address: "10.0.0.6", Type: "A", Action: "create", Fingerprint: "preview", ContextFingerprint: "context"}}
	intent := &setupIntentMemory{}
	repo := &setupRepository{}
	snapshot := &setupSnapshot{}
	svc := New(Options{}, repo, &setupCaddy{}, Dependencies{SetupDNS: dns, SetupIntent: intent, Secrets: &setupSecret{}, Snapshot: snapshot, SetupProbe: setupProbe{dns: false}})
	cf := SetupCloudflare{Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Address: "10.0.0.6", Fingerprint: "preview", Confirmed: true}
	if _, err := svc.ConfirmSetupDNS(context.Background(), validSetupSettings(), cf); err != nil || intent.intent.Plan.Fingerprint != "preview" || repo.completed || snapshot.written {
		t.Fatal("propagation failure lost intent or initialized", err)
	}
	dns.plan.Fingerprint = "after"
	svc.setupProbe = setupProbe{dns: true}
	if _, err := svc.ConfirmSetupDNS(context.Background(), validSetupSettings(), cf); err != nil || repo.completed || snapshot.written {
		t.Fatal("retry failed or prematurely initialized", err)
	}
}

func TestFinalSetupDoesNotWriteUnappliedDNS(t *testing.T) {
	dns := &setupDNSStub{plan: SetupDNSPlan{Name: "*.home.example.com", Address: "10.0.0.6", Type: "A", Action: "create", Fingerprint: "preview"}}
	repo := &setupRepository{}
	svc := New(Options{}, repo, &setupCaddy{}, Dependencies{SetupDNS: dns, SetupIntent: &setupIntentMemory{}, Secrets: &setupSecret{}, Snapshot: &setupSnapshot{}, BootstrapCertificate: &setupBootstrap{}, SetupProbe: setupProbe{dns: true, resolver: true}})
	req := SetupRequest{Username: "admin", Password: "a-secure-password", Settings: validSetupSettings(), Cloudflare: &SetupCloudflare{Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Address: "10.0.0.6", Fingerprint: "preview", Confirmed: true}}
	if _, err := svc.CompleteSetup(context.Background(), req); err == nil || dns.calls != 0 || repo.completed {
		t.Fatal("final submit wrote unapplied DNS", err)
	}
}

func (probe setupProbe) DNSResults(context.Context, string, string, []string) (SetupDNSReport, error) {
	return SetupDNSReport{Verified: probe.dns, Queries: []SetupDNSQuery{}}, nil
}

type initializedSetupRepository struct{ setupRepository }

func (*initializedSetupRepository) IsInitialized(context.Context) (bool, error) { return true, nil }
func TestCheckSetupDNSValidatesModeSettingsAndInitialization(t *testing.T) {
	for _, tc := range []struct {
		name            string
		options         Options
		address         string
		initialized     bool
		invalidSettings bool
		wantStatus      int
	}{
		{name: "embedded", address: "10.0.0.6"},
		{name: "embedded needs target", wantStatus: 422},
		{name: "test mode can query only", options: Options{TestTLS: true}},
		{name: "external can query only", options: Options{AdminURL: "http://127.0.0.1:2019"}},
		{name: "invalid address", address: "bad", wantStatus: 422},
		{name: "invalid settings", address: "10.0.0.6", invalidSettings: true, wantStatus: 422},
		{name: "initialized", address: "10.0.0.6", initialized: true, wantStatus: 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var repo Repository = &setupRepository{}
			if tc.initialized {
				repo = &initializedSetupRepository{}
			}
			svc := New(tc.options, repo, &setupCaddy{}, Dependencies{SetupProbe: setupProbe{dns: true}})
			settings := validSetupSettings()
			if tc.invalidSettings {
				settings.Domain = ""
			}
			report, err := svc.CheckSetupDNS(context.Background(), settings, tc.address)
			if tc.wantStatus == 0 {
				if err != nil || !report.Verified {
					t.Fatal(report, err)
				}
				return
			}
			var appErr *domain.AppError
			if !errors.As(err, &appErr) || appErr.Status != tc.wantStatus {
				t.Fatal(err)
			}
		})
	}
}
