package caddy

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/gofxq/caddy_admin/internal/application"
)

type Snapshot struct{ Path string }

var _ application.SnapshotStore = (*Snapshot)(nil)

func (snapshot *Snapshot) Exists() (bool, error) {
	_, err := os.Stat(snapshot.Path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (snapshot *Snapshot) Read() ([]byte, error)  { return os.ReadFile(snapshot.Path) }
func (snapshot *Snapshot) Write(raw []byte) error { return AtomicWrite(snapshot.Path, raw) }
func (snapshot *Snapshot) Remove() error {
	err := os.Remove(snapshot.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func AtomicWrite(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(raw)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
