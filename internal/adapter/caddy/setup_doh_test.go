package caddy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type dohTransport func(*http.Request) (*http.Response, error)

func (f dohTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDoHProbeChecksBothFamiliesAndWildcardWithoutCredentials(t *testing.T) {
	for _, scenario := range []string{"match", "CNAME", "truncated", "wrong-address", "extra-AAAA", "unrelated-answer", "wrong-question", "NXDOMAIN", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int32
			probe := &DoHProbe{Client: &http.Client{Transport: dohTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if r.URL.Host != "cloudflare-dns.com" || r.URL.Scheme != "https" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Accept") != "application/dns-json" {
					t.Error("unsafe DoH request")
				}
				if scenario == "timeout" {
					return nil, errors.New("transport with secret")
				}
				name := r.URL.Query().Get("name")
				kind := 1
				if r.URL.Query().Get("type") == "AAAA" {
					kind = 28
				}
				if !strings.HasPrefix(name, "caddyadmin.") && !strings.HasPrefix(name, "setup-check-") {
					t.Error("unexpected probe name")
				}
				question := name
				if scenario == "wrong-question" {
					question = "other.example."
				}
				answers := []map[string]any{}
				if kind == 1 {
					address := "10.0.0.6"
					owner := name
					if scenario == "wrong-address" {
						address = "10.0.0.7"
					}
					if scenario == "unrelated-answer" {
						owner = "other.example."
					}
					if scenario == "CNAME" {
						owner = "target.example."
						answers = append(answers, map[string]any{"name": name, "type": 5, "data": owner})
					}
					answers = append(answers, map[string]any{"name": owner, "type": kind, "data": address})
				}
				if kind == 28 && scenario == "extra-AAAA" {
					answers = append(answers, map[string]any{"name": name, "type": kind, "data": "fd00::7"})
				}
				status := 0
				if scenario == "NXDOMAIN" {
					status = 3
					answers = nil
				}
				raw, _ := json.Marshal(map[string]any{"TC": scenario == "truncated", "Status": status, "Question": []map[string]any{{"name": question, "type": kind}}, "Answer": answers})
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})}}
			report, err := probe.Check(context.Background(), "home.example.com", "10.0.0.6")
			if err != nil || report.Verified != (scenario == "match" || scenario == "CNAME") || len(report.Queries) != 2 {
				t.Fatal(report, err)
			}
			if scenario == "match" && calls.Load() != 4 {
				t.Fatal("did not query both families")
			}
			raw, _ := json.Marshal(report)
			if strings.Contains(string(raw), "transport with secret") {
				t.Fatal("transport error leaked")
			}
		})
	}
}

func TestDoHProbeDoesNotFollowRedirects(t *testing.T) {
	var calls atomic.Int32
	probe := &DoHProbe{Client: &http.Client{Transport: dohTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Host != "cloudflare-dns.com" {
			t.Error("redirect followed")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://private.example/"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}}
	report, err := probe.Check(context.Background(), "home.example.com", "10.0.0.6")
	if err != nil || report.Verified || calls.Load() != 4 {
		t.Fatal(report, err, calls.Load())
	}
}
