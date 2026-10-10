package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestPortalUsesPublishedModelAndFiltersByTrustedSource(t *testing.T) {
	s, r, _ := detailFixture()
	internet := "internet"
	r.latest.Settings.Domains = append(r.latest.Settings.Domains, domain.ManagedDomain{ID: "public", Name: "public.test", Access: &internet})
	r.latest.Services = append(r.latest.Services,
		domain.Service{ID: "public", Name: "Alpha", DomainID: "public", Hostname: "app.public.test", Enabled: true, Host: "secret.internal", Notes: "private note"},
		domain.Service{ID: "off", Name: "Disabled", DomainID: "public", Hostname: "off.public.test", Enabled: false})
	r.draft.Services = []domain.Service{{Name: "Draft only", Enabled: true}}
	for _, tc := range []struct {
		source string
		count  int
	}{{"203.0.113.1", 1}, {"10.1.2.3", 2}, {"::ffff:10.1.2.3", 2}, {"invalid", 1}} {
		result, err := s.Portal(context.Background(), tc.source)
		if err != nil || len(result) != tc.count {
			t.Fatalf("source=%s result=%+v err=%v", tc.source, result, err)
		}
		if result[0].Name != "Alpha" || result[0].URL != "https://app.public.test" {
			t.Fatalf("result=%+v", result)
		}
		raw, _ := json.Marshal(result)
		for _, secret := range []string{"secret.internal", "private note", "dial", "notes", "Draft only", "Disabled"} {
			if strings.Contains(string(raw), secret) {
				t.Fatalf("leaked %s: %s", secret, raw)
			}
		}
	}
	// The next successful version changes the guide, including rollback/removal.
	r.latest.Services = nil
	result, err := s.Portal(context.Background(), "10.1.2.3")
	if err != nil || result == nil || len(result) != 0 {
		t.Fatalf("empty result=%+v err=%v", result, err)
	}
	r.latest = domain.Deployment{}
	result, err = s.Portal(context.Background(), "10.1.2.3")
	if err != nil || result == nil || len(result) != 0 {
		t.Fatalf("initial result=%+v err=%v", result, err)
	}
}

func TestPortalFailsClosedDuringPendingPublishOrRepositoryFailure(t *testing.T) {
	s, r, _ := detailFixture()
	r.pending = domain.Deployment{ID: "pending", Status: "uncertain"}
	if _, err := s.Portal(context.Background(), "10.1.2.3"); err == nil {
		t.Fatal("pending publish disclosed stale policy")
	}
	r.onPending = func() (domain.Deployment, error) { return domain.Deployment{}, errors.New("database unavailable") }
	if _, err := s.Portal(context.Background(), "10.1.2.3"); err == nil {
		t.Fatal("repository error ignored")
	}
}

func TestPortalDoesNotTrustForwardedAddressFromArbitraryPeer(t *testing.T) {
	service, _, _ := detailFixture()
	address := service.ClientAddress(context.Background(), "203.0.113.1:1234", "10.1.2.3")
	result, err := service.Portal(context.Background(), address)
	if err != nil || len(result) != 0 {
		t.Fatalf("spoofed trusted address disclosed services: %+v err=%v", result, err)
	}
	address = service.ClientAddress(context.Background(), "127.0.0.1:1234", "10.1.2.3")
	result, err = service.Portal(context.Background(), address)
	if err != nil || len(result) != 1 {
		t.Fatalf("confirmed Caddy source lost trusted address: %+v err=%v", result, err)
	}
}

func TestPortalDoesNotWaitForAnInProgressConfigurationOperation(t *testing.T) {
	service, _, _ := detailFixture()
	service.mu.Lock()
	defer service.mu.Unlock()
	_, err := service.Portal(context.Background(), "10.1.2.3")
	appErr, ok := err.(*domain.AppError)
	if !ok || appErr.Status != 503 || appErr.Code != "policy_pending" {
		t.Fatalf("busy operation returned %v", err)
	}
}

func TestPortalReadCannotOverlapPublication(t *testing.T) {
	service, repo, _ := detailFixture()
	repo.onLatest = func(_ int) domain.Deployment {
		if service.mu.TryLock() {
			service.mu.Unlock()
			t.Error("publication could start while reading the directory")
		}
		return repo.latest
	}
	if _, err := service.Portal(context.Background(), "10.1.2.3"); err != nil {
		t.Fatal(err)
	}
}
