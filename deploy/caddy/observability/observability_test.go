package observability

import (
	"encoding/json"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/prometheus/client_golang/prometheus"
)

func TestMiddlewareMetricsAndWriteBeforeRedaction(t *testing.T) {
	a := newApp(prometheus.NewRegistry())
	a.registry.MustRegister(a.duration, a.firstByte, a.requestBytes, a.responseBytes, a.requests)
	a.AccessLogs = true
	m := Middleware{ServiceID: "stable-id", Hostname: "app.example.test", app: a}
	req := httptest.NewRequest("POST", "https://app.example.test/api/secret-token?token=secret", strings.NewReader("hello"))
	req.RemoteAddr = "192.168.1.123:1234"
	req.Header.Set("Authorization", "secret")
	rec := httptest.NewRecorder()
	err := m.ServeHTTP(rec, req, caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		b := make([]byte, 8)
		r.Body.Read(b)
		w.WriteHeader(201)
		w.Write([]byte("OK"))
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	e := <-a.queue
	raw, _ := json.Marshal(e)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "192.168.1.123") || e.Path != "/api/*" || e.IP != "192.168.1.0/24" || e.RequestBytes != 5 || e.ResponseBytes != 2 {
		t.Fatalf("unsanitized or inaccurate: %s", raw)
	}
	f, _ := a.registry.Gather()
	found := false
	for _, v := range f {
		if v.GetName() == "caddy_admin_duration_seconds" {
			found = true
			if v.Metric[0].Histogram.GetSampleCount() != 1 {
				t.Fatal("request not counted")
			}
		}
	}
	if !found {
		t.Fatal("missing histogram")
	}
}
func TestUnknownHostDoesNotCreateSeries(t *testing.T) {
	a := newApp(prometheus.NewRegistry())
	a.registry.MustRegister(a.duration, a.firstByte, a.requestBytes, a.responseBytes, a.requests)
	m := Middleware{ServiceID: "id", Hostname: "app.example.test", app: a}
	for i := 0; i < 20; i++ {
		r := httptest.NewRequest("GET", "https://attacker.example/", nil)
		m.ServeHTTP(httptest.NewRecorder(), r, caddyhttp.HandlerFunc(func(http.ResponseWriter, *http.Request) error { return nil }))
	}
	f, _ := a.registry.Gather()
	for _, v := range f {
		if strings.HasPrefix(v.GetName(), "caddy_admin_") && v.GetName() != "caddy_admin_observation_epoch" {
			t.Fatal("unknown host made a series")
		}
	}
}
func TestRedactionAndErrorClassification(t *testing.T) {
	for _, p := range []string{"/secret", "/api/token", "/health?pass=foo", "/login/secret"} {
		if strings.Contains(redactPath(p), "secret") || strings.Contains(redactPath(p), "token") || strings.Contains(redactPath(p), "foo") {
			t.Fatal("path not redacted")
		}
	}
	if redactIP("[2001:db8:abcd:1234::1]:99") != "2001:db8:abcd::/48" {
		t.Fatal("IPv6 redaction")
	}
	if classify("ACME HTTP 429 too many requests token=secret") != "acme_rate_limit" || classify("CAA forbids issuance") != "caa" {
		t.Fatal("classification")
	}
}
func TestLogQueueNeverBlocksAndSpoolIsBounded(t *testing.T) {
	a := newApp(prometheus.NewRegistry())
	a.registry.MustRegister(a.duration, a.firstByte, a.requestBytes, a.responseBytes, a.requests)
	a.AccessLogs = true
	a.LogFile = t.TempDir() + "/access.jsonl"
	a.startWriter()
	for i := 0; i < 10000; i++ {
		a.record(Event{Time: time.Now().UTC().Format(time.RFC3339Nano), Kind: "access", Path: "/<redacted>"})
	}
	a.stopWriter()
	if a.dropped.Load() == 0 {
		t.Fatal("expected bounded queue overflow")
	}
	if len(a.logs) > 2000 {
		t.Fatal("unbounded log ring")
	}
}

func TestFirstCursorReportsTruncatedRing(t *testing.T) {
	a := newApp(prometheus.NewRegistry())
	a.AccessLogs = true
	a.available.Store(true)
	a.logs = []Event{{Sequence: 100, Epoch: a.epoch, Kind: "access"}}
	old := active.Swap(a)
	defer active.Store(old)
	for _, query := range []string{"", "?epoch=" + a.epoch + "&after=0", "?epoch=" + a.epoch + "&after=99"} {
		w := httptest.NewRecorder()
		if err := serveLogs(w, httptest.NewRequest("GET", "http://localhost/logs"+query, nil)); err != nil {
			t.Fatal(err)
		}
		var batch struct {
			Gap bool `json:"gap"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &batch); err != nil {
			t.Fatal(err)
		}
		want := !strings.Contains(query, "after=99")
		if batch.Gap != want {
			t.Fatalf("query=%s gap=%v", query, batch.Gap)
		}
	}
}

func TestSpoolRotationPermissionsAndDiskFailure(t *testing.T) {
	path := t.TempDir() + "/access.jsonl"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(16 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	a := newApp(prometheus.NewRegistry())
	a.AccessLogs = true
	a.LogFile = path
	a.startWriter()
	a.record(Event{Kind: "access", Time: time.Now().UTC().Format(time.RFC3339Nano), Path: "/<redacted>"})
	a.stopWriter()
	for _, p := range []string{path, path + ".1"} {
		info, err := os.Stat(p)
		if err != nil || info.Size() > 16<<20 || info.Mode().Perm() != 0600 {
			t.Fatalf("spool %s: %v %v", p, info, err)
		}
	}
	if len(a.logs) != 1 {
		t.Fatal("rotated event missing")
	}
	unavailable := newApp(prometheus.NewRegistry())
	unavailable.AccessLogs = true
	unavailable.LogFile = path + "/invalid/access.jsonl"
	unavailable.startWriter()
	unavailable.record(Event{Kind: "access"})
	unavailable.stopWriter()
	if unavailable.available.Load() || unavailable.dropped.Load() != 1 {
		t.Fatal("disk failure not reported")
	}
}
func TestSafeEncoderDiscardsContextAndRawFields(t *testing.T) {
	e := &SafeEncoder{Encoder: zapcore.NewJSONEncoder(zapcore.EncoderConfig{})}
	e.AddString("password", "secret-context")
	b, err := e.Clone().EncodeEntry(zapcore.Entry{Message: "ACME 429 secret-token", Level: zapcore.WarnLevel, LoggerName: "secret-logger", Stack: "secret-stack", Time: time.Now()}, []zapcore.Field{zap.String("authorization", "secret-field")})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Free()
	if strings.Contains(b.String(), "secret") || strings.Contains(b.String(), "authorization") || !strings.Contains(b.String(), "acme_rate_limit") {
		t.Fatal(b.String())
	}
}
