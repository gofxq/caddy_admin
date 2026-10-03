package application_test

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/adapter/gormstore"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type policyCaddy struct {
	application.CaddyPort
	raw    []byte
	reject bool
}

func (c *policyCaddy) Read(context.Context) ([]byte, error)     { return c.raw, nil }
func (c *policyCaddy) Validate(context.Context, []byte) error   { return nil }
func (c *policyCaddy) Load(_ context.Context, raw []byte) error { c.raw = raw; return nil }

type policySnapshot struct {
	raw  []byte
	fail bool
}

func (s *policySnapshot) Read() ([]byte, error) { return s.raw, nil }
func (s *policySnapshot) Write(raw []byte) error {
	if s.fail {
		return domain.Invalid("snapshot failed")
	}
	s.raw = raw
	return nil
}
func (s *policySnapshot) Exists() (bool, error) { return true, nil }
func (s *policySnapshot) Remove() error         { return nil }

type policyCertificates struct{}

func (policyCertificates) Certificates(context.Context, application.CertificateQuery) []domain.Certificate {
	return nil
}
func (policyCertificates) PublicReady(context.Context, application.CertificateQuery) bool {
	return true
}

func policyFixture(t *testing.T) (*application.Service, *gormstore.Store, *policyCaddy, *policySnapshot, domain.ManagedSettings) {
	t.Helper()
	ctx := context.Background()
	settings := domain.ManagedSettings{Domains: []domain.ManagedDomain{{ID: "home", Name: "example.com"}}, AdminDomain: "caddyadmin.example.com", Origin: "https://caddyadmin.example.com", LAN: []string{}, UpstreamCIDRs: []string{}, AllowedNames: []string{}, DeniedIPs: []string{}, Resolvers: []string{"1.1.1.1"}}
	store, err := gormstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = store.CompleteSetup(ctx, application.SetupCredentials{Username: "admin", Password: "test-password"}, settings, domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, ActivationStatus: "success", PublicStatus: "ready"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := domain.Generate(domain.CaddyConfig{Domains: settings.Domains, AdminDomain: settings.AdminDomain, ManagerDial: "127.0.0.1:8080", LAN: settings.LAN, Resolvers: settings.Resolvers, HTTPPort: "80", HTTPSPort: "443", CertificateMode: domain.CertificateModeCloudflare}, nil)
	caddy := &policyCaddy{raw: raw}
	snapshot := &policySnapshot{raw: raw}
	s := application.New(application.Options{RuntimePolicy: settings, CertificateMode: domain.CertificateModeCloudflare, ManagerDial: "127.0.0.1:8080", ProbeAddress: "127.0.0.1:443", HTTPPort: "80", HTTPSPort: "443"}, store, caddy, application.Dependencies{Snapshot: snapshot, Certificates: policyCertificates{}, LocalAddresses: func() ([]net.Addr, error) { return nil, nil }})
	return s, store, caddy, snapshot, settings
}

func TestSettingsDraftPublishesAtomicallyAndPreservesUnpublishedChanges(t *testing.T) {
	s, store, _, _, settings := policyFixture(t)
	ctx := context.Background()
	draft, _ := store.Draft(ctx)
	settings.ConsoleLANOnly = true
	settings.LAN = []string{"10.0.0.0/8"}
	settings.UpstreamCIDRs = []string{"10.1.0.0/16"}
	draft, err := s.SaveSettings(ctx, draft.Revision, settings, false, "admin", "10.0.0.6")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ControlAccess(ctx, "203.0.113.1"); err != nil {
		t.Fatal("draft activated prematurely", err)
	}
	preview, err := s.Validate(ctx, draft.Revision, "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.SettingsChanged {
		t.Fatal("policy diff missing")
	}
	d, err := s.Begin(ctx, domain.PublishRequest{Revision: preview.Revision, ValidationID: preview.ValidationID, ExpectedHash: preview.RuntimeHash, ClientAddress: "10.0.0.6", Idempotency: "settings-first-publish"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ControlAccess(ctx, "203.0.113.1"); err == nil {
		t.Fatal("pending access policy permitted request")
	}
	s.Apply(d.ID)
	persisted, err := store.ManagedSettings(ctx)
	if err != nil || !persisted.ConsoleLANOnly {
		t.Fatal("successful policy not persisted", err)
	}
	if err = s.ControlAccess(ctx, "203.0.113.1"); err == nil {
		t.Fatal("public source still allowed after activation")
	}
	if err = s.ControlAccess(ctx, "10.0.0.6"); err != nil {
		t.Fatal(err)
	}
	current, _ := store.Draft(ctx)
	old, _ := json.Marshal(draft.Settings)
	now, _ := json.Marshal(current.Settings)
	if string(old) != string(now) || current.Revision != draft.Revision {
		t.Fatal("publication rewrote draft")
	}
}

func TestSnapshotFailureLeavesPolicyUncertainAndRetainsCandidate(t *testing.T) {
	s, store, _, snapshot, settings := policyFixture(t)
	ctx := context.Background()
	draft, _ := store.Draft(ctx)
	settings.Resolvers = []string{"8.8.8.8"}
	draft, err := s.SaveSettings(ctx, draft.Revision, settings, false, "admin", "203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Validate(ctx, draft.Revision, "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Begin(ctx, domain.PublishRequest{Revision: p.Revision, ValidationID: p.ValidationID, ExpectedHash: p.RuntimeHash, Idempotency: "settings-snapshot-fail"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.fail = true
	s.Apply(d.ID)
	recorded, _ := store.Deployment(ctx, d.ID)
	if recorded.Status != "uncertain" {
		t.Fatalf("status=%s", recorded.Status)
	}
	active, _ := store.ManagedSettings(ctx)
	if active.Resolvers[0] != "1.1.1.1" {
		t.Fatal("unconfirmed policy committed")
	}
	if _, err = s.Begin(ctx, domain.PublishRequest{Revision: p.Revision, ValidationID: p.ValidationID, ExpectedHash: p.RuntimeHash, Idempotency: "settings-new-publish-x"}, "admin"); err == nil {
		t.Fatal("uncertain policy allowed repeat publish")
	}
	snapshot.fail = false
	if err = s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	active, _ = store.ManagedSettings(ctx)
	if active.Resolvers[0] != "8.8.8.8" {
		t.Fatal("reconciled policy not persisted")
	}
}

type policySetupProbe struct{ application.SetupProbe }

func (policySetupProbe) ExternalConsole(context.Context, string, string) bool { return true }

func publishPolicy(t *testing.T, s *application.Service, revision int64, key, source string, exposure bool) {
	t.Helper()
	ctx := context.Background()
	p, err := s.Validate(ctx, revision, "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Begin(ctx, domain.PublishRequest{Revision: p.Revision, ValidationID: p.ValidationID, ExpectedHash: p.RuntimeHash, Idempotency: key, ClientAddress: source, ConfirmExposure: exposure}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	s.Apply(d.ID)
	saved, err := s.Deployment(ctx, d.ID)
	if err != nil || saved.Status != "success" {
		t.Fatalf("deployment=%+v err=%v", saved, err)
	}
}

func TestConsoleHandoffIntentSurvivesOrdinarySaveAndRevokesSessions(t *testing.T) {
	_, store, caddy, snapshot, settings := policyFixture(t)
	ctx := context.Background()
	s := application.New(application.Options{RuntimePolicy: settings, CertificateMode: domain.CertificateModeCloudflare, ManagerDial: "127.0.0.1:8080", ProbeAddress: "127.0.0.1:443", HTTPPort: "80", HTTPSPort: "443"}, store, caddy, application.Dependencies{Snapshot: snapshot, Certificates: policyCertificates{}, SetupProbe: policySetupProbe{}, LocalAddresses: func() ([]net.Addr, error) { return nil, nil }})
	draft, _ := store.Draft(ctx)
	settings.AdminDomain = "newadmin.example.com"
	draft, err := s.SaveSettings(ctx, draft.Revision, settings, false, "admin", "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	publishPolicy(t, s, draft.Revision, "console-start-transition", "203.0.113.9", false)
	active, _ := s.RuntimeSettings(ctx)
	if active.PreviousAdminDomain != "caddyadmin.example.com" {
		t.Fatal("old entry lost")
	}
	if _, err = s.CompleteConsoleTransition(ctx, draft.Revision, true, active.PreviousOrigin, "admin"); err == nil {
		t.Fatal("old origin completed transition")
	}
	session, err := store.Login(ctx, "admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	draft, err = s.CompleteConsoleTransition(ctx, draft.Revision, true, active.Origin, "admin")
	if err != nil {
		t.Fatal(err)
	}
	draft.Settings.Resolvers = []string{"8.8.8.8"}
	// Client-provided handoff fields cannot resurrect the old entry.
	draft.Settings.PreviousAdminDomain = "caddyadmin.example.com"
	draft.Settings.PreviousOrigin = "https://caddyadmin.example.com"
	draft, err = s.SaveSettings(ctx, draft.Revision, draft.Settings, false, "admin", "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if draft.Settings.PreviousAdminDomain != "" || draft.Settings.PreviousOrigin != "" {
		t.Fatal("ordinary save revoked confirmed handoff")
	}
	publishPolicy(t, s, draft.Revision, "console-finish-transition", "203.0.113.9", false)
	active, _ = s.RuntimeSettings(ctx)
	if active.PreviousAdminDomain != "" {
		t.Fatal("old entry retained")
	}
	if _, err = store.Session(ctx, session.Token); err == nil {
		t.Fatal("old session survived handoff")
	}
}

func TestPolicyPublicationRejectsCurrentSourceLockoutAndUnconfirmedExposure(t *testing.T) {
	s, store, _, _, settings := policyFixture(t)
	ctx := context.Background()
	draft, _ := store.Draft(ctx)
	settings.ConsoleLANOnly = true
	settings.LAN = []string{"10.0.0.0/8"}
	draft, err := s.SaveSettings(ctx, draft.Revision, settings, false, "admin", "10.0.0.6")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Validate(ctx, draft.Revision, "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Begin(ctx, domain.PublishRequest{Revision: p.Revision, ValidationID: p.ValidationID, ExpectedHash: p.RuntimeHash, ClientAddress: "203.0.113.9", Idempotency: "policy-lockout-rejected"}, "admin"); err == nil {
		t.Fatal("publish permitted current source lockout")
	}
	publishPolicy(t, s, draft.Revision, "policy-lockout-valid-x", "10.0.0.6", false)
	settings.LAN = []string{"0.0.0.0/0"}
	if _, err = s.SaveSettings(ctx, draft.Revision, settings, false, "admin", "10.0.0.6"); err == nil {
		t.Fatal("network widening saved without confirmation")
	}
	draft, err = s.SaveSettings(ctx, draft.Revision, settings, true, "admin", "10.0.0.6")
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Validate(ctx, draft.Revision, "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Begin(ctx, domain.PublishRequest{Revision: p.Revision, ValidationID: p.ValidationID, ExpectedHash: p.RuntimeHash, ClientAddress: "10.0.0.6", Idempotency: "policy-exposure-rejected"}, "admin"); err == nil {
		t.Fatal("network widening published without confirmation")
	}
	publishPolicy(t, s, draft.Revision, "policy-exposure-confirmed", "10.0.0.6", true)
	if err = s.ControlAccess(ctx, "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
}

type selectivePolicyCertificates struct{ ready map[string]bool }

func (*selectivePolicyCertificates) Certificates(context.Context, application.CertificateQuery) []domain.Certificate {
	return nil
}
func (p *selectivePolicyCertificates) PublicReady(_ context.Context, q application.CertificateQuery) bool {
	for _, d := range q.Domains {
		if !p.ready[d] {
			return false
		}
	}
	return len(q.Domains) > 0
}
func TestNewDomainCanIssueCertificateBeforeBusinessPublication(t *testing.T) {
	_, store, caddy, snapshot, settings := policyFixture(t)
	ctx := context.Background()
	certs := &selectivePolicyCertificates{ready: map[string]bool{"example.com": true}}
	s := application.New(application.Options{RuntimePolicy: settings, CertificateMode: domain.CertificateModeCloudflare, ManagerDial: "127.0.0.1:8080", ProbeAddress: "127.0.0.1:443", HTTPPort: "80", HTTPSPort: "443"}, store, caddy, application.Dependencies{Snapshot: snapshot, Certificates: certs, LocalAddresses: func() ([]net.Addr, error) { return nil, nil }})
	draft, _ := store.Draft(ctx)
	settings.Domains = append(settings.Domains, domain.ManagedDomain{ID: "extra", Name: "extra.example.net", Access: domain.DomainAccess("internet")})
	settings.UpstreamCIDRs = []string{"10.0.0.0/8"}
	draft, err := s.SaveSettings(ctx, draft.Revision, settings, true, "admin", "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	publishPolicy(t, s, draft.Revision, "register-new-domain-first", "203.0.113.9", true)
	if !strings.Contains(string(snapshot.raw), "*.extra.example.net") {
		t.Fatal("certificate subject not registered")
	}
	draft, err = s.SaveService(ctx, draft.Revision, domain.Service{Name: "Photos", DomainID: "extra", Hostname: "photos.extra.example.net", Scheme: "http", Host: "10.2.3.4", Port: 8080, Enabled: true}, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Validate(ctx, draft.Revision, "", "admin"); err == nil {
		t.Fatal("business published before its certificate")
	}
	certs.ready["extra.example.net"] = true
	publishPolicy(t, s, draft.Revision, "publish-new-domain-service", "203.0.113.9", true)
	if !strings.Contains(string(snapshot.raw), "photos.extra.example.net") {
		t.Fatal("ready business service missing")
	}
}
