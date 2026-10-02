// Package gateway serves the suite's MCP endpoint and connect page.
package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// ServiceName is the gateway's platform name.
const ServiceName = "mcp"

// DefaultBudget bounds all backend requests made for one gateway call.
const DefaultBudget = 50 * time.Second

// Config supplies the handler's sources and backend policy.
type Config struct {
	Banner       func(u page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Budget       time.Duration
	Telemetry    *telemetry.Writer
}

type requestState struct {
	cfg      Config
	entries  services.List
	reached  []string
	received time.Time
}
type requestStateKey struct{}

func requestStateFrom(ctx context.Context) *requestState {
	state, _ := ctx.Value(requestStateKey{}).(*requestState)
	return state
}

// Handler builds the gateway HTTP surface and requires nginx's caller identity.
func Handler(cfg Config) http.Handler {
	if cfg.Budget <= 0 {
		cfg.Budget = DefaultBudget
	}
	routes := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received := time.Now()
		entries, err := services.Read(cfg.ServicesPath)
		if err != nil {
			entries = nil
		}
		path := r.URL.Path
		if path == "/mcp" || strings.HasPrefix(path, "/mcp/") {
			reached := mcpNames(entries)
			if path != "/mcp" {
				var ok bool
				reached, ok = scopeNames(strings.TrimPrefix(path, "/mcp/"))
				if !ok {
					gatewayNotFound(w, r)
					return
				}
			}
			state := &requestState{cfg: cfg, entries: entries, reached: reached, received: received}
			cfg.MCP.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestStateKey{}, state)))
			return
		}
		if strings.HasPrefix(path, "/_appkit/") {
			serveAssets(w, r)
			return
		}
		serveConnect(w, r, cfg, entries)
	})
	return telemetry.Middleware(cfg.Telemetry, identity.Require(routes))
}
