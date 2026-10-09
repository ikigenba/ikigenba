// Package web composes cron's authenticated routes and request telemetry.
package web

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/cron/internal/pages"
	"github.com/ikigenba/ikigenba/cron/internal/scheduler"
	"github.com/ikigenba/ikigenba/cron/internal/store"
	"github.com/ikigenba/ikigenba/cron/internal/tools"
)

// Config supplies the route handlers and their shared state.
type Config struct {
	Banner       func(u page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Scheduler    *scheduler.Scheduler
	Telemetry    *telemetry.Writer
	Events       *events.Emitter
}

// Handler builds the exact route table, with telemetry around every answer.
func Handler(cfg Config) http.Handler {
	set, err := pages.Load()
	if err != nil {
		panic(err)
	}
	tools.Register(cfg.MCP, tools.Config{Store: cfg.Store, Scheduler: cfg.Scheduler})
	ph := pages.Handler(pages.Config{Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Store: cfg.Store, Scheduler: cfg.Scheduler, MCP: cfg.MCP})
	static := page.Static()
	authenticated := events.Middleware(identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mcp":
			cfg.MCP.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, page.StaticPrefix):
			static.ServeHTTP(w, r)
		default:
			ph.ServeHTTP(w, r)
		}
	})))
	delivery := events.DeliveryHandler(nil)
	declarations := events.DeclarationsHandler(cfg.Events, nil)
	return telemetry.Middleware(cfg.Telemetry, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events":
			delivery.ServeHTTP(w, r)
		case "/declarations":
			declarations.ServeHTTP(w, r)
		default:
			authenticated.ServeHTTP(w, r)
		}
	}))
}
