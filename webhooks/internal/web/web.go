// Package web composes webhooks' routes and request telemetry.
package web

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/webhooks/internal/ingress"
	"github.com/ikigenba/ikigenba/webhooks/internal/pages"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
	"github.com/ikigenba/ikigenba/webhooks/internal/tools"
	"github.com/ikigenba/ikigenba/webhooks/internal/urls"
)

// Config supplies the route handlers and their shared state.
type Config struct {
	Banner       func(u page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Telemetry    *telemetry.Writer
	Events       *events.Emitter
}

// Handler builds the exact route table, with telemetry around every answer.
func Handler(cfg Config) http.Handler {
	set, err := pages.Load()
	if err != nil {
		panic(err)
	}
	tools.Register(cfg.MCP, tools.Config{Store: cfg.Store, Telemetry: cfg.Telemetry, Events: cfg.Events})
	ph := pages.Handler(pages.Config{Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Store: cfg.Store})
	in := ingress.Handler(ingress.Config{Store: cfg.Store, Telemetry: cfg.Telemetry, Events: cfg.Events})
	static := page.Static()
	endpoint := events.Middleware(identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.MCP.ServeHTTP(w, r.WithContext(urls.NewContext(r.Context(), urls.Base(r, cfg.ServicesPath))))
	})))
	open := identity.Optional(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, urls.IngressPrefix):
			in.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, page.StaticPrefix):
			static.ServeHTTP(w, r)
		default:
			ph.ServeHTTP(w, r)
		}
	}))
	delivery := events.DeliveryHandler(nil)
	declarations := events.DeclarationsHandler(cfg.Events, nil)
	return telemetry.Middleware(cfg.Telemetry, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events":
			delivery.ServeHTTP(w, r)
		case "/declarations":
			declarations.ServeHTTP(w, r)
		case "/mcp":
			endpoint.ServeHTTP(w, r)
		default:
			open.ServeHTTP(w, r)
		}
	}))
}
