package domain_test

import (
	"context"
	"testing"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestTargetPolicyProtectsEverySystemEndpoint(t *testing.T) {
	policy := domain.TargetPolicy{
		Domains: []domain.ManagedDomain{{ID: "home", Name: "home.example", Access: domain.DomainAccess("trusted")}}, AdminDomain: "caddyadmin.home.example",
		UpstreamCIDRs: []string{"10.0.0.0/8"}, SystemEndpoints: []string{"10.77.0.8:8080"},
	}
	_, err := domain.NormalizeService(context.Background(), policy, domain.Service{Name: "Photos", DomainID: "home", Hostname: "photos.home.example", Scheme: "http", Host: "10.77.0.8", Port: 2283})
	if err == nil {
		t.Fatal("system endpoint was accepted as an upstream")
	}
}

func TestValidateManagedSettingsRejectsIncompletePolicy(t *testing.T) {
	err := domain.ValidateManagedSettings(domain.ManagedSettings{Domains: []domain.ManagedDomain{{ID: "home", Name: "home.example", Access: domain.DomainAccess("trusted")}}, AdminDomain: "caddyadmin.home.example"})
	if err == nil {
		t.Fatal("managed settings without network policy and resolvers were accepted")
	}
}
