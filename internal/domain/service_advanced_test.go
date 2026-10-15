package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestServiceAdvancedFragmentAndStructuredOptions(t *testing.T) {
	s := Service{Scheme: "https", Advanced: &ServiceAdvanced{Snippet: `# service options
encode zstd gzip
header {
    X-Frame-Options "SAMEORIGIN"
    -Server
}
request_header +X-Application "hello world"
reverse_proxy {
    header_up Host backend.internal
    header_down +X-Upstream yes
    transport http {
        dial_timeout 5s
        response_header_timeout 60s
    }
}`}}
	a, err := EffectiveServiceAdvanced(s)
	if err != nil || a.Compression != "zstd_gzip" || a.DialTimeout != "5s" || a.ResponseHeaderTimeout != "1m0s" || len(a.RequestHeaders) != 2 || len(a.ResponseHeaders) != 3 {
		t.Fatalf("effective=%+v err=%v", a, err)
	}
	if a.RequestHeaders[0].Value != "hello world" {
		t.Fatal(a.RequestHeaders)
	}
}

func TestServiceAdvancedRejectsUnsafeUnsupportedAndConflictingInput(t *testing.T) {
	for _, tc := range []struct {
		name     string
		advanced ServiceAdvanced
		scheme   string
	}{
		{"extra upstream", ServiceAdvanced{Snippet: "reverse_proxy 127.0.0.1:2019"}, "http"},
		{"global", ServiceAdvanced{Snippet: "{\n admin :2019\n}"}, "http"},
		{"site", ServiceAdvanced{Snippet: "evil.example.com {\n encode gzip\n}"}, "http"},
		{"files", ServiceAdvanced{Snippet: "import /etc/passwd"}, "http"},
		{"skip verify", ServiceAdvanced{Snippet: "reverse_proxy {\n transport http {\n tls_insecure_skip_verify\n }\n}"}, "https"},
		{"env", ServiceAdvanced{Snippet: `header X-Leak "{env.CLOUDFLARE_API_TOKEN}"`}, "http"},
		{"secret header", ServiceAdvanced{RequestHeaders: []HeaderRule{{Name: "Authorization", Operation: "set", Value: "private"}}}, "http"},
		{"secret cookie", ServiceAdvanced{Snippet: `header Set-Cookie private`}, "http"},
		{"header injection", ServiceAdvanced{ResponseHeaders: []HeaderRule{{Name: "X-Test", Operation: "set", Value: "a\r\nb"}}}, "http"},
		{"bad header", ServiceAdvanced{ResponseHeaders: []HeaderRule{{Name: "Bad Name", Operation: "set", Value: "a"}}}, "http"},
		{"bad operation", ServiceAdvanced{RequestHeaders: []HeaderRule{{Name: "X-Test", Operation: "replace", Value: "a"}}}, "http"},
		{"duplicate compression", ServiceAdvanced{Compression: "gzip", Snippet: "encode gzip"}, "http"},
		{"duplicate timeout", ServiceAdvanced{DialTimeout: "3s", Snippet: "reverse_proxy {\n transport http {\n dial_timeout 5s\n }\n}"}, "http"},
		{"duplicate header", ServiceAdvanced{ResponseHeaders: []HeaderRule{{Name: "X-Test", Operation: "set", Value: "a"}}, Snippet: "header x-test b"}, "http"},
		{"invalid duration", ServiceAdvanced{DialTimeout: "bad"}, "http"},
		{"negative", ServiceAdvanced{DialTimeout: "-1s"}, "http"},
		{"too long", ServiceAdvanced{ResponseHeaderTimeout: "25h"}, "http"},
		{"grpc compression", ServiceAdvanced{Compression: "gzip"}, "h2c"},
		{"oversized snippet", ServiceAdvanced{Snippet: strings.Repeat("x", 4097)}, "http"},
		{"unknown compression", ServiceAdvanced{Compression: "br"}, "http"},
		{"missing brace", ServiceAdvanced{Snippet: "header {\n X-Test ok"}, "http"},
		{"trailing brace", ServiceAdvanced{Snippet: "header X-Test ok\n}"}, "http"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := EffectiveServiceAdvanced(Service{Scheme: tc.scheme, Advanced: &tc.advanced}); err == nil {
				t.Fatal("unsafe/invalid options accepted")
			}
		})
	}
	_, err := EffectiveServiceAdvanced(Service{Scheme: "http", Advanced: &ServiceAdvanced{Snippet: "# comment\nunknown value"}})
	if err == nil || !strings.Contains(err.Error(), "第 2 行") {
		t.Fatal("missing source line", err)
	}
}

func TestAdvancedDiffUsesValuesAndIncludesOptions(t *testing.T) {
	original := Service{ID: "one", Scheme: "http", Advanced: &ServiceAdvanced{Compression: "gzip"}}
	var copy Service
	raw, _ := json.Marshal(original)
	_ = json.Unmarshal(raw, &copy)
	if len(Diff([]Service{original}, []Service{copy})) != 0 {
		t.Fatal("equal serialized advanced created a false diff")
	}
	copy.Advanced.Compression = "zstd_gzip"
	if len(Diff([]Service{original}, []Service{copy})) != 1 {
		t.Fatal("advanced change missing")
	}
}

func TestGeneratedAdvancedOptionsPreserveFixedTargetAndTLS(t *testing.T) {
	for _, scheme := range []string{"http", "https", "h2c", "grpcs"} {
		t.Run(scheme, func(t *testing.T) {
			s := Service{ID: "one", DomainID: "home", Hostname: "app.example.test", Scheme: scheme, Host: "backend.internal", Port: 443, Enabled: true, Dial: "10.0.0.8:443", Advanced: &ServiceAdvanced{DialTimeout: "5s", ResponseHeaderTimeout: "60s", RequestHeaders: []HeaderRule{{Name: "X-App", Operation: "set", Value: "demo"}}, ResponseHeaders: []HeaderRule{{Name: "Server", Operation: "delete"}}}}
			if scheme == "http" || scheme == "https" {
				s.Advanced.Compression = "zstd_gzip"
			}
			raw, err := Generate(CaddyConfig{Domains: []ManagedDomain{{ID: "home", Name: "example.test", Access: DomainAccess("internet")}}, AdminDomain: "admin.example.test", HTTPPort: "80", HTTPSPort: "443"}, []Service{s})
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []string{"10.0.0.8:443", "5000000000", "60000000000", "X-App"} {
				if !strings.Contains(string(raw), required) {
					t.Errorf("missing %s", required)
				}
			}
			if scheme == "https" || scheme == "grpcs" {
				if !strings.Contains(string(raw), `"server_name": "backend.internal"`) || strings.Contains(string(raw), "insecure_skip_verify") {
					t.Fatal(string(raw))
				}
			}
			if scheme == "h2c" && !strings.Contains(string(raw), `"h2c"`) {
				t.Fatal("HTTP/2 cleartext missing")
			}
			if scheme == "grpcs" && !strings.Contains(string(raw), `"versions"`) {
				t.Fatal("TLS HTTP/2 missing")
			}
		})
	}
}
