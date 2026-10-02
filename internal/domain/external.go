package domain

import "encoding/json"

// PreserveExternalSettings retains only infrastructure settings owned by an
// external Caddy. Unknown application configuration is deliberately replaced.
func PreserveExternalSettings(candidate, running []byte) ([]byte, error) {
	var next, current map[string]json.RawMessage
	if err := json.Unmarshal(candidate, &next); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(running, &current); err != nil {
		return nil, err
	}
	for _, key := range []string{"admin", "storage"} {
		delete(next, key)
		if value, exists := current[key]; exists {
			next[key] = value
		}
	}
	return json.MarshalIndent(next, "", "  ")
}
