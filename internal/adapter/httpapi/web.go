package httpapi

import (
	"net/http"
	"os"
	"path"
	"strings"
)

func WebHandler(root string, api http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api" || strings.HasPrefix(request.URL.Path, "/api/") {
			api.ServeHTTP(writer, request)
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writer.Header().Set("Allow", "GET, HEAD")
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		for _, part := range strings.Split(request.URL.Path, "/") {
			if strings.HasPrefix(part, ".") {
				http.NotFound(writer, request)
				return
			}
		}
		name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if name == "" || path.Ext(name) == "" {
			name = "index.html"
		}
		filesystem, err := os.OpenRoot(root)
		if err != nil {
			http.NotFound(writer, request)
			return
		}
		defer filesystem.Close()
		file, err := filesystem.Open(name)
		if err != nil {
			http.NotFound(writer, request)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Cache-Control", "no-cache")
		if request.URL.Path == "/setup" {
			writer.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' https://cloudflare-dns.com/dns-query https://dns.google/resolve; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
			writer.Header().Set("Referrer-Policy", "no-referrer")
		}
		http.ServeContent(writer, request, name, info.ModTime(), file)
	})
}
