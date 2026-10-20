// Package web assembles the app's HTTP routes.
package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/pages"
	"github.com/ikigenba/ikigenba/scrapes/internal/runs"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/tools"
)

// Config supplies the shared dependencies of all routes.
type Config struct {
	Banner       func(u page.User) page.Banner
	MCP          *mcp.Server
	ServicesPath string
	Store        *store.Store
	Scrapers     *scrapers.Registry
	Runs         *runs.Engine
	Telemetry    *telemetry.Writer
	Events       *events.Emitter
}

// Read refusal copy.
const (
	NotFound = "The scraper or run was not found."
	NoRun    = "This scraper has no completed run."
)

// Handler builds the route table without cleaning or redirecting paths.
func Handler(cfg Config) http.Handler {
	s, err := pages.Load()
	if err != nil {
		panic(err)
	}
	tools.Register(cfg.MCP, tools.Config{Runs: cfg.Runs, Scrapers: cfg.Scrapers, Store: cfg.Store})
	ph := pages.Handler(pages.Config{Banner: cfg.Banner, Pages: s, ServicesPath: cfg.ServicesPath, Store: cfg.Store, Scrapers: cfg.Scrapers})
	static := page.Static()
	mcpHandler := events.Middleware(cfg.MCP)
	delivery := events.DeliveryHandler(cfg.Runs.Handlers())
	declarations := events.DeclarationsHandler(cfg.Events, cfg.Runs.Handlers())
	protected := identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mcp":
			mcpHandler.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/_appkit/"):
			static.ServeHTTP(w, r)
		default:
			kind, key := sibling(r.URL.EscapedPath())
			if kind != "" {
				read(cfg, w, r, kind, key)
			} else {
				ph.ServeHTTP(w, r)
			}
		}
	}))
	return telemetry.Middleware(cfg.Telemetry, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events":
			delivery.ServeHTTP(w, r)
		case "/declarations":
			declarations.ServeHTTP(w, r)
		default:
			protected.ServeHTTP(w, r)
		}
	}))
}

func sibling(path string) (kind, key string) {
	parts := strings.Split(path, "/")
	if len(parts) != 4 || parts[0] != "" || parts[2] == "" {
		return "", ""
	}
	if parts[1] == "scrapers" && parts[3] == "current" {
		return "current", parts[2]
	}
	if parts[1] == "runs" && (parts[3] == "record" || parts[3] == "rows") {
		return parts[3], parts[2]
	}
	return "", ""
}

func refusal(w http.ResponseWriter, r *http.Request, status int, line string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(line + "\n"))
	}
}

func read(cfg Config, w http.ResponseWriter, r *http.Request, kind, key string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var run store.Run
	var err error
	missing := NotFound
	if kind == "current" {
		if _, ok := cfg.Scrapers.Get(key); !ok {
			refusal(w, r, 404, NotFound)
			return
		}
		run, err = cfg.Store.CurrentRun(r.Context(), key)
		missing = NoRun
	} else {
		run, err = cfg.Store.GetRun(r.Context(), key)
	}
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			refusal(w, r, 404, missing)
		} else {
			refusal(w, r, 503, store.Unreachable)
		}
		return
	}
	if kind != "rows" {
		b, _ := json.Marshal(runs.RecordOf(run))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(append(b, '\n'))
		}
		return
	}
	started := false
	begin := func() {
		if started {
			return
		}
		started = true
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
	err = cfg.Store.EachRow(r.Context(), key, func(row source.Row) error {
		begin()
		if r.Method == http.MethodHead {
			return nil
		}
		b, marshalErr := json.Marshal(row)
		if marshalErr != nil {
			return marshalErr
		}
		stamp, _ := json.Marshal(row.Scraped.UTC().Format(time.RFC3339))
		compat := append([]byte(`,"collected":`), stamp...)
		compat = append(compat, []byte(`,"digest":`)...)
		b = bytes.Replace(b, []byte(`,"digest":`), compat, 1)
		_, writeErr := w.Write(append(b, '\n'))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return writeErr
	})
	if err != nil && !started {
		line := store.Unreachable
		if errors.Is(err, store.ErrMissing) {
			line = store.Missing
		}
		refusal(w, r, 503, line)
		return
	}
	begin()
}
