// Package panel serves the widget control panel over a caller-owned store.
package panel

import (
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/dummy"
	"github.com/ikigenba/ikigenba/dummy/internal/tools"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

// ServiceName names the service.
const ServiceName = "dummy"

// MethodNotAllowedBody reports a refused fragment method.
const MethodNotAllowedBody = "method not allowed\n"

// NotFoundMessage reports an unknown route.
const NotFoundMessage = "That page was not found."

// MethodNotAllowedMessage reports a refused page method.
const MethodNotAllowedMessage = "That method is not allowed here."

// UnsupportedMediaTypeMessage reports an unsupported submission encoding.
const UnsupportedMediaTypeMessage = "That media type is not supported."

// LocalLogoutURL is the development logout endpoint.
const LocalLogoutURL = "http://localhost:3001/logout"

// LocalProfileURL is the development profile endpoint.
const LocalProfileURL = "http://localhost:3001/"

type handler struct {
	store     *widget.Store
	templates *template.Template
	banner    func(page.User) page.Banner
	srv       *mcp.Server
}

// Handler constructs a panel whose requests share s.
func Handler(s *widget.Store, banner func(page.User) page.Banner, srv *mcp.Server, stderr io.Writer) http.Handler {
	set := template.Must(page.Templates().ParseFS(dummy.Assets(), "*.html"))
	tools.Register(srv, s)
	required := identity.Require(ServiceName, stderr, &handler{store: s, banner: banner, srv: srv, templates: set})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-User-Id") == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(identity.MissingBody)))
		}
		required.ServeHTTP(w, r)
	})
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

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, page.StaticPrefix) {
		page.Static().ServeHTTP(w, r)
		return
	}
	switch r.URL.Path {
	case "/mcp":
		h.srv.ServeHTTP(w, r)
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
