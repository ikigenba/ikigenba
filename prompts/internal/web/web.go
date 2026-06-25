package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed landing.tmpl static
var content embed.FS

var landing = template.Must(template.ParseFS(content, "landing.tmpl"))

type landingData struct {
	Service string
	Version string
}

// LandingHandler renders the unauthenticated service landing page.
func LandingHandler(service, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = landing.Execute(w, landingData{
			Service: service,
			Version: version,
		})
	}
}

// StaticHandler serves the embedded landing-page static assets.
func StaticHandler() http.Handler {
	static, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(static))
}
