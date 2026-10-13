package pages

import (
	"bytes"
	"html/template"
	"net/http"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates"
)

// Set holds the shared and service templates.
type Set struct{ templates *template.Template }

// Load parses the embedded page templates.
func Load() (*Set, error) {
	t, err := page.Templates().ParseFS(candidates.Assets(), "*.html")
	if err != nil {
		return nil, err
	}
	return &Set{templates: t}, nil
}
func (s *Set) Write(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var body bytes.Buffer
	if err := s.templates.ExecuteTemplate(&body, name, data); err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}
