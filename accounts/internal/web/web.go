// Package web composes the service's authenticated and guest routes.
package web

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/daemonapi"
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
	Cloud        cloud.Cloud
	Hosts        settings.Hosts
	Configured   bool
	Wake         func()
	Daemon       *daemonapi.Server
	Telemetry    *telemetry.Writer
	Settings     settings.Settings
}

// Handler registers the service's tools and composes its routes.
func Handler(cfg Config) http.Handler {
	tools.Register(cfg.MCP, tools.Config{
		Store: cfg.Store, Cloud: cfg.Cloud, Hosts: cfg.Hosts, Configured: cfg.Configured, Wake: cfg.Wake, Telemetry: cfg.Telemetry,
	})
	set, err := pages.Load()
	if err != nil {
		panic(err)
	}
	ph := identity.Optional(pages.Handler(pages.Config{
		Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Store: cfg.Store,
		MCP: cfg.MCP, Configured: cfg.Configured, Domain: cfg.Hosts.Domain, Now: cfg.Telemetry.Now,
	}))
	mh := identity.Require(cfg.MCP)
	sh := identity.Optional(page.Static())
	return telemetry.Middleware(cfg.Telemetry, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/mcp":
			mh.ServeHTTP(w, r)
		case strings.HasPrefix(path, "/_appkit/"):
			sh.ServeHTTP(w, r)
		case path == "/accountsd" || strings.HasPrefix(path, "/accountsd/"):
			cfg.Daemon.ServeHTTP(w, r)
		default:
			ph.ServeHTTP(w, r)
		}
	}))
}
