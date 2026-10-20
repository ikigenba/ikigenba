package pages

import (
	"bytes"
	"html/template"
	"net/http"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/campaigns"
)

// Set holds the embedded service templates and shared chrome.
type Set struct{ templates *template.Template }

// Load prepares a fresh set without depending on the working directory.
func Load() (*Set, error) {
	t, err := page.Templates().ParseFS(campaigns.Assets(), "*.html")
	if err != nil {
		return nil, err
	}
	return &Set{templates: t}, nil
}

// Write renders the whole page before committing its status.
func (s *Set) Write(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var body bytes.Buffer
	err := s.templates.ExecuteTemplate(&body, name, data)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}
