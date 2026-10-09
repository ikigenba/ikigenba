package web

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

// FilesConfig supplies the catalog, run folders and not-found page.
type FilesConfig struct {
	Banner func(u page.User) page.Banner
	Pages  *pages.Set
	Store  *store.Store
	Runs   *runs.Core
}

// Files serves record files and regular work files without cleaning paths.
func Files(cfg FilesConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		missing := func() {
			cfg.Pages.Write(w, r, http.StatusNotFound, "notfound", pages.NoticeData{Banner: cfg.Banner(page.User{})})
		}
		failed := func(err error) {
			if errors.Is(err, store.ErrNotFound) {
				missing()
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte(store.Unreachable + "\n"))
			}
		}
		caller, _ := identity.FromContext(r.Context())
		p := pieces(r)
		prompt, err := cfg.Store.Find(r.Context(), caller.UserID, p[0])
		if err != nil {
			failed(err)
			return
		}
		u, err := cfg.Store.FindRun(r.Context(), caller.UserID, p[2])
		if err != nil {
			failed(err)
			return
		}
		if u.Prompt != prompt.ID || cfg.Runs.Gone(u) {
			missing()
			return
		}
		parts := p[3:]
		if u.Status == store.StatusQueued && len(parts) == 1 && (parts[0] == runs.StdoutFile || parts[0] == runs.StderrFile || parts[0] == runs.TranscriptFile) {
			missing()
			return
		}
		root, err := os.OpenRoot(cfg.Runs.Folder(u))
		if err != nil {
			missing()
			return
		}
		defer func() { _ = root.Close() }()
		path, work := namedFile(root, parts)
		if path == "" {
			missing()
			return
		}
		content, err := root.ReadFile(path)
		if err != nil {
			missing()
			return
		}
		typ := "text/plain; charset=utf-8"
		if work {
			typ = "application/octet-stream"
			w.Header().Set("Content-Disposition", disposition(parts[len(parts)-1]))
		}
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = io.Copy(w, bytes.NewReader(content))
		}
	})
}
func namedFile(root *os.Root, parts []string) (string, bool) {
	work := false
	switch {
	case len(parts) == 1:
		switch parts[0] {
		case runs.InputFile, runs.StdoutFile, runs.StderrFile, runs.TranscriptFile:
		default:
			return "", false
		}
	case len(parts) > 1 && parts[0] == runs.WorkDir:
		work = true
	default:
		return "", false
	}
	path := ""
	for i, p := range parts {
		if p == "" || p == "." || p == ".." || strings.ContainsAny(p, "/\x00") {
			return "", false
		}
		path = filepath.Join(path, p)
		info, err := root.Lstat(path)
		if err != nil {
			return "", false
		}
		if i == len(parts)-1 {
			if !info.Mode().IsRegular() {
				return "", false
			}
		} else if !info.IsDir() {
			return "", false
		}
	}
	return path, work
}
func disposition(name string) string {
	for i := 0; i < len(name); i++ {
		if name[i] < 0x20 || name[i] > 0x7e {
			return "attachment"
		}
	}
	name = strings.ReplaceAll(name, "\\", "\\\\")
	name = strings.ReplaceAll(name, "\"", "\\\"")
	return "attachment; filename=\"" + name + "\""
}
