package prompt

import (
	"net/http"
	"path/filepath"
)

// RunContentHandler serves run sandbox files over the loopback content plane.
// It is deliberately unauthenticated: the loopback perimeter is the trust
// boundary, while requests that passed through the public front door are
// rejected by their injected identity headers.
func (s *Service) RunContentHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Owner-Email") != "" || r.Header.Get("X-Forwarded-Proto") != "" {
			http.NotFound(w, r)
			return
		}

		runID := r.URL.Query().Get("run_id")
		relPath := r.URL.Query().Get("path")
		if runID == "" || relPath == "" {
			http.NotFound(w, r)
			return
		}

		f, info, err := s.sandbox.Open(runID, relPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		http.ServeContent(w, r, filepath.Base(relPath), info.ModTime(), f)
	})
}
