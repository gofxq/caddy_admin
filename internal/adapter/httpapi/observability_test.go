package httpapi

import (
	"context"
	"github.com/gofxq/caddy_admin/internal/domain"
	"net/http/httptest"
	"testing"
)

type observationAPIApplication struct {
	detailAPIApplication
	calls int
}

func (a *observationAPIApplication) ObservationStatus(context.Context) domain.ObservationStatus {
	return domain.ObservationStatus{State: "ready"}
}
func (a *observationAPIApplication) Traffic(context.Context, domain.TrafficQuery) (domain.Traffic, error) {
	a.calls++
	return domain.Traffic{Points: []domain.TrafficPoint{}}, nil
}
func (a *observationAPIApplication) AccessLogs(context.Context, domain.LogQuery) ([]domain.AccessEvent, error) {
	a.calls++
	return []domain.AccessEvent{}, nil
}
func (a *observationAPIApplication) Alerts(context.Context) ([]domain.Alert, error) {
	return []domain.Alert{}, nil
}
func (a *observationAPIApplication) AcknowledgeAlert(context.Context, string, string) error {
	a.calls++
	return nil
}
func (a *observationAPIApplication) Diagnose(context.Context, string) (domain.Diagnostics, error) {
	a.calls++
	return domain.Diagnostics{}, nil
}
func TestObservationAPIAuthQueryAndCSRF(t *testing.T) {
	for _, tc := range []struct {
		path, method, body string
		auth               bool
		csrf               string
		want               int
	}{
		{"/traffic", "GET", "", false, "csrf", 401}, {"/traffic?from=garbage", "GET", "", true, "csrf", 422}, {"/traffic?target=127.0.0.1", "GET", "", true, "csrf", 422}, {"/traffic", "GET", "", true, "csrf", 200}, {"/logs?limit=10001", "GET", "", true, "csrf", 422}, {"/logs?offset=bad", "GET", "", true, "csrf", 422}, {"/diagnostics", "POST", `{"domain_id":"d","target":"localhost"}`, true, "csrf", 422}, {"/diagnostics", "POST", `{"domain_id":"d"}`, true, "", 403}, {"/diagnostics", "POST", `{"domain_id":"d"}`, true, "csrf", 200}, {"/alerts/a/acknowledge", "POST", `{}`, true, "", 403}, {"/alerts/a/acknowledge", "POST", `{}`, true, "csrf", 200},
	} {
		a := &observationAPIApplication{}
		r := detailAPIRequest(tc.method, tc.path, tc.body, tc.auth)
		r.Header.Set("X-CSRF-Token", tc.csrf)
		w := httptest.NewRecorder()
		New(a, Options{Origin: "https://admin.example.test"}).Handler().ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body)
		}
		if tc.want != 200 && a.calls != 0 {
			t.Fatal("invalid query invoked usecase")
		}
	}
}
