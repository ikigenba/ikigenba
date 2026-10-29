package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func headcountLinkResult(t *testing.T, id string, hs ...enrichers.Headcount) store.Enrichment {
	t.Helper()
	b, err := json.Marshal(enrichers.LinkFound{Categories: []enrichers.Category{}, FleetVerdict: sources.FleetUnknown, Headcounts: hs})
	contractOK(t, err)
	return store.Enrichment{Candidate: id, Kind: enrichers.KindLinkFacts, Inputs: "links", Source: enrichers.SourceLinks, Found: b}
}
func labeledBrief(t *testing.T, id string, failed bool, labels ...string) store.Enrichment {
	t.Helper()
	e := briefResult(t, id, "summary", failed)
	var b enrichers.BriefFound
	contractOK(t, json.Unmarshal(e.Found, &b))
	b.Labels = append([]string{}, labels...)
	var err error
	e.Found, err = json.Marshal(b)
	contractOK(t, err)
	return e
}

// R-SXY1-ZLGX
func TestHeadcountFactsProjectionNewestAndGoodBrief(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(id, sources.WebsiteNone)})
	contractOK(t, err)
	unknown := factsAt(ctx, t, f.s, id)
	contractEqual(t, unknown.Headcounts, []sources.Headcount{})
	contractEqual(t, unknown.BriefLabels, []string{})
	hs := []enrichers.Headcount{{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, Date: "2020-04-28"}, {Scraper: fmcsa.Name, Kind: sources.HeadcountDrivers, Count: 20}}
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{headcountLinkResult(t, id, hs...), labeledBrief(t, id, false, "excavating contractor", "snow removal")})
	contractOK(t, err)
	want := []sources.Headcount{{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, AsOf: time.Date(2020, 4, 28, 0, 0, 0, 0, time.UTC)}, {Scraper: fmcsa.Name, Kind: sources.HeadcountDrivers, Count: 20}}
	got := factsAt(ctx, t, f.s, id)
	contractEqual(t, got.Headcounts, want)
	contractEqual(t, got.BriefLabels, []string{"excavating contractor", "snow removal"})
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{labeledBrief(t, id, true)})
	contractOK(t, err)
	contractEqual(t, factsAt(ctx, t, f.s, id).BriefLabels, got.BriefLabels)
	f.reopen()
	contractEqual(t, factsAt(ctx, t, f.s, id).Headcounts, want)
	contractEqual(t, factsAt(ctx, t, f.s, id).BriefLabels, got.BriefLabels)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{headcountLinkResult(t, id), labeledBrief(t, id, false)})
	contractOK(t, err)
	contractEqual(t, factsAt(ctx, t, f.s, id).Headcounts, []sources.Headcount{})
	contractEqual(t, factsAt(ctx, t, f.s, id).BriefLabels, []string{})
	// A result from the previous release omitted headcounts entirely.
	old := headcountLinkResult(t, id)
	old.Found = json.RawMessage(`{"categories":[],"fleet_verdict":"unknown"}`)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{old})
	contractOK(t, err)
	contractEqual(t, factsAt(ctx, t, f.s, id).Headcounts, []sources.Headcount{})
}

// R-4N5M-7PGF
func TestFillFactsRefreshesOnlyHeadcountsAndLabelsAndIsIdempotent(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{headcountLinkResult(t, id, enrichers.Headcount{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, Date: "2020-04-28"}), labeledBrief(t, id, false, "snow removal")})
	contractOK(t, err)
	// Model an old release writing stale extra columns and independent facts.
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "UPDATE facts SET headcounts='[]',brief_labels='[]',brief='previous-release-summary',website='none',website_url='prior-url',brief_at='2020-01-01T00:00:00Z' WHERE candidate=?", id)
		return e
	}))
	before := factsAt(ctx, t, f.s, id)
	contractOK(t, fillFactsWalk(ctx, f.s))
	got := factsAt(ctx, t, f.s, id)
	expected := before
	expected.Headcounts = []sources.Headcount{{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, AsOf: time.Date(2020, 4, 28, 0, 0, 0, 0, time.UTC)}}
	expected.BriefLabels = []string{"snow removal"}
	contractEqual(t, got, expected)
	beforeSecond := contractState(t, f)
	resultsBefore, err := f.s.Enrichments(ctx, id)
	contractOK(t, err)
	contractOK(t, fillFactsWalk(ctx, f.s))
	contractEqual(t, contractState(t, f), beforeSecond)
	contractEqual(t, factsAt(ctx, t, f.s, id), expected)
	resultsAfter, err := f.s.Enrichments(ctx, id)
	contractOK(t, err)
	contractEqual(t, resultsAfter, resultsBefore)
	f.reopen()
	contractEqual(t, factsAt(ctx, t, f.s, id), expected)
}

// R-T2TN-IOFP
func TestHeadcountHoursPersistenceAndPreviousWriter(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	hours, newerHours := int64(715864), int64(47840)
	hs := []enrichers.Headcount{
		{Scraper: "osha_ita", Kind: sources.HeadcountEmployees, Count: 23, Date: "2025-12-31", Hours: &newerHours},
		{Scraper: "osha_ita", Kind: sources.HeadcountEmployees, Count: 357, Date: "2024-12-31", Hours: &hours},
		{Scraper: "osha_ita", Kind: sources.HeadcountEmployees, Count: 357, Date: "2024-12-31", Hours: &hours},
		{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, Date: "2020-04-28"},
	}
	want := []sources.Headcount{}
	for _, h := range hs {
		at, err := time.Parse(time.DateOnly, h.Date)
		contractOK(t, err)
		want = append(want, sources.Headcount{Scraper: h.Scraper, Kind: h.Kind, Count: h.Count, AsOf: at, Hours: h.Hours})
	}
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{headcountLinkResult(t, id, hs...)})
	contractOK(t, err)
	contractEqual(t, factsAt(ctx, t, f.s, id).Headcounts, want)
	f.reopen()
	contractEqual(t, factsAt(ctx, t, f.s, id).Headcounts, want)
	legacy := hs[0]
	legacy.Hours = nil
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{headcountLinkResult(t, id, legacy)})
	contractOK(t, err)
	noHours := want[0]
	noHours.Hours = nil
	contractEqual(t, factsAt(ctx, t, f.s, id).Headcounts, []sources.Headcount{noHours})
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{headcountLinkResult(t, id, hs[0])})
	contractOK(t, err)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE facts SET headcounts=? WHERE candidate=?`, `[{"scraper":"osha_ita","kind":"employees","count":23,"date":"2025-12-31T00:00:00Z"}]`, id)
		return e
	}))
	contractEqual(t, factsAt(ctx, t, f.s, id).Headcounts, []sources.Headcount{noHours})
	contractOK(t, fillFactsWalk(ctx, f.s))
	contractEqual(t, factsAt(ctx, t, f.s, id).Headcounts, want[:1])
	f.reopen()
	contractEqual(t, factsAt(ctx, t, f.s, id).Headcounts, want[:1])
}
