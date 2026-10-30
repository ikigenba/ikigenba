// Package pages serves home's signed-in pages and shared assets.
package pages

import (
	"bytes"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/home"
	"github.com/ikigenba/ikigenba/home/internal/bookmarks"
	"github.com/ikigenba/ikigenba/home/internal/favicon"
)

// ServiceName identifies home in the suite.
const ServiceName = "home"

// Description is home's one-line description.
const Description string = "Every service on this space"

// Tile carries one listed service to the landing template.
type Tile struct {
	Name, URL string
	Icon      template.HTML
	Enabled   bool
}

// LandingData supplies the landing template.
type LandingData struct {
	Banner        page.Banner
	Bookmarks     []BookmarkTile
	Backend, Core []Tile
}

// BookmarkTile carries a stored link to the landing template.
type BookmarkTile struct {
	ID, Name, URL string
	HasIcon       bool
}

// BookmarkRow carries a stored link and its position in the caller's list.
type BookmarkRow struct {
	ID, Name, URL        string
	HasIcon, First, Last bool
}

// BookmarksData supplies the bookmarks template.
type BookmarksData struct {
	Banner     page.Banner
	Bookmarks  []BookmarkRow
	Submission bookmarks.Submission
	Errors     bookmarks.FieldErrors
	NoIcon     bool
}

// IconCacheControl keeps stored icons private and cacheable for an hour.
const IconCacheControl = "private, max-age=3600"

// IconContentSecurityPolicy confines documents opened at an icon URL.
const IconContentSecurityPolicy = "default-src 'none'; style-src 'unsafe-inline'; sandbox"

// IconContentTypeOptions prevents sniffing of stored icons.
const IconContentTypeOptions = "nosniff"

// AboutData supplies the about template.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// NoticeData supplies the notfound template.
type NoticeData struct{ Banner page.Banner }

// Config supplies the page's banner, services file and request events.
type Config struct {
	Banner       func(u page.User) page.Banner
	ServicesPath string
	Telemetry    *telemetry.Writer
	Store        *bookmarks.Store
	Favicon      *favicon.Fetcher
}

// Handler returns home's handler, including identity and request telemetry.
func Handler(cfg Config) http.Handler {
	templates := template.Must(page.Templates().ParseFS(home.Assets(), "*.html"))
	static := page.Static()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, page.StaticPrefix) {
			static.ServeHTTP(w, r)
			return
		}
		id, action := bookmarkRoute(r.URL.Path)
		if r.URL.Path == "/bookmarks" {
			if r.Method == http.MethodPost {
				addBookmark(w, r, cfg, templates)
				return
			}
			if refuse(w, r, "GET, HEAD, POST", http.MethodGet, http.MethodHead) {
				return
			}
			showBookmarks(w, r, cfg, templates, http.StatusOK, bookmarks.Submission{}, bookmarks.FieldErrors{}, r.URL.Query().Get("icon") == "none")
			return
		}
		if action == "delete" {
			if refuse(w, r, "POST", http.MethodPost) {
				return
			}
			removed, err := cfg.Store.Delete(r.Context(), r.Header.Get("X-User-Id"), id)
			if err != nil {
				unreachable(w, r)
				return
			}
			if removed {
				cfg.Telemetry.Emit(r.Context(), "bookmark.deleted", telemetry.Attrs{"bookmark": id})
			}
			redirect(w, "/bookmarks")
			return
		}
		if action == "move" {
			if refuse(w, r, "POST", http.MethodPost) {
				return
			}
			values := formBody(r)
			moved, changed, err := cfg.Store.Move(r.Context(), r.Header.Get("X-User-Id"), id, bookmarks.Direction(values.Get("to")))
			if err != nil {
				unreachable(w, r)
				return
			}
			if changed {
				cfg.Telemetry.Emit(r.Context(), "bookmark.moved", telemetry.Attrs{"bookmark": id, "from": moved.From, "to": moved.To})
			}
			redirect(w, "/bookmarks")
			return
		}
		if action == "icon" {
			if refuse(w, r, "GET, HEAD", http.MethodGet, http.MethodHead) {
				return
			}
			icon, err := cfg.Store.Icon(r.Context(), r.Header.Get("X-User-Id"), id)
			if err == nil {
				w.Header().Set("Content-Type", icon.Type)
				w.Header().Set("Cache-Control", IconCacheControl)
				w.Header().Set("Content-Security-Policy", IconContentSecurityPolicy)
				w.Header().Set("X-Content-Type-Options", IconContentTypeOptions)
				w.WriteHeader(http.StatusOK)
				if r.Method != http.MethodHead {
					_, _ = w.Write(icon.Data)
				}
				return
			}
			if !errors.Is(err, bookmarks.ErrNotFound) {
				unreachable(w, r)
				return
			}
		}
		status, name := http.StatusNotFound, "notfound"
		if r.URL.Path == "/" || r.URL.Path == "/about" {
			if refuse(w, r, "GET, HEAD", http.MethodGet, http.MethodHead) {
				return
			}
			status, name = http.StatusOK, "landing"
			if r.URL.Path == "/about" {
				name = "about"
			}
		}
		var held []bookmarks.Bookmark
		if name == "landing" {
			var err error
			held, err = cfg.Store.List(r.Context(), r.Header.Get("X-User-Id"))
			if err != nil {
				unreachable(w, r)
				return
			}
		}
		list, _ := services.Read(cfg.ServicesPath)
		b := cfg.Banner(bannerUser(r, list))
		b.Trail = nil
		var data any = NoticeData{Banner: b}
		switch name {
		case "landing":
			landing := LandingData{Banner: b}
			for _, m := range held {
				landing.Bookmarks = append(landing.Bookmarks, BookmarkTile{m.ID, m.Name, m.URL, m.HasIcon})
			}
			for _, e := range list {
				if !e.HasIcon || e.Name == ServiceName {
					continue
				}
				tile := Tile{Name: e.Name, URL: e.URL, Icon: e.Icon, Enabled: e.Enabled}
				if e.Group == "core" {
					landing.Core = append(landing.Core, tile)
				} else {
					landing.Backend = append(landing.Backend, tile)
				}
			}
			data = landing
		case "about":
			b.Trail = []page.Level{{Name: "about", URL: "/about"}}
			data = AboutData{Banner: b, Description: Description}
		}
		render(w, r, templates, status, name, data)
	})
	return telemetry.Middleware(cfg.Telemetry, identity.Require(h))
}

func bannerUser(r *http.Request, list services.List) page.User {
	base := ""
	if auth, ok := list.Find("auth"); ok {
		base = auth.URL
	}
	if base == "" {
		scheme := r.Header.Get("X-Forwarded-Proto")
		if scheme != "http" && scheme != "https" {
			scheme = "https"
		}
		space := r.Host
		if colon := strings.LastIndexByte(space, ':'); colon >= 0 && digits(space[colon+1:]) {
			space = space[:colon]
		}
		if strings.HasPrefix(space, "home.") && len(space) > len("home.") {
			space = strings.TrimPrefix(space, "home.")
		}
		base = scheme + "://auth." + space
	}
	return page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: base + "/", LogoutURL: base + "/logout"}
}

func digits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func bookmarkRoute(path string) (string, string) {
	parts := strings.Split(path, "/")
	if len(parts) == 4 && parts[0] == "" && parts[1] == "bookmarks" && parts[2] != "" && (parts[3] == "icon" || parts[3] == "delete" || parts[3] == "move") {
		return parts[2], parts[3]
	}
	return "", ""
}

func refuse(w http.ResponseWriter, r *http.Request, allow string, methods ...string) bool {
	for _, method := range methods {
		if r.Method == method {
			return false
		}
	}
	w.Header().Set("Allow", allow)
	w.WriteHeader(http.StatusMethodNotAllowed)
	return true
}

func render(w http.ResponseWriter, r *http.Request, templates *template.Template, status int, name string, data any) {
	var body bytes.Buffer
	if err := templates.ExecuteTemplate(&body, name, data); err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}

func unreachable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, bookmarks.Unreachable+"\n")
	}
}

func redirect(w http.ResponseWriter, location string) {
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusSeeOther)
}

func showBookmarks(w http.ResponseWriter, r *http.Request, cfg Config, templates *template.Template, status int, s bookmarks.Submission, fieldErrors bookmarks.FieldErrors, noIcon bool) {
	held, err := cfg.Store.List(r.Context(), r.Header.Get("X-User-Id"))
	if err != nil {
		unreachable(w, r)
		return
	}
	list, _ := services.Read(cfg.ServicesPath)
	b := cfg.Banner(bannerUser(r, list))
	b.Trail = []page.Level{{Name: "bookmarks", URL: "/bookmarks"}}
	data := BookmarksData{Banner: b, Submission: s, Errors: fieldErrors, NoIcon: noIcon}
	for i, k := range held {
		data.Bookmarks = append(data.Bookmarks, BookmarkRow{k.ID, k.Name, k.URL, k.HasIcon, i == 0, i == len(held)-1})
	}
	render(w, r, templates, status, "bookmarks", data)
}

func addBookmark(w http.ResponseWriter, r *http.Request, cfg Config, templates *template.Template) {
	mediaType, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";")
	if !strings.EqualFold(strings.TrimSpace(mediaType), "application/x-www-form-urlencoded") {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	values := formBody(r)
	s := bookmarks.Submission{Name: values.Get("name"), URL: values.Get("url")}
	fieldErrors := bookmarks.Validate(s)
	if fieldErrors.Any() {
		showBookmarks(w, r, cfg, templates, http.StatusUnprocessableEntity, s, fieldErrors, false)
		return
	}
	if _, err := cfg.Store.List(r.Context(), r.Header.Get("X-User-Id")); err != nil {
		unreachable(w, r)
		return
	}
	data, typ := cfg.Favicon.Fetch(r.Context(), strings.TrimSpace(s.URL))
	k, err := cfg.Store.Create(r.Context(), bookmarks.Draft{Name: s.Name, URL: s.URL, CreatedBy: r.Header.Get("X-User-Id"), Icon: bookmarks.Icon{Type: typ, Data: data}})
	if errors.Is(err, bookmarks.ErrNameTaken) {
		showBookmarks(w, r, cfg, templates, http.StatusUnprocessableEntity, s, bookmarks.FieldErrors{Name: bookmarks.NameTakenMessage}, false)
		return
	}
	if err != nil {
		unreachable(w, r)
		return
	}
	cfg.Telemetry.Emit(r.Context(), "bookmark.created", telemetry.Attrs{"bookmark": k.ID, "name": k.Name, "icon": k.HasIcon, "icon_bytes": len(data)})
	location := "/bookmarks"
	if !k.HasIcon {
		location += "?icon=none"
	}
	redirect(w, location)
}

func formBody(r *http.Request) url.Values {
	mediaType, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";")
	if !strings.EqualFold(strings.TrimSpace(mediaType), "application/x-www-form-urlencoded") {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 10485761))
	values, _ := url.ParseQuery(string(body))
	return values
}
