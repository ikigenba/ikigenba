// Package web composes scripts' HTTP handlers.
package web

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/scripts/internal/pages"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// FilesConfig supplies the file handler's catalog, folders and notice page.
type FilesConfig struct {
	Banner func(u page.User) page.Banner
	Pages  *pages.Set
	Store  *store.Store
	Runs   *runs.Core
}

func filePieces(r *http.Request) []string {
	pieces := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
	for i, p := range pieces {
		v, e := url.PathUnescape(p)
		if e != nil {
			v = p
		}
		pieces[i] = v
	}
	return pieces
}
func namedFile(root *os.Root, pieces []string) (string, bool) {
	if len(pieces) == 1 && (pieces[0] == runs.InputFile || pieces[0] == runs.StdoutFile || pieces[0] == runs.StderrFile) {
		p := pieces[0]
		st, e := root.Lstat(p)
		return p, e == nil && st.Mode().IsRegular()
	}
	if len(pieces) < 2 || pieces[0] != runs.OutDir {
		return "", false
	}
	p := ""
	for i, v := range pieces {
		if v == "" || v == "." || v == ".." || strings.ContainsAny(v, "/\x00") {
			return "", false
		}
		p = filepath.Join(p, v)
		st, e := root.Lstat(p)
		if e != nil {
			return "", false
		}
		if i == len(pieces)-1 {
			return p, st.Mode().IsRegular()
		}
		if !st.IsDir() {
			return "", false
		}
	}
	return "", false
}
func disposition(base string) string {
	for _, b := range []byte(base) {
		if b < 32 || b > 126 {
			return "attachment"
		}
	}
	base = strings.ReplaceAll(base, "\\", "\\\\")
	base = strings.ReplaceAll(base, "\"", "\\\"")
	return "attachment; filename=\"" + base + "\""
}

// Files serves only retained regular files of the caller's runs.
func Files(cfg FilesConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(405)
			return
		}
		missing := func() { cfg.Pages.Write(w, r, 404, "notfound", pages.NoticeData{Banner: cfg.Banner(page.User{})}) }
		refusal := func() {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(503)
			if r.Method != http.MethodHead {
				_, _ = io.WriteString(w, store.Unreachable+"\n")
			}
		}
		pieces := filePieces(r)
		if len(pieces) < 4 {
			missing()
			return
		}
		caller, _ := identity.FromContext(r.Context())
		owner := caller.UserID
		sc, e := cfg.Store.Find(r.Context(), owner, pieces[0])
		if e != nil {
			if errors.Is(e, store.ErrNotFound) {
				missing()
			} else {
				refusal()
			}
			return
		}
		u, e := cfg.Store.FindRun(r.Context(), owner, pieces[2])
		if e != nil {
			if errors.Is(e, store.ErrNotFound) {
				missing()
			} else {
				refusal()
			}
			return
		}
		if u.Script != sc.ID || cfg.Runs.Gone(u) {
			missing()
			return
		}
		root, e := os.OpenRoot(cfg.Runs.Folder(u))
		if e != nil {
			missing()
			return
		}
		defer func() { _ = root.Close() }()
		p, ok := namedFile(root, pieces[3:])
		if !ok {
			missing()
			return
		}
		f, e := root.Open(p)
		if e != nil {
			missing()
			return
		}
		defer func() { _ = f.Close() }()
		st, e := f.Stat()
		if e != nil {
			missing()
			return
		}
		ct := "text/plain; charset=utf-8"
		if pieces[3] == runs.OutDir {
			ct = "application/octet-stream"
			w.Header().Set("Content-Disposition", disposition(pieces[len(pieces)-1]))
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(200)
		if r.Method != http.MethodHead {
			_, _ = io.Copy(w, f)
		}
	})
}
