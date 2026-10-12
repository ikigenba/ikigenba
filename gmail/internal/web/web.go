// Package web wires the connector's authenticated HTTP surface.
package web

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gmail/internal/drafts"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/pages"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/tools"
	"github.com/ikigenba/ikigenba/gmail/internal/transfers"
)

// Config supplies the routes’ shared services and identity banner.
type Config struct {
	Banner       func(u page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Transfers    *transfers.Engine
	Drafts       *drafts.Service
	Gmail        *gmail.Client
	Telemetry    *telemetry.Writer
	Events       *events.Emitter
}

// Handler registers tools and wraps every route in request telemetry.
func Handler(cfg Config) http.Handler {
	s, err := pages.Load()
	if err != nil {
		panic(err)
	}
	tools.Register(cfg.MCP, tools.Config{Transfers: cfg.Transfers, Drafts: cfg.Drafts, Gmail: cfg.Gmail, Store: cfg.Store})
	ph := pages.Handler(pages.Config{Banner: cfg.Banner, Pages: s, ServicesPath: cfg.ServicesPath, Store: cfg.Store})
	static := page.Static()
	delivery := events.DeliveryHandler(nil)
	declarations := events.DeclarationsHandler(cfg.Events, nil)
	protected := identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mcp":
			cfg.MCP.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/_appkit/"):
			static.ServeHTTP(w, r)
		default:
			ph.ServeHTTP(w, r)
		}
	}))
	routes := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events":
			delivery.ServeHTTP(w, r)
		case "/declarations":
			declarations.ServeHTTP(w, r)
		default:
			protected.ServeHTTP(w, r)
		}
	})
	return telemetry.Middleware(cfg.Telemetry, events.Middleware(routes))
}
