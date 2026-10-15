package domain

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MaxServiceSnippetBytes = 4096

type HeaderRule struct {
	Name      string `json:"name"`
	Operation string `json:"operation"`
	Value     string `json:"value,omitempty"`
}

type ServiceAdvanced struct {
	Compression           string       `json:"compression,omitempty"`
	RequestHeaders        []HeaderRule `json:"request_headers,omitempty"`
	ResponseHeaders       []HeaderRule `json:"response_headers,omitempty"`
	DialTimeout           string       `json:"dial_timeout,omitempty"`
	ResponseHeaderTimeout string       `json:"response_header_timeout,omitempty"`
	Snippet               string       `json:"snippet,omitempty"`
}

var headerName = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

func validateHeaders(rules []HeaderRule) error {
	if len(rules) > 32 {
		return Invalid("每个方向最多配置 32 条头规则")
	}
	seen := map[string]bool{}
	for _, r := range rules {
		key := strings.ToLower(r.Name)
		if !headerName.MatchString(r.Name) || len(r.Name) > 128 {
			return Invalid("头部名称格式无效")
		}
		switch key {
		case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key", "x-auth-token":
			return Invalid("不支持在服务配置中保存凭据或 Cookie 头")
		}
		if seen[key] {
			return Invalid("同一方向的头部重复配置：" + r.Name)
		}
		seen[key] = true
		if r.Operation != "set" && r.Operation != "add" && r.Operation != "delete" {
			return Invalid("头操作必须为设置、追加或删除")
		}
		if len(r.Value) > 2048 || strings.ContainsAny(r.Value, "\r\n\x00{}") {
			return Invalid("头部值不得包含控制字符或动态变量，且不超过 2048 字节")
		}
		for _, c := range r.Value {
			if c < 32 && c != '\t' || c == 127 {
				return Invalid("头部值不得包含控制字符")
			}
		}
		if r.Operation == "delete" && r.Value != "" {
			return Invalid("删除头部时不能填写值")
		}
	}
	return nil
}

func validDuration(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 || d > 24*time.Hour {
		return "", Invalid("超时须为大于 0 且不超过 24 小时的时长，例如 5s、1m")
	}
	return d.String(), nil
}

// EffectiveServiceAdvanced treats snippets as a supported service-level syntax,
// never as a second complete Caddy configuration or an unchecked JSON overlay.
func EffectiveServiceAdvanced(s Service) (ServiceAdvanced, error) {
	a := ServiceAdvanced{}
	if s.Advanced != nil {
		a = *s.Advanced
		a.RequestHeaders = append([]HeaderRule(nil), a.RequestHeaders...)
		a.ResponseHeaders = append([]HeaderRule(nil), a.ResponseHeaders...)
	}
	if len(a.Snippet) > MaxServiceSnippetBytes {
		return a, Invalid("服务级片段不能超过 4096 字节")
	}
	extra, err := parseServiceSnippet(a.Snippet)
	if err != nil {
		return a, err
	}
	merge := func(dst *string, value, label string) error {
		if value == "" {
			return nil
		}
		if *dst != "" {
			return Invalid(label + "在表单与片段中重复配置，请只保留一处")
		}
		*dst = value
		return nil
	}
	if err = merge(&a.Compression, extra.Compression, "响应压缩"); err != nil {
		return a, err
	}
	if err = merge(&a.DialTimeout, extra.DialTimeout, "连接超时"); err != nil {
		return a, err
	}
	if err = merge(&a.ResponseHeaderTimeout, extra.ResponseHeaderTimeout, "响应头超时"); err != nil {
		return a, err
	}
	a.RequestHeaders = append(a.RequestHeaders, extra.RequestHeaders...)
	a.ResponseHeaders = append(a.ResponseHeaders, extra.ResponseHeaders...)
	switch a.Compression {
	case "", "off", "gzip", "zstd_gzip":
	default:
		return a, Invalid("响应压缩仅支持关闭、gzip 或 zstd + gzip")
	}
	if (s.Scheme == "h2c" || s.Scheme == "grpcs") && a.Compression != "" && a.Compression != "off" {
		return a, Invalid("gRPC 消息压缩由客户端与服务端协商，请关闭 HTTP 响应压缩")
	}
	if err = validateHeaders(a.RequestHeaders); err != nil {
		return a, err
	}
	if err = validateHeaders(a.ResponseHeaders); err != nil {
		return a, err
	}
	if a.DialTimeout, err = validDuration(a.DialTimeout); err != nil {
		return a, err
	}
	if a.ResponseHeaderTimeout, err = validDuration(a.ResponseHeaderTimeout); err != nil {
		return a, err
	}
	a.Snippet = ""
	return a, nil
}

type snippetDirective struct {
	name     string
	args     []string
	block    []snippetDirective
	line     int
	hasBlock bool
}

func snippetError(line int, message string) error {
	return Invalid(fmt.Sprintf("服务级片段第 %d 行：%s", line, message))
}

// Tokenize only static words, quoted strings, comments and standalone blocks.
// Unsupported Caddy features (variables, imports, matchers) are not evaluated.
func snippetWords(line string, lineNo int) ([]string, error) {
	words := []string{}
	for i := 0; i < len(line); {
		if line[i] == ' ' || line[i] == '\t' || line[i] == '\r' {
			i++
			continue
		}
		if line[i] == '#' {
			break
		}
		if line[i] == '{' || line[i] == '}' {
			words = append(words, string(line[i]))
			i++
			continue
		}
		if line[i] == '"' {
			start := i
			i++
			for i < len(line) && line[i] != '"' {
				if line[i] == '\\' {
					i++
				}
				i++
			}
			if i >= len(line) {
				return nil, snippetError(lineNo, "引号未闭合")
			}
			i++
			v, err := strconv.Unquote(line[start:i])
			if err != nil {
				return nil, snippetError(lineNo, "字符串转义无效")
			}
			words = append(words, v)
			continue
		}
		start := i
		for i < len(line) && !strings.ContainsRune(" \t\r{}", rune(line[i])) {
			i++
		}
		words = append(words, line[start:i])
	}
	return words, nil
}

func snippetTree(raw string) ([]snippetDirective, error) {
	lines := strings.Split(raw, "\n")
	index := 0
	var read func(bool) ([]snippetDirective, error)
	read = func(nested bool) ([]snippetDirective, error) {
		nodes := []snippetDirective{}
		for index < len(lines) {
			lineNo := index + 1
			w, err := snippetWords(lines[index], lineNo)
			index++
			if err != nil {
				return nil, err
			}
			if len(w) == 0 {
				continue
			}
			if w[0] == "}" {
				if !nested || len(w) != 1 {
					return nil, snippetError(lineNo, "多余的结束括号")
				}
				return nodes, nil
			}
			if w[0] == "{" {
				return nil, snippetError(lineNo, "不支持全局设置或独立站点块")
			}
			n := snippetDirective{name: w[0], args: w[1:], line: lineNo}
			if w[len(w)-1] == "{" {
				n.hasBlock = true
				n.args = w[1 : len(w)-1]
				n.block, err = read(true)
				if err != nil {
					return nil, err
				}
			}
			for _, arg := range n.args {
				if arg == "{" || arg == "}" {
					return nil, snippetError(lineNo, "括号须独立构成指令块")
				}
			}
			nodes = append(nodes, n)
		}
		if nested {
			return nil, snippetError(len(lines), "指令块未闭合")
		}
		return nodes, nil
	}
	return read(false)
}

func parseServiceSnippet(raw string) (ServiceAdvanced, error) {
	result := ServiceAdvanced{}
	nodes, err := snippetTree(raw)
	if err != nil {
		return result, err
	}
	header := func(n snippetDirective, args []string, dst *[]HeaderRule) error {
		if n.hasBlock || len(args) < 1 || len(args) > 2 {
			return snippetError(n.line, "头规则格式：名称 值，或 -名称")
		}
		name := args[0]
		operation := "set"
		if strings.HasPrefix(name, "+") {
			operation = "add"
			name = name[1:]
		} else if strings.HasPrefix(name, "-") {
			operation = "delete"
			name = name[1:]
		}
		if operation == "delete" && len(args) != 1 || operation != "delete" && len(args) != 2 {
			return snippetError(n.line, "设置或追加须填写值，删除只填写 -名称")
		}
		r := HeaderRule{Name: name, Operation: operation}
		if len(args) == 2 {
			r.Value = args[1]
		}
		if err := validateHeaders([]HeaderRule{r}); err != nil {
			return snippetError(n.line, err.Error())
		}
		*dst = append(*dst, r)
		return nil
	}
	timeout := func(n snippetDirective, dst *string) error {
		if n.hasBlock || len(n.args) != 1 {
			return snippetError(n.line, "超时指令须填写一个时长")
		}
		if *dst != "" {
			return snippetError(n.line, "超时重复配置")
		}
		v, err := validDuration(n.args[0])
		if err != nil {
			return snippetError(n.line, err.Error())
		}
		*dst = v
		return nil
	}
	reverseSeen := false
	for _, n := range nodes {
		switch n.name {
		case "encode":
			if n.hasBlock || result.Compression != "" {
				return result, snippetError(n.line, "encode 不支持子块或重复配置")
			}
			joined := strings.Join(n.args, " ")
			switch joined {
			case "gzip":
				result.Compression = "gzip"
			case "zstd gzip":
				result.Compression = "zstd_gzip"
			default:
				return result, snippetError(n.line, "仅支持 encode gzip 或 encode zstd gzip")
			}
		case "header", "request_header":
			dst := &result.ResponseHeaders
			if n.name == "request_header" {
				dst = &result.RequestHeaders
			}
			if n.hasBlock {
				if len(n.args) != 0 {
					return result, snippetError(n.line, "头规则块不支持额外参数")
				}
				for _, child := range n.block {
					if err = header(child, append([]string{child.name}, child.args...), dst); err != nil {
						return result, err
					}
				}
			} else {
				if err = header(n, n.args, dst); err != nil {
					return result, err
				}
			}
		case "reverse_proxy":
			if reverseSeen || !n.hasBlock || len(n.args) != 0 {
				return result, snippetError(n.line, "reverse_proxy 只允许一个无上游地址的配置块，上游由表单管理")
			}
			reverseSeen = true
			transportSeen := false
			for _, child := range n.block {
				switch child.name {
				case "header_up":
					err = header(child, child.args, &result.RequestHeaders)
				case "header_down":
					err = header(child, child.args, &result.ResponseHeaders)
				case "transport":
					if transportSeen || !child.hasBlock || len(child.args) != 1 || child.args[0] != "http" {
						return result, snippetError(child.line, "仅支持一个 transport http 块")
					}
					transportSeen = true
					for _, option := range child.block {
						switch option.name {
						case "dial_timeout":
							err = timeout(option, &result.DialTimeout)
						case "response_header_timeout":
							err = timeout(option, &result.ResponseHeaderTimeout)
						default:
							return result, snippetError(option.line, "不支持该传输指令："+option.name)
						}
						if err != nil {
							return result, err
						}
					}
				default:
					return result, snippetError(child.line, "不支持该代理指令："+child.name)
				}
				if err != nil {
					return result, err
				}
			}
		default:
			return result, snippetError(n.line, "不支持该指令："+n.name)
		}
	}
	return result, nil
}

func caddyHeaderOps(rules []HeaderRule) object {
	ops := object{}
	for _, r := range rules {
		key := http.CanonicalHeaderKey(r.Name)
		if r.Operation == "delete" {
			values, _ := ops["delete"].([]string)
			ops["delete"] = append(values, key)
		} else {
			values, _ := ops[r.Operation].(object)
			if values == nil {
				values = object{}
			}
			values[key] = []string{r.Value}
			ops[r.Operation] = values
		}
	}
	return ops
}

func serviceProxy(s Service) ([]any, error) {
	a, err := EffectiveServiceAdvanced(s)
	if err != nil {
		return nil, err
	}
	proxy := object{"handler": "reverse_proxy", "upstreams": []any{object{"dial": s.Dial}}}
	transport := object{"protocol": "http"}
	switch s.Scheme {
	case "https", "grpcs":
		transport["tls"] = object{"server_name": s.Host}
	}
	switch s.Scheme {
	case "h2c":
		transport["versions"] = []string{"h2c"}
	case "grpcs":
		transport["versions"] = []string{"2"}
	}
	if a.DialTimeout != "" {
		d, _ := time.ParseDuration(a.DialTimeout)
		transport["dial_timeout"] = int64(d)
	}
	if a.ResponseHeaderTimeout != "" {
		d, _ := time.ParseDuration(a.ResponseHeaderTimeout)
		transport["response_header_timeout"] = int64(d)
	}
	if len(transport) > 1 {
		proxy["transport"] = transport
	}
	headers := object{}
	if len(a.RequestHeaders) > 0 {
		headers["request"] = caddyHeaderOps(a.RequestHeaders)
	}
	if len(a.ResponseHeaders) > 0 {
		headers["response"] = caddyHeaderOps(a.ResponseHeaders)
	}
	if len(headers) > 0 {
		proxy["headers"] = headers
	}
	handlers := []any{}
	if a.Compression == "gzip" {
		handlers = append(handlers, object{"handler": "encode", "encodings": object{"gzip": object{}}, "prefer": []string{"gzip"}})
	} else if a.Compression == "zstd_gzip" {
		handlers = append(handlers, object{"handler": "encode", "encodings": object{"zstd": object{}, "gzip": object{}}, "prefer": []string{"zstd", "gzip"}})
	}
	return append(handlers, proxy), nil
}
