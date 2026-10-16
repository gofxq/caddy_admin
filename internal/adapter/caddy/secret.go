package caddy

import (
	"os"
	"strings"

	"github.com/gofxq/caddy_admin/internal/application"
	"github.com/gofxq/caddy_admin/internal/domain"
)

type SecretFile struct {
	Path       string
	DNSPodPath string
}

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

func (secret *SecretFile) tokenPath(provider domain.DNSProvider) string {
	if provider == domain.DNSProviderDNSPod {
		return secret.DNSPodPath
	}
	return secret.Path
}

func (secret *SecretFile) ReadDNSToken(provider domain.DNSProvider) (string, error) {
	raw, err := os.ReadFile(secret.tokenPath(provider))
	if err != nil {
		return "", domain.Invalid("DNS Token 尚未配置，请先在设置中保存")
	}
	token := strings.TrimSpace(string(raw))
	if err := domain.ValidateDNSToken(provider, token); err != nil {
		return "", err
	}
	return token, nil
}

func (secret *SecretFile) WriteDNSToken(provider domain.DNSProvider, token string) error {
	if err := domain.ValidateDNSToken(provider, token); err != nil {
		return err
	}
	return AtomicWrite(secret.tokenPath(provider), []byte(token+"\n"))
}

func (secret *SecretFile) DNSTokenConfigured(provider domain.DNSProvider) bool {
	_, err := secret.ReadDNSToken(provider)
	return err == nil
}
