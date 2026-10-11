// Package pages renders the connections service's HTML surfaces.
package pages

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	connections "github.com/ikigenba/ikigenba/connections"
	"github.com/ikigenba/ikigenba/connections/internal/broker"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/tools"
	"github.com/ikigenba/ikigenba/connections/internal/urls"
)

// Service identity and callback notice failure values.
const (
	ServiceName     = "connections"
	Description     = "Accounts at outside providers, connected for the suite's connectors"
	FailureMissing  = "missing"
	FailureUnknown  = "unknown"
	FailureExpired  = "expired"
	FailureForeign  = "foreign"
	FailureDenied   = "denied"
	FailureExchange = "exchange"
	FailureAccount  = "account"
)

// ToolsLevel names the tools page in the banner trail.
var ToolsLevel = page.Level{Name: "tools", URL: "/tools"}

// AboutLevel names the about page in the banner trail.
var AboutLevel = page.Level{Name: "about", URL: "/about"}

// Set holds the embedded page templates and shared chrome.
type Set struct{ templates *template.Template }

// Load parses the service assets alongside appkit templates.
func Load() (*Set, error) {
	t, err := page.Templates().ParseFS(connections.Assets(), "*.html")
	if err != nil {
		return nil, err
	}
	return &Set{templates: t}, nil
}

// Write renders a template with the requested status, without a body for HEAD.
func (s *Set) Write(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var body bytes.Buffer
	if err := s.templates.ExecuteTemplate(&body, name, data); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}

// Config supplies the caller chrome, templates and connection operations.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Providers    []providers.Provider
	Store        *store.Store
	Broker       *broker.Broker
}

// LandingData contains the caller's provider rows.
type LandingData struct {
	Banner      page.Banner
	Connections []ConnectionRow
}

// ConnectionRow is one provider and its displayed connection metadata.
type ConnectionRow struct {
	Name, DisplayName, Status, Account string
	Scopes                             []string
	Connected, ConnectedText           string
}

// NoticeData names a provider and a callback failure without credentials.
type NoticeData struct {
	Banner               page.Banner
	DisplayName, Failure string
}

// ToolsData contains the registered tool catalog.
type ToolsData struct {
	Banner page.Banner
	Tools  []ToolRow
}

// ToolRow contains a tool's name and first description line.
type ToolRow struct{ Name, Description string }

// AboutData contains the service description and version chrome.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// NotFoundData supplies the footer for an unknown path.
type NotFoundData struct{ Banner page.Banner }

func plain(w http.ResponseWriter, r *http.Request, status int, line string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(line + "\n"))
	}
}

// Handler serves pages and same-origin disconnect forms.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.EscapedPath()
		auth := urls.Auth(r, cfg.ServicesPath)
		b := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: auth + "/", LogoutURL: auth + "/logout"})
		var disconnect string
		for _, p := range cfg.Providers {
			if path == "/"+p.Name+"/disconnect" {
				disconnect = p.Name
				break
			}
		}
		if disconnect != "" {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if !urls.SameOrigin(r) {
				plain(w, r, http.StatusForbidden, urls.CrossOrigin)
				return
			}
			_, err := cfg.Broker.Disconnect(r.Context(), disconnect)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				plain(w, r, http.StatusServiceUnavailable, store.Unreachable)
				return
			}
			w.Header().Set("Location", "/")
			w.WriteHeader(http.StatusFound)
			return
		}
		if path != "/" && path != "/tools" && path != "/about" {
			cfg.Pages.Write(w, r, http.StatusNotFound, "notfound", NotFoundData{Banner: b})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch path {
		case "/":
			caller, _ := identity.FromContext(r.Context())
			cons, err := cfg.Store.List(r.Context(), caller.UserID)
			if err != nil {
				plain(w, r, http.StatusServiceUnavailable, store.Unreachable)
				return
			}
			byProvider := make(map[string]store.Connection, len(cons))
			for _, c := range cons {
				byProvider[c.Provider] = c
			}
			b.Trail = nil
			data := LandingData{Banner: b, Connections: make([]ConnectionRow, 0, len(cfg.Providers))}
			for _, p := range cfg.Providers {
				row := ConnectionRow{Name: p.Name, DisplayName: p.DisplayName, Status: tools.StatusNotConnected}
				if c, ok := byProvider[p.Name]; ok {
					row.Status = tools.StatusOf(p, c)
					row.Account = c.AccountEmail
					row.Scopes = c.Scopes
					row.Connected = c.Created.UTC().Format(time.RFC3339)
					row.ConnectedText = c.Created.UTC().Format("2006-01-02 15:04")
				}
				data.Connections = append(data.Connections, row)
			}
			cfg.Pages.Write(w, r, http.StatusOK, "landing", data)
		case "/tools":
			b.Trail = []page.Level{ToolsLevel}
			data := ToolsData{Banner: b}
			for _, entry := range tools.Catalog() {
				data.Tools = append(data.Tools, ToolRow{Name: entry.Name, Description: tools.FirstLine(entry.Description)})
			}
			cfg.Pages.Write(w, r, http.StatusOK, "tools", data)
		case "/about":
			b.Trail = []page.Level{AboutLevel}
			cfg.Pages.Write(w, r, http.StatusOK, "about", AboutData{Banner: b, Description: Description})
		}
	})
}
