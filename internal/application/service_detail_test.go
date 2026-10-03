package application

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type detailRepository struct {
	Repository
	draft       domain.Draft
	latest      domain.Deployment
	pending     domain.Deployment
	history     []domain.Deployment
	latestReads int
	onLatest    func(int) domain.Deployment
	onPending   func() (domain.Deployment, error)
}

func (r *detailRepository) Draft(context.Context) (domain.Draft, error) { return r.draft, nil }
func (r *detailRepository) Latest(context.Context) (domain.Deployment, error) {
	r.latestReads++
	if r.onLatest != nil {
		return r.onLatest(r.latestReads), nil
	}
	if r.latest.ID == "" {
		return domain.Deployment{}, domain.NotFound("none")
	}
	return r.latest, nil
}
func (r *detailRepository) Pending(context.Context) (domain.Deployment, error) {
	if r.onPending != nil {
		return r.onPending()
	}
	if r.pending.ID == "" {
		return domain.Deployment{}, domain.NotFound("none")
	}
	return r.pending, nil
}
func (r *detailRepository) Deployments(_ context.Context, offset, limit int) ([]domain.Deployment, error) {
	if offset != 0 || limit != 20 {
		panic("unbounded history")
	}
	return r.history, nil
}

type detailCaddy struct {
	CaddyPort
	raw  []byte
	err  error
	read func(context.Context) ([]byte, error)
}

func (c *detailCaddy) Read(ctx context.Context) ([]byte, error) {
	if c.read != nil {
		return c.read(ctx)
	}
	return c.raw, c.err
}

type detailSnapshot struct{ raw []byte }

func (s *detailSnapshot) Read() ([]byte, error) { return s.raw, nil }
func (s *detailSnapshot) Write([]byte) error    { return nil }
func (s *detailSnapshot) Exists() (bool, error) { return true, nil }
func (s *detailSnapshot) Remove() error         { return nil }

func detailFixture() (*Service, *detailRepository, *detailCaddy) {
	raw := []byte(`{"apps":{"http":{}}}`)
	access := "trusted"
	settings := domain.ManagedSettings{Domains: []domain.ManagedDomain{{ID: "home", Name: "example.com", Access: &access}}, AdminDomain: "admin.example.com", Origin: "https://admin.example.com", LAN: []string{"10.0.0.0/8"}, UpstreamCIDRs: []string{"10.0.0.0/8"}, AllowedNames: []string{"photos.internal"}, Resolvers: []string{"1.1.1.1"}}
	item := domain.Service{ID: "photos", Name: "Photos", DomainID: "home", Hostname: "photos.example.com", Scheme: "http", Host: "10.42.0.8", Port: 2283, Enabled: true, Dial: "10.42.0.8:2283"}
	repository := &detailRepository{draft: domain.Draft{Revision: 7, Services: []domain.Service{item}, Settings: settings}, latest: domain.Deployment{ID: "release-1", Version: 1, Status: "success", Hash: domain.Fingerprint(raw), Config: raw, Services: []domain.Service{item}, Settings: settings}}
	caddy := &detailCaddy{raw: raw}
	service := New(Options{RuntimePolicy: settings, ManagerDial: "127.0.0.1:8080", ProbeAddress: "127.0.0.1:443"}, repository, caddy, Dependencies{Snapshot: &detailSnapshot{raw: raw}, LocalAddresses: func() ([]net.Addr, error) { return nil, nil }})
	return service, repository, caddy
}

func TestServiceDetailSeparatesDraftAndPublishedByID(t *testing.T) {
	service, repo, _ := detailFixture()
	repo.draft.Services[0].Hostname = "new.example.com"
	repo.draft.Settings.Domains = append([]domain.ManagedDomain{}, repo.draft.Settings.Domains...)
	repo.draft.Settings.Domains[0].Access = nil
	detail, err := service.ServiceDetail(context.Background(), "photos")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Draft.Hostname != "new.example.com" || detail.Published.Hostname != "photos.example.com" || detail.Runtime.Status != "matched" {
		t.Fatalf("detail=%+v", detail)
	}
	if detail.DraftDomain.Access != nil || detail.PublishedDomain.Access == nil {
		t.Fatal("draft policy mixed with published")
	}
	repo.draft.Services = []domain.Service{{ID: "replacement", Hostname: "photos.example.com"}}
	detail, err = service.ServiceDetail(context.Background(), "photos")
	if err != nil || detail.Draft != nil || detail.Published.ID != "photos" {
		t.Fatalf("deleted draft=%+v err=%v", detail, err)
	}
	if _, err = service.ServiceDetail(context.Background(), "missing"); !domain.IsMissing(err) {
		t.Fatalf("unknown ID=%v", err)
	}
	repo.latest = domain.Deployment{}
	detail, err = service.ServiceDetail(context.Background(), "replacement")
	if err != nil || detail.Published != nil || detail.Draft.ID != "replacement" {
		t.Fatalf("draft only=%+v err=%v", detail, err)
	}
}
func TestServiceDetailKeepsConfigurationWhenRuntimeUnknown(t *testing.T) {
	service, repo, caddy := detailFixture()
	for _, tc := range []struct {
		name, status string
		raw          []byte
		err          error
		pending      domain.Deployment
	}{
		{"offline", "unknown", nil, errors.New("secret internal detail"), domain.Deployment{}},
		{"drift", "drift", []byte(`{"apps":{}}`), nil, domain.Deployment{}},
		{"uncertain", "pending", caddy.raw, nil, domain.Deployment{ID: "pending", Status: "uncertain"}},
		{"applying offline", "pending", nil, errors.New("offline"), domain.Deployment{ID: "pending", Status: "applying"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caddy.raw = tc.raw
			caddy.err = tc.err
			repo.pending = tc.pending
			detail, err := service.ServiceDetail(context.Background(), "photos")
			if err != nil || detail.Runtime.Status != tc.status || detail.Draft == nil || detail.Published == nil {
				t.Fatalf("detail=%+v err=%v", detail, err)
			}
		})
	}
}
func TestServiceDetailFiltersRecentChangesByServiceID(t *testing.T) {
	service, repo, _ := detailFixture()
	for i := 0; i < 8; i++ {
		repo.history = append(repo.history, domain.Deployment{ID: "related", Version: int64(9 - i), Status: "success", Changes: []domain.Change{{Before: &domain.Service{ID: "photos"}}}})
	}
	repo.history = append([]domain.Deployment{{ID: "unrelated", Services: repo.latest.Services, Changes: []domain.Change{{After: &domain.Service{ID: "another", Hostname: "photos.example.com"}}}}}, repo.history...)
	detail, err := service.ServiceDetail(context.Background(), "photos")
	if err != nil || len(detail.RecentDeployments) != 5 || detail.RecentDeployments[0].Version != 9 {
		t.Fatalf("history=%+v err=%v", detail.RecentDeployments, err)
	}
}
func TestServiceDetailRejectsMixedRuntimeAndPublishedVersions(t *testing.T) {
	service, repo, caddy := detailFixture()
	caddy.read = func(ctx context.Context) ([]byte, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("runtime read is unbounded")
		}
		repo.latest.ID = "release-2"
		repo.latest.Hash = domain.Fingerprint([]byte(`{"changed":true}`))
		return caddy.raw, nil
	}
	detail, err := service.ServiceDetail(context.Background(), "photos")
	if err != nil || detail.Runtime.Status != "unknown" {
		t.Fatalf("mixed version=%+v err=%v", detail.Runtime, err)
	}
}

func TestServiceDetailDetectsPublishCompletedDuringPendingRead(t *testing.T) {
	service, repo, _ := detailFixture()
	repo.onPending = func() (domain.Deployment, error) {
		repo.latest.ID = "release-2"
		repo.latest.Hash = domain.Fingerprint([]byte(`{"new":true}`))
		return domain.Deployment{}, domain.NotFound("none")
	}
	detail, err := service.ServiceDetail(context.Background(), "photos")
	if err != nil || detail.Runtime.Status != "unknown" {
		t.Fatalf("old model marked verified: runtime=%+v err=%v", detail.Runtime, err)
	}
}
