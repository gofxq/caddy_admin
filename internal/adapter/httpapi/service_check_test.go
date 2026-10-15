package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type checkAPIApplication struct {
	detailAPIApplication
	checked int
}

func (a *checkAPIApplication) CheckService(_ context.Context, revision int64, value domain.Service) (application.ServiceConfigurationCheck, error) {
	a.checked++
	return application.ServiceConfigurationCheck{Revision: revision, Service: value}, nil
}
func TestServiceConfigurationCheckProtectsInputAndPermissions(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		authenticated, mustChange bool
		origin, csrf, body        string
		want                      int
	}{
		{"valid", true, false, "https://admin.example.test", "csrf", `{"revision":3,"service":{"scheme":"h2c","advanced":{"dial_timeout":"5s"}}}`, 200},
		{"anonymous", false, false, "https://admin.example.test", "csrf", `{}`, 401},
		{"change password", true, true, "https://admin.example.test", "csrf", `{}`, 403},
		{"csrf", true, false, "https://admin.example.test", "", `{}`, 403},
		{"origin", true, false, "https://evil.example.test", "csrf", `{}`, 403},
		{"unknown advanced", true, false, "https://admin.example.test", "csrf", `{"revision":3,"service":{"advanced":{"tls_insecure_skip_verify":true}}}`, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &checkAPIApplication{detailAPIApplication: detailAPIApplication{mustChange: tc.mustChange}}
			req := detailAPIRequest("POST", "/services/check", tc.body, tc.authenticated)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-CSRF-Token", tc.csrf)
			w := httptest.NewRecorder()
			New(a, Options{Origin: "https://admin.example.test"}).Handler().ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.want != 200 && a.checked != 0 {
				t.Fatal("rejected check reached application")
			}
		})
	}
}
