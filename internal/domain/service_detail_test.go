package domain

import "testing"

func TestValidateServiceDial(t *testing.T) {
	policy := TargetPolicy{Domains: []ManagedDomain{{ID: "home", Name: "example.com"}}, AdminDomain: "admin.example.com", UpstreamCIDRs: []string{"10.0.0.0/8", "fd42::/16"}, AllowedNames: []string{"photos.internal"}, ReservedIPs: []string{"10.0.0.1"}}
	service := Service{ID: "photos", Name: "Photos", DomainID: "home", Hostname: "photos.example.com", Scheme: "http", Host: "photos.internal", Port: 2283, Enabled: true, Dial: "10.0.0.8:2283"}
	for _, tc := range []struct {
		name, host, dial string
		want             bool
	}{
		{"fixed named snapshot", "photos.internal", "10.0.0.8:2283", true},
		{"literal", "10.0.0.8", "10.0.0.8:2283", true},
		{"different literal", "10.0.0.9", "10.0.0.8:2283", false},
		{"port changed", "photos.internal", "10.0.0.8:8080", false},
		{"DNS name in dial", "photos.internal", "photos.internal:2283", false},
		{"unlicensed name", "other.internal", "10.0.0.8:2283", false},
		{"protected", "photos.internal", "10.0.0.1:2283", false},
		{"loopback", "photos.internal", "127.0.0.1:2283", false},
		{"metadata", "photos.internal", "169.254.169.254:2283", false},
		{"outside permit", "photos.internal", "192.168.0.8:2283", false},
		{"IPv6 snapshot", "photos.internal", "[fd42::8]:2283", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := service
			value.Host = tc.host
			value.Dial = tc.dial
			if err := ValidateServiceDial(policy, value); (err == nil) != tc.want {
				t.Fatalf("allowed=%v want=%v err=%v", err == nil, tc.want, err)
			}
		})
	}
	policy.DeniedIPs = []string{"10.0.0.8"}
	if err := ValidateServiceDial(policy, service); err == nil {
		t.Fatal("revoked address accepted")
	}
}
