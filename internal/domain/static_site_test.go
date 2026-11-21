package domain_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestStaticSiteService(t *testing.T) {
	base := domain.ManagedDomain{ID: "home", Name: "home.example", Access: domain.DomainAccess("internet")}
	policy := domain.TargetPolicy{Domains: []domain.ManagedDomain{base}, UpstreamCIDRs: []string{"10.0.0.0/8"}}
	config := domain.CaddyConfig{AdminDomain: "admin.home.example", Domains: policy.Domains, HTTPPort: "80", HTTPSPort: "443", AdminURL: "http://caddy:2019"}
	for _, tc := range []struct {
		root, matcher string
		valid         bool
	}{
		{"/srv/sites/douji_admin", "/api/*", true},
		{"/srv/sites/shop", "/v2/backend/*", true},
		{"/srv/sites", "/api/*", false},
		{"/srv/sites/../snapshots", "/api/*", false},
		{"/data", "/api/*", false},
		{"/srv/sites/app", "/*", false},
		{"/srv/sites/app", "/{path}/*", false},
		{"/srv/sites/app", "/api/../backend/*", false},
	} {
		t.Run(tc.root+" "+tc.matcher, func(t *testing.T) {
			s := domain.Service{Name: "Test", Hostname: "app.home.example", DomainID: "home", Scheme: "http", Host: "10.10.0.5", Port: 18009, Enabled: true}
			body, _ := json.Marshal(map[string]any{"static_site": map[string]string{"root": tc.root, "api_path": tc.matcher}})
			if err := json.Unmarshal(body, &s); err != nil {
				t.Fatal(err)
			}
			s, err := domain.NormalizeService(context.Background(), policy, s)
			if !tc.valid {
				if err == nil {
					t.Fatal("invalid static site accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := domain.Generate(config, []domain.Service{s})
			if err != nil {
				t.Fatal(err)
			}
			// In external mode the console is proxied, so only this service serves files.
			for _, want := range []string{tc.root, tc.matcher, "10.10.0.5:18009", `"{http.request.uri.path}/"`, `"/index.html"`, `"handler": "file_server"`} {
				if !strings.Contains(string(raw), want) {
					t.Errorf("missing %q in service config", want)
				}
			}
		})
	}
}

func TestFilesOnlyNeedsNoUpstream(t *testing.T) {
	base := domain.ManagedDomain{ID: "home", Name: "home.example", Access: domain.DomainAccess("internet")}
	policy := domain.TargetPolicy{Domains: []domain.ManagedDomain{base}}
	s := domain.Service{Name: "Docs", Hostname: "docs.home.example", DomainID: "home", Enabled: true,
		StaticSite: &domain.StaticSite{Root: "/srv/sites/docs"}}
	normalized, err := domain.NormalizeService(context.Background(), policy, s)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Dial != "" {
		t.Fatal("unexpected proxy target")
	}
	raw, err := domain.Generate(domain.CaddyConfig{Domains: policy.Domains, AdminDomain: "admin.home.example", AdminURL: "http://manager:2019", HTTPPort: "80", HTTPSPort: "443"}, []domain.Service{normalized})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "/srv/sites/docs") || strings.Count(string(raw), `"handler": "reverse_proxy"`) != 1 {
		t.Fatal("expected only the console to use a proxy")
	}
}
