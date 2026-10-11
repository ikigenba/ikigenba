// Package web assembles the service's authenticated HTTP surfaces.
package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/artifacts/internal/fetch"
	"github.com/ikigenba/ikigenba/artifacts/internal/files"
	"github.com/ikigenba/ikigenba/artifacts/internal/pages"
	"github.com/ikigenba/ikigenba/artifacts/internal/settings"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/tools"
	"github.com/ikigenba/ikigenba/artifacts/internal/urls"
)

// Config supplies shared resources for the HTTP surfaces.
type Config struct {
	Banner       func(page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Telemetry    *telemetry.Writer
	Events       *events.Emitter
	Settings     settings.Settings
	Now          func() time.Time
	ImportAfter  func(time.Duration) <-chan time.Time
}

// Handler registers the tools and builds the authenticated route table.
func Handler(cfg Config) http.Handler {
	set, err := pages.Load()
	if err != nil {
		panic(err)
	}
	ph := pages.Handler(pages.Config{Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Store: cfg.Store})
	fh := events.Middleware(files.Handler(files.Config{Store: cfg.Store, Max: cfg.Settings.ArtifactMaxBytes, ServicesPath: cfg.ServicesPath, Telemetry: cfg.Telemetry, Events: cfg.Events}))
	tools.Register(cfg.MCP, tools.Config{Store: cfg.Store, Telemetry: cfg.Telemetry, Events: cfg.Events, Max: cfg.Settings.ArtifactMaxBytes, Now: cfg.Now, Fetch: fetch.Config{ServicesPath: cfg.ServicesPath, Telemetry: cfg.Telemetry, Limit: cfg.Settings.Import(), After: cfg.ImportAfter}})
	mh := events.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.MCP.ServeHTTP(w, r.WithContext(urls.NewContext(r.Context(), urls.Base(r, cfg.ServicesPath))))
	}))
	static := page.Static()
	gated := identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mcp":
			mh.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.EscapedPath(), "/files/"):
			fh.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/_appkit/"):
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
		default:
			gated.ServeHTTP(w, r)
		}
	}))
}
