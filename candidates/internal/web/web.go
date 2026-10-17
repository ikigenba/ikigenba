// Package web assembles the authenticated routes and event endpoints.
package web

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/pages"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// Config supplies the handler dependencies.
type Config struct {
	Banner       func(u page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Passes       *passes.Engine
	Telemetry    *telemetry.Writer
	Events       *events.Emitter
	Website      *website.Checker
}

// Handler builds the service HTTP handler.
func Handler(cfg Config) http.Handler {
	set, err := pages.Load()
	if err != nil {
		panic(err)
	}
	tools.Register(cfg.MCP, tools.Config{Passes: cfg.Passes, Store: cfg.Store, Website: cfg.Website})
	ph := pages.Handler(pages.Config{Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Store: cfg.Store})
	static := page.Static()
	gated := identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mcp":
			cfg.MCP.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/_appkit/"):
			static.ServeHTTP(w, r)
		default:
			ph.ServeHTTP(w, r)
		}
	}))
	delivery := events.DeliveryHandler(cfg.Passes.Handlers())
	declarations := events.DeclarationsHandler(cfg.Events, cfg.Passes.Handlers())
	routes := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events":
			delivery.ServeHTTP(w, r)
		case "/declarations":
			declarations.ServeHTTP(w, r)
		default:
			gated.ServeHTTP(w, r)
		}
	})
	return telemetry.Middleware(cfg.Telemetry, events.Middleware(routes))
}
