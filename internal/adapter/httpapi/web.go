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
		http.ServeContent(writer, request, name, info.ModTime(), file)
	})
}
