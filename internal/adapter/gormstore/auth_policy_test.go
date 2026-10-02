package gormstore

import (
	"context"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

func TestPasswordHashLengthPolicy(t *testing.T) {
	for _, tt := range []struct {
		name, password string
		invalid        bool
	}{
		{"seven ASCII", "1234567", true},
		{"eight ASCII", "12345678", false},
		{"seven Unicode", "一二三四五六七", true},
		{"eight Unicode", "一二三四五六七八", false},
		{"maximum bytes", strings.Repeat("x", 256), false},
		{"too many bytes", strings.Repeat("x", 257), true},
		{"Unicode exceeds bytes", strings.Repeat("中", 86), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := passwordHash(tt.password)
			if (err != nil) != tt.invalid {
				t.Fatalf("error = %v, want invalid %v", err, tt.invalid)
			}
			if err == nil && !passwordMatches(hash, tt.password) {
				t.Fatal("accepted password cannot log in")
			}
		})
	}
}

func TestEightCharacterPasswordChangesRevokeSessions(t *testing.T) {
	for _, operation := range []string{"change", "reset"} {
		t.Run(operation, func(t *testing.T) {
			store := testStore(t)
			ctx := context.Background()
			if err := store.CompleteSetup(ctx, application.SetupCredentials{Username: "admin", Password: "initial8"}, validManagedSettings(), domain.CertificateStatus{Mode: domain.CertificateModeBootstrapInternal, ActivationStatus: "idle", PublicStatus: "unknown"}); err != nil {
				t.Fatal(err)
			}
			session, err := store.Login(ctx, "admin", "initial8")
			if err != nil {
				t.Fatal(err)
			}
			write := func(next string) error {
				if operation == "reset" {
					return store.ResetPassword(ctx, next)
				}
				return store.ChangePassword(ctx, "initial8", next)
			}
			if err := write("short77"); err == nil {
				t.Fatal("seven-character password accepted")
			}
			if _, err := store.Session(ctx, session.Token); err != nil {
				t.Fatal("failed change revoked original session", err)
			}
			if err := write("newpass8"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Session(ctx, session.Token); err == nil {
				t.Fatal("old session survived password change")
			}
			if _, err := store.Login(ctx, "admin", "initial8"); err == nil {
				t.Fatal("old password still works")
			}
			if _, err := store.Login(ctx, "admin", "newpass8"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
