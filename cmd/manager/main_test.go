package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	caddyadapter "github.com/gofxq/caddy_admin/internal/adapter/caddy"
	"github.com/gofxq/caddy_admin/internal/adapter/httpapi"
	"github.com/gofxq/caddy_admin/internal/config"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestInternalTemporaryEntryUsesOnlyLoopbackBridge(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	t.Setenv("SETUP_LISTEN", occupied.Addr().String())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("handoff page"), 0600); err != nil {
		t.Fatal(err)
	}
	c := config.Config{DataDir: dir, StaticRoot: dir, LAN: []string{"192.168.1.0/24"}}
	server, err := startTemporaryEntry(c, httpapi.New(nil, httpapi.Options{}), nil)
	if err != nil {
		t.Fatalf("internal handoff depended on an independent Setup port: %v", err)
	}
	defer server.Close()
	client := &http.Client{Timeout: time.Second}
	request, _ := http.NewRequest("GET", "http://127.0.0.1:8083/", nil)
	request.Host = "192.168.1.10"
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("bridge accepted request without Caddy client address: %d", response.StatusCode)
	}
	request.Header.Set(domain.ClientAddressHeader, "192.168.1.20")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || string(body) != "handoff page" {
		t.Fatalf("bridge did not serve handoff page: %d %q %v", response.StatusCode, body, err)
	}
}

func TestManagerCommandSurface(t *testing.T) {
	for _, command := range []string{"run", "serve", "health", "reset-password", "history", "seed-examples", "check-secret"} {
		if !knownManagerCommand(command) {
			t.Errorf("supported command %q was rejected", command)
		}
	}
	for _, command := range []string{"init", "migrate-run-root", "restore-maintenance", "backup", "rescue", "migrate-settings", "setup-state", "setup-token"} {
		if knownManagerCommand(command) {
			t.Errorf("removed command %q is still accepted", command)
		}
	}
}

func TestSetupAccessAllowsPublicAndPrivatePeersWithoutSourceFilter(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := setupAccessHandler(next)

	request := httptest.NewRequest(http.MethodGet, "https://10.23.45.10:8087/", nil)
	request.RemoteAddr = "10.23.45.20:42000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTemporaryRedirect || response.Header().Get("Location") != "/setup" {
		t.Fatalf("private setup redirect = %d %q", response.Code, response.Header().Get("Location"))
	}

	request = httptest.NewRequest(http.MethodGet, "https://10.23.45.10:8087/api/v1/setup/status", nil)
	request.RemoteAddr = "10.23.45.20:42000"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("API request was redirected: %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "https://10.23.45.10:8087/setup", nil)
	request.RemoteAddr = "203.0.113.8:42000"
	request.Header.Set("X-Forwarded-For", "10.23.45.20")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("public peer could not open Setup: %d", response.Code)
	}
}

func TestHTTPSetupAcceptsPublicPrivateAndHostnameRequests(t *testing.T) {
	handler := setupAccessHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, test := range []struct {
		remote, host, method string
		want                 int
	}{
		{"192.168.1.20:1234", "192.168.1.10", "GET", 204},
		{"192.168.1.20:1234", "setup.example.com", "GET", 204},
		{"203.0.113.20:1234", "192.168.1.10", "GET", 204},
		{"203.0.113.20:1234", "203.0.113.10", "POST", 204},
		{"[2001:db8::20]:1234", "setup.example.com", "GET", 204},
		{"192.168.1.20:1234", "192.168.1.10", "POST", 204},
	} {
		request := httptest.NewRequest(test.method, "http://"+test.host+"/setup", nil)
		request.RemoteAddr = test.remote
		request.Header.Set("X-Forwarded-For", "192.168.1.20")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("%+v = %d", test, response.Code)
		}
	}
}

func TestTemporaryBridgeOnlyTrustsLoopbackCaddyClientHeader(t *testing.T) {
	handler := temporaryBridgeHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, r.RemoteAddr) }))
	for _, test := range []struct {
		peer, client string
		want         int
	}{
		{"127.0.0.1:5000", "192.168.1.20", 200},
		{"192.168.1.20:5000", "127.0.0.1", 403},
		{"127.0.0.1:5000", "not-an-address", 403},
	} {
		request := httptest.NewRequest("GET", "http://192.168.1.10/setup", nil)
		request.RemoteAddr = test.peer
		request.Header.Set(domain.ClientAddressHeader, test.client)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("%+v = %d", test, response.Code)
		}
		if response.Code == 200 && response.Body.String() != "192.168.1.20:0" {
			t.Fatal(response.Body.String())
		}
	}
}

func TestTemporaryEntryKeepsHandoffAvailableOutsideConfiguredLAN(t *testing.T) {
	handoff := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	web := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	handler, err := temporaryEntryHandler(config.Config{LAN: []string{"10.0.0.0/8"}}, web, handoff)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "https://192.168.1.10/api/v1/setup/handoff", nil)
	request.RemoteAddr = "192.168.1.20:42000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("private handoff outside configured LAN = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "https://192.168.1.10/api/v1/auth/session", nil)
	request.RemoteAddr = "192.168.1.20:42000"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("temporary route did not delegate dynamic access checks: %d", response.Code)
	}
}

func TestTemporaryEntryGraceStartsAfterReadyWasRead(t *testing.T) {
	ready := make(chan struct{})
	closed := make(chan struct{})
	go closeTemporaryEntryAfterReady(ready, 40*time.Millisecond, func() { close(closed) })
	select {
	case <-closed:
		t.Fatal("entry closed before ready was read")
	case <-time.After(50 * time.Millisecond):
	}
	close(ready)
	select {
	case <-closed:
		t.Fatal("entry closed without grace period")
	case <-time.After(10 * time.Millisecond):
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("entry did not close after grace period")
	}
}

func TestSetupHTTPSListenerServesEncryptedHTTP(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "setup.crt")
	keyPath := filepath.Join(dir, "setup.key")
	if err := caddyadapter.EnsureBootstrapCertificate(certPath, keyPath, "https://127.0.0.1:8080"); err != nil {
		t.Fatal(err)
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	server := newSetupHTTPServer("127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "setup-over-tls") }))
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skip("sandbox does not permit loopback listeners")
		}
		t.Fatal(err)
	}
	server.Addr = listener.Addr().String()
	done := make(chan error, 1)
	go func() { done <- server.ServeTLS(listener, certPath, keyPath) }()
	t.Cleanup(func() {
		_ = server.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("setup TLS server did not stop")
		}
	})
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	resp, err := client.Get("https://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "setup-over-tls" {
		t.Fatalf("HTTPS setup response = %q, %v", body, err)
	}
}

func TestSetupShutdownDrainsStandardTLSListenerBeforeReturning(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	standard := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(409)
		_, _ = io.WriteString(w, "another request initialized the instance")
	}))
	defer standard.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer redirect.Close()
	response := make(chan error, 1)
	go func() {
		r, err := standard.Client().Post(standard.URL+"/api/v1/setup/complete", "application/json", strings.NewReader("{}"))
		if err == nil {
			if r.StatusCode != 409 {
				err = fmt.Errorf("losing setup status = %d", r.StatusCode)
			}
			_ = r.Body.Close()
		}
		response <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	drained := make(chan struct{})
	go func() { shutdownSetupServers(ctx, redirect.Config, standard.Config); close(drained) }()
	select {
	case <-drained:
		t.Fatal("shutdown returned while TLS request was still running")
	case <-time.After(40 * time.Millisecond):
	}
	close(release)
	if err := <-response; err != nil {
		t.Fatal(err)
	}
	select {
	case <-drained:
	case <-ctx.Done():
		t.Fatal("TLS shutdown did not finish")
	}
}

func TestLANOnlyHandlerUsesTCPPeerAndIgnoresForwardedHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler, err := lanOnlyHandler([]string{"192.0.2.0/24"}, next)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, remote, forwarded string
		want                    int
	}{
		{"allowed peer", "192.0.2.19:42000", "203.0.113.8", http.StatusNoContent},
		{"forged forwarded address", "203.0.113.8:42000", "192.0.2.19", http.StatusForbidden},
		{"loopback maintenance", "127.0.0.1:42000", "", http.StatusNoContent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "https://192.0.2.10:9988/", nil)
			r.RemoteAddr = tt.remote
			r.Header.Set("X-Forwarded-For", tt.forwarded)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestRescueEntryRemainsUntilPublicCertificateIsReady(t *testing.T) {
	c := config.Default()
	for _, tt := range []struct {
		name   string
		config config.Config
		status domain.CertificateStatus
		want   bool
	}{
		{"bootstrap mode", c, domain.CertificateStatus{Mode: domain.CertificateModeBootstrapInternal, PublicStatus: "unknown"}, true},
		{"Cloudflare pending", c, domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, PublicStatus: "pending"}, true},
		{"public certificate ready", c, domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, PublicStatus: "ready"}, false},
		{"external Caddy", func() config.Config {
			external := c
			external.AdminURL = "https://caddy.example.test:2019"
			return external
		}(), domain.CertificateStatus{Mode: domain.CertificateModeCloudflare, PublicStatus: "pending"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := rescueEntryEnabled(tt.config, tt.status); got != tt.want {
				t.Fatalf("rescueEntryEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
