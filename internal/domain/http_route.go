package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Business routes run inside the generated hostname and access-control route.
// Only handlers with known effects may execute inside that boundary.
type routeNormalizer struct {
	ctx       context.Context
	policy    TargetPolicy
	service   Service
	needsRoot bool
}

func normalizeHTTPRoute(ctx context.Context, policy TargetPolicy, service Service, raw json.RawMessage) (json.RawMessage, error) {
	var routes []any
	if len(raw) > 256*1024 || !json.Valid(raw) {
		return nil, Invalid("HTTP 路由 JSON 无效或过大")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&routes) != nil || len(routes) == 0 {
		return nil, Invalid("HTTP 路由不能为空")
	}
	if err := checkRouteValues(routes, 0); err != nil {
		return nil, err
	}
	n := routeNormalizer{ctx: ctx, policy: policy, service: service}
	if err := eachObject(routes, n.route); err != nil {
		return nil, err
	}
	if n.needsRoot {
		root := "/srv/sites/" + service.Hostname
		if err := checkSiteRoot(root); err != nil {
			return nil, err
		}
		routes = append([]any{object{"handle": []any{object{"handler": "vars", "root": root}}}}, routes...)
	}
	return json.Marshal(routes)
}

// Inspect decoded keys and values so JSON escapes cannot change placeholder policy.
var routePlaceholder = regexp.MustCompile(`\{([^{}]*)\}`)
var safeRoutePlaceholder = regexp.MustCompile(`^(?:http\.request\.(?:uri(?:\.(?:path|query)(?:\.[A-Za-z0-9_-]+)?)?|orig_uri(?:\.(?:path|query))?|host(?:port)?|port|method|scheme|proto|remote(?:\.host|\.port)?|client_ip|header\.[A-Za-z0-9_-]+)|http\.matchers\.file\.(?:relative|absolute|type|remainder)|http\.regexp\.[A-Za-z0-9_.-]+|http\.reverse_proxy\.upstream\.(?:hostport|host|port)|[0-9]+(?:,[0-9]*)?)$`)

func checkRouteValues(value any, depth int) error {
	if depth > 64 {
		return Invalid("HTTP 路由嵌套过深")
	}
	switch v := value.(type) {
	case object:
		for k, child := range v {
			if strings.EqualFold(k, "@id") {
				return Invalid("服务路由不支持配置对象 ID")
			}
			if err := checkRouteValues(k, depth+1); err != nil {
				return err
			}
			if err := checkRouteValues(child, depth+1); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := checkRouteValues(child, depth+1); err != nil {
				return err
			}
		}
	case string:
		for _, match := range routePlaceholder.FindAllStringSubmatch(v, -1) {
			// Literal JSON objects in response bodies also contain braces.
			// The regexp visits innermost braces, including placeholders
			// inside quoted JSON string values.
			if !safeRoutePlaceholder.MatchString(match[1]) && !json.Valid([]byte(match[0])) {
				return Invalid("服务路由不支持变量 " + match[0])
			}
		}
	}
	return nil
}

func routeFields(v any, allowed string) (object, error) {
	o, ok := v.(object)
	if !ok {
		return nil, Invalid("HTTP 路由选项必须是对象")
	}
	fields := strings.Fields(allowed)
	for key := range o {
		if !slices.Contains(fields, key) {
			return nil, Invalid("服务路由不支持选项 " + key)
		}
	}
	return o, nil
}
func eachObject(v any, visit func(object) error) error {
	items, ok := v.([]any)
	if !ok {
		return Invalid("HTTP 路由选项必须是数组")
	}
	for _, item := range items {
		o, ok := item.(object)
		if !ok {
			return Invalid("HTTP 路由元素必须是对象")
		}
		if err := visit(o); err != nil {
			return err
		}
	}
	return nil
}
func (n *routeNormalizer) route(o object) error {
	if _, err := routeFields(o, "match handle group terminal"); err != nil {
		return err
	}
	if m, ok := o["match"]; ok {
		if err := eachObject(m, n.matcher); err != nil {
			return err
		}
	}
	if h, ok := o["handle"]; ok {
		return eachObject(h, n.handler)
	}
	return nil
}
func checkSiteRoot(v any) error {
	root, _ := v.(string)
	return (&StaticSite{Root: root}).validate()
}
func (n *routeNormalizer) fileRoot(o object) error {
	if v, ok := o["root"]; ok {
		return checkSiteRoot(v)
	}
	n.needsRoot = true
	return nil
}
func (n *routeNormalizer) matcher(o object) error {
	if _, err := routeFields(o, "host path path_regexp header header_regexp method query protocol remote_ip client_ip not file"); err != nil {
		return err
	}
	if f, ok := o["file"]; ok {
		m, err := routeFields(f, "root try_files try_policy split_path")
		if err != nil {
			return err
		}
		if err = n.fileRoot(m); err != nil {
			return err
		}
	}
	if negated, ok := o["not"]; ok {
		return eachObject(negated, n.matcher)
	}
	return nil
}
func (n *routeNormalizer) handler(o object) error {
	kind, _ := o["handler"].(string)
	var fields string
	switch kind {
	case "subroute":
		fields = "routes"
	case "vars":
		fields = "root"
	case "headers":
		fields = "request response"
	case "rewrite":
		fields = "method uri strip_path_prefix strip_path_suffix uri_substring path_regexp query"
	case "static_response":
		fields = "status_code headers body close abort"
	case "encode":
		fields = "encodings prefer minimum_length match"
	case "file_server":
		fields = "root hide index_names canonical_uris pass_thru browse status_code"
	case "reverse_proxy":
		fields = "upstreams transport headers load_balancing flush_interval stream_timeout stream_close_delay rewrite"
	default:
		return Invalid("服务路由不支持处理器 " + kind)
	}
	if _, err := routeFields(o, "handler "+fields); err != nil {
		return err
	}
	switch kind {
	case "subroute":
		return eachObject(o["routes"], n.route)
	case "vars":
		return checkSiteRoot(o["root"])
	case "file_server":
		if browse, ok := o["browse"]; ok {
			// Caddy's built-in listing may be enabled without loading a template file.
			if _, err := routeFields(browse, ""); err != nil {
				return err
			}
		}
		if names, ok := o["index_names"]; ok {
			indices, ok := names.([]any)
			if !ok {
				return Invalid("index 必须是文件名列表")
			}
			for _, name := range indices {
				v, _ := name.(string)
				if !simpleIndex.MatchString(v) {
					return Invalid("index 必须是固定文件名")
				}
			}
		}
		return n.fileRoot(o)
	case "encode":
		enc, err := routeFields(o["encodings"], "zstd gzip")
		if err != nil {
			return err
		}
		for _, v := range enc {
			if _, err := routeFields(v, "level"); err != nil {
				return err
			}
		}
	case "reverse_proxy":
		return n.proxy(o)
	}
	return nil
}

var simpleIndex = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*$`)

func (n *routeNormalizer) proxy(o object) error {
	var tls object
	if transport, ok := o["transport"]; ok {
		t, err := routeFields(transport, "protocol tls versions read_timeout write_timeout dial_timeout response_header_timeout keep_alive compression")
		if err != nil {
			return err
		}
		if t["protocol"] != "http" {
			return Invalid("服务上游仅支持 HTTP transport")
		}
		if v, ok := t["tls"]; ok {
			tls, err = routeFields(v, "server_name handshake_timeout")
			if err != nil {
				return err
			}
			if v, ok := tls["server_name"]; ok {
				s, _ := v.(string)
				if !ValidDomain(s) && net.ParseIP(s) == nil {
					return Invalid("TLS server_name 必须是固定主机名或 IP")
				}
			}
		}
	}
	if balancing, ok := o["load_balancing"]; ok {
		b, err := routeFields(balancing, "selection_policy try_duration try_interval retries")
		if err != nil {
			return err
		}
		if v, ok := b["selection_policy"]; ok {
			selector, err := routeFields(v, "policy")
			if err != nil {
				return err
			}
			switch selector["policy"] {
			case "random", "round_robin", "least_conn", "first", "ip_hash", "uri_hash", "client_ip_hash":
			default:
				return Invalid("服务路由不支持此负载均衡策略")
			}
		}
	}
	upstreams, ok := o["upstreams"].([]any)
	if !ok || len(upstreams) == 0 {
		return Invalid("代理必须配置固定上游")
	}
	// A transport shares its TLS identity across the upstream pool.
	explicitName := tls != nil && tls["server_name"] != nil
	serverName := ""
	err := eachObject(upstreams, func(up object) error {
		if _, err := routeFields(up, "dial max_requests"); err != nil {
			return err
		}
		dial, _ := up["dial"].(string)
		host, port, err := net.SplitHostPort(dial)
		if err != nil {
			return Invalid("无效上游地址 " + dial)
		}
		target := n.service
		target.Host = strings.ToLower(host)
		target.Port, err = strconv.Atoi(port)
		if err != nil {
			return Invalid("无效上游端口 " + port)
		}
		target.Scheme = "http"
		if tls != nil {
			target.Scheme = "https"
			if !explicitName && serverName != "" && serverName != target.Host {
				return Invalid("TLS 上游池含多个主机名，请指定共享的 tls_server_name")
			}
			serverName = target.Host
		}
		target, err = normalizeUpstream(n.ctx, n.policy, target)
		if err != nil {
			return err
		}
		up["dial"] = target.Dial
		return nil
	})
	if err != nil {
		return err
	}
	if tls != nil && !explicitName {
		tls["server_name"] = serverName
	}
	return nil
}
