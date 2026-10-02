package caddy

import (
	"encoding/json"
	"github.com/gofxq/caddy_admin/internal/application"
	"os"
)

type SetupIntentFile struct{ Path string }

func (f *SetupIntentFile) Read() (application.SetupIntent, error) {
	result := application.SetupIntent{}
	raw, err := os.ReadFile(f.Path)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result)
	return result, err
}
func (f *SetupIntentFile) Write(intent application.SetupIntent) error {
	raw, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	return AtomicWrite(f.Path, raw)
}
func (f *SetupIntentFile) Remove() error {
	err := os.Remove(f.Path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
