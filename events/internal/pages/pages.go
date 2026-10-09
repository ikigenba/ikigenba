// Package pages renders events' HTML screens from the embedded templates.
package pages

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/store"
)

// Description is the bus description shared with its manifest.
const Description = "The suite's internal event bus"

// Config supplies the store and page identity.
type Config struct {
	Banner       func(u page.User) page.Banner
	ServicesPath string
	Store        *store.Store
}

// Pages holds the immutable templates and page configuration.
type Pages struct {
	cfg       Config
	templates *template.Template
}

// LandingData supplies the landing template.
type LandingData struct {
	Banner      page.Banner
	Subscribers []SubscriberRow
}

// SubscriberRow holds one subscriber’s display values.
type SubscriberRow struct {
	Service, Status, Cursor, Lag string
	Since                        time.Time
	Reason                       *Reason
}

// Reason holds the stuck event's values for the landing template.
type Reason struct {
	Name  string
	Seq   int64
	Error string
}

// AboutData supplies the about template.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// NoticeData supplies the footer of notice templates.
type NoticeData struct{ Banner page.Banner }

// New parses the embedded templates and constructs the screens.
func New(cfg Config) *Pages {
	return &Pages{cfg: cfg, templates: template.Must(page.Templates().ParseFS(events.Assets(), "*.html"))}
}

func (p *Pages) notice() page.Banner {
	return p.cfg.Banner(page.User{})
}
func (p *Pages) banner(r *http.Request) page.Banner {
	u := page.User{Email: r.Header.Get("X-User-Email")}
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme != "http" && scheme != "https" {
		scheme = "https"
	}
	space := r.Host
	if i := strings.LastIndexByte(space, ':'); i >= 0 {
		digits := true
		for _, c := range space[i+1:] {
			if c < '0' || c > '9' {
				digits = false
				break
			}
		}
		if digits {
			space = space[:i]
		}
	}
	if len(space) > 7 && strings.EqualFold(space[:7], "events.") {
		space = space[7:]
	}
	auth := scheme + "://auth." + space
	if list, err := services.Read(p.cfg.ServicesPath); err == nil {
		if entry, ok := list.Find("auth"); ok && entry.URL != "" {
			auth = entry.URL
		}
	}
	u.ProfileURL = auth + "/"
	u.LogoutURL = auth + "/logout"
	return p.cfg.Banner(u)
}
func row(s store.Subscriber) SubscriberRow {
	r := SubscriberRow{Service: s.Service, Status: string(s.Status), Cursor: strconv.FormatInt(s.Cursor, 10), Lag: strconv.FormatInt(s.Lag, 10), Since: s.Since}
	if s.Reason != nil {
		r.Reason = &Reason{Name: s.Reason.Name, Seq: s.Reason.Seq, Error: s.Reason.Error}
	}
	return r
}
func (p *Pages) answer(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_ = p.templates.ExecuteTemplate(w, name, data)
	}
}

// Landing renders the subscribers or an unavailable notice.
func (p *Pages) Landing(w http.ResponseWriter, r *http.Request) {
	subs, err := p.cfg.Store.Subscribers(r.Context())
	if err != nil {
		p.answer(w, r, http.StatusServiceUnavailable, "unavailable", NoticeData{Banner: p.notice()})
		return
	}
	data := LandingData{Banner: p.banner(r)}
	for _, s := range subs {
		data.Subscribers = append(data.Subscribers, row(s))
	}
	p.answer(w, r, http.StatusOK, "landing", data)
}

// About renders the service identity.
func (p *Pages) About(w http.ResponseWriter, r *http.Request) {
	p.answer(w, r, http.StatusOK, "about", AboutData{Banner: p.banner(r), Description: Description})
}

// NotFound renders the missing-address notice.
func (p *Pages) NotFound(w http.ResponseWriter, r *http.Request) {
	p.answer(w, r, http.StatusNotFound, "notfound", NoticeData{Banner: p.notice()})
}
