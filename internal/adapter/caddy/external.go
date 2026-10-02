package caddy

import "github.com/gofxq/caddy_admin/internal/domain"

// PreserveExternalSettings keeps infrastructure owned by an external Caddy
// while replacing the complete application configuration.
func PreserveExternalSettings(candidate, running []byte) ([]byte, error) {
	return domain.PreserveExternalSettings(candidate, running)
}
