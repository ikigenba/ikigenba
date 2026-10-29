package pages

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-U9HJ-NE5T
func TestSearchSiteTextViewShape(_ *testing.T) {
	_ = struct {
		Status string
		Pages  int64
	}(SearchSiteTextView{})
}

// R-0KVS-GG9W
func TestSearchContextProjection(t *testing.T) {
	good := enrichers.WebsiteSearchRun{ID: "context-run", Status: enrichers.RunExited, ExitCode: new(0)}
	for _, tc := range []struct {
		name   string
		found  enrichers.WebsiteSearchFound
		reason string
		site   string
		from   string
		text   *SearchSiteTextView
		result string
	}{
		{"legacy", enrichers.WebsiteSearchFound{Run: good, Answer: &enrichers.WebsiteSearchAnswer{URL: "https://chosen.test/"}}, "", "", "", nil, ""},
		{"different", enrichers.WebsiteSearchFound{Run: good, Answer: &enrichers.WebsiteSearchAnswer{URL: "https://chosen.test/"}, Reason: enrichers.SearchReasonEmptySite, Website: "https://old.test/", WebsiteFrom: store.WebsiteByCheck, SiteText: &enrichers.WebsiteSearchSiteText{Status: "failed", Pages: 0}, Case: enrichers.SearchCaseDifferent}, enrichers.SearchReasonEmptySite, "https://old.test/", store.WebsiteByCheck, &SearchSiteTextView{Status: "failed", Pages: 0}, enrichers.SearchCaseDifferent},
		{"stands", enrichers.WebsiteSearchFound{Run: good, Answer: &enrichers.WebsiteSearchAnswer{URL: "https://chosen.test/"}, Reason: enrichers.SearchReasonAgain, Website: "https://chosen.test/", WebsiteFrom: store.WebsiteBySearch, SiteText: &enrichers.WebsiteSearchSiteText{Status: enrichers.RunExited, Pages: 4}, Case: enrichers.SearchCaseStands}, enrichers.SearchReasonAgain, "https://chosen.test/", store.WebsiteBySearch, &SearchSiteTextView{Status: enrichers.RunExited, Pages: 4}, enrichers.SearchCaseStands},
		{"none", enrichers.WebsiteSearchFound{Run: good, Answer: &enrichers.WebsiteSearchAnswer{}, Reason: enrichers.SearchReasonNoWebsite, Case: enrichers.SearchCaseNone}, enrichers.SearchReasonNoWebsite, "", "", nil, enrichers.SearchCaseNone},
		{"failed", enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "failed-run", Status: "failed"}, Reason: enrichers.SearchReasonEmptySite, Website: "https://chosen.test/", WebsiteFrom: store.WebsiteBySearch, SiteText: &enrichers.WebsiteSearchSiteText{Status: enrichers.RunExited, Pages: 0}}, enrichers.SearchReasonEmptySite, "https://chosen.test/", store.WebsiteBySearch, &SearchSiteTextView{Status: enrichers.RunExited, Pages: 0}, ""},
		{"again without text", enrichers.WebsiteSearchFound{Run: good, Answer: &enrichers.WebsiteSearchAnswer{}, Reason: enrichers.SearchReasonAgain, Website: "https://chosen.test/", WebsiteFrom: store.WebsiteByCheck, Case: enrichers.SearchCaseNone}, enrichers.SearchReasonAgain, "https://chosen.test/", store.WebsiteByCheck, nil, enrichers.SearchCaseNone},
		{"again with zero pages", enrichers.WebsiteSearchFound{Run: good, Answer: &enrichers.WebsiteSearchAnswer{}, Reason: enrichers.SearchReasonAgain, Website: "https://chosen.test/", WebsiteFrom: store.WebsiteByCheck, SiteText: &enrichers.WebsiteSearchSiteText{Status: "failed", Pages: 0}, Case: enrichers.SearchCaseNone}, enrichers.SearchReasonAgain, "https://chosen.test/", store.WebsiteByCheck, &SearchSiteTextView{Status: "failed", Pages: 0}, enrichers.SearchCaseNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := enrichmentRow(store.Enrichment{Kind: enrichers.KindWebsiteSearch, Found: encodedJSON(tc.found)}, enrichers.Subject{})
			if r.Reason != tc.reason || r.Website != tc.site || r.WebsiteFrom != tc.from || !reflect.DeepEqual(r.SiteText, tc.text) || r.Case != tc.result {
				t.Fatal(r, tc)
			}
			for _, kind := range []string{sources.WebsiteCheck, enrichers.KindSiteText, enrichers.KindDriveRoute} {
				r = enrichmentRow(store.Enrichment{Kind: kind, Found: encodedJSON(tc.found)}, enrichers.Subject{})
				if r.Reason != "" || r.Website != "" || r.WebsiteFrom != "" || r.SiteText != nil || r.Case != "" {
					t.Fatal(kind, r)
				}
			}
		})
	}
}

// R-R4VC-OMK6
func TestSearchContextTemplateStates(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"", enrichers.SearchReasonNoWebsite, enrichers.SearchReasonEmptySite, enrichers.SearchReasonAgain} {
		for _, site := range []string{"", "https://recorded.test/"} {
			for _, from := range []string{"", store.WebsiteByCheck, store.WebsiteBySearch} {
				for _, text := range []*SearchSiteTextView{nil, {Status: "failed", Pages: 0}, {Status: enrichers.RunExited, Pages: 4}} {
					if reason == enrichers.SearchReasonEmptySite && text != nil && text.Pages != 0 {
						continue
					}
					for _, result := range []string{"", enrichers.SearchCaseDifferent, enrichers.SearchCaseStands, enrichers.SearchCaseNone} {
						for _, answer := range []*AnswerView{nil, {}, {URL: "https://answer.test/"}} {
							d := CandidateData{Candidate: CandidateView{Status: store.StatusActive}, EnrichmentResults: []EnrichmentRow{{Kind: enrichers.KindWebsiteSearch, Reason: reason, Website: site, WebsiteFrom: from, SiteText: text, Case: result, Answer: answer}, {Kind: sources.WebsiteCheck}, {Kind: enrichers.KindSiteText}, {Kind: enrichers.KindDriveRoute}}}
							if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "candidate", d); err != nil {
								t.Fatal(reason, site, from, text, result, answer, err)
							}
						}
					}
				}
			}
		}
	}
}

// R-02LA-PW5H
func TestTakeoverWebsiteFields(t *testing.T) {
	at := fixtureTime()
	check := store.Enrichment{Kind: sources.WebsiteCheck, At: at.Add(time.Hour), Found: encodedJSON(website.Result{State: sources.WebsiteHTTPS, URL: "https://recorded.test/", Title: "check-title"})}
	good := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "takeover-run", Status: enrichers.RunExited, ExitCode: new(0)}, Answer: &enrichers.WebsiteSearchAnswer{URL: "https://answer.test/", Why: "chosen-why"}, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTP, URL: "http://answer.test/", Title: "answer-title"}, Reason: enrichers.SearchReasonEmptySite, Website: "https://recorded.test/", WebsiteFrom: store.WebsiteByCheck, SiteText: &enrichers.WebsiteSearchSiteText{Status: "failed", Pages: 0}, Case: enrichers.SearchCaseDifferent}
	for _, tc := range []struct {
		name        string
		found       enrichers.WebsiteSearchFound
		checkURL    string
		from, state string
		url, title  string
		why         string
		when        time.Time
	}{
		{"takes over same site", good, "https://recorded.test/", store.WebsiteBySearch, sources.WebsiteHTTP, "http://answer.test/", "answer-title", "chosen-why", at},
		{"new check site wins", good, "https://changed.test/", store.WebsiteByCheck, sources.WebsiteHTTPS, "https://changed.test/", "check-title", "", check.At},
		{"same site with different URL bytes takes over", good, "https://www.recorded.test/", store.WebsiteBySearch, sources.WebsiteHTTP, "http://answer.test/", "answer-title", "chosen-why", at},
		{"normalized check site takes over", good, "http://WWW.Recorded.test.:8080/login?token=2", store.WebsiteBySearch, sources.WebsiteHTTP, "http://answer.test/", "answer-title", "chosen-why", at},
		{"normalized search context takes over", func() enrichers.WebsiteSearchFound {
			f := good
			f.Website = "WWW.Recorded.test./home?token=2"
			return f
		}(), "https://recorded.test/", store.WebsiteBySearch, sources.WebsiteHTTP, "http://answer.test/", "answer-title", "chosen-why", at},
		{"search context takes over independently", func() enrichers.WebsiteSearchFound {
			f := good
			f.Website = "https://previous-search.test/"
			f.WebsiteFrom = store.WebsiteBySearch
			return f
		}(), "https://recorded.test/", store.WebsiteBySearch, sources.WebsiteHTTP, "http://answer.test/", "answer-title", "chosen-why", at},
		{"case does not override confirmed cross-site result", func() enrichers.WebsiteSearchFound { f := good; f.Case = enrichers.SearchCaseStands; return f }(), "https://recorded.test/", store.WebsiteBySearch, sources.WebsiteHTTP, "http://answer.test/", "answer-title", "chosen-why", at},
		{"different context is inert", func() enrichers.WebsiteSearchFound { f := good; f.Website = "https://unrelated.test/"; return f }(), "https://recorded.test/", store.WebsiteByCheck, sources.WebsiteHTTPS, "https://recorded.test/", "check-title", "", check.At},
		{"failed is inert", func() enrichers.WebsiteSearchFound { f := good; f.Run.Status = "failed"; f.Case = ""; return f }(), "https://recorded.test/", store.WebsiteByCheck, sources.WebsiteHTTPS, "https://recorded.test/", "check-title", "", check.At},
		{"standing site is inert", func() enrichers.WebsiteSearchFound {
			f := good
			f.Case = enrichers.SearchCaseStands
			f.Answer = &enrichers.WebsiteSearchAnswer{URL: "https://recorded.test/"}
			f.Confirmation = &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTP, URL: "http://recorded.test/"}
			return f
		}(), "https://recorded.test/", store.WebsiteByCheck, sources.WebsiteHTTPS, "https://recorded.test/", "check-title", "", check.At},
		{"confirmation returns to normalized context", func() enrichers.WebsiteSearchFound {
			f := good
			f.Confirmation = &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://WWW.Recorded.test./login?token=2"}
			return f
		}(), "https://recorded.test/", store.WebsiteByCheck, sources.WebsiteHTTPS, "https://recorded.test/", "check-title", "", check.At},
		{"unconfirmed cross-site answer is inert", func() enrichers.WebsiteSearchFound {
			f := good
			f.Confirmation = &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteNone, URL: "https://answer.test/"}
			return f
		}(), "https://recorded.test/", store.WebsiteByCheck, sources.WebsiteHTTPS, "https://recorded.test/", "check-title", "", check.At},
		{"empty answer is inert", func() enrichers.WebsiteSearchFound {
			f := good
			f.Case = enrichers.SearchCaseNone
			f.Answer = &enrichers.WebsiteSearchAnswer{Why: "no-match"}
			f.Confirmation = nil
			return f
		}(), "https://recorded.test/", store.WebsiteByCheck, sources.WebsiteHTTPS, "https://recorded.test/", "check-title", "", check.At},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check.Found = encodedJSON(website.Result{State: sources.WebsiteHTTPS, URL: tc.checkURL, Title: "check-title"})
			search := store.Enrichment{Kind: enrichers.KindWebsiteSearch, At: at, Found: encodedJSON(tc.found)}
			for _, es := range [][]store.Enrichment{{check, search}, {search, check}} {
				v := CandidateView{WebsiteWhy: "stale", WebsiteURL: "stale"}
				setWebsite(&v, es, "", "")
				if v.WebsiteSource != tc.from || v.Website != tc.state || v.WebsiteURL != tc.url || v.WebsiteTitle != tc.title || v.WebsiteWhy != tc.why || v.WebsiteChecked != tc.when.Format(time.RFC3339) || v.WebsiteCheckedText != tc.when.Format(SecondLayout) {
					t.Fatal(v, tc)
				}
			}
		})
	}
}

// R-03T7-3NW6 R-0KVS-GG9W R-02LA-PW5H
func TestCandidatePageShowsSearchTakeoverContext(t *testing.T) {
	cfg, _ := pageFixture(t)
	_, ids, err := cfg.Store.Attach(t.Context(), "context-run", false, []store.Observation{{Scraper: "fixture", SourceID: "context-row", Digest: "context-digest", Reading: sources.Reading{Name: "context-business", NameKey: "context-business"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := ids[0]
	check := store.Enrichment{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "check-input", Source: "recorded.test", Found: encodedJSON(website.Result{State: sources.WebsiteHTTPS, URL: "https://recorded.test/", Title: "check-title"})}
	search := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "search-run", Status: enrichers.RunExited, ExitCode: new(0)}, Answer: &enrichers.WebsiteSearchAnswer{URL: "https://answer.test/", Why: "chosen-why"}, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://answer.test/", Title: "answer-title"}, Reason: enrichers.SearchReasonEmptySite, Website: "https://recorded.test/", WebsiteFrom: store.WebsiteByCheck, SiteText: &enrichers.WebsiteSearchSiteText{Status: enrichers.RunExited, Pages: 0}, Case: enrichers.SearchCaseDifferent}
	if _, err := cfg.Store.AddEnrichments(t.Context(), []store.Enrichment{check, {Candidate: id, Kind: enrichers.KindWebsiteSearch, Inputs: "search-input", Source: enrichers.SourcePrompts, Found: encodedJSON(search)}}); err != nil {
		t.Fatal(err)
	}
	got := capturedCandidate(t, cfg, id)
	if got.Candidate.WebsiteSource != store.WebsiteBySearch || got.Candidate.WebsiteURL != search.Confirmation.URL || got.Candidate.WebsiteTitle != search.Confirmation.Title || got.Candidate.WebsiteWhy != search.Answer.Why {
		t.Fatal(got.Candidate)
	}
	if len(got.EnrichmentResults) != 2 {
		t.Fatal(got.EnrichmentResults)
	}
	r := got.EnrichmentResults[0]
	if r.Kind != enrichers.KindWebsiteSearch || r.Reason != search.Reason || r.Website != search.Website || r.WebsiteFrom != search.WebsiteFrom || r.SiteText == nil || *r.SiteText != (SearchSiteTextView{Status: enrichers.RunExited, Pages: 0}) || r.Case != search.Case {
		t.Fatal(r)
	}
	r = got.EnrichmentResults[1]
	if r.Reason != "" || r.Website != "" || r.WebsiteFrom != "" || r.SiteText != nil || r.Case != "" {
		t.Fatal(r)
	}
	facts, err := cfg.Store.GetFacts(t.Context(), id)
	if err != nil || facts.WebsiteSource != got.Candidate.WebsiteSource || facts.Website != got.Candidate.Website || facts.WebsiteURL != got.Candidate.WebsiteURL {
		t.Fatal(facts, got.Candidate, err)
	}
}

// R-02LA-PW5H R-03T7-3NW6
func TestCandidateAndListFollowSearchTakeoverChain(t *testing.T) {
	cfg, handle := pageFixture(t)
	now := fixtureTime()
	cfg.Store = store.New(handle, store.Config{Now: func() time.Time { return now }, Rand: &sequence{}})
	ctx := t.Context()
	_, ids, err := cfg.Store.Attach(ctx, "chain-run", false, []store.Observation{{Scraper: "fixture", SourceID: "chain-row", Digest: "chain-digest", Reading: sources.Reading{Name: "chain-business", NameKey: "chain-business"}}})
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	id := ids[0]
	if err := judgePageFixture(ctx, cfg.Store); err != nil {
		t.Fatal(err)
	}
	list, err := cfg.Store.CreateProspectList(ctx, store.ProspectList{Name: "chain-list", Size: 10, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	record := func(kind string, found any) {
		t.Helper()
		now = now.Add(time.Minute)
		if _, err := cfg.Store.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: kind, Inputs: "chain-inputs", Source: "chain-source", Found: encodedJSON(found)}}); err != nil {
			t.Fatal(err)
		}
	}
	check := website.Result{State: sources.WebsiteHTTPS, URL: "https://WWW.Recorded.test./home?token=2", Title: "chain-check-title"}
	first := enrichers.WebsiteSearchFound{
		Run:          enrichers.WebsiteSearchRun{ID: "first-chain-search", Status: enrichers.RunExited, ExitCode: new(0)},
		Answer:       &enrichers.WebsiteSearchAnswer{URL: "https://first-search.test/", Why: "first-chain-why"},
		Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://first-search.test/", Title: "first-chain-title"},
		Reason:       enrichers.SearchReasonEmptySite, Website: "http://recorded.test/", WebsiteFrom: store.WebsiteByCheck, SiteText: &enrichers.WebsiteSearchSiteText{Status: enrichers.RunExited}, Case: enrichers.SearchCaseDifferent,
	}
	assertDecision := func(want enrichers.WebsiteSearchFound, at time.Time) {
		t.Helper()
		candidate := capturedCandidate(t, cfg, id).Candidate
		confirmation := want.Confirmation
		if candidate.WebsiteSource != store.WebsiteBySearch || candidate.Website != confirmation.State || candidate.WebsiteURL != confirmation.URL || candidate.WebsiteTitle != confirmation.Title || candidate.WebsiteWhy != want.Answer.Why || candidate.WebsiteChecked != at.Format(time.RFC3339) || candidate.WebsiteCheckedText != at.Format(SecondLayout) {
			t.Fatal(candidate, want, at)
		}
		facts, err := cfg.Store.GetFacts(ctx, id)
		if err != nil || facts.WebsiteSource != candidate.WebsiteSource || facts.Website != candidate.Website || facts.WebsiteURL != candidate.WebsiteURL {
			t.Fatal(facts, candidate, err)
		}
		members := pageProspectList(t, cfg, list.ID).Members
		if len(members) != 1 || members[0].ID != id || members[0].WebsiteSource != candidate.WebsiteSource || members[0].Website != candidate.Website || members[0].WebsiteURL != candidate.WebsiteURL {
			t.Fatal(members, candidate)
		}
	}
	record(sources.WebsiteCheck, check)
	record(enrichers.KindWebsiteSearch, first)
	firstAt := now
	assertDecision(first, firstAt)
	record(sources.WebsiteCheck, check)
	assertDecision(first, firstAt)
	second := first
	second.Run.ID = "second-chain-search"
	second.Reason, second.Website, second.WebsiteFrom = enrichers.SearchReasonAgain, first.Confirmation.URL, store.WebsiteBySearch
	second.Answer = &enrichers.WebsiteSearchAnswer{URL: "https://second-search.test/", Why: "second-chain-why"}
	second.Confirmation = &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTP, URL: "http://second-search.test/", Title: "second-chain-title"}
	record(enrichers.KindWebsiteSearch, second)
	secondAt := now
	assertDecision(second, secondAt)
	record(sources.WebsiteCheck, check)
	assertDecision(second, secondAt)
	for _, outcome := range []string{"stands", "none", "failed", "unconfirmed"} {
		next := second
		next.Run.ID = "later-chain-" + outcome
		next.Website = second.Confirmation.URL
		next.Answer = &enrichers.WebsiteSearchAnswer{URL: "https://third-search.test/", Why: "later-chain-why"}
		switch outcome {
		case "stands":
			next.Confirmation = &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://WWW.Second-search.test./about?token=2", Title: "later-chain-title"}
			next.Case = enrichers.SearchCaseStands
		case "none":
			next.Case, next.Answer, next.Confirmation = enrichers.SearchCaseNone, &enrichers.WebsiteSearchAnswer{Why: "later-none-why"}, nil
		case "failed":
			next.Run.Status, next.Run.ExitCode = "failed", nil
			next.Case = ""
		case "unconfirmed":
			next.Confirmation = &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteNone, URL: "https://third-search.test/"}
		}
		record(enrichers.KindWebsiteSearch, next)
		assertDecision(second, secondAt)
	}
}
