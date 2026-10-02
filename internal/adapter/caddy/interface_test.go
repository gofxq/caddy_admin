package caddy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/application"
)

func TestClientImplementsCaddyPort(t *testing.T) {
	var _ application.CaddyPort = (*Client)(nil)
	var _ application.SnapshotStore = (*Snapshot)(nil)
	var _ application.CertificateProbe = (*Probe)(nil)
	var _ application.SecretStore = (*SecretFile)(nil)
}

func TestExternalClientRejectsRedirectAndInvalidTLS(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("untrusted or redirected target reached")
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	for _, address := range []string{target.URL, redirect.URL} {
		client := NewClient(Options{AdminURL: address})
		if _, err := client.Read(context.Background()); err == nil {
			t.Fatalf("unsafe read accepted for %s", address)
		}
		if err := client.Load(context.Background(), []byte(`{}`)); err == nil {
			t.Fatalf("unsafe load accepted for %s", address)
		}
	}
}

func TestUnixClientNeverUsesProxy(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "admin.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"apps":{}}`)
	})}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()

	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	client := NewClient(Options{Socket: socket})
	if raw, err := client.Read(context.Background()); err != nil || string(raw) != `{"apps":{}}` {
		t.Fatalf("Read() = %q, %v", raw, err)
	}
}

func TestValidationNeverReturnsRawModuleOutput(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-caddy")
	secret := "private-module-output"
	script := "#!/bin/sh\nprintf '%s\\n' '" + secret + "' >&2\nexit 1\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	client := NewClient(Options{CaddyBinary: binary, DataDir: dir})
	err := client.Validate(context.Background(), []byte(`{"apps":{}}`))
	if err == nil {
		t.Fatal("validation unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), secret) || err.Error() != "Caddy 配置校验失败，请检查模块、部署密钥及上游配置" {
		t.Fatalf("unsafe validation error: %q", err)
	}
}
