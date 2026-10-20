package pages

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-DUEN-N7F8 R-DVMK-0Z5X R-DWUG-EQWM R-DY2C-SINB R-DZA9-6AE0 R-E2XY-BLM3 R-E45U-PDCS R-NDOT-KDNW
func TestSearchEnrichmentProjection(t *testing.T) {
	zero, one := 0, 1
	full := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "r1", Status: enrichers.RunExited, ExitCode: &zero}, Answer: &enrichers.WebsiteSearchAnswer{URL: "https://acme.test/", Why: "chosen-why", Rejected: []enrichers.WebsiteSearchSite{{URL: "https://other.test/", Why: "chosen-rejection"}, {URL: "https://third.test/"}}}, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://www.acme.test/", Title: "chosen-title", StatusCode: 200, Size: 1256}}
	for _, f := range []enrichers.WebsiteSearchFound{full, {Run: full.Run}, {Run: full.Run, Answer: &enrichers.WebsiteSearchAnswer{}}, {Run: full.Run, Answer: full.Answer, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteNone, Error: "chosen-error"}}, {Run: enrichers.WebsiteSearchRun{Status: "failed"}}, {Run: enrichers.WebsiteSearchRun{Status: enrichers.RunExited, ExitCode: &one}, Answer: full.Answer}, {Run: enrichers.WebsiteSearchRun{Status: "killed"}, Answer: full.Answer}} {
		x := store.Enrichment{Kind: enrichers.KindWebsiteSearch, Source: enrichers.SourcePrompts, Found: encodedJSON(f), Cost: .023714, At: fixtureTime()}
		got := enrichmentRow(x)
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
		got := enrichmentRow(store.Enrichment{Kind: kind, Found: json.RawMessage(`{}`)})
		if got.Run != nil || got.Answer != nil || got.Confirmation != nil {
			t.Fatal(got)
		}
	}
}

// R-N55I-VZH1 R-E1Q1-XTVE R-NB90-SU6I R-EBH8-ZZSY R-E0I5-K24P
func TestSearchWebsiteCandidateAndMember(t *testing.T) {
	cfg, _ := pageFixture(t)
	_, ids, err := cfg.Store.Attach(t.Context(), "run", false, []store.Observation{{Scraper: "fixture", SourceID: "row", Digest: "digest", Reading: sources.Reading{Name: "search-business", NameKey: "search-business"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := ids[0]
	if _, err = cfg.Store.JudgeAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	co, err := cfg.Store.CreateCohort(t.Context(), store.Cohort{Name: "search-cohort", Size: 10, CreatedUserID: "owner"})
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
	if len(got.Enrichments) != 2 || got.Enrichments[0].Run.ID != f.Run.ID || got.Enrichments[0].Failed || got.Facts == nil {
		t.Fatal(got)
	}
	written := false
	for _, w := range got.Facts.Written {
		if w.Kind == enrichers.KindWebsiteSearch {
			written = true
		}
	}
	if !written {
		t.Fatal(got.Facts)
	}
	checkMember := func(source, state, url string) {
		t.Helper()
		view := pageCohort(t, cfg, co.ID)
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

// R-EF4Y-5B11
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
				d := CandidateData{Candidate: CandidateView{Status: store.StatusActive, WebsiteSource: source, Website: state, WebsiteURL: text, WebsiteTitle: text, WebsiteWhy: text, WebsiteChecked: text, WebsiteCheckedText: text}, Facts: &FactsView{Written: []WrittenRow{{Kind: enrichers.KindWebsiteSearch}}}}
				for _, failed := range []bool{false, true} {
					for n := 0; n < 3; n++ {
						r := EnrichmentRow{Kind: enrichers.KindWebsiteSearch, Found: []FactRow{}, Failed: failed, Cost: text, Run: &RunView{ID: text, Status: text, ExitCode: text}}
						render("candidate", CandidateData{Candidate: d.Candidate, Enrichments: []EnrichmentRow{r}})
						r.Answer = &AnswerView{URL: text, Why: text, Rejected: []RejectedRow{}}
						for i := 0; i < n; i++ {
							r.Answer.Rejected = append(r.Answer.Rejected, RejectedRow{URL: text, Why: text})
						}
						d.Enrichments = []EnrichmentRow{r}
						render("candidate", d)
						r.Confirmation = &ConfirmationView{State: state, URL: text, Title: text, StatusCode: text, Size: 1256, Error: text}
						d.Enrichments = []EnrichmentRow{r, {Kind: enrichers.KindLinkFacts}, {Kind: sources.WebsiteCheck}}
						render("candidate", d)
					}
				}
				render("cohort", CohortData{Members: []MemberRow{{WebsiteSource: source, Website: state, WebsiteURL: text}}})
			}
		}
	}
	render("pass", PassData{Pass: PassView{Kind: store.KindEnrichCohort, EnrichKind: enrichers.KindWebsiteSearch, Runs: 2, RunsFailed: 1, Cost: "$0.023714"}})
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

// R-E1Q1-XTVE
func TestWebsiteSearchDecisionHistory(t *testing.T) {
	zero := 0
	good := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{Status: enrichers.RunExited, ExitCode: &zero}, Answer: &enrichers.WebsiteSearchAnswer{URL: "https://answer.test/", Why: "chosen-why"}, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://confirmed.test/", Title: "chosen-title"}}
	search := store.Enrichment{Kind: enrichers.KindWebsiteSearch, Found: encodedJSON(good), At: fixtureTime()}
	for _, bad := range []enrichers.WebsiteSearchFound{{Run: enrichers.WebsiteSearchRun{Status: "failed"}, Answer: good.Answer, Confirmation: good.Confirmation}, {Run: good.Run, Answer: good.Answer, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteNone}}, {Run: good.Run, Answer: &enrichers.WebsiteSearchAnswer{}}} {
		var got CandidateView
		setWebsite(&got, []store.Enrichment{{Kind: enrichers.KindWebsiteSearch, Found: encodedJSON(bad), At: fixtureTime().Add(1)}, search})
		if got.WebsiteSource != store.WebsiteBySearch || got.Website != sources.WebsiteHTTPS || got.WebsiteURL != good.Confirmation.URL || got.WebsiteTitle != good.Confirmation.Title || got.WebsiteWhy != good.Answer.Why || got.WebsiteChecked != "2026-10-05T09:00:07Z" || got.WebsiteCheckedText != "2026-10-05 09:00:07 UTC" {
			t.Fatal(got)
		}
	}
	check := store.Enrichment{Kind: sources.WebsiteCheck, Found: encodedJSON(website.Result{State: sources.WebsiteHTTP, URL: "http://check.test/", Title: "check-title"}), At: fixtureTime()}
	for _, es := range [][]store.Enrichment{{search, check}, {check, search}} {
		var got CandidateView
		setWebsite(&got, es)
		if got.WebsiteSource != store.WebsiteByCheck || got.Website != sources.WebsiteHTTP || got.WebsiteURL != "http://check.test/" || got.WebsiteTitle != "check-title" || got.WebsiteWhy != "" {
			t.Fatal(got)
		}
	}
}

// R-N55I-VZH1 R-E0I5-K24P
func TestCandidateAndMemberViewShapes(_ *testing.T) {
	_ = struct {
		Banner      page.Banner
		Candidate   CandidateView
		Facts       *FactsView
		Brief       *BriefView
		Assessment  *AssessmentView
		Estimates   []EstimateView
		Links       []LinkView
		Rulings     []RulingView
		Pages       []PageRow
		Enrichments []EnrichmentRow
		Rejects     []RejectRecordRow
		Campaigns   []CampaignRow
	}(CandidateData{})
	_ = struct {
		ID, Status, MergedInto, Name                                                                                                                     string
		Names                                                                                                                                            []string
		Street, City, State, Zip, County, Phone, Email, WebsiteSource, Website, WebsiteURL, WebsiteTitle, WebsiteWhy, WebsiteChecked, WebsiteCheckedText string
		People                                                                                                                                           []PersonRow
		Class                                                                                                                                            string
		Cargo                                                                                                                                            []string
		Created, CreatedText, Updated, UpdatedText                                                                                                       string
	}(CandidateView{})
	_ = struct {
		Number, Path, URL, Title       string
		Size                           int64
		Fetched, FetchedText, By, User string
	}(PageRow{})
	_ = struct{ Kind, Cohort, CohortName, By, User, At, AtText, Why string }(RejectRecordRow{})
	_ = struct {
		Rank                                                                                                                                                                                                  int64
		ID, Name, City, State, OperatingConfidence, RevenueLow, RevenueHigh, RevenueConfidence                                                                                                                string
		Categories                                                                                                                                                                                            []string
		ClassifiedBy, Class, FleetVerdict, Website, WebsiteURL, WebsiteSource, SiteText, SiteTextText, Brief, BriefText, Minutes, GovernmentMatch, KnownSince, KnownSinceText, NewestFiling, NewestFilingText string
		Excluded                                                                                                                                                                                              bool
	}(MemberRow{})
	_ = struct{ Candidate, By, User, At, AtText, Why string }(RejectRow{})
}
