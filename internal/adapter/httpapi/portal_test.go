package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type portalApplication struct {
	Application
	restricted  bool
	unavailable bool
}

func (a *portalApplication) ClientAddress(_ context.Context, remote, forwarded string) string {
	return remote
}
func (a *portalApplication) ControlAccess(_ context.Context, address string) error {
	if a.restricted && address != "10.1.2.3:1234" {
		return &domain.AppError{Status: 403, Code: "network_restricted", Message: "仅限可信网络"}
	}
	return nil
}
func (a *portalApplication) Portal(_ context.Context, address string) ([]domain.PortalService, error) {
	if a.unavailable {
		return nil, errors.New("private database error")
	}
	return []domain.PortalService{{Name: "照片库", Hostname: "photos.example.com", URL: "https://photos.example.com"}}, nil
}
func TestPortalAnonymousReadRespectsNetworkRestriction(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, source string
		restricted, unavailable    bool
		want                       int
	}{
		{"anonymous", "GET", "/portal", "203.0.113.1:1234", false, false, 200},
		{"restricted", "GET", "/portal", "203.0.113.1:1234", true, false, 403},
		{"trusted", "GET", "/portal", "10.1.2.3:1234", true, false, 200},
		{"failure", "GET", "/portal", "10.1.2.3:1234", false, true, 503},
		{"read only", "POST", "/portal", "10.1.2.3:1234", false, false, 405},
		{"protected services", "GET", "/services", "10.1.2.3:1234", false, false, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &portalApplication{restricted: tc.restricted, unavailable: tc.unavailable}
			req := httptest.NewRequest(tc.method, "/api/v1"+tc.path, nil)
			req.RemoteAddr = tc.source
			req.Header.Set("X-Forwarded-For", "10.1.2.3")
			req.Header.Set(domain.ClientAddressHeader, "10.1.2.3")
			w := httptest.NewRecorder()
			New(a, Options{}).Handler().ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if tc.want == 200 && !strings.Contains(w.Body.String(), `"services":[`) {
				t.Fatal(w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private database error") {
				t.Fatal("internal error leaked")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable directory")
			}
		})
	}
}
