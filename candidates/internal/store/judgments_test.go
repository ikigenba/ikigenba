package store_test

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func near(t *testing.T, p *float64, w float64) {
	t.Helper()
	if p == nil || math.Abs(*p-w) > 1e-9 {
		t.Fatalf("got %v want %v", p, w)
	}
}

// R-5J4Y-DB63 R-5LKR-4UNH R-Q42O-WIPN R-5HX1-ZJFE R-5KCU-R2WS
func TestJudgmentsAggregateAndPersist(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.now = time.Date(2026, 10, 15, 0, 0, 0, 456, time.UTC)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 31}))
	active := sources.Evidence{Name: sources.EvidenceFMCSAActive, Source: fmcsa.Name, Weight: .6}
	activity := sources.Evidence{Name: sources.EvidenceFMCSAActivity, Source: fmcsa.Name, Weight: .3}
	domain := sources.Evidence{Name: sources.EvidenceOwnDomain, Source: fmcsa.Name, Weight: .2}
	loan := sources.Evidence{Name: sources.EvidencePPPLoan, Source: ppp.Name, Weight: .2}
	high := int64(3500000)
	a := contractObservation("carrier", "Combined business", time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	a.Scraper = fmcsa.Name
	a.Reading.Estimate = &sources.Estimate{Source: fmcsa.Name, Low: 2100000, High: &high, Confidence: .7, Formula: sources.Rules}
	a.Reading.Evidence = []sources.Evidence{domain, activity, active, domain}
	a.Reading.KnownSince = time.Date(2015, 3, 1, 0, 0, 0, 0, time.UTC)
	a.Reading.Category = sources.CategoryTrucking
	id := contractAttach(t, f, "first", a)[0]
	b := a
	b.Scraper = ppp.Name
	b.SourceID = "loan"
	b.Reading.Filed = time.Date(2020, 4, 9, 0, 0, 0, 0, time.UTC)
	b.Reading.KnownSince = time.Date(2018, 4, 9, 0, 0, 0, 0, time.UTC)
	b.Reading.Category = sources.CategoryConstruction
	b.Reading.Evidence = []sources.Evidence{loan}
	ph := int64(3840000)
	b.Reading.Estimate = &sources.Estimate{Source: ppp.Name, Low: 2400000, High: &ph, Confidence: .4, Formula: sources.Rules}
	b.Reading.GovernmentMatch = "fixture-government"
	contractAttach(t, f, "first", b)
	smaller := b
	smaller.SourceID = "smaller"
	smaller.Reading.Filed = b.Reading.Filed.AddDate(1, 0, 0)
	sh := int64(1920000)
	smaller.Reading.Estimate = &sources.Estimate{Source: ppp.Name, Low: 1200000, High: &sh, Confidence: .4}
	smaller.Reading.GovernmentMatch = ""
	contractAttach(t, f, "first", smaller)
	empty := contractObservation("empty", "Empty business", time.Time{})
	empty.Reading.Estimate = nil
	empty.Reading.Evidence = []sources.Evidence{active}
	emptyID := contractAttach(t, f, "first", empty)[0]
	beforeCandidates, err := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
	contractOK(t, err)
	beforeLinks, err := f.s.Links(ctx, id)
	contractOK(t, err)
	deriveAll(t, f.s)
	c, err := f.s.Judge(ctx, []string{id, id, "missing", emptyID})
	contractOK(t, err)
	contractEqual(t, c, store.Counts{Judged: 2, Unranged: 1})
	after := contractState(t, f)
	contractEqual(t, after.candidates, beforeCandidates)
	contractEqual(t, after.links[id], beforeLinks)
	contractEqual(t, len(after.rulings[id]), 0)
	got, err := f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	near(t, got.Operating, .8208)
	contractEqual(t, got.OperatingKey, int64(820800))
	contractEqual(t, *got.RevenueLow, int64(1772727))
	contractEqual(t, *got.RevenueHigh, int64(2925455))
	near(t, got.RevenueConfidence, .7)
	contractEqual(t, got.Evidence, []sources.Evidence{active, activity, domain, loan})
	contractEqual(t, factsAt(ctx, t, f.s, id).KnownSince, a.Reading.KnownSince)
	contractEqual(t, factsAt(ctx, t, f.s, id).NewestFiling, a.Reading.Filed)
	contractEqual(t, factsAt(ctx, t, f.s, id).Categories, []sources.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}, {Name: sources.CategoryGovernment, Source: "name"}, {Name: sources.CategoryTrucking, Source: fmcsa.Name}})
	contractEqual(t, factsAt(ctx, t, f.s, id).GovernmentMatch, b.Reading.GovernmentMatch)
	contractEqual(t, *factsAt(ctx, t, f.s, id).Minutes, int64(31))
	contractEqual(t, got.Rules, sources.Rules)
	contractEqual(t, got.Calculated, f.now.Truncate(time.Second))
	es, err := f.s.Estimates(ctx, id)
	contractOK(t, err)
	contractEqual(t, es, []sources.Estimate{*a.Reading.Estimate, *smaller.Reading.Estimate})
	blank, err := f.s.GetJudgments(ctx, emptyID)
	contractOK(t, err)
	contractEqual(t, blank.Operating, (*float64)(nil))
	contractEqual(t, blank.OperatingKey, int64(-1))
	contractEqual(t, blank.RevenueLow, (*int64)(nil))
	contractEqual(t, blank.RevenueHigh, (*int64)(nil))
	contractEqual(t, blank.RevenueConfidence, (*float64)(nil))
	contractEqual(t, blank.Evidence, []sources.Evidence{})
	contractEqual(t, blank.HeadcountLow, (*int64)(nil))
	contractEqual(t, blank.HeadcountHigh, (*int64)(nil))
	contractEqual(t, blank.HeadcountBasis, "")
	c, err = f.s.Judge(ctx, nil)
	contractOK(t, err)
	contractEqual(t, c, store.Counts{})
	contractEqual(t, contractState(t, f), after)
	f.reopen()
	persisted, err := f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	contractEqual(t, persisted, got)
	// Gone PPP evidence and its estimate no longer contribute.
	_, _, err = f.s.MarkGone(ctx, ppp.Name, "absent")
	contractOK(t, err)
	_, err = f.s.JudgeAll(ctx)
	contractOK(t, err)
	got, err = f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	near(t, got.Operating, .776)
	contractEqual(t, *got.RevenueLow, int64(2100000))
	a.Digest = "aged"
	a.Reading.Filed = time.Date(2023, 4, 15, 0, 0, 0, 0, time.UTC)
	contractAttach(t, f, "aged", a)
	_, err = f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	got, err = f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	near(t, got.Operating, .608)
	near(t, got.RevenueConfidence, .7)
}

// R-MUPS-FJX3
func TestConcurrentIngestAndJudgments(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	a := contractObservation("a", "Alpha", f.now)
	b := contractObservation("b", "Beta", f.now)
	ids := contractAttach(t, f, "run", a, b)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	added := make(chan []store.Enrichment, 1)
	for _, fn := range []func() error{func() error {
		_, _, e := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids[0], Other: ids[1], Why: "same", UserID: "owner"})
		return e
	}, func() error {
		b.SourceID = "extra"
		_, _, e := f.s.Attach(ctx, "next", false, []store.Observation{b})
		return e
	}, func() error { _, _, e := f.s.MarkGone(ctx, a.Scraper, "next"); return e }, func() error { _, e := f.s.Judge(ctx, ids); return e }, func() error { _, e := f.s.JudgeAll(ctx); return e }, func() error { return f.s.FillFacts(ctx) }, func() error { _, _, e := f.s.Derive(ctx, "link_facts", "", false, 10); return e }, func() error {
		results, e := f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: ids[1], Kind: sources.WebsiteCheck, Inputs: "fixture.test", Source: "fixture.test", Found: websiteFound(website.Result{State: sources.WebsiteHTTPS, URL: "https://race.example.test/"})}})
		added <- results
		return e
	}} {
		wg.Go(func() { errs <- fn() })
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		contractOK(t, e)
	}
	recorded := <-added
	for i := range recorded {
		recorded[i].Candidate = ids[0]
	}
	survivorResults, e := f.s.Enrichments(ctx, ids[0])
	contractOK(t, e)
	contractEqual(t, onlyWebsite(survivorResults), recorded)
	for _, id := range ids {
		c, e := f.s.GetCandidate(ctx, id)
		contractOK(t, e)
		ls, e := f.s.Links(ctx, id)
		contractOK(t, e)
		es, e := f.s.Enrichments(ctx, id)
		contractOK(t, e)
		if c.Status == store.StatusMerged {
			_, e := f.s.GetFacts(ctx, id)
			contractError(t, e, store.ErrNotFound)
		}
		if c.Status == store.StatusMerged && (len(ls) != 0 || len(es) != 0) {
			t.Fatal("merged candidate retains links or enrichments")
		}
	}
}

// R-5XOA-44OS R-VFA1-PLHH
func TestGivingLinksFillDisplayFields(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	a := contractObservation("older", "Matched business", f.now.AddDate(-1, 0, 0))
	id := contractAttach(t, f, "initial", a)[0]
	b := a
	b.SourceID = "newer"
	b.Scraper = ppp.Name
	b.Reading.Filed = f.now
	b.Reading.Name = "New display"
	b.Reading.Phone = ""
	b.Reading.Email = ""
	b.Reading.Class = ""
	b.Reading.People = nil
	b.Reading.Cargo = nil
	contractAttach(t, f, "current", b)
	c, e := f.s.GetCandidate(ctx, id)
	contractOK(t, e)
	contractEqual(t, c.Name, b.Reading.Name)
	contractEqual(t, c.Phone, a.Reading.Phone)
	contractEqual(t, c.Email, a.Reading.Email)
	contractEqual(t, c.Class, a.Reading.Class)
	contractEqual(t, c.People, a.Reading.People)
	contractEqual(t, c.Cargo, a.Reading.Cargo)
	_, _, e = f.s.MarkGone(ctx, a.Scraper, "absent")
	contractOK(t, e)
	c, e = f.s.GetCandidate(ctx, id)
	contractOK(t, e)
	contractEqual(t, c.Phone, "")
	contractEqual(t, c.Email, "")
	contractEqual(t, c.Class, "")
	contractEqual(t, c.People, []sources.Person{})
	contractEqual(t, c.Cargo, a.Reading.Cargo)
	_, _, e = f.s.MarkGone(ctx, b.Scraper, "absent")
	contractOK(t, e)
	c, e = f.s.GetCandidate(ctx, id)
	contractOK(t, e)
	contractEqual(t, c.Phone, a.Reading.Phone)
}

// R-2ATP-W04E R-2EHF-1BCH
func TestJudgePassTriggerRepeatsAcrossDeliveries(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	input := store.Pass{Kind: store.KindJudge, Origin: store.OriginCron, Trigger: store.TriggerJudge, Delivery: "day-one", UserID: "service", UserEmail: "service@example.test", RequestID: "request"}
	p, e := f.s.AddPass(ctx, input)
	contractOK(t, e)
	contractEqual(t, p.Kind, input.Kind)
	contractEqual(t, p.Origin, input.Origin)
	contractEqual(t, p.Trigger, input.Trigger)
	contractEqual(t, p.Delivery, input.Delivery)
	input.Delivery = "day-two"
	_, e = f.s.AddPass(ctx, input)
	contractError(t, e, store.ErrBusy)
	_, e = f.s.FinishPass(ctx, p.ID, store.Counts{Judged: 7, Unranged: 2})
	contractOK(t, e)
	next, e := f.s.AddPass(ctx, input)
	contractOK(t, e)
	contractEqual(t, next.Trigger, p.Trigger)
	_, e = f.s.FindDelivered(ctx, "", store.TriggerJudge)
	contractError(t, e, store.ErrNotFound)
	_, e = f.s.AddPass(ctx, input)
	contractError(t, e, store.ErrDelivered)
	_, e = f.s.FinishPass(ctx, next.ID, store.Counts{})
	contractOK(t, e)
	input.Kind = store.KindIngest
	input.Origin = store.OriginRun
	input.Delivery = "run-delivery"
	ingest, e := f.s.AddPass(ctx, input)
	contractOK(t, e)
	_, e = f.s.FinishPass(ctx, ingest.ID, store.Counts{})
	contractOK(t, e)
	input.Delivery = "duplicate-run"
	_, e = f.s.AddPass(ctx, input)
	contractError(t, e, store.ErrDelivered)
	f.reopen()
	got, e := f.s.GetPass(ctx, p.ID)
	contractOK(t, e)
	contractEqual(t, got.Kind, p.Kind)
	contractEqual(t, got.Origin, p.Origin)
	contractEqual(t, got.Judged, int64(7))
	contractEqual(t, got.Unranged, int64(2))
}

// R-5HX1-ZJFE R-5KCU-R2WS R-5J4Y-DB63
func TestJudgmentConflictsFollowLinkOrder(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	newer := contractObservation("newer", "Same business", f.now)
	newer.Scraper = fmcsa.Name
	newer.Reading.Estimate = nil
	newer.Reading.Category = sources.CategoryTrucking
	newer.Reading.GovernmentMatch = "newer-pattern"
	newer.Reading.Evidence = []sources.Evidence{{Name: "equal", Source: "first", Weight: .4}, {Name: "maximum", Source: "weaker", Weight: .1}, {Name: "zero", Source: "zero", Weight: 0}, {Name: "negative", Source: "negative", Weight: -.1}}
	older := newer
	older.SourceID = "older"
	older.Reading.Filed = f.now.AddDate(-1, 0, 0)
	older.Reading.Estimate = &sources.Estimate{Source: fmcsa.Name, Low: 999, Confidence: .8}
	older.Reading.GovernmentMatch = "older-pattern"
	older.Reading.Evidence = []sources.Evidence{{Name: "equal", Source: "second", Weight: .4}, {Name: "maximum", Source: "strongest", Weight: .8}}
	oldest := older
	oldest.SourceID = "oldest"
	oldest.Reading.Filed = f.now.AddDate(-2, 0, 0)
	oldest.Reading.Evidence = []sources.Evidence{{Name: "maximum", Source: "last-weaker", Weight: .2}}
	id := contractAttach(t, f, "run", oldest, older, newer)[0]
	// Every scraper uses its freshest link, even when an older estimate is larger.
	p := newer
	p.Scraper = ppp.Name
	p.SourceID = "loan-old"
	p.Reading.Filed = f.now.AddDate(-2, 0, 0)
	p.Reading.Estimate = &sources.Estimate{Source: ppp.Name, Low: 1500, Confidence: .3}
	p.Reading.Evidence = nil
	p.Reading.GovernmentMatch = "loan-pattern"
	p.Reading.Category = sources.CategoryConstruction
	q := p
	q.SourceID = "loan-new"
	q.Reading.Filed = f.now.AddDate(-1, 0, 0)
	hi := int64(900)
	q.Reading.Estimate = &sources.Estimate{Source: ppp.Name, Low: 500, High: &hi, Confidence: .7}
	contractAttach(t, f, "run", p, q)
	deriveAll(t, f.s)
	_, e := f.s.Judge(ctx, []string{id})
	contractOK(t, e)
	es, e := f.s.Estimates(ctx, id)
	contractOK(t, e)
	contractEqual(t, es, []sources.Estimate{*q.Reading.Estimate})
	a, e := f.s.GetJudgments(ctx, id)
	contractOK(t, e)
	contractEqual(t, factsAt(ctx, t, f.s, id).FleetVerdict, sources.FleetUnknown)
	contractEqual(t, factsAt(ctx, t, f.s, id).GovernmentMatch, "newer-pattern")
	contractEqual(t, a.Evidence, []sources.Evidence{{Name: "maximum", Source: "strongest", Weight: .8}, {Name: "equal", Source: "first", Weight: .4}})
	contractEqual(t, factsAt(ctx, t, f.s, id).Categories, []sources.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}, {Name: sources.CategoryGovernment, Source: "name"}, {Name: sources.CategoryTrucking, Source: fmcsa.Name}})
	// A different scraper cannot supply an FMCSA fleet verdict.
	q.Reading.FleetVerdict = sources.FleetPass
	q.Digest = "fleet-change"
	contractAttach(t, f, "next", q)
	deriveAll(t, f.s)
	_, e = f.s.Judge(ctx, []string{id})
	contractOK(t, e)
	a, e = f.s.GetJudgments(ctx, id)
	contractOK(t, e)
	contractEqual(t, factsAt(ctx, t, f.s, id).FleetVerdict, sources.FleetUnknown)
}

// R-5J4Y-DB63 R-5LKR-4UNH
func TestJudgmentFleetUsesOnlyFreshestLiveFMCSA(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	o := contractObservation("carrier", "Fleet business", f.now)
	o.Scraper = fmcsa.Name
	o.Reading.FleetVerdict = sources.FleetPass
	o.Reading.PowerUnits = ptrInt(3)
	o.Reading.Drivers = ptrInt(4)
	id := contractAttach(t, f, "first", o)[0]
	for _, tc := range []struct {
		verdict        string
		units, drivers *int64
	}{
		{sources.FleetPass, ptrInt(3), ptrInt(4)}, {sources.FleetFail, ptrInt(1), ptrInt(4)}, {sources.FleetUnknown, nil, nil}, {"", nil, nil},
	} {
		o.Reading.FleetVerdict = tc.verdict
		o.Reading.PowerUnits = tc.units
		o.Reading.Drivers = tc.drivers
		o.Digest += "x"
		contractAttach(t, f, "first", o)
		deriveAll(t, f.s)
		_, e := f.s.Judge(ctx, []string{id})
		contractOK(t, e)
		a, e := f.s.GetJudgments(ctx, id)
		contractOK(t, e)
		verdict := tc.verdict
		if verdict == "" {
			verdict = sources.FleetUnknown
		}
		contractEqual(t, factsAt(ctx, t, f.s, id).FleetVerdict, verdict)
		contractEqual(t, factsAt(ctx, t, f.s, id).PowerUnits, tc.units)
		contractEqual(t, factsAt(ctx, t, f.s, id).Drivers, tc.drivers)
		f.reopen()
		b, e := f.s.GetJudgments(ctx, id)
		contractOK(t, e)
		contractEqual(t, b, a)
	}
	o.Reading.FleetVerdict = sources.FleetFail
	o.Reading.PowerUnits = ptrInt(1)
	o.Digest += "x"
	contractAttach(t, f, "current", o)
	older := o
	older.SourceID = "older"
	older.Reading.Filed = f.now.AddDate(-2, 0, 0)
	older.Reading.FleetVerdict = sources.FleetPass
	older.Reading.PowerUnits = ptrInt(5)
	older.Reading.Drivers = ptrInt(6)
	contractAttach(t, f, "older", older)
	loan := older
	loan.Scraper = ppp.Name
	loan.SourceID = "loan"
	loan.Reading.Filed = f.now.AddDate(1, 0, 0)
	contractAttach(t, f, "loan", loan)
	check := func(verdict string, units, drivers *int64) {
		t.Helper()
		deriveAll(t, f.s)
		_, e := f.s.Judge(ctx, []string{id})
		contractOK(t, e)
		_, e = f.s.GetJudgments(ctx, id)
		contractOK(t, e)
		contractEqual(t, factsAt(ctx, t, f.s, id).FleetVerdict, verdict)
		contractEqual(t, factsAt(ctx, t, f.s, id).PowerUnits, units)
		contractEqual(t, factsAt(ctx, t, f.s, id).Drivers, drivers)
	}
	check(sources.FleetFail, ptrInt(1), nil)
	_, _, e := f.s.MarkGone(ctx, fmcsa.Name, "older")
	contractOK(t, e)
	check(sources.FleetPass, ptrInt(5), ptrInt(6))
	_, _, e = f.s.MarkGone(ctx, fmcsa.Name, "none")
	contractOK(t, e)
	check(sources.FleetUnknown, nil, nil)
}

// R-Q42O-WIPN R-5LKR-4UNH
func TestJudgeAllAgreesWithExplicitActiveIDs(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 42}))
	ids := contractAttach(t, f, "run", contractObservation("a", "Alpha", f.now), contractObservation("b", "Beta", f.now))
	prospectList, e := f.s.CreateProspectList(ctx, store.ProspectList{Name: "Preserved", Size: store.ProspectListSizeMax, CreatedUserID: "owner"})
	contractOK(t, e)
	_, e = f.s.Reject(ctx, store.Reject{Kind: store.KindReject, ProspectList: prospectList.ID, Candidate: ids[0], Why: "Owner choice", UserID: "owner"})
	contractOK(t, e)
	rejects, e := f.s.Rejects(ctx, ids[0])
	contractOK(t, e)
	explicit, e := f.s.Judge(ctx, ids)
	contractOK(t, e)
	before := contractState(t, f)
	all, e := f.s.JudgeAll(ctx)
	contractOK(t, e)
	contractEqual(t, all, explicit)
	contractEqual(t, contractState(t, f), before)
	persisted, e := f.s.GetProspectList(ctx, prospectList.ID)
	contractOK(t, e)
	contractEqual(t, persisted, prospectList)
	afterRejects, e := f.s.Rejects(ctx, ids[0])
	contractOK(t, e)
	contractEqual(t, afterRejects, rejects)
}

// R-5P8G-A5VK
func TestRulingJudgmentsUpdateProspectListMembership(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := contractAttach(t, f, "run", contractObservation("a", "Alpha", f.now), contractObservation("b", "Beta", f.now))
	deriveAll(t, f.s)
	_, e := f.s.JudgeAll(ctx)
	contractOK(t, e)
	prospectList, e := f.s.CreateProspectList(ctx, store.ProspectList{Name: "Membership", Size: store.ProspectListSizeMax, CreatedUserID: "owner"})
	contractOK(t, e)
	for _, kind := range []string{store.KindExclude, store.KindInclude} {
		untouched, e := f.s.GetJudgments(ctx, ids[1])
		contractOK(t, e)
		es, e := f.s.Estimates(ctx, ids[1])
		contractOK(t, e)
		f.now = f.now.Add(time.Hour)
		_, cs, e := f.s.Decide(ctx, store.Ruling{Kind: kind, Candidate: ids[0], Why: "Owner ruling", UserID: "owner"})
		contractOK(t, e)
		contractEqual(t, len(cs), 1)
		got, e := f.s.GetJudgments(ctx, ids[0])
		contractOK(t, e)
		contractEqual(t, got.Excluded, kind == store.KindExclude)
		contractEqual(t, got.Calculated, f.now.UTC().Truncate(time.Second))
		other, e := f.s.GetJudgments(ctx, ids[1])
		contractOK(t, e)
		contractEqual(t, other, untouched)
		after, e := f.s.Estimates(ctx, ids[1])
		contractOK(t, e)
		contractEqual(t, after, es)
		read, e := f.s.ReadProspectList(ctx, prospectList.ID)
		contractOK(t, e)
		want := 2
		if kind == store.KindExclude {
			want = 1
		}
		contractEqual(t, read.Matching, int64(want))
		contractEqual(t, len(read.Members), want)
	}
}

// R-5P8G-A5VK
func TestMovingRulingsWriteTheSameJudgmentsAsJudge(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	a := contractObservation("a", "Same business", f.now)
	a.Scraper = fmcsa.Name
	a.Reading.FleetVerdict = sources.FleetPass
	a.Reading.PowerUnits = ptrInt(5)
	a.Reading.Headcount = &sources.Headcount{Scraper: fmcsa.Name, Kind: sources.HeadcountDrivers, Count: 4, AsOf: a.Reading.Filed}
	b := a
	b.SourceID = "b"
	b.Reading.Filed = f.now.AddDate(0, 1, 0)
	b.Reading.FleetVerdict = sources.FleetFail
	b.Reading.PowerUnits = ptrInt(1)
	b.Reading.Headcount = &sources.Headcount{Scraper: fmcsa.Name, Kind: sources.HeadcountDrivers, Count: 8, AsOf: b.Reading.Filed}
	id := contractAttach(t, f, "run", a, b)[0]
	deriveAll(t, f.s)
	_, e := f.s.JudgeAll(ctx)
	contractOK(t, e)
	split, cs, e := f.s.Decide(ctx, store.Ruling{Kind: store.KindSplit, Candidate: id, Keys: []string{sources.SourceKey(b.Scraper, b.SourceID)}, Why: "Separate businesses", UserID: "owner"})
	contractOK(t, e)
	check := func(cs []store.Candidate) {
		t.Helper()
		for _, c := range cs {
			if c.Status != store.StatusActive {
				continue
			}
			before, e := f.s.GetJudgments(ctx, c.ID)
			contractOK(t, e)
			es, e := f.s.Estimates(ctx, c.ID)
			contractOK(t, e)
			contractEqual(t, before.Calculated, f.now.UTC().Truncate(time.Second))
			_, e = f.s.Judge(ctx, []string{c.ID})
			contractOK(t, e)
			after, e := f.s.GetJudgments(ctx, c.ID)
			contractOK(t, e)
			contractEqual(t, after, before)
			afterE, e := f.s.Estimates(ctx, c.ID)
			contractOK(t, e)
			contractEqual(t, afterE, es)
		}
	}
	check(cs)
	_, e = f.s.GetJudgments(ctx, id)
	contractOK(t, e)
	contractEqual(t, factsAt(ctx, t, f.s, id).FleetVerdict, sources.FleetPass)
	contractEqual(t, factsAt(ctx, t, f.s, id).PowerUnits, ptrInt(5))
	_, e = f.s.GetJudgments(ctx, split.Other)
	contractOK(t, e)
	contractEqual(t, factsAt(ctx, t, f.s, split.Other).FleetVerdict, sources.FleetFail)
	contractEqual(t, factsAt(ctx, t, f.s, split.Other).PowerUnits, ptrInt(1))
	f.now = f.now.Add(time.Hour)
	_, cs, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: id, Other: split.Other, Why: "One business", UserID: "owner"})
	contractOK(t, e)
	check(cs)
	_, e = f.s.GetJudgments(ctx, id)
	contractOK(t, e)
	contractEqual(t, factsAt(ctx, t, f.s, id).FleetVerdict, sources.FleetFail)
	contractEqual(t, factsAt(ctx, t, f.s, id).PowerUnits, ptrInt(1))
	counts, e := f.s.Judge(ctx, []string{id, id, split.Other, "missing"})
	contractOK(t, e)
	contractEqual(t, counts.Judged, int64(1))
}
