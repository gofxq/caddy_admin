package gormstore_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func fixtureConfiguration(t *testing.T, fixture *orchestrationFixture) application.Configuration {
	t.Helper()
	ctx := context.Background()
	settings := domain.ManagedSettings{Origin: "https://caddyadmin.home.example.test", HomelabDomain: "home.example.test", AdminDomain: "caddyadmin.home.example.test", LAN: []string{"10.0.0.0/8"}, UpstreamCIDRs: []string{"10.0.0.0/8"}, Resolvers: []string{"10.0.0.53"}}
	if err := fixture.store.CompleteSetup(ctx, application.SetupCredentials{Username: "admin", Password: "a-secure-password"}, settings, domain.CertificateStatus{Mode: domain.CertificateModeBootstrapInternal, ActivationStatus: "idle", PublicStatus: "unknown"}); err != nil {
		t.Fatal(err)
	}
	config, err := fixture.service.ExportConfiguration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config.Services = []application.PortableService{{Name: "Photos", Group: "homelab", Hostname: "photos.home.example.test", Scheme: "https", Host: "10.0.0.10", Port: 8443, Enabled: true, Notes: "Imported"}}
	return config
}

func TestConfigurationImportRevalidatesAndNeverPublishes(t *testing.T) {
	ctx := context.Background()
	fixture := newOrchestrationFixture(t)
	config := fixtureConfiguration(t, fixture)
	runtime := append([]byte{}, fixture.caddy.raw...)
	snapshot := append([]byte{}, fixture.snapshot.raw...)
	preview, err := fixture.service.PreviewConfiguration(ctx, config)
	if err != nil || preview.Revision != 0 || len(preview.Changes) != 1 {
		t.Fatalf("preview: %#v %v", preview, err)
	}
	if _, err = fixture.service.ImportConfiguration(ctx, config, 0, false, "admin"); err == nil {
		t.Fatal("unconfirmed import accepted")
	}
	draft, err := fixture.service.ImportConfiguration(ctx, config, 0, true, "admin")
	if err != nil || draft.Revision != 1 || len(draft.Services) != 1 {
		t.Fatalf("import: %#v %v", draft, err)
	}
	id := draft.Services[0].ID
	if _, err = fixture.service.ImportConfiguration(ctx, config, 0, true, "admin"); err == nil {
		t.Fatal("stale revision accepted")
	}
	config.Services[0].Host = "127.0.0.1"
	if _, err = fixture.service.ImportConfiguration(ctx, config, 1, true, "admin"); err == nil {
		t.Fatal("unsafe upstream accepted")
	}
	config.Services[0].Host = "10.0.0.3"
	if _, err = fixture.service.ImportConfiguration(ctx, config, 1, true, "admin"); err == nil {
		t.Fatal("protected Caddy accepted")
	}
	config.Services[0].Host = "10.0.0.10"
	config.Services = append(config.Services, config.Services[0])
	if _, err = fixture.service.PreviewConfiguration(ctx, config); err == nil {
		t.Fatal("duplicate hostnames accepted")
	}
	config.Services = config.Services[:1]
	config.Version = 2
	if _, err = fixture.service.PreviewConfiguration(ctx, config); err == nil {
		t.Fatal("future file version accepted")
	}
	config.Version = 1
	preview, err = fixture.service.PreviewConfiguration(ctx, config)
	if err != nil || preview.Services[0].ID != id {
		t.Fatalf("import did not retain hostname identity: %#v %v", preview, err)
	}
	if string(fixture.caddy.raw) != string(runtime) || string(fixture.snapshot.raw) != string(snapshot) {
		t.Fatal("import changed runtime or startup snapshot")
	}
	current, err := fixture.store.Draft(ctx)
	if err != nil || current.Revision != 1 || current.Services[0].Host != "10.0.0.10" {
		t.Fatalf("invalid import changed draft: %#v %v", current, err)
	}
}

func TestSetupImportedServicesAreAtomicAndRemainUnpublished(t *testing.T) {
	fixture := newOrchestrationFixture(t)
	config := fixtureConfiguration(t, fixture)
	fresh := newOrchestrationFixture(t)
	fresh.snapshot.raw = nil
	request := application.SetupRequest{Username: "new-admin", Password: "fresh-admin-password", Settings: application.SetupSettings{HomelabDomain: config.Settings.HomelabDomain, LAN: config.Settings.LAN, UpstreamCIDRs: config.Settings.UpstreamCIDRs, Resolvers: config.Settings.Resolvers}, Services: config.Services, AcknowledgeWarnings: true}
	if _, err := fresh.service.CompleteSetup(context.Background(), request); err == nil {
		t.Fatal("imported setup did not require confirmation")
	}
	if fresh.snapshot.raw != nil {
		t.Fatal("unconfirmed import wrote snapshot")
	}
	request.ConfirmImport = true
	request.Services[0].Host = "127.0.0.1"
	if _, err := fresh.service.CompleteSetup(context.Background(), request); err == nil {
		t.Fatal("unsafe setup import accepted")
	}
	if initialized, err := fresh.store.IsInitialized(context.Background()); err != nil || initialized {
		t.Fatalf("failed setup left administrator: %v %v", initialized, err)
	}
	request.Services[0].Host = "10.0.0.10"
	if _, err := fresh.service.CompleteSetup(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	draft, err := fresh.store.Draft(context.Background())
	if err != nil || draft.Revision != 1 || len(draft.Services) != 1 {
		t.Fatalf("setup draft = %#v %v", draft, err)
	}
	var snapshot map[string]any
	if err = json.Unmarshal(fresh.snapshot.raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(fresh.snapshot.raw), "photos.home.example.test") {
		t.Fatal("imported business route was published at setup")
	}
	if _, err = fresh.store.Login(context.Background(), "new-admin", request.Password); err != nil {
		t.Fatal("new administrator not stored", err)
	}
	if _, err = fresh.service.CompleteSetup(context.Background(), request); err == nil {
		t.Fatal("second setup succeeded")
	}
}

func TestImportRetainsInstancePolicyAndEmptyImportClearsOnlyDraft(t *testing.T) {
	ctx := context.Background()
	fixture := newOrchestrationFixture(t)
	config := fixtureConfiguration(t, fixture)
	original, _ := fixture.store.ManagedSettings(ctx)
	config.Settings.UpstreamCIDRs = []string{"192.168.0.0/16"}
	// Uploaded policy is portable metadata; existing instances use their own policy.
	draft, err := fixture.service.ImportConfiguration(ctx, config, 0, true, "admin")
	if err != nil || len(draft.Services) != 1 {
		t.Fatalf("current policy was not used: %#v %v", draft, err)
	}
	config.Services[0].Host = "192.168.2.6"
	if _, err = fixture.service.ImportConfiguration(ctx, config, 1, true, "admin"); err == nil {
		t.Fatal("uploaded network policy widened current instance access")
	}
	config.Services = []application.PortableService{}
	cleared, err := fixture.service.ImportConfiguration(ctx, config, 1, true, "admin")
	if err != nil || cleared.Revision != 2 || len(cleared.Services) != 0 {
		t.Fatalf("empty import: %#v %v", cleared, err)
	}
	retained, err := fixture.store.ManagedSettings(ctx)
	if err != nil || retained.Origin != original.Origin || strings.Join(retained.UpstreamCIDRs, ",") != strings.Join(original.UpstreamCIDRs, ",") {
		t.Fatalf("import replaced instance policy: %#v %v", retained, err)
	}
	history, err := fixture.store.DraftRevision(ctx, 1)
	if err != nil || len(history.Services) != 1 {
		t.Fatalf("empty import lost history: %#v %v", history, err)
	}
}
