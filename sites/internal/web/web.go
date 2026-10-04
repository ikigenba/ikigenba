// Package web composes the site's routes and request middleware.
package web

import (
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cache"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
	"github.com/ikigenba/ikigenba/sites/internal/serving"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
	"github.com/ikigenba/ikigenba/sites/internal/urls"
)

// Config supplies the composed handler's dependencies.
type Config struct {
	Banner       func(u page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Cache        *cache.Cache
	Limits       *limits.Limits
	Telemetry    *telemetry.Writer
	Rand         io.Reader
}

// Handler registers the tools and wraps every route in request telemetry.
func Handler(cfg Config) http.Handler {
	set, err := pages.Load()
	if err != nil {
		panic(err)
	}
	tools.Register(cfg.MCP, tools.Config{Store: cfg.Store, Cache: cfg.Cache, Limits: cfg.Limits, Telemetry: cfg.Telemetry})
	sc := serving.Config{Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Store: cfg.Store, Cache: cfg.Cache, Telemetry: cfg.Telemetry, Rand: cfg.Rand}
	apex, sites := serving.Apex(sc), serving.Sites(sc)
	p := pages.Handler(pages.Config{Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Store: cfg.Store})
	static := page.Static()
	endpoint := identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.MCP.ServeHTTP(w, r.WithContext(urls.NewContext(r.Context(), urls.SitesURL(r, cfg.ServicesPath))))
	}))
	routes := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, e := net.SplitHostPort(host); e == nil {
			host = h
		}
		host = asciiLower(host)
		if host != "sites" && !strings.HasPrefix(host, "sites.") {
			apex.ServeHTTP(w, r)
			return
		}
		switch {
		case r.URL.Path == "/mcp":
			endpoint.ServeHTTP(w, r)
		case r.URL.Path == "/" || r.URL.Path == "/about":
			p.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/_appkit/"):
			static.ServeHTTP(w, r)
		default:
			sites.ServeHTTP(w, r)
		}
	})
	return telemetry.Middleware(cfg.Telemetry, identity.Optional(routes))
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
