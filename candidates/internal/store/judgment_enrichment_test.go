package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-UG0J-M2SV R-UCCU-GRKS R-UDKQ-UJBH R-UESN-8B26
func TestJudgmentUsesNewestRecordedWebsiteResult(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.now = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	o := contractObservation("carrier", "Website business", time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	o.Scraper = fmcsa.Name
	o.Reading.Estimate = nil
	active := sources.Evidence{Name: sources.EvidenceFMCSAActive, Source: fmcsa.Name, Weight: .6}
	domain := sources.Evidence{Name: sources.EvidenceOwnDomain, Source: fmcsa.Name, Weight: .2}
	live := sources.Evidence{Name: sources.EvidenceWebsiteLive, Source: sources.WebsiteCheck, Weight: .5, Description: sources.DescribeWebsiteLive}
	o.Reading.Evidence = []sources.Evidence{domain, active}
	id := contractAttach(t, f, "run", o)[0]
	for i, tc := range []struct {
		state, recordedURL, url string
		operating               float64
		evidence                []sources.Evidence
	}{
		{sources.WebsiteUnknown, "", "", .68, []sources.Evidence{active, domain}},
		{sources.WebsiteHTTPS, "https://first.example.test/", "https://first.example.test/", .84, []sources.Evidence{active, live, domain}},
		{sources.WebsiteNone, "https://unresponsive.example.test/", "", .68, []sources.Evidence{active, domain}},
		{sources.WebsiteHTTP, "http://last.example.test/", "http://last.example.test/", .84, []sources.Evidence{active, live, domain}},
	} {
		if i > 0 {
			_, err := f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Source: "fixture.example.test", Inputs: "fixture.example.test", Found: websiteFound(website.Result{State: tc.state, URL: tc.recordedURL + "unused"})}})
			contractOK(t, err)
			// Same-second results use their recording order, not domain or URL order.
			_, err = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Source: "other.example.test", Inputs: "other.example.test", Found: websiteFound(website.Result{State: tc.state, URL: tc.recordedURL})}})
			contractOK(t, err)
		}
		counts, err := f.s.Judge(ctx, []string{id, id})
		contractOK(t, err)
		contractEqual(t, counts, store.Counts{Judged: 1, Unranged: 1})
		a, err := f.s.GetAssessment(ctx, id)
		contractOK(t, err)
		near(t, a.Operating, tc.operating)
		contractEqual(t, a.Evidence, tc.evidence)
		contractEqual(t, a.Website, tc.state)
		contractEqual(t, a.WebsiteURL, tc.url)
		f.reopen()
		persisted, err := f.s.GetAssessment(ctx, id)
		contractOK(t, err)
		contractEqual(t, persisted, a)
	}
	f.now = f.now.AddDate(10, 0, 0)
	_, err := f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	aged, err := f.s.GetAssessment(ctx, id)
	contractOK(t, err)
	contractEqual(t, aged.Website, sources.WebsiteHTTP)
	contractEqual(t, aged.WebsiteURL, "http://last.example.test/")
	contractEqual(t, aged.Evidence, []sources.Evidence{live, domain})
	near(t, aged.Operating, .6)

}

// R-UDKQ-UJBH R-UCCU-GRKS
func TestWebsiteEvidenceDeduplicatesWithRowEvidence(t *testing.T) {
	for _, weight := range []float64{.4, .5, .7} {
		f := contractDB(t)
		ctx := context.Background()
		o := contractObservation("row", "Duplicate evidence", f.now)
		row := sources.Evidence{Name: sources.EvidenceWebsiteLive, Source: "row-source", Weight: weight, Description: "fixture-description"}
		o.Reading.Evidence = []sources.Evidence{row}
		id := contractAttach(t, f, "run", o)[0]
		_, err := f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "fixture.test", Source: "fixture.test", Found: websiteFound(website.Result{State: sources.WebsiteHTTPS, URL: "https://example.test/"})}})
		contractOK(t, err)
		_, err = f.s.Judge(ctx, []string{id})
		contractOK(t, err)
		a, err := f.s.GetAssessment(ctx, id)
		contractOK(t, err)
		expected := row
		if weight < .5 {
			expected = sources.Evidence{Name: sources.EvidenceWebsiteLive, Source: sources.WebsiteCheck, Weight: .5, Description: sources.DescribeWebsiteLive}
		}
		contractEqual(t, a.Evidence, []sources.Evidence{expected})
	}
}

// R-H5QH-WBC8 R-OSZL-VFY2 R-UIGC-DMA9
func TestMergeTransfersWebsiteResultsAndSplitLeavesThemOnOriginal(t *testing.T) {
	for _, originalLatest := range []bool{false, true} {
		t.Run(map[bool]string{false: "other_latest", true: "original_latest"}[originalLatest], func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			a := contractObservation("a", "Original", f.now)
			b := contractObservation("b", "Other", f.now)
			original := contractAttach(t, f, "run", a)[0]
			other := contractAttach(t, f, "run", b)[0]
			_, err := f.s.AddEnrichments(ctx, []store.Enrichment{
				{Candidate: original, Kind: sources.WebsiteCheck, Source: "first.test", Inputs: "first.test", Found: websiteFound(website.Result{State: sources.WebsiteNone})},
				{Candidate: other, Kind: sources.WebsiteCheck, Source: "second.test", Inputs: "second.test", Found: websiteFound(website.Result{State: sources.WebsiteHTTPS, URL: "https://second.test/"})},
			})
			contractOK(t, err)
			if originalLatest {
				_, err = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: original, Kind: sources.WebsiteCheck, Source: "third.test", Inputs: "third.test", Found: websiteFound(website.Result{State: sources.WebsiteHTTP, URL: "http://third.test/"})}})
				contractOK(t, err)
			}
			moved, err := f.s.Enrichments(ctx, other)
			contractOK(t, err)
			kept, err := f.s.Enrichments(ctx, original)
			contractOK(t, err)
			_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: original, Other: other, Why: "same", UserID: "owner"})
			contractOK(t, err)
			moved[0].Candidate = original
			want := append([]store.Enrichment{}, moved...)
			want = append(want, kept...)
			if originalLatest {
				want = []store.Enrichment{kept[0], moved[0], kept[1]}
			}
			results, err := f.s.Enrichments(ctx, original)
			contractOK(t, err)
			contractEqual(t, onlyWebsite(results), want)
			empty, err := f.s.Enrichments(ctx, other)
			contractOK(t, err)
			contractEqual(t, onlyWebsite(empty), []store.Enrichment{})
			assessment, err := f.s.GetAssessment(ctx, original)
			contractOK(t, err)
			contractEqual(t, assessment.Website, websiteDecoded(want[0]).State)
			contractEqual(t, assessment.WebsiteURL, websiteDecoded(want[0]).URL)
			split, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindSplit, Candidate: original, Keys: []string{sources.SourceKey(b.Scraper, b.SourceID)}, Why: "separate", UserID: "owner"})
			contractOK(t, err)
			results, err = f.s.Enrichments(ctx, original)
			contractOK(t, err)
			contractEqual(t, onlyWebsite(results), want)
			empty, err = f.s.Enrichments(ctx, split.Other)
			contractOK(t, err)
			contractEqual(t, onlyWebsite(empty), []store.Enrichment{})
			assessment, err = f.s.GetAssessment(ctx, split.Other)
			contractOK(t, err)
			contractEqual(t, assessment.Website, sources.WebsiteUnknown)
			contractEqual(t, assessment.WebsiteURL, "")
			f.reopen()
			results, err = f.s.Enrichments(ctx, original)
			contractOK(t, err)
			contractEqual(t, onlyWebsite(results), want)
		})
	}
}

// R-OSZL-VFY2
func TestOrdinaryStoreWritesPreserveWebsiteResults(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	o := contractObservation("row", "Preserved result", f.now)
	id := contractAttach(t, f, "initial", o)[0]
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Source: "preserved.test", Cost: .25, Inputs: "preserved.test", Found: websiteFound(website.Result{State: sources.WebsiteHTTPS, URL: "https://preserved.test/", Title: "fixture-title", StatusCode: 200, Size: 123, Error: "fixture-error"})}})
	contractOK(t, err)
	before, err := f.s.Enrichments(ctx, id)
	contractOK(t, err)
	for _, write := range []func() error{
		func() error { _, _, e := f.s.Attach(ctx, "rebuilt", true, []store.Observation{o}); return e },
		func() error { _, _, e := f.s.MarkGone(ctx, o.Scraper, "absent"); return e },
		func() error { _, e := f.s.Judge(ctx, []string{id}); return e },
		func() error { _, e := f.s.JudgeAll(ctx); return e },
		func() error { _, e := f.s.PrunePasses(ctx, 1); return e },
	} {
		contractOK(t, write())
		after, e := f.s.Enrichments(ctx, id)
		contractOK(t, e)
		contractEqual(t, after, before)
	}
}
