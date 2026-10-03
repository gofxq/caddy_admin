package gormstore

import (
	"context"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
	"reflect"
	"testing"
)

func TestSettingsHistoryAndSuccessfulDeploymentCommit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	original := validManagedSettings()
	if err := s.CompleteSetup(ctx, application.SetupCredentials{Username: "admin", Password: "long-enough-password"}, original, domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, ActivationStatus: "idle", PublicStatus: "unknown"}); err != nil {
		t.Fatal(err)
	}
	candidate := original
	candidate.ConsoleLANOnly = true
	d, err := s.SaveSettings(ctx, 0, candidate, "admin")
	if err != nil {
		t.Fatal(err)
	}
	active, _ := s.ManagedSettings(ctx)
	if active.ConsoleLANOnly {
		t.Fatal("draft changed active")
	}
	history, _ := s.DraftRevision(ctx, 0)
	if history.Settings.ConsoleLANOnly {
		t.Fatal("history mutated")
	}
	if _, err = s.SaveSettings(ctx, 0, original, "admin"); err == nil {
		t.Fatal("stale settings accepted")
	}
	serviceDraft, err := s.SaveService(ctx, d.Revision, domain.Service{ID: "one", DomainID: "home", Hostname: "one.home.example"}, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(serviceDraft.Settings, candidate) {
		t.Fatal("service write lost settings")
	}
	p, err := s.BeginDeployment(ctx, serviceDraft.Revision, domain.Deployment{ID: "publish", Revision: d.Revision, Status: "applying", Config: []byte(`{}`), Settings: candidate, Services: []domain.Service{}, Changes: []domain.Change{}, Idempotency: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishDeployment(ctx, p, "success", ""); err != nil {
		t.Fatal(err)
	}
	active, _ = s.ManagedSettings(ctx)
	if !reflect.DeepEqual(active, candidate) {
		t.Fatal("success did not commit policy")
	}
	after, _ := s.Draft(ctx)
	if !reflect.DeepEqual(after, serviceDraft) {
		t.Fatal("publication modified draft")
	}
}

func TestSetupKeepsImportedPolicyAsUnpublishedDraft(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	imported := validManagedSettings()
	imported.ConsoleLANOnly = true
	imported.Domains = append(imported.Domains, domain.ManagedDomain{ID: "extra", Name: "extra.example", Access: domain.DomainAccess("internet")})
	if err := s.CompleteSetup(ctx, application.SetupCredentials{Username: "admin", Password: "long-enough-password"}, imported, domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, ActivationStatus: "idle", PublicStatus: "unknown"}); err != nil {
		t.Fatal(err)
	}
	active, err := s.ManagedSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if active.ConsoleLANOnly || len(active.Domains) != 1 {
		t.Fatalf("import prematurely activated: %+v", active)
	}
	draft, err := s.Draft(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(draft.Settings, imported) {
		t.Fatalf("import draft lost settings: %+v", draft)
	}
	old, err := s.DraftRevision(ctx, 0)
	if err != nil || !reflect.DeepEqual(old.Settings, imported) {
		t.Fatalf("setup history lost settings: %+v %v", old, err)
	}
}

func TestConsoleHandoffSuccessRevokesSessionsAtomically(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	settings := validManagedSettings()
	if err := s.CompleteSetup(ctx, application.SetupCredentials{Username: "admin", Password: "long-enough-password"}, settings, domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, ActivationStatus: "idle", PublicStatus: "unknown"}); err != nil {
		t.Fatal(err)
	}
	settings.PreviousAdminDomain = "old.home.example"
	settings.PreviousOrigin = "https://old.home.example"
	first, err := s.BeginDeployment(ctx, 0, domain.Deployment{ID: "first", Status: "applying", Config: []byte(`{}`), Settings: settings, Services: []domain.Service{}, Changes: []domain.Change{}, Idempotency: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishDeployment(ctx, first, "success", ""); err != nil {
		t.Fatal(err)
	}
	session, err := s.Login(ctx, "admin", "long-enough-password")
	if err != nil {
		t.Fatal(err)
	}
	settings.PreviousAdminDomain = ""
	settings.PreviousOrigin = ""
	second, err := s.BeginDeployment(ctx, 0, domain.Deployment{ID: "second", Status: "applying", Config: []byte(`{}`), Settings: settings, Services: []domain.Service{}, Changes: []domain.Change{}, Idempotency: "second"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a caller with stale settings: the persisted candidate is authoritative.
	second.Settings = first.Settings
	if err = s.FinishDeployment(ctx, second, "success", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, session.Token); err == nil {
		t.Fatal("old handoff session survived committed removal")
	}
	active, err := s.ManagedSettings(ctx)
	if err != nil || active.PreviousAdminDomain != "" {
		t.Fatalf("handoff not committed: %+v %v", active, err)
	}
}

func TestValidationPreservesCandidatePolicy(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	settings := validManagedSettings()
	record := application.ValidationRecord{ID: "policy", Revision: 0, Services: "[]", Config: []byte(`{}`), Settings: settings, Created: 1}
	if err := s.SaveValidation(ctx, record, "admin"); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Validation(ctx, record.ID)
	if err != nil || !reflect.DeepEqual(stored.Settings, settings) {
		t.Fatalf("validation lost policy: %+v %v", stored, err)
	}
}
