package observability

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

type Middleware struct {
	ServiceID string `json:"service_id"`
	Hostname  string `json:"hostname"`
	app       *App
}

func (Middleware) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.admin_observe", New: func() caddy.Module { return new(Middleware) }}
}
func (m *Middleware) Provision(ctx caddy.Context) error {
	a, err := ctx.App("caddy_admin_observability")
	if err != nil {
		return err
	}
	m.app = a.(*App)
	return nil
}
func (m *Middleware) Validate() error {
	if len(m.ServiceID) == 0 || len(m.ServiceID) > 128 || len(m.Hostname) == 0 || len(m.Hostname) > 253 {
		return fmt.Errorf("invalid fixed observation labels")
	}
	return nil
}

type countingBody struct {
	io.ReadCloser
	n int64
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	b.n += int64(n)
	return n, e
}
func (m *Middleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	host := r.Host
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	if !strings.EqualFold(host, m.Hostname) {
		return next.ServeHTTP(w, r)
	}
	start := time.Now()
	first := 0.0
	rec := caddyhttp.NewResponseRecorder(w, nil, func(status int, _ http.Header) bool {
		if status >= 200 && first == 0 {
			first = time.Since(start).Seconds()
		}
		return false
	})
	var body *countingBody
	if r.Body != nil {
		body = &countingBody{ReadCloser: r.Body}
		r.Body = body
	}
	err := next.ServeHTTP(rec, r)
	status := rec.Status()
	if status == 0 {
		status = 200
	}
	if err != nil && rec.Status() == 0 {
		status = 500
		if h, ok := err.(caddyhttp.HandlerError); ok && h.StatusCode >= 100 && h.StatusCode <= 599 {
			status = h.StatusCode
		}
	}
	elapsed := time.Since(start).Seconds()
	if first == 0 {
		first = elapsed
	}
	input := int64(0)
	if body != nil {
		input = body.n
	}
	if m.app.Metrics {
		labels := []string{m.ServiceID, m.Hostname}
		m.app.duration.WithLabelValues(labels...).Observe(elapsed)
		m.app.firstByte.WithLabelValues(labels...).Observe(first)
		m.app.requestBytes.WithLabelValues(labels...).Add(float64(input))
		m.app.responseBytes.WithLabelValues(labels...).Add(float64(rec.Size()))
		class := status / 100
		if class < 1 || class > 5 {
			class = 5
		}
		m.app.requests.WithLabelValues(m.ServiceID, m.Hostname, fmt.Sprintf("%dxx", class)).Inc()
	}
	m.app.record(Event{Time: time.Now().UTC().Format(time.RFC3339Nano), Kind: "access", ServiceID: m.ServiceID, Hostname: m.Hostname, Method: normalizeMethod(r.Method), Status: status, Path: redactPath(r.URL.Path), IP: redactIP(r.RemoteAddr), Duration: elapsed, RequestBytes: input, ResponseBytes: int64(rec.Size())})
	return err
}
func normalizeMethod(m string) string {
	switch m {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE":
		return m
	default:
		return "OTHER"
	}
}
func redactPath(p string) string {
	first := strings.Split(strings.TrimPrefix(p, "/"), "/")[0]
	switch first {
	case "":
		return "/"
	case "api", "assets", "static", "health", "login", "auth", "public":
		return "/" + first + "/*"
	default:
		return "/<redacted>"
	}
}
func redactIP(value string) string {
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		host = value
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	ip = ip.Unmap()
	bits := 48
	if ip.Is4() {
		bits = 24
	}
	return netip.PrefixFrom(ip, bits).Masked().String()
}
