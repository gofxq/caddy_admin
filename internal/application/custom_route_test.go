package application_test

import (
	"context"
	"encoding/json"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
	"net"
	"strings"
	"testing"
)

type routeCaddy struct{ *policyCaddy }

func (c *routeCaddy) AdaptHTTPRoute(_ context.Context, source string) (json.RawMessage, error) {
	body, _ := json.Marshal([]any{map[string]any{"handle": []any{map[string]any{"handler": "static_response", "body": source}}}})
	return body, nil
}
func TestCustomRouteSaveImportPublishAndRollback(t *testing.T) {
	_, store, base, snapshot, settings := policyFixture(t)
	c := &routeCaddy{base}
	s := application.New(application.Options{RuntimePolicy: settings, CertificateMode: domain.CertificateModeCloudflare, ManagerDial: "127.0.0.1:8080", ProbeAddress: "127.0.0.1:443", HTTPPort: "80", HTTPSPort: "443"}, store, c, application.Dependencies{Snapshot: snapshot, Certificates: policyCertificates{}, LocalAddresses: func() ([]net.Addr, error) { return nil, nil }})
	ctx := context.Background()
	settings.Domains[0].Access = domain.DomainAccess("internet")
	draft, err := s.SaveSettings(ctx, 0, settings, true, "admin", "203.0.113.1")
	if err != nil {
		t.Fatal(err)
	}
	item := domain.Service{Name: "Custom", DomainID: "home", Hostname: "custom.example.com", Enabled: true, Caddyfile: "respond first"}
	draft, err = s.SaveService(ctx, draft.Revision, item, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	exported, err := s.ExportConfiguration(ctx)
	if err != nil || exported.Services[0].Caddyfile != item.Caddyfile {
		t.Fatal("source export failed", err)
	}
	draft, err = s.ImportConfiguration(ctx, exported, draft.Revision, true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	publishPolicy(t, s, draft.Revision, "custom-first-release-0001", "203.0.113.1", true)
	releases, err := s.Deployments(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	first := releases[0].ID
	item = draft.Services[0]
	item.Caddyfile = "respond second"
	draft, err = s.SaveService(ctx, draft.Revision, item, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	publishPolicy(t, s, draft.Revision, "custom-second-release-0002", "203.0.113.1", false)
	p, err := s.Validate(ctx, draft.Revision, first, "admin")
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Begin(ctx, domain.PublishRequest{Revision: p.Revision, ValidationID: p.ValidationID, ExpectedHash: p.RuntimeHash, Idempotency: "custom-rollback-release-0003", ClientAddress: "203.0.113.1", ConfirmExposure: true}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	s.Apply(d.ID)
	if !strings.Contains(string(snapshot.raw), "respond first") || strings.Contains(string(snapshot.raw), "respond second") {
		t.Fatal("rollback did not restore source route")
	}
	retained, err := store.Draft(ctx)
	if err != nil || retained.Services[0].Caddyfile != "respond second" {
		t.Fatal("rollback altered draft")
	}
}
