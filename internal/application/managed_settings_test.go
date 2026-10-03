package application

import (
	"context"
	"encoding/json"
	"github.com/gofxq/caddy_admin/internal/domain"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSetupPreconfiguredCredentialsExposeOnlyStatus(t *testing.T) {
	s := New(Options{SetupPassword: "private-password", SetupToken: strings.Repeat("a", 40)}, &setupRepository{}, &setupCaddy{}, Dependencies{})
	status, err := s.SetupStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(status)
	if strings.Contains(string(raw), "private-password") || strings.Contains(string(raw), strings.Repeat("a", 40)) {
		t.Fatal("credential leaked")
	}
	if status.AdminPasswordStatus != "ready" || status.CloudflareTokenStatus != "ready" || status.SetupID == "" {
		t.Fatalf("missing preconfigured status: %s", raw)
	}
}

func TestOrdinarySetupDoesNotCreateNetworkOrDomainAccessPolicy(t *testing.T) {
	settings, err := ManagedSettingsForSetup(SetupSettings{Domain: "*.Example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if settings.ConsoleLANOnly || len(settings.LAN) != 0 || len(settings.UpstreamCIDRs) != 0 {
		t.Fatal("setup silently added network policy")
	}
	if len(settings.Domains) != 1 || settings.Domains[0].Access != nil || settings.Domains[0].Name != "example.com" {
		t.Fatalf("wrong domain: %#v", settings.Domains)
	}
}

func TestConfiguredPasswordRequiresCurrentExplicitConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		confirm bool
		id      string
		want    bool
	}{
		{"missing confirmation", false, "", false},
		{"stale process", true, "stale", false},
		{"confirmed", true, "current", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &setupRepository{}
			snapshot := &setupSnapshot{}
			s := New(Options{SetupPassword: "private-password", TestTLS: true, ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, repo, &setupCaddy{}, Dependencies{Snapshot: snapshot, SetupProbe: setupProbe{resolver: true, dns: true}})
			id := tc.id
			if id == "current" {
				id = s.setupID
			}
			_, err := s.CompleteSetup(context.Background(), SetupRequest{Username: "admin", Settings: SetupSettings{Domain: "example.com"}, UseConfiguredPassword: tc.confirm, SetupID: id})
			if (err == nil) != tc.want || repo.completed != tc.want {
				t.Fatalf("completion=%v err=%v", repo.completed, err)
			}
		})
	}
}

func TestPreconfiguredCredentialsFourCombinationsAndInvalidValues(t *testing.T) {
	for _, tc := range []struct{ password, token, passwordStatus, tokenStatus string }{
		{"", "", "missing", "missing"},
		{"private-password", "", "ready", "missing"},
		{"", strings.Repeat("a", 40), "missing", "ready"},
		{"private-password", strings.Repeat("a", 40), "ready", "ready"},
		{"short", "bad token", "invalid", "invalid"},
	} {
		s := New(Options{SetupPassword: tc.password, SetupToken: tc.token}, &setupRepository{}, &setupCaddy{}, Dependencies{})
		status, err := s.SetupStatus(context.Background())
		if err != nil || status.AdminPasswordStatus != tc.passwordStatus || status.CloudflareTokenStatus != tc.tokenStatus {
			t.Fatalf("status=%+v err=%v", status, err)
		}
		if tc.token != "" {
			if _, err = s.ResolveSetupToken("", false, s.setupID); err == nil {
				t.Fatal("preconfigured token used without confirmation")
			}
			if _, err = s.ResolveSetupToken("", true, "stale"); err == nil {
				t.Fatal("stale confirmation accepted")
			}
			_, err = s.ResolveSetupToken("", true, s.setupID)
			if (err == nil) != (tc.tokenStatus == "ready") {
				t.Fatalf("confirmed token err=%v", err)
			}
		}
	}
}

func TestTrustedNetworkWideningRequiresExposureConfirmation(t *testing.T) {
	before := domain.ManagedSettings{ConsoleLANOnly: true, LAN: []string{"10.0.0.0/8", "fd00::/8"}}
	for _, tc := range []struct {
		ranges []string
		widen  bool
	}{
		{[]string{"10.1.0.0/16"}, false},
		{[]string{"10.0.0.0/8", "fd01::/16"}, false},
		{[]string{"0.0.0.0/0"}, true},
		{[]string{"10.0.0.0/8", "192.168.0.0/16"}, true},
		{[]string{"::/0"}, true},
	} {
		after := before
		after.LAN = tc.ranges
		if widensAccess(before, after, nil, nil) != tc.widen {
			t.Fatalf("ranges=%v", tc.ranges)
		}
	}
	before.ConsoleLANOnly = false
	before.Domains = []domain.ManagedDomain{{ID: "a", Name: "example.com", Access: domain.DomainAccess("trusted")}}
	after := before
	after.LAN = []string{"0.0.0.0/0"}
	if !widensAccess(before, after, nil, nil) {
		t.Fatal("business trusted-network widening overlooked")
	}
}

type accessCommitRepository struct {
	Repository
	mu        sync.Mutex
	pending   bool
	committed chan struct{}
	release   chan struct{}
}

func (r *accessCommitRepository) Pending(context.Context) (domain.Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending {
		return domain.Deployment{Settings: domain.ManagedSettings{ConsoleLANOnly: true, LAN: []string{"10.0.0.0/8"}}}, nil
	}
	return domain.Deployment{}, domain.NotFound("none")
}
func (r *accessCommitRepository) FinishDeployment(context.Context, domain.Deployment, string, string) error {
	r.mu.Lock()
	r.pending = false
	r.mu.Unlock()
	close(r.committed)
	<-r.release
	return nil
}

type accessCommitCaddy struct {
	CaddyPort
	raw []byte
}

func (c *accessCommitCaddy) Read(context.Context) ([]byte, error) { return c.raw, nil }
func TestAccessCannotObserveSuccessfulCommitWithOldPolicy(t *testing.T) {
	repo := &accessCommitRepository{pending: true, committed: make(chan struct{}), release: make(chan struct{})}
	raw := []byte(`{"apps":{}}`)
	s := New(Options{RuntimePolicy: domain.ManagedSettings{}}, repo, &accessCommitCaddy{raw: raw}, Dependencies{Snapshot: &setupSnapshot{}})
	d := domain.Deployment{Hash: domain.Fingerprint(raw), Config: raw, Settings: domain.ManagedSettings{ConsoleLANOnly: true, LAN: []string{"10.0.0.0/8"}}}
	complete := make(chan error, 1)
	go func() { complete <- s.reconcile(context.Background(), d) }()
	<-repo.committed
	checked := make(chan error, 1)
	go func() { checked <- s.ControlAccess(context.Background(), "203.0.113.9") }()
	select {
	case err := <-checked:
		close(repo.release)
		t.Fatalf("access ran before policy commit finished: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(repo.release)
	if err := <-complete; err != nil {
		t.Fatal(err)
	}
	if err := <-checked; err == nil {
		t.Fatal("old policy allowed public source")
	}
}

func TestImportedSettingsRequireConfirmationEvenWithoutServices(t *testing.T) {
	settings, err := ManagedSettingsForSetup(SetupSettings{Domain: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	s := New(Options{TestTLS: true, ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, &setupRepository{}, &setupCaddy{}, Dependencies{Snapshot: &setupSnapshot{}, SetupProbe: setupProbe{resolver: true, dns: true}})
	request := SetupRequest{Username: "admin", Password: "test-password", Settings: SetupSettings{Domain: "example.com"}, ImportSettings: &settings}
	if _, err = s.CompleteSetup(context.Background(), request); err == nil {
		t.Fatal("imported settings adopted without confirmation")
	}
	request.ConfirmImport = true
	if _, err = s.CompleteSetup(context.Background(), request); err != nil {
		t.Fatal(err)
	}
}
