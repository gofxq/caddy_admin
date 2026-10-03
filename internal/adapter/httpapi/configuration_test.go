package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type configurationApplication struct {
	Application
	mustChange bool
}

func (app *configurationApplication) Session(context.Context, string) (domain.Session, error) {
	return domain.Session{Username: "admin", CSRF: "csrf", MustChange: app.mustChange}, nil
}
func (*configurationApplication) ExportConfiguration(context.Context) (application.Configuration, error) {
	return application.Configuration{Format: application.ConfigurationFormat, Version: 2, Services: []application.PortableService{}}, nil
}
func (*configurationApplication) PreviewConfiguration(context.Context, application.Configuration) (application.ConfigurationPreview, error) {
	return application.ConfigurationPreview{Revision: 3, Services: []domain.Service{}, Changes: []domain.Change{}}, nil
}
func (*configurationApplication) ImportConfiguration(context.Context, application.Configuration, int64, bool, string) (domain.Draft, error) {
	return domain.Draft{Revision: 4, Services: []domain.Service{}}, nil
}

func TestConfigurationRoutesRequireAuthenticationOriginAndCSRF(t *testing.T) {
	for _, test := range []struct {
		method, path                   string
		auth, csrf, origin, mustChange bool
		want                           int
	}{
		{"GET", "/configuration/export", false, false, false, false, 401},
		{"GET", "/configuration/export", true, false, false, true, 403},
		{"GET", "/configuration/export", true, false, false, false, 200},
		{"POST", "/configuration/preview", true, false, true, false, 403},
		{"POST", "/configuration/import", true, true, false, false, 403},
		{"POST", "/configuration/import", true, true, true, true, 403},
		{"POST", "/configuration/preview", true, true, true, false, 200},
		{"POST", "/configuration/import", true, true, true, false, 200},
	} {
		t.Run(test.method+test.path+strconv.Itoa(test.want), func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/api/v1"+test.path, strings.NewReader(`{"configuration":{"format":"caddy-web-admin","version":2,"settings":{},"services":[]},"revision":3,"confirm":true}`))
			if test.path == "/configuration/preview" {
				request = httptest.NewRequest(test.method, "/api/v1"+test.path, strings.NewReader(`{"configuration":{"format":"caddy-web-admin","version":2,"settings":{},"services":[]}}`))
			}
			request.Header.Set("Content-Type", "application/json")
			if test.auth {
				request.AddCookie(&http.Cookie{Name: "__Host-session", Value: "session"})
			}
			if test.csrf {
				request.Header.Set("X-CSRF-Token", "csrf")
			}
			if test.origin {
				request.Header.Set("Origin", "https://console.example.test")
			}
			response := httptest.NewRecorder()
			New(&configurationApplication{mustChange: test.mustChange}, Options{Origin: "https://console.example.test"}).Handler().ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d want %d: %s", response.Code, test.want, response.Body.String())
			}
			if test.method == "GET" && test.want == 200 && response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("export response may be cached")
			}
		})
	}
}

func TestConfigurationUploadRejectsSecretsAndServerOwnedFields(t *testing.T) {
	for _, extra := range []string{`"token":"secret",`, `"password":"secret",`, `"snapshot":{},`} {
		body := `{"configuration":{"format":"caddy-web-admin","version":2,` + extra + `"settings":{},"services":[]}}`
		request := httptest.NewRequest("POST", "/api/v1/configuration/preview", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://console.example.test")
		request.Header.Set("X-CSRF-Token", "csrf")
		request.AddCookie(&http.Cookie{Name: "__Host-session", Value: "session"})
		response := httptest.NewRecorder()
		New(&configurationApplication{}, Options{Origin: "https://console.example.test"}).Handler().ServeHTTP(response, request)
		if response.Code != 422 || strings.Contains(response.Body.String(), "secret") {
			t.Fatalf("unknown sensitive field accepted or echoed: %d %s", response.Code, response.Body.String())
		}
	}
}
