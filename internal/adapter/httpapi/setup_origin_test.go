package httpapi

import "testing"

func TestSetupOriginAllowsPublicAndPrivateExactHost(t *testing.T) {
	for _, tc := range []struct {
		origin, host string
		allowed      bool
	}{
		{"http://192.168.1.6", "192.168.1.6", true},
		{"https://192.168.1.6", "192.168.1.6", true},
		{"http://localhost:8080", "localhost:8080", true},
		{"http://[fd12::6]", "[fd12::6]", true},
		{"http://203.0.113.6", "203.0.113.6", true},
		{"http://example.com", "example.com", true},
		{"https://example.com:8443", "example.com:8443", true},
		{"https://evil.example", "example.com", false},
		{"http://192.168.1.7", "192.168.1.6", false},
		{"http://192.168.1.6:81", "192.168.1.6", false},
		{"http://192.168.1.6/path", "192.168.1.6", false},
	} {
		if got := setupOriginMatches(tc.origin, tc.host); got != tc.allowed {
			t.Errorf("%q host %q = %v", tc.origin, tc.host, got)
		}
	}
}
