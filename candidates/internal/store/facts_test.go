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

// R-85EN-1S9M R-OU7I-97OR R-WPW0-5EVS R-WOO3-RN53
func TestFactsRefreshIndependentlyFromOpinions(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	// Judging a candidate cannot create facts or enrichment results.
	before, err := f.s.Enrichments(ctx, id)
	contractOK(t, err)
	_, err = f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	after, err := f.s.Enrichments(ctx, id)
	contractOK(t, err)
	contractEqual(t, after, before)
	_, err = f.s.GetFacts(ctx, id)
	contractError(t, err, store.ErrNotFound)
	deriveAll(t, f.s)
	facts, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	_, err = f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	old, err := f.s.GetAssessment(ctx, id)
	contractOK(t, err)
	cohort := cohortCreate(t, f, "facts live")
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(id, sources.WebsiteHTTPS)})
	contractOK(t, err)
	current, err := f.s.GetAssessment(ctx, id)
	contractOK(t, err)
	contractEqual(t, current.Website, sources.WebsiteHTTPS)
	contractEqual(t, current.Calculated, old.Calculated)
	contractEqual(t, current.Operating, old.Operating)
	members := cohortRead(t, f, cohort).Members
	contractEqual(t, len(members), 1)
	contractEqual(t, members[0].Assessment.Website, sources.WebsiteHTTPS)
	// A ZIP replacement and raw enrichment write do not change the facts row.
	c, err := f.s.GetCandidate(ctx, id)
	contractOK(t, err)
	saved, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{c.Zip: 999}))
	raw := websiteResult(id, sources.WebsiteNone)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO enrichments(candidate,kind,found,source,at,cost,inputs) VALUES (?,?,?,?,?,?,?)`, id, raw.Kind, string(raw.Found), raw.Source, f.now.UTC().Format(time.RFC3339), raw.Cost, raw.Inputs)
		return e
	}))
	_, err = f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	judged, err := f.s.GetAssessment(ctx, id)
	contractOK(t, err)
	contractEqual(t, judged.Website, sources.WebsiteHTTPS)
	contractEqual(t, judged.Minutes, saved.Minutes)
	got, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, got, saved)
	contractEqual(t, facts.Website, sources.WebsiteUnknown)
}

// R-MOMA-IP7M R-R7HY-562T R-LD70-4W25 R-WNG7-DVEE R-MTHW-1S6E
func TestMovingRulingsRefreshDerivedFactsAndKeepMergedJudgments(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	a := contractObservation("carrier", "Carrier", f.now)
	a.Scraper = fmcsa.Name
	a.Reading.Class = sources.ClassForHire
	a.Reading.Category = sources.CategoryTrucking
	a.Reading.FleetVerdict = sources.FleetPass
	a.Reading.PowerUnits = ptrInt(3)
	b := contractObservation("loan", "Borrower", f.now)
	b.Scraper = ppp.Name
	other := contractAttach(t, f, "run", a)[0]
	survivor := contractAttach(t, f, "run", b)[0]
	deriveAll(t, f.s)
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(other, sources.WebsiteHTTPS)})
	contractOK(t, err)
	_, err = f.s.Judge(ctx, []string{other})
	contractOK(t, err)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, other, "merged brief", false, sources.CategoryRetail)})
	contractOK(t, err)
	_, err = f.s.Judge(ctx, []string{other})
	contractOK(t, err)
	old, err := f.s.GetAssessment(ctx, other)
	contractOK(t, err)
	oldCandidate, err := f.s.GetCandidate(ctx, other)
	contractOK(t, err)
	oldLinks, err := f.s.Links(ctx, other)
	contractOK(t, err)
	oldEstimates, err := f.s.Estimates(ctx, other)
	contractOK(t, err)
	before, err := f.s.Enrichments(ctx, survivor)
	contractOK(t, err)
	moved, err := f.s.Enrichments(ctx, other)
	contractOK(t, err)
	_, cs, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: survivor, Other: other, Why: "same business", UserID: "owner"})
	contractOK(t, err)
	contractEqual(t, len(cs), 2)
	contractEqual(t, cs[0].ID, survivor)
	contractEqual(t, cs[1].ID, other)
	oldCandidate.Status = store.StatusMerged
	oldCandidate.MergedInto = survivor
	oldCandidate.Updated = f.now.UTC().Truncate(time.Second)
	contractEqual(t, cs[1], oldCandidate)
	merged, err := f.s.GetAssessment(ctx, other)
	contractOK(t, err)
	contractEqual(t, merged, old)
	estimates, err := f.s.Estimates(ctx, other)
	contractOK(t, err)
	contractEqual(t, estimates, oldEstimates)
	_, err = f.s.GetFacts(ctx, other)
	contractError(t, err, store.ErrNotFound)
	links, err := f.s.Links(ctx, survivor)
	contractOK(t, err)
	for _, l := range links {
		if l.Scraper == a.Scraper && l.SourceID == a.SourceID {
			want := oldLinks[0]
			want.Candidate = survivor
			want.Reason = store.LinkDecision
			want.Confidence = 1
			contractEqual(t, l, want)
		}
	}
	results, err := f.s.Enrichments(ctx, survivor)
	contractOK(t, err)
	contractEqual(t, len(results), len(before)+len(moved)+2)
	contractEqual(t, results[0].Kind, enrichers.KindLinkFacts)
	contractEqual(t, results[1].Kind, enrichers.KindDriveMinutes)
	var lf enrichers.LinkFound
	contractOK(t, json.Unmarshal(results[0].Found, &lf))
	contractEqual(t, lf.FleetVerdict, sources.FleetPass)
	contractEqual(t, *lf.PowerUnits, int64(3))
	facts, err := f.s.GetFacts(ctx, survivor)
	contractOK(t, err)
	contractEqual(t, facts.Website, sources.WebsiteHTTPS)
	contractEqual(t, facts.Brief, "merged brief")
	contractEqual(t, facts.Categories, []sources.Category{{Name: sources.CategoryRetail, Source: enrichers.KindBrief}})
	contractEqual(t, store.ClassifiedBy(facts.Categories), enrichers.KindBrief)
	split, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindSplit, Candidate: survivor, Keys: []string{sources.SourceKey(a.Scraper, a.SourceID)}, Why: "separate", UserID: "owner"})
	contractOK(t, err)
	facts, err = f.s.GetFacts(ctx, survivor)
	contractOK(t, err)
	contractEqual(t, facts.FleetVerdict, sources.FleetUnknown)
	contractEqual(t, facts.Website, sources.WebsiteHTTPS)
	child, err := f.s.GetFacts(ctx, split.Other)
	contractOK(t, err)
	contractEqual(t, child.FleetVerdict, sources.FleetPass)
	contractEqual(t, child.Website, sources.WebsiteUnknown)
	contractEqual(t, facts.Brief, "merged brief")
	contractEqual(t, child.Brief, "")
	contractEqual(t, facts.Categories, []sources.Category{{Name: sources.CategoryRetail, Source: enrichers.KindBrief}})
	contractEqual(t, child.Categories, []sources.Category{{Name: sources.CategoryTrucking, Source: fmcsa.Name}})
	contractEqual(t, store.ClassifiedBy(child.Categories), enrichers.SourceLinks)
	f.reopen()
	persistedChild, e := f.s.GetFacts(ctx, split.Other)
	contractOK(t, e)
	contractEqual(t, persistedChild, child)
	persistedSurvivor, e := f.s.GetFacts(ctx, survivor)
	contractOK(t, e)
	contractEqual(t, persistedSurvivor, facts)
	beforeFacts := facts
	beforeResults, err := f.s.Enrichments(ctx, survivor)
	contractOK(t, err)
	// Nonmoving rulings preserve stale derived facts after a link changes.
	b.Digest = "changed"
	b.Reading.GovernmentMatch = "new"
	contractAttach(t, f, "later", b)
	for _, kind := range []string{store.KindExclude, store.KindInclude, store.KindNotSame} {
		_, _, err = f.s.Decide(ctx, store.Ruling{Kind: kind, Candidate: survivor, Other: split.Other, Why: "owner", UserID: "owner"})
		contractOK(t, err)
		facts, err = f.s.GetFacts(ctx, survivor)
		contractOK(t, err)
		contractEqual(t, facts, beforeFacts)
		results, err = f.s.Enrichments(ctx, survivor)
		contractOK(t, err)
		contractEqual(t, results, beforeResults)
	}
	f.reopen()
	facts, err = f.s.GetFacts(ctx, survivor)
	contractOK(t, err)
	contractEqual(t, facts, beforeFacts)
}

// R-OSZL-VFY2 R-OU7I-97OR R-OECT-A71Q R-WPW0-5EVS R-MTHW-1S6E
func TestOrdinaryWritesPreserveFactsAndResultHistory(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	other := enrichmentCandidate(t, f, "unrelated", "office@unrelated.test")
	deriveAll(t, f.s)
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(id, sources.WebsiteHTTPS)})
	contractOK(t, err)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{siteTextResult(t, id, "failed")})
	contractOK(t, err)
	ids := []string{id, other}
	savedFacts := map[string]store.Facts{}
	savedResults := map[string][]store.Enrichment{}
	for _, id := range ids {
		savedFacts[id], err = f.s.GetFacts(ctx, id)
		contractOK(t, err)
		savedResults[id], err = f.s.Enrichments(ctx, id)
		contractOK(t, err)
	}
	page, err := f.s.AddPage(ctx, store.Page{Candidate: id, URL: "https://kept.test/", Text: "kept", UserID: "owner"})
	contractOK(t, err)
	for _, id := range ids {
		facts, e := f.s.GetFacts(ctx, id)
		contractOK(t, e)
		contractEqual(t, facts, savedFacts[id])
		results, e := f.s.Enrichments(ctx, id)
		contractOK(t, e)
		contractEqual(t, results, savedResults[id])
	}

	pages, err := f.s.Pages(ctx, id)
	contractOK(t, err)
	counts, err := f.s.CountPages(ctx, id)
	contractOK(t, err)
	calls := []func() error{
		func() error {
			o := contractObservation("one", "Changed", f.now)
			o.Reading.Zip = "99999"
			_, _, e := f.s.Attach(ctx, "later", true, []store.Observation{o})
			return e
		},
		func() error { _, _, e := f.s.MarkGone(ctx, "source-z", "absent"); return e },
		func() error { _, e := f.s.Judge(ctx, ids); return e },
		func() error { _, e := f.s.JudgeAll(ctx); return e },
		func() error { return f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 99}) },
		func() error { return f.s.FillFacts(ctx) },
		func() error {
			p := contractNewPass(t, f, "ended")
			_, e := f.s.FinishPass(ctx, p.ID, store.Counts{Derived: 4})
			return e
		},
		func() error {
			p := contractNewPass(t, f, "failed")
			_, e := f.s.FailPass(ctx, p.ID, store.ReasonStore)
			return e
		},
		func() error { _, e := f.s.PrunePasses(ctx, 1); return e },
	}
	for _, kind := range []string{store.KindExclude, store.KindInclude, store.KindNotSame} {
		calls = append(calls, func() error {
			_, _, e := f.s.Decide(ctx, store.Ruling{Kind: kind, Candidate: id, Other: other, Why: "owner decision", UserID: "owner"})
			return e
		})
	}
	for _, call := range calls {
		contractOK(t, call())
		gotPages, e := f.s.Pages(ctx, id)
		contractOK(t, e)
		contractEqual(t, gotPages, pages)
		gotPage, e := f.s.GetPage(ctx, id, page.Number)
		contractOK(t, e)
		contractEqual(t, gotPage, page)
		gotCounts, e := f.s.CountPages(ctx, id)
		contractOK(t, e)
		contractEqual(t, gotCounts, counts)

		for _, id := range ids {
			facts, e := f.s.GetFacts(ctx, id)
			contractOK(t, e)
			contractEqual(t, facts, savedFacts[id])
			results, e := f.s.Enrichments(ctx, id)
			contractOK(t, e)
			contractEqual(t, results, savedResults[id])
		}
	}
	f.reopen()
	for _, id := range ids {
		facts, e := f.s.GetFacts(ctx, id)
		contractOK(t, e)
		contractEqual(t, facts, savedFacts[id])
	}
}

// R-7UFJ-LULD R-GSBL-OU6L R-80J1-IPAU
func TestFactsProjectionUsesNewestResultOfEveryKind(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	low, power, drivers := int64(84), int64(3), int64(7)
	lf := enrichers.LinkFound{KnownSince: "2018-04-09", NewestFiling: "2025-06-01", GovernmentMatch: "pattern", Categories: []enrichers.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}, {Name: sources.CategoryTrucking, Source: fmcsa.Name}}, FleetVerdict: sources.FleetPass, PowerUnits: &power, Drivers: &drivers}
	encode := func(v any) json.RawMessage { b, e := json.Marshal(v); contractOK(t, e); return b }
	first := []store.Enrichment{
		{Candidate: id, Kind: enrichers.KindLinkFacts, Inputs: "links-one", Source: enrichers.SourceLinks, Found: encode(lf)},
		{Candidate: id, Kind: enrichers.KindDriveMinutes, Inputs: "zip-one", Source: enrichers.SourceZipDrive, Found: encode(enrichers.MinutesFound{Minutes: &low})},
		websiteResult(id, sources.WebsiteHTTPS),
	}
	_, err := f.s.AddEnrichments(ctx, first)
	contractOK(t, err)
	stamp := f.now.UTC().Truncate(time.Second)
	expected := store.Facts{Candidate: id, KnownSince: time.Date(2018, 4, 9, 0, 0, 0, 0, time.UTC), NewestFiling: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), GovernmentMatch: lf.GovernmentMatch, Categories: []sources.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}, {Name: sources.CategoryTrucking, Source: fmcsa.Name}}, FleetVerdict: sources.FleetPass, PowerUnits: &power, Drivers: &drivers, Website: sources.WebsiteHTTPS, WebsiteURL: websiteDecoded(first[2]).URL, Minutes: &low, Written: []store.Written{{Kind: enrichers.KindDriveMinutes, At: stamp}, {Kind: enrichers.KindLinkFacts, At: stamp}, {Kind: sources.WebsiteCheck, At: stamp}}}
	expected.Records = append([]sources.Category{}, expected.Categories...)
	got, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, got, expected)
	// Same-second results replace by append order, independently for each kind.
	lf.KnownSince = "2019-02-03"
	lf.NewestFiling = ""
	lf.GovernmentMatch = ""
	lf.Categories = []enrichers.Category{{Name: sources.CategoryOther, Source: ppp.Name}}
	lf.FleetVerdict = sources.FleetUnknown
	lf.PowerUnits = nil
	lf.Drivers = nil
	replacement := store.Enrichment{Candidate: id, Kind: enrichers.KindLinkFacts, Inputs: "links-two", Source: enrichers.SourceLinks, Found: encode(lf)}
	none := websiteResult(id, sources.WebsiteNone)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{replacement, none})
	contractOK(t, err)
	expected.KnownSince = time.Date(2019, 2, 3, 0, 0, 0, 0, time.UTC)
	expected.NewestFiling = time.Time{}
	expected.GovernmentMatch = ""
	expected.Categories = []sources.Category{{Name: sources.CategoryOther, Source: ppp.Name}}
	expected.Records = append([]sources.Category{}, expected.Categories...)
	expected.FleetVerdict = sources.FleetUnknown
	expected.PowerUnits = nil
	expected.Drivers = nil
	expected.Website = sources.WebsiteNone
	expected.WebsiteURL = ""
	got, err = f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, got, expected)
	f.reopen()
	got, err = f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, got, expected)
}
