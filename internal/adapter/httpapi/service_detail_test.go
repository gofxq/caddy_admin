package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type detailAPIApplication struct {
	Application
	mustChange bool
	calls      int
	id, hash   string
}

func (a *detailAPIApplication) Session(context.Context, string) (domain.Session, error) {
	return domain.Session{Username: "admin", CSRF: "csrf", MustChange: a.mustChange}, nil
}
func (a *detailAPIApplication) ServiceDetail(_ context.Context, id string) (domain.ServiceDetail, error) {
	a.id = id
	if id == "missing" {
		return domain.ServiceDetail{}, domain.NotFound("服务不存在")
	}
	return domain.ServiceDetail{ID: id, Runtime: domain.RuntimeCheck{Status: "unknown"}, RecentDeployments: []domain.ServiceRelease{}}, nil
}
func (a *detailAPIApplication) CheckUpstream(_ context.Context, id, hash, deploymentID string) (domain.UpstreamCheck, error) {
	a.id = id
	a.hash = hash
	if deploymentID != "release-1" {
		return domain.UpstreamCheck{}, domain.Conflict("发布标识过期")
	}
	a.calls++
	return domain.UpstreamCheck{Status: "reachable", Vantage: "manager"}, nil
}
func detailAPIRequest(method, path, body string, authenticated bool) *http.Request {
	request := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://admin.example.test")
	request.Header.Set("X-CSRF-Token", "csrf")
	if authenticated {
		request.AddCookie(&http.Cookie{Name: "__Host-session", Value: "token"})
	}
	return request
}
func TestServiceDetailAPIProtectsReadAndManualCheck(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body  string
		authenticated, mustChange bool
		origin, csrf              string
		want                      int
	}{
		{"detail", "GET", "/services/photos", "", true, false, "https://admin.example.test", "csrf", 200},
		{"missing", "GET", "/services/missing", "", true, false, "https://admin.example.test", "csrf", 404},
		{"anonymous read", "GET", "/services/photos", "", false, false, "https://admin.example.test", "csrf", 401},
		{"change password read", "GET", "/services/photos", "", true, true, "https://admin.example.test", "csrf", 403},
		{"check", "POST", "/services/photos/check-upstream", `{"expected_hash":"verified","expected_deployment_id":"release-1"}`, true, false, "https://admin.example.test", "csrf", 200},
		{"anonymous check", "POST", "/services/photos/check-upstream", `{"expected_hash":"verified","expected_deployment_id":"release-1"}`, false, false, "https://admin.example.test", "csrf", 401},
		{"change password check", "POST", "/services/photos/check-upstream", `{"expected_hash":"verified","expected_deployment_id":"release-1"}`, true, true, "https://admin.example.test", "csrf", 403},
		{"wrong origin", "POST", "/services/photos/check-upstream", `{"expected_hash":"verified","expected_deployment_id":"release-1"}`, true, false, "https://evil.example.test", "csrf", 403},
		{"no csrf", "POST", "/services/photos/check-upstream", `{"expected_hash":"verified","expected_deployment_id":"release-1"}`, true, false, "https://admin.example.test", "", 403},
		{"arbitrary target", "POST", "/services/photos/check-upstream", `{"expected_hash":"verified","expected_deployment_id":"release-1","target":"127.0.0.1:2019"}`, true, false, "https://admin.example.test", "csrf", 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &detailAPIApplication{mustChange: tc.mustChange}
			request := detailAPIRequest(tc.method, tc.path, tc.body, tc.authenticated)
			request.Header.Set("Origin", tc.origin)
			request.Header.Set("X-CSRF-Token", tc.csrf)
			response := httptest.NewRecorder()
			New(a, Options{Origin: "https://admin.example.test"}).Handler().ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
			if tc.want != 200 && a.calls != 0 {
				t.Fatal("unauthorized/invalid target probed")
			}
			if tc.name == "check" {
				if a.id != "photos" || a.hash != "verified" {
					t.Fatalf("id=%s hash=%s", a.id, a.hash)
				}
				var result domain.UpstreamCheck
				if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Status != "reachable" {
					t.Fatal(response.Body.String())
				}
			}
		})
	}
}
