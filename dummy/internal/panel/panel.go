// Package panel serves the widget control panel over a caller-owned store.
package panel

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/ikigenba/ikigenba/dummy"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

// ServiceName is the name shown in the panel chrome.
const ServiceName = "dummy"

// MissingIdentityBody reports an absent upstream identity.
const MissingIdentityBody = "identity header missing\n"

// MethodNotAllowedBody reports a method refused by the fragment route.
const MethodNotAllowedBody = "method not allowed\n"

// NotFoundMessage is the message on unknown routes.
const NotFoundMessage = "That page was not found."

// MethodNotAllowedMessage is the message on page routes refusing a method.
const MethodNotAllowedMessage = "That method is not allowed here."

// UnsupportedMediaTypeMessage reports an unsupported form encoding.
const UnsupportedMediaTypeMessage = "That media type is not supported."

// SignOutText labels the chrome's sign-out link.
const SignOutText = "Sign out"

// LocalSignOutURL is auth's local development origin.
const LocalSignOutURL = "http://localhost:3001/"

//go:embed templates/*.html
var templateFiles embed.FS

type handler struct {
	store     *widget.Store
	stderr    io.Writer
	stderrMu  sync.Mutex
	templates *template.Template
}

// Handler constructs a panel whose requests share s.
func Handler(s *widget.Store, stderr io.Writer) http.Handler {
	return &handler{store: s, stderr: stderr, templates: template.Must(template.New("panel").Funcs(template.FuncMap{"attr": safeAttribute, "esc": escapeText}).ParseFS(templateFiles, "templates/*.html"))}
}

// SignOutURL derives auth's root from the request host and strict proxy scheme.
func SignOutURL(host, forwardedProto string) string {
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if !strings.HasPrefix(host, "dummy.") || len(host) == len("dummy.") {
		return LocalSignOutURL
	}
	scheme := "https"
	if forwardedProto == "http" || forwardedProto == "https" {
		scheme = forwardedProto
	}
	return scheme + "://auth." + strings.TrimPrefix(host, "dummy.") + "/"
}

// safeAttribute is only called with template-owned attribute names. Escaping
// equals signs also keeps echoed bytes from looking like attribute occurrences.
func safeAttribute(name string, value any) string {
	escaped := strings.ReplaceAll(html.EscapeString(fmt.Sprint(value)), "=", "&#61;")
	return name + `="` + escaped + `"`
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-User-Id") == "" {
		plainFailure(w, r, http.StatusInternalServerError, MissingIdentityBody)
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = "-"
		}
		h.stderrMu.Lock()
		_, _ = h.stderr.Write([]byte("dummy: request " + id + ": X-User-Id is missing\n"))
		h.stderrMu.Unlock()
		return
	}
	switch r.URL.Path {
	case "/":
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			w.Header().Set("Location", "/widgets")
			w.WriteHeader(http.StatusSeeOther)
			return
		}
		w.Header().Set("Allow", "GET, HEAD")
		h.renderFailure(w, r, http.StatusMethodNotAllowed, MethodNotAllowedMessage)
	case "/widgets":
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.renderPage(w, r, http.StatusOK, widget.Submission{}, widget.FieldErrors{})
		case http.MethodPost:
			h.serveForm(w, r)
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			h.renderFailure(w, r, http.StatusMethodNotAllowed, MethodNotAllowedMessage)
		}
	case "/widgets/table":
		h.table(w, r)
	default:
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			h.asset(w, r)
			return
		}
		h.renderFailure(w, r, http.StatusNotFound, NotFoundMessage)
	}
}

func (h *handler) asset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	if name == "" || strings.Contains(name, "/") {
		h.renderFailure(w, r, http.StatusNotFound, NotFoundMessage)
		return
	}
	file := "assets/" + name
	info, err := fs.Stat(dummy.Assets, file)
	if err != nil || !info.Mode().IsRegular() {
		h.renderFailure(w, r, http.StatusNotFound, NotFoundMessage)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.renderFailure(w, r, http.StatusMethodNotAllowed, MethodNotAllowedMessage)
		return
	}
	body, err := dummy.Assets.ReadFile(file)
	if err != nil {
		panic(err)
	}
	etag := fmt.Sprintf(`"%x"`, sha256.Sum256(body))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	for _, field := range r.Header.Values("If-None-Match") {
		for entry := range strings.SplitSeq(field, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "*" || entry == etag || entry == "W/"+etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}
	w.Header().Set("Content-Type", assetContentType(name))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, bytes.NewReader(body))
	}
}

func assetContentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(name, ".txt"):
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

func plainFailure(w http.ResponseWriter, r *http.Request, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(body))
	}
}

func escapeText(value any) string { return html.EscapeString(fmt.Sprint(value)) }
