package application

import (
	"context"
	"errors"
	"testing"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type setupRepository struct {
	Repository
	completed bool
}

type setupCaddy struct{ CaddyPort }

func (*setupRepository) IsInitialized(context.Context) (bool, error) { return false, nil }
func (repository *setupRepository) CompleteSetup(context.Context, SetupCredentials, domain.ManagedSettings, domain.CertificateStatus) error {
	repository.completed = true
	return nil
}

type setupSnapshot struct{ written bool }

func (*setupSnapshot) Exists() (bool, error) { return false, nil }
func (*setupSnapshot) Read() ([]byte, error) { return nil, errors.New("missing") }
func (snapshot *setupSnapshot) Write([]byte) error {
	snapshot.written = true
	return nil
}
func (*setupSnapshot) Remove() error { return nil }

type setupProbe struct {
	resolver bool
	dns      bool
	console  bool
}

func (probe setupProbe) ResolverReachable(context.Context, []string) bool { return probe.resolver }
func (probe setupProbe) AdminDNS(context.Context, string, []string) bool  { return probe.dns }
func (probe setupProbe) ExternalConsole(context.Context, string, string) bool {
	return probe.console
}

func validSetupSettings() SetupSettings {
	return SetupSettings{HomelabDomain: "Home.Example.com.", LAN: []string{"10.0.0.0/8"}, UpstreamCIDRs: []string{"10.0.0.0/8"}, Resolvers: []string{"10.0.0.53"}}
}

func TestPreflightSetupNormalizesSettingsAndClassifiesChecks(t *testing.T) {
	service := New(Options{}, &setupRepository{}, &setupCaddy{}, Dependencies{SetupProbe: setupProbe{resolver: true}})
	result := service.PreflightSetup(context.Background(), validSetupSettings())
	if result.Normalized.AdminDomain != "caddyadmin.home.example.com" || result.Normalized.AdminOrigin != "https://caddyadmin.home.example.com" {
		t.Fatalf("normalized settings = %#v", result.Normalized)
	}
	if !result.CanComplete || !result.RequiresAcknowledgement {
		t.Fatalf("preflight flags = complete %v acknowledgement %v", result.CanComplete, result.RequiresAcknowledgement)
	}
	want := map[string]string{"settings_valid": "pass", "network_scope": "pass", "resolver_reachable": "pass", "admin_dns": "warning"}
	for _, check := range result.Checks {
		if status, ok := want[check.ID]; ok {
			if check.Status != status {
				t.Errorf("%s=%s, want %s", check.ID, check.Status, status)
			}
			delete(want, check.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing checks: %v", want)
	}
}

func TestNetworkPreflightDoesNotRequireUnenteredDNSButCompletionDoes(t *testing.T) {
	service := New(Options{}, &setupRepository{}, &setupCaddy{}, Dependencies{})
	input := validSetupSettings()
	input.Resolvers = nil
	result := service.PreflightSetup(context.Background(), input)
	if !result.NetworkValid || result.CanComplete {
		t.Fatalf("network valid / completion blocked = %#v", result)
	}
	input.LAN = []string{"invalid"}
	result = service.PreflightSetup(context.Background(), input)
	if result.NetworkValid || result.NetworkError == "" {
		t.Fatalf("invalid network was not blocked = %#v", result)
	}
}

func TestPreflightSetupBlocksInvalidSettingsAndWarnsForOpenNetworks(t *testing.T) {
	service := New(Options{}, &setupRepository{}, &setupCaddy{}, Dependencies{SetupProbe: setupProbe{resolver: true, dns: true}})
	invalid := validSetupSettings()
	invalid.Resolvers = []string{"not-an-ip"}
	blocked := service.PreflightSetup(context.Background(), invalid)
	if blocked.CanComplete || blocked.Checks[0].ID != "settings_valid" || blocked.Checks[0].Status != "block" {
		t.Fatalf("invalid preflight = %#v", blocked)
	}
	open := validSetupSettings()
	open.LAN = []string{"0.0.0.0/0"}
	result := service.PreflightSetup(context.Background(), open)
	if !result.CanComplete || !result.RequiresAcknowledgement {
		t.Fatalf("open network preflight = %#v", result)
	}
	found := false
	for _, check := range result.Checks {
		if check.ID == "network_scope" && check.Status == "warning" {
			found = true
		}
	}
	if !found {
		t.Fatal("network_scope warning missing")
	}
}

func TestCompleteSetupRequiresWarningAcknowledgement(t *testing.T) {
	repository := &setupRepository{}
	snapshot := &setupSnapshot{}
	service := New(Options{ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, repository, &setupCaddy{}, Dependencies{Snapshot: snapshot, SetupProbe: setupProbe{resolver: false, dns: false}})
	request := SetupRequest{Username: "admin", Password: "a-secure-password", Settings: validSetupSettings()}
	if _, err := service.CompleteSetup(context.Background(), request); err == nil || err.(*domain.AppError).Code != "setup_warning_confirmation_required" {
		t.Fatalf("unacknowledged completion error = %v", err)
	}
	if repository.completed || snapshot.written {
		t.Fatal("setup wrote state before warning acknowledgement")
	}
	request.AcknowledgeWarnings = true
	if _, err := service.CompleteSetup(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if !repository.completed || !snapshot.written {
		t.Fatal("acknowledged setup did not persist")
	}
}
