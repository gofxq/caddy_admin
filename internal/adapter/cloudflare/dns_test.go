package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gofxq/caddy_admin/internal/application"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const testToken = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func fixture(t *testing.T, records *[]record, admin *[]record, writes *int, lost bool) *DNS {
	t.Helper()
	d := New()
	d.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.cloudflare.com" {
			t.Fatal("unexpected API target", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Fatal("missing authorization")
		}
		w := httptest.NewRecorder()
		var result any
		if r.URL.Path == "/client/v4/zones" {
			if r.URL.Query().Get("name") == "example.com" {
				result = []map[string]string{{"id": "zone", "name": "example.com"}}
			} else {
				result = []any{}
			}
		} else if r.Method == "GET" {
			if r.URL.Query().Get("name.exact") == "" {
				t.Fatal("DNS API query lacks exact name filter")
			}
			if r.URL.Query().Get("name.exact") == "caddyadmin.h.example.com" {
				result = *admin
			} else {
				result = *records
			}
			rows := append([]record(nil), result.([]record)...)
			for i := range rows {
				if rows[i].Name == "" {
					rows[i].Name = r.URL.Query().Get("name.exact")
				}
			}
			result = rows
		} else {
			*writes++
			var body record
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			body.ID = "record"
			*records = []record{body}
			if lost {
				return nil, errors.New("lost response " + testToken)
			}
			result = body
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
		return w.Result(), nil
	})
	return d
}
func TestDNSPreviewAndApplyCreateReuseAndRecoverLostResponse(t *testing.T) {
	for _, lost := range []bool{false, true} {
		records := []record{}
		admin := []record{}
		writes := 0
		d := fixture(t, &records, &admin, &writes, lost)
		p, err := d.Preview(context.Background(), testToken, "h.example.com", "192.168.1.6")
		if err != nil {
			t.Fatal(err)
		}
		if p.Action != "create" || p.Name != "*.h.example.com" || p.ZoneID != "zone" || p.Type != "A" || p.Fingerprint == "" || writes != 0 {
			t.Fatalf("preview=%+v writes=%d", p, writes)
		}
		raw, _ := json.Marshal(p)
		if strings.Contains(string(raw), testToken) {
			t.Fatal("leaked token")
		}
		if err = d.Apply(context.Background(), testToken, p); err != nil {
			t.Fatal(err)
		}
		if writes != 1 || records[0].Proxied || records[0].Content != "192.168.1.6" {
			t.Fatal(records, writes)
		}
		if err = d.Apply(context.Background(), testToken, p); err != nil {
			t.Fatal(err)
		}
		if writes != 1 {
			t.Fatal("retry duplicated DNS write")
		}
	}
}
func TestDNSPreviewUpdatesWithConflictProtection(t *testing.T) {
	records := []record{{ID: "record", Type: "A", Name: "*.h.example.com", Content: "192.168.1.8", Proxied: true}}
	admin := []record{}
	writes := 0
	d := fixture(t, &records, &admin, &writes, false)
	p, err := d.Preview(context.Background(), testToken, "h.example.com", "192.168.1.6")
	if err != nil || p.Action != "update" || p.OldAddress != "192.168.1.8" {
		t.Fatal(p, err)
	}
	records[0].Content = "192.168.1.9"
	if err = d.Apply(context.Background(), testToken, p); err == nil || writes != 0 {
		t.Fatal("stale preview wrote DNS", err, writes)
	}
	p, err = d.Preview(context.Background(), testToken, "h.example.com", "192.168.1.6")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Apply(context.Background(), testToken, p); err != nil || writes != 1 {
		t.Fatal(err, writes)
	}
}
func TestDNSPreviewRejectsConflictingRecordsAndUnsafeAddresses(t *testing.T) {
	for _, tc := range []struct {
		records, admin []record
		address        string
	}{
		{records: []record{{Type: "CNAME", Name: "*.h.example.com", Content: "elsewhere.example.com"}}, address: "192.168.1.6"},
		{records: []record{{Type: "A"}, {Type: "A"}}, address: "192.168.1.6"},
		{admin: []record{{Type: "A", Content: "192.168.1.9"}}, address: "192.168.1.6"},
		{address: "127.0.0.1"}, {address: "0.0.0.0"}, {address: "169.254.1.2"}, {address: "203.0.113.6"},
	} {
		writes := 0
		d := fixture(t, &tc.records, &tc.admin, &writes, false)
		if _, err := d.Preview(context.Background(), testToken, "h.example.com", tc.address); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
		if writes != 0 {
			t.Fatal("preview wrote DNS")
		}
	}
}
func TestDNSPreviewIPv6AndErrorRedaction(t *testing.T) {
	records := []record{}
	admin := []record{}
	writes := 0
	d := fixture(t, &records, &admin, &writes, false)
	p, err := d.Preview(context.Background(), testToken, "h.example.com", "fd12::6")
	if err != nil || p.Type != "AAAA" {
		t.Fatal(p, err)
	}
	d.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"success":false,"errors":[{"message":"` + testToken + `"}]}`)), Header: http.Header{}}, nil
	})
	_, err = d.Preview(context.Background(), testToken, "h.example.com", "192.168.1.6")
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatal("missing or unredacted error", err)
	}
	var _ application.SetupDNS = d
}

func TestDNSPreviewRejectsExactNamesWithoutMatchingAddress(t *testing.T) {
	for _, kind := range []string{"TXT", "MX", "NS"} {
		records := []record{}
		admin := []record{{Type: kind, Name: "caddyadmin.h.example.com", Content: "other"}}
		writes := 0
		d := fixture(t, &records, &admin, &writes, false)
		if _, err := d.Preview(context.Background(), testToken, "h.example.com", "192.168.1.6"); err == nil {
			t.Fatalf("exact %s record incorrectly accepted", kind)
		}
	}
}
func TestDNSApplyDoesNotReuseWhenUnconfirmedOtherRecordsChange(t *testing.T) {
	records := []record{{ID: "record", Type: "A", Name: "*.h.example.com", Content: "192.168.1.6"}}
	admin := []record{}
	writes := 0
	d := fixture(t, &records, &admin, &writes, false)
	p, err := d.Preview(context.Background(), testToken, "h.example.com", "192.168.1.6")
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, record{ID: "other", Type: "AAAA", Name: "*.h.example.com", Content: "fd12::9"})
	if err = d.Apply(context.Background(), testToken, p); err == nil {
		t.Fatal("reused DNS despite an unconfirmed new AAAA record")
	}
	if writes != 0 {
		t.Fatal("wrote despite conflict")
	}
}

func TestDNSNeverFollowsRedirectsOrReturnsTransportCredentials(t *testing.T) {
	d := New()
	calls := 0
	d.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.cloudflare.com" {
			t.Fatal("followed redirect with credentials")
		}
		return &http.Response{StatusCode: 307, Body: io.NopCloser(strings.NewReader(`{"success":false}`)), Header: http.Header{"Location": []string{"https://untrusted.example/"}}, Request: r}, nil
	})
	if _, err := d.Preview(context.Background(), testToken, "h.example.com", "192.168.1.6"); err == nil || calls != 1 {
		t.Fatal("redirect was accepted", err, calls)
	}
	d.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("failure with " + testToken) })
	_, err := d.Preview(context.Background(), testToken, "h.example.com", "192.168.1.6")
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatal("transport error leaked token", err)
	}
}

func TestDNSPreviewRejectsRecordsOutsideTheExactName(t *testing.T) {
	records := []record{{ID: "other", Name: "unrelated.example.com", Type: "A", Content: "192.168.1.8"}}
	admin := []record{}
	writes := 0
	d := fixture(t, &records, &admin, &writes, false)
	if _, err := d.Preview(context.Background(), testToken, "h.example.com", "192.168.1.6"); err == nil {
		t.Fatal("accepted unrelated record from DNS API")
	}
	if writes != 0 {
		t.Fatal("preview wrote DNS")
	}
}
