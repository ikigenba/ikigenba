// Package pages renders the embedded page templates.
package pages

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/gdrive"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
	"github.com/ikigenba/ikigenba/gdrive/internal/tools"
	"github.com/ikigenba/ikigenba/gdrive/internal/urls"
)

// ServiceName names the service in the shared page chrome.
const ServiceName = "gdrive"

// Description is the deployment and about-page description.
const Description = "Google Drive files, pulled into and pushed from artifacts"

// MinuteLayout formats the landing page time values.
const MinuteLayout = "2006-01-02 15:04"

// SecondLayout formats the transfer page time values.
const SecondLayout = "2006-01-02 15:04:05 UTC"

// ToolsLevel is the tools page breadcrumb.
var ToolsLevel = page.Level{Name: "tools", URL: "/tools"}

// AboutLevel is the about page breadcrumb.
var AboutLevel = page.Level{Name: "about", URL: "/about"}

// Set holds the parsed embedded page templates.
type Set struct{ templates *template.Template }

// Load parses the service templates with the shared page chrome.
func Load() (*Set, error) {
	ts, e := page.Templates().ParseFS(gdrive.Assets(), "*.html")
	if e != nil {
		return nil, e
	}
	return &Set{ts}, nil
}

// Write renders the named template with the supplied status and data.
func (s *Set) Write(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	var b bytes.Buffer
	if e := s.templates.ExecuteTemplate(&b, name, data); e != nil {
		panic(e)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b.Bytes())
	}
}

// Config supplies the dependencies used by the handler.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Store        *store.Store
}

// LandingData is the landing template input.
type LandingData struct {
	Banner    page.Banner
	Transfers []TransferRow
	Next      string
}

// TransferRow contains the public fields shown in a landing row.
type TransferRow struct {
	ID, Direction, Status, Reason, Name, Link, Path, ArtifactPage, URL string
	Bytes                                                              int64
	Started, StartedText, Finished, FinishedText                       string
}

// TransferData is the transfer template input.
type TransferData struct {
	Banner   page.Banner
	Transfer TransferView
}

// TransferView contains the public fields shown on a transfer page.
type TransferView struct {
	ID, Direction, Status, Reason, File, Name, Kind, Path, ArtifactPage, ContentType, Format string
	Bytes                                                                                    int64
	Started, StartedText, Finished, FinishedText, URL, Link                                  string
}

// ToolsData is the tools template input.
type ToolsData struct {
	Banner page.Banner
	Tools  []ToolRow
}

// ToolRow contains a catalog entry shown on the tools page.
type ToolRow struct{ Name, Description string }

// AboutData is the about template input.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// NotFoundData is the notfound template input.
type NotFoundData struct{ Banner page.Banner }

func artifact(t store.Transfer, p string) string {
	if p == "" || t.ArtifactID == "" {
		return ""
	}
	es, e := services.Read(p)
	if e != nil {
		return ""
	}
	for _, s := range es {
		if s.Name == "artifacts" && s.URL != "" {
			return urls.ArtifactPage(urls.Base(s.URL), t.ArtifactID)
		}
	}
	return ""
}
func times(t store.Transfer, layout string) (string, string, string, string) {
	a, b := t.Started.UTC().Format(time.RFC3339), t.Started.UTC().Format(layout)
	if t.Finished.IsZero() {
		return a, b, "", ""
	}
	return a, b, t.Finished.UTC().Format(time.RFC3339), t.Finished.UTC().Format(layout)
}
func row(t store.Transfer, p string) TransferRow {
	a, b, c, d := times(t, MinuteLayout)
	return TransferRow{t.ID, t.Direction, t.Status, t.Reason, t.Name, t.Link, t.Path, artifact(t, p), t.URL, t.Bytes, a, b, c, d}
}
func view(t store.Transfer, p string) TransferView {
	a, b, c, d := times(t, SecondLayout)
	return TransferView{t.ID, t.Direction, t.Status, t.Reason, t.File, t.Name, t.Kind, t.Path, artifact(t, p), t.ContentType, t.Format, t.Bytes, a, b, c, d, t.URL, t.Link}
}

// Handler builds the HTTP handler from its dependencies.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := identity.FromContext(r.Context())
		auth := urls.Auth(r, cfg.ServicesPath)
		b := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: auth + "/", LogoutURL: auth + "/logout"})
		p := r.URL.EscapedPath()
		id := strings.TrimPrefix(p, "/")
		isTransfer := strings.HasPrefix(p, "/") && store.ValidID(id)
		known := p == "/" || p == "/tools" || p == "/about" || isTransfer
		if known && r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		notFound := func() { cfg.Pages.Write(w, r, 404, "notfound", NotFoundData{b}) }
		unavailable := func() {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(503)
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte(store.Unreachable + "\n"))
			}
		}
		switch {
		case p == "/":
			ts, e := cfg.Store.ListAfter(r.Context(), c.UserID, time.Time{}, "", paging.DefaultLimit+1)
			if e != nil {
				unavailable()
				return
			}
			next := ""
			if len(ts) > paging.DefaultLimit {
				ts = ts[:paging.DefaultLimit]
				last := ts[len(ts)-1]
				next = (paging.Token{Tool: "transfers"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID)
			}
			rs := make([]TransferRow, 0, len(ts))
			for _, t := range ts {
				rs = append(rs, row(t, cfg.ServicesPath))
			}
			b.Trail = nil
			cfg.Pages.Write(w, r, 200, "landing", LandingData{b, rs, next})
		case p == "/tools":
			rs := make([]ToolRow, 0, 8)
			for _, t := range tools.Catalog() {
				rs = append(rs, ToolRow{t.Name, t.Description})
			}
			b.Trail = []page.Level{ToolsLevel}
			cfg.Pages.Write(w, r, 200, "tools", ToolsData{b, rs})
		case p == "/about":
			b.Trail = []page.Level{AboutLevel}
			cfg.Pages.Write(w, r, 200, "about", AboutData{b, Description})
		case isTransfer:
			t, e := cfg.Store.Get(r.Context(), c.UserID, id)
			if errors.Is(e, store.ErrNotFound) {
				notFound()
				return
			}
			if e != nil {
				unavailable()
				return
			}
			b.Trail = []page.Level{{Name: id, URL: "/" + id}}
			cfg.Pages.Write(w, r, 200, "transfer", TransferData{b, view(t, cfg.ServicesPath)})
		default:
			notFound()
		}
	})
}
