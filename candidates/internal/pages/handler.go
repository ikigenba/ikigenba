package pages

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates/internal/scoring"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/urls"
)

// Handler builds the service HTTP handler.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.EscapedPath()
		cid := strings.TrimPrefix(path, "/candidates/")
		pid := strings.TrimPrefix(path, "/passes/")
		candidate := strings.HasPrefix(path, "/candidates/") && store.ValidCandidateID(cid)
		pass := strings.HasPrefix(path, "/passes/") && store.ValidPassID(pid)
		known := path == "/" || path == "/tools" || path == "/about" || candidate || pass
		auth := urls.Auth(r, cfg.ServicesPath)
		b := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: auth + "/", LogoutURL: auth + "/logout"})
		notfound := func() { cfg.Pages.Write(w, r, http.StatusNotFound, "notfound", NotFoundData{Banner: b}) }
		if !known {
			notfound()
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fail := func() {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte(store.Unreachable + "\n"))
			}
		}
		switch {
		case path == "/tools":
			b.Trail = []page.Level{ToolsLevel}
			d := ToolsData{Banner: b, Tools: []ToolRow{}}
			for _, e := range tools.Catalog() {
				d.Tools = append(d.Tools, ToolRow{e.Name, e.Description})
			}
			cfg.Pages.Write(w, r, 200, "tools", d)
		case path == "/about":
			b.Trail = []page.Level{AboutLevel}
			cfg.Pages.Write(w, r, 200, "about", AboutData{b, Description})
		case path == "/":
			b.Trail = nil
			d := LandingData{Banner: b, MaxMinutes: scoring.DefaultMaxMinutes, MinRevenue: dollars(scoring.DefaultMinRevenue), Shortlist: []ShortlistRow{}, Passes: []PassRow{}}
			ranked, err := cfg.Store.Shortlist(r.Context(), store.ShortlistFilter{MaxMinutes: scoring.DefaultMaxMinutes, MinRevenue: scoring.DefaultMinRevenue}, 0, ShortlistShown)
			if err != nil {
				fail()
				return
			}
			for i, e := range ranked {
				d.Shortlist = append(d.Shortlist, shortlistRow(e, i))
			}
			for offset := int64(0); ; offset += 100 {
				list, err := cfg.Store.ListPasses(r.Context(), offset, 100)
				if err != nil {
					fail()
					return
				}
				for _, p := range list {
					d.Passes = append(d.Passes, passRow(p))
				}
				if len(list) < 100 {
					break
				}
			}
			cfg.Pages.Write(w, r, 200, "landing", d)
		case pass:
			p, err := cfg.Store.GetPass(r.Context(), pid)
			if errors.Is(err, store.ErrNotFound) {
				notfound()
				return
			}
			if err != nil {
				fail()
				return
			}
			b.Trail = []page.Level{{Name: pid, URL: "/passes/" + pid}}
			cfg.Pages.Write(w, r, 200, "pass", PassData{b, passView(p, cfg.ServicesPath)})
		case candidate:
			c, err := cfg.Store.GetCandidate(r.Context(), cid)
			if errors.Is(err, store.ErrNotFound) {
				notfound()
				return
			}
			if err != nil {
				fail()
				return
			}
			d := CandidateData{Candidate: candidateView(c), Estimates: []EstimateView{}, Links: []LinkView{}, Rulings: []RulingView{}}
			rulings, err := cfg.Store.Rulings(r.Context(), cid)
			if err != nil {
				fail()
				return
			}
			for _, v := range rulings {
				d.Rulings = append(d.Rulings, rulingView(v, cid))
			}
			if c.Status != store.StatusMerged {
				now := cfg.Now()
				a, err := cfg.Store.GetAssessment(r.Context(), cid)
				if err == nil {
					v := assessmentView(a, now)
					d.Assessment = &v
				} else if !errors.Is(err, store.ErrNotFound) {
					fail()
					return
				}
				estimates, err := cfg.Store.Estimates(r.Context(), cid)
				if err != nil {
					fail()
					return
				}
				for _, v := range estimates {
					d.Estimates = append(d.Estimates, estimateView(v))
				}
				links, err := cfg.Store.Links(r.Context(), cid)
				if err != nil {
					fail()
					return
				}
				for _, v := range links {
					d.Links = append(d.Links, linkView(v, cfg.ServicesPath))
				}
			}
			b.Trail = []page.Level{{Name: cid, URL: "/candidates/" + cid}}
			d.Banner = b
			cfg.Pages.Write(w, r, 200, "candidate", d)
		}
	})
}
