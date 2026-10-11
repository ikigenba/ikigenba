// Package web composes the service's route table and middleware.
package web

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gdrive/internal/drive"
	"github.com/ikigenba/ikigenba/gdrive/internal/pages"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
	"github.com/ikigenba/ikigenba/gdrive/internal/tools"
	"github.com/ikigenba/ikigenba/gdrive/internal/transfers"
)

// Config supplies the dependencies used by the handler.
type Config struct {
	Banner       func(u page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Transfers    *transfers.Engine
	Drive        *drive.Client
	Telemetry    *telemetry.Writer
	Events       *events.Emitter
}

// Handler builds the HTTP handler from its dependencies.
func Handler(cfg Config) http.Handler {
	s, e := pages.Load()
	if e != nil {
		panic(e)
	}
	ph := pages.Handler(pages.Config{Banner: cfg.Banner, Pages: s, ServicesPath: cfg.ServicesPath, Store: cfg.Store})
	tools.Register(cfg.MCP, tools.Config{Transfers: cfg.Transfers, Drive: cfg.Drive, Store: cfg.Store})
	static := page.Static()
	delivery := events.DeliveryHandler(nil)
	declarations := events.DeclarationsHandler(cfg.Events, nil)
	private := identity.Require(events.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mcp":
			cfg.MCP.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/_appkit/"):
			static.ServeHTTP(w, r)
		default:
			ph.ServeHTTP(w, r)
		}
	})))
	return telemetry.Middleware(cfg.Telemetry, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events":
			delivery.ServeHTTP(w, r)
		case "/declarations":
			declarations.ServeHTTP(w, r)
		default:
			private.ServeHTTP(w, r)
		}
	}))
}
