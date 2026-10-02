package gormstore

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
	"github.com/libtnb/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func validManagedSettings() domain.ManagedSettings {
	return domain.ManagedSettings{Origin: "https://caddyadmin.home.example", HomelabDomain: "home.example", AdminDomain: "caddyadmin.home.example", LAN: []string{"10.0.0.0/8"}, UpstreamCIDRs: []string{"10.0.0.0/8"}, Resolvers: []string{"1.1.1.1"}}
}

func TestCompleteSetupIsAtomicAndSingleWriter(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	credentials := application.SetupCredentials{Username: "admin", Password: "long-enough-password"}
	certificate := domain.CertificateStatus{Mode: domain.CertificateModeBootstrapInternal, ActivationStatus: "idle", PublicStatus: "unknown"}
	if err := store.CompleteSetup(ctx, credentials, validManagedSettings(), certificate); err != nil {
		t.Fatal(err)
	}
	if initialized, err := store.IsInitialized(ctx); err != nil || !initialized {
		t.Fatalf("setup not initialized: %v %v", initialized, err)
	}
	if err := store.CompleteSetup(ctx, application.SetupCredentials{Username: "other", Password: "another-long-password"}, validManagedSettings(), certificate); err == nil {
		t.Fatal("second setup writer was accepted")
	}
	if _, err := store.Login(ctx, "admin", credentials.Password); err != nil {
		t.Fatal("failed second writer changed original administrator", err)
	}
}

func TestValidationAndDeploymentLifecycleThroughRepositoryPort(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	record := application.ValidationRecord{ID: "validation-1", Revision: 0, Config: []byte(`{"apps":{}}`), Services: "[]", Created: 1}
	if err := store.SaveValidation(ctx, record, "admin"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Validation(ctx, record.ID); err != nil || !json.Valid(got.Config) {
		t.Fatalf("validation did not round-trip: %#v %v", got, err)
	}
	deployment := domain.Deployment{ID: "deployment-1", Revision: 0, Status: "applying", Config: record.Config, Services: []domain.Service{}, Changes: []domain.Change{}, Actor: "admin", Created: now(), Idempotency: "key", RequestHash: "request"}
	started, err := store.BeginDeployment(ctx, 0, deployment)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishDeployment(ctx, started, "success", ""); err != nil {
		t.Fatal(err)
	}
	latest, err := store.Latest(ctx)
	if err != nil || latest.ID != deployment.ID || latest.Status != "success" {
		t.Fatalf("deployment lifecycle mismatch: %#v %v", latest, err)
	}
}

func TestDeploymentPendingIndexMetadata(t *testing.T) {
	parsed, err := schema.Parse(&deploymentRow{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	index := parsed.LookIndex("one_pending_deployment")
	if index == nil {
		t.Fatal("one_pending_deployment metadata is missing")
	}
	if index.Class != "UNIQUE" || index.Where != "status = 'applying' OR status = 'uncertain'" || len(index.Fields) != 1 || index.Fields[0].Expression != "(1)" {
		t.Fatalf("unexpected pending index metadata: %#v", index)
	}
}
func TestDraftConflictAndAudit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	d, e := s.Draft(ctx)
	if e != nil || d.Revision != 0 {
		t.Fatal(d, e)
	}
	v := Service{ID: "1", Name: "Photos", Group: "homelab", Hostname: "photo.home.example.com"}
	d, e = s.SaveService(ctx, 0, v, false, "admin")
	if e != nil || d.Revision != 1 {
		t.Fatal(d, e)
	}
	if _, e = s.SaveService(ctx, 0, v, false, "admin"); e == nil {
		t.Fatal("stale draft accepted")
	}
	a, e := s.Audits(ctx, 0, 20)
	if e != nil || len(a) != 1 {
		t.Fatal(a, e)
	}
}
func TestPasswordAndSession(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CompleteSetup(ctx, application.SetupCredentials{Username: "admin", Password: "long-enough-password"}, validManagedSettings(), domain.CertificateStatus{Mode: domain.CertificateModeBootstrapInternal, ActivationStatus: "idle", PublicStatus: "unknown"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Login(ctx, "admin", "wrong"); e == nil {
		t.Fatal("wrong password accepted")
	}
	session, e := s.Login(ctx, "admin", "long-enough-password")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Session(ctx, session.Token); e != nil {
		t.Fatal(e)
	}
	if e = s.Logout(ctx, session.Token); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Session(ctx, session.Token); e == nil {
		t.Fatal("revoked session accepted")
	}
}

func TestCertificateStatusDefaultsToBootstrapInternal(t *testing.T) {
	s := testStore(t)
	got, err := s.CertificateStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := CertificateStatus{Mode: CertificateModeBootstrapInternal, ActivationStatus: "idle", PublicStatus: "unknown"}
	if got.Mode != want.Mode || got.ActivationStatus != want.ActivationStatus || got.PublicStatus != want.PublicStatus {
		t.Fatalf("initial certificate status = %+v, want mode %q activation %q public %q", got, want.Mode, want.ActivationStatus, want.PublicStatus)
	}

	want.Mode = CertificateModeCloudflare
	want.ActivationStatus = "success"
	want.PublicStatus = "pending"
	want.BeforeHash = "before"
	want.CandidateHash = "candidate"
	if err = s.SetCertificateStatus(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err = s.CertificateStatus(context.Background())
	if err != nil || got.Mode != want.Mode || got.ActivationStatus != want.ActivationStatus || got.PublicStatus != want.PublicStatus || got.BeforeHash != want.BeforeHash || got.CandidateHash != want.CandidateHash {
		t.Fatalf("certificate status did not round-trip: got %+v want %+v: %v", got, want, err)
	}
}

func TestOpenStoreRejectsUnsupportedSchemaWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.db.Model(&schemaVersionRow{}).Where("version = ?", currentSchemaVersion).Update("version", 5).Error; err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}

	if reopened, openErr := Open(path); openErr == nil {
		reopened.Close()
		t.Fatal("v5 database was accepted and upgraded")
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	row, err := gorm.G[schemaVersionRow](db).Take(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if row.Version != 5 {
		t.Fatalf("rejected database was modified to version %d", row.Version)
	}
}

func TestOpenStoreRejectsCurrentSchemaMissingPendingIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.db.Migrator().DropIndex(&deploymentRow{}, "one_pending_deployment"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, openErr := Open(path); openErr == nil {
		reopened.Close()
		t.Fatal("v6 database missing the pending-deployment constraint was accepted")
	}
}
