// Package web serves the unauthenticated landing page and embedded assets.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed landing.tmpl static/tokens.css static/fonts/*.woff2
var assets embed.FS

var landingTemplate = template.Must(template.ParseFS(assets, "landing.tmpl"))

type landingData struct {
	Service string
	Version string
}

// LandingHandler returns the exact-root HTML landing page.
func LandingHandler(service, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := landingTemplate.Execute(w, landingData{Service: service, Version: version}); err != nil {
			http.Error(w, "render landing page", http.StatusInternalServerError)
		}
	}
}

// StaticHandler serves the embedded landing-page assets below /static/.
func StaticHandler() http.Handler {
	static, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(static)))
}
