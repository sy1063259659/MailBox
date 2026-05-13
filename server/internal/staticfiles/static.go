package staticfiles

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func Handler(distDir string, apiHandler http.Handler) http.Handler {
	fileServer := http.FileServer(http.Dir(distDir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiHandler.ServeHTTP(w, r)
			return
		}

		path := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if path == "." || path == string(filepath.Separator) {
			path = "index.html"
		}

		fullPath := filepath.Join(distDir, path)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, filepath.Join(distDir, "index.html"))
	})
}
