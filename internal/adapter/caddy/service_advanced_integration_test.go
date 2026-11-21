package caddy

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func runAdvancedCaddy(t *testing.T, services []domain.Service, root []byte, mounts map[string]string) (*http.Client, string, func([]domain.Service) error) {
	t.Helper()
	binary := os.Getenv("CADDY_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("set CADDY_INTEGRATION_BINARY")
	}
	dir := t.TempDir()
	cfg := integrationConfig(t, dir)
	cfg.Domains[0].Access = domain.DomainAccess("internet")
	// Map container mount points to local fixtures when running outside Docker.
	generate := func(items []domain.Service) ([]byte, error) {
		raw, err := domain.Generate(cfg, items)
		for containerPath, localPath := range mounts {
			from, _ := json.Marshal(containerPath)
			to, _ := json.Marshal(localPath)
			raw = bytes.ReplaceAll(raw, from, to)
		}
		return raw, err
	}
	raw, err := generate(services)
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewClient(Options{Socket: cfg.Socket, CaddyBinary: binary, DataDir: dir})
	if err = adapter.Validate(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "advanced.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "run", "--config", path)
	command.Env = append(os.Environ(), "XDG_DATA_HOME="+dir, "XDG_CONFIG_HOME="+dir)
	if len(root) > 0 {
		trust := filepath.Join(dir, "upstream-ca.pem")
		if err = os.WriteFile(trust, root, 0600); err != nil {
			t.Fatal(err)
		}
		command.Env = append(command.Env, "SSL_CERT_FILE="+trust)
	}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Signal(os.Interrupt); _ = command.Wait() })
	for range 100 {
		if _, err = adapter.Read(context.Background()); err == nil {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if err != nil {
		t.Fatal("Caddy not ready", err)
	}
	for range 100 {
		conn, e := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", "127.0.0.1:"+cfg.HTTPSPort, &tls.Config{InsecureSkipVerify: true, ServerName: "app.home.example.test"})
		err = e
		if err == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if err != nil {
		t.Fatal("internal certificate not ready", err)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: "app.home.example.test"}, ForceAttemptHTTP2: true, DisableCompression: true} // Isolated internal CA only.
	t.Cleanup(transport.CloseIdleConnections)
	reload := func(items []domain.Service) error {
		next, err := generate(items)
		if err != nil {
			return err
		}
		if err = adapter.Validate(context.Background(), next); err != nil {
			return err
		}
		return adapter.Load(context.Background(), next)
	}
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}, "https://127.0.0.1:" + cfg.HTTPSPort, reload
}
func TestCaddyIntegrationServiceCompressionHeadersAndTimeouts(t *testing.T) {
	if os.Getenv("CADDY_INTEGRATION_BINARY") == "" {
		t.Skip("set CADDY_INTEGRATION_BINARY")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			time.Sleep(150 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Server", "private-server")
		w.Header().Set("X-Received", r.Header.Get("X-App"))
		_, _ = io.WriteString(w, strings.Repeat("Hello from the upstream! ", 100))
	}))
	defer upstream.Close()
	item := domain.Service{ID: "advanced", DomainID: "home", Hostname: "app.home.example.test", Scheme: "http", Host: "127.0.0.1", Dial: strings.TrimPrefix(upstream.URL, "http://"), Enabled: true, Advanced: &domain.ServiceAdvanced{Snippet: "encode gzip\nheader X-Frame-Options SAMEORIGIN\nreverse_proxy {\n header_up X-App photos\n header_down -Server\n transport http {\n dial_timeout 2s\n response_header_timeout 30ms\n }\n}"}}
	client, base, reload := runAdvancedCaddy(t, []domain.Service{item}, nil, nil)
	req, _ := http.NewRequest("GET", base+"/", nil)
	req.Host = item.Hostname
	req.Header.Set("Accept-Encoding", "gzip")
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Encoding") != "gzip" || response.Header.Get("X-Frame-Options") != "SAMEORIGIN" || response.Header.Get("X-Received") != "photos" || response.Header.Get("Server") != "" {
		t.Fatalf("status=%d headers=%v", response.StatusCode, response.Header)
	}
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(reader)
	_ = reader.Close()
	if !strings.Contains(string(body), "Hello from the upstream!") {
		t.Fatal("bad compressed body")
	}
	req, _ = http.NewRequest("GET", base+"/slow", nil)
	req.Host = item.Hostname
	response, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("response-header timeout did not reject slow upstream: %d", response.StatusCode)
	}

	// Real reload and restoration of the original business model.
	changed := item
	changed.Advanced = &domain.ServiceAdvanced{ResponseHeaders: []domain.HeaderRule{{Name: "X-Version", Operation: "set", Value: "next"}}}
	for _, version := range []domain.Service{changed, item} {
		if err = reload([]domain.Service{version}); err != nil {
			t.Fatal(err)
		}
		request, _ := http.NewRequest("GET", base+"/", nil)
		request.Host = item.Hostname
		request.Header.Set("Accept-Encoding", "gzip")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if version.Advanced.Snippet == "" {
			if response.Header.Get("X-Version") != "next" || response.Header.Get("Content-Encoding") != "" {
				t.Fatal("replacement options not loaded", response.Header)
			}
		} else if response.Header.Get("X-Version") != "" || response.Header.Get("Content-Encoding") != "gzip" || response.Header.Get("X-Received") != "photos" {
			t.Fatal("original options not restored", response.Header)
		}
	}
}
func TestCaddyIntegrationGRPCUsesHTTP2AndValidatesUpstreamTLS(t *testing.T) {
	if os.Getenv("CADDY_INTEGRATION_BINARY") == "" {
		t.Skip("set CADDY_INTEGRATION_BINARY")
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			http.Error(w, "HTTP/2 required", 400)
			return
		}
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Trailer", "Grpc-Status")
		w.WriteHeader(200)
		_, _ = w.Write([]byte{0, 0, 0, 0, 2, 'o', 'k'})
		w.Header().Set("Grpc-Status", "0")
	})
	clear := httptest.NewUnstartedServer(handler)
	clear.Config.Protocols = new(http.Protocols)
	clear.Config.Protocols.SetHTTP1(true)
	clear.Config.Protocols.SetUnencryptedHTTP2(true)
	clear.Start()
	defer clear.Close()
	secure := httptest.NewUnstartedServer(handler)
	secure.EnableHTTP2 = true
	secure.StartTLS()
	defer secure.Close()
	services := []domain.Service{
		{ID: "clear", DomainID: "home", Hostname: "grpc.home.example.test", Scheme: "h2c", Host: "127.0.0.1", Dial: strings.TrimPrefix(clear.URL, "http://"), Enabled: true},
		{ID: "tls", DomainID: "home", Hostname: "tls.home.example.test", Scheme: "grpcs", Host: "127.0.0.1", Dial: strings.TrimPrefix(secure.URL, "https://"), Enabled: true},
		{ID: "invalid", DomainID: "home", Hostname: "invalid.home.example.test", Scheme: "grpcs", Host: "wrong.example.test", Dial: strings.TrimPrefix(secure.URL, "https://"), Enabled: true},
	}
	client, base, _ := runAdvancedCaddy(t, services, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: secure.Certificate().Raw}), nil)
	for _, s := range services {
		req, _ := http.NewRequest("POST", base+"/test.Echo/Call", strings.NewReader("\x00\x00\x00\x00\x00"))
		req.Host = s.Hostname
		req.Header.Set("Content-Type", "application/grpc")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if s.ID == "invalid" {
			if response.StatusCode != 502 {
				t.Fatalf("wrong TLS name accepted: %d", response.StatusCode)
			}
			continue
		}
		if response.StatusCode != 200 || response.Trailer.Get("Grpc-Status") != "0" || string(body) != "\x00\x00\x00\x00\x02ok" {
			t.Fatalf("%s status=%d trailer=%v body=%q", s.ID, response.StatusCode, response.Trailer, body)
		}
	}
}

func TestCaddyIntegrationStaticSiteRoutes(t *testing.T) {
	if os.Getenv("CADDY_INTEGRATION_BINARY") == "" {
		t.Skip("set CADDY_INTEGRATION_BINARY")
	}
	dir := t.TempDir()
	for name, body := range map[string]string{"index.html": "SPA entry", "asset.js": "static asset"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/missing" {
			w.WriteHeader(http.StatusNotFound)
		}
		io.WriteString(w, r.Method+" "+r.URL.RequestURI())
	}))
	defer upstream.Close()
	site := domain.StaticSite{Root: "/srv/sites/douji_admin", APIPath: "/api/*"}
	service := domain.Service{ID: "douji", DomainID: "home", Hostname: "app.home.example.test", Enabled: true, Scheme: "http", Dial: strings.TrimPrefix(upstream.URL, "http://"), StaticSite: &site}
	client, base, _ := runAdvancedCaddy(t, []domain.Service{service}, nil, map[string]string{site.Root: dir})
	for _, tc := range []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/", 200, "SPA entry"},
		{"GET", "/assets/settings", 200, "SPA entry"},
		{"GET", "/asset.js", 200, "static asset"},
		{"POST", "/api/items?q=1", 200, "POST /api/items?q=1"},
		{"GET", "/api/missing", 404, "GET /api/missing"},
		{"GET", "/api", 200, "SPA entry"},
		{"GET", "/apix/page", 200, "SPA entry"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, base+tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Host = service.Hostname
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != tc.status || string(body) != tc.body {
				t.Fatalf("%s: got %d %q, error %v", tc.path, response.StatusCode, body, err)
			}
		})
	}
}

func TestCaddyIntegrationCustomHTTPRoutes(t *testing.T) {
	binary := os.Getenv("CADDY_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("set CADDY_INTEGRATION_BINARY")
	}
	source := `handle /api/* {
	reverse_proxy 10.77.0.8:18009 {
		header_up Host {host}
		header_up X-Forwarded-Proto {scheme}
		header_up X-Forwarded-For {remote}
	}
}
handle /about {
	respond "about douji" 200
}
handle {
	root * /srv/sites/douji_admin
	try_files {path} {path}/ /index.html
	file_server
}`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("front end"), 0600); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, r.RequestURI) }))
	defer upstream.Close()
	service := domain.Service{ID: "douji", Name: "Douji", Hostname: "app.home.example.test", DomainID: "home", Caddyfile: source, Enabled: true}
	policy := domain.TargetPolicy{Domains: []domain.ManagedDomain{{ID: "home", Name: "home.example.test"}}, UpstreamCIDRs: []string{"10.77.0.0/16"}, RouteAdapter: NewClient(Options{CaddyBinary: binary})}
	normalized, err := domain.NormalizeService(context.Background(), policy, service)
	if err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]string{"/srv/sites/douji_admin": dir, "10.77.0.8:18009": strings.TrimPrefix(upstream.URL, "http://")}
	client, base, _ := runAdvancedCaddy(t, []domain.Service{normalized}, nil, fixtures)
	read := func(path, want string) {
		t.Helper()
		req, err := http.NewRequest("GET", base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = service.Hostname
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != 200 || string(body) != want {
			t.Fatalf("%s returned %d %q (%v)", path, resp.StatusCode, body, err)
		}
	}
	read("/settings", "front end")
	read("/about", "about douji")
	read("/api/users?q=1", "/api/users?q=1")
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("new build"), 0600); err != nil {
		t.Fatal(err)
	}
	read("/settings", "new build")
}
