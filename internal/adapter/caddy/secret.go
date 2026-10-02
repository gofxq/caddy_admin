package caddy

import (
	"os"
	"strings"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type SecretFile struct{ Path string }

var _ application.SecretStore = (*SecretFile)(nil)

func (secret *SecretFile) WriteCloudflareToken(token string) error {
	if err := domain.ValidateCloudflareToken(token); err != nil {
		return err
	}
	return AtomicWrite(secret.Path, []byte(token+"\n"))
}

func (secret *SecretFile) CloudflareTokenConfigured() bool {
	raw, err := os.ReadFile(secret.Path)
	return err == nil && domain.ValidateCloudflareToken(strings.TrimSpace(string(raw))) == nil
}
