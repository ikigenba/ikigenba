// Package web assembles sessions' authenticated routes over one store.
package web

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
	"github.com/ikigenba/ikigenba/sessions/internal/settings"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
)

// Config supplies the common state and identity sources for every route.
type Config struct {
	Banner       func(u page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Telemetry    *telemetry.Writer
	Settings     settings.Settings
}

// Handler registers the tools once and dispatches paths without cleaning them.
func Handler(cfg Config) http.Handler {
	tools.Register(cfg.MCP, tools.Config{Store: cfg.Store, Telemetry: cfg.Telemetry})
	set, err := pages.Load()
	if err != nil {
		panic(err)
	}
	pagesHandler := identity.Optional(pages.Handler(pages.Config{
		Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath,
		Store: cfg.Store, MCP: cfg.MCP,
	}))
	mcpHandler := identity.Require(cfg.MCP)
	staticHandler := identity.Optional(page.Static())
	routes := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mcp":
			mcpHandler.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/_appkit/"):
			staticHandler.ServeHTTP(w, r)
		default:
			pagesHandler.ServeHTTP(w, r)
		}
	})
	return telemetry.Middleware(cfg.Telemetry, routes)
}
