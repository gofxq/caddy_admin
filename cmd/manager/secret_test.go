package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCloudflareSecretUsesDefaultAndCustomFile(t *testing.T) {
	for _, custom := range []bool{false, true} {
		name := "default"
		if custom {
			name = "custom"
		}
		t.Run(name, func(t *testing.T) {
			dataDir := t.TempDir()
			path := filepath.Join(dataDir, "secrets", "cloudflare_token")
			t.Setenv("DATA_DIR", dataDir)
			t.Setenv("CLOUDFLARE_API_TOKEN_FILE", "")
			t.Setenv("CLOUDFLARE_API_TOKEN", "")
			if custom {
				path = filepath.Join(dataDir, "custom_token")
				t.Setenv("CLOUDFLARE_API_TOKEN_FILE", path)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("test-only-token\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Run("entrypoint", func(t *testing.T) {
				cmd := exec.Command("sh", "../../deploy/entrypoint.sh", "sh", "-c", `test "$CLOUDFLARE_API_TOKEN" = test-only-token`)
				if err := cmd.Run(); err != nil {
					t.Fatal("entrypoint did not pass the file token to its child process")
				}
			})
			t.Run("manager", func(t *testing.T) {
				if err := loadCloudflareSecret(); err != nil {
					t.Fatal(err)
				}
				if os.Getenv("CLOUDFLARE_API_TOKEN") != "test-only-token" {
					t.Fatal("manager did not load the file token")
				}
			})
		})
	}
}

func TestCloudflareSecretMissingDefaultFileFails(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("CLOUDFLARE_API_TOKEN_FILE", "")
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	if err := loadCloudflareSecret(); err == nil {
		t.Fatal("missing token file must fail when a Cloudflare secret is required")
	}
}
