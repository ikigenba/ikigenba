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

// R-ZU1N-7JFF
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

// R-ZWHF-Z2WT
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
		{"same site with different URL bytes is inert", good, "https://www.recorded.test/", store.WebsiteByCheck, sources.WebsiteHTTPS, "https://www.recorded.test/", "check-title", "", check.At},
		{"different context is inert", func() enrichers.WebsiteSearchFound { f := good; f.Website = "https://unrelated.test/"; return f }(), "https://recorded.test/", store.WebsiteByCheck, sources.WebsiteHTTPS, "https://recorded.test/", "check-title", "", check.At},
		{"failed is inert", func() enrichers.WebsiteSearchFound { f := good; f.Run.Status = "failed"; f.Case = ""; return f }(), "https://recorded.test/", store.WebsiteByCheck, sources.WebsiteHTTPS, "https://recorded.test/", "check-title", "", check.At},
		{"standing site is inert", func() enrichers.WebsiteSearchFound {
			f := good
			f.Case = enrichers.SearchCaseStands
			f.Answer = &enrichers.WebsiteSearchAnswer{URL: "https://recorded.test/"}
			f.Confirmation = &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTP, URL: "http://recorded.test/"}
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
				setWebsite(&v, es)
				if v.WebsiteSource != tc.from || v.Website != tc.state || v.WebsiteURL != tc.url || v.WebsiteTitle != tc.title || v.WebsiteWhy != tc.why || v.WebsiteChecked != tc.when.Format(time.RFC3339) || v.WebsiteCheckedText != tc.when.Format(SecondLayout) {
					t.Fatal(v, tc)
				}
			}
		})
	}
}

// R-ZXPC-CUNI R-ZU1N-7JFF R-ZWHF-Z2WT
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
