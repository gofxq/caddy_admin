package domain_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/gofxq/caddy_admin/internal/domain"
)

type policyResolver struct {
	addresses []netip.Addr
	err       error
}

func (r policyResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r.addresses, r.err
}

func openTargetPolicy() domain.TargetPolicy {
	return domain.TargetPolicy{Domains: []domain.ManagedDomain{{ID: "home", Name: "example.com", Access: domain.DomainAccess("internet")}}, AdminDomain: "caddyadmin.example.com", UpstreamCIDRs: []string{"0.0.0.0/0", "::/0"}, AllowedNames: []string{"*"}}
}

func TestAllUpstreamNamesRequireValidNamesAndSafeAddresses(t *testing.T) {
	for _, tc := range []struct {
		name string
		host string
		ips  []string
		err  error
		want bool
	}{
		{name: "Docker service", host: "photos", ips: []string{"172.20.0.8"}, want: true},
		{name: "private DNS", host: "photos.internal", ips: []string{"10.0.0.8"}, want: true},
		{name: "public DNS and IPv6", host: "app.example.net", ips: []string{"2001:db8::8", "203.0.113.8"}, want: true},
		{name: "invalid name", host: "bad/name", ips: []string{"10.0.0.8"}},
		{name: "literal wildcard", host: "*", ips: []string{"10.0.0.8"}},
		{name: "empty DNS", host: "photos.internal"},
		{name: "DNS failure", host: "photos.internal", err: errors.New("DNS offline")},
		{name: "mixed safe and loopback", host: "photos.internal", ips: []string{"10.0.0.8", "127.0.0.1"}},
		{name: "mixed safe and metadata", host: "photos.internal", ips: []string{"10.0.0.8", "169.254.169.254"}},
		{name: "system IP via DNS", host: "photos.internal", ips: []string{"10.0.0.2"}},
		{name: "denied IP via DNS", host: "photos.internal", ips: []string{"10.0.0.9"}},
		{name: "admin name", host: "caddyadmin.example.com", ips: []string{"10.0.0.8"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := openTargetPolicy()
			p.ReservedIPs, p.DeniedIPs = []string{"10.0.0.2"}, []string{"10.0.0.9"}
			r := policyResolver{err: tc.err}
			for _, ip := range tc.ips {
				r.addresses = append(r.addresses, netip.MustParseAddr(ip))
			}
			p.Resolver = r
			s, err := domain.NormalizeService(context.Background(), p, domain.Service{Name: "Photos", DomainID: "home", Hostname: "photos.example.com", Scheme: "https", Host: tc.host, Port: 8443, Enabled: true})
			if (err == nil) != tc.want || (tc.want && s.Dial == "") {
				t.Fatalf("normalize = %+v, %v; want allowed=%v", s, err, tc.want)
			}
		})
	}
}

func TestOpenIPPolicyStillProtectsSystemAndSpecialTargets(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "0.0.0.0", "::", "169.254.169.254", "100.100.100.200", "fd00:ec2::254", "fe80::8", "224.0.0.1", "ff02::1", "255.255.255.255", "10.0.0.2", "10.0.0.9"} {
		p := openTargetPolicy()
		p.ReservedIPs, p.DeniedIPs = []string{"10.0.0.2"}, []string{"10.0.0.9"}
		if _, err := domain.NormalizeService(context.Background(), p, domain.Service{Name: "Photos", DomainID: "home", Hostname: "photos.example.com", Scheme: "http", Host: ip, Port: 80}); err == nil {
			t.Errorf("protected IP accepted: %s", ip)
		}
	}
	for _, ip := range []string{"192.168.1.8", "100.64.0.8", "203.0.113.8", "fd12::8", "2001:db8::8"} {
		if _, err := domain.NormalizeService(context.Background(), openTargetPolicy(), domain.Service{Name: "Photos", DomainID: "home", Hostname: "photos.example.com", Scheme: "http", Host: ip, Port: 80}); err != nil {
			t.Errorf("safe IP refused: %s: %v", ip, err)
		}
	}
}

func TestUpstreamWildcardIsExplicitAndDoesNotBypassCIDRs(t *testing.T) {
	for _, tc := range []struct {
		names []string
		cidrs []string
		want  bool
	}{
		{[]string{"*"}, []string{"10.0.0.0/8"}, true},
		{[]string{"photos.internal"}, []string{"10.0.0.0/8"}, true},
		{[]string{"other.internal"}, []string{"10.0.0.0/8"}, false},
		{[]string{}, []string{"0.0.0.0/0"}, false},
		{[]string{"*"}, []string{}, false},
		{[]string{"*"}, []string{"192.168.0.0/16"}, false},
	} {
		p := openTargetPolicy()
		p.AllowedNames, p.UpstreamCIDRs = tc.names, tc.cidrs
		p.Resolver = policyResolver{addresses: []netip.Addr{netip.MustParseAddr("10.0.0.8")}}
		_, err := domain.NormalizeService(context.Background(), p, domain.Service{Name: "Photos", DomainID: "home", Hostname: "photos.example.com", Scheme: "http", Host: "photos.internal", Port: 80})
		if (err == nil) != tc.want {
			t.Errorf("names=%v cidrs=%v: %v", tc.names, tc.cidrs, err)
		}
	}
	for _, name := range []string{"*", "photos", "photos.internal", "*.example.com", "photo*", ""} {
		err := domain.ValidateManagedNetwork(domain.ManagedSettings{AllowedNames: []string{name}})
		want := name == "*" || name == "photos" || name == "photos.internal"
		if (err == nil) != want {
			t.Errorf("name policy %q = %v", name, err)
		}
	}
}
