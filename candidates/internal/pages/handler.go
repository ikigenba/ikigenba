package pages

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
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
		hid := strings.TrimPrefix(path, "/cohorts/")
		cohort := strings.HasPrefix(path, "/cohorts/") && store.ValidCohortID(hid)
		candidate := strings.HasPrefix(path, "/candidates/") && store.ValidCandidateID(cid)
		pass := strings.HasPrefix(path, "/passes/") && store.ValidPassID(pid)
		known := path == "/" || path == "/tools" || path == "/about" || candidate || pass || cohort
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
			sp, pp := r.URL.Query().Get("cohorts_page"), r.URL.Query().Get("passes_page")
			cohortPos, ok := decodeCohortsPage(sp)
			if !ok {
				notfound()
				return
			}
			passPos, ok := decodePassesPage(pp)
			if !ok {
				notfound()
				return
			}
			d := LandingData{Banner: b, Cohorts: []CohortRow{}, CohortsPage: sp, Passes: []PassRow{}, PassesPage: pp}
			var cohorts []store.Cohort
			var err error
			if cohortPos.reversed {
				cohorts, err = cfg.Store.ListCohortsReversed(r.Context(), cohortPos.name, cohortPos.id, paging.DefaultLimit+1)
			} else {
				cohorts, err = cfg.Store.ListCohorts(r.Context(), cohortPos.name, cohortPos.id, paging.DefaultLimit+1)
			}
			if err != nil {
				fail()
				return
			}
			cohortMore := len(cohorts) > paging.DefaultLimit
			cohortUnder := cohortPos.reversed && !cohortMore
			if cohortMore {
				cohorts = cohorts[:paging.DefaultLimit]
			}
			if cohortPos.reversed {
				slices.Reverse(cohorts)
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
			if cohortUnder || passUnder {
				loc := "/"
				if !cohortUnder && sp != "" {
					loc += "?cohorts_page=" + sp
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
			if len(cohorts) > 0 {
				first, last := cohorts[0], cohorts[len(cohorts)-1]
				if sp != "" {
					d.CohortsPrev = (paging.Token{Tool: "cohorts-before"}).Encode(first.Name, first.ID)
				}
				if !cohortPos.last && (cohortMore || cohortPos.reversed) {
					d.CohortsNext = (paging.Token{Tool: "cohorts"}).Encode(last.Name, last.ID)
				}
			}
			for _, e := range cohorts {
				d.Cohorts = append(d.Cohorts, cohortRow(e))
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
		case cohort:
			v, err := cfg.Store.ReadCohort(r.Context(), hid)
			if errors.Is(err, store.ErrNotFound) {
				notfound()
				return
			}
			if err != nil {
				fail()
				return
			}
			b.Trail = []page.Level{{Name: hid, URL: "/cohorts/" + hid}}
			d := CohortData{Banner: b, Cohort: cohortView(v.Cohort), Matching: v.Matching, Members: []MemberRow{}, Rejects: []RejectRow{}}
			for i, m := range v.Members {
				d.Members = append(d.Members, memberRow(m, i))
			}
			for _, x := range v.Rejects {
				d.Rejects = append(d.Rejects, rejectRow(x))
			}
			cfg.Pages.Write(w, r, 200, "cohort", d)
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
			d := CandidateData{Candidate: candidateView(c), Estimates: []EstimateView{}, Links: []LinkView{}, Rulings: []RulingView{}, Rejects: []RejectRecordRow{}}
			rulings, err := cfg.Store.Rulings(r.Context(), cid)
			if err != nil {
				fail()
				return
			}
			for _, v := range rulings {
				d.Rulings = append(d.Rulings, rulingView(v, cid))
			}
			rejects, err := cfg.Store.Rejects(r.Context(), cid)
			if err != nil {
				fail()
				return
			}
			for _, x := range rejects {
				d.Rejects = append(d.Rejects, rejectRecordRow(x))
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
	name, started, id string
	reversed, last    bool
}

func decodeCohortsPage(value string) (landingPosition, bool) {
	p := landingPosition{}
	if value == "" {
		return p, true
	}
	if (paging.Token{Tool: "cohorts"}).Decode(value, &p.name, &p.id) == nil {
		return p, true
	}
	if (paging.Token{Tool: "cohorts-before"}).Decode(value, &p.name, &p.id) == nil {
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
