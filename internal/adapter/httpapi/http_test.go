package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestStrictJSONBinding(t *testing.T) {
	type requestBody struct {
		Name string `json:"name"`
	}
	engine := newEngine()
	engine.POST("/bind", func(c *gin.Context) {
		body, ok := bindJSON[requestBody](c)
		if ok {
			respondJSON(c, http.StatusOK, body)
		}
	})
	tests := []struct {
		name, contentType, body string
		want                    int
	}{
		{"valid", "application/json", `{"name":"ok"}`, 200},
		{"content type", "text/plain", `{"name":"no"}`, 422},
		{"unknown field", "application/json", `{"name":"no","extra":true}`, 422},
		{"multiple values", "application/json", `{"name":"no"}{}`, 422},
		{"empty", "application/json", ``, 422},
		{"oversized", "application/json", `{"name":"` + strings.Repeat("x", 65<<10) + `"}`, 422},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/bind", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestRecoveryRedactsPanicAndSetsRequestID(t *testing.T) {
	var logs bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(original)
	engine := newEngine()
	engine.GET("/panic", func(*gin.Context) { panic("private-token-password") })
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("X-Request-ID") == "" {
		t.Fatalf("status=%d headers=%v", response.Code, response.Header())
	}
	if strings.Contains(response.Body.String(), "private-token") || strings.Contains(logs.String(), "private-token") {
		t.Fatal("panic detail leaked")
	}
}

type publishApplication struct {
	Application
	written <-chan struct{}
	applied chan<- string
}

func (*publishApplication) Session(context.Context, string) (domain.Session, error) {
	return domain.Session{Username: "admin", CSRF: "csrf"}, nil
}
func (*publishApplication) Begin(context.Context, domain.PublishRequest, string) (domain.Deployment, error) {
	return domain.Deployment{ID: "deployment-1", Revision: 7, Status: "applying"}, nil
}
func (application *publishApplication) Apply(id string) {
	select {
	case <-application.written:
		application.applied <- id
	case <-time.After(time.Second):
		application.applied <- "apply-before-response"
	}
}

type signalWriter struct {
	*httptest.ResponseRecorder
	once sync.Once
	done chan struct{}
}

func (writer *signalWriter) Write(raw []byte) (int, error) {
	count, err := writer.ResponseRecorder.Write(raw)
	writer.once.Do(func() { close(writer.done) })
	return count, err
}

func TestPublishWritesAcceptedResponseBeforeDetachedApply(t *testing.T) {
	written := make(chan struct{})
	applied := make(chan string, 1)
	application := &publishApplication{written: written, applied: applied}
	api := New(application, Options{Origin: "https://admin.example.test"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/deployments", strings.NewReader(`{"validation_id":"validation","revision":7,"expected_hash":"hash","idempotency_key":"0123456789abcdef"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://admin.example.test")
	request.Header.Set("X-CSRF-Token", "csrf")
	request.AddCookie(&http.Cookie{Name: "__Host-session", Value: "token"})
	response := &signalWriter{ResponseRecorder: httptest.NewRecorder(), done: written}
	api.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"id":"deployment-1"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	select {
	case id := <-applied:
		if id != "deployment-1" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("detached Apply was not started")
	}
}

type createApplication struct {
	Application
	received domain.Service
}

func (*createApplication) Session(context.Context, string) (domain.Session, error) {
	return domain.Session{Username: "admin", CSRF: "csrf"}, nil
}
func (application *createApplication) SaveService(_ context.Context, _ int64, service domain.Service, _ bool, _ string) (domain.Draft, error) {
	application.received = service
	return domain.Draft{Revision: 1, Services: []domain.Service{}}, nil
}

func TestCreateServiceIgnoresClientSuppliedID(t *testing.T) {
	application := &createApplication{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/services", strings.NewReader(`{"revision":0,"service":{"id":"client-controlled","name":"Photos"}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://admin.example.test")
	request.Header.Set("X-CSRF-Token", "csrf")
	request.AddCookie(&http.Cookie{Name: "__Host-session", Value: "token"})
	response := httptest.NewRecorder()
	New(application, Options{Origin: "https://admin.example.test"}).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || application.received.ID != "" {
		t.Fatalf("status=%d received ID=%q", response.Code, application.received.ID)
	}
}

type setupApplication struct {
	SetupApplication
	calls int
}

func (*setupApplication) SetupStatus(context.Context) (application.SetupStatus, error) {
	return application.SetupStatus{Resolvers: []string{"10.0.0.53"}}, nil
}
func (*setupApplication) PreflightSetup(context.Context, application.SetupSettings) application.SetupPreflight {
	return application.SetupPreflight{CanComplete: true, Checks: []application.SetupCheck{{ID: "settings_valid", Status: "pass"}}}
}
func (fake *setupApplication) CompleteSetup(context.Context, application.SetupRequest) (string, error) {
	fake.calls++
	return "https://caddyadmin.home.example.test", nil
}

func TestSetupPreflightAndStatusExposeTCPClientOnly(t *testing.T) {
	handler := NewSetup(&setupApplication{}, SetupOptions{}, nil)
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil)
	statusRequest.RemoteAddr = "10.20.30.40:4567"
	statusRequest.Header.Set("X-Forwarded-For", "203.0.113.9")
	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"client_ip":"10.20.30.40"`) || !strings.Contains(statusResponse.Body.String(), `"resolver_suggestions":["10.0.0.53"]`) {
		t.Fatalf("status=%d body=%s", statusResponse.Code, statusResponse.Body.String())
	}

	preflightRequest := httptest.NewRequest(http.MethodPost, "/api/v1/setup/preflight", strings.NewReader(`{"settings":{"homelab_domain":"home.example.test"}}`))
	preflightRequest.Host = "10.20.30.1:8082"
	preflightRequest.RemoteAddr = "10.20.30.40:4567"
	preflightRequest.Header.Set("Origin", "https://10.20.30.1:8082")
	preflightRequest.Header.Set("Content-Type", "application/json")
	preflightResponse := httptest.NewRecorder()
	handler.ServeHTTP(preflightResponse, preflightRequest)
	if preflightResponse.Code != http.StatusOK || !strings.Contains(preflightResponse.Body.String(), `"settings_valid"`) {
		t.Fatalf("preflight=%d body=%s", preflightResponse.Code, preflightResponse.Body.String())
	}
}

func TestSetupRequiresExactPrivateHTTPSOrigin(t *testing.T) {
	fake := &setupApplication{}
	handler := NewSetup(fake, SetupOptions{}, nil)
	body := `{"username":"admin","password":"a-secure-password","settings":{"homelab_domain":"home.example.test","lan_cidrs":[],"upstream_cidrs":[],"allowed_names":[],"denied_ips":[],"resolvers":[]}}`
	for _, test := range []struct {
		origin string
		want   int
	}{
		{"https://evil.example", http.StatusForbidden},
		{"http://127.0.0.1:8080", http.StatusForbidden},
		{"https://127.0.0.1:8080", http.StatusAccepted},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/setup/complete", strings.NewReader(body))
		request.Host = "127.0.0.1:8080"
		request.RemoteAddr = "127.0.0.1:1234"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", test.origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("origin=%s status=%d body=%s", test.origin, response.Code, response.Body.String())
		}
	}
	if fake.calls != 1 {
		t.Fatalf("CompleteSetup calls=%d", fake.calls)
	}
}

func TestWebHandlerServesSPAAndKeepsAPIErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("app-shell"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app.js"), []byte("script"), 0600); err != nil {
		t.Fatal(err)
	}
	api := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { http.Error(writer, "api-error", 404) })
	handler := WebHandler(root, api)
	for _, test := range []struct {
		path, body string
		status     int
	}{
		{"/", "app-shell", 200}, {"/services", "app-shell", 200}, {"/assets/app.js", "script", 200},
		{"/assets/missing.js", "404", 404}, {"/api/v1/missing", "api-error", 404}, {"/../secret", "404", 404},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.body) {
			t.Fatalf("%s: %d %s", test.path, response.Code, response.Body.String())
		}
	}
}

type settingsApplication struct{ Application }

func (*settingsApplication) Session(context.Context, string) (domain.Session, error) {
	return domain.Session{Username: "admin"}, nil
}
func (*settingsApplication) BuildInfo(context.Context) (string, bool) { return "test", true }
func (*settingsApplication) CertificateState(context.Context) (domain.CertificateStatus, error) {
	return domain.CertificateStatus{}, nil
}
func (*settingsApplication) TokenConfigured() bool { return false }

func TestSettingsExposeOnlyExternalCaddyMode(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	request.AddCookie(&http.Cookie{Name: "__Host-session", Value: "token"})
	response := httptest.NewRecorder()
	New(&settingsApplication{}, Options{ExternalCaddy: true, RuntimeConfig: map[string]string{"origin": "https://admin.example.test"}}).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"external_caddy":true`) || strings.Contains(response.Body.String(), "admin_url") {
		t.Fatalf("settings=%d %s", response.Code, response.Body.String())
	}
}

type handoffApplication struct{ status application.SetupHandoff }

func (fake *handoffApplication) SetupHandoff(context.Context) application.SetupHandoff {
	return fake.status
}

func TestHandoffRestrictsPeerAndHostAndOmitsSensitiveDetails(t *testing.T) {
	fake := &handoffApplication{status: application.SetupHandoff{Initialized: true, Mode: "embedded", AdminOrigin: "https://caddyadmin.home.example.test", ManagerStatus: "ready", DNSStatus: "pending", ConsoleStatus: "pending", TemporaryEntry: true, CheckedAt: "2026-10-01T00:00:00Z"}}
	handler := NewHandoff(fake, HandoffOptions{}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/setup/handoff", nil)
	request.Host = "10.0.0.2:8082"
	request.RemoteAddr = "10.0.0.3:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.8")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), `"console_status":"pending"`) {
		t.Fatalf("handoff=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	for _, secret := range []string{"admin_url", "probe_address", "resolved_ips", "last_error", "token"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("handoff exposed %q: %s", secret, response.Body.String())
		}
	}

	for _, test := range []struct{ host, remote string }{{"console.example.test", "10.0.0.3:1234"}, {"10.0.0.2:8082", "203.0.113.8:1234"}} {
		denied := httptest.NewRequest(http.MethodGet, "/api/v1/setup/handoff", nil)
		denied.Host, denied.RemoteAddr = test.host, test.remote
		deniedResponse := httptest.NewRecorder()
		handler.ServeHTTP(deniedResponse, denied)
		if deniedResponse.Code != http.StatusForbidden {
			t.Fatalf("host=%q remote=%q status=%d", test.host, test.remote, deniedResponse.Code)
		}
	}
}

func TestHandoffAllowsExplicitConfiguredLANSource(t *testing.T) {
	fake := &handoffApplication{status: application.SetupHandoff{ConsoleStatus: "pending"}}
	handler := NewHandoff(fake, HandoffOptions{LAN: []string{"203.0.113.0/24"}}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/setup/handoff", nil)
	request.Host, request.RemoteAddr = "10.0.0.2:8082", "203.0.113.8:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("configured LAN status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHandoffSignalsOnlyAfterReadyWasReturned(t *testing.T) {
	ready := make(chan struct{}, 1)
	fake := &handoffApplication{status: application.SetupHandoff{ConsoleStatus: "ready"}}
	handler := NewHandoff(fake, HandoffOptions{}, func() { ready <- struct{}{} })
	request := httptest.NewRequest(http.MethodGet, "/api/v1/setup/handoff", nil)
	request.Host, request.RemoteAddr = "localhost:8082", "127.0.0.1:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	select {
	case <-ready:
	default:
		t.Fatal("ready response did not signal lifecycle")
	}
}

func TestErrorEnvelopeContract(t *testing.T) {
	engine := newEngine()
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing", nil))
	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != 404 || body.Error.Code != "not_found" || body.Error.Message != "接口不存在" || body.Error.RequestID == "" {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
}
