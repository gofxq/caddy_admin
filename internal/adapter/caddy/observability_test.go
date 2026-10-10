package caddy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestObservationMetricsParseAndRejectBadCounters(t *testing.T) {
	text := `# TYPE caddy_admin_observation_epoch gauge
caddy_admin_observation_epoch{epoch="e"} 1
# TYPE caddy_admin_duration_seconds histogram
caddy_admin_duration_seconds_bucket{service_id="stable",host="app.test",le="1"} 3
caddy_admin_duration_seconds_bucket{service_id="stable",host="app.test",le="+Inf"} 3
caddy_admin_duration_seconds_sum{service_id="stable",host="app.test"} 1.5
caddy_admin_duration_seconds_count{service_id="stable",host="app.test"} 3
`
	snapshot, err := ParseMetrics([]byte(text))
	if err != nil || snapshot.Epoch != "e" || len(snapshot.Series) != 3 {
		t.Fatal(snapshot, err)
	}
	if _, err = ParseMetrics([]byte(strings.ReplaceAll(text, " 3", " NaN"))); err == nil {
		t.Fatal("NaN accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "https://evil.test/", 302) }))
	defer server.Close()
	client := NewClient(Options{AdminURL: server.URL})
	if _, err = client.Metrics(context.Background()); err == nil {
		t.Fatal("redirect accepted")
	}
}
