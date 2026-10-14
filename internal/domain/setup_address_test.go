package domain

import "testing"

func TestSetupAddressRequiresExplicitReachableUnicast(t *testing.T) {
	for _, value := range []string{"", " ", "0.0.0.0", "::", "127.0.0.1", "::1", "169.254.2.3", "fe80::1", "ff02::1", "224.0.0.1", "::ffff:10.0.0.1", "fe80::1%eth0", "https://10.0.0.1", "10.0.0.1:80"} {
		if _, err := NormalizeSetupAddress(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	for _, value := range []string{"10.0.0.6", "203.0.113.10", "fd00::6", "2001:db8::10"} {
		if got, err := NormalizeSetupAddress(" " + value + " "); err != nil || got != value {
			t.Errorf("address %q: %q %v", value, got, err)
		}
	}
}
