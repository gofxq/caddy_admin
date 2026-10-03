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
			t.Setenv("CLOUDFLARE_API_TOKEN", "stale-deployment-token")
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
				cmd := exec.Command("sh", "../../deploy/entrypoint.sh", "sh", "-c", `test "$CLOUDFLARE_API_TOKEN" = stale-deployment-token`)
				if err := cmd.Run(); err != nil {
					t.Fatal("entrypoint overwrote preconfigured Setup token for Manager")
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
	t.Setenv("CLOUDFLARE_API_TOKEN", "stale-deployment-token")
	if err := loadCloudflareSecret(); err == nil {
		t.Fatal("missing token file must fail when a Cloudflare secret is required")
	}
}

func TestEntrypointLoadsPersistedTokenOnlyForDirectCaddy(t *testing.T) {
	dir := t.TempDir()
	for name, script := range map[string]string{
		"id":      "#!/bin/sh\necho 10001\n",
		"caddy":   "#!/bin/sh\ntest \"$CLOUDFLARE_API_TOKEN\" = persisted-runtime-token\n",
		"manager": "#!/bin/sh\ntest \"$CLOUDFLARE_API_TOKEN\" = deployment-setup-token\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	tokenPath := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenPath, []byte("persisted-runtime-token"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLOUDFLARE_API_TOKEN", "deployment-setup-token")
	t.Setenv("CLOUDFLARE_API_TOKEN_FILE", tokenPath)
	t.Setenv("TEST_TLS", "true")
	for _, command := range []string{"manager", "caddy"} {
		t.Run(command, func(t *testing.T) {
			if err := exec.Command("sh", "../../deploy/entrypoint.sh", command).Run(); err != nil {
				t.Fatalf("%s credential source was incorrect: %v", command, err)
			}
		})
	}
}
