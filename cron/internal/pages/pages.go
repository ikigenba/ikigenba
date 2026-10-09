// Package pages renders cron's signed-in pages from embedded templates.
package pages

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	cron "github.com/ikigenba/ikigenba/cron"
	"github.com/ikigenba/ikigenba/cron/internal/scheduler"
	"github.com/ikigenba/ikigenba/cron/internal/store"
)

// ServiceName identifies cron in shared platform components.
const ServiceName = "cron"

// Description is cron's manifest and about description.
const Description string = "Triggers that emit events on a schedule"

// Set holds the parsed embedded page templates.
type Set struct{ templates *template.Template }

// LandingData supplies chrome and ordered trigger rows.
type LandingData struct {
	Banner   page.Banner
	Triggers []TriggerRow
}

// TriggerRow contains the public display fields of one trigger.
type TriggerRow struct {
	ID, Slug, When, Owner, Status string
	Mine                          bool
	LastFired, LastFiredText      string
	Next, NextText                string
}

// AboutData supplies chrome and the service description.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// ToolsData supplies chrome and the server's registered tools.
type ToolsData struct {
	Banner page.Banner
	Tools  []Tool
}

// Tool contains the name and whole description of a registered tool.
type Tool struct {
	Name, Description string
}

// NoticeData supplies the footer of a not-found page.
type NoticeData struct{ Banner page.Banner }

// Config supplies the sources used to draw pages.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Store        *store.Store
	Scheduler    *scheduler.Scheduler
	MCP          *mcp.Server
}

// Load parses cron's assets into appkit's template set.
func Load() (*Set, error) {
	t, err := page.Templates().ParseFS(cron.Assets(), "*.html")
	if err != nil {
		return nil, err
	}
	return &Set{templates: t}, nil
}

// Write sends a rendered page, preserving headers other than Content-Type.
func (s *Set) Write(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var b bytes.Buffer
	// Loaded templates execute with the data types declared by this package.
	if err := s.templates.ExecuteTemplate(&b, name, data); err != nil {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b.Bytes())
	}
}

func bannerUser(r *http.Request, path string) page.User {
	base := ""
	if entries, err := services.Read(path); err == nil {
		if auth, ok := entries.Find("auth"); ok {
			base = auth.URL
		}
	}
	if base == "" {
		scheme := r.Header.Get("X-Forwarded-Proto")
		if scheme != "http" && scheme != "https" {
			scheme = "https"
		}
		host := r.Host
		if i := strings.LastIndexByte(host, ':'); i >= 0 {
			digits := true
			for _, b := range []byte(host[i+1:]) {
				if b < '0' || b > '9' {
					digits = false
					break
				}
			}
			if digits {
				host = host[:i]
			}
		}
		if strings.HasPrefix(host, "cron.") && len(host) > 5 {
			host = host[5:]
		}
		base = scheme + "://auth." + host
	}
	return page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: base + "/", LogoutURL: base + "/logout"}
}
func row(t store.Trigger, user string, s *scheduler.Scheduler) TriggerRow {
	r := TriggerRow{ID: t.ID, Slug: t.Slug, When: t.When, Owner: t.OwnerEmail, Status: t.Status, Mine: t.OwnerID == user}
	if !t.LastFired.IsZero() {
		r.LastFired = t.LastFired.UTC().Format(time.RFC3339)
		r.LastFiredText = t.LastFired.UTC().Format("2006-01-02 15:04")
	}
	if n, ok := s.Next(t.ID); ok {
		r.Next = n.UTC().Format(time.RFC3339)
		r.NextText = n.UTC().Format("2006-01-02 15:04")
	}
	return r
}

// Handler answers exact page paths and renders notices for all other paths.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if (path == "/" || path == "/about" || path == "/tools") && r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if path == "/" {
			entries, err := cfg.Store.List(r.Context())
			if err != nil {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				if r.Method != http.MethodHead {
					_, _ = w.Write([]byte(store.Unreachable + "\n"))
				}
				return
			}
			data := LandingData{Banner: cfg.Banner(bannerUser(r, cfg.ServicesPath)), Triggers: make([]TriggerRow, 0, len(entries))}
			data.Banner.Trail = nil
			caller, _ := identity.FromContext(r.Context())
			for _, t := range entries {
				data.Triggers = append(data.Triggers, row(t, caller.UserID, cfg.Scheduler))
			}
			cfg.Pages.Write(w, r, http.StatusOK, "landing", data)
			return
		}
		b := cfg.Banner(bannerUser(r, cfg.ServicesPath))
		b.Trail = nil
		if path == "/about" {
			b.Trail = []page.Level{{Name: "about", URL: "/about"}}
			cfg.Pages.Write(w, r, http.StatusOK, "about", AboutData{Banner: b, Description: Description})
			return
		}
		if path == "/tools" {
			b.Trail = []page.Level{{Name: "tools", URL: "/tools"}}
			registered := cfg.MCP.Tools()
			data := ToolsData{Banner: b, Tools: make([]Tool, 0, len(registered))}
			for _, t := range registered {
				data.Tools = append(data.Tools, Tool{Name: t.Name, Description: t.Description})
			}
			cfg.Pages.Write(w, r, http.StatusOK, "tools", data)
			return
		}
		cfg.Pages.Write(w, r, http.StatusNotFound, "notfound", NoticeData{Banner: b})
	})
}
