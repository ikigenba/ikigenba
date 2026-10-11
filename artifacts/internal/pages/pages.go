// Package pages renders the artifact service's read-only HTML surfaces.
package pages

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	artifacts "github.com/ikigenba/ikigenba/artifacts"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/tools"
	"github.com/ikigenba/ikigenba/artifacts/internal/urls"
)

// ServiceName identifies the service in its chrome and manifest.
const ServiceName = "artifacts"

// Description is the service's one-line summary.
const Description = "Binary files agents and services share"

// ToolsLevel names the catalog child page.
var ToolsLevel = page.Level{Name: "tools", URL: "/tools"}

// AboutLevel names the about child page.
var AboutLevel = page.Level{Name: "about", URL: "/about"}

// Set holds the service templates and shared chrome.
type Set struct{ templates *template.Template }

// Load parses the embedded service templates alongside appkit's chrome.
func Load() (*Set, error) {
	t, err := page.Templates().ParseFS(artifacts.Assets(), "*.html")
	if err != nil {
		return nil, err
	}
	return &Set{templates: t}, nil
}

// Write renders a named template, suppressing the body for HEAD.
func (s *Set) Write(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var body bytes.Buffer
	if err := s.templates.ExecuteTemplate(&body, name, data); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}

// Config supplies the page renderer, caller chrome and metadata store.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Store        *store.Store
}

// LandingData is the root page's data.
type LandingData struct {
	Banner     page.Banner
	Artifacts  []ArtifactRow
	TotalBytes int64
}

// ArtifactRow is one artifact in the root page.
type ArtifactRow struct {
	ID, Path                                                         string
	Bytes                                                            int64
	ContentType, Owner, Modified, ModifiedText, Expires, ExpiresText string
}

// ArtifactData is an artifact page's data.
type ArtifactData struct {
	Banner   page.Banner
	Artifact ArtifactView
}

// ArtifactView is the artifact's complete display metadata.
type ArtifactView struct {
	ID, Path, URL                                                                                  string
	Bytes                                                                                          int64
	ContentType, SHA256, Owner, Created, CreatedText, Modified, ModifiedText, Expires, ExpiresText string
}

// ToolsData is the catalog page's data.
type ToolsData struct {
	Banner page.Banner
	Tools  []ToolRow
}

// ToolRow is a catalog entry's name and first description line.
type ToolRow struct{ Name, Description string }

// AboutData is the about screen's data.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// NoticeData is the not-found screen's data.
type NoticeData struct{ Banner page.Banner }

func stamp(t time.Time, format string) (string, string) {
	if t.IsZero() {
		return "", ""
	}
	return t.UTC().Format(time.RFC3339), t.UTC().Format(format)
}
func unavailable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(store.Unreachable + "\n"))
	}
}

// Handler returns the read-only page routes; its caller enforces identity.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.EscapedPath()
		id := ""
		if len(path) > 1 {
			id = path[1:]
		}
		validID := store.ValidID(id)
		known := path == "/" || path == "/tools" || path == "/about" || validID
		if known && r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		auth := urls.Auth(r, cfg.ServicesPath)
		banner := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: auth + "/", LogoutURL: auth + "/logout"})
		switch path {
		case "/":
			all, err := cfg.Store.List(r.Context())
			if err != nil {
				unavailable(w, r)
				return
			}
			banner.Trail = nil
			data := LandingData{Banner: banner, Artifacts: make([]ArtifactRow, 0, len(all))}
			for _, a := range all {
				modified, mt := stamp(a.Modified, "2006-01-02 15:04")
				expires, et := stamp(a.Expires, "2006-01-02 15:04")
				data.Artifacts = append(data.Artifacts, ArtifactRow{ID: a.ID, Path: a.Path, Bytes: a.Bytes, ContentType: a.ContentType, Owner: a.OwnerEmail, Modified: modified, ModifiedText: mt, Expires: expires, ExpiresText: et})
				data.TotalBytes += a.Bytes
			}
			cfg.Pages.Write(w, r, http.StatusOK, "landing", data)
		case "/tools":
			banner.Trail = []page.Level{ToolsLevel}
			data := ToolsData{Banner: banner}
			for _, e := range tools.Catalog("") {
				data.Tools = append(data.Tools, ToolRow{Name: e.Name, Description: tools.FirstLine(e.Description)})
			}
			cfg.Pages.Write(w, r, http.StatusOK, "tools", data)
		case "/about":
			banner.Trail = []page.Level{AboutLevel}
			cfg.Pages.Write(w, r, http.StatusOK, "about", AboutData{Banner: banner, Description: Description})
		default:
			if validID {
				a, err := cfg.Store.GetID(r.Context(), id)
				if err == nil {
					banner.Trail = []page.Level{{Name: a.Path, URL: "/" + id}}
					created, ct := stamp(a.Created, "2006-01-02 15:04:05 UTC")
					modified, mt := stamp(a.Modified, "2006-01-02 15:04:05 UTC")
					expires, et := stamp(a.Expires, "2006-01-02 15:04:05 UTC")
					cfg.Pages.Write(w, r, http.StatusOK, "artifact", ArtifactData{Banner: banner, Artifact: ArtifactView{ID: a.ID, Path: a.Path, URL: urls.File(urls.Base(r, cfg.ServicesPath), a.Path), Bytes: a.Bytes, ContentType: a.ContentType, SHA256: a.SHA256, Owner: a.OwnerEmail, Created: created, CreatedText: ct, Modified: modified, ModifiedText: mt, Expires: expires, ExpiresText: et}})
					return
				}
				if !errors.Is(err, store.ErrNotFound) {
					unavailable(w, r)
					return
				}
			}
			cfg.Pages.Write(w, r, http.StatusNotFound, "notfound", NoticeData{Banner: banner})
		}
	})
}
