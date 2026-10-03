package caddy

import (
	"context"
	"crypto/tls"
	"encoding/json"
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

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	return port
}

func integrationConfig(t *testing.T, dir string) domain.CaddyConfig {
	t.Helper()
	return domain.CaddyConfig{
		Socket: filepath.Join(dir, "admin.sock"), Domains: []domain.ManagedDomain{{ID: "home", Name: "home.example.test", Access: domain.DomainAccess("trusted")}},
		AdminDomain: "caddyadmin.home.example.test", LAN: []string{"127.0.0.1/32"},
		ManagerDial: "127.0.0.1:1", StaticRoot: dir, CaddyStorage: filepath.Join(dir, "storage"),
		CertificateMode: domain.CertificateModeBootstrapInternal, TestTLS: true,
		HTTPPort: freePort(t), HTTPSPort: freePort(t),
	}
}

func TestCaddyIntegrationSetupIPTLSRedirectAndUnknownHostIsolation(t *testing.T) {
	binary := os.Getenv("CADDY_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("set CADDY_INTEGRATION_BINARY to test a real Caddy")
	}
	dir := t.TempDir()
	config := integrationConfig(t, dir)
	config.SetupCertPath, config.SetupKeyPath = filepath.Join(dir, "setup.crt"), filepath.Join(dir, "setup.key")
	if err := EnsureBootstrapCertificate(config.SetupCertPath, config.SetupKeyPath, "https://127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	forwarded := make(chan string, 10)
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded <- r.Header.Get(domain.ClientAddressHeader)
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "temporary handoff")
	}))
	defer bridge.Close()
	raw, err := domain.Generate(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.ReplaceAll(string(raw), domain.SetupBridgeAddress, strings.TrimPrefix(bridge.URL, "http://")))
	client := NewClient(Options{Socket: config.Socket, CaddyBinary: binary, DataDir: dir})
	if err = client.Validate(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "active.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "run", "--config", path)
	command.Env = append(os.Environ(), "XDG_DATA_HOME="+dir, "XDG_CONFIG_HOME="+dir)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Signal(os.Interrupt); _ = command.Wait() }()
	for range 50 {
		if _, err = client.Read(context.Background()); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // Isolated self-signed Setup fixture only.
	defer transport.CloseIdleConnections()
	web := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, _ := http.NewRequest("GET", "https://127.0.0.1:"+config.HTTPSPort+"/api/v1/setup/handoff", nil)
	request.Header.Set(domain.ClientAddressHeader, "203.0.113.20")
	response, err := web.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || string(body) != "temporary handoff" {
		t.Fatalf("IP TLS handoff = %d %s", response.StatusCode, body)
	}
	if clientIP := <-forwarded; clientIP != "127.0.0.1" {
		t.Fatalf("Caddy trusted forged client header: %q", clientIP)
	}
	request, _ = http.NewRequest("GET", "https://127.0.0.1:"+config.HTTPSPort+"/", nil)
	request.Host = "unregistered.example.test"
	response, err = web.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatalf("unregistered host reached temporary proxy: %d", response.StatusCode)
	}
	response, err = web.Get("http://127.0.0.1:" + config.HTTPPort + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 302 || response.Header.Get("Location") != "https://127.0.0.1:"+config.HTTPSPort+"/setup" {
		t.Fatalf("IP HTTP redirect = %d %v", response.StatusCode, response.Header)
	}
	request, _ = http.NewRequest("GET", "http://127.0.0.1:"+config.HTTPPort+"/", nil)
	request.Host = config.AdminDomain
	response, err = web.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 308 || !strings.Contains(response.Header.Get("Location"), config.AdminDomain) {
		t.Fatalf("formal domain HTTP redirect = %d %v", response.StatusCode, response.Header)
	}
	request, _ = http.NewRequest("GET", "http://127.0.0.1:"+config.HTTPPort+"/", nil)
	request.Host = "[fd00::10]:" + config.HTTPPort
	response, err = web.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.Header.Get("Location") != "https://[fd00::10]:"+config.HTTPSPort+"/setup" {
		t.Fatalf("IPv6 redirect = %d %v", response.StatusCode, response.Header)
	}
}

func TestCaddyIntegrationValidateLoadAndPreserveFailedRuntime(t *testing.T) {
	binary := os.Getenv("CADDY_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("set CADDY_INTEGRATION_BINARY to test a real Caddy")
	}
	dir := t.TempDir()
	config := integrationConfig(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("admin"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := domain.Generate(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(Options{Socket: config.Socket, CaddyBinary: binary, DataDir: dir})
	if err = client.Validate(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	snapshot := &Snapshot{Path: filepath.Join(dir, "active.json")}
	if err = snapshot.Write(raw); err != nil {
		t.Fatal(err)
	}
	start := func() *exec.Cmd {
		process := exec.Command(binary, "run", "--config", snapshot.Path)
		process.Env = append(os.Environ(), "XDG_DATA_HOME="+dir, "XDG_CONFIG_HOME="+dir)
		process.Stdout, process.Stderr = io.Discard, io.Discard
		if startErr := process.Start(); startErr != nil {
			t.Fatal(startErr)
		}
		return process
	}
	command := start()
	defer func() {
		if command.Process != nil {
			_ = command.Process.Signal(os.Interrupt)
			_ = command.Wait()
		}
	}()

	var running []byte
	for attempt := 0; attempt < 50; attempt++ {
		running, err = client.Read(context.Background())
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal("Caddy did not expose its Unix Admin API:", err)
	}
	before := domain.Fingerprint(running)
	var invalid map[string]any
	if err = json.Unmarshal(running, &invalid); err != nil {
		t.Fatal(err)
	}
	invalid["apps"].(map[string]any)["nonexistent-module"] = map[string]any{}
	bad, _ := json.Marshal(invalid)
	if err = client.Validate(context.Background(), bad); err == nil {
		t.Fatal("real validation accepted an invalid module")
	}
	if err = client.Load(context.Background(), bad); err == nil {
		t.Fatal("real Caddy accepted an invalid module")
	}
	after, err := client.Read(context.Background())
	if err != nil || domain.Fingerprint(after) != before {
		t.Fatalf("failed load changed runtime: %v", err)
	}
	var candidateRoot map[string]any
	if err = json.Unmarshal(raw, &candidateRoot); err != nil {
		t.Fatal(err)
	}
	apps := candidateRoot["apps"].(map[string]any)
	httpApp := apps["http"].(map[string]any)
	servers := httpApp["servers"].(map[string]any)
	managed := servers["managed"].(map[string]any)
	routes := managed["routes"].([]any)
	fallback := routes[len(routes)-1].(map[string]any)
	handlers := fallback["handle"].([]any)
	handlers[0].(map[string]any)["body"] = "Not found after hot load"
	candidate, err := json.MarshalIndent(candidateRoot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Validate(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	if err = client.Load(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	loaded, err := client.Read(context.Background())
	if err != nil || domain.Fingerprint(loaded) != domain.Fingerprint(candidate) {
		t.Fatalf("hot load did not become authoritative: %v", err)
	}
	if err = snapshot.Write(candidate); err != nil {
		t.Fatal(err)
	}
	if err = command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err = command.Wait(); err != nil {
		t.Fatal(err)
	}
	command = start()
	for attempt := 0; attempt < 50; attempt++ {
		loaded, err = client.Read(context.Background())
		if err == nil && domain.Fingerprint(loaded) == domain.Fingerprint(candidate) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil || domain.Fingerprint(loaded) != domain.Fingerprint(candidate) {
		t.Fatalf("restart did not restore the active snapshot: %v", err)
	}
}

func TestCloudflareValidationReadsRestrictedSecretFile(t *testing.T) {
	binary := os.Getenv("CADDY_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("set CADDY_INTEGRATION_BINARY to validate the Cloudflare module")
	}
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secrets", "cloudflare_token")
	secret := &SecretFile{Path: secretPath}
	if err := secret.WriteCloudflareToken("cfat_" + strings.Repeat("x", 32)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	t.Setenv("CLOUDFLARE_API_TOKEN_FILE", secretPath)
	config := integrationConfig(t, dir)
	config.TestTLS = false
	config.CertificateMode = domain.CertificateModeCloudflare
	config.TemporaryAdminCertificate = true
	config.TemporaryAdminCertPath = filepath.Join(dir, "secrets", "admin.crt")
	config.TemporaryAdminKeyPath = filepath.Join(dir, "secrets", "admin.key")
	if err := EnsureBootstrapCertificate(config.TemporaryAdminCertPath, config.TemporaryAdminKeyPath, "https://"+config.AdminDomain); err != nil {
		t.Fatal(err)
	}
	raw, err := domain.Generate(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(Options{Socket: config.Socket, CaddyBinary: binary, DataDir: dir})
	if err = client.Validate(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
}
