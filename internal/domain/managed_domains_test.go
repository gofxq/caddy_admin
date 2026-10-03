package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManagedDomainsOptionalAccessAndConsoleACL(t *testing.T) {
	settings := ManagedSettings{Domains: []ManagedDomain{{ID: "first", Name: "example.com"}}, AdminDomain: "admin.example.com", Origin: "https://admin.example.com", Resolvers: []string{"1.1.1.1"}}
	if err := ValidateManagedSettings(settings); err != nil {
		t.Fatal(err)
	}
	cfg := CaddyConfig{Domains: settings.Domains, AdminDomain: settings.AdminDomain, HTTPPort: "80", HTTPSPort: "443", TestTLS: true}
	raw, err := Generate(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "remote_ip") {
		t.Fatal("default console must not restrict source")
	}
	service := Service{DomainID: "first", Hostname: "app.example.com", Enabled: true, Dial: "10.0.0.2:80"}
	if _, err = Generate(cfg, []Service{service}); err == nil {
		t.Fatal("unset access must reject enabled publication")
	}
	service.Enabled = false
	if _, err = Generate(cfg, []Service{service}); err != nil {
		t.Fatal(err)
	}
	cfg.Domains[0].Access = DomainAccess("internet")
	service.Enabled = true
	raw, err = Generate(cfg, []Service{service})
	if err != nil {
		t.Fatal(err)
	}
	var parsed any
	if err = json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	cfg.ConsoleLANOnly = true
	if _, err = Generate(cfg, nil); err == nil {
		t.Fatal("console ACL requires ranges")
	}
	cfg.LAN = []string{"10.0.0.0/8"}
	cfg.PreviousAdminDomain = "old.example.com"
	raw, err = Generate(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "old.example.com") || !strings.Contains(string(raw), "remote_ip") {
		t.Fatal("handoff entries must share explicit ACL")
	}
}

func TestManagedDomainsRejectAmbiguousNames(t *testing.T) {
	s := ManagedSettings{Domains: []ManagedDomain{{ID: "a", Name: "example.com"}, {ID: "b", Name: "sub.example.com"}}, AdminDomain: "admin.example.com", Origin: "https://admin.example.com", Resolvers: []string{"1.1.1.1"}}
	if ValidateManagedSettings(s) == nil {
		t.Fatal("overlapping bases accepted")
	}
}

func TestTrustedBusinessDomainDoesNotRestrictConsole(t *testing.T) {
	cfg := CaddyConfig{Domains: []ManagedDomain{{ID: "home", Name: "example.com", Access: DomainAccess("trusted")}}, AdminDomain: "admin.example.com", LAN: []string{"10.0.0.0/8"}, HTTPPort: "80", HTTPSPort: "443", TestTLS: true}
	raw, err := Generate(cfg, []Service{{DomainID: "home", Hostname: "app.example.com", Enabled: true, Dial: "10.0.0.2:80"}})
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err = json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	routes := root["apps"].(map[string]any)["http"].(map[string]any)["servers"].(map[string]any)["managed"].(map[string]any)["routes"].([]any)
	first := routes[0].(map[string]any)["match"].([]any)[0].(map[string]any)
	hosts := first["host"].([]any)
	for _, host := range hosts {
		if host == "*.example.com" || host == cfg.AdminDomain {
			t.Fatal("business ACL also restricts console")
		}
	}
}
