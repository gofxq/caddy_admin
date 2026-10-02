package application

import (
	"context"
	"testing"
)

func TestSetupPasswordMinimumCharacters(t *testing.T) {
	for _, tt := range []struct {
		name, password string
		invalid        bool
	}{
		{"seven ASCII", "1234567", true},
		{"eight ASCII", "12345678", false},
		{"seven Unicode", "一二三四五六七", true},
		{"eight Unicode", "一二三四五六七八", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repository := &setupRepository{}
			snapshot := &setupSnapshot{}
			service := New(Options{TestTLS: true, ManagerDial: "127.0.0.1:8080", HTTPPort: "80", HTTPSPort: "443"}, repository, &setupCaddy{}, Dependencies{Snapshot: snapshot, SetupProbe: setupProbe{resolver: true, dns: true}})
			_, err := service.CompleteSetup(context.Background(), SetupRequest{Username: "admin", Password: tt.password, Settings: validSetupSettings()})
			if (err != nil) != tt.invalid {
				t.Fatalf("error = %v, want invalid %v", err, tt.invalid)
			}
			if tt.invalid && (repository.completed || snapshot.written) {
				t.Fatal("invalid password wrote setup state")
			}
			if !tt.invalid && (!repository.completed || !snapshot.written) {
				t.Fatal("valid password did not finish setup")
			}
		})
	}
}
