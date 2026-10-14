package pages

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
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
			sp, pp := r.URL.Query().Get("shortlist_page"), r.URL.Query().Get("passes_page")
			var key int64
			var id, started, passID string
			if sp != "" {
				if err := (paging.Token{Tool: "shortlist"}).Decode(sp, &key, &id); err != nil {
					notfound()
					return
				}
			}
			if pp != "" {
				if err := (paging.Token{Tool: "passes"}).Decode(pp, &started, &passID); err != nil {
					notfound()
					return
				}
			}
			d := LandingData{Banner: b, MaxMinutes: scoring.DefaultMaxMinutes, MinRevenue: dollars(scoring.DefaultMinRevenue), Shortlist: []ShortlistRow{}, ShortlistPage: sp, Passes: []PassRow{}, PassesPage: pp}
			filter := store.ShortlistFilter{MaxMinutes: scoring.DefaultMaxMinutes, MinRevenue: scoring.DefaultMinRevenue}
			ranked, err := cfg.Store.Shortlist(r.Context(), filter, key, id, paging.DefaultLimit+1)
			if err != nil {
				fail()
				return
			}
			var offset int64
			if id != "" {
				offset, err = cfg.Store.ShortlistOffset(r.Context(), filter, key, id)
				if err != nil {
					fail()
					return
				}
			}
			if len(ranked) > paging.DefaultLimit {
				ranked = ranked[:paging.DefaultLimit]
				last := ranked[len(ranked)-1]
				d.ShortlistNext = (paging.Token{Tool: "shortlist"}).Encode(last.Assessment.ScoreKey, last.Candidate.ID)
			}
			for i, e := range ranked {
				d.Shortlist = append(d.Shortlist, shortlistRow(e, i, offset))
			}
			list, err := cfg.Store.ListPasses(r.Context(), started, passID, paging.DefaultLimit+1)
			if err != nil {
				fail()
				return
			}
			if len(list) > paging.DefaultLimit {
				list = list[:paging.DefaultLimit]
				last := list[len(list)-1]
				d.PassesNext = (paging.Token{Tool: "passes"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID)
			}
			for _, p := range list {
				d.Passes = append(d.Passes, passRow(p))
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
				a, err := cfg.Store.GetAssessment(r.Context(), cid)
				if err == nil {
					v := assessmentView(a)
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
