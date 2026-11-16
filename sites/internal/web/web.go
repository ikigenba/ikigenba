// Package web composes the site's routes and request middleware.
package web

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

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
	registered := cfg.MCP.Tools()
	pageTools := make([]pages.Tool, len(registered))
	for i, tool := range registered {
		pageTools[i] = pages.Tool{Name: tool.Name, Description: tool.Description}
	}
	random := cfg.Rand
	if random != nil {
		random = &lockedReader{source: random}
	}
	sc := serving.Config{Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Store: cfg.Store, Cache: cfg.Cache, Telemetry: cfg.Telemetry, Rand: random}
	apex, sites, domains := serving.Apex(sc), serving.Sites(sc), serving.Domains(sc)
	p := pages.Handler(pages.Config{Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Store: cfg.Store, Tools: pageTools})
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
		if strings.Contains(host, ".") {
			base, e := url.Parse(urls.SitesURL(r, cfg.ServicesPath))
			name := ""
			if e == nil {
				h := asciiLower(base.Hostname())
				if strings.HasPrefix(h, "sites.") && len(h) > len("sites.") {
					name = h[len("sites."):]
				}
			}
			gateway := name != "" && host == "sites."+name || name == "" && strings.HasPrefix(host, "sites.")
			if !gateway {
				if i := strings.IndexByte(name, '.'); i >= 0 && host == name[i+1:] {
					apex.ServeHTTP(w, r)
				} else {
					domains.ServeHTTP(w, r)
				}
				return
			}
		}
		switch {
		case r.URL.Path == "/mcp":
			endpoint.ServeHTTP(w, r)
		case r.URL.Path == "/" || r.URL.Path == "/about" || r.URL.Path == "/tools":
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

// lockedReader serializes the source shared by the two site routes.
type lockedReader struct {
	mu     sync.Mutex
	source io.Reader
}

func (r *lockedReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.source.Read(p)
}
