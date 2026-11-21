package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAdaptRouteNative(t *testing.T) {
	source := `@api {
 path /api/*
 method GET
}
handle @api {
 route {
  header X-Test "two words"
  reverse_proxy example.test:8080 {
   header_up X-Custom example
  }
 }
}
handle_path /static/* {
 root * /srv/static
 try_files {path} /index.html
 encode gzip zstd
 file_server
}
handle {
 redir /old /new
 rewrite /fallback
 respond "not found" 404
}`
	raw, err := adaptRoute([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	var routes []struct {
		Match []map[string]json.RawMessage
		Group string
	}
	if err := json.Unmarshal(raw, &routes); err != nil || len(routes) != 3 {
		t.Fatalf("routes %s: %v", raw, err)
	}
	for _, r := range routes {
		for _, m := range r.Match {
			if _, ok := m["host"]; ok {
				t.Fatal("synthetic host matcher")
			}
		}
	}
	for _, want := range []string{"reverse_proxy", "X-Custom", "file_server", "subroute", "not found", "/api/*", "/srv/static", "encode"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s", want)
		}
	}
}
func TestRejectUnsafeServiceContexts(t *testing.T) {
	for _, source := range []string{
		"", "# only", "@unused path /", "}\n:443 {\nrespond ok", "{\nadmin off\n}", "example.test {\nrespond x\n}", "tls internal", "log",
		"import /private/fragment", "route {\nhandle {\nimport x\n}\n}", "respond {$PRIVATE}", "respond ok\n# {$PRIVATE}",
		"route {\ntls internal\n}", "route {\nrespond ok", "respond \"unfinished", "respond \"}\"",
		"reverse_proxy backend {\ntransport http {\ntls_trusted_ca_certs /private/file\n}\n}", "reverse_proxy backend {\nflush_interval 1 dynamic a\n}",
		"reverse_proxy example.test:1-1000", "file_server {\nstatus 200 precompressed zstd\n}", "@m unsupported_module x\nrespond @m ok",
		"encode {\nminimum_length 1 unknown_encoder\n}",
	} {
		if _, err := adaptRoute([]byte(source)); err == nil {
			t.Errorf("accepted %q", source)
		} else if strings.Contains(err.Error(), "/private/") {
			t.Errorf("leaked parameter: %v", err)
		}
	}
}
func TestRouteInputLimit(t *testing.T) {
	var out strings.Builder
	if runRouteAdapter(strings.NewReader(strings.Repeat("x", maxRouteSource+1)), &out) == nil || out.Len() != 0 {
		t.Fatal("accepted oversized source")
	}
}

func TestAdaptProxyTransportAndMatcherOptions(t *testing.T) {
	source := `@clients {
  client_ip 192.0.2.0/24
  not {
    remote_ip 192.0.2.42
  }
}
handle @clients {
  reverse_proxy https://example.test:8443 {
    lb_policy round_robin
    transport http {
      versions 2
      tls_server_name example.test
      dial_timeout 5s
      read_timeout 10s
    }
  }
}
file_server {
  browse
}`
	raw, err := adaptRoute([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"versions":["2"]`, `"server_name":"example.test"`, `"policy":"round_robin"`, `"client_ip"`, `"not"`, `"remote_ip"`, `"browse":{}`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %s in %s", want, raw)
		}
	}
	for _, source := range []string{"@m not path /skip\nrespond @m ok", "@m not {\npath /skip\n}\nrespond @m ok", "reverse_proxy h2c://example.test:8080", "reverse_proxy https://example.test:443 {\ntransport http {\nversions 2\n}\n}"} {
		if _, err := adaptRoute([]byte(source)); err != nil {
			t.Errorf("could not adapt %s: %v", source, err)
		}
	}
	for _, source := range []string{"@m not unsafe_module args\nrespond @m ok", "@m not {\nunsafe_module args\n}\nrespond @m ok", "file_server {\nbrowse /private/template\n}", "reverse_proxy backend {\ntransport http {\ndial_timeout 1s tls_trust_pool file\n}\n}", "reverse_proxy backend {\nlb_policy cookie\n}"} {
		if _, err := adaptRoute([]byte(source)); err == nil {
			t.Errorf("accepted unexpected option: %s", source)
		}
	}
}
