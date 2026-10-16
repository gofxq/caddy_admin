package dnspod

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gofxq/caddy_admin/internal/domain"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const testToken = "123456,aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type fixture struct {
	records, admin []record
	writes         int
	lost, delayed  bool
	total          string
	calls          int
}

func (f *fixture) dns(t *testing.T) *DNS {
	t.Helper()
	d := New()
	d.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		f.calls++
		if r.URL.Scheme != "https" || r.URL.Host != "dnsapi.cn" || r.Method != "POST" {
			t.Fatal("unsafe request", r.URL)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("login_token") == "" {
			t.Fatal("missing token")
		}
		out := map[string]any{"status": map[string]string{"code": "1"}}
		switch r.URL.Path {
		case "/Domain.Info":
			if r.Form.Get("domain") != "example.com" {
				out["status"] = map[string]string{"code": "6"}
			} else {
				out["domain"] = map[string]string{"id": "zone", "name": "example.com"}
			}
		case "/Record.List":
			rows := f.records
			if r.Form.Get("sub_domain") == "caddyadmin.h" {
				rows = f.admin
			} else if r.Form.Get("sub_domain") != "*.h" {
				t.Fatal("wrong record query", r.Form)
			}
			if f.delayed && f.writes > 0 {
				rows = nil
			}
			total := strconv.Itoa(len(rows))
			if f.total != "" {
				total = f.total
			}
			offset, _ := strconv.Atoi(r.Form.Get("offset"))
			if offset > len(rows) {
				t.Fatal("invalid offset")
			}
			end := offset + 100
			if end > len(rows) {
				end = len(rows)
			}
			out["records"] = rows[offset:end]
			out["info"] = map[string]string{"record_total": total}
		case "/Record.Create", "/Record.Modify":
			f.writes++
			if r.Form.Get("record_line_id") != "0" || r.Form.Get("sub_domain") != "*.h" {
				t.Fatal("wrong write", r.Form)
			}
			f.records = []record{{ID: "record", Name: r.Form.Get("sub_domain"), Type: r.Form.Get("record_type"), Content: r.Form.Get("value"), LineID: "0", Enabled: "1", TTL: "600"}}
			if f.lost {
				return nil, errors.New("lost " + testToken)
			}
		default:
			t.Fatal("unknown action", r.URL)
		}
		raw, _ := json.Marshal(out)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: http.Header{}, Request: r}, nil
	})
	return d
}
func TestPreviewApplyAndLostResponse(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(strconv.FormatBool(lost), func(t *testing.T) {
			f := &fixture{lost: lost}
			d := f.dns(t)
			p, e := d.Preview(context.Background(), testToken, "h.example.com", "203.0.113.6")
			if e != nil || p.Action != "create" || f.writes != 0 {
				t.Fatal(p, e)
			}
			if e = d.Apply(context.Background(), testToken, p); e != nil {
				t.Fatal(e)
			}
			if e = d.Apply(context.Background(), testToken, p); e != nil || f.writes != 1 {
				t.Fatal("duplicated write", e, f.writes)
			}
			raw, _ := json.Marshal(p)
			if strings.Contains(string(raw), testToken) {
				t.Fatal("token leaked")
			}
		})
	}
}
func rr(kind, value string) record {
	return record{ID: "r", Name: "*.h", Type: kind, Content: value, LineID: "0", Enabled: "1", TTL: "600"}
}
func TestUpdateAndAccountConflict(t *testing.T) {
	f := &fixture{records: []record{rr("A", "203.0.113.9")}}
	d := f.dns(t)
	p, e := d.Preview(context.Background(), testToken, "h.example.com", "203.0.113.6")
	if e != nil || p.Action != "update" {
		t.Fatal(p, e)
	}
	if e = d.Apply(context.Background(), "789101,bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", p); e == nil || f.writes != 0 {
		t.Fatal("changed account accepted", e)
	}
	f.records[0].Content = "203.0.113.8"
	if e = d.Apply(context.Background(), testToken, p); e == nil || f.writes != 0 {
		t.Fatal("changed records accepted", e)
	}
	p, e = d.Preview(context.Background(), testToken, "h.example.com", "203.0.113.6")
	if e != nil {
		t.Fatal(e)
	}
	if e = d.Apply(context.Background(), testToken, p); e != nil || f.writes != 1 {
		t.Fatal(e, f.writes)
	}
}
func TestConflicts(t *testing.T) {
	for _, name := range []string{"cname", "duplicate", "split-line", "disabled", "exact-txt", "exact-address", "unrelated", "incomplete-page", "invalid-token", "unsafe-address"} {
		t.Run(name, func(t *testing.T) {
			f := &fixture{}
			token, address := testToken, "203.0.113.6"
			switch name {
			case "cname":
				f.records = []record{rr("CNAME", "elsewhere.example.com")}
			case "duplicate":
				r := rr("A", address)
				f.records = []record{r, r}
				f.records[1].ID = "other"
			case "split-line":
				r := rr("A", address)
				r.LineID = "10=0"
				f.records = []record{r}
			case "disabled":
				r := rr("A", address)
				r.Enabled = "0"
				f.records = []record{r}
			case "exact-txt", "exact-address":
				r := rr("TXT", "other")
				if name == "exact-address" {
					r.Type = "A"
				}
				r.Name = "caddyadmin.h"
				f.admin = []record{r}
			case "unrelated":
				r := rr("A", address)
				r.Name = "other"
				f.records = []record{r}
			case "incomplete-page":
				f.total = "2"
			case "invalid-token":
				token = "bad"
			case "unsafe-address":
				address = "127.0.0.1"
			}
			if _, e := f.dns(t).Preview(context.Background(), token, "h.example.com", address); e == nil || f.writes != 0 {
				t.Fatal("accepted unsafe preview", e)
			}
		})
	}
}
func TestPaginationAndIPv6(t *testing.T) {
	f := &fixture{}
	for i := 0; i < 101; i++ {
		r := rr("TXT", "value")
		r.ID = strconv.Itoa(i)
		f.records = append(f.records, r)
	}
	p, e := f.dns(t).Preview(context.Background(), testToken, "h.example.com", "2001:db8::6")
	if e != nil || p.Type != "AAAA" || p.Action != "create" {
		t.Fatal(p, e)
	}
}
func TestTransportSafety(t *testing.T) {
	for _, kind := range []string{"redirect", "oversized", "transport", "provider"} {
		t.Run(kind, func(t *testing.T) {
			d := New()
			calls := 0
			d.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "dnsapi.cn" {
					t.Fatal("followed redirect")
				}
				if kind == "transport" {
					return nil, errors.New(testToken)
				}
				status, body := 200, `{"status":{"code":"-1","message":"`+testToken+`"}}`
				header := http.Header{}
				if kind == "redirect" {
					status = 307
					header.Set("Location", "https://unsafe.example/")
				}
				if kind == "oversized" {
					body = strings.Repeat("x", (1<<20)+1)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: header, Request: r}, nil
			})
			_, e := d.Preview(context.Background(), testToken, "h.example.com", "203.0.113.6")
			if e == nil || strings.Contains(e.Error(), testToken) || calls != 1 {
				t.Fatal("unsafe error", e, calls)
			}
		})
	}
}
func TestDelayedLostResponseDoesNotDuplicate(t *testing.T) {
	f := &fixture{lost: true, delayed: true}
	d := f.dns(t)
	p, e := d.Preview(context.Background(), testToken, "h.example.com", "203.0.113.6")
	if e != nil {
		t.Fatal(e)
	}
	if e = d.Apply(context.Background(), testToken, p); e == nil {
		t.Fatal("expected uncertainty")
	}
	if e = d.Apply(context.Background(), testToken, p); e == nil || f.writes != 1 {
		t.Fatal("duplicated uncertain write", e, f.writes)
	}
	f.delayed = false
	if e = d.Apply(context.Background(), testToken, p); e != nil || f.writes != 1 {
		t.Fatal(e, f.writes)
	}
}

func TestPreservesOtherAddressAndBindsExactRecordState(t *testing.T) {
	f := &fixture{records: []record{rr("AAAA", "2001:db8::8")}}
	r := rr("A", "203.0.113.6")
	r.Name = "caddyadmin.h"
	f.admin = []record{r}
	d := f.dns(t)
	p, e := d.Preview(context.Background(), testToken, "h.example.com", "203.0.113.6")
	if e != nil || p.Action != "create" || p.Warning == "" {
		t.Fatal(p, e)
	}
	f.admin[0].TTL = "300"
	if e = d.Apply(context.Background(), testToken, p); e == nil || f.writes != 0 {
		t.Fatal("accepted changed exact override", e)
	}
}
func TestRejectsAmbiguousExactDuplicatesAndSecurityProxy(t *testing.T) {
	for _, mode := range []string{"exact-duplicate", "security-proxy"} {
		t.Run(mode, func(t *testing.T) {
			f := &fixture{}
			r := rr("A", "203.0.113.6")
			if mode == "exact-duplicate" {
				r.Name = "caddyadmin.h"
				f.admin = []record{r, r}
				f.admin[1].ID = "second"
			} else {
				r.UseAQB = "yes"
				f.records = []record{r}
			}
			if _, e := f.dns(t).Preview(context.Background(), testToken, "h.example.com", "203.0.113.6"); e == nil {
				t.Fatal("accepted unsafe records")
			}
		})
	}
}
func TestApplyRejectsChangedProviderAndHasBoundedClient(t *testing.T) {
	f := &fixture{}
	d := f.dns(t)
	if d.client.Timeout <= 0 {
		t.Fatal("unbounded HTTP client")
	}
	p, e := d.Preview(context.Background(), testToken, "h.example.com", "203.0.113.6")
	if e != nil {
		t.Fatal(e)
	}
	p.Provider = "cloudflare"
	if e = d.Apply(context.Background(), testToken, p); e == nil || f.writes != 0 {
		t.Fatal("accepted changed provider", e)
	}
}

type writeRejected interface{ DNSWriteRejected() bool }

func rejected(err error) bool {
	var marker writeRejected
	return errors.As(err, &marker) && marker.DNSWriteRejected()
}
func TestRejectedWriteAllowsRetry(t *testing.T) {
	f := &fixture{}
	d := f.dns(t)
	base := d.client.Transport
	deny := true
	attempts := 0
	d.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/Record.Create" {
			attempts++
			if deny {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":{"code":"7","message":"` + testToken + `"}}`)), Header: http.Header{}, Request: r}, nil
			}
		}
		return base.RoundTrip(r)
	})
	p, e := d.Preview(context.Background(), testToken, "h.example.com", "203.0.113.6")
	if e != nil {
		t.Fatal(e)
	}
	e = d.Apply(context.Background(), testToken, p)
	if !rejected(e) || strings.Contains(e.Error(), testToken) {
		t.Fatal("explicit rejection not marked safely", e)
	}
	deny = false
	if e = d.Apply(context.Background(), testToken, p); e != nil || attempts != 2 || f.writes != 1 {
		t.Fatal("permission fix not retryable", e, attempts, f.writes)
	}
}
func TestUncertainResponsesNeverMarkRejected(t *testing.T) {
	for _, kind := range []string{"transport", "malformed", "unknown", "http-error", "readback-rejection"} {
		t.Run(kind, func(t *testing.T) {
			f := &fixture{}
			d := f.dns(t)
			base := d.client.Transport
			attempts := 0
			dispatched := false
			d.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/Record.Create" {
					attempts++
					dispatched = true
					status, body := 200, `{"status":{"code":"987654"}}`
					switch kind {
					case "transport":
						return nil, errors.New(testToken)
					case "malformed":
						body = `{"status":`
					case "http-error":
						status = 502
						body = `{"status":{"code":"7"}}`
					case "readback-rejection":
						body = `{"status":{"code":"1"}}`
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
				}
				if dispatched && kind == "readback-rejection" {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":{"code":"7"}}`)), Header: http.Header{}, Request: r}, nil
				}
				return base.RoundTrip(r)
			})
			p, e := d.Preview(context.Background(), testToken, "h.example.com", "203.0.113.6")
			if e != nil {
				t.Fatal(e)
			}
			if e = d.Apply(context.Background(), testToken, p); e == nil || rejected(e) {
				t.Fatal("uncertain write marked rejected", e)
			}
			if e = d.Apply(context.Background(), testToken, p); e == nil || rejected(e) || attempts != 1 {
				t.Fatal("uncertain retry cleared guard", e, attempts)
			}
		})
	}
}
func TestPreDispatchFailureMarksRejected(t *testing.T) {
	f := &fixture{}
	d := f.dns(t)
	p, e := d.Preview(context.Background(), testToken, "h.example.com", "203.0.113.6")
	if e != nil {
		t.Fatal(e)
	}
	d.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })
	if e = d.Apply(context.Background(), testToken, p); !rejected(e) {
		t.Fatal("preflight failure should permit retry", e)
	}
	var appErr *domain.AppError
	if !errors.As(e, &appErr) {
		t.Fatal("wrapper lost application error")
	}
}
