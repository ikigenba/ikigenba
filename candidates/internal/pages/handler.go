package pages

import (
	"errors"
	"net/http"
	"slices"
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
			shortPos, ok := decodeShortlistPage(sp)
			if !ok {
				notfound()
				return
			}
			passPos, ok := decodePassesPage(pp)
			if !ok {
				notfound()
				return
			}
			d := LandingData{Banner: b, MaxMinutes: scoring.DefaultMaxMinutes, MinRevenue: dollars(scoring.DefaultMinRevenue), Shortlist: []ShortlistRow{}, ShortlistPage: sp, Passes: []PassRow{}, PassesPage: pp}
			filter := store.ShortlistFilter{MaxMinutes: scoring.DefaultMaxMinutes, MinRevenue: scoring.DefaultMinRevenue}
			var ranked []store.Ranked
			var err error
			if shortPos.reversed {
				ranked, err = cfg.Store.ShortlistReversed(r.Context(), filter, shortPos.key, shortPos.id, paging.DefaultLimit+1)
			} else {
				ranked, err = cfg.Store.Shortlist(r.Context(), filter, shortPos.key, shortPos.id, paging.DefaultLimit+1)
			}
			if err != nil {
				fail()
				return
			}
			shortMore := len(ranked) > paging.DefaultLimit
			shortUnder := shortPos.reversed && !shortMore
			if shortMore {
				ranked = ranked[:paging.DefaultLimit]
			}
			if shortPos.reversed {
				slices.Reverse(ranked)
			}
			var offset int64
			if len(ranked) > 0 && sp != "" {
				key, id := shortPos.key, shortPos.id
				if shortPos.reversed {
					key, id = ranked[0].Assessment.ScoreKey, ranked[0].Candidate.ID
				}
				offset, err = cfg.Store.ShortlistOffset(r.Context(), filter, key, id)
				if err != nil {
					fail()
					return
				}
				if shortPos.reversed {
					offset--
				}
			}
			var list []store.Pass
			if passPos.reversed {
				list, err = cfg.Store.ListPassesReversed(r.Context(), passPos.started, passPos.id, paging.DefaultLimit+1)
			} else {
				list, err = cfg.Store.ListPasses(r.Context(), passPos.started, passPos.id, paging.DefaultLimit+1)
			}
			if err != nil {
				fail()
				return
			}
			passMore := len(list) > paging.DefaultLimit
			passUnder := passPos.reversed && !passMore
			if shortUnder || passUnder {
				loc := "/"
				if !shortUnder && sp != "" {
					loc += "?shortlist_page=" + sp
				} else if !passUnder && pp != "" {
					loc += "?passes_page=" + pp
				}
				w.Header().Set("Location", loc)
				w.WriteHeader(http.StatusSeeOther)
				return
			}
			if passMore {
				list = list[:paging.DefaultLimit]
			}
			if passPos.reversed {
				slices.Reverse(list)
			}
			if len(ranked) > 0 {
				first, last := ranked[0], ranked[len(ranked)-1]
				if sp != "" {
					d.ShortlistPrev = (paging.Token{Tool: "shortlist-before"}).Encode(first.Assessment.ScoreKey, first.Candidate.ID)
				}
				if !shortPos.last && (shortMore || shortPos.reversed) {
					d.ShortlistNext = (paging.Token{Tool: "shortlist"}).Encode(last.Assessment.ScoreKey, last.Candidate.ID)
				}
			}
			for i, e := range ranked {
				d.Shortlist = append(d.Shortlist, shortlistRow(e, i, offset))
			}
			if len(list) > 0 {
				first, last := list[0], list[len(list)-1]
				if pp != "" {
					d.PassesPrev = (paging.Token{Tool: "passes-before"}).Encode(first.Started.UTC().Format(time.RFC3339), first.ID)
				}
				if !passPos.last && (passMore || passPos.reversed) {
					d.PassesNext = (paging.Token{Tool: "passes"}).Encode(last.Started.UTC().Format(time.RFC3339), last.ID)
				}
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

// landingPosition identifies a window in either landing list.
type landingPosition struct {
	key            int64
	started, id    string
	reversed, last bool
}

func decodeShortlistPage(value string) (landingPosition, bool) {
	p := landingPosition{}
	if value == "" {
		return p, true
	}
	if (paging.Token{Tool: "shortlist"}).Decode(value, &p.key, &p.id) == nil {
		return p, true
	}
	if (paging.Token{Tool: "shortlist-before"}).Decode(value, &p.key, &p.id) == nil {
		p.reversed = true
		return p, true
	}
	if value == "last" {
		return landingPosition{reversed: true, last: true}, true
	}
	return p, false
}

func decodePassesPage(value string) (landingPosition, bool) {
	p := landingPosition{}
	if value == "" {
		return p, true
	}
	if (paging.Token{Tool: "passes"}).Decode(value, &p.started, &p.id) == nil {
		return p, true
	}
	if (paging.Token{Tool: "passes-before"}).Decode(value, &p.started, &p.id) == nil {
		p.reversed = true
		return p, true
	}
	if value == "last" {
		return landingPosition{reversed: true, last: true}, true
	}
	return p, false
}
