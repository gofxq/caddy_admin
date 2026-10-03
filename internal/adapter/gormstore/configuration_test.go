package gormstore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestConfigurationExportContainsOnlyPortableBusinessData(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	settings := validManagedSettings()
	if err := store.CompleteSetup(ctx, application.SetupCredentials{Username: "admin", Password: "private-admin-password"}, settings, domain.CertificateStatus{Mode: domain.CertificateModeBootstrapInternal, ActivationStatus: "idle", PublicStatus: "unknown"}); err != nil {
		t.Fatal(err)
	}
	_, err := store.SaveService(ctx, 0, domain.Service{ID: "internal-id", Name: "Photos", DomainID: "home", Hostname: "photos.home.example", Scheme: "http", Host: "10.0.0.8", Port: 8080, Enabled: true, Dial: "10.0.0.8:8080", UpdatedAt: "internal-time"}, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(application.Options{RuntimePolicy: settings}, store, nil, application.Dependencies{})
	config, err := service.ExportConfiguration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-admin-password", "internal-id", "internal-time", `"dial"`, `"password"`, `"token"`, `"csrf"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("export exposed %s", forbidden)
		}
	}
	if config.Version != 2 || config.Format != "caddy-web-admin" || len(config.Services) != 1 || config.Settings.Domains[0].Name != "home.example" {
		t.Fatalf("export = %#v", config)
	}
}

func TestReplaceDraftIsAtomicPreservesHistoryAndRejectsConflicts(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	values := []domain.Service{{ID: "one", Hostname: "one.home.example"}, {ID: "two", Hostname: "two.home.example"}}
	draft, err := store.ReplaceDraft(ctx, 0, values, "admin")
	if err != nil || draft.Revision != 1 || len(draft.Services) != 2 {
		t.Fatalf("replace = %#v %v", draft, err)
	}
	old, err := store.DraftRevision(ctx, 0)
	if err != nil || len(old.Services) != 0 {
		t.Fatalf("history lost: %#v %v", old, err)
	}
	if _, err = store.ReplaceDraft(ctx, 0, nil, "admin"); err == nil {
		t.Fatal("stale revision accepted")
	}
	if _, err = store.ReplaceDraft(ctx, 1, []domain.Service{{ID: "three", Hostname: "same.home.example"}, {ID: "four", Hostname: "same.home.example"}}, "admin"); err == nil {
		t.Fatal("duplicate accepted")
	}
	current, err := store.Draft(ctx)
	if err != nil || current.Revision != 1 || len(current.Services) != 2 {
		t.Fatalf("failed import mutated draft: %#v %v", current, err)
	}
	audits, err := store.Audits(ctx, 0, 20)
	if err != nil || len(audits) != 1 || audits[0].Action != "configuration.import" {
		t.Fatalf("audit = %#v %v", audits, err)
	}
}

func TestSetupWithDraftRollsBackAllDataOnLateFailure(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	err := store.CompleteSetupWithDraft(ctx, application.SetupCredentials{Username: "admin", Password: "private-admin-password"}, validManagedSettings(), domain.CertificateStatus{Mode: "invalid"}, []domain.Service{{ID: "one", Hostname: "one.home.example"}})
	if err == nil {
		t.Fatal("invalid certificate accepted")
	}
	initialized, err := store.IsInitialized(ctx)
	if err != nil || initialized {
		t.Fatalf("failed setup left initialized state: %v %v", initialized, err)
	}
	var admins, settings int64
	store.db.Model(&adminRow{}).Count(&admins)
	store.db.Model(&managedSettingsRow{}).Count(&settings)
	if admins != 0 || settings != 0 {
		t.Fatalf("partial setup persisted: admins=%d settings=%d", admins, settings)
	}
	draft, err := store.Draft(ctx)
	if err != nil || draft.Revision != 0 || len(draft.Services) != 0 {
		t.Fatalf("failed setup changed draft: %#v %v", draft, err)
	}
	if _, err = store.DraftRevision(ctx, 1); err == nil {
		t.Fatal("failed setup left immutable revision")
	}
	audits, err := store.Audits(ctx, 0, 20)
	if err != nil || len(audits) != 0 {
		t.Fatalf("failed setup left audit: %#v %v", audits, err)
	}
}
