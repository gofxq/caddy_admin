package application_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/gofxq/caddy_admin/internal/application"
)

type fakeCaddy struct{}

func (*fakeCaddy) Read(context.Context) ([]byte, error)     { return []byte(`{}`), nil }
func (*fakeCaddy) Validate(context.Context, []byte) error   { return nil }
func (*fakeCaddy) Load(context.Context, []byte) error       { return nil }
func (*fakeCaddy) BuildInfo(context.Context) (string, bool) { return "test", true }

var _ application.CaddyPort = (*fakeCaddy)(nil)

func TestPortsAcceptInfrastructureImplementations(t *testing.T) {
	var port application.CaddyPort = &fakeCaddy{}
	raw, err := port.Read(context.Background())
	if err != nil || !json.Valid(raw) {
		t.Fatalf("Caddy port contract failed: %s %v", raw, err)
	}
	options := application.Options{DataDir: "/data", SnapshotDir: "/snapshots", AdminURL: "https://caddy:2019"}
	if options.DataDir == "" || options.SnapshotDir == "" || options.AdminURL == "" {
		t.Fatal("application options lost runtime values")
	}
}

func TestApplicationHasNoFrameworkImports(t *testing.T) {
	command := exec.Command("go", "list", "-json", "github.com/gofxq/caddy_admin/internal/application")
	raw, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Imports []string
	}
	if err = json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	productionImports := strings.Join(metadata.Imports, "\n")
	for _, forbidden := range []string{"/adapter/", "gin-gonic", "gorm.io", "modernc.org", "libtnb/sqlite"} {
		if strings.Contains(productionImports, forbidden) {
			t.Fatalf("application depends on outer layer %q", forbidden)
		}
	}
}
