package application_test

import (
	"context"
	"net"
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/adapter/gormstore"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type openPolicyResolver struct{ ip string }

func (r *openPolicyResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr(r.ip)}, nil
}

func TestOpenDefaultsAllowFirstNamedServiceButStillRequireExposureConfirmation(t *testing.T) {
	ctx := context.Background()
	settings, err := application.ManagedSettingsForSetup(application.SetupSettings{Domain: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := gormstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = store.CompleteSetup(ctx, application.SetupCredentials{Username: "admin", Password: "private-password"}, settings, domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, ActivationStatus: "success", PublicStatus: "ready"}); err != nil {
		t.Fatal(err)
	}
	raw, err := domain.Generate(domain.CaddyConfig{Domains: settings.Domains, AdminDomain: settings.AdminDomain, LAN: settings.LAN, Resolvers: settings.Resolvers, ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443", CertificateMode: domain.CertificateModeCloudflare}, nil)
	if err != nil {
		t.Fatal(err)
	}
	caddy, snapshot := &policyCaddy{raw: raw}, &policySnapshot{raw: raw}
	resolver := &openPolicyResolver{ip: "172.20.0.8"}
	options := application.Options{RuntimePolicy: settings, CertificateMode: domain.CertificateModeCloudflare, ManagerDial: "127.0.0.1:8080", ProbeAddress: "127.0.0.1:443", HTTPPort: "80", HTTPSPort: "443"}
	deps := application.Dependencies{Snapshot: snapshot, Certificates: policyCertificates{}, Resolver: resolver, LocalAddresses: func() ([]net.Addr, error) { return nil, nil }}
	svc := application.New(options, store, caddy, deps)
	draft, err := svc.SaveService(ctx, 0, domain.Service{Name: "Photos", DomainID: settings.Domains[0].ID, Hostname: "photos.example.com", Scheme: "http", Host: "photos", Port: 2283, Enabled: true}, false, "admin")
	if err != nil || len(draft.Services) != 1 || string(caddy.raw) != string(raw) || string(snapshot.raw) != string(raw) {
		t.Fatalf("first service changed runtime or failed: %+v %v", draft, err)
	}
	p, err := svc.Validate(ctx, draft.Revision, "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	request := domain.PublishRequest{Revision: p.Revision, ValidationID: p.ValidationID, ExpectedHash: p.RuntimeHash, ClientAddress: "203.0.113.9", Idempotency: "open-first-service"}
	if _, err = svc.Begin(ctx, request, "admin"); err == nil || !strings.Contains(err.Error(), "确认放开") {
		t.Fatalf("public route did not require confirmation: %v", err)
	}
	request.ConfirmExposure = true
	resolver.ip = "172.20.0.9"
	if _, err = svc.Begin(ctx, request, "admin"); err == nil || !strings.Contains(err.Error(), "解析已变化") {
		t.Fatalf("wildcard bypassed DNS revalidation: %v", err)
	}
	resolver.ip = "172.20.0.8"
	release, err := svc.Begin(ctx, request, "admin")
	if err != nil {
		t.Fatal(err)
	}
	svc.Apply(release.ID)
	result, err := store.Deployment(ctx, release.ID)
	if err != nil || result.Status != "success" || !strings.Contains(string(snapshot.raw), "photos.example.com") {
		t.Fatalf("publication failed: %+v %v", result, err)
	}
	// Rebuilt service must use persisted settings rather than reapply defaults.
	persisted, err := store.ManagedSettings(ctx)
	if err != nil || !reflect.DeepEqual(persisted, settings) {
		t.Fatalf("open policy not persisted: %+v %v", persisted, err)
	}
	options.RuntimePolicy = persisted
	restarted := application.New(options, store, caddy, deps)
	if err = restarted.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	retained, err := restarted.Draft(ctx)
	if err != nil || !reflect.DeepEqual(retained, draft) {
		t.Fatalf("recovery changed draft: %+v %v", retained, err)
	}
}
