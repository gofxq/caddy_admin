package httpapi

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestManagementRouteMatrix(t *testing.T) {
	engine, ok := New(nil, Options{}).Handler().(*gin.Engine)
	if !ok {
		t.Fatal("management API is not backed by Gin")
	}
	want := map[string]bool{
		"POST /api/v1/auth/login": true, "GET /api/v1/auth/session": true,
		"POST /api/v1/auth/logout": true, "POST /api/v1/auth/password": true,
		"GET /api/v1/overview": true, "GET /api/v1/services": true,
		"POST /api/v1/services": true, "PUT /api/v1/services/:id": true,
		"DELETE /api/v1/services/:id": true, "GET /api/v1/draft/preview": true,
		"GET /api/v1/draft/revisions/:revision": true, "POST /api/v1/draft/validate": true,
		"POST /api/v1/deployments": true, "GET /api/v1/deployments": true,
		"GET /api/v1/deployments/:id": true, "GET /api/v1/audit": true,
		"GET /api/v1/settings": true, "POST /api/v1/settings/cloudflare": true,
		"GET /api/v1/certificates":         true,
		"GET /api/v1/configuration/export": true, "POST /api/v1/configuration/preview": true, "POST /api/v1/configuration/import": true,
	}
	for _, route := range engine.Routes() {
		delete(want, route.Method+" "+route.Path)
	}
	for route := range want {
		t.Errorf("missing route %s", route)
	}
}
