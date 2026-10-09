// Package pages serves home's signed-in pages and shared assets.
package pages

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/home"
)

// ServiceName identifies home in the suite.
const ServiceName = "home"

// Description is home's one-line description.
const Description string = "Every service on this space"

// LandingData supplies the landing template.
type LandingData struct {
	Banner   page.Banner
	Services []page.Service
}

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
		status, name := http.StatusNotFound, "notfound"
		if r.URL.Path == "/" || r.URL.Path == "/about" {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			status, name = http.StatusOK, "landing"
			if r.URL.Path == "/about" {
				name = "about"
			}
		}
		b := cfg.Banner(bannerUser(r, cfg.ServicesPath))
		b.Trail = nil
		var data any = NoticeData{Banner: b}
		switch name {
		case "landing":
			data = LandingData{Banner: b, Services: b.Services}
		case "about":
			b.Trail = []page.Level{{Name: "about", URL: "/about"}}
			data = AboutData{Banner: b, Description: Description}
		}
		var body bytes.Buffer
		if err := templates.ExecuteTemplate(&body, name, data); err != nil {
			panic(err)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body.Bytes())
		}
	})
	return telemetry.Middleware(cfg.Telemetry, identity.Require(h))
}

func bannerUser(r *http.Request, path string) page.User {
	base := ""
	if list, err := services.Read(path); err == nil {
		if auth, ok := list.Find("auth"); ok {
			base = auth.URL
		}
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
