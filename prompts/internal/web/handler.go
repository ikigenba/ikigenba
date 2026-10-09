// Package web assembles the service routes and serves run files.
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
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"github.com/ikigenba/ikigenba/prompts/internal/tools"
)

// Config supplies the shared dependencies for the service handler.
type Config struct {
	Banner              func(u page.User) page.Banner
	MCP                 *mcp.Server
	ServicesPath        string
	Store               *store.Store
	Runs                *runs.Core
	KeepDays, KeepCount int64
	Telemetry           *telemetry.Writer
}

// Handler registers tools and wraps all routes with request telemetry.
func Handler(cfg Config) http.Handler {
	set, err := pages.Load()
	if err != nil {
		panic(err)
	}
	tools.Register(cfg.MCP, tools.Config{Store: cfg.Store, Runs: cfg.Runs, Telemetry: cfg.Telemetry})
	p := pages.Handler(pages.Config{Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Store: cfg.Store, Runs: cfg.Runs, KeepDays: cfg.KeepDays, KeepCount: cfg.KeepCount})
	f := Files(FilesConfig{Banner: cfg.Banner, Pages: set, Store: cfg.Store, Runs: cfg.Runs})
	static := page.Static()
	handlers := events.Handlers{"*": cfg.Runs.Deliver}
	delivery := events.DeliveryHandler(handlers)
	declarations := events.DeclarationsHandler(nil, handlers)
	gated := identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mcp":
			cfg.MCP.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, page.StaticPrefix):
			static.ServeHTTP(w, r)
		case isRunFile(r):
			f.ServeHTTP(w, r)
		default:
			p.ServeHTTP(w, r)
		}
	}))
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

func pieces(r *http.Request) []string {
	parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
	for i, s := range parts {
		if decoded, err := url.PathUnescape(s); err == nil {
			parts[i] = decoded
		}
	}
	return parts
}
func isRunFile(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, page.StaticPrefix) {
		return false
	}
	p := pieces(r)
	return len(p) >= 4 && p[1] == "runs" && (len(p) != 4 || p[3] != "")
}
