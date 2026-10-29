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
)

// R-XT4M-RPN7 R-XUCJ-5HDW R-4BA8-N4ED R-Y2VT-TVKR
func TestBasisViewShapesAndProjection(t *testing.T) {
	_ = struct {
		Banner               page.Banner
		ProspectList         ProspectListView
		Matching, Classified int64
		ByBasis              BasisCountsView
		Changes              ChangesView
		Members              []MemberRow
		Rejects              []RejectRow
	}(ProspectListData{})
	_ = struct {
		ID, Name                                                                                   string
		Size                                                                                       int64
		Filters                                                                                    FiltersView
		Order                                                                                      []OrderRow
		CreatedBy, CreatedUser, Created, CreatedText, ChangedBy, ChangedUser, Changed, ChangedText string
	}(ProspectListView{})
	_ = struct{ Field, Direction string }(OrderRow{})
	_ = struct{ Records, Partial, Enriched int64 }(BasisCountsView{})
	_ = struct {
		Since, SinceText  string
		DefinitionChanged []string
		Joined            []JoinedRow
		Left              []LeftRow
	}(ChangesView{})
	_ = struct{ Candidate, Name, Basis, Why string }(JoinedRow{})
	_ = struct{ Candidate, Name, BasisThen, BasisNow, Why, Filter string }(LeftRow{})
	_ = struct {
		RevenueMin, RevenueMax string
		RevenueUnknown         bool
		MaxMinutes             string
		MinutesUnknown         bool
		Categories             []string
		CategoryUnknown        bool
		Fleet                  string
		FleetUnknown           bool
		Website                []string
		WebsiteUnknown         bool
		Trend                  []string
		TrendUnknown           bool
		Government             string
		MaxFilingAgeDays       string
		FilingUnknown          bool
		MinCampaignAgeDays     string
		Class                  string
		ClassUnknown           bool
		Basis                  []string
		ShowExcluded           bool
	}(FiltersView{})
	if got := basisCountsView(store.BasisCounts{Records: 3, Partial: 2, Enriched: 1}); got != (BasisCountsView{3, 2, 1}) {
		t.Fatal(got)
	}
	x := store.Changes{Since: fixtureTime(), DefinitionChanged: []string{store.DefinitionFilters, store.DefinitionSize}, Joined: []store.Joined{{Candidate: "joined-id", Name: "joined-name", Basis: store.BasisPartial, Why: store.WhyNew}}, Left: []store.Left{{Candidate: "left-id", Name: "left-name", BasisThen: store.BasisEnriched, BasisNow: store.BasisRecords, Why: store.WhyFilter, Filter: "max_minutes"}, {Candidate: "cut-id", Name: "cut-name", BasisThen: store.BasisRecords, BasisNow: store.BasisRecords, Why: store.WhyCut}}}
	want := ChangesView{"2026-10-05T09:00:07Z", "2026-10-05 09:00", []string{store.DefinitionFilters, store.DefinitionSize}, []JoinedRow{{"joined-id", "joined-name", store.BasisPartial, store.WhyNew}}, []LeftRow{{"left-id", "left-name", store.BasisEnriched, store.BasisRecords, store.WhyFilter, "max_minutes"}, {"cut-id", "cut-name", store.BasisRecords, store.BasisRecords, store.WhyCut, ""}}}
	if got := changesView(x); !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	if got := changesView(store.Changes{}); !reflect.DeepEqual(got, ChangesView{DefinitionChanged: []string{}, Joined: []JoinedRow{}, Left: []LeftRow{}}) {
		t.Fatal(got)
	}
}

// R-XZ84-OKCO R-HK09-DZRZ
func TestBasisFiltersAndMemberProjection(t *testing.T) {
	for _, basis := range [][]string{nil, {store.BasisRecords, store.BasisEnriched}} {
		got := filtersView(store.ProspectListFilters{Basis: basis})
		if got.Basis == nil || !reflect.DeepEqual(got.Basis, append([]string{}, basis...)) {
			t.Fatal(got)
		}
	}
	if got := memberRow(store.Ranked{Basis: store.BasisPartial}, 0); got.Basis != store.BasisPartial {
		t.Fatal(got)
	}
	cfg, _ := pageFixture(t)
	pl, e := cfg.Store.CreateProspectList(t.Context(), store.ProspectList{Name: "basis-filter-list", Size: 2, Filters: store.ProspectListFilters{Basis: []string{store.BasisEnriched, store.BasisRecords}}, CreatedUserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	got := pageProspectList(t, cfg, pl.ID)
	if !reflect.DeepEqual(got.ProspectList.Filters.Basis, []string{store.BasisRecords, store.BasisEnriched}) {
		t.Fatal(got)
	}
}

// R-IW98-Y03Q
func TestBasisTemplateStatesAndValues(t *testing.T) {
	set, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	render := func(name string, d any) string {
		t.Helper()
		var b bytes.Buffer
		if e := set.templates.ExecuteTemplate(&b, name, d); e != nil {
			t.Fatal(name, e)
		}
		return b.String()
	}
	contains := func(body string, values ...string) {
		t.Helper()
		for _, s := range values {
			if s != "" && !strings.Contains(body, s) {
				t.Fatalf("missing chosen value %q", s)
			}
		}
	}
	for _, basis := range []string{store.BasisRecords, store.BasisPartial, store.BasisEnriched} {
		for _, nonempty := range []bool{false, true} {
			d := ProspectListData{}
			c := CandidateData{Candidate: CandidateView{Status: store.StatusActive}, Enrichments: &EnrichmentsView{Basis: basis}}
			if nonempty {
				d.Members = []MemberRow{{Basis: basis}}
				d.ProspectList.Filters.Basis = []string{store.BasisRecords, store.BasisPartial, store.BasisEnriched}
				d.ByBasis = BasisCountsView{1, 2, 3}
				c.Enrichments.Kinds = KindsView{[]string{"chosen-present-kind"}, []string{"chosen-missing-kind"}}
			}
			render("prospect_list", d)
			render("candidate", c)
		}
	}
	for mask := 0; mask < 8; mask++ {
		d := ProspectListData{Members: []MemberRow{{Basis: "chosen-member-basis"}}, ProspectList: ProspectListView{Filters: FiltersView{Basis: []string{"chosen-filter-basis"}}}, Changes: ChangesView{Since: "chosen-since-stamp", SinceText: "chosen-since-text"}}
		values := []string{d.Changes.Since, d.Changes.SinceText, "chosen-member-basis", "chosen-filter-basis"}
		if mask&1 != 0 {
			d.Changes.DefinitionChanged = []string{store.DefinitionFilters, store.DefinitionOrder, store.DefinitionSize}
			values = append(values, d.Changes.DefinitionChanged...)
		}
		if mask&2 != 0 {
			for i, why := range []string{store.WhyNew, store.WhyPasses, store.WhyMovedUp} {
				suffix := string(rune('a' + i))
				j := JoinedRow{"joined-id-" + suffix, "joined-name-" + suffix, "joined-basis-" + suffix, why}
				d.Changes.Joined = append(d.Changes.Joined, j)
				values = append(values, j.Candidate, j.Name, j.Basis)
			}
		}
		if mask&4 != 0 {
			for i, why := range []string{store.WhyFilter, store.WhyRejected, store.WhyExcluded, store.WhyCut, store.WhyGone} {
				suffix := string(rune('a' + i))
				l := LeftRow{"left-id-" + suffix, "left-name-" + suffix, "left-before-" + suffix, "left-after-" + suffix, why, ""}
				if why == store.WhyGone {
					l.BasisNow = ""
				}
				if why == store.WhyFilter {
					l.Filter = "chosen-filter-member"
				}
				d.Changes.Left = append(d.Changes.Left, l)
				values = append(values, l.Candidate, l.Name, l.BasisThen, l.BasisNow, l.Filter)
			}
		}
		contains(render("prospect_list", d), values...)
	}
	c := CandidateData{Candidate: CandidateView{Status: store.StatusActive}, Enrichments: &EnrichmentsView{Basis: "chosen-candidate-basis", Kinds: KindsView{[]string{"chosen-present-first", "chosen-present-second"}, []string{"chosen-missing-first", "chosen-missing-second"}}}}
	contains(render("candidate", c), c.Enrichments.Basis, "chosen-present-first", "chosen-present-second", "chosen-missing-first", "chosen-missing-second")
}

func assertListBody(t *testing.T, cfg Config, method, id string, want store.ProspectListRead) {
	t.Helper()
	b := bannerFor(cfg)
	b.Trail = []page.Level{{Name: id, URL: "/prospect-lists/" + id}}
	d := ProspectListData{Banner: b, ProspectList: prospectListView(want.ProspectList), Matching: want.Matching, Classified: want.Classified, ByBasis: basisCountsView(want.ByBasis), Changes: changesView(want.Changes), Members: []MemberRow{}, Rejects: []RejectRow{}}
	for i, m := range want.Members {
		d.Members = append(d.Members, memberRow(m, i))
	}
	for _, r := range want.Rejects {
		d.Rejects = append(d.Rejects, rejectRow(r))
	}
	var expected bytes.Buffer
	if e := cfg.Pages.templates.ExecuteTemplate(&expected, "prospect_list", d); e != nil {
		t.Fatal(e)
	}
	response := requestPage(cfg, method, "/prospect-lists/"+id, "chosen-reader")
	if response.Code != http.StatusOK || method == http.MethodGet && response.Body.String() != expected.String() || method == http.MethodHead && response.Body.Len() != 0 {
		t.Fatalf("%s: status %d expected template equal %v", method, response.Code, response.Body.String() == expected.String())
	}
}

// R-Y43Q-7NBG R-6F76-WDU6 R-6HMZ-NXBK
func TestProspectListRequestsRecordOneOpen(t *testing.T) {
	cfg, handle := pageFixture(t)
	now := fixtureTime()
	cfg.Store = store.New(handle, store.Config{Now: func() time.Time { return now }, Rand: &sequence{}})
	ctx := t.Context()
	pl, e := cfg.Store.CreateProspectList(ctx, store.ProspectList{Name: "open-list", Size: 2, CreatedUserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	read := func() store.ProspectListRead {
		t.Helper()
		r, e := cfg.Store.ReadProspectList(ctx, pl.ID)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	opens := func() []store.Open {
		t.Helper()
		o, e := cfg.Store.Opens(ctx, pl.ID)
		if e != nil {
			t.Fatal(e)
		}
		return o
	}
	definition, e := cfg.Store.GetProspectList(ctx, pl.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got := read().Changes; !got.Since.IsZero() {
		t.Fatal(got)
	}
	assertListBody(t, cfg, http.MethodGet, pl.ID, read())
	if got := opens(); len(got) != 1 || got[0].UserID != "chosen-reader" || got[0].UserEmail != "reader@example.test" || !got[0].At.Equal(now) {
		t.Fatal(got)
	}
	now = fixtureTime().Add(83 * time.Second)
	if got := read().Changes; !got.Since.Equal(fixtureTime()) {
		t.Fatal(got)
	}
	assertListBody(t, cfg, http.MethodHead, pl.ID, read())
	if got := opens(); len(got) != 2 || !got[0].At.Equal(now) {
		t.Fatal(got)
	}
	now = now.Add(time.Minute)
	size := int64(3)
	if _, e = cfg.Store.EditProspectList(ctx, store.ProspectListEdit{ID: pl.ID, Size: &size, UserID: "editor"}); e != nil {
		t.Fatal(e)
	}
	if got := read().Changes; !reflect.DeepEqual(got.DefinitionChanged, []string{store.DefinitionSize}) {
		t.Fatal(got)
	}
	assertListBody(t, cfg, http.MethodGet, pl.ID, read())
	before := read()
	count := len(opens())
	for _, request := range []struct{ method, path string }{{"GET", "/"}, {"GET", "/about"}, {"POST", "/prospect-lists/" + pl.ID}, {"GET", "/prospect-lists/pl_0000000000000000"}} {
		requestPage(cfg, request.method, request.path, "chosen-reader")
	}
	if got := read(); !reflect.DeepEqual(got, before) || len(opens()) != count {
		t.Fatal("unrelated page recorded open", got)
	}
	now = now.Add(time.Minute)
	handle.SetFailing(true)
	for _, method := range []string{"GET", "HEAD"} {
		r := requestPage(cfg, method, "/prospect-lists/"+pl.ID, "chosen-reader")
		if r.Code != 503 {
			t.Fatal(method, r.Code)
		}
	}
	handle.SetFailing(false)
	if got := opens(); len(got) != count {
		t.Fatal(got)
	}
	if got := read(); !reflect.DeepEqual(got, before) {
		t.Fatal(got, before)
	}
	assertListBody(t, cfg, http.MethodGet, pl.ID, read())
	if len(opens()) != count+1 {
		t.Fatal(opens())
	}
	after, e := cfg.Store.GetProspectList(ctx, pl.ID)
	if e != nil {
		t.Fatal(e)
	}
	if !after.Created.Equal(definition.Created) || after.CreatedUserID != definition.CreatedUserID || after.ChangedUserID != "editor" {
		t.Fatal(after)
	}
	// A refusal on a never-opened list leaves its first successful open without changes.
	virgin, e := cfg.Store.CreateProspectList(ctx, store.ProspectList{Name: "virgin-list", Size: 1, CreatedUserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	handle.SetFailing(true)
	if r := requestPage(cfg, "GET", "/prospect-lists/"+virgin.ID, "chosen-reader"); r.Code != 503 {
		t.Fatal(r.Code)
	}
	handle.SetFailing(false)
	v, e := cfg.Store.ReadProspectList(ctx, virgin.ID)
	if e != nil || !v.Changes.Since.IsZero() {
		t.Fatal(v, e)
	}
	assertListBody(t, cfg, "GET", virgin.ID, v)
}

// R-F933-DLCR R-HK09-DZRZ R-Y43Q-7NBG
func TestCandidateBasisAndKindsOnPages(t *testing.T) {
	cfg, handle := pageFixture(t)
	now := fixtureTime()
	cfg.Store = store.New(handle, store.Config{Now: func() time.Time { return now }, Rand: &sequence{}})
	ctx := t.Context()
	_, ids, e := cfg.Store.Attach(ctx, "basis-run", false, []store.Observation{{Scraper: "chosen", SourceID: "basis-one", Digest: "one", Reading: sources.Reading{Name: "basis-business-one", NameKey: "one"}}, {Scraper: "chosen", SourceID: "basis-two", Digest: "two", Reading: sources.Reading{Name: "basis-business-two", NameKey: "two"}}})
	if e != nil || len(ids) != 2 {
		t.Fatal(ids, e)
	}
	if e = judgePageFixture(ctx, cfg.Store); e != nil {
		t.Fatal(e)
	}
	pl, e := cfg.Store.CreateProspectList(ctx, store.ProspectList{Name: "basis-members", Size: 2, CreatedUserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	all := []string{enrichers.KindWebsiteSearch, enrichers.KindSiteText, enrichers.KindBrief, enrichers.KindDriveRoute}
	check := func(id, basis string, present, missing []string) {
		t.Helper()
		c, e := cfg.Store.GetCandidate(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		es, e := cfg.Store.Enrichments(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		b := bannerFor(cfg)
		b.Trail = []page.Level{{Name: id, URL: "/candidates/" + id}}
		d := CandidateData{Banner: b, Candidate: candidateView(c), Enrichments: &EnrichmentsView{Basis: basis, Kinds: KindsView{present, missing}, Brief: briefView(es)}, Estimates: []EstimateView{}, Links: []LinkView{}, Rulings: []RulingView{}, Pages: []PageRow{}, EnrichmentResults: []EnrichmentRow{}, Rejects: []RejectRecordRow{}, Campaigns: []CampaignRow{}}
		setWebsite(&d.Candidate, es, "")
		subject, e := cfg.Store.Subject(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		if f, e := cfg.Store.GetFacts(ctx, id); e == nil {
			fv := factsView(f, subject.Origin)
			d.Enrichments.Facts = &fv
		}
		judgments, e := cfg.Store.GetJudgments(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		jv := judgmentsView(judgments)
		d.Judgments = &jv
		links, e := cfg.Store.Links(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		for _, l := range links {
			d.Links = append(d.Links, linkView(l, cfg.ServicesPath))
		}
		for _, x := range es {
			d.EnrichmentResults = append(d.EnrichmentResults, enrichmentRow(x, subject))
		}
		// Execute the embedded template with independent expected basis/kinds, then use the actual HTTP handler.
		var expected bytes.Buffer
		if e = cfg.Pages.templates.ExecuteTemplate(&expected, "candidate", d); e != nil {
			t.Fatal(e)
		}
		response := requestPage(cfg, "GET", "/candidates/"+id, "reader")
		if response.Code != 200 || response.Body.String() != expected.String() {
			t.Fatalf("basis %s kinds %#v: status %d body equal %v", basis, d.Enrichments.Kinds, response.Code, response.Body.String() == expected.String())
		}
		list := pageProspectList(t, cfg, pl.ID)
		found := false
		for _, m := range list.Members {
			if m.ID == id {
				found = true
				if m.Basis != basis {
					t.Fatal(m, basis)
				}
			}
		}
		if !found {
			t.Fatal("missing member", id)
		}
	}
	add := func(id, kind, source string, found any) {
		t.Helper()
		now = now.Add(time.Minute)
		if _, e := cfg.Store.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: kind, Source: source, Inputs: "chosen-inputs", Found: encodedJSON(found)}}); e != nil {
			t.Fatal(kind, e)
		}
	}
	check(ids[0], store.BasisRecords, []string{}, all)
	for _, kind := range []string{enrichers.KindLinkFacts, enrichers.KindDriveMinutes} {
		if _, e = cfg.Store.DeriveAmong(ctx, kind, []string{ids[0]}, false); e != nil {
			t.Fatal(e)
		}
	}
	add(ids[0], sources.WebsiteCheck, "chosen.example.test", map[string]any{"state": sources.WebsiteNone})
	check(ids[0], store.BasisRecords, []string{}, all)
	add(ids[0], enrichers.KindBrief, enrichers.SourcePrompts, enrichers.BriefFound{Run: "failed-brief", Status: "failed", Categories: []enrichers.BriefCategory{}, Labels: []string{}})
	check(ids[0], store.BasisPartial, []string{enrichers.KindBrief}, []string{enrichers.KindWebsiteSearch, enrichers.KindSiteText, enrichers.KindDriveRoute})
	add(ids[0], enrichers.KindBrief, enrichers.SourcePrompts, enrichers.BriefFound{Run: "good-brief", Status: "exited", ExitCode: new(0), Summary: "chosen-summary", Categories: []enrichers.BriefCategory{}, Labels: []string{}})
	add(ids[0], enrichers.KindDriveRoute, enrichers.SourceGoogleRoutes, enrichers.RouteFound{Minutes: new(int64(47)), Miles: new(30.0), Address: "chosen-address", Precision: "building", Status: "OK"})
	check(ids[0], store.BasisEnriched, []string{enrichers.KindBrief, enrichers.KindDriveRoute}, []string{enrichers.KindWebsiteSearch, enrichers.KindSiteText})
	list := pageProspectList(t, cfg, pl.ID)
	if list.ByBasis != (BasisCountsView{Records: 1, Enriched: 1}) {
		t.Fatal(list.ByBasis)
	}
	add(ids[0], enrichers.KindSiteText, enrichers.SourcePrompts, enrichers.SiteTextFound{Run: "site-run", Status: "exited", ExitCode: new(0)})
	add(ids[0], enrichers.KindWebsiteSearch, enrichers.SourcePrompts, enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "search-run", Status: enrichers.RunExited, ExitCode: new(0)}})
	check(ids[0], store.BasisEnriched, all, []string{})
}

// R-6HMZ-NXBK
func TestUnrelatedPageRequestsNeverRecordOpens(t *testing.T) {
	cfg, handle := pageFixture(t)
	now := fixtureTime()
	cfg.Store = store.New(handle, store.Config{Now: func() time.Time { return now }, Rand: &sequence{}})
	ctx := t.Context()
	_, ids, err := cfg.Store.Attach(ctx, "unrelated-run", false, []store.Observation{{Scraper: "chosen", SourceID: "unrelated-row", Digest: "chosen-digest", Reading: sources.Reading{Name: "unrelated-business", NameKey: "unrelated-business"}}})
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	if err = judgePageFixture(ctx, cfg.Store); err != nil {
		t.Fatal(err)
	}
	pass, err := cfg.Store.AddPass(ctx, store.Pass{Kind: store.KindJudge, Origin: store.OriginJudge, Trigger: store.TriggerJudgeTool, UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	retained := []string{}
	for _, name := range []string{"unrelated-first-list", "unrelated-second-list", "unrelated-deleted-list"} {
		pl, err := cfg.Store.CreateProspectList(ctx, store.ProspectList{Name: name, Size: 2, CreatedUserID: "owner"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = cfg.Store.OpenProspectList(ctx, pl.ID, "initial-reader", "initial@example.test"); err != nil {
			t.Fatal(err)
		}
		retained = append(retained, pl.ID)
	}
	deleted := retained[2]
	retained = retained[:2]
	if _, err = cfg.Store.DeleteProspectList(ctx, deleted); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	type listState struct {
		Read  store.ProspectListRead
		Opens []store.Open
	}
	snapshot := func() []listState {
		t.Helper()
		states := []listState{}
		for _, id := range retained {
			read, err := cfg.Store.ReadProspectList(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			opens, err := cfg.Store.Opens(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			states = append(states, listState{read, opens})
		}
		opens, err := cfg.Store.Opens(ctx, deleted)
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, listState{Opens: opens})
		return states
	}
	before := snapshot()
	for _, path := range []string{"/", "/candidates/" + ids[0], "/passes/" + pass.ID, "/prospect-lists/" + deleted, "/tools", "/about", "/unknown"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			response := requestPage(cfg, method, path, "unrelated-reader")
			want := http.StatusOK
			if path == "/prospect-lists/"+deleted || path == "/unknown" {
				want = http.StatusNotFound
			}
			if response.Code != want {
				t.Fatal(method, path, response.Code, want)
			}
			if after := snapshot(); !reflect.DeepEqual(after, before) {
				t.Fatal(method, path, "changed list reads or opens", after, before)
			}
		}
	}
	for _, id := range retained {
		if response := requestPage(cfg, http.MethodPost, "/prospect-lists/"+id, "unrelated-reader"); response.Code != http.StatusMethodNotAllowed {
			t.Fatal(response.Code)
		}
		if after := snapshot(); !reflect.DeepEqual(after, before) {
			t.Fatal("POST changed list reads or opens", after, before)
		}
	}
}
