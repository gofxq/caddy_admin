package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gofxq/caddy_admin/internal/domain"
)

const redactedConfigValue = "[REDACTED]"

// Display copies never participate in validation, fingerprinting or publication.
// Unknown control/storage modules are opaque: guessing their credential fields
// could disclose secrets. Keep their shape, but hide their leaf values.
func configForDisplay(raw json.RawMessage) (json.RawMessage, []string, error) {
	if !json.Valid(raw) {
		return nil, nil, fmt.Errorf("invalid Caddy configuration JSON")
	}
	var config map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&config); err != nil {
		return nil, nil, err
	}
	if config == nil {
		return nil, nil, fmt.Errorf("invalid Caddy configuration object")
	}
	redacted := []string{}
	var walk func(any, string, bool) any
	walk = func(value any, path string, opaque bool) any {
		switch node := value.(type) {
		case map[string]any:
			inheritedOpaque := opaque
			// Selectors identify modules; everything else in an unknown module is hidden.
			for _, selector := range []string{"module", "handler", "protocol", "format", "via", "output", "filter"} {
				if name, ok := node[selector].(string); ok && !displayModuleKnown(selector, name) {
					opaque = true
				}
			}
			if strings.HasSuffix(path, "/provider") && node["name"] != "cloudflare" {
				opaque = true
			}
			for key, child := range node {
				childPath := path + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
				hide := opaque || sensitiveConfigKey(key) || opaqueConfigBranch(path, key)
				if path == "" && key != "admin" && key != "storage" && key != "apps" && key != "logging" {
					hide = true
				}
				if path == "/apps" && key != "http" && key != "tls" && key != "pki" && key != "caddy_admin_observability" {
					hide = true
				}
				if path == "/admin" {
					hide = hide || key != "listen" && key != "disabled" && key != "enforce_origin" && key != "origins" && key != "config"
				}
				if path == "/admin/config" {
					hide = hide || key != "persist"
				}
				if path == "/storage" {
					hide = hide || key != "module" && !(node["module"] == "file_system" && key == "root")
				}
				// Preserve module identifiers even when their settings are opaque.
				if _, isString := child.(string); isString && key == displayModuleSelector(path) && !inheritedOpaque {
					hide = false
				}
				node[key] = walk(child, childPath, hide)
			}
			return node
		case []any:
			for index, child := range node {
				node[index] = walk(child, path+"/"+strconv.Itoa(index), opaque)
			}
			return node
		default:
			hide := opaque
			if text, ok := node.(string); ok {
				hide = hide || strings.Contains(text, "PRIVATE KEY-----")
				if parsed, err := url.Parse(text); err == nil && parsed.Scheme != "" && (parsed.User != nil || parsed.RawQuery != "") {
					hide = true
				}
			}
			if hide && node != nil {
				redacted = append(redacted, path)
				return redactedConfigValue
			}
			return node
		}
	}
	walk(config, "", false)
	sort.Strings(redacted)
	display, err := json.Marshal(config)
	return display, redacted, err
}

func sensitiveConfigKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "_", ".", "_").Replace(key))
	for _, part := range []string{"password", "passwd", "secret", "token", "credential", "authorization", "cookie", "private_key", "access_key", "api_key"} {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	if strings.HasSuffix(normalized, "_key") || strings.HasSuffix(normalized, "_keys") {
		return true
	}
	switch normalized {
	case "key", "pem", "load_pem", "authentication", "client_certificate", "client_certificate_file", "client_certificate_key_file":
		return true
	}
	return false
}

func displayModuleKnown(selector, name string) bool {
	switch selector {
	case "module":
		return name == "file_system" || name == "acme" || name == "internal"
	case "handler":
		switch name {
		case "headers", "subroute", "reverse_proxy", "vars", "rewrite", "file_server", "static_response", "encode", "admin_observe":
			return true
		}
	case "protocol":
		return name == "http"
	case "format":
		return name == "json" || name == "console" || name == "admin_diagnostics"
	case "output":
		return name == "stdout" || name == "stderr" || name == "discard" || name == "file"
	}
	return false
}

// Only selectors at actual supported module boundaries are public identifiers.
// A similarly named field inside an opaque module is still a private parameter.
func displayModuleSelector(path string) string {
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]
	parent := ""
	if len(parts) > 1 {
		parent = parts[len(parts)-2]
	}
	switch {
	case path == "/storage" || parent == "issuers":
		return "module"
	case parent == "handle":
		return "handler"
	case last == "transport":
		return "protocol"
	case last == "encoder" || last == "wrap":
		return "format"
	case last == "writer":
		return "output"
	case last == "provider":
		return "name"
	}
	return ""
}

func opaqueConfigBranch(path, key string) bool {
	// These extension points are not generated by this application. Their module
	// schemas can contain arbitrarily named secrets, so hide parameters by default.
	switch key {
	case "get_certificate", "client_authentication", "encrypted_client_hello", "session_tickets", "trusted_proxies", "listener_wrappers", "packet_conn_wrappers", "dynamic_upstreams", "circuit_breaker", "load_balancing", "network_proxy", "handshake_context", "permission", "precompressed", "core":
		return true
	case "storage":
		return path != ""
	}
	if strings.HasSuffix(path, "/transport/tls") && key == "ca" {
		return true
	}
	if path == "/apps/tls" && key == "dns" {
		return true
	}
	if path == "/apps/tls/certificates" {
		return key != "automate" && key != "load_files"
	}
	if strings.HasPrefix(path, "/apps/http/") {
		parts := strings.Split(path, "/")
		parent := ""
		if len(parts) > 1 {
			parent = parts[len(parts)-2]
		}
		if parent == "match" || parent == "not" || strings.HasSuffix(path, "/match") {
			switch key {
			case "host", "path", "not", "remote_ip", "file", "expression", "sni":
				return false
			}
			return true
		}
		if strings.HasSuffix(path, "/encodings") {
			return key != "gzip" && key != "zstd"
		}
	}
	return false
}

type displayPreview struct {
	domain.Preview
	RuntimeConfig                json.RawMessage `json:"runtime_config"`
	ConfigRedactedFields         []string        `json:"config_redacted_fields"`
	RuntimeConfigRedactedFields  []string        `json:"runtime_config_redacted_fields"`
	RollbackConfigRedactedFields []string        `json:"rollback_config_redacted_fields,omitempty"`
}

func previewForDisplay(preview domain.Preview) (displayPreview, error) {
	result := displayPreview{Preview: preview}
	var err error
	result.Config, result.ConfigRedactedFields, err = configForDisplay(preview.Config)
	if err == nil && len(preview.RuntimeConfig) > 0 {
		result.RuntimeConfig, result.RuntimeConfigRedactedFields, err = configForDisplay(preview.RuntimeConfig)
	}
	if err == nil && len(preview.RollbackConfig) > 0 {
		result.RollbackConfig, result.RollbackConfigRedactedFields, err = configForDisplay(preview.RollbackConfig)
	}
	return result, err
}
