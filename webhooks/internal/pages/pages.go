// Package pages renders webhooks' signed-in pages from embedded templates.
package pages

import (
	"bytes"
	"html/template"
	"net/http"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/webhooks"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
	"github.com/ikigenba/ikigenba/webhooks/internal/tools"
	"github.com/ikigenba/ikigenba/webhooks/internal/urls"
)

// ServiceName identifies webhooks in shared platform components.
const ServiceName = urls.Service

// Description is webhooks' manifest and about description.
const Description string = "Webhooks that turn deliveries from outside into events"

// The levels of the two pages below the root, named by their path segments.
var (
	ToolsLevel = page.Level{Name: "tools", URL: "/tools"}
	AboutLevel = page.Level{Name: "about", URL: "/about"}
)

// Set holds the parsed embedded page templates.
type Set struct{ templates *template.Template }

// LandingData supplies chrome and ordered webhook rows.
type LandingData struct {
	Banner   page.Banner
	Webhooks []WebhookRow
}

// WebhookRow contains the public display fields of one webhook.
type WebhookRow struct {
	ID, Slug, Scheme, URL, Owner   string
	Mine                           bool
	LastReceived, LastReceivedText string
}

// ToolsData supplies chrome and the tools in registration order.
type ToolsData struct {
	Banner page.Banner
	Tools  []ToolRow
}

// ToolRow names one tool and the first line of its description.
type ToolRow struct{ Name, Description string }

// AboutData supplies chrome and the service description.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// NoticeData supplies the footer of a not-found page.
type NoticeData struct{ Banner page.Banner }

// Config supplies the sources used to draw pages.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Store        *store.Store
}

// Load parses webhooks' assets into appkit's template set.
func Load() (*Set, error) {
	t, err := page.Templates().ParseFS(webhooks.Assets(), "*.html")
	if err != nil {
		return nil, err
	}
	return &Set{templates: t}, nil
}

// Write sends a rendered page, preserving headers other than Content-Type.
func (s *Set) Write(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var b bytes.Buffer
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
	base := urls.Auth(r, path)
	return page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: base + "/", LogoutURL: base + "/logout"}
}

func row(x store.Webhook, user, base string) WebhookRow {
	r := WebhookRow{ID: x.ID, Slug: x.Slug, Scheme: x.Scheme, URL: urls.Hook(base, x.Slug), Owner: x.OwnerEmail, Mine: x.OwnerID == user}
	if !x.LastReceived.IsZero() {
		r.LastReceived = x.LastReceived.UTC().Format(time.RFC3339)
		r.LastReceivedText = x.LastReceived.UTC().Format("2006-01-02 15:04")
	}
	return r
}

// Handler answers the page paths and renders the not-found page for every other path.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		isPage := path == "/" || path == "/tools" || path == "/about"
		if isPage && r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		caller, _ := identity.FromContext(r.Context())
		if isPage && caller.UserID == "" {
			w.Header().Set("Location", urls.SignIn(r, cfg.ServicesPath))
			w.WriteHeader(http.StatusFound)
			return
		}
		switch path {
		case "/":
			entries, err := cfg.Store.List(r.Context())
			if err != nil {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				if r.Method != http.MethodHead {
					_, _ = w.Write([]byte(store.Unreachable + "\n"))
				}
				return
			}
			base := urls.Base(r, cfg.ServicesPath)
			data := LandingData{Banner: cfg.Banner(bannerUser(r, cfg.ServicesPath)), Webhooks: make([]WebhookRow, 0, len(entries))}
			for _, x := range entries {
				data.Webhooks = append(data.Webhooks, row(x, caller.UserID, base))
			}
			cfg.Pages.Write(w, r, http.StatusOK, "landing", data)
		case "/tools":
			b := cfg.Banner(bannerUser(r, cfg.ServicesPath))
			b.Trail = []page.Level{ToolsLevel}
			data := ToolsData{Banner: b}
			for _, t := range tools.Catalog() {
				data.Tools = append(data.Tools, ToolRow{Name: t.Name, Description: tools.FirstLine(t.Description)})
			}
			cfg.Pages.Write(w, r, http.StatusOK, "tools", data)
		case "/about":
			b := cfg.Banner(bannerUser(r, cfg.ServicesPath))
			b.Trail = []page.Level{AboutLevel}
			cfg.Pages.Write(w, r, http.StatusOK, "about", AboutData{Banner: b, Description: Description})
		default:
			cfg.Pages.Write(w, r, http.StatusNotFound, "notfound", NoticeData{Banner: cfg.Banner(bannerUser(r, cfg.ServicesPath))})
		}
	})
}
