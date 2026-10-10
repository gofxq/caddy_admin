package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type object = map[string]any

const (
	ClientAddressHeader          = "X-Caddy-Client-IP"
	TemporaryAdminCertificateTag = "caddy-admin-temporary"
	SetupCertificateTag          = "caddy-admin-setup-entry"
	SetupBridgeAddress           = "127.0.0.1:8083"
	setupIPHostExpression        = `{http.request.host} == 'localhost' || {http.request.host}.matches('^[0-9]{1,3}([.][0-9]{1,3}){3}$') || {http.request.host}.contains(':')`
)

type CaddyConfig struct {
	MetricsEnabled            bool
	AccessLogsEnabled         bool
	AlertsEnabled             bool
	UpstreamChecksEnabled     bool
	Socket                    string
	AdminURL                  string
	Domains                   []ManagedDomain
	ConsoleLANOnly            bool
	PreviousAdminDomain       string
	AdminDomain               string
	LAN                       []string
	Resolvers                 []string
	ManagerDial               string
	StaticRoot                string
	CaddyStorage              string
	CertificateMode           CertificateMode
	TemporaryAdminCertificate bool
	TemporaryAdminCertPath    string
	TemporaryAdminKeyPath     string
	TestTLS                   bool
	HTTPPort                  string
	HTTPSPort                 string
	SetupCertPath             string
	SetupKeyPath              string
}

func Generate(config CaddyConfig, services []Service) ([]byte, error) {
	list := append([]Service{}, services...)
	sort.Slice(list, func(i, j int) bool { return list[i].Hostname < list[j].Hostname })
	if err := ValidateUniqueServices(list); err != nil {
		return nil, err
	}
	if (config.MetricsEnabled || config.AccessLogsEnabled || config.AlertsEnabled || config.UpstreamChecksEnabled) && (len(list) > 500 || len(config.Domains) > 64) {
		return nil, Invalid("观测最多支持 500 个服务和 64 个域名")
	}
	routes := []any{}
	observe := func(service Service) any {
		return object{"handler": "admin_observe", "service_id": service.ID, "hostname": service.Hostname}
	}
	observed := config.MetricsEnabled || config.AccessLogsEnabled
	deny := func(hosts []string) any {
		return object{"match": []any{object{"host": hosts, "not": []any{object{"remote_ip": object{"ranges": config.LAN}}}}}, "handle": []any{object{"handler": "static_response", "status_code": 403, "body": "Access restricted to configured LAN/VPN networks"}}, "terminal": true}
	}
	adminHosts := []string{config.AdminDomain}
	if config.PreviousAdminDomain != "" {
		adminHosts = append(adminHosts, config.PreviousAdminDomain)
	}
	if config.ConsoleLANOnly {
		if len(config.LAN) == 0 {
			return nil, Invalid("控制台来源限制需要可信网段")
		}
		routes = append(routes, deny(adminHosts))
	}
	for _, d := range config.Domains {
		if d.Access != nil && *d.Access == "trusted" {
			if len(config.LAN) == 0 {
				return nil, Invalid("仅可信网络的域名需要可信网段")
			}
			hosts := []string{}
			for _, service := range list {
				if service.Enabled && service.DomainID == d.ID {
					hosts = append(hosts, service.Hostname)
				}
			}
			if len(hosts) > 0 {
				if observed {
					for _, item := range list {
						if item.Enabled && item.DomainID == d.ID {
							route := deny([]string{item.Hostname}).(object)
							route["handle"] = append([]any{observe(item)}, route["handle"].([]any)...)
							routes = append(routes, route)
						}
					}
				} else {
					routes = append(routes, deny(hosts))
				}
			}
		}
	}
	adminRouteIndex := len(routes)
	security := object{"handler": "headers", "response": object{"set": object{"X-Content-Type-Options": []string{"nosniff"}, "Referrer-Policy": []string{"same-origin"}, "X-Frame-Options": []string{"DENY"}, "Content-Security-Policy": []string{"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"}}}}
	managerProxy := object{"handler": "reverse_proxy", "headers": object{"request": object{"set": object{ClientAddressHeader: []string{"{http.request.remote.host}"}}}}, "upstreams": []any{object{"dial": config.ManagerDial}}}
	routes = append(routes, object{"match": []any{object{"host": adminHosts}}, "handle": []any{security, object{"handler": "subroute", "routes": []any{
		object{"match": []any{object{"path": []string{"/api", "/api/*"}}}, "handle": []any{managerProxy}, "terminal": true},
		object{"handle": []any{object{"handler": "vars", "root": config.StaticRoot}}},
		object{"match": []any{object{"file": object{"try_files": []string{"{http.request.uri.path}", "/index.html"}}}}, "handle": []any{object{"handler": "rewrite", "uri": "{http.matchers.file.relative}"}}},
		object{"handle": []any{object{"handler": "file_server"}}},
	}}}, "terminal": true})
	if config.AdminURL != "" {
		routes[adminRouteIndex] = object{"match": []any{object{"host": adminHosts}}, "handle": []any{security, managerProxy}, "terminal": true}
	}
	for _, service := range list {
		var selected *ManagedDomain
		for i := range config.Domains {
			if config.Domains[i].ID == service.DomainID {
				selected = &config.Domains[i]
				break
			}
		}
		if selected == nil || !OneLevel(service.Hostname, selected.Name) || service.Hostname == config.AdminDomain || (config.PreviousAdminDomain != "" && service.Hostname == config.PreviousAdminDomain) {
			return nil, Invalid("禁止生成不受管或保留域名")
		}
		if !service.Enabled {
			continue
		}
		if selected.Access == nil || (*selected.Access != "trusted" && *selected.Access != "internet") {
			return nil, Invalid("发布启用服务前必须配置域名访问范围")
		}
		if service.Dial == "" {
			return nil, fmt.Errorf("missing validated upstream")
		}
		proxy := object{"handler": "reverse_proxy", "upstreams": []any{object{"dial": service.Dial}}}
		if service.Scheme == "https" {
			proxy["transport"] = object{"protocol": "http", "tls": object{"server_name": service.Host}}
		}
		handlers := []any{proxy}
		if observed {
			handlers = append([]any{observe(service)}, handlers...)
		}
		routes = append(routes, object{"match": []any{object{"host": []string{service.Hostname}}}, "handle": handlers, "terminal": true})
	}
	routes = append(routes, object{"handle": []any{object{"handler": "static_response", "status_code": 404, "body": "Not found"}}, "terminal": true})
	subjects := []string{}
	for _, d := range config.Domains {
		subjects = append(subjects, "*."+d.Name)
	}
	sort.Strings(subjects)
	issuer := object{"module": "acme", "challenges": object{"dns": object{"provider": object{"name": "cloudflare", "api_token": "{env.CLOUDFLARE_API_TOKEN}"}, "resolvers": config.Resolvers}}}
	if config.TestTLS || config.CertificateMode == CertificateModeBootstrapInternal {
		issuer = object{"module": "internal"}
	} else if config.CertificateMode != "" && config.CertificateMode != CertificateModeCloudflare {
		return nil, Invalid("无效证书策略")
	}
	httpPort, err := strconv.Atoi(config.HTTPPort)
	if err != nil {
		return nil, err
	}
	httpsPort, err := strconv.Atoi(config.HTTPSPort)
	if err != nil {
		return nil, err
	}
	certificates := object{"automate": subjects}
	setupEntry := config.AdminURL == "" && config.SetupCertPath != "" && config.SetupKeyPath != ""

	tlsPolicies := []any{object{}}
	if setupEntry {
		certificates["load_files"] = []any{object{"certificate": config.SetupCertPath, "key": config.SetupKeyPath, "tags": []string{SetupCertificateTag}}}
		tlsPolicies = []any{object{"match": object{"sni": subjects}}, object{"default_sni": "127.0.0.1", "certificate_selection": object{"any_tag": []string{SetupCertificateTag}}}}
		// Only IP literals and localhost may reach the temporary management
		// entry. Unknown domain names continue to end at the explicit 404.
		setupRoute := object{"match": []any{object{"expression": setupIPHostExpression}}, "handle": []any{security, object{"handler": "subroute", "routes": []any{
			object{"handle": []any{object{"handler": "reverse_proxy", "headers": object{"request": object{"set": object{ClientAddressHeader: []string{"{http.request.remote.host}"}}}}, "upstreams": []any{object{"dial": SetupBridgeAddress}}}}, "terminal": true},
		}}}, "terminal": true}
		routes = append(routes[:len(routes)-1], setupRoute, routes[len(routes)-1])
	}
	if config.CertificateMode == CertificateModeCloudflare && config.TemporaryAdminCertificate && !config.TestTLS && config.AdminURL == "" {
		files, _ := certificates["load_files"].([]any)
		certificates["load_files"] = append(files, temporaryAdminCertificateLoader(config)...)
	}
	result := object{
		"admin":   object{"listen": "unix/" + config.Socket, "config": object{"persist": false}},
		"storage": object{"module": "file_system", "root": config.CaddyStorage},
		"apps": object{
			"http": object{"http_port": httpPort, "https_port": httpsPort, "servers": object{"managed": object{"listen": []string{":" + config.HTTPSPort}, "routes": routes, "tls_connection_policies": tlsPolicies, "automatic_https": object{"disable_certificates": true}}}},
			"tls":  object{"certificates": certificates, "automation": object{"policies": []any{object{"subjects": subjects, "issuers": []any{issuer}}}}},
		},
	}
	if observed {
		result["apps"].(object)["caddy_admin_observability"] = object{"metrics": config.MetricsEnabled, "access_logs": config.AccessLogsEnabled}
		if config.AccessLogsEnabled {
			result["logging"] = object{"logs": object{"default": object{"exclude": []string{"tls.obtain", "tls.issuance"}}, "admin_diagnostics": object{"include": []string{"tls.obtain", "tls.issuance"}, "encoder": object{"format": "admin_diagnostics"}}}}
		}
	}
	if setupEntry {
		location := "https://{http.request.host}"
		ipv6Location := "https://[{http.request.host}]"
		if config.HTTPSPort != "443" {
			location += ":" + config.HTTPSPort
			ipv6Location += ":" + config.HTTPSPort
		}
		redirectHosts := append([]string{}, adminHosts...)
		for _, service := range list {
			if service.Enabled {
				redirectHosts = append(redirectHosts, service.Hostname)
			}
		}
		result["apps"].(object)["http"].(object)["servers"].(object)["setup_redirect"] = object{"listen": []string{":" + config.HTTPPort}, "routes": []any{
			object{"match": []any{object{"expression": `{http.request.host}.contains(':') && !{http.request.host}.startsWith('[')`, "method": []string{"GET", "HEAD"}}}, "handle": []any{object{"handler": "static_response", "status_code": 302, "headers": object{"Location": []string{ipv6Location + "/setup"}}}}, "terminal": true},
			object{"match": []any{object{"expression": setupIPHostExpression, "method": []string{"GET", "HEAD"}}}, "handle": []any{object{"handler": "static_response", "status_code": 302, "headers": object{"Location": []string{location + "/setup"}}}}, "terminal": true},
			object{"match": []any{object{"host": redirectHosts}}, "handle": []any{object{"handler": "static_response", "status_code": 308, "headers": object{"Location": []string{location + "{http.request.uri}"}}}}, "terminal": true},
			object{"handle": []any{object{"handler": "static_response", "status_code": 404}}, "terminal": true},
		}, "automatic_https": object{"disable": true}}
	}
	if config.TestTLS {
		result["apps"].(object)["pki"] = object{"certificate_authorities": object{"local": object{"install_trust": false}}}
	}
	if config.AdminURL != "" {
		delete(result, "admin")
		delete(result, "storage")
	}
	return json.MarshalIndent(result, "", "  ")
}

type temporaryCertificateFile struct {
	Certificate string   `json:"certificate"`
	Key         string   `json:"key"`
	Tags        []string `json:"tags"`
}

func temporaryAdminCertificateLoader(config CaddyConfig) []any {
	return []any{object{"certificate": config.TemporaryAdminCertPath, "key": config.TemporaryAdminKeyPath, "tags": []string{TemporaryAdminCertificateTag}}}
}

func temporaryAdminLoaderMatches(existing any, config CaddyConfig) (bool, bool, error) {
	encoded, err := json.Marshal(existing)
	if err != nil {
		return false, false, err
	}
	var files []temporaryCertificateFile
	if err = json.Unmarshal(encoded, &files); err != nil {
		return false, false, Invalid("手工证书配置无效")
	}
	hasTag := strings.Contains(string(encoded), TemporaryAdminCertificateTag)
	filtered := []temporaryCertificateFile{}
	for _, file := range files {
		if !setupCertificateMatches(file, config) {
			filtered = append(filtered, file)
		}
	}
	exact := len(filtered) == 1 && filtered[0].Certificate == config.TemporaryAdminCertPath && filtered[0].Key == config.TemporaryAdminKeyPath && len(filtered[0].Tags) == 1 && filtered[0].Tags[0] == TemporaryAdminCertificateTag
	return exact, hasTag, nil
}

func setupCertificateMatches(file temporaryCertificateFile, config CaddyConfig) bool {
	return config.SetupCertPath != "" && file.Certificate == config.SetupCertPath && file.Key == config.SetupKeyPath && len(file.Tags) == 1 && file.Tags[0] == SetupCertificateTag
}

func RemoveTemporaryAdminCertificate(raw []byte, config CaddyConfig) ([]byte, bool, error) {
	root, certificates, err := certificateConfig(raw)
	if err != nil {
		return nil, false, err
	}
	existing, exists := certificates["load_files"]
	if !exists {
		return raw, false, nil
	}
	exact, hasTag, err := temporaryAdminLoaderMatches(existing, config)
	if err != nil {
		return nil, false, err
	}
	if !exact {
		if hasTag {
			return nil, false, Conflict("临时管理证书与未知手工证书混合，拒绝自动移除")
		}
		return raw, false, nil
	}
	encoded, _ := json.Marshal(existing)
	var files []temporaryCertificateFile
	_ = json.Unmarshal(encoded, &files)
	kept := []temporaryCertificateFile{}
	for _, file := range files {
		if setupCertificateMatches(file, config) {
			kept = append(kept, file)
		}
	}
	if len(kept) == 0 {
		delete(certificates, "load_files")
	} else {
		certificates["load_files"] = kept
	}
	updated, err := json.MarshalIndent(root, "", "  ")
	return updated, true, err
}

func certificateConfig(raw []byte) (map[string]any, map[string]any, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, nil, Invalid("无效 Caddy 启动快照")
	}
	apps, ok := root["apps"].(map[string]any)
	if !ok {
		return nil, nil, Invalid("Caddy 启动快照缺少 apps")
	}
	tlsApp, ok := apps["tls"].(map[string]any)
	if !ok {
		return nil, nil, Invalid("Caddy 启动快照缺少 TLS 配置")
	}
	certificates, ok := tlsApp["certificates"].(map[string]any)
	if !ok {
		return nil, nil, Invalid("Caddy 启动快照缺少证书配置")
	}
	return root, certificates, nil
}
