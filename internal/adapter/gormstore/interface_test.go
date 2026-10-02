package gormstore_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gofxq/caddy_admin/internal/adapter/gormstore"
	"github.com/gofxq/caddy_admin/internal/application"
)

var _ application.Repository = (*gormstore.Store)(nil)

func TestStoreImplementsRepository(t *testing.T) {
	store, err := gormstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}
