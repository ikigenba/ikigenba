package pages

import (
	"bytes"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/urls"
)

// R-40V8-V2LG R-O907-V4XG R-3X7J-PRDD
func TestTargetPassAndCosts(t *testing.T) {
	for _, tc := range []struct {
		n    int64
		want string
	}{{0, "$0.000000"}, {499, "$0.000000"}, {500, "$0.000001"}, {23714000, "$0.023714"}, {23714499, "$0.023714"}, {23714500, "$0.023715"}, {1500000000, "$1.500000"}, {1234567890123456, "$1234567.890123"}} {
		if got := costText(tc.n); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	for _, target := range []string{"pl_0123456789abcdef", "cand_0123456789abcdef", "invalid", ""} {
		p := store.Pass{Kind: store.KindEnrichTarget, Target: target, EnrichKind: enrichers.KindSiteText, Runs: 2, RunsFailed: 1, Pages: 5, Cost: 23714000}
		v := passView(p, "")
		path := ""
		if store.ValidCandidateID(target) {
			path = "/candidates/" + target
		}
		if store.ValidProspectListID(target) {
			path = "/prospect-lists/" + target
		}
		if v.Target != target || v.TargetPage != path || v.EnrichKind != p.EnrichKind || v.Runs != 2 || v.RunsFailed != 1 || v.Pages != 5 || v.Cost != "$0.023714" {
			t.Fatal(v)
		}
	}
	v := passView(store.Pass{}, "")
	if v.Target != "" || v.TargetPage != "" || v.EnrichKind != "" || v.Runs != 0 || v.RunsFailed != 0 || v.Pages != 0 || v.Cost != "$0.000000" {
		t.Fatal(v)
	}
}

// R-909X-89BV
func TestSiteTextResultProjection(t *testing.T) {
	for _, tc := range []struct {
		status string
		code   *int
		failed bool
	}{{"exited", new(0), false}, {"exited", new(1), true}, {"failed", nil, true}, {"timed_out", nil, true}, {"killed", nil, true}} {
		found := enrichers.SiteTextFound{Run: "fixture-run", Status: tc.status, ExitCode: tc.code, Pages: 3, CostNanos: 23714000}
		v := enrichmentRow(store.Enrichment{Kind: enrichers.KindSiteText, Source: enrichers.SourcePrompts, Found: encodedJSON(found), Cost: .023714}, enrichers.Subject{})
		if v.Failed != tc.failed || v.Cost != "$0.023714" || v.State != "" || v.URL != "" || v.Title != "" || v.StatusCode != "" || v.Error != "" || v.Size != 0 {
			t.Fatal(v)
		}
		if tc.code != nil {
			if got := []string{v.Found[0].Key, v.Found[1].Key, v.Found[2].Key, v.Found[3].Key, v.Found[4].Key}; !reflect.DeepEqual(got, []string{"cost_nanos", "exit_code", "pages", "run", "status"}) {
				t.Fatal(got)
			}
		}
	}
	for _, tc := range []struct {
		cost float64
		want string
	}{{0, ""}, {.0000325, "$0.000033"}, {.0000004, "$0.000000"}, {.0000000004, ""}} {
		for _, kind := range []string{sources.WebsiteCheck, enrichers.KindDriveMinutes} {
			v := enrichmentRow(store.Enrichment{Kind: kind, Cost: tc.cost}, enrichers.Subject{})
			if v.Failed || v.Cost != tc.want {
				t.Fatal(v)
			}
		}
	}
	if enrichmentRow(store.Enrichment{Kind: enrichers.KindSiteText, Found: []byte("invalid")}, enrichers.Subject{}).Failed {
		t.Fatal("malformed result marked failed")
	}
}

// R-45QU-E5K8 R-XY08-ASLZ R-3ZNC-HAUR
func TestKeptPageProjections(t *testing.T) {
	p := store.Page{Candidate: "cand_0123456789abcdef", Number: 7, URL: "https://fixture.test/", Size: 37, Fetched: fixtureTime(), UserID: "fixture-user", Text: "fixture text"}
	row := pageRow(p)
	wantRow := PageRow{"7", "/candidates/" + p.Candidate + "/pages/7", p.URL, "", 37, "2026-10-05T09:00:07Z", "2026-10-05 09:00", "", p.UserID}
	if !reflect.DeepEqual(row, wantRow) {
		t.Fatal(row, wantRow)
	}
	view := pageView(p, store.Candidate{Name: "fixture-name"})
	wantView := PageView{p.Candidate, "fixture-name", "7", p.URL, "", 37, "2026-10-05T09:00:07Z", "2026-10-05 09:00:07 UTC", "", p.UserID, p.Text}
	if !reflect.DeepEqual(view, wantView) {
		t.Fatal(view, wantView)
	}
}

// R-46YQ-RXAX R-MITC-CU41 R-O6KF-3LG2
func TestKeptPagePathGrammar(t *testing.T) {
	cfg, d := pageFixture(t)
	d.SetFailing(true)
	base := "/candidates/cand_abcdef0123456789/pages/"
	for _, n := range []string{"1", "7", "9223372036854775807"} {
		if _, _, ok := decodePagePath(base + n); !ok {
			t.Fatal(n)
		}
	}
	for _, path := range []string{base + "0", base + "01", base + "-1", base + "+1", base + "9223372036854775808", base + "1/", base, strings.TrimSuffix(base, "/"), base + "%31", "/candidates/cand_abcdef012345678/pages/1", "/prospect-lists/pl_abcdef0123456789/pages/1", "/passes/pass_abcdef0123456789/pages/1", "/candidates/cand_abcdef0123456789/page/1"} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			got := requestPage(cfg, method, path, "reader")
			want := requestPage(cfg, method, "/unknown", "reader")
			if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
				t.Fatal(path, method, got.Code)
			}
		}
	}
}

// R-92PP-ZST9 R-Y6JI-Z6SU R-UM9B-S0C0 R-WUQP-0V9U
func TestKeptPageRoutes(t *testing.T) {
	cfg, d := pageFixture(t)
	ctx := t.Context()
	_, ids, err := cfg.Store.Attach(ctx, "fixture-run", false, []store.Observation{{Scraper: "fixture", SourceID: "one", Digest: "one", Reading: sources.Reading{Name: "fixture-name", Names: []string{"fixture-name"}, NameKey: "one"}}, {Scraper: "fixture", SourceID: "two", Digest: "two", Reading: sources.Reading{Name: "second-name", Names: []string{"second-name"}, NameKey: "two"}}})
	if err != nil || len(ids) != 2 {
		t.Fatal(ids, err)
	}
	var kept []store.Page
	for i, u := range []string{"https://fixture.test/a", "https://fixture.test/b", "https://fixture.test/a"} {
		p, err := cfg.Store.AddPage(ctx, store.Page{Candidate: ids[0], URL: u, Title: []string{"old title", "other title", "new title"}[i], Text: []string{"old text", "", "new text"}[i], Size: int64(100 + i), Fetched: fixtureTime().Add(time.Duration(i) * time.Minute), UserID: "fixture-adder", UserEmail: "adder@example.test"})
		if err != nil {
			t.Fatal(err)
		}
		kept = append(kept, p)
	}
	c, err := cfg.Store.GetCandidate(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range kept {
		b := bannerFor(cfg)
		b.Trail = []page.Level{{Name: ids[0], URL: "/candidates/" + ids[0]}, {Name: pageRow(p).Number, URL: urls.PagePath(ids[0], p.Number)}}
		comparePage(t, cfg, urls.PagePath(ids[0], p.Number), "page", 200, PageData{b, pageView(p, c)})
	}
	got := capturedCandidate(t, cfg, ids[0])
	want := []PageRow{pageRow(kept[2]), pageRow(kept[1])}
	if !reflect.DeepEqual(got.Pages, want) {
		t.Fatal(got.Pages, want)
	}
	for _, p := range got.Pages {
		if w := requestPage(cfg, "GET", p.Path, "reader"); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if got := capturedCandidate(t, cfg, ids[1]); got.Pages == nil || len(got.Pages) != 0 {
		t.Fatal(got.Pages)
	}
	for _, path := range []string{urls.PagePath(ids[1], kept[0].Number), urls.PagePath(ids[0], 9223372036854775807)} {
		comparePage(t, cfg, path, "notfound", 404, NotFoundData{bannerFor(cfg)})
	}
	valid := urls.PagePath(ids[0], kept[0].Number)
	for _, failing := range []bool{false, true} {
		d.SetFailing(failing)
		for _, method := range []string{"POST", "PUT", "DELETE"} {
			w := requestPage(cfg, method, valid, "reader")
			if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" || w.Body.Len() != 0 {
				t.Fatal(w)
			}
		}
	}
	for _, method := range []string{"GET", "HEAD"} {
		w := requestPage(cfg, method, valid, "reader")
		if w.Code != 503 {
			t.Fatal(w.Code)
		}
	}
	d.SetFailing(false)
	if _, _, err := cfg.Store.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids[1], Other: ids[0], UserID: "owner", Why: "fixture merge"}); err != nil {
		t.Fatal(err)
	}
	comparePage(t, cfg, valid, "notfound", 404, NotFoundData{bannerFor(cfg)})
	for _, p := range kept {
		p.Candidate = ids[1]
		c, err := cfg.Store.GetCandidate(ctx, ids[1])
		if err != nil {
			t.Fatal(err)
		}
		b := bannerFor(cfg)
		b.Trail = []page.Level{{Name: ids[1], URL: "/candidates/" + ids[1]}, {Name: pageRow(p).Number, URL: urls.PagePath(ids[1], p.Number)}}
		comparePage(t, cfg, urls.PagePath(ids[1], p.Number), "page", 200, PageData{b, pageView(p, c)})
	}

	if got := capturedCandidate(t, cfg, ids[0]); got.Pages == nil || len(got.Pages) != 0 || got.Candidate.Status != store.StatusMerged || got.Enrichments != nil || got.Judgments != nil || len(got.EnrichmentResults) != 0 || len(got.Rulings) != 1 {
		t.Fatal(got.Pages)
	}
}

// R-98T7-WNIQ
func TestMemberSiteTextStamp(t *testing.T) {
	for _, at := range []time.Time{{}, fixtureTime()} {
		v := memberRow(store.Ranked{SiteText: at}, 0)
		a, b := stamp(at, MinuteLayout)
		if v.SiteText != a || v.SiteTextText != b {
			t.Fatal(v)
		}
	}
}

// R-P8DA-YHFL
func TestAddedTemplateStates(t *testing.T) {
	t.Chdir(t.TempDir())
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	render := func(name string, data any) {
		t.Helper()
		if err := s.templates.ExecuteTemplate(&bytes.Buffer{}, name, data); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, text := range []string{"", "fixture-value"} {
		for _, flag := range []bool{false, true} {
			p := PageView{Candidate: "cand_abcdef0123456789", Name: text, Number: "7", URL: "https://fixture.test/", Title: text, Size: 7, Fetched: text, FetchedText: text, By: text, User: "fixture-user", Text: text}
			if !flag {
				p.Size = 0
			}
			render("page", PageData{Page: p})
			for _, kind := range []string{enrichers.KindSiteText, sources.WebsiteCheck, enrichers.KindDriveMinutes, enrichers.KindLinkFacts} {
				for _, candidateStatus := range []string{store.StatusActive, store.StatusMerged} {
					for _, found := range [][]FactRow{{}, {{"a", "1"}}, {{"a", "1"}, {"b", "2"}}} {
						render("candidate", CandidateData{Candidate: CandidateView{Status: candidateStatus}, Pages: []PageRow{{Number: "7", URL: p.URL, Title: text, By: text, User: p.User}}, EnrichmentResults: []EnrichmentRow{{Kind: kind, Cost: text, Failed: flag, Found: found}}})
					}
				}
				for _, target := range []string{"pl_abcdef0123456789", "cand_abcdef0123456789"} {
					for _, status := range []string{store.StatusRunning, store.StatusDone, store.StatusFailed} {
						for _, reason := range []string{store.ReasonScrapes, store.ReasonStore, store.ReasonInterrupted, store.ReasonPrompts} {
							for _, by := range []string{"fixture@example.test", "fixture-user"} {
								path := "/prospect-lists/" + target
								if store.ValidCandidateID(target) {
									path = "/candidates/" + target
								}
								v := PassView{Kind: store.KindEnrichTarget, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, Status: status, Reason: reason, Finished: text, FinishedText: text, Target: target, TargetPage: path, EnrichKind: kind, StartedBy: by, Cost: "$0.000000"}
								if flag {
									v.Derived, v.Due, v.Fetched, v.HTTPS, v.HTTP, v.None, v.Runs, v.RunsFailed, v.Pages, v.Cost = 7, 5, 3, 2, 1, 2, 2, 1, 4, "$0.023714"
								}
								render("pass", PassData{Pass: v})
								render("landing", LandingData{Passes: []PassRow{{Kind: store.KindEnrichTarget, Trigger: store.TriggerEnrich, Status: status, Reason: reason, Finished: text, FinishedText: text}}})
							}
						}
					}
				}
			}
			render("prospect_list", ProspectListData{Members: []MemberRow{{SiteText: text, SiteTextText: text}}})
		}
	}
}

// R-98T7-WNIQ
func TestSiteTextCandidateAndProspectListAgree(t *testing.T) {
	cfg, _ := pageFixture(t)
	ctx := t.Context()
	_, ids, err := cfg.Store.Attach(ctx, "fixture-run", false, []store.Observation{
		{Scraper: "fixture", SourceID: "one", Digest: "one", Reading: sources.Reading{Name: "first", Names: []string{"first"}, NameKey: "first"}},
		{Scraper: "fixture", SourceID: "two", Digest: "two", Reading: sources.Reading{Name: "second", Names: []string{"second"}, NameKey: "second"}},
	})
	if err != nil || len(ids) != 2 {
		t.Fatal(ids, err)
	}
	h, err := cfg.Store.CreateProspectList(ctx, store.ProspectList{Name: "fixture", Size: 10, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Store.AddEnrichments(ctx, []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindSiteText, Inputs: "fixture", Source: enrichers.SourcePrompts, Found: encodedJSON(enrichers.SiteTextFound{Run: "fixture-run", Status: "exited", ExitCode: new(1), CostNanos: 23714000}), Cost: .023714}}); err != nil {
		t.Fatal(err)
	}
	candidate := capturedCandidate(t, cfg, ids[0])
	if candidate.Enrichments.Facts == nil || len(candidate.EnrichmentResults) != 1 || !candidate.EnrichmentResults[0].Failed || candidate.EnrichmentResults[0].Cost != "$0.023714" {
		t.Fatal(candidate)
	}
	written := ""
	for _, w := range candidate.Enrichments.Facts.Written {
		if w.Kind == enrichers.KindSiteText {
			written = w.At
		}
	}
	if written == "" {
		t.Fatal(candidate.Enrichments.Facts)
	}
	if err := judgePageFixture(ctx, cfg.Store); err != nil {
		t.Fatal(err)
	}
	prospectList := pageProspectList(t, cfg, h.ID)
	if len(prospectList.Members) != 2 {
		t.Fatal(prospectList)
	}
	for _, member := range prospectList.Members {
		if member.ID == ids[0] {
			if member.SiteText != written || member.SiteTextText != "2026-10-05 09:00" {
				t.Fatal(member, candidate.Enrichments.Facts)
			}
		} else if member.SiteText != "" || member.SiteTextText != "" {
			t.Fatal(member)
		}
	}
}

// R-O907-V4XG R-3X7J-PRDD
func TestTargetPassPage(t *testing.T) {
	cfg, _ := pageFixture(t)
	for _, target := range []string{"pl_abcdef0123456789", "cand_abcdef0123456789"} {
		p, err := cfg.Store.AddPass(t.Context(), store.Pass{Kind: store.KindEnrichTarget, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: "fixture-owner", Target: target, EnrichKind: enrichers.KindSiteText})
		if err != nil {
			t.Fatal(err)
		}
		b := bannerFor(cfg)
		b.Trail = []page.Level{{Name: p.ID, URL: "/passes/" + p.ID}}
		want := passView(p, "")
		if want.Cost != "$0.000000" || want.TargetPage == "" {
			t.Fatal(want)
		}
		comparePage(t, cfg, "/passes/"+p.ID, "pass", 200, PassData{b, want})
		p, err = cfg.Store.FinishPass(t.Context(), p.ID, store.Counts{Derived: 7, Due: 5, Fetched: 3, HTTPS: 2, HTTP: 1, None: 2, Runs: 2, RunsFailed: 1, Pages: 5, Cost: 23714000})
		if err != nil {
			t.Fatal(err)
		}
		want = passView(p, "")
		if want.Runs != 2 || want.RunsFailed != 1 || want.Pages != 5 || want.Cost != "$0.023714" || want.StartedBy != "fixture-owner" {
			t.Fatal(want)
		}
		comparePage(t, cfg, "/passes/"+p.ID, "pass", 200, PassData{b, want})
	}
}

// R-XY08-ASLZ R-3X7J-PRDD R-3ZNC-HAUR
// R-JN4O-3OFH
func TestAddedViewShapes(t *testing.T) {
	candidate := CandidateData{page.Banner{}, CandidateView{}, nil, nil, []EstimateView{}, []LinkView{}, []RulingView{}, []PageRow{}, []EnrichmentRow{}, []RejectRecordRow{}, []CampaignRow{}}
	if candidate.Pages == nil || candidate.EnrichmentResults == nil {
		t.Fatal(candidate)
	}
	_ = struct {
		ID, Kind, Origin, Trigger, RunPage, Status, Reason, StartedBy, Started, StartedText, Finished, FinishedText      string
		Rows, New, Changed, Unchanged, Gone, Created, Linked, Judged, Unranged, Derived, Due, Fetched, HTTPS, HTTP, None int64
		Target, TargetPage, EnrichKind                                                                                   string
		Runs, RunsFailed, Pages                                                                                          int64
		Cost                                                                                                             string
	}(PassView{})
	_ = struct {
		Kind, Source                  string
		Found                         []FactRow
		State, URL, Title, StatusCode string
		Size                          int64
		Error                         string
		Run                           *RunView
		Answer                        *AnswerView
		Confirmation                  *ConfirmationView
		Route                         *RouteView
		Failed                        bool
		Cost, At, AtText              string
	}(EnrichmentRow{})
	_ = PageData{page.Banner{}, PageView{"candidate", "name", "1", "url", "title", 3, "fetched", "fetched text", "by", "user", "text"}}
}
