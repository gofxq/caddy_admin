package domain

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
)

func TestHTTPRouteNormalizesNestedBackends(t *testing.T) {
	raw := json.RawMessage(`[{"group":"api","match":[{"path":["/api/*"],"header":{"X-App":["demo"]}}],"handle":[{"handler":"subroute","routes":[{"handle":[{"handler":"rewrite","strip_path_prefix":"/api"},{"handler":"reverse_proxy","transport":{"protocol":"http","tls":{}},"upstreams":[{"dial":"backend.test:8443"},{"dial":"backend.test:9443"}],"load_balancing":{"selection_policy":{"policy":"round_robin"}}}]}]}]}]`)
	policy := TargetPolicy{AllowedNames: []string{"backend.test"}, UpstreamCIDRs: []string{"192.0.2.0/24"}, Resolver: dialSnapshotResolver{netip.MustParseAddr("192.0.2.4")}}
	got, err := normalizeHTTPRoute(context.Background(), policy, Service{Hostname: "site.example"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"dial":"192.0.2.4:8443"`, `"dial":"192.0.2.4:9443"`, `"server_name":"backend.test"`, `"group":"api"`, `"strip_path_prefix":"/api"`, `"policy":"round_robin"`} {
		if !strings.Contains(string(got), fragment) {
			t.Errorf("missing %s in %s", fragment, got)
		}
	}
}
func TestHTTPRouteScopesInheritedFileRoot(t *testing.T) {
	raw := json.RawMessage(`[{"match":[{"file":{"try_files":["{http.request.uri.path}","/index.html"]}}],"handle":[{"handler":"rewrite","uri":"{http.matchers.file.relative}"}]},{"handle":[{"handler":"file_server","index_names":["index.html"]}]}]`)
	got, err := normalizeHTTPRoute(context.Background(), TargetPolicy{}, Service{Hostname: "site.example"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), `[{"handle":[{"handler":"vars","root":"/srv/sites/site.example"}]},`) {
		t.Fatal(string(got))
	}
}
func TestHTTPRouteRejectsUnsupportedOptions(t *testing.T) {
	for _, handler := range []string{
		`{"handler":"file_server","root":"/opt/www"}`,
		`{"handler":"file_server","fs":"alternate"}`,
		`{"handler":"templates"}`,
		`{"handler":"file_server","browse":{"template_file":"custom.html"}}`,
		`{"handler":"static_response","body":"{\"name\":\"{env.EXAMPLE}\"}"}`,
		`{"handler":"vars","root":"/srv/sites"}`,
		`{"handler":"static_response","body":"{env.EXAMPLE}"}`,
		`{"handler":"reverse_proxy","upstreams":[{"dial":"192.0.2.4:80"}],"transport":{"protocol":"http","tls":{"insecure_skip_verify":true}}}`,
		`{"handler":"reverse_proxy","upstreams":[{"dial":"192.0.2.4:80"},{"dial":"198.51.100.2:80"}]}`,
	} {
		raw := json.RawMessage(`[{"handle":[` + handler + `]}]`)
		if _, err := normalizeHTTPRoute(context.Background(), TargetPolicy{UpstreamCIDRs: []string{"192.0.2.0/24"}}, Service{}, raw); err == nil {
			t.Errorf("accepted %s", handler)
		}
	}
}

func TestHTTPRoutePreservesCommonNativeHandlers(t *testing.T) {
	raw := json.RawMessage(`[{"match":[{"header":{"Accept-Encoding":["*gzip*"]}}],"handle":[{"handler":"encode","encodings":{"gzip":{},"zstd":{}},"prefer":["zstd","gzip"]},{"handler":"headers","request":{"set":{"X-Remote":["{http.request.remote}"]}}},{"handler":"static_response","body":"{\"ok\":true}"}]},{"handle":[{"handler":"file_server","root":"/srv/sites/demo","browse":{}}]}]`)
	got, err := normalizeHTTPRoute(context.Background(), TargetPolicy{}, Service{Hostname: "demo.example"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(raw) != Fingerprint(got) {
		t.Fatalf("route changed: %s", got)
	}
}
