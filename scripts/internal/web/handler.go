// Package web assembles scripts' authenticated HTTP routes.
package web

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/pages"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/source"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
	"github.com/ikigenba/ikigenba/scripts/internal/tools"
)

// Config supplies the HTTP service's dependencies.
type Config struct {
	Banner       func(page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Source       *source.Source
	Runs         *runs.Core
	Limits       *limits.Limits
	Telemetry    *telemetry.Writer
}

// Handler mounts the pages, files, tools and shared assets behind identity.
func Handler(cfg Config) http.Handler {
	set, err := pages.Load()
	if err != nil {
		panic(err)
	}
	settings := cfg.Limits.Settings()
	p := pages.Handler(pages.Config{Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Store: cfg.Store, Source: cfg.Source, Runs: cfg.Runs, KeepDays: settings.RunKeepDays, KeepCount: settings.RunKeepCount, TreeMaxBytes: settings.TreeMaxBytes, OperationSeconds: settings.OperationSeconds})
	f := Files(FilesConfig{Banner: cfg.Banner, Pages: set, Store: cfg.Store, Runs: cfg.Runs})
	static := page.Static()
	tools.Register(cfg.MCP, tools.Config{Store: cfg.Store, Source: cfg.Source, Runs: cfg.Runs, Telemetry: cfg.Telemetry})
	routes := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, page.StaticPrefix):
			static.ServeHTTP(w, r)
		case r.URL.Path == "/mcp":
			cfg.MCP.ServeHTTP(w, r)
		case runFilePath(r):
			f.ServeHTTP(w, r)
		default:
			p.ServeHTTP(w, r)
		}
	})
	gated := identity.Require(routes)
	handlers := events.Handlers{"*": cfg.Runs.Deliver}
	delivery := events.DeliveryHandler(handlers)
	declarations := events.DeclarationsHandler(nil, handlers)
	return telemetry.Middleware(cfg.Telemetry, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case events.EventsPath:
			delivery.ServeHTTP(w, r)
		case events.DeclarationsPath:
			declarations.ServeHTTP(w, r)
		default:
			gated.ServeHTTP(w, r)
		}
	}))
}

func runFilePath(r *http.Request) bool {
	pieces := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
	for i, piece := range pieces {
		if decoded, err := url.PathUnescape(piece); err == nil {
			pieces[i] = decoded
		}
	}
	return len(pieces) >= 4 && pieces[1] == "runs" && (len(pieces) != 4 || pieces[3] != "")
}
