package pages

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-DVMK-0Z5X R-DWUG-EQWM R-DY2C-SINB R-DZA9-6AE0 R-E45U-PDCS
// R-HM0W-RKUQ R-0M3O-U80L
func TestSearchEnrichmentProjection(t *testing.T) {
	zero, one := 0, 1
	full := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "r1", Status: enrichers.RunExited, ExitCode: &zero}, Answer: &enrichers.WebsiteSearchAnswer{URL: "https://acme.test/", Why: "chosen-why", Rejected: []enrichers.WebsiteSearchSite{{URL: "https://other.test/", Why: "chosen-rejection"}, {URL: "https://third.test/"}}}, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://www.acme.test/", Title: "chosen-title", StatusCode: 200, Size: 1256}}
	for _, f := range []enrichers.WebsiteSearchFound{full, {Run: full.Run}, {Run: full.Run, Answer: &enrichers.WebsiteSearchAnswer{}}, {Run: full.Run, Answer: full.Answer, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteNone, Error: "chosen-error"}}, {Run: enrichers.WebsiteSearchRun{Status: "failed"}}, {Run: enrichers.WebsiteSearchRun{Status: enrichers.RunExited, ExitCode: &one}, Answer: full.Answer}, {Run: enrichers.WebsiteSearchRun{Status: "killed"}, Answer: full.Answer}} {
		x := store.Enrichment{Kind: enrichers.KindWebsiteSearch, Source: enrichers.SourcePrompts, Found: encodedJSON(f), Cost: .023714, At: fixtureTime()}
		got := enrichmentRow(x, enrichers.Subject{})
		want := EnrichmentRow{Kind: x.Kind, Source: x.Source, Found: []FactRow{}, Run: &RunView{ID: f.Run.ID, Status: f.Run.Status}, Failed: f.Failed(), Cost: "$0.023714", At: "2026-10-05T09:00:07Z", AtText: "2026-10-05 09:00"}
		if f.Run.ExitCode != nil {
			if *f.Run.ExitCode == 0 {
				want.Run.ExitCode = "0"
			} else {
				want.Run.ExitCode = "1"
			}
		}
		if f.Answer != nil {
			want.Answer = &AnswerView{URL: f.Answer.URL, Why: f.Answer.Why, Rejected: []RejectedRow{}}
			for _, r := range f.Answer.Rejected {
				want.Answer.Rejected = append(want.Answer.Rejected, RejectedRow{r.URL, r.Why})
			}
		}
		if c := f.Confirmation; c != nil {
			want.Confirmation = &ConfirmationView{State: c.State, URL: c.URL, Title: c.Title, Size: c.Size, Error: c.Error}
			if c.StatusCode != 0 {
				want.Confirmation.StatusCode = "200"
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v want %#v", got, want)
		}
	}
	// The nested fields are absent on the other result shapes.
	for _, kind := range []string{sources.WebsiteCheck, enrichers.KindSiteText, enrichers.KindBrief, enrichers.KindLinkFacts} {
		got := enrichmentRow(store.Enrichment{Kind: kind, Found: json.RawMessage(`{}`)}, enrichers.Subject{})
		if got.Run != nil || got.Answer != nil || got.Confirmation != nil {
			t.Fatal(got)
		}
	}
}

// R-RU1K-AXX2 R-8PAT-SBNM R-8QIQ-63EB
func TestSearchWebsiteCandidateAndMember(t *testing.T) {
	cfg, _ := pageFixture(t)
	_, ids, err := cfg.Store.Attach(t.Context(), "run", false, []store.Observation{{Scraper: "fixture", SourceID: "row", Digest: "digest", Reading: sources.Reading{Name: "search-business", NameKey: "search-business"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := ids[0]
	if err = judgePageFixture(t.Context(), cfg.Store); err != nil {
		t.Fatal(err)
	}
	co, err := cfg.Store.CreateProspectList(t.Context(), store.ProspectList{Name: "search-prospect_list", Size: 10, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	f := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "chosen-run", Status: enrichers.RunExited, ExitCode: &zero}, Answer: &enrichers.WebsiteSearchAnswer{URL: "https://acme.test/", Why: "chosen-explanation"}, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://www.acme.test/", Title: "chosen-title", StatusCode: 200}}
	record := func(kind string, found any) {
		t.Helper()
		if _, err := cfg.Store.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: kind, Source: enrichers.SourcePrompts, Inputs: "chosen-input", Found: encodedJSON(found), Cost: .023714}}); err != nil {
			t.Fatal(err)
		}
	}
	record(sources.WebsiteCheck, website.Result{State: sources.WebsiteNone})
	record(enrichers.KindWebsiteSearch, f)
	got := capturedCandidate(t, cfg, id)
	if got.Candidate.WebsiteSource != store.WebsiteBySearch || got.Candidate.Website != sources.WebsiteHTTPS || got.Candidate.WebsiteURL != f.Confirmation.URL || got.Candidate.WebsiteTitle != f.Confirmation.Title || got.Candidate.WebsiteWhy != f.Answer.Why || got.Candidate.WebsiteChecked == "" {
		t.Fatal(got.Candidate)
	}
	if len(got.EnrichmentResults) != 2 || got.EnrichmentResults[0].Run.ID != f.Run.ID || got.EnrichmentResults[0].Failed || got.Enrichments.Facts == nil {
		t.Fatal(got)
	}
	written := false
	for _, w := range got.Enrichments.Facts.Written {
		if w.Kind == enrichers.KindWebsiteSearch {
			written = true
		}
	}
	if !written {
		t.Fatal(got.Enrichments.Facts)
	}
	checkMember := func(source, state, url string) {
		t.Helper()
		view := pageProspectList(t, cfg, co.ID)
		if len(view.Members) != 1 {
			t.Fatal(view)
		}
		m := view.Members[0]
		if m.WebsiteSource != source || m.Website != state || m.WebsiteURL != url {
			t.Fatal(m)
		}
	}
	checkMember(store.WebsiteBySearch, sources.WebsiteHTTPS, f.Confirmation.URL)
	record(sources.WebsiteCheck, website.Result{State: sources.WebsiteHTTP, URL: "http://check.test/", Title: "check-title"})
	got = capturedCandidate(t, cfg, id)
	if got.Candidate.WebsiteSource != store.WebsiteByCheck || got.Candidate.WebsiteWhy != "" {
		t.Fatal(got.Candidate)
	}
	checkMember(store.WebsiteByCheck, sources.WebsiteHTTP, "http://check.test/")
	// A failed search and an unconfirmed answer leave the working check in charge.
	for _, failed := range []bool{false, true} {
		f.Confirmation.State = sources.WebsiteNone
		if failed {
			f.Run.Status = "failed"
		}
		record(enrichers.KindWebsiteSearch, f)
		got = capturedCandidate(t, cfg, id)
		if got.Candidate.WebsiteSource != store.WebsiteByCheck {
			t.Fatal(got.Candidate)
		}
	}
}

// R-IOXU-NDNK
func TestWebsiteSearchTemplateStates(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	render := func(name string, data any) {
		t.Helper()
		if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, name, data); err != nil {
			t.Fatal(err)
		}
	}
	for _, source := range []string{"", store.WebsiteByCheck, store.WebsiteBySearch} {
		for _, state := range []string{sources.WebsiteHTTPS, sources.WebsiteHTTP, sources.WebsiteNone, sources.WebsiteUnknown} {
			for _, text := range []string{"", "chosen-value"} {
				d := CandidateData{Candidate: CandidateView{Status: store.StatusActive, WebsiteSource: source, Website: state, WebsiteURL: text, WebsiteTitle: text, WebsiteWhy: text, WebsiteChecked: text, WebsiteCheckedText: text}, Enrichments: &EnrichmentsView{Facts: &FactsView{Written: []WrittenRow{{Kind: enrichers.KindWebsiteSearch}}}}}
				for _, failed := range []bool{false, true} {
					for n := 0; n < 3; n++ {
						r := EnrichmentRow{Kind: enrichers.KindWebsiteSearch, Found: []FactRow{}, Failed: failed, Cost: text, Run: &RunView{ID: text, Status: text, ExitCode: text}}
						render("candidate", CandidateData{Candidate: d.Candidate, EnrichmentResults: []EnrichmentRow{r}})
						r.Answer = &AnswerView{URL: text, Why: text, Rejected: []RejectedRow{}}
						for i := 0; i < n; i++ {
							r.Answer.Rejected = append(r.Answer.Rejected, RejectedRow{URL: text, Why: text})
						}
						d.EnrichmentResults = []EnrichmentRow{r}
						render("candidate", d)
						r.Confirmation = &ConfirmationView{State: state, URL: text, Title: text, StatusCode: text, Size: 1256, Error: text}
						d.EnrichmentResults = []EnrichmentRow{r, {Kind: enrichers.KindLinkFacts}, {Kind: sources.WebsiteCheck}}
						render("candidate", d)
					}
				}
				render("prospect_list", ProspectListData{Members: []MemberRow{{WebsiteSource: source, Website: state, WebsiteURL: text}}})
			}
		}
	}
	render("pass", PassData{Pass: PassView{Kind: store.KindEnrichTarget, EnrichKind: enrichers.KindWebsiteSearch, Runs: 2, RunsFailed: 1, Cost: "$0.023714"}})
}

// R-DVMK-0Z5X R-DWUG-EQWM R-DY2C-SINB R-DZA9-6AE0
func TestSearchViewShapes(_ *testing.T) {
	_ = struct{ ID, Status, ExitCode string }(RunView{})
	_ = struct {
		URL, Why string
		Rejected []RejectedRow
	}(AnswerView{})
	_ = struct{ URL, Why string }(RejectedRow{})
	_ = struct {
		State, URL, Title, StatusCode string
		Size                          int64
		Error                         string
	}(ConfirmationView{})
}

// R-8PAT-SBNM
func TestWebsiteSearchDecisionHistory(t *testing.T) {
	zero := 0
	good := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{Status: enrichers.RunExited, ExitCode: &zero}, Answer: &enrichers.WebsiteSearchAnswer{URL: "https://answer.test/", Why: "chosen-why"}, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://confirmed.test/", Title: "chosen-title"}}
	search := store.Enrichment{Kind: enrichers.KindWebsiteSearch, Found: encodedJSON(good), At: fixtureTime()}
	for _, bad := range []enrichers.WebsiteSearchFound{{Run: enrichers.WebsiteSearchRun{Status: "failed"}, Answer: good.Answer, Confirmation: good.Confirmation}, {Run: good.Run, Answer: good.Answer, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteNone}}, {Run: good.Run, Answer: &enrichers.WebsiteSearchAnswer{}}} {
		var got CandidateView
		setWebsite(&got, []store.Enrichment{{Kind: enrichers.KindWebsiteSearch, Found: encodedJSON(bad), At: fixtureTime().Add(1)}, search}, "", "")
		if got.WebsiteSource != store.WebsiteBySearch || got.Website != sources.WebsiteHTTPS || got.WebsiteURL != good.Confirmation.URL || got.WebsiteTitle != good.Confirmation.Title || got.WebsiteWhy != good.Answer.Why || got.WebsiteChecked != "2026-10-05T09:00:07Z" || got.WebsiteCheckedText != "2026-10-05 09:00:07 UTC" {
			t.Fatal(got)
		}
	}
	check := store.Enrichment{Kind: sources.WebsiteCheck, Found: encodedJSON(website.Result{State: sources.WebsiteHTTP, URL: "http://check.test/", Title: "check-title"}), At: fixtureTime()}
	for _, es := range [][]store.Enrichment{{search, check}, {check, search}} {
		var got CandidateView
		setWebsite(&got, es, "", "")
		if got.WebsiteSource != store.WebsiteByCheck || got.Website != sources.WebsiteHTTP || got.WebsiteURL != "http://check.test/" || got.WebsiteTitle != "check-title" || got.WebsiteWhy != "" {
			t.Fatal(got)
		}
	}
}

// R-RU1K-AXX2
// R-4A2C-9CNO
func TestCandidateAndMemberViewShapes(_ *testing.T) {
	_ = struct {
		Banner            page.Banner
		Candidate         CandidateView
		Enrichments       *EnrichmentsView
		Judgments         *JudgmentsView
		Estimates         []EstimateView
		Links             []LinkView
		Rulings           []RulingView
		OwnerRecords      []OwnerRecordRow
		Pages             []PageRow
		EnrichmentResults []EnrichmentRow
		Rejects           []RejectRecordRow
		Campaigns         []CampaignRow
	}(CandidateData{})
	_ = struct {
		Basis string
		Kinds KindsView
		Facts *FactsView
		Brief *BriefView
	}(EnrichmentsView{})
	_ = struct{ Present, Missing []string }(KindsView{})
	_ = struct {
		ID, Status, MergedInto, Name                                                                                        string
		Street, City, State, Zip, County, Phone, Email                                                                      string
		NameSource, AddressSource, PhoneSource, EmailSource                                                                 string
		OtherNames, OtherAddresses, OtherPhones                                                                             []OtherRow
		Park                                                                                                                *ParkLink
		WebsiteSource, Website, WebsiteURL, WebsiteTitle, WebsiteWhy, WebsiteChecked, WebsiteCheckedText, WebsiteCheckedURL string
		People                                                                                                              []PersonRow
		EIN, EINSource                                                                                                      string
		OtherEmails                                                                                                         []OtherRow
		Founded                                                                                                             *OwnerNoteView
		OwnerSet                                                                                                            map[string]OwnerNoteView
		Class                                                                                                               string
		Cargo                                                                                                               []string
		Created, CreatedText, Updated, UpdatedText                                                                          string
	}(CandidateView{})
	_ = struct {
		Number, Path, URL, Title               string
		Size                                   int64
		Reason, Fetched, FetchedText, By, User string
	}(PageRow{})
	_ = struct{ Kind, ProspectList, ProspectListName, By, User, At, AtText, Why string }(RejectRecordRow{})
	_ = struct {
		Rank                                                                                                                                                                                                                 int64
		ID, Name, City, State, Basis, OperatingConfidence, RevenueLow, RevenueHigh, RevenueConfidence, HeadcountLow, Trend, TrendChange                                                                                      string
		Categories                                                                                                                                                                                                           []string
		ClassifiedBy, Class, FleetVerdict, Website, WebsiteURL, WebsiteSource, SiteText, SiteTextText, Brief, BriefText, Minutes, MinutesSource, GovernmentMatch, KnownSince, KnownSinceText, NewestFiling, NewestFilingText string
		Excluded                                                                                                                                                                                                             bool
	}(MemberRow{})
	_ = struct{ Candidate, By, User, At, AtText, Why string }(RejectRow{})
}

// R-8PAT-SBNM
func TestUnfoundWebsiteDecisionFields(t *testing.T) {
	at := fixtureTime()
	check := store.Enrichment{Kind: sources.WebsiteCheck, Found: encodedJSON(website.Result{State: sources.WebsiteNone, URL: "https://ignored.test/", Title: "check-title"}), At: at}
	workingCheck := store.Enrichment{Kind: sources.WebsiteCheck, Found: encodedJSON(website.Result{State: sources.WebsiteHTTP, URL: "http://check.test/", Title: "working-title"}), At: at}
	empty := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{Status: enrichers.RunExited, ExitCode: new(0)}, Answer: &enrichers.WebsiteSearchAnswer{Why: "search-reason"}}
	unloaded := empty
	unloaded.Answer = &enrichers.WebsiteSearchAnswer{URL: "https://unloaded.test/", Why: "search-reason"}
	unloaded.Confirmation = &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteNone, Title: "ignored-title"}
	incomplete := unloaded
	incomplete.Confirmation = nil
	failed := empty
	failed.Run.Status = "failed"
	confirmed := unloaded
	confirmed.Confirmation = &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://confirmed.test/", Title: "confirmed-title"}
	search := func(f enrichers.WebsiteSearchFound) store.Enrichment {
		return store.Enrichment{Kind: enrichers.KindWebsiteSearch, Found: encodedJSON(f), At: at.Add(time.Hour)}
	}
	none := CandidateView{Website: sources.WebsiteNone, WebsiteWhy: empty.Answer.Why, WebsiteChecked: "2026-10-05T10:00:07Z", WebsiteCheckedText: "2026-10-05 10:00:07 UTC"}
	unknown := CandidateView{Website: sources.WebsiteUnknown, WebsiteTitle: "check-title", WebsiteChecked: "2026-10-05T09:00:07Z", WebsiteCheckedText: "2026-10-05 09:00:07 UTC"}
	found := CandidateView{WebsiteSource: store.WebsiteBySearch, Website: sources.WebsiteHTTPS, WebsiteURL: confirmed.Confirmation.URL, WebsiteTitle: confirmed.Confirmation.Title, WebsiteWhy: confirmed.Answer.Why, WebsiteChecked: none.WebsiteChecked, WebsiteCheckedText: none.WebsiteCheckedText}
	checked := CandidateView{WebsiteSource: store.WebsiteByCheck, Website: sources.WebsiteHTTP, WebsiteURL: "http://check.test/", WebsiteTitle: "working-title", WebsiteChecked: unknown.WebsiteChecked, WebsiteCheckedText: unknown.WebsiteCheckedText}
	for _, tc := range []struct {
		results []store.Enrichment
		want    CandidateView
	}{
		{nil, CandidateView{Website: sources.WebsiteUnknown}},
		{[]store.Enrichment{{Kind: enrichers.KindLinkFacts}, {Kind: enrichers.KindDriveMinutes}}, CandidateView{Website: sources.WebsiteUnknown}},
		{[]store.Enrichment{check}, unknown},
		{[]store.Enrichment{check, workingCheck}, unknown},
		{[]store.Enrichment{search(empty), check}, none},
		{[]store.Enrichment{search(unloaded), check}, none},
		{[]store.Enrichment{check, search(empty)}, none},
		{[]store.Enrichment{search(incomplete), search(empty)}, none},
		{[]store.Enrichment{search(failed), search(empty)}, none},
		{[]store.Enrichment{check, search(empty), workingCheck}, none},
		{[]store.Enrichment{workingCheck, search(empty)}, checked},
		{[]store.Enrichment{search(empty), workingCheck}, checked},
		{[]store.Enrichment{search(empty), search(confirmed), check}, found},
		{[]store.Enrichment{search(unloaded), search(confirmed), check}, found},
		{[]store.Enrichment{search(failed), search(confirmed), check}, found},
		{[]store.Enrichment{search(failed), check}, unknown},
	} {
		var got CandidateView
		setWebsite(&got, tc.results, "", "")
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("results %+v: got %+v want %+v", tc.results, got, tc.want)
		}
	}
}

// R-IQ5R-15E9
func TestUnfoundAndUnknownWebsiteTemplates(t *testing.T) {
	t.Chdir(t.TempDir())
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{sources.WebsiteNone, sources.WebsiteUnknown} {
		for _, detail := range []string{"", "chosen-detail"} {
			for _, stamp := range []string{"", "chosen-stamp"} {
				c := CandidateView{Status: store.StatusActive, Website: state, WebsiteChecked: stamp, WebsiteCheckedText: stamp}
				if state == sources.WebsiteNone {
					c.WebsiteWhy = detail
				} else {
					c.WebsiteTitle = detail
				}
				if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "candidate", CandidateData{Candidate: c}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

// R-03T7-3NW6 R-8QIQ-63EB
func TestUnfoundCandidateAndMemberWithoutJudging(t *testing.T) {
	for _, emptyURL := range []bool{false, true} {
		cfg, handle := pageFixture(t)
		now := fixtureTime()
		cfg.Store = store.New(handle, store.Config{Now: func() time.Time { return now }, Rand: &sequence{}})
		_, ids, err := cfg.Store.Attach(t.Context(), "chosen-run", false, []store.Observation{{Scraper: "fixture", SourceID: "chosen-row", Digest: "chosen-digest", Reading: sources.Reading{Name: "chosen-business", NameKey: "chosen-name"}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := judgePageFixture(t.Context(), cfg.Store); err != nil {
			t.Fatal(err)
		}
		pl, err := cfg.Store.CreateProspectList(t.Context(), store.ProspectList{Name: "chosen-list", Size: 10, CreatedUserID: "owner"})
		if err != nil {
			t.Fatal(err)
		}
		id := ids[0]
		check := func(want CandidateView, count int) {
			t.Helper()
			got := capturedCandidate(t, cfg, id)
			c := got.Candidate
			if c.WebsiteSource != want.WebsiteSource || c.Website != want.Website || c.WebsiteURL != want.WebsiteURL || c.WebsiteTitle != want.WebsiteTitle || c.WebsiteWhy != want.WebsiteWhy || c.WebsiteChecked != want.WebsiteChecked || c.WebsiteCheckedText != want.WebsiteCheckedText || len(got.EnrichmentResults) != count {
				t.Fatal(got)
			}
			members := pageProspectList(t, cfg, pl.ID).Members
			if len(members) != 1 || members[0].Website != want.Website || members[0].WebsiteURL != want.WebsiteURL || members[0].WebsiteSource != want.WebsiteSource {
				t.Fatal(members)
			}
		}
		record := func(kind string, found any) {
			t.Helper()
			if _, err := cfg.Store.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: kind, Inputs: "chosen-input", Source: "chosen-source", Found: encodedJSON(found)}}); err != nil {
				t.Fatal(err)
			}
		}
		check(CandidateView{Website: sources.WebsiteUnknown}, 0)
		record(sources.WebsiteCheck, website.Result{State: sources.WebsiteNone, Title: "chosen-check-title"})
		check(CandidateView{Website: sources.WebsiteUnknown, WebsiteTitle: "chosen-check-title", WebsiteChecked: "2026-10-05T09:00:07Z", WebsiteCheckedText: "2026-10-05 09:00:07 UTC"}, 1)
		now = now.Add(time.Hour)
		f := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "chosen-search", Status: enrichers.RunExited, ExitCode: new(0)}, Answer: &enrichers.WebsiteSearchAnswer{URL: "https://unloaded.test/", Why: "chosen-unfound-reason"}, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteNone, Title: "ignored-title"}}
		if emptyURL {
			f.Answer.URL = ""
			f.Confirmation = nil
		}
		record(enrichers.KindWebsiteSearch, f)
		check(CandidateView{Website: sources.WebsiteNone, WebsiteWhy: f.Answer.Why, WebsiteChecked: "2026-10-05T10:00:07Z", WebsiteCheckedText: "2026-10-05 10:00:07 UTC"}, 2)
		got := capturedCandidate(t, cfg, id)
		if got.EnrichmentResults[0].Kind != enrichers.KindWebsiteSearch || got.EnrichmentResults[0].Failed || got.EnrichmentResults[0].Run.ID != f.Run.ID || got.EnrichmentResults[1].State != sources.WebsiteNone {
			t.Fatal(got.EnrichmentResults)
		}
	}
}
