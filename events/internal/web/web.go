// Package web routes events' socket HTTP requests.
package web

import (
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Pages supplies the three page routes.
type Pages interface {
	Landing(w http.ResponseWriter, r *http.Request)
	About(w http.ResponseWriter, r *http.Request)
	NotFound(w http.ResponseWriter, r *http.Request)
}

// Config supplies the socket endpoints and request writer.
type Config struct {
	Pages     Pages
	MCP       *mcp.Server
	Sink      events.Sink
	Telemetry *telemetry.Writer
}

// Handler composes routing, caller identity and request telemetry.
func Handler(cfg Config) http.Handler {
	static := page.Static()
	emit := events.EmitHandler(cfg.Sink)
	public := telemetry.Middleware(cfg.Telemetry, identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/" || r.URL.Path == "/about":
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if r.URL.Path == "/" {
				cfg.Pages.Landing(w, r)
			} else {
				cfg.Pages.About(w, r)
			}
		case r.URL.Path == "/mcp":
			cfg.MCP.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, page.StaticPrefix):
			static.ServeHTTP(w, r)
		default:
			cfg.Pages.NotFound(w, r)
		}
	})))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/emit" {
			emit.ServeHTTP(w, r)
			return
		}
		public.ServeHTTP(w, r)
	})
}
