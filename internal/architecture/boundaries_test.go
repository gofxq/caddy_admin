package architecture_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func projectRoot(t *testing.T) string {
	t.Helper()
	_, current, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
}

func TestNoCorePackageRemains(t *testing.T) {
	if _, err := os.Stat(filepath.Join(projectRoot(t), "internal", "core")); !os.IsNotExist(err) {
		t.Fatalf("legacy core package still exists: %v", err)
	}
}

func TestLayerDependencies(t *testing.T) {
	for packagePath, forbidden := range map[string][]string{
		"github.com/gofxq/caddy_admin/internal/domain":      {"internal/application", "internal/adapter", "gin-gonic", "gorm.io", "modernc.org", "libtnb/sqlite"},
		"github.com/gofxq/caddy_admin/internal/application": {"internal/adapter", "gin-gonic", "gorm.io", "modernc.org", "libtnb/sqlite"},
	} {
		command := exec.Command("go", "list", "-json", packagePath)
		command.Dir = projectRoot(t)
		raw, err := command.Output()
		if err != nil {
			t.Fatal(err)
		}
		var metadata struct{ Imports []string }
		if err = json.Unmarshal(raw, &metadata); err != nil {
			t.Fatal(err)
		}
		imports := strings.Join(metadata.Imports, "\n")
		for _, dependency := range forbidden {
			if strings.Contains(imports, dependency) {
				t.Errorf("%s imports forbidden dependency %s", packagePath, dependency)
			}
		}
	}
}

func TestOnlyManagerIsCompositionRoot(t *testing.T) {
	root := projectRoot(t)
	for _, directory := range []string{"internal/application", "internal/adapter/caddy", "internal/adapter/gormstore", "internal/adapter/httpapi"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			text := string(raw)
			if directory != "internal/adapter/caddy" && strings.Contains(text, "internal/adapter/caddy") || directory != "internal/adapter/gormstore" && strings.Contains(text, "internal/adapter/gormstore") || directory != "internal/adapter/httpapi" && strings.Contains(text, "internal/adapter/httpapi") {
				t.Errorf("%s constructs or imports a sibling adapter", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
