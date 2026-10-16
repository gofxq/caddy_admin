package domain

import (
	"bytes"
	"testing"
)

func TestDNSProviderSelection(t *testing.T) {
	for _, p := range []DNSProvider{"", DNSProviderCloudflare, DNSProviderDNSPod} {
		if _, err := NormalizeDNSProvider(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NormalizeDNSProvider("unknown"); err == nil {
		t.Fatal("unknown provider accepted")
	}
	if err := ValidateDNSToken(DNSProviderDNSPod, "123,abcdefghijklmnop0123456789"); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "123", "id,abcdefghijklmnop0123456789", "123,short"} {
		if ValidateDNSToken(DNSProviderDNSPod, token) == nil {
			t.Fatalf("accepted %q", token)
		}
	}
	if ValidateDNSToken(DNSProviderCloudflare, "123,abcdefghijklmnop0123456789") == nil {
		t.Fatal("provider credentials crossed")
	}
}
func TestDNSPodGeneratedCredentialsArePlaceholderOnly(t *testing.T) {
	c := CaddyConfig{CertificateMode: CertificateModeDNSPod, HTTPPort: "80", HTTPSPort: "443", ManagerDial: "127.0.0.1:8080", AdminDomain: "caddyadmin.home.example.test", Domains: []ManagedDomain{{ID: "home", Name: "home.example.test", Access: DomainAccess("internet")}}, Resolvers: []string{"1.1.1.1"}}
	raw, err := Generate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"name": "dnspod"`)) || !bytes.Contains(raw, []byte(`{env.DNSPOD_TOKEN}`)) || bytes.Contains(raw, []byte(`CLOUDFLARE_API_TOKEN`)) {
		t.Fatal(string(raw))
	}
}
