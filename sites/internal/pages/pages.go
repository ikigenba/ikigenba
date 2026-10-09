package pages

import (
	"html/template"
	"io"
	"net/http"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/sites"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/urls"
)

// Set holds the embedded site templates and shared page chrome.
type Set struct{ templates *template.Template }

// Load parses the embedded templates into a fresh shared template set.
func Load() (*Set, error) {
	t, err := page.Templates().ParseFS(sites.Assets(), "*.html")
	if err != nil {
		return nil, err
	}
	return &Set{templates: t}, nil
}

// Write sends a named page, retaining headers supplied by the caller.
func (s *Set) Write(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_ = s.templates.ExecuteTemplate(w, name, data)
	}
}

// LandingData supplies the banner, address, and visible catalog rows.
type LandingData struct {
	Banner   page.Banner
	SitesURL string
	Sites    []SiteRow
}

// SiteRow exposes only the site's public presentation fields.
type SiteRow struct {
	Slug, Name, URL, Visibility string
	Listed, Published, Mine     bool
}

// AboutData supplies service identity to the about screen.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// Tool supplies a tool's name and description to the tools page.
type Tool struct{ Name, Description string }

// ToolsData supplies the banner and ordered tools to the tools page.
type ToolsData struct {
	Banner page.Banner
	Tools  []Tool
}

// NoticeData supplies the footer of a notice page.
type NoticeData struct{ Banner page.Banner }

// Config provides page rendering and catalog dependencies.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Store        *store.Store
	Tools        []Tool
}

// Handler serves the landing, about and tools pages to signed-in callers.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		caller, _ := identity.FromContext(r.Context())
		if caller.UserID == "" {
			w.Header().Set("Location", urls.SignIn(r))
			w.WriteHeader(http.StatusFound)
			return
		}
		var rows []SiteRow
		var base string
		if r.URL.Path == "/" {
			xs, err := cfg.Store.Visible(r.Context(), caller.UserID)
			if err != nil {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				if r.Method != http.MethodHead {
					_, _ = io.WriteString(w, store.Unreachable+"\n")
				}
				return
			}
			base = urls.SitesURL(r, cfg.ServicesPath)
			rows = make([]SiteRow, 0, len(xs))
			for _, x := range xs {
				rows = append(rows, SiteRow{Slug: x.Slug, Name: x.Name, URL: urls.SiteURL(base, x.Slug), Visibility: x.Visibility, Listed: x.Listed, Published: x.Commit != "", Mine: x.Owner == caller.UserID})
			}
		}
		banner := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: urls.AuthProfile(r, cfg.ServicesPath), LogoutURL: urls.AuthLogout(r, cfg.ServicesPath)})
		if r.URL.Path == "/about" {
			banner.Trail = []page.Level{{Name: "about", URL: "/about"}}
			cfg.Pages.Write(w, r, http.StatusOK, "about", AboutData{Banner: banner, Description: Description})
			return
		}
		if r.URL.Path == "/tools" {
			banner.Trail = []page.Level{{Name: "tools", URL: "/tools"}}
			cfg.Pages.Write(w, r, http.StatusOK, "tools", ToolsData{Banner: banner, Tools: cfg.Tools})
			return
		}
		banner.Trail = nil
		cfg.Pages.Write(w, r, http.StatusOK, "landing", LandingData{Banner: banner, SitesURL: base, Sites: rows})
	})
}
