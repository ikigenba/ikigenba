// Package serving answers site paths and apex redirects.
package serving

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cache"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/urls"
	"github.com/ikigenba/ikigenba/sites/internal/visitor"
)

// Config supplies the handlers' catalog, trees, pages and request services.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *pages.Set
	ServicesPath string
	Store        *store.Store
	Cache        *cache.Cache
	Telemetry    *telemetry.Writer
	Rand         io.Reader
}

// Sites serves the first path segment's site.
func Sites(cfg Config) http.Handler {
	var randomMu sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		slug, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if strings.Contains(strings.TrimPrefix(r.URL.Path, "/"), "/") {
			rest = "/" + rest
		}
		site, err := cfg.Store.BySlug(r.Context(), slug)
		if errors.Is(err, store.ErrNotFound) {
			w.Header().Set("Cache-Control", "no-cache")
			notice(cfg, w, r, 404, "notfound")
			return
		}
		if err != nil {
			catalog(w, r)
			return
		}
		caller, _ := identity.FromContext(r.Context())
		if site.Visibility == store.Private && caller.UserID == "" {
			redirect(w, r, 302, urls.SignIn(r))
			return
		}
		status, template, location, file, tag := 404, "notfound", "", "", ""
		var treeErr error
		var treeRoot *os.Root
		switch {
		case rest == "":
			status, template, location = 301, "", withQuery("/"+site.Slug+"/", r)
		case site.Commit == "":
		default:
			var tree string
			tree, treeErr = cfg.Cache.Tree(r.Context(), site.ID, site.Repo, site.Commit)
			if treeErr != nil {
				if !errors.Is(treeErr, limits.ErrDraining) && (errors.Is(treeErr, limits.ErrHalted) || (r.Context().Err() != nil && errors.Is(treeErr, context.Cause(r.Context())))) {
					return
				}
				status = 503
				if errors.Is(treeErr, limits.ErrDraining) {
					template = "stopping"
				} else {
					template = "unavailable"
				}
			} else {
				treeRoot, err = os.OpenRoot(tree)
				if err == nil {
					defer func() { _ = treeRoot.Close() }()
				}
				var directory bool
				file, directory = resolve(treeRoot, rest)
				switch {
				case directory:
					status, template, location = 301, "", withQuery(r.URL.EscapedPath()+"/", r)
				case file != "":
					status, template, tag = 200, "", "\""+site.Commit+"\""
					if matching(r, tag) {
						status = 304
					}
				default:
					file = regular(treeRoot, "404.html")
					if file != "" {
						template = ""
					}
				}
			}
		}
		id, valid := visitor.FromRequest(r)
		if !valid {
			randomMu.Lock()
			id, err = visitor.Mint(cfg.Rand)
			randomMu.Unlock()
			valid = err == nil
			if valid {
				w.Header().Add("Set-Cookie", visitor.Cookie(id, visitor.Secure(r)))
			}
		}
		control := "public, no-cache"
		if site.Visibility == store.Private {
			control = "private, no-cache"
		}
		w.Header().Set("Cache-Control", control)
		switch {
		case location != "":
			redirect(w, r, status, location)
		case status == 304:
			w.Header().Set("ETag", tag)
			w.WriteHeader(status)
		case template == "stopping":
			w.Header().Set("Retry-After", "30")
			text(w, r, status, "sites is stopping; try again later\n")
		case template != "":
			if template == "unavailable" {
				w.Header().Set("Retry-After", "60")
			}
			notice(cfg, w, r, status, template)
		default:
			serveFile(w, r, status, treeRoot, file, tag)
		}
		if r.Context().Err() != nil {
			return
		}
		if valid {
			cfg.Telemetry.Emit(r.Context(), "site.viewed", telemetry.Attrs{"site": site.ID, "visitor": id, "path": r.URL.Path, "status": int64(status), "referrer_host": visitor.ReferrerHost(r), "commit": site.Commit})
		}
		if template == "unavailable" {
			cfg.Telemetry.Emit(r.Context(), "site.unavailable", telemetry.Attrs{"site": site.ID, "commit": site.Commit, "reason": cache.Reason(treeErr)})
		}
	})
}

// Apex redirects every request to the configured apex site's gateway URL.
func Apex(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site, set, err := cfg.Store.Apex(r.Context())
		if err != nil {
			catalog(w, r)
			return
		}
		if !set {
			w.Header().Set("Cache-Control", "no-cache")
			notice(cfg, w, r, 404, "notfound")
			return
		}
		redirect(w, r, 302, withQuery(urls.ApexBase(r, cfg.ServicesPath)+"/"+site.Slug+"/"+strings.TrimPrefix(r.URL.EscapedPath(), "/"), r))
	})
}
func withQuery(s string, r *http.Request) string {
	if r.URL.RawQuery != "" {
		return s + "?" + r.URL.RawQuery
	}
	return s
}
func redirect(w http.ResponseWriter, _ *http.Request, status int, location string) {
	w.Header().Set("Location", location)
	w.WriteHeader(status)
}
func text(w http.ResponseWriter, r *http.Request, status int, s string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, s)
	}
}
func catalog(w http.ResponseWriter, r *http.Request) { text(w, r, 503, store.Unreachable+"\n") }
func notice(cfg Config, w http.ResponseWriter, r *http.Request, status int, name string) {
	cfg.Pages.Write(w, r, status, name, pages.NoticeData{Banner: cfg.Banner(page.User{})})
}
func regular(root *os.Root, p string) string {
	if root == nil {
		return ""
	}
	st, err := root.Lstat(p)
	if err == nil && st.Mode().IsRegular() {
		return p
	}
	return ""
}
func resolve(root *os.Root, rest string) (string, bool) {
	if root == nil {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(rest, "/"), "/")
	dir := "."
	for i, p := range parts {
		if strings.HasPrefix(p, ".") || (p == "" && i < len(parts)-1) {
			return "", false
		}
		if i == len(parts)-1 && p == "" {
			return regular(root, filepath.Join(dir, "index.html")), false
		}
		dir = filepath.Join(dir, p)
		st, err := root.Lstat(dir)
		if err != nil || st.Mode()&os.ModeSymlink != 0 {
			return "", false
		}
		if i < len(parts)-1 {
			if !st.IsDir() {
				return "", false
			}
			continue
		}
		if st.IsDir() {
			return "", true
		}
		if st.Mode().IsRegular() {
			return dir, false
		}
	}
	return "", false
}
func matching(r *http.Request, tag string) bool {
	for _, v := range r.Header.Values("If-None-Match") {
		for _, p := range strings.Split(v, ",") {
			p = strings.Trim(p, " \t")
			if p == "*" || strings.TrimPrefix(p, "W/") == tag {
				return true
			}
		}
	}
	return false
}
func serveFile(w http.ResponseWriter, r *http.Request, status int, root *os.Root, file, tag string) {
	f, err := root.Open(file)
	if err != nil {
		w.WriteHeader(status)
		return
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", contentType(file))
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	if tag != "" {
		w.Header().Set("ETag", tag)
	}
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, f)
	}
}

var types = map[string]string{
	".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8", ".txt": "text/plain; charset=utf-8", ".md": "text/markdown; charset=utf-8", ".csv": "text/csv; charset=utf-8", ".json": "application/json", ".map": "application/json", ".webmanifest": "application/manifest+json", ".xml": "application/xml", ".rss": "application/rss+xml", ".atom": "application/atom+xml", ".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif", ".ico": "image/x-icon", ".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf", ".pdf": "application/pdf", ".wasm": "application/wasm", ".mp4": "video/mp4", ".webm": "video/webm", ".mp3": "audio/mpeg", ".ogg": "audio/ogg", ".wav": "audio/wav", ".zip": "application/zip",
}

func contentType(name string) string {
	ext := path.Ext(name)
	ext = strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, ext)
	if t, ok := types[ext]; ok {
		return t
	}
	return "application/octet-stream"
}
