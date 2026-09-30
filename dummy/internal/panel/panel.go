// Package panel serves the widget control panel over a caller-owned store.
package panel

import (
	"embed"
	"fmt"
	"html"
	htmltemplate "html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/ikigenba/ikigenba/appkit"
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

// LocalLogoutURL is auth's local development logout endpoint.
const LocalLogoutURL = "http://localhost:3001/logout"

// LocalProfileURL is auth's local development profile endpoint.
const LocalProfileURL = "http://localhost:3001/"

// PlusIcon draws the widget creation button.
const PlusIcon = `<svg class="ico" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5l0 14"/><path d="M5 12l14 0"/></svg>`

//go:embed templates/*.html
var templateFiles embed.FS

type handler struct {
	store           *widget.Store
	stderr          io.Writer
	stderrMu        sync.Mutex
	templates       *template.Template
	bannerTemplates *htmltemplate.Template
	banner          func(appkit.User) appkit.Banner
}

// Handler constructs a panel whose requests share s.
func Handler(s *widget.Store, banner func(u appkit.User) appkit.Banner, stderr io.Writer) http.Handler {
	set := template.Must(template.New("panel").Funcs(template.FuncMap{"attr": safeAttribute, "esc": escapeText, "plusIcon": func() string { return PlusIcon }}).ParseFS(templateFiles, "templates/*.html"))
	return &handler{store: s, banner: banner, stderr: stderr, templates: set, bannerTemplates: appkit.Templates()}
}

// LogoutURL derives auth's logout endpoint from the request host and strict proxy scheme.
func LogoutURL(host, forwardedProto string) string {
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if !strings.HasPrefix(host, "dummy.") || len(host) == len("dummy.") {
		return LocalLogoutURL
	}
	scheme := "https"
	if forwardedProto == "http" || forwardedProto == "https" {
		scheme = forwardedProto
	}
	return scheme + "://auth." + strings.TrimPrefix(host, "dummy.") + "/logout"
}

// ProfileURL derives auth's profile endpoint from the request host and strict proxy scheme.
func ProfileURL(host, forwardedProto string) string {
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if !strings.HasPrefix(host, "dummy.") || len(host) == len("dummy.") {
		return LocalProfileURL
	}
	scheme := "https"
	if forwardedProto == "http" || forwardedProto == "https" {
		scheme = forwardedProto
	}
	return scheme + "://auth." + strings.TrimPrefix(host, "dummy.") + "/"
}

// safeAttribute is called only with template-owned names. Escaping equals
// preserves the design's attribute occurrence rule even for echoed values.
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
	if strings.HasPrefix(r.URL.Path, appkit.StaticPrefix) {
		appkit.Static().ServeHTTP(w, r)
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
		h.renderFailure(w, r, http.StatusNotFound, NotFoundMessage)
	}
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
