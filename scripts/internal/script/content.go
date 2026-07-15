package script

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// RunContentHandler serves files from persisted run trees. The composition
// root owns the loopback-only perimeter around this otherwise unauthenticated
// handler.
func (s *Service) RunContentHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runID := r.URL.Query().Get("run_id")
		if !cleanRunID(runID) {
			http.NotFound(w, r)
			return
		}

		name := r.URL.Query().Get("path")
		target, err := resolveWithin(s.runDir(runID), name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(target)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}

		http.ServeContent(w, r, filepath.Base(target), info.ModTime(), f)
	})
}

func cleanRunID(runID string) bool {
	return runID != "" && runID != "." && runID != ".." &&
		!strings.ContainsAny(runID, `/\`) && filepath.Clean(runID) == runID
}
