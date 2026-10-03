package caddy

import (
	"os"
	"strings"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type SecretFile struct{ Path string }

func (secret *SecretFile) ReadCloudflareToken() (string, error) {
	raw, err := os.ReadFile(secret.Path)
	if err != nil {
		return "", domain.Invalid("Cloudflare Token 尚未配置，请先在设置中保存")
	}
	token := strings.TrimSpace(string(raw))
	if err = domain.ValidateCloudflareToken(token); err != nil {
		return "", err
	}
	return token, nil
}

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
