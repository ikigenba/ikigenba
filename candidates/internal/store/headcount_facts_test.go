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

// R-LQ8Z-ZH75
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
	var raw, labels string
	contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT headcounts,brief_labels FROM facts WHERE candidate=?", id).Scan(&raw, &labels)
	}))
	var stored []map[string]any
	contractOK(t, json.Unmarshal([]byte(raw), &stored))
	contractEqual(t, stored, []map[string]any{{"scraper": ppp.Name, "kind": sources.HeadcountJobs, "count": float64(26), "date": "2020-04-28T00:00:00Z"}, {"scraper": fmcsa.Name, "kind": sources.HeadcountDrivers, "count": float64(20)}})
	var storedLabels []string
	contractOK(t, json.Unmarshal([]byte(labels), &storedLabels))
	contractEqual(t, storedLabels, []string{"excavating contractor", "snow removal"})
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{labeledBrief(t, id, true)})
	contractOK(t, err)
	contractEqual(t, factsAt(ctx, t, f.s, id).BriefLabels, got.BriefLabels)
	f.reopen()
	contractEqual(t, factsAt(ctx, t, f.s, id).Headcounts, want)
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

// R-LRGW-D8XU
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
	contractOK(t, f.s.FillFacts(ctx))
	got := factsAt(ctx, t, f.s, id)
	expected := before
	expected.Headcounts = []sources.Headcount{{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, AsOf: time.Date(2020, 4, 28, 0, 0, 0, 0, time.UTC)}}
	expected.BriefLabels = []string{"snow removal"}
	contractEqual(t, got, expected)
	beforeSecond := contractState(t, f)
	resultsBefore, err := f.s.Enrichments(ctx, id)
	contractOK(t, err)
	contractOK(t, f.s.FillFacts(ctx))
	contractEqual(t, contractState(t, f), beforeSecond)
	contractEqual(t, factsAt(ctx, t, f.s, id), expected)
	resultsAfter, err := f.s.Enrichments(ctx, id)
	contractOK(t, err)
	contractEqual(t, resultsAfter, resultsBefore)
	f.reopen()
	contractEqual(t, factsAt(ctx, t, f.s, id), expected)
}
