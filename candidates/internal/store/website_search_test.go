package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func searchResult(t *testing.T, id, state, url string) store.Enrichment {
	t.Helper()
	zero := 0
	found := enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "search-run", Status: enrichers.RunExited, ExitCode: &zero}, Answer: &enrichers.WebsiteSearchAnswer{URL: url, Why: "owner's business", Rejected: []enrichers.WebsiteSearchSite{}}, Confirmation: &enrichers.WebsiteSearchConfirmation{State: state, URL: url, Title: "confirmed title", StatusCode: 200, Size: 10}}
	if url == "" {
		found.Confirmation = nil
	}
	b, err := json.Marshal(found)
	contractOK(t, err)
	return store.Enrichment{Candidate: id, Kind: enrichers.KindWebsiteSearch, Inputs: "name-city-state", Source: enrichers.SourcePrompts, Found: b}
}

// R-X36J-5G5F R-QJ7S-Y7FR R-AZZS-ERF4 R-HS56-N73R R-BC6S-8GU2
func TestWebsiteDecisionKeepsConfirmedSearchAndPrefersWorkingCheck(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	_, err := f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	k := prospectListCreate(t, f, "website decision")
	assert := func(state, url, source string) {
		t.Helper()
		facts, e := f.s.GetFacts(ctx, id)
		contractOK(t, e)
		contractEqual(t, facts.Website, state)
		contractEqual(t, facts.WebsiteURL, url)
		contractEqual(t, facts.WebsiteSource, source)
		_, e = f.s.GetJudgments(ctx, id)
		contractOK(t, e)
		contractEqual(t, factsAt(ctx, t, f.s, id).WebsiteSource, source)
		contractEqual(t, factsAt(ctx, t, f.s, id).WebsiteURL, url)
		read := prospectListRead(t, f, k)
		contractEqual(t, len(read.Members), 1)
		contractEqual(t, read.Members[0].Facts.WebsiteSource, source)
		f.reopen()
		kept, e := f.s.GetFacts(ctx, id)
		contractOK(t, e)
		contractEqual(t, kept, facts)
	}
	add := func(e store.Enrichment) {
		t.Helper()
		f.now = f.now.Add(time.Second)
		_, x := f.s.AddEnrichments(ctx, []store.Enrichment{e})
		contractOK(t, x)
	}
	add(searchResult(t, id, sources.WebsiteHTTPS, "https://found.test/"))
	assert(sources.WebsiteHTTPS, "https://found.test/", store.WebsiteBySearch)
	failed := searchResult(t, id, "", "")
	var failedFound enrichers.WebsiteSearchFound
	contractOK(t, json.Unmarshal(failed.Found, &failedFound))
	failedFound.Run.Status = "failed"
	failedFound.Run.ExitCode = nil
	failed.Found, err = json.Marshal(failedFound)
	contractOK(t, err)
	for _, e := range []store.Enrichment{searchResult(t, id, sources.WebsiteNone, "https://unresponsive.test/"), searchResult(t, id, "", ""), failed} {
		add(e)
		assert(sources.WebsiteHTTPS, "https://found.test/", store.WebsiteBySearch)
		facts, x := f.s.GetFacts(ctx, id)
		contractOK(t, x)
		contractEqual(t, facts.Written, []store.Written{{Kind: enrichers.KindWebsiteSearch, At: f.now.UTC().Truncate(time.Second)}})
	}
	add(websiteResult(id, sources.WebsiteNone))
	assert(sources.WebsiteHTTPS, "https://found.test/", store.WebsiteBySearch)
	add(websiteResult(id, sources.WebsiteHTTP))
	assert(sources.WebsiteHTTP, websiteDecoded(websiteResult(id, sources.WebsiteHTTP)).URL, store.WebsiteByCheck)
	add(searchResult(t, id, sources.WebsiteHTTPS, "https://later-search.test/"))
	assert(sources.WebsiteHTTP, websiteDecoded(websiteResult(id, sources.WebsiteHTTP)).URL, store.WebsiteByCheck)
	// A non-working newest check permits the newest confirmed search again.
	add(websiteResult(id, sources.WebsiteNone))
	assert(sources.WebsiteHTTPS, "https://later-search.test/", store.WebsiteBySearch)
}

// R-ARGH-QD89
func TestSubjectWebsiteSearchSupplement(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := enrichmentCandidate(t, f, "subject-search", "office@business.test")
	c, err := f.s.GetCandidate(ctx, id)
	contractOK(t, err)
	before, err := f.s.Subject(ctx, id)
	contractOK(t, err)
	contractEqual(t, before.Street, c.Street)
	contractEqual(t, before.Phone, c.Phone)
	contractEqual(t, before.WebsiteSource, "")
	contractEqual(t, before.Check, (*website.Result)(nil))
	own := website.Result{State: sources.WebsiteNone, URL: "https://business.test/", Title: "own domain check"}
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "business.test", Source: "business.test", Found: websiteFound(own)}, {Candidate: id, Kind: sources.WebsiteCheck, Inputs: "different.test", Source: "different.test", Found: websiteFound(website.Result{State: sources.WebsiteNone})}, searchResult(t, id, sources.WebsiteHTTPS, "https://resolved.test/")})
	contractOK(t, err)
	subject, err := f.s.Subject(ctx, id)
	contractOK(t, err)
	contractEqual(t, subject.Check, &own)
	contractEqual(t, subject.WebsiteSource, store.WebsiteBySearch)
	contractEqual(t, subject.Website, "https://resolved.test/")
	consumer := enrichmentCandidate(t, f, "consumer-email", "office@gmail.com")
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: consumer, Kind: sources.WebsiteCheck, Inputs: "gmail.com", Source: "gmail.com", Found: websiteFound(own)}})
	contractOK(t, err)
	subject, err = f.s.Subject(ctx, consumer)
	contractOK(t, err)
	contractEqual(t, subject.Check, (*website.Result)(nil))
}

// R-NJBV-4XYY R-5J4Y-DB63 R-NEG9-LV06 R-NLRN-WHGC
func TestSearchWebsiteDoesNotInflateOperatingConfidence(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.now = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	o := contractObservation("search-score", "Search business", time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	o.Scraper = fmcsa.Name
	o.Reading.Estimate = nil
	active := sources.Evidence{Name: sources.EvidenceFMCSAActive, Source: fmcsa.Name, Weight: .6}
	domain := sources.Evidence{Name: sources.EvidenceOwnDomain, Source: fmcsa.Name, Weight: .2}
	o.Reading.Evidence = []sources.Evidence{active, domain}
	id := contractAttach(t, f, "run", o)[0]
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{searchResult(t, id, sources.WebsiteHTTPS, "https://found.test/")})
	contractOK(t, err)
	assert := func(source string, operating float64, evidence []sources.Evidence) {
		t.Helper()
		_, e := f.s.Judge(ctx, []string{id})
		contractOK(t, e)
		a, e := f.s.GetJudgments(ctx, id)
		contractOK(t, e)
		near(t, a.Operating, operating)
		contractEqual(t, a.Evidence, evidence)
		contractEqual(t, factsAt(ctx, t, f.s, id).WebsiteSource, source)
	}
	assert(store.WebsiteBySearch, .68, []sources.Evidence{active, domain})
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO enrichments(candidate,kind,inputs,found,source,at,cost) VALUES (?,?,?,?,?,?,0)`, id, sources.WebsiteCheck, "raw.test", string(websiteFound(website.Result{State: sources.WebsiteHTTP, URL: "http://raw.test/"})), "raw.test", f.now.Format(time.RFC3339))
		return e
	}))
	assert(store.WebsiteBySearch, .68, []sources.Evidence{active, domain})
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(id, sources.WebsiteHTTP)})
	contractOK(t, err)
	live := sources.Evidence{Name: sources.EvidenceWebsiteLive, Source: sources.WebsiteCheck, Weight: .5, Description: sources.DescribeWebsiteLive}
	assert(store.WebsiteByCheck, .84, []sources.Evidence{active, live, domain})
	legacy := contractAttach(t, f, "run", func() store.Observation {
		v := o
		v.SourceID = "legacy"
		v.Reading.Name = "Legacy business"
		v.Reading.NameKey = "LEGACY BUSINESS"
		return v
	}())[0]
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO facts(candidate,website,website_url) VALUES (?,'https','https://legacy.test/')`, legacy)
		return e
	}))
	_, err = f.s.Judge(ctx, []string{legacy})
	contractOK(t, err)
	a, err := f.s.GetJudgments(ctx, legacy)
	contractOK(t, err)
	near(t, a.Operating, .84)
	contractEqual(t, a.Evidence, []sources.Evidence{active, live, domain})
	contractEqual(t, factsAt(ctx, t, f.s, legacy).WebsiteSource, "")
}

// R-AZZS-ERF4 R-PWRA-LW9H R-NKJR-IPPN
func TestSearchFactsFillMergeAndSplit(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	original := enrichmentCandidate(t, f, "search-original", "office@original.test")
	other := enrichmentCandidate(t, f, "search-other", "office@other.test")
	source := searchResult(t, other, sources.WebsiteHTTPS, "https://moved.test/")
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{source})
	contractOK(t, err)
	before, err := f.s.GetFacts(ctx, other)
	contractOK(t, err)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `DELETE FROM facts WHERE candidate=?`, other)
		return e
	}))
	_, err = f.s.GetFacts(ctx, other)
	contractError(t, err, store.ErrNotFound)
	contractOK(t, fillFactsWalk(ctx, f.s))
	filled, err := f.s.GetFacts(ctx, other)
	contractOK(t, err)
	contractEqual(t, filled, before)
	contractOK(t, fillFactsWalk(ctx, f.s))
	filled, err = f.s.GetFacts(ctx, other)
	contractOK(t, err)
	contractEqual(t, filled, before)
	_, err = f.s.Judge(ctx, []string{other})
	contractOK(t, err)
	old, err := f.s.GetJudgments(ctx, other)
	contractOK(t, err)
	links, err := f.s.Links(ctx, other)
	contractOK(t, err)
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: original, Other: other, Why: "same business", UserID: "owner"})
	contractOK(t, err)
	moved, err := f.s.GetFacts(ctx, original)
	contractOK(t, err)
	contractEqual(t, moved.WebsiteSource, store.WebsiteBySearch)
	contractEqual(t, moved.WebsiteURL, "https://moved.test/")
	merged, err := f.s.GetJudgments(ctx, other)
	contractOK(t, err)
	contractEqual(t, merged, old)
	a, err := f.s.GetJudgments(ctx, original)
	contractOK(t, err)
	contractEqual(t, factsAt(ctx, t, f.s, original).WebsiteSource, store.WebsiteBySearch)
	for _, e := range a.Evidence {
		if e.Name == sources.EvidenceWebsiteLive {
			t.Fatal("search added live website evidence")
		}
	}
	split, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindSplit, Candidate: original, Keys: []string{sources.SourceKey(links[0].Scraper, links[0].SourceID)}, Why: "separate business", UserID: "owner"})
	contractOK(t, err)
	kept, err := f.s.GetFacts(ctx, original)
	contractOK(t, err)
	contractEqual(t, kept.WebsiteSource, store.WebsiteBySearch)
	contractEqual(t, kept.WebsiteURL, moved.WebsiteURL)
	fresh, err := f.s.GetFacts(ctx, split.Other)
	contractOK(t, err)
	contractEqual(t, fresh.Website, sources.WebsiteUnknown)
	contractEqual(t, fresh.WebsiteSource, "")
	es, err := f.s.Enrichments(ctx, split.Other)
	contractOK(t, err)
	for _, e := range es {
		if e.Kind == enrichers.KindWebsiteSearch {
			t.Fatal("split moved search result")
		}
	}
	f.reopen()
	kept, err = f.s.GetFacts(ctx, original)
	contractOK(t, err)
	contractEqual(t, kept.WebsiteSource, store.WebsiteBySearch)
}

// R-X36J-5G5F
func TestWebsiteSourceProtocolWords(t *testing.T) {
	type word string
	const check word = store.WebsiteByCheck
	const search word = store.WebsiteBySearch
	contractEqual(t, check, word("check"))
	contractEqual(t, search, word("search"))
}

// R-QJ7S-Y7FR R-AZZS-ERF4 R-HS56-N73R R-BC6S-8GU2
func TestWebsiteUnknownAndUnfoundSearchProjection(t *testing.T) {
	for _, noSite := range []string{"empty_answer", "failed_confirmation"} {
		t.Run(noSite, func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			id := contractSeed(t, f)
			_, err := f.s.Judge(ctx, []string{id})
			contractOK(t, err)
			list := prospectListCreate(t, f, "search outcome")
			add := func(e store.Enrichment, state, url, source string) {
				t.Helper()
				f.now = f.now.Add(time.Second)
				_, eErr := f.s.AddEnrichments(ctx, []store.Enrichment{e})
				contractOK(t, eErr)
				facts, eErr := f.s.GetFacts(ctx, id)
				contractOK(t, eErr)
				contractEqual(t, facts.Website, state)
				contractEqual(t, facts.WebsiteURL, url)
				contractEqual(t, facts.WebsiteSource, source)
				for _, written := range facts.Written {
					if written.Kind == e.Kind {
						contractEqual(t, written.At, f.now.UTC().Truncate(time.Second))
					}
				}
				contractEqual(t, prospectListRead(t, f, list).Members[0].Facts, facts)
				f.reopen()
				kept, eErr := f.s.GetFacts(ctx, id)
				contractOK(t, eErr)
				contractEqual(t, kept, facts)
			}
			add(websiteResult(id, sources.WebsiteNone), sources.WebsiteUnknown, "", "")
			unfound := searchResult(t, id, "", "")
			if noSite == "failed_confirmation" {
				unfound = searchResult(t, id, sources.WebsiteNone, "https://unreachable.test/")
			}
			add(unfound, sources.WebsiteNone, "", "")
			add(websiteResult(id, sources.WebsiteNone), sources.WebsiteNone, "", "")
			for _, failed := range []bool{true, false} {
				attempt := searchResult(t, id, sources.WebsiteHTTPS, "https://unconfirmed.test/")
				var found enrichers.WebsiteSearchFound
				contractOK(t, json.Unmarshal(attempt.Found, &found))
				found.Confirmation = nil
				if failed {
					found.Run.Status = "failed"
					found.Run.ExitCode = nil
				}
				attempt.Found, err = json.Marshal(found)
				contractOK(t, err)
				add(attempt, sources.WebsiteNone, "", "")
			}
			working := websiteResult(id, sources.WebsiteHTTP)
			add(working, sources.WebsiteHTTP, websiteDecoded(working).URL, store.WebsiteByCheck)
			add(unfound, sources.WebsiteHTTP, websiteDecoded(working).URL, store.WebsiteByCheck)
			// An older working check no longer decides once the newest check fails.
			add(websiteResult(id, sources.WebsiteNone), sources.WebsiteNone, "", "")
			add(searchResult(t, id, sources.WebsiteHTTP, "http://first-found.test/"), sources.WebsiteHTTP, "http://first-found.test/", store.WebsiteBySearch)
			add(searchResult(t, id, sources.WebsiteHTTPS, "https://newest-found.test/"), sources.WebsiteHTTPS, "https://newest-found.test/", store.WebsiteBySearch)
			add(unfound, sources.WebsiteHTTPS, "https://newest-found.test/", store.WebsiteBySearch)
		})
	}
}
