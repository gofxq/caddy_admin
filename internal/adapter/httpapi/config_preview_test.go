package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type previewAPIApplication struct {
	detailAPIApplication
	unavailable bool
}

func (a *previewAPIApplication) Preview(context.Context, string) (domain.Preview, error) {
	if a.unavailable {
		return domain.Preview{}, &domain.AppError{Status: 503, Code: "unavailable", Message: "Caddy 不可达，请重试"}
	}
	raw := json.RawMessage(`{"admin":{"listen":"unix//run/caddy/admin.sock"},"storage":{"module":"custom","opaque":"runtime-private"},"apps":{"http":{"servers":{"main":{"listen":[":8443"]}}}}}`)
	return domain.Preview{Config: json.RawMessage(`{"apps":{}}`), RuntimeConfig: raw, RuntimeHash: domain.Fingerprint(raw), RuntimeReadAt: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}, nil
}
func (a *previewAPIApplication) Validate(ctx context.Context, _ int64, _, _ string) (domain.Preview, error) {
	return a.Preview(ctx, "")
}

func TestRuntimeConfigurationPreviewRequiresAuthenticationAndRedactsResponses(t *testing.T) {
	for _, tc := range []struct {
		name, method, path            string
		auth, mustChange, unavailable bool
		want                          int
	}{
		{"preview", "GET", "/draft/preview", true, false, false, 200},
		{"rollback preview", "GET", "/draft/preview?rollback=history", true, false, false, 200},
		{"validate", "POST", "/draft/validate", true, false, false, 200},
		{"anonymous preview", "GET", "/draft/preview", false, false, false, 401},
		{"password change", "GET", "/draft/preview", true, true, false, 403},
		{"offline", "GET", "/draft/preview", true, false, true, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &previewAPIApplication{detailAPIApplication: detailAPIApplication{mustChange: tc.mustChange}, unavailable: tc.unavailable}
			req := detailAPIRequest(tc.method, tc.path, `{"revision":1}`, tc.auth)
			response := httptest.NewRecorder()
			New(a, Options{Origin: "https://admin.example.test"}).Handler().ServeHTTP(response, req)
			body := response.Body.String()
			if response.Code != tc.want {
				t.Fatalf("status=%d body=%s", response.Code, body)
			}
			if strings.Contains(body, "runtime-private") {
				t.Fatal("runtime credential leaked")
			}
			if tc.want == 200 {
				if response.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("config may be cached")
				}
				var result struct {
					RuntimeConfig map[string]any `json:"runtime_config"`
					RuntimeHash   string         `json:"runtime_hash"`
					RuntimeReadAt string         `json:"runtime_read_at"`
					Fields        []string       `json:"runtime_config_redacted_fields"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.RuntimeConfig["admin"] == nil || result.RuntimeConfig["storage"] == nil || result.RuntimeHash == "" || result.RuntimeReadAt != "2026-10-15T12:00:00Z" || len(result.Fields) == 0 {
					t.Fatalf("missing config preview data: %s", body)
				}
			} else if strings.Contains(body, "runtime_config") {
				t.Fatal("failed request displayed a substitute runtime config")
			}
		})
	}
}
