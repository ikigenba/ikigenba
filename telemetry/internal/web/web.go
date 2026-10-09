// Package web serves telemetry's pages, shared assets, ingestion, and tools.
package web

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	assets "github.com/ikigenba/ikigenba/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/store"
	"github.com/ikigenba/ikigenba/telemetry/internal/tools"
)

// ServiceName is the platform service identity.
const ServiceName = "telemetry"

// Description is the service's one-line purpose.
const Description string = "The suite's trail of events"

// NotFound is the copy for an unknown route.
const NotFound string = "not found"

// Config supplies the handler's page, protocol, and trail dependencies.
type Config struct {
	Banner       func(u page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Telemetry    *telemetry.Writer
}

// Handler builds the HTTP surface and registers its four tools.
func Handler(cfg Config) http.Handler {
	templates := template.Must(page.Templates().ParseFS(assets.Assets(), "*.html"))
	tools.Register(cfg.MCP, cfg.Store)
	static := page.Static()
	ingest := telemetry.IngestHandler(cfg.Store)
	routes := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/" || r.URL.Path == "/about":
			servePage(w, r, cfg, templates)
		case r.URL.Path == "/mcp":
			cfg.MCP.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, page.StaticPrefix):
			static.ServeHTTP(w, r)
		default:
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte(NotFound + "\n"))
			}
		}
	})
	protected := telemetry.Middleware(cfg.Telemetry, identity.Require(routes))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == telemetry.IngestPath {
			ingest.ServeHTTP(w, r)
			return
		}
		protected.ServeHTTP(w, r)
	})
}

func servePage(w http.ResponseWriter, r *http.Request, cfg Config, templates *template.Template) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	banner := cfg.Banner(bannerUser(r, cfg.ServicesPath))
	var body bytes.Buffer
	var err error
	if r.URL.Path == "/" {
		err = templates.ExecuteTemplate(&body, "landing", struct{ Banner page.Banner }{banner})
	} else {
		err = templates.ExecuteTemplate(&body, "about", struct {
			Banner      page.Banner
			Description string
		}{banner, Description})
	}
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}

func bannerUser(r *http.Request, path string) page.User {
	origin := ""
	if entries, err := services.Read(path); err == nil {
		if auth, ok := entries.Find("auth"); ok {
			origin = auth.URL
		}
	}
	if origin == "" {
		scheme := r.Header.Get("X-Forwarded-Proto")
		if scheme != "http" && scheme != "https" {
			scheme = "https"
		}
		space := r.Host
		if colon := strings.LastIndexByte(space, ':'); colon >= 0 && strings.Trim(space[colon+1:], "0123456789") == "" {
			space = space[:colon]
		}
		if strings.HasPrefix(space, ServiceName+".") && len(space) > len(ServiceName)+1 {
			space = space[len(ServiceName)+1:]
		}
		origin = scheme + "://auth." + space
	}
	return page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: origin + "/", LogoutURL: origin + "/logout"}
}
