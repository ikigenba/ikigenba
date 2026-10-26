// Package web composes ledger' authenticated and guest routes.
package web

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/ledger/internal/pages"
	"github.com/ikigenba/ikigenba/ledger/internal/settings"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
	"github.com/ikigenba/ikigenba/ledger/internal/tools"
)

// Config supplies the shared services of every route.
type Config struct {
	Banner       func(page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Telemetry    *telemetry.Writer
	Events       *events.Emitter
	Artifacts    *artifacts.Client
	Settings     settings.Settings
}

// Handler builds the service's routes, registering its tools once.
func Handler(cfg Config) http.Handler {
	tools.Register(cfg.MCP, tools.Config{Store: cfg.Store, Telemetry: cfg.Telemetry, Events: cfg.Events, Artifacts: cfg.Artifacts})
	ps, err := pages.Load()
	if err != nil {
		panic(err)
	}
	ph := identity.Optional(pages.Handler(pages.Config{Banner: cfg.Banner, Pages: ps, ServicesPath: cfg.ServicesPath, Store: cfg.Store, MCP: cfg.MCP}))
	mh := identity.Require(events.Middleware(cfg.MCP))
	sh := identity.Optional(page.Static())
	dh := events.DeclarationsHandler(cfg.Events, nil)
	eh := events.DeliveryHandler(nil)
	return telemetry.Middleware(cfg.Telemetry, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/events":
			eh.ServeHTTP(w, r)
		case r.URL.Path == "/declarations":
			dh.ServeHTTP(w, r)
		case r.URL.Path == "/mcp":
			mh.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/_appkit/"):
			sh.ServeHTTP(w, r)
		default:
			ph.ServeHTTP(w, r)
		}
	}))
}
