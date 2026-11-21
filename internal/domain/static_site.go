package domain

import (
	"encoding/json"
	"regexp"
)

// StaticSite describes the file and API branches of one managed host.
type StaticSite struct {
	Root        string `json:"root"`
	APIPath     string `json:"api_path,omitempty"`
	SPAFallback *bool  `json:"spa_fallback,omitempty"`
}

const literalSegment = `/[A-Za-z0-9_-][A-Za-z0-9._-]*`

var siteRootPattern = regexp.MustCompile(`^/srv/sites(?:` + literalSegment + `)+$`)
var apiPathPattern = regexp.MustCompile(`^(?:` + literalSegment + `)+/\*$`)

func (site *StaticSite) validate() error {
	if site == nil {
		return nil
	}
	if len(site.Root) > 1024 || !siteRootPattern.MatchString(site.Root) {
		return Invalid("静态目录必须是 /srv/sites/ 下的项目目录，例如 /srv/sites/douji_admin")
	}
	if site.APIPath != "" && (len(site.APIPath) > 256 || !apiPathPattern.MatchString(site.APIPath)) {
		return Invalid("API 路径须为固定前缀并以 /* 结尾，例如 /api/*")
	}
	return nil
}

func (s Service) FilesOnly() bool {
	return s.StaticSite != nil && s.StaticSite.APIPath == ""
}

func serviceHandlers(service Service) ([]any, error) {
	if service.Caddyfile != "" {
		if !json.Valid(service.ResolvedRoutes) || len(service.ResolvedRoutes) == 0 || service.ResolvedRoutes[0] != '[' {
			return nil, Invalid("请先校验服务 Caddyfile")
		}
		return []any{object{"handler": "subroute", "routes": service.ResolvedRoutes}}, nil
	}
	site := service.StaticSite
	if err := site.validate(); err != nil {
		return nil, err
	}
	var handlers []any
	if !service.FilesOnly() {
		if service.Dial == "" {
			return nil, Invalid("服务缺少已校验的上游目标")
		}
		proxy, err := serviceProxy(service)
		if err != nil || site == nil {
			return proxy, err
		}
		handlers = []any{object{"match": []any{object{"path": []string{site.APIPath}}}, "handle": proxy, "terminal": true}}
	}
	candidates := []string{"{http.request.uri.path}", "{http.request.uri.path}/"}
	fallback := site.APIPath != ""
	if site.SPAFallback != nil {
		fallback = *site.SPAFallback
	}
	if fallback {
		candidates = append(candidates, "/index.html")
	}
	routes := append(handlers, staticFileRoutes(site.Root, candidates)...)
	return []any{object{"handler": "subroute", "routes": routes}}, nil
}
