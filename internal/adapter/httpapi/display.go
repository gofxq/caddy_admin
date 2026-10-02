package httpapi

import (
	"encoding/json"

	"github.com/gofxq/caddy_admin/internal/domain"
)

func configForDisplay(raw json.RawMessage) (json.RawMessage, []string, error) {
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, nil, err
	}
	redacted := []string{}
	for _, key := range []string{"admin", "storage"} {
		if _, exists := config[key]; exists {
			delete(config, key)
			redacted = append(redacted, key)
		}
	}
	display, err := json.Marshal(config)
	return display, redacted, err
}

type displayPreview struct {
	domain.Preview
	ConfigRedactedFields         []string `json:"config_redacted_fields"`
	RollbackConfigRedactedFields []string `json:"rollback_config_redacted_fields,omitempty"`
}

func previewForDisplay(preview domain.Preview) (displayPreview, error) {
	result := displayPreview{Preview: preview}
	var err error
	result.Config, result.ConfigRedactedFields, err = configForDisplay(preview.Config)
	if err == nil && len(preview.RollbackConfig) > 0 {
		result.RollbackConfig, result.RollbackConfigRedactedFields, err = configForDisplay(preview.RollbackConfig)
	}
	return result, err
}
