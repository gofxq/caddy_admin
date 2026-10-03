package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	caddyadapter "github.com/gofxq/caddy_admin/internal/adapter/caddy"
	"github.com/gofxq/caddy_admin/internal/adapter/cloudflare"
	"github.com/gofxq/caddy_admin/internal/adapter/gormstore"
	"github.com/gofxq/caddy_admin/internal/adapter/httpapi"
	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/config"
	"github.com/gofxq/caddy_admin/internal/domain"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func password() (string, error) {
	fmt.Fprint(os.Stderr, "管理员密码（至少 8 个字符，最多 256 字节）: ")
	b, e := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b), e
}
func run() error {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if !knownManagerCommand(command) {
		return fmt.Errorf("unknown manager command %q", command)
	}
	c, err := config.Load()
	if err != nil {
		return err
	}
	if command == "check-secret" {
		return domain.ValidateCloudflareToken(os.Getenv("CLOUDFLARE_API_TOKEN"))
	}
	if command == "run" {
		return runContainer(c)
	}
	ctx := context.Background()
	if command == "health" {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:8081/ready", nil)
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			return fmt.Errorf("manager readiness unavailable")
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return fmt.Errorf("manager not ready")
		}
		return nil
	}
	if err = os.MkdirAll(c.DataDir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(c.DataDir, "manager.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("manager is running; stop it before maintenance")
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	store, err := gormstore.Open(filepath.Join(c.DataDir, "state.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	initialized, err := store.IsInitialized(ctx)
	if err != nil {
		return err
	}
	certificate := domain.CertificateStatus{Mode: domain.CertificateModeBootstrapInternal, PublicStatus: "unknown"}
	if !initialized && command == "serve" {
		return serveSetup(c, store)
	}
	if initialized {
		settings, e := store.ManagedSettings(ctx)
		if e != nil {
			return e
		}
		if e = config.ApplyManagedSettings(&c, settings); e != nil {
			return fmt.Errorf("stored settings are invalid: %w", e)
		}
		var ce error
		certificate, ce = store.CertificateStatus(ctx)
		if ce != nil {
			return ce
		}
		c.CertificateMode = certificate.Mode
		if c.AdminURL == "" && certificate.Mode == domain.CertificateModeCloudflare {
			if e = loadCloudflareSecret(); e != nil {
				return e
			}
		}
	}
	switch command {
	case "reset-password":
		p, e := password()
		if e != nil {
			return e
		}
		return store.ResetPassword(ctx, p)
	case "history":
		records, e := store.Deployments(ctx, 0, 100)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(records)
	case "seed-examples":
		draft, e := store.Draft(ctx)
		if e != nil {
			return e
		}
		if len(draft.Services) > 0 {
			return fmt.Errorf("examples require an empty draft")
		}
		if len(c.Domains) == 0 {
			return fmt.Errorf("no registered domain")
		}
		for _, s := range []domain.Service{{Name: "Router", DomainID: c.Domains[0].ID, Hostname: "router." + c.Domains[0].Name, Scheme: "http", Host: "10.0.0.1", Port: 80, Enabled: true}, {Name: "File browser", DomainID: c.Domains[0].ID, Hostname: "files." + c.Domains[0].Name, Scheme: "http", Host: "10.0.0.8", Port: 5666, Enabled: true}} {
			s.ID = domain.ID()
			s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			s, e = domain.NormalizeService(ctx, c.TargetPolicy(net.DefaultResolver, nil), s)
			if e != nil {
				return e
			}
			draft, e = store.SaveService(ctx, draft.Revision, s, false, "operator")
			if e != nil {
				return e
			}
		}
		return nil
	case "serve":
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	if _, err = os.Stat(c.ActivePath()); err != nil {
		return fmt.Errorf("missing active.json; initialize or restore snapshot")
	}
	service := composeApplication(c, store)
	recoverCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = service.Recover(recoverCtx)
	cancel()
	if err != nil {
		log.Print("startup reconciliation deferred; inspect overview")
	}
	api := httpapi.New(service, httpapi.Options{Origin: c.Origin, ManagerVersion: config.Version, RuntimeConfig: c, ExternalCaddy: c.AdminURL != ""})
	if os.Getenv("MANAGER_SUPERVISED") == "true" {
		api.SetRestart(func() {
			time.Sleep(time.Second)
			_ = syscall.Kill(os.Getppid(), syscall.SIGTERM)
		})
	}
	server := &http.Server{Addr: c.Listen, Handler: api.ControlHandler(httpapi.WebHandler(c.StaticRoot, api.Handler())), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	var rescueServer *http.Server
	temporaryEntry := rescueEntryEnabled(c, certificate)
	if c.AdminURL != "" {
		_, latestErr := store.Latest(ctx)
		var appErr *domain.AppError
		if latestErr != nil && (!errors.As(latestErr, &appErr) || appErr.Code != "not_found") {
			return latestErr
		}
		temporaryEntry = latestErr != nil
	}
	if temporaryEntry {
		rescueServer, err = startTemporaryEntry(c, api, service)
		if err != nil {
			return err
		}
		defer rescueServer.Close()
	}
	host, port, _ := net.SplitHostPort(c.Listen)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	readiness := &http.Server{Addr: "127.0.0.1:8081", Handler: httpapi.ReadinessHandler(service, &http.Client{Timeout: 2 * time.Second}, "http://"+net.JoinHostPort(host, port)+"/api/v1/auth/session"), ReadHeaderTimeout: 2 * time.Second, WriteTimeout: 3 * time.Second}
	readyListener, err := net.Listen("tcp", readiness.Addr)
	if err != nil {
		return err
	}
	defer readiness.Close()
	go func() {
		if err := readiness.Serve(readyListener); err != nil && err != http.ErrServerClosed {
			log.Print("readiness server stopped")
			_ = server.Close()
		}
	}()
	life, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-life.Done():
				return
			case <-ticker.C:
				recoverCtx, recoverCancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := service.Recover(recoverCtx); err != nil {
					slog.Warn("reconciliation_deferred", "stage", "periodic_recovery", "error_class", "unavailable")
				}
				recoverCancel()
			}
		}
	}()
	go func() {
		<-life.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		if rescueServer != nil {
			_ = rescueServer.Shutdown(ctx)
		}
	}()
	log.Printf("manager %s listening on %s", config.Version, c.Listen)
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func knownManagerCommand(command string) bool {
	switch command {
	case "run", "serve", "health", "reset-password", "history", "seed-examples", "check-secret":
		return true
	default:
		return false
	}
}

func composeApplication(c config.Config, store *gormstore.Store) *application.Service {
	secretPath := config.CloudflareTokenPath(c.DataDir)
	options := application.Options{
		DataDir: c.DataDir, SnapshotDir: c.SnapshotDir, AdminURL: c.AdminURL,
		ManagerDial: c.ManagerDial, ProbeAddress: c.ProbeAddress, CaddyStorage: c.CaddyStorage,
		StaticRoot: c.StaticRoot, Socket: c.Socket, SetupPassword: os.Getenv("ADMIN_PASSWORD"), SetupToken: os.Getenv("CLOUDFLARE_API_TOKEN"), CaddyBinary: c.CaddyBinary,
		HTTPPort: c.HTTPPort, HTTPSPort: c.HTTPSPort, TestTLS: c.TestTLS,
		CertificateMode: c.CertificateMode, RuntimePolicy: c.ManagedSettings(),
	}
	client := caddyadapter.NewClient(caddyadapter.Options{AdminURL: c.AdminURL, Socket: c.Socket, CaddyBinary: c.CaddyBinary, DataDir: c.DataDir})
	return application.New(options, store, client, application.Dependencies{
		UpstreamProbe: &caddyadapter.TCPUpstreamProbe{},
		SetupDNS:      cloudflare.New(), SetupIntent: &caddyadapter.SetupIntentFile{Path: filepath.Join(c.DataDir, "secrets", "setup_dns_intent.json")},
		Resolver: net.DefaultResolver, Certificates: &caddyadapter.Probe{},
		Snapshot: &caddyadapter.Snapshot{Path: c.ActivePath()},
		Secrets:  &caddyadapter.SecretFile{Path: secretPath}, BootstrapCertificate: &caddyadapter.BootstrapTLS{},
		SetupProbe: &caddyadapter.SetupProbe{}, ResolverSuggestions: func() []string { return caddyadapter.ResolverSuggestions("/etc/resolv.conf") },
	})
}

func rescueEntryEnabled(c config.Config, status domain.CertificateStatus) bool {
	return c.AdminURL == "" && status.PublicStatus != "ready"
}

func startTemporaryEntry(c config.Config, api *httpapi.API, service *application.Service) (*http.Server, error) {
	certPath := filepath.Join(c.DataDir, "secrets", "setup_tls.crt")
	keyPath := filepath.Join(c.DataDir, "secrets", "setup_tls.key")
	if err := caddyadapter.EnsureBootstrapCertificate(certPath, keyPath, "https://127.0.0.1"); err != nil {
		return nil, err
	}
	var web http.Handler = httpapi.WebHandler(c.StaticRoot, http.NotFoundHandler())
	if c.AdminURL == "" {
		api.SetRescueEntryEnabled(true)
		web = api.ControlHandler(httpapi.WebHandler(c.StaticRoot, api.Handler()))
	}
	ready := make(chan struct{})
	var readyOnce sync.Once
	api.SetConsoleSeen(func() { readyOnce.Do(func() { close(ready) }) })
	handler, err := temporaryEntryHandler(c, web, httpapi.NewHandoff(service, httpapi.HandoffOptions{}, nil))
	if err != nil {
		return nil, err
	}
	listen := domain.SetupBridgeAddress
	if c.AdminURL == "" {
		handler = temporaryBridgeHandler(handler)
	} else {
		listen = os.Getenv("SETUP_LISTEN")
		if listen == "" {
			listen = ":8082"
		}
	}
	server := newSetupHTTPServer(listen, handler)
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}
	server.RegisterOnShutdown(func() { api.SetRescueEntryEnabled(false) })
	go func() {
		var serveErr error
		if c.AdminURL == "" {
			serveErr = server.Serve(listener)
		} else {
			serveErr = server.ServeTLS(listener, certPath, keyPath)
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			slog.Error("rescue_entry_stopped", "error_class", "unavailable")
		}
	}()
	go func() {
		closeTemporaryEntryAfterReady(ready, 5*time.Minute, func() {
			shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownContext)
		})
	}()
	log.Printf("temporary handoff entry listening on %s", listen)
	return server, nil
}

func closeTemporaryEntryAfterReady(ready <-chan struct{}, grace time.Duration, closeEntry func()) {
	<-ready
	timer := time.NewTimer(grace)
	defer timer.Stop()
	<-timer.C
	closeEntry()
}

func temporaryEntryHandler(c config.Config, web, handoff http.Handler) (http.Handler, error) {
	mux := http.NewServeMux()
	mux.Handle("/api/v1/setup/handoff", handoff)
	mux.Handle("/", web)
	return mux, nil
}

// This listener is container-loopback only. Caddy overwrites the client header
// before forwarding; restore the TCP client for the existing source policies.
func temporaryBridgeHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		address, parseErr := netip.ParseAddr(peer)
		client, clientErr := netip.ParseAddr(r.Header.Get(domain.ClientAddressHeader))
		if err != nil || parseErr != nil || !address.IsLoopback() || clientErr != nil {
			http.Error(w, "Untrusted temporary entry proxy", http.StatusForbidden)
			return
		}
		request := r.Clone(r.Context())
		request.RemoteAddr = net.JoinHostPort(client.Unmap().String(), "0")
		next.ServeHTTP(w, request)
	})
}

func setupPeerAllowed(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	_, err = netip.ParseAddr(host)
	return err == nil
}
func setupAccessHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path == "/" {
			http.Redirect(w, r, "/setup", http.StatusTemporaryRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func serveSetup(c config.Config, store *gormstore.Store) error {
	certPath := filepath.Join(c.DataDir, "secrets", "setup_tls.crt")
	keyPath := filepath.Join(c.DataDir, "secrets", "setup_tls.key")
	if err := caddyadapter.EnsureBootstrapCertificate(certPath, keyPath, "https://127.0.0.1"); err != nil {
		return err
	}
	done := make(chan struct{})
	var once sync.Once
	service := composeApplication(c, store)
	web := httpapi.WebHandler(c.StaticRoot, httpapi.NewSetup(service, httpapi.SetupOptions{}, func() { once.Do(func() { close(done) }) }))
	handler := setupAccessHandler(web)
	setupListen := ":" + c.HTTPSPort
	if c.AdminURL != "" {
		setupListen = os.Getenv("SETUP_LISTEN")
		if setupListen == "" {
			setupListen = ":8082"
		}
	}
	server := newSetupHTTPServer(setupListen, handler)
	life, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	additional := []*http.Server{}
	if c.AdminURL == "" {
		redirect := newSetupHTTPServer(":"+c.HTTPPort, handler)
		listener, listenErr := net.Listen("tcp", redirect.Addr)
		if listenErr != nil {
			return listenErr
		}
		defer redirect.Close()
		additional = append(additional, redirect)
		go func() {
			if serveErr := redirect.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
				stop()
				_ = server.Close()
			}
		}()
	}
	ready := &http.Server{Addr: "127.0.0.1:8081", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }), ReadHeaderTimeout: 2 * time.Second}
	listener, err := net.Listen("tcp", ready.Addr)
	if err != nil {
		return err
	}
	defer ready.Close()
	go func() { _ = ready.Serve(listener) }()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-life.Done():
		case <-done:
			// Let requests that raced with the winning setup POST enter the
			// server so graceful shutdown can return their deterministic 409.
			time.Sleep(250 * time.Millisecond)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownSetupServers(ctx, append(additional, server)...)
	}()
	if c.AdminURL == "" {
		log.Printf("manager setup on HTTPS :%s and HTTP :%s", c.HTTPSPort, c.HTTPPort)
	} else {
		log.Printf("manager setup listening on %s", setupListen)
	}
	err = server.ListenAndServeTLS(certPath, keyPath)
	stop()
	<-shutdownDone
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func shutdownSetupServers(ctx context.Context, servers ...*http.Server) {
	var pending sync.WaitGroup
	for _, server := range servers {
		pending.Add(1)
		go func() { defer pending.Done(); _ = server.Shutdown(ctx) }()
	}
	pending.Wait()
}

func newSetupHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
}

func lanOnlyHandler(cidrs []string, next http.Handler) (http.Handler, error) {
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for _, value := range cidrs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, prefix)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		address, parseErr := netip.ParseAddr(host)
		allowed := err == nil && parseErr == nil && address.IsLoopback()
		if !allowed && err == nil && parseErr == nil {
			for _, prefix := range prefixes {
				if prefix.Contains(address.Unmap()) {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			http.Error(w, "Access restricted to configured LAN/VPN networks", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}

func loadCloudflareSecret() error {
	b, err := os.ReadFile(config.CloudflareTokenPath(os.Getenv("DATA_DIR")))
	if err != nil {
		return fmt.Errorf("cannot read Cloudflare secret")
	}
	return os.Setenv("CLOUDFLARE_API_TOKEN", strings.TrimSpace(string(b)))
}
