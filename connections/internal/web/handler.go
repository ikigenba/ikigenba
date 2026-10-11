// Package web assembles the authenticated HTTP surface of connections.
package web

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/connections/internal/broker"
	"github.com/ikigenba/ikigenba/connections/internal/pages"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/tools"
	"github.com/ikigenba/ikigenba/connections/internal/urls"
)

// Config supplies the collaborators and runtime hooks shared by HTTP routes.
type Config struct {
	Banner       func(u page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	CallbackURL  string
	Clients      []providers.Client
	Store        *store.Store
	Telemetry    *telemetry.Writer
	Events       *events.Emitter
	Now          func() time.Time
	Rand         io.Reader
}

// Handler registers tools and assembles the route table without path normalization.
func Handler(cfg Config) http.Handler {
	set, err := pages.Load()
	if err != nil {
		panic(err)
	}
	b := broker.New(broker.Config{Clients: cfg.Clients, Store: cfg.Store, Telemetry: cfg.Telemetry, Events: cfg.Events, CallbackURL: cfg.CallbackURL, ServicesPath: cfg.ServicesPath, Now: cfg.Now, Rand: cfg.Rand})
	provs := make([]providers.Provider, len(cfg.Clients))
	connects := make(map[string]http.Handler, len(cfg.Clients))
	callbacks := make(map[string]http.Handler, len(cfg.Clients))
	for i, cl := range cfg.Clients {
		provs[i] = cl.Provider
		if _, known := connects[cl.Provider.Name]; known {
			continue
		}
		connects[cl.Provider.Name] = events.Middleware(connectRoute(b, cl.Provider.Name))
		callbacks[cl.Provider.Name] = events.Middleware(callbackRoute(b, cfg, set, cl))
	}
	tools.Register(cfg.MCP, tools.Config{Base: urls.Configured(cfg.ServicesPath), Providers: provs, Store: cfg.Store, Broker: b})
	ph := pages.Handler(pages.Config{Banner: cfg.Banner, Pages: set, ServicesPath: cfg.ServicesPath, Providers: provs, Store: cfg.Store, Broker: b})
	disconnect := events.Middleware(ph)
	mh := events.Middleware(cfg.MCP)
	th := events.Middleware(tokenRoute(b, cfg))
	static := page.Static()
	gated := identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mcp":
			mh.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/token/"):
			th.ServeHTTP(w, r)
		default:
			name, action := providerPath(r.URL.EscapedPath())
			if _, known := connects[name]; known {
				switch {
				case action == "connect" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
					connects[name].ServeHTTP(w, r)
					return
				case action == "callback" && r.Method == http.MethodGet:
					callbacks[name].ServeHTTP(w, r)
					return
				case action == "disconnect" && r.Method == http.MethodPost:
					disconnect.ServeHTTP(w, r)
					return
				}
			}
			if strings.HasPrefix(r.URL.Path, "/_appkit/") {
				static.ServeHTTP(w, r)
				return
			}
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

func providerPath(path string) (string, string) {
	if !strings.HasPrefix(path, "/") {
		return "", ""
	}
	name, action, ok := strings.Cut(path[1:], "/")
	if !ok || name == "" || strings.ContainsAny(name, "/%") {
		return "", ""
	}
	switch action {
	case "connect", "callback", "disconnect":
		return name, action
	}
	return "", ""
}
