package domain_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestDomainJSONContractMatchesAPI(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  map[string]any
	}{
		{"service", domain.Service{ID: "service-1", Name: "Photos", Enabled: true, Port: 2283}, map[string]any{"id": "service-1", "name": "Photos", "domain_id": "", "hostname": "", "scheme": "", "host": "", "port": float64(2283), "enabled": true, "notes": "", "dial": "", "updated_at": ""}},
		{"session hides token", domain.Session{Username: "admin", CSRF: "csrf", Token: "secret", Expires: 7}, map[string]any{"username": "admin", "csrf": "csrf", "must_change": false, "expires": float64(7)}},
		{"deployment hides config and idempotency", domain.Deployment{ID: "deploy-1", Config: json.RawMessage(`{"secret":true}`), Idempotency: "key", RequestHash: "hash", Services: []domain.Service{}, Changes: []domain.Change{}}, map[string]any{"id": "deploy-1", "version": float64(0), "revision": float64(0), "status": "", "services": []any{}, "base_hash": "", "hash": "", "actor": "", "created": "", "finished": "", "error": "", "rollback_id": "", "changes": []any{}, "settings": map[string]any{"metrics_enabled": false, "access_logs_enabled": false, "alerts_enabled": false, "upstream_checks_enabled": false, "origin": "", "admin_domain": "", "domains": nil, "console_lan_only": false, "lan_cidrs": nil, "upstream_cidrs": nil, "allowed_names": nil, "denied_ips": nil, "resolvers": nil}}},
		{"certificate status hides fingerprints", domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, BeforeHash: "before", CandidateHash: "candidate"}, map[string]any{"mode": "cloudflare", "activation_status": "", "public_status": "", "last_error_class": "", "updated_at": ""}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("JSON contract changed:\n got %#v\nwant %#v", got, test.want)
			}
		})
	}
}

func TestAppErrorContract(t *testing.T) {
	tests := []struct {
		err     error
		status  int
		code    string
		missing bool
	}{
		{domain.Invalid("bad"), 422, "validation", false},
		{domain.Conflict("changed"), 409, "conflict", false},
		{&domain.AppError{Status: 404, Code: "not_found", Message: "missing"}, 404, "not_found", true},
	}
	for _, test := range tests {
		var appErr *domain.AppError
		if !errors.As(test.err, &appErr) || appErr.Status != test.status || appErr.Code != test.code {
			t.Fatalf("unexpected error contract: %#v", test.err)
		}
		if domain.IsMissing(test.err) != test.missing {
			t.Fatalf("IsMissing(%v) mismatch", test.err)
		}
	}
	if domain.ErrorClass(errors.New("private infrastructure detail")) != "unavailable" {
		t.Fatal("raw infrastructure error was not classified as unavailable")
	}
}

func TestDiffIgnoresUpdatedAt(t *testing.T) {
	before := []domain.Service{{ID: "one", Hostname: "one.home.example", Name: "One", UpdatedAt: "old"}}
	after := []domain.Service{{ID: "one", Hostname: "one.home.example", Name: "One", UpdatedAt: "new"}}
	if changes := domain.Diff(before, after); len(changes) != 0 {
		t.Fatalf("timestamp-only change produced diff: %#v", changes)
	}
}
