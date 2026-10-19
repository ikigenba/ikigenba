package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-M10O-KUCX R-OFKP-NYSF
func TestSubjectReadsDisplayLinksAndCurrentFacts(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := enrichmentCandidate(t, f, "subject", "owner@business.test")
	assertSubject := func(id string) {
		t.Helper()
		c, err := f.s.GetCandidate(ctx, id)
		contractOK(t, err)
		links, err := f.s.Links(ctx, id)
		contractOK(t, err)
		want := enrichers.Subject{ID: c.ID, Name: c.Name, City: c.City, State: c.State, Email: c.Email, Zip: c.Zip, Links: []enrichers.Link{}, Pages: []enrichers.Page{}}
		if m, e := f.s.ZipMinutes(ctx, c.Zip); e == nil {
			want.Minutes = &m
		}
		if facts, e := f.s.GetFacts(ctx, id); e == nil {
			want.Website = facts.WebsiteURL
			want.Facts = &enrichers.LinkFound{GovernmentMatch: facts.GovernmentMatch, FleetVerdict: facts.FleetVerdict, PowerUnits: facts.PowerUnits, Drivers: facts.Drivers, Categories: []enrichers.Category{}}
			if !facts.KnownSince.IsZero() {
				want.Facts.KnownSince = facts.KnownSince.UTC().Format("2006-01-02")
			}
			if !facts.NewestFiling.IsZero() {
				want.Facts.NewestFiling = facts.NewestFiling.UTC().Format("2006-01-02")
			}
			for _, c := range facts.Categories {
				want.Facts.Categories = append(want.Facts.Categories, enrichers.Category{Name: c.Name, Source: c.Source})
			}
		}
		for _, l := range links {
			want.Links = append(want.Links, enrichers.Link{Scraper: l.Scraper, SourceID: l.SourceID, Gone: l.Gone, Reading: l.Reading})
		}
		got, err := f.s.Subject(ctx, id)
		contractOK(t, err)
		contractEqual(t, got, want)
	}
	assertSubject(id)
	c, err := f.s.GetCandidate(ctx, id)
	contractOK(t, err)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{c.Zip: 0}))
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(id, sources.WebsiteHTTPS)})
	contractOK(t, err)
	_, _, err = f.s.MarkGone(ctx, "source-z", "absent")
	contractOK(t, err)
	assertSubject(id)
	survivor := enrichmentCandidate(t, f, "survivor", "owner@survivor.test")
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: survivor, Other: id, Why: "same", UserID: "owner"})
	contractOK(t, err)
	assertSubject(id)
	got, err := f.s.Subject(ctx, "missing")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, got, enrichers.Subject{})
}

// R-OGSM-1QJ4 R-OI0I-FI9T R-OO40-CCZA
func TestDueAmongKeepsTargetOrderAndSkipsIneligible(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	a := enrichmentCandidate(t, f, "target-a", "owner@a.test")
	b := enrichmentCandidate(t, f, "target-b", "owner@b.test")
	empty := enrichmentCandidate(t, f, "target-empty", "")
	merged := enrichmentCandidate(t, f, "target-merged", "owner@merged.test")
	_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: a, Other: merged, Why: "same", UserID: "owner"})
	contractOK(t, err)
	ids := []string{b, "missing", empty, merged, a, b, a}
	got, err := f.s.DueAmong(ctx, sources.WebsiteCheck, ids, false)
	contractOK(t, err)
	contractEqual(t, got, []store.Due{{Candidate: b, Inputs: "b.test"}, {Candidate: a, Inputs: "a.test"}})
	e := websiteResult(b, sources.WebsiteHTTPS)
	e.Inputs = "b.test"
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{e})
	contractOK(t, err)
	f.now = f.now.AddDate(10, 0, 0)
	got, err = f.s.DueAmong(ctx, sources.WebsiteCheck, ids, false)
	contractOK(t, err)
	contractEqual(t, got, []store.Due{{Candidate: a, Inputs: "a.test"}})
	got, err = f.s.DueAmong(ctx, sources.WebsiteCheck, ids, true)
	contractOK(t, err)
	contractEqual(t, got, []store.Due{{Candidate: b, Inputs: "b.test"}, {Candidate: a, Inputs: "a.test"}})
	for _, force := range []bool{false, true} {
		got, err = f.s.DueAmong(ctx, sources.WebsiteCheck, []string{empty, merged, "missing"}, force)
		contractOK(t, err)
		contractEqual(t, got, []store.Due{})
		got, err = f.s.DueAmong(ctx, "invalid", ids, force)
		contractError(t, err, store.ErrUnreachable)
		contractEqual(t, got, []store.Due(nil))
	}
}

// R-82YU-A8S8 R-OKGB-71R7 R-OVFE-MZFG R-OWNB-0R65
func TestDeriveAmongRecordsOnlyDistinctEligibleTargets(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	a := enrichmentCandidate(t, f, "derive-a", "owner@a.test")
	b := enrichmentCandidate(t, f, "derive-b", "owner@b.test")
	outside := enrichmentCandidate(t, f, "derive-outside", "owner@outside.test")
	merged := enrichmentCandidate(t, f, "derive-merged", "owner@merged.test")
	_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: a, Other: merged, Why: "same", UserID: "owner"})
	contractOK(t, err)
	// Merge derives a already; force tests both previously written and unwritten targets.
	ids := []string{b, "missing", merged, a, b, a}
	before := map[string][]store.Enrichment{}
	for _, id := range []string{a, b, outside, merged} {
		before[id], err = f.s.Enrichments(ctx, id)
		contractOK(t, err)
	}
	mark, err := f.s.EnrichmentMark(ctx)
	contractOK(t, err)
	f.now = f.now.Add(time.Hour)
	n, err := f.s.DeriveAmong(ctx, enrichers.KindDriveMinutes, ids, true)
	contractOK(t, err)
	contractEqual(t, n, int64(2))
	latest, err := f.s.EnrichmentAfter(ctx, enrichers.KindDriveMinutes, "55072", mark)
	contractOK(t, err)
	contractEqual(t, latest.Candidate, a)
	for _, id := range []string{a, b} {
		subject, e := f.s.Subject(ctx, id)
		contractOK(t, e)
		want := enrichers.DriveMinutes{}.Derive(subject)
		es, e := f.s.Enrichments(ctx, id)
		contractOK(t, e)
		contractEqual(t, len(es), len(before[id])+1)
		contractEqual(t, es[0], store.Enrichment{Candidate: id, Kind: enrichers.KindDriveMinutes, Inputs: subject.Zip, Source: want.Source, Found: want.Found, Cost: want.Cost, At: f.now.UTC().Truncate(time.Second)})
		facts, e := f.s.GetFacts(ctx, id)
		contractOK(t, e)
		contractEqual(t, facts.Minutes, subject.Minutes)
	}
	for _, id := range []string{outside, merged} {
		es, e := f.s.Enrichments(ctx, id)
		contractOK(t, e)
		contractEqual(t, es, before[id])
	}
	n, err = f.s.DeriveAmong(ctx, enrichers.KindDriveMinutes, ids, false)
	contractOK(t, err)
	contractEqual(t, n, int64(0))
	for _, kind := range []string{sources.WebsiteCheck, enrichers.KindSiteText, "invalid"} {
		n, err = f.s.DeriveAmong(ctx, kind, ids, true)
		contractError(t, err, store.ErrUnreachable)
		contractEqual(t, n, int64(0))
	}
	_, err = f.s.GetFacts(ctx, outside)
	contractError(t, err, store.ErrNotFound)
	f.reopen()
	facts, err := f.s.GetFacts(ctx, b)
	contractOK(t, err)
	contractEqual(t, facts.Written, []store.Written{{Kind: enrichers.KindDriveMinutes, At: f.now.UTC().Truncate(time.Second)}})
}

func siteTextResult(t *testing.T, id, status string) store.Enrichment {
	t.Helper()
	found := enrichers.SiteTextFound{Run: "prompt-run", Status: status, Pages: 2, CostNanos: 1250000000}
	if status == enrichers.RunExited {
		zero := 0
		found.ExitCode = &zero
	}
	b, err := json.Marshal(found)
	contractOK(t, err)
	return store.Enrichment{Candidate: id, Kind: enrichers.KindSiteText, Inputs: "https://business.test/", Source: enrichers.SourcePrompts, Found: b, Cost: 1.25}
}

// R-L5GX-B4T3 R-80J1-IPAU
func TestSiteTextWrittenProjectsIntoCohortWithoutChangingMembership(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	a := contractSeed(t, f)
	b := enrichmentCandidate(t, f, "site-other", "owner@other.test")
	deriveAll(t, f.s)
	_, err := f.s.JudgeAll(ctx)
	contractOK(t, err)
	cohort := cohortCreate(t, f, "site text stamps")
	before := cohortRead(t, f, cohort)
	for _, status := range []string{enrichers.RunExited, "failed"} {
		f.now = f.now.Add(time.Second)
		_, err = f.s.AddEnrichments(ctx, []store.Enrichment{siteTextResult(t, a, status)})
		contractOK(t, err)
	}
	stamp := f.now.UTC().Truncate(time.Second)
	f.now = f.now.Add(time.Second)
	_, err = f.s.DeriveAmong(ctx, enrichers.KindLinkFacts, []string{a}, true)
	contractOK(t, err)
	got := cohortRead(t, f, cohort)
	contractEqual(t, got.Matching, before.Matching)
	contractEqual(t, cohortIDs(got.Members), cohortIDs(before.Members))
	for i, m := range got.Members {
		contractEqual(t, m.Candidate, before.Members[i].Candidate)
		contractEqual(t, m.Assessment, before.Members[i].Assessment)
		switch m.Candidate.ID {
		case a:
			contractEqual(t, m.SiteText, stamp)
		case b:
			contractEqual(t, m.SiteText, time.Time{})
		}
	}
	facts, err := f.s.GetFacts(ctx, a)
	contractOK(t, err)
	found := false
	for _, w := range facts.Written {
		if w.Kind == enrichers.KindSiteText {
			found = true
			contractEqual(t, w.At, stamp)
		}
	}
	if !found {
		t.Fatal("missing site text written time")
	}
	f.reopen()
	contractEqual(t, cohortRead(t, f, cohort), got)
}

// R-82YU-A8S8
func TestDeriveAmongNeverRecordsEmptyInputs(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	o := contractObservation("no-zip", "No ZIP", f.now)
	o.Reading.Zip = ""
	o.Reading.PhoneKey = ""
	o.Reading.DomainKey = ""
	id := contractAttach(t, f, "run", o)[0]
	for _, force := range []bool{false, true} {
		n, err := f.s.DeriveAmong(ctx, enrichers.KindDriveMinutes, []string{id, id}, force)
		contractOK(t, err)
		contractEqual(t, n, int64(0))
		es, err := f.s.Enrichments(ctx, id)
		contractOK(t, err)
		contractEqual(t, es, []store.Enrichment{})
		_, err = f.s.GetFacts(ctx, id)
		contractError(t, err, store.ErrNotFound)
	}
}
