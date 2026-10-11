package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/adapter/gormstore"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestManagedSettingsMutationsRequireAuthenticationAndCSRF(t *testing.T) {
	for _, route := range []struct{ method, path string }{{"PUT", "/settings"}, {"POST", "/settings/console/complete"}, {"POST", "/settings/dns/preview"}, {"POST", "/settings/dns/confirm"}} {
		for _, authenticated := range []bool{false, true} {
			request := httptest.NewRequest(route.method, "/api/v1"+route.path, strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "https://console.example.test")
			if authenticated {
				request.AddCookie(&http.Cookie{Name: "__Host-session", Value: "session"})
			}
			response := httptest.NewRecorder()
			New(&configurationApplication{}, Options{Origin: "https://console.example.test"}).Handler().ServeHTTP(response, request)
			want := 401
			if authenticated {
				want = 403
			}
			if response.Code != want {
				t.Fatalf("%s authenticated=%v status=%d", route.path, authenticated, response.Code)
			}
		}
	}
}

func TestPublicConsoleLoginAndExplicitSourceRestriction(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		t.Run(map[bool]string{false: "default unrestricted", true: "explicit trusted networks"}[restricted], func(t *testing.T) {
			ctx := context.Background()
			settings := domain.ManagedSettings{Domains: []domain.ManagedDomain{{ID: "home", Name: "example.com"}}, AdminDomain: "caddyadmin.example.com", Origin: "https://caddyadmin.example.com", Resolvers: []string{"1.1.1.1"}, ConsoleLANOnly: restricted}
			if restricted {
				settings.LAN = []string{"10.0.0.0/8"}
			}
			store, err := gormstore.Open(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			if err = store.CompleteSetup(ctx, application.SetupCredentials{Username: "admin", Password: "test-password"}, settings, domain.CertificateStatus{Mode: domain.CertificateModeBootstrapInternal, ActivationStatus: "idle", PublicStatus: "unknown"}); err != nil {
				t.Fatal(err)
			}
			service := application.New(application.Options{RuntimePolicy: settings, ProbeAddress: "127.0.0.1:443"}, store, nil, application.Dependencies{})
			api := New(service, Options{Origin: settings.Origin})
			for _, source := range []string{"203.0.113.9:4567", "10.0.0.6:4567"} {
				request := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"test-password"}`))
				request.RemoteAddr = source
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Origin", settings.Origin)
				request.Header.Set("X-Forwarded-For", "10.0.0.7")
				request.Header.Set(domain.ClientAddressHeader, "10.0.0.7")
				response := httptest.NewRecorder()
				api.Handler().ServeHTTP(response, request)
				want := 200
				if restricted && strings.HasPrefix(source, "203.") {
					want = 403
				}
				if response.Code != want {
					t.Fatalf("source=%s status=%d body=%s", source, response.Code, response.Body)
				}
				if want == 200 {
					cookies := response.Result().Cookies()
					if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
						t.Fatal("authentication cookie protection lost")
					}
				}
				portalRequest := httptest.NewRequest("GET", "/api/v1/portal", nil)
				portalRequest.RemoteAddr = source
				portalRequest.Header.Set("X-Forwarded-For", "10.0.0.7")
				portalRequest.Header.Set(domain.ClientAddressHeader, "10.0.0.7")
				portalResponse := httptest.NewRecorder()
				api.Handler().ServeHTTP(portalResponse, portalRequest)
				if portalResponse.Code != want {
					t.Fatalf("anonymous portal source=%s status=%d body=%s", source, portalResponse.Code, portalResponse.Body)
				}
				staticResponse := httptest.NewRecorder()
				staticRequest := httptest.NewRequest("GET", "/", nil)
				staticRequest.RemoteAddr = source
				staticRequest.Header.Set(domain.ClientAddressHeader, "10.0.0.7")
				api.ControlHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })).ServeHTTP(staticResponse, staticRequest)
				if staticResponse.Code != want {
					t.Fatalf("static source=%s status=%d", source, staticResponse.Code)
				}
			}
		})
	}
}
