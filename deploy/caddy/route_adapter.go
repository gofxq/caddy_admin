package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

const maxRouteSource = 16 << 10
const maxRouteOutput = 1 << 20
const routeFilename = "Service.Caddyfile"

func runRouteAdapter(r io.Reader, w io.Writer) error {
	source, err := io.ReadAll(io.LimitReader(r, maxRouteSource+1))
	if err != nil || len(source) > maxRouteSource {
		return errors.New("服务 Caddyfile 超过 16 KiB 或无法读取")
	}
	routes, err := adaptRoute(source)
	if err != nil {
		return err
	}
	_, err = w.Write(routes)
	return err
}

func adaptRoute(source []byte) (json.RawMessage, error) {
	if bytes.Contains(source, []byte("{$")) {
		return nil, errors.New("服务 Caddyfile 不支持环境变量展开")
	}
	// A hostless, plain HTTP site gives us native directive ordering without a
	// generated host matcher, redirect, certificate policy, or TLS listener.
	wrapped := append([]byte("http://:80 {\n"), source...)
	wrapped = append(wrapped, '\n', '}', '\n')
	tokens, err := caddyfile.Tokenize(wrapped, routeFilename)
	if err != nil || len(tokens) < 4 {
		return nil, errors.New("请检查服务 Caddyfile 的引号、指令及块语法")
	}
	last := tokens[len(tokens)-1]
	if last.Text != "}" || last.Quoted() || last.Line != bytes.Count(source, []byte{'\n'})+3 {
		return nil, errors.New("服务 Caddyfile 的引号或块未闭合")
	}
	if err := checkRouteTokens(tokens[2 : len(tokens)-1]); err != nil {
		return nil, err
	}
	config, _, err := caddyconfig.GetAdapter("caddyfile").Adapt(wrapped, map[string]any{"filename": routeFilename})
	if err != nil {
		return nil, errors.New("Caddy 无法适配服务片段，请检查指令参数、匹配器名称和块语法")
	}
	if len(config) > maxRouteOutput {
		return nil, errors.New("服务路由展开后超过大小限制")
	}
	// Strictly reject additions anywhere above routes, including extra apps,
	// server options or a different listener, before discarding the wrapper.
	var cfg struct {
		Apps struct {
			HTTP struct {
				Servers map[string]struct {
					Listen []string
					Routes []json.RawMessage
				}
			}
		}
	}
	dec := json.NewDecoder(bytes.NewReader(config))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, errors.New("服务片段包含路由之外的设置")
	}
	srv := cfg.Apps.HTTP.Servers["srv0"]
	if len(cfg.Apps.HTTP.Servers) != 1 || !slices.Equal(srv.Listen, []string{":80"}) || len(srv.Routes) == 0 {
		return nil, errors.New("服务片段必须生成单个 HTTP 服务的路由")
	}
	return json.Marshal(srv.Routes)
}

func lineError(line int, msg string) error {
	return fmt.Errorf("服务 Caddyfile 第 %d 行：%s", line-1, msg)
}

func checkRouteTokens(tokens []caddyfile.Token) error {
	stack := []string{"route"}
	for len(tokens) > 0 {
		end := 1
		for end < len(tokens) && tokens[end].Line <= tokens[end-1].Line+tokens[end-1].NumLineBreaks() {
			end++
		}
		line := tokens[:end]
		tokens = tokens[end:]
		first := line[0]
		if first.Text == "}" && !first.Quoted() && len(line) == 1 {
			if len(stack) == 1 {
				return lineError(first.Line, "多余的右花括号")
			}
			stack = stack[:len(stack)-1]
			continue
		}
		open := line[len(line)-1].Text == "{" && !line[len(line)-1].Quoted()
		if open {
			line = line[:len(line)-1]
		}
		if len(line) == 0 {
			return lineError(first.Line, "左花括号应跟在指令后面")
		}
		for _, tok := range line {
			if tok.Text == "import" {
				return lineError(tok.Line, "服务片段不支持 import")
			}
			if tok.Text == "{" || tok.Text == "}" {
				return lineError(tok.Line, "请检查块边界")
			}
		}
		child, ok := routeLine(stack[len(stack)-1], line, open)
		if !ok {
			return lineError(first.Line, "此上下文不支持该指令、模块或参数")
		}
		if open {
			if child == "" || len(stack) >= 32 {
				return lineError(first.Line, "此处不支持子块或嵌套过深")
			}
			stack = append(stack, child)
		}
	}
	if len(stack) != 1 {
		return errors.New("服务 Caddyfile 存在未闭合的块")
	}
	return nil
}

func matcherLine(line []caddyfile.Token, open bool) (string, bool) {
	// The native "not" matcher dispatches to another matcher module. Validate
	// every nested module name, including the inline form.
	for depth := 0; len(line) > 0 && depth < 32; depth++ {
		if line[0].Quoted() {
			return "", false
		}
		if line[0].Text != "not" {
			return "", !open && safeMatcher(line[0].Text)
		}
		line = line[1:]
		if len(line) == 0 {
			return "matchers", open
		}
	}
	return "", false
}

func safeMatcher(name string) bool {
	switch name {
	case "path", "path_regexp", "method", "header", "header_regexp", "query", "host", "protocol", "remote_ip", "client_ip":
		return true
	}
	return false
}

var upstreamPortRange = regexp.MustCompile(`:[0-9]+-[0-9]+`)

func noPortRange(args []caddyfile.Token) bool {
	for _, arg := range args {
		if upstreamPortRange.MatchString(arg.Text) {
			return false
		}
	}
	return true
}

// Approve parser entry points before adaptation. Several otherwise valid
// Caddy modules read files while unmarshaling. New modules and nested parser
// contexts must be reviewed before adding them here.
func routeLine(parent string, line []caddyfile.Token, open bool) (string, bool) {
	name := line[0].Text
	args := line[1:]
	switch parent {
	case "route":
		if strings.HasPrefix(name, "@") {
			if open && len(args) == 0 {
				return "matchers", true
			}
			return matcherLine(args, open)
		}
		switch name {
		case "route", "handle", "handle_path":
			return "route", true
		case "reverse_proxy":
			return name, noPortRange(args)
		case "file_server", "try_files", "header", "respond":
			return name, true
		case "rewrite", "redir", "request_header", "root", "uri":
			return "", true
		case "encode":
			for i, arg := range args {
				if i == 0 && (arg.Text == "*" || strings.HasPrefix(arg.Text, "/") || strings.HasPrefix(arg.Text, "@")) {
					continue
				}
				if arg.Text != "gzip" && arg.Text != "zstd" {
					return "", false
				}
			}
			return name, true
		}
	case "matchers":
		return matcherLine(line, open)
	case "file_server":
		switch name {
		case "index", "hide":
			return "", len(args) > 0
		case "root", "status":
			return "", len(args) == 1
		case "pass_thru", "disable_canonical_uris", "browse":
			return "", len(args) == 0
		}
	case "reverse_proxy":
		switch name {
		case "header_up", "header_down":
			return "", len(args) >= 1 && len(args) <= 3
		case "transport":
			return "http-transport", len(args) == 1 && args[0].Text == "http"
		case "lb_policy":
			if len(args) == 1 {
				switch args[0].Text {
				case "random", "round_robin", "least_conn", "first", "ip_hash", "uri_hash", "client_ip_hash":
					return "", true
				}
			}
			return "", false
		case "method", "rewrite", "flush_interval", "stream_timeout", "stream_close_delay", "lb_try_duration", "lb_try_interval", "lb_retries":
			return "", len(args) == 1
		case "to":
			return "", noPortRange(args)
		}
	case "http-transport":
		switch name {
		case "versions":
			return "", len(args) > 0
		case "tls":
			return "", len(args) == 0
		case "tls_server_name", "tls_timeout", "dial_timeout", "read_timeout", "write_timeout", "response_header_timeout", "keepalive", "keepalive_interval", "keepalive_idle_conns", "keepalive_idle_conns_per_host":
			return "", len(args) == 1
		case "compression":
			return "", len(args) == 0 || (len(args) == 1 && args[0].Text == "off")
		}
	case "try_files":
		return "", name == "policy" && len(args) == 1
	case "respond":
		return "", (name == "body" && len(args) == 1) || (name == "close" && len(args) == 0)
	case "header":
		return "", true
	case "encode":
		return "", ((name == "gzip" || name == "zstd") && len(args) <= 1) || (name == "minimum_length" && len(args) == 1)
	}
	return "", false
}
