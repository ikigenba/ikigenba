package pages

import (
	"bytes"
	"html/template"
	"net/http"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/sessions"
)

// Set holds the shared chrome and sessions' templates.
type Set struct{ templates *template.Template }

// Load parses the embedded templates independently of the working directory.
func Load() (*Set, error) {
	t, err := page.Templates().ParseFS(sessions.Assets(), "*.html")
	if err != nil {
		return nil, err
	}
	return &Set{templates: t}, nil
}

// Write buffers a template before committing its status and body.
func (s *Set) Write(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var body bytes.Buffer
	if err := s.templates.ExecuteTemplate(&body, name, data); err != nil {
		status = http.StatusInternalServerError
		body.Reset()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}
