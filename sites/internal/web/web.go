package web

import (
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

var landingTemplate = template.Must(template.ParseFS(embedded, "landing.html"))

// LandingHandler renders the public landing card for the running service.
func LandingHandler(service, version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = landingTemplate.Execute(w, struct {
			Service string
			Version string
		}{
			Service: service,
			Version: version,
		})
	})
}

// StaticHandler serves the web package's embedded static assets below /static/.
func StaticHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/static/") {
			http.NotFound(w, r)
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/static/")
		if name == "." || name == "" || strings.HasPrefix(name, "../") {
			http.NotFound(w, r)
			return
		}

		body, err := fs.ReadFile(embedded, "static/"+name)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		switch path.Ext(name) {
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case ".woff2":
			w.Header().Set("Content-Type", "font/woff2")
		default:
			w.Header().Set("Content-Type", http.DetectContentType(body))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
}
