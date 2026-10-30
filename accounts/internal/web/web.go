// Package web composes the service's authenticated and guest routes.
package web

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Config supplies the shared dependencies of every route.
type Config struct {
	Banner       func(u page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Host         host.Host
	Telemetry    *telemetry.Writer
	Settings     settings.Settings
}

// Handler registers the service's tools and composes its routes.
func Handler(cfg Config) http.Handler {
	tools.Register(cfg.MCP, tools.Config{Store: cfg.Store, Host: cfg.Host, Telemetry: cfg.Telemetry})
	set, err := pages.Load()
	if err != nil {
		panic(err)
	}
	ph := identity.Optional(pages.Handler(pages.Config{Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Store: cfg.Store, Host: cfg.Host, MCP: cfg.MCP}))
	mh := identity.Require(cfg.MCP)
	sh := identity.Optional(page.Static())
	return telemetry.Middleware(cfg.Telemetry, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mcp":
			mh.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/_appkit/"):
			sh.ServeHTTP(w, r)
		default:
			ph.ServeHTTP(w, r)
		}
	}))
}
