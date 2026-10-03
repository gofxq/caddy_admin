package application

import (
	"fmt"
	"github.com/gofxq/caddy_admin/internal/domain"
	"strings"
	"testing"
)

func TestConfigurationSizeUsesPortableJSONWithoutHTMLEscaping(t *testing.T) {
	value := Configuration{Format: ConfigurationFormat, Version: ConfigurationVersion, Settings: domain.ManagedSettings{Origin: "https://caddyadmin.home.example.test", Domains: []domain.ManagedDomain{{ID: "home", Name: "home.example.test", Access: domain.DomainAccess("trusted")}}, AdminDomain: "caddyadmin.home.example.test", LAN: []string{"10.0.0.0/8"}, UpstreamCIDRs: []string{"10.0.0.0/8"}, Resolvers: []string{"10.0.0.53"}}, Services: []PortableService{}}
	for i := 0; i < 6; i++ {
		value.Services = append(value.Services, PortableService{Name: "Photos", DomainID: "home", Hostname: fmt.Sprintf("photos%d.home.example.test", i), Scheme: "http", Host: "10.0.0.8", Port: 8080, Notes: strings.Repeat("<>&", 666)})
	}
	if err := validateConfiguration(value); err != nil {
		t.Fatalf("small portable file rejected after HTML expansion: %v", err)
	}
	value.Services[0].Notes = strings.Repeat("x", MaxConfigurationBytes)
	if err := validateConfiguration(value); err == nil {
		t.Fatal("oversized configuration accepted")
	}
}
