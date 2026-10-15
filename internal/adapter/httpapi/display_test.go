package httpapi

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestConfigDisplayRetainsStructureAndRedactsSecrets(t *testing.T) {
	raw := json.RawMessage(`{"admin":{"listen":"unix//run/caddy/admin.sock","config":{"persist":false},"remote":{"opaque":"admin-secret"}},"storage":{"module":"custom","bucket":"storage-secret","nested":{"opaque":"nested-secret"}},"apps":{"http":{"servers":{"main":{"routes":[{"handle":[{"handler":"headers","request":{"set":{"Authorization":["header-secret"],"X-App":["photos"]}}},{"handler":"authentication","providers":{"http_basic":{"accounts":[{"username":"admin","password":"hash-secret"}]}}}]}]}}},"tls":{"automation":{"policies":[{"issuers":[{"module":"acme","challenges":{"dns":{"provider":{"name":"cloudflare","api_token":"dns-secret"}}}}]}]},"certificates":{"load_pem":[{"certificate":"public-cert","key":"pem-secret"}]}},"custom_app":{"opaque":"app-secret"}},"logging":{"logs":{"default":{"level":"INFO"}}}}`)
	before := string(raw)
	got, fields, err := configForDisplay(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"admin-secret", "storage-secret", "nested-secret", "header-secret", "hash-secret", "dns-secret", "pem-secret", "app-secret"} {
		if strings.Contains(string(got), secret) {
			t.Fatalf("secret leaked: %s", secret)
		}
	}
	for _, visible := range []string{`"admin"`, `"storage"`, `"remote"`, `"opaque":"[REDACTED]"`, `"listen":"unix//run/caddy/admin.sock"`, `"persist":false`, `"module":"custom"`, `"X-App":["photos"]`, `"level":"INFO"`} {
		if !strings.Contains(string(got), visible) {
			t.Errorf("missing visible structure %s: %s", visible, got)
		}
	}
	if len(fields) < 8 || string(raw) != before {
		t.Fatal("missing redaction paths or mutated source")
	}
	if !slices.IsSorted(fields) {
		t.Fatal("redaction paths must be deterministic")
	}
}

func TestConfigDisplayPreservesSafeFileStorageAndNumbers(t *testing.T) {
	raw := json.RawMessage(`{"admin":{"listen":"localhost:2019","origins":["https://admin.example.com"]},"storage":{"module":"file_system","root":"/data/caddy"},"apps":{"http":{"http_port":9007199254740993}}}`)
	got, fields, err := configForDisplay(raw)
	if err != nil || len(fields) != 0 || domain.Fingerprint(got) != domain.Fingerprint(raw) {
		t.Fatalf("safe config changed: %s %v %v", got, fields, err)
	}
}

func TestConfigDisplayRejectsInvalidRoot(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `"config"`, `{`, `{} {}`} {
		if _, _, err := configForDisplay(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid config %s", raw)
		}
	}
}

func TestPreviewDisplayRedactsEveryConfigurationAndKeepsFullHashes(t *testing.T) {
	raw := json.RawMessage(`{"admin":{"listen":"unix//run/caddy/admin.sock","credential":"test-private"},"storage":{"module":"file_system","root":"/data"},"apps":{"tls":{"api_token":"test-token"}}}`)
	p := domain.Preview{Config: raw, RuntimeConfig: raw, RollbackConfig: raw, Hash: domain.Fingerprint(raw), RuntimeHash: domain.Fingerprint(raw), RollbackHash: domain.Fingerprint(raw)}
	result, err := previewForDisplay(p)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "test-private") || strings.Contains(string(body), "test-token") {
		t.Fatalf("preview leaked: %s", body)
	}
	if len(result.ConfigRedactedFields) == 0 || len(result.RuntimeConfigRedactedFields) == 0 || len(result.RollbackConfigRedactedFields) == 0 {
		t.Fatal("missing per-config redaction paths")
	}
	if result.Hash != domain.Fingerprint(raw) || result.RuntimeHash != result.Hash || result.RollbackHash != result.Hash || domain.Fingerprint(result.Config) == result.Hash {
		t.Fatal("display changed full configuration fingerprints")
	}
	for _, field := range []string{`"runtime_config":`, `"config":`, `"rollback_config":`} {
		if !strings.Contains(string(body), field) {
			t.Fatalf("missing %s", field)
		}
	}
	if !strings.Contains(string(p.RuntimeConfig), "test-private") {
		t.Fatal("redaction mutated application config")
	}
}

func TestConfigDisplayRedactsOpaqueSelectorValuesAndACMEKeys(t *testing.T) {
	raw := json.RawMessage(`{"admin":{"remote":{"module":"opaque-admin-secret"}},"storage":{"module":"custom","format":"direct-selector-secret","protocol":{"value":"non-string-selector-secret"},"options":{"handler":"opaque-handler-secret","module":"opaque-module-secret","protocol":"opaque-protocol-secret","format":"opaque-format-secret"}},"apps":{"tls":{"automation":{"policies":[{"issuers":[{"module":"acme","external_account":{"key_id":"public-id","mac_key":"eab-key-secret"}}]}]}}}}`)
	got, _, err := configForDisplay(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"opaque-admin-secret", "opaque-handler-secret", "opaque-module-secret", "opaque-protocol-secret", "opaque-format-secret", "eab-key-secret", "direct-selector-secret", "non-string-selector-secret"} {
		if strings.Contains(string(got), secret) {
			t.Errorf("secret leaked: %s", secret)
		}
	}
	if !strings.Contains(string(got), `"module":"custom"`) || !strings.Contains(string(got), `"module":"acme"`) {
		t.Fatal("lost module identifiers")
	}
}

func TestConfigDisplayRedactsUnknownModuleMapsAndInlineModules(t *testing.T) {
	raw := json.RawMessage(`{"apps":{"tls":{"certificates":{"custom_loader":{"value":"loader-secret","nested":{"format":"nested-selector-secret"}}},"automation":{"policies":[{"get_certificate":[{"via":"custom_manager","value":"manager-secret"}]}]}},"http":{"servers":{"main":{"routes":[{"match":[{"custom_matcher":{"value":"matcher-secret"},"host":["photos.example.com"]}],"handle":[{"handler":"encode","encodings":{"custom_encoder":{"value":"encoder-secret"},"gzip":{}}}]}]}}}},"logging":{"logs":{"default":{"writer":{"output":"custom_writer","value":"writer-secret"},"encoder":{"format":"filter","fields":{"secret_field":{"filter":"custom_filter","value":"filter-secret"}}}}}}}`)
	got, _, err := configForDisplay(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"loader-secret", "nested-selector-secret", "manager-secret", "matcher-secret", "encoder-secret", "writer-secret", "filter-secret"} {
		if strings.Contains(string(got), secret) {
			t.Errorf("secret leaked: %s", secret)
		}
	}
	if !strings.Contains(string(got), `"host":["photos.example.com"]`) || !strings.Contains(string(got), `"custom_loader"`) {
		t.Fatal("lost safe host or unknown module structure")
	}
}

func TestConfigDisplayEscapesPathsAndHidesCredentialValues(t *testing.T) {
	raw := json.RawMessage(`{"apps":{"http":{"servers":{"edge/~":{"routes":[{"handle":[{"handler":"static_response","body":"-----BEGIN PRIVATE KEY-----test-only-----END PRIVATE KEY-----"}]}]}}},"tls":{"automation":{"policies":[{"issuers":[{"module":"acme","ca":"https://user:test-password@ca.example.com/directory?token=test-query-token"}]}]}}}}`)
	got, fields, err := configForDisplay(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE KEY", "test-password", "test-query-token"} {
		if strings.Contains(string(got), secret) {
			t.Errorf("secret leaked: %s", secret)
		}
	}
	want := []string{"/apps/http/servers/edge~1~0/routes/0/handle/0/body", "/apps/tls/automation/policies/0/issuers/0/ca"}
	if !slices.Equal(fields, want) {
		t.Fatalf("paths=%v want=%v", fields, want)
	}
}

func TestConfigDisplayRedactsUnknownUpstreamCAPool(t *testing.T) {
	raw := json.RawMessage(`{"apps":{"http":{"servers":{"main":{"routes":[{"handle":[{"handler":"reverse_proxy","transport":{"protocol":"http","tls":{"server_name":"photos.example.com","ca":{"provider":"custom","value":"ca-module-private","options":{"format":"ca-selector-private"}}}}}]}]}}}}}`)
	got, _, err := configForDisplay(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "ca-module-private") || strings.Contains(string(got), "ca-selector-private") {
		t.Fatalf("unknown CA provider parameters leaked: %s", got)
	}
	if !strings.Contains(string(got), `"server_name":"photos.example.com"`) {
		t.Fatal("safe TLS setting hidden")
	}
}
