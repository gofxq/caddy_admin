package caddy

import (
	"os"
	"strings"
	"testing"
)

func TestAdaptHTTPRouteCommand(t *testing.T) {
	binary := os.Getenv("CADDY_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("set CADDY_INTEGRATION_BINARY")
	}
	client := NewClient(Options{CaddyBinary: binary})
	raw, err := client.AdaptHTTPRoute(t.Context(), "respond /hello ok")
	if err != nil || !strings.Contains(string(raw), `"path":["/hello"]`) {
		t.Fatalf("routes %s, error %v", raw, err)
	}
	_, err = client.AdaptHTTPRoute(t.Context(), "route {\nimport /private/token\n}")
	if err == nil || !strings.Contains(err.Error(), "第 2 行") || strings.Contains(err.Error(), "/private/token") {
		t.Fatalf("diagnostic %v", err)
	}
}
