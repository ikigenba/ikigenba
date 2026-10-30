package pages

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
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
		pageID, number, textPage := decodePagePath(path)
		parkID := strings.TrimPrefix(path, "/parks/")
		park := strings.HasPrefix(path, "/parks/") && store.ValidParkID(parkID)
		pid := strings.TrimPrefix(path, "/passes/")
		hid := strings.TrimPrefix(path, "/prospect-lists/")
		legacyID := strings.TrimPrefix(path, "/cohorts/")
		legacy := strings.HasPrefix(path, "/cohorts/"+store.LegacyListIDPrefix) && store.ValidProspectListID(store.MapListID(legacyID))
		prospectList := strings.HasPrefix(path, "/prospect-lists/") && store.ValidProspectListID(hid)
		candidate := strings.HasPrefix(path, "/candidates/") && store.ValidCandidateID(cid)
		pass := strings.HasPrefix(path, "/passes/") && store.ValidPassID(pid)
		known := path == "/" || path == "/tools" || path == "/about" || path == "/parks" || park || candidate || pass || prospectList || legacy || textPage
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
		case path == "/parks":
			drawParks(w, r, cfg, b, notfound, fail)
		case park:
			drawPark(w, r, cfg, b, parkID, notfound, fail)
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
			sp, pp := r.URL.Query().Get("prospect_lists_page"), r.URL.Query().Get("passes_page")
			prospectListPos, ok := decodeProspectListsPage(sp)
			if !ok {
				notfound()
				return
			}
			passPos, ok := decodePassesPage(pp)
			if !ok {
				notfound()
				return
			}
			d := LandingData{Banner: b, ProspectLists: []ProspectListRow{}, ProspectListsPage: sp, Passes: []PassRow{}, PassesPage: pp}
			var prospectLists []store.ProspectList
			var err error
			if prospectListPos.reversed {
				prospectLists, err = cfg.Store.ListProspectListsReversed(r.Context(), prospectListPos.name, prospectListPos.id, paging.DefaultLimit+1)
			} else {
				prospectLists, err = cfg.Store.ListProspectLists(r.Context(), prospectListPos.name, prospectListPos.id, paging.DefaultLimit+1)
			}
			if err != nil {
				fail()
				return
			}
			prospectListMore := len(prospectLists) > paging.DefaultLimit
			prospectListUnder := prospectListPos.reversed && !prospectListMore
			if prospectListMore {
				prospectLists = prospectLists[:paging.DefaultLimit]
			}
			if prospectListPos.reversed {
				slices.Reverse(prospectLists)
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
			if prospectListUnder || passUnder {
				loc := "/"
				if !prospectListUnder && sp != "" {
					loc += "?prospect_lists_page=" + sp
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
			if len(prospectLists) > 0 {
				first, last := prospectLists[0], prospectLists[len(prospectLists)-1]
				if sp != "" {
					d.ProspectListsPrev = (paging.Token{Tool: "prospect_lists-before"}).Encode(first.Name, first.ID)
				}
				if !prospectListPos.last && (prospectListMore || prospectListPos.reversed) {
					d.ProspectListsNext = (paging.Token{Tool: "prospect_lists"}).Encode(last.Name, last.ID)
				}
			}
			for _, e := range prospectLists {
				d.ProspectLists = append(d.ProspectLists, prospectListRow(e))
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
			d.Parks.Count, _, err = cfg.Store.CountParks(r.Context())
			if err != nil {
				fail()
				return
			}
			cfg.Pages.Write(w, r, 200, "landing", d)
		case legacy:
			id := store.MapListID(legacyID)
			w.Header().Set("Location", "/prospect-lists/"+id)
			w.WriteHeader(http.StatusSeeOther)
		case prospectList:
			caller, _ := identity.FromContext(r.Context())
			v, err := cfg.Store.OpenProspectList(r.Context(), hid, caller.UserID, caller.Email)
			if errors.Is(err, store.ErrNotFound) {
				notfound()
				return
			}
			if err != nil {
				fail()
				return
			}
			b.Trail = []page.Level{{Name: hid, URL: "/prospect-lists/" + hid}}
			d := ProspectListData{Banner: b, ProspectList: prospectListView(v.ProspectList), Matching: v.Matching, Classified: v.Classified, ByBasis: basisCountsView(v.ByBasis), Changes: changesView(v.Changes), Members: []MemberRow{}, Rejects: []RejectRow{}}
			for i, m := range v.Members {
				d.Members = append(d.Members, memberRow(m, i))
			}
			for _, x := range v.Rejects {
				d.Rejects = append(d.Rejects, rejectRow(x))
			}
			cfg.Pages.Write(w, r, 200, "prospect_list", d)
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
			v := passView(p, cfg.ServicesPath)
			walks, err := cfg.Store.PassWalks(r.Context(), p.ID)
			if err != nil {
				fail()
				return
			}
			for _, x := range walks {
				v.Parks = append(v.Parks, walkParkRow(x))
			}
			cfg.Pages.Write(w, r, 200, "pass", PassData{b, v})
		case textPage:
			c, err := cfg.Store.GetCandidate(r.Context(), pageID)
			if errors.Is(err, store.ErrNotFound) {
				notfound()
				return
			}
			if err != nil {
				fail()
				return
			}
			p, err := cfg.Store.GetPage(r.Context(), pageID, number)
			if errors.Is(err, store.ErrNotFound) {
				notfound()
				return
			}
			if err != nil {
				fail()
				return
			}
			b.Trail = []page.Level{{Name: pageID, URL: "/candidates/" + pageID}, {Name: strconv.FormatInt(number, 10), URL: path}}
			cfg.Pages.Write(w, r, 200, "page", PageData{Banner: b, Page: pageView(p, c)})
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
			d := CandidateData{Candidate: candidateView(c), Estimates: []EstimateView{}, Links: []LinkView{}, Rulings: []RulingView{}, Pages: []PageRow{}, EnrichmentResults: []EnrichmentRow{}, Rejects: []RejectRecordRow{}, Campaigns: []CampaignRow{}, OwnerRecords: []OwnerRecordRow{}}
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
				owners, err := cfg.Store.OwnerRecords(r.Context(), cid)
				if err != nil {
					fail()
					return
				}
				d.OwnerRecords, d.Candidate.OwnerSet, d.Candidate.Founded = ownerViews(cid, owners)
				d.Enrichments = &EnrichmentsView{}
				park, err := cfg.Store.CandidatePark(r.Context(), cid)
				if err != nil {
					fail()
					return
				}
				d.Candidate.Park = parkLink(r.Context(), cfg.Store, park)
				uses, err := cfg.Store.CampaignUses(r.Context(), cid)
				if err != nil {
					fail()
					return
				}
				for _, use := range uses {
					row, err := campaignRow(r.Context(), cfg, use)
					if err != nil {
						fail()
						return
					}
					d.Campaigns = append(d.Campaigns, row)
				}
				enrichments, err := cfg.Store.Enrichments(r.Context(), cid)
				if err != nil {
					fail()
					return
				}
				kept, err := cfg.Store.Pages(r.Context(), cid)
				if err != nil {
					fail()
					return
				}
				for _, p := range kept {
					d.Pages = append(d.Pages, pageRow(p))
				}
				d.Enrichments.Basis = store.BasisOf(enrichments)
				d.Enrichments.Kinds = enrichmentKinds(enrichments)
				placesURL, err := cfg.Store.PlacesURL(r.Context(), cid)
				if err != nil {
					fail()
					return
				}
				setWebsite(&d.Candidate, enrichments, placesURL, ownerURL(owners))
				d.Enrichments.Brief = briefView(enrichments)
				subject, err := cfg.Store.Subject(r.Context(), cid)
				if err != nil {
					fail()
					return
				}
				for _, x := range enrichments {
					d.EnrichmentResults = append(d.EnrichmentResults, enrichmentRow(x, subject))
				}
				facts, err := cfg.Store.GetFacts(r.Context(), cid)
				if err == nil {
					v := factsView(facts, subject.Origin)
					d.Enrichments.Facts = &v
				} else if !errors.Is(err, store.ErrNotFound) {
					fail()
					return
				}
				a, err := cfg.Store.GetJudgments(r.Context(), cid)
				if err == nil {
					v := judgmentsView(a)
					d.Judgments = &v
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
					row := linkView(v, cfg.ServicesPath)
					row.Park = parkLink(r.Context(), cfg.Store, v.Park)
					d.Links = append(d.Links, row)
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

func decodeProspectListsPage(value string) (landingPosition, bool) {
	p := landingPosition{}
	if value == "" {
		return p, true
	}
	if (paging.Token{Tool: "prospect_lists"}).Decode(value, &p.name, &p.id) == nil {
		return p, true
	}
	if (paging.Token{Tool: "prospect_lists-before"}).Decode(value, &p.name, &p.id) == nil {
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

func decodePagePath(path string) (string, int64, bool) {
	parts := strings.Split(path, "/")
	if len(parts) != 5 || parts[0] != "" || parts[1] != "candidates" || !store.ValidCandidateID(parts[2]) || parts[3] != "pages" || parts[4] == "" || parts[4][0] == '0' {
		return "", 0, false
	}
	for _, c := range parts[4] {
		if c < '0' || c > '9' {
			return "", 0, false
		}
	}
	n, err := strconv.ParseInt(parts[4], 10, 64)
	return parts[2], n, err == nil
}

func campaignRow(ctx context.Context, cfg Config, u store.CampaignUse) (CampaignRow, error) {
	v := CampaignRow{Campaign: u.Campaign, CampaignPage: urls.CampaignPage(urls.Campaigns(cfg.ServicesPath), u.Campaign), ProspectList: u.ProspectList, Reason: u.Reason, Rejected: !u.Rejected.IsZero()}
	if u.ProspectList != "" {
		c, err := cfg.Store.GetProspectList(ctx, u.ProspectList)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return CampaignRow{}, err
		}
		if err == nil {
			v.ProspectListName = c.Name
		}
		if v.ProspectListName != "" {
			v.ProspectListPage = "/prospect-lists/" + u.ProspectList
		}
	}
	v.Added, v.AddedText = stamp(u.Added, MinuteLayout)
	v.RejectedAt, v.RejectedAtText = stamp(u.Rejected, MinuteLayout)
	v.Delivered, v.DeliveredText = stamp(u.Delivered, MinuteLayout)
	v.Opened, v.OpenedText = stamp(u.Opened, MinuteLayout)
	return v, nil
}
