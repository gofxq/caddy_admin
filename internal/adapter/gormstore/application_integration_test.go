package gormstore_test

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gofxq/caddy_admin/internal/adapter/gormstore"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type orchestrationCaddy struct {
	mu             sync.Mutex
	raw            []byte
	loadErr        error
	unreachable    bool
	applyBeforeErr bool
}

func (caddy *orchestrationCaddy) Read(context.Context) ([]byte, error) {
	caddy.mu.Lock()
	defer caddy.mu.Unlock()
	if caddy.unreachable {
		return nil, errors.New("unreachable")
	}
	return append([]byte{}, caddy.raw...), nil
}
func (*orchestrationCaddy) Validate(context.Context, []byte) error { return nil }
func (caddy *orchestrationCaddy) Load(_ context.Context, raw []byte) error {
	caddy.mu.Lock()
	defer caddy.mu.Unlock()
	if caddy.loadErr == nil || caddy.applyBeforeErr {
		caddy.raw = append([]byte{}, raw...)
	}
	return caddy.loadErr
}
func (*orchestrationCaddy) BuildInfo(context.Context) (string, bool) { return "test", true }

type orchestrationSnapshot struct {
	mu        sync.Mutex
	raw       []byte
	failWrite bool
}

func (snapshot *orchestrationSnapshot) Exists() (bool, error) {
	snapshot.mu.Lock()
	defer snapshot.mu.Unlock()
	return snapshot.raw != nil, nil
}
func (snapshot *orchestrationSnapshot) Read() ([]byte, error) {
	snapshot.mu.Lock()
	defer snapshot.mu.Unlock()
	if snapshot.raw == nil {
		return nil, errors.New("missing")
	}
	return append([]byte{}, snapshot.raw...), nil
}
func (snapshot *orchestrationSnapshot) Write(raw []byte) error {
	snapshot.mu.Lock()
	defer snapshot.mu.Unlock()
	if snapshot.failWrite {
		return errors.New("snapshot unavailable")
	}
	snapshot.raw = append([]byte{}, raw...)
	return nil
}
func (snapshot *orchestrationSnapshot) Remove() error {
	snapshot.mu.Lock()
	defer snapshot.mu.Unlock()
	snapshot.raw = nil
	return nil
}

type orchestrationFixture struct {
	service  *application.Service
	store    *gormstore.Store
	caddy    *orchestrationCaddy
	snapshot *orchestrationSnapshot
}

func newOrchestrationFixture(t *testing.T) *orchestrationFixture {
	t.Helper()
	store, err := gormstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	settings := domain.ManagedSettings{Origin: "https://caddyadmin.home.example.test", HomelabDomain: "home.example.test", AdminDomain: "caddyadmin.home.example.test", LAN: []string{"10.0.0.0/8"}, UpstreamCIDRs: []string{"10.0.0.0/8"}, Resolvers: []string{"1.1.1.1"}}
	options := application.Options{AdminURL: "http://10.0.0.4:2019", ManagerDial: "10.0.0.2:8080", ProbeAddress: "10.0.0.3:443", StaticRoot: "/srv/web", CaddyStorage: "/data/caddy", Socket: "/run/caddy.sock", HTTPPort: "80", HTTPSPort: "443", CertificateMode: domain.CertificateModeCloudflare, RuntimePolicy: settings}
	initial, err := domain.Generate(domain.CaddyConfig{AdminURL: options.AdminURL, PublicDomain: settings.PublicDomain, HomelabDomain: settings.HomelabDomain, AdminDomain: settings.AdminDomain, LAN: settings.LAN, Resolvers: settings.Resolvers, ManagerDial: options.ManagerDial, StaticRoot: options.StaticRoot, CaddyStorage: options.CaddyStorage, Socket: options.Socket, CertificateMode: options.CertificateMode, HTTPPort: options.HTTPPort, HTTPSPort: options.HTTPSPort}, nil)
	if err != nil {
		t.Fatal(err)
	}
	caddy := &orchestrationCaddy{raw: initial}
	snapshot := &orchestrationSnapshot{raw: initial}
	service := application.New(options, store, caddy, application.Dependencies{Snapshot: snapshot, LocalAddresses: func() ([]net.Addr, error) { return nil, nil }})
	return &orchestrationFixture{service: service, store: store, caddy: caddy, snapshot: snapshot}
}

func (fixture *orchestrationFixture) validation(t *testing.T) domain.Preview {
	t.Helper()
	draft, err := fixture.service.SaveService(context.Background(), 0, domain.Service{Name: "Photos", Group: "homelab", Hostname: "photos.home.example.test", Scheme: "http", Host: "10.0.0.10", Port: 8080, Enabled: true}, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := fixture.service.Validate(context.Background(), draft.Revision, "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	return preview
}

func TestPublishIdempotencyAndLostResponseReconcile(t *testing.T) {
	fixture := newOrchestrationFixture(t)
	preview := fixture.validation(t)
	request := domain.PublishRequest{ValidationID: preview.ValidationID, Revision: preview.Revision, ExpectedHash: preview.RuntimeHash, Idempotency: "0123456789abcdef"}
	deployment, err := fixture.service.Begin(context.Background(), request, "admin")
	if err != nil {
		t.Fatal(err)
	}
	same, err := fixture.service.Begin(context.Background(), request, "admin")
	if err != nil || same.ID != deployment.ID {
		t.Fatalf("idempotent Begin() = %s, %v", same.ID, err)
	}
	changed := request
	changed.ConfirmDrift = true
	if _, err = fixture.service.Begin(context.Background(), changed, "admin"); err == nil {
		t.Fatal("idempotency collision accepted")
	}
	fixture.caddy.loadErr = errors.New("response lost")
	fixture.caddy.applyBeforeErr = true
	fixture.service.Apply(deployment.ID)
	saved, err := fixture.store.Deployment(context.Background(), deployment.ID)
	if err != nil || saved.Status != "success" {
		t.Fatalf("lost response was not reconciled: status=%s err=%v", saved.Status, err)
	}
}

type staleValidationRepository struct{ application.Repository }

func (repository *staleValidationRepository) Validation(ctx context.Context, id string) (application.ValidationRecord, error) {
	record, err := repository.Repository.Validation(ctx, id)
	record.Created = time.Now().Add(-16 * time.Minute).Unix()
	return record, err
}

func TestExpiredValidationIsRejected(t *testing.T) {
	fixture := newOrchestrationFixture(t)
	preview := fixture.validation(t)
	service := application.New(application.Options{AdminURL: "http://10.0.0.4:2019", ManagerDial: "10.0.0.2:8080", ProbeAddress: "10.0.0.3:443", StaticRoot: "/srv/web", CaddyStorage: "/data/caddy", Socket: "/run/caddy.sock", HTTPPort: "80", HTTPSPort: "443", CertificateMode: domain.CertificateModeCloudflare, RuntimePolicy: domain.ManagedSettings{Origin: "https://caddyadmin.home.example.test", HomelabDomain: "home.example.test", AdminDomain: "caddyadmin.home.example.test", LAN: []string{"10.0.0.0/8"}, UpstreamCIDRs: []string{"10.0.0.0/8"}, Resolvers: []string{"1.1.1.1"}}}, &staleValidationRepository{Repository: fixture.store}, fixture.caddy, application.Dependencies{Snapshot: fixture.snapshot, LocalAddresses: func() ([]net.Addr, error) { return nil, nil }})
	_, err := service.Begin(context.Background(), domain.PublishRequest{ValidationID: preview.ValidationID, Revision: preview.Revision, ExpectedHash: preview.RuntimeHash, Idempotency: "fedcba9876543210"}, "admin")
	if err == nil {
		t.Fatal("expired validation accepted")
	}
}

func TestSnapshotFailureMarksDeploymentUncertainAndBlocksNext(t *testing.T) {
	fixture := newOrchestrationFixture(t)
	preview := fixture.validation(t)
	request := domain.PublishRequest{ValidationID: preview.ValidationID, Revision: preview.Revision, ExpectedHash: preview.RuntimeHash, Idempotency: "0123456789abcdef"}
	deployment, err := fixture.service.Begin(context.Background(), request, "admin")
	if err != nil {
		t.Fatal(err)
	}
	fixture.snapshot.failWrite = true
	fixture.service.Apply(deployment.ID)
	saved, err := fixture.store.Deployment(context.Background(), deployment.ID)
	if err != nil || saved.Status != "uncertain" {
		t.Fatalf("status=%s err=%v", saved.Status, err)
	}
	request.Idempotency = "abcdef0123456789"
	if _, err = fixture.service.Begin(context.Background(), request, "admin"); err == nil {
		t.Fatal("unresolved deployment did not block the next publication")
	}
}
