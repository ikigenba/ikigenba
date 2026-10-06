// Package web assembles repos' authenticated pages, tools and git routes.
package web

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/clone"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/smarthttp"
	"github.com/ikigenba/ikigenba/repos/internal/store"
	"github.com/ikigenba/ikigenba/repos/internal/tools"
)

// ServiceName is repos' name in the platform service catalog.
const ServiceName = "repos"

// Description is the service's one-line description.
const Description = "Git repositories for the suite's content"

// Config supplies the handler's banner, tools, repositories and request writer.
type Config struct {
	Banner       func(u page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Git          *git.Git
	Limits       *limits.Limits
	Telemetry    *telemetry.Writer
	Events       *events.Emitter
}

// Handler registers the tools and wraps every route in the common middleware.
func Handler(cfg Config) http.Handler {
	tools.Register(cfg.MCP, tools.Config{Store: cfg.Store, Limits: cfg.Limits, Telemetry: cfg.Telemetry})
	pages := newPages(cfg)
	static := page.Static()
	gitHTTP := smarthttp.Handler(smarthttp.Config{Store: cfg.Store, Git: cfg.Git, Limits: cfg.Limits, Telemetry: cfg.Telemetry, Events: cfg.Events})
	routes := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/" || r.URL.Path == "/about":
			pages.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, page.StaticPrefix):
			static.ServeHTTP(w, r)
		case r.URL.Path == "/mcp":
			ctx := clone.NewContext(r.Context(), clone.Base(r, cfg.ServicesPath))
			cfg.MCP.ServeHTTP(w, r.WithContext(ctx))
		case isGitPath(r.URL.Path):
			gitHTTP.ServeHTTP(w, r)
		default:
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte("not found\n"))
			}
		}
	})
	authenticated := events.Middleware(identity.Require(routes))
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

func isGitPath(path string) bool {
	if !strings.HasPrefix(path, "/") {
		return false
	}
	segment, _, _ := strings.Cut(path[1:], "/")
	return strings.HasSuffix(segment, ".git")
}
