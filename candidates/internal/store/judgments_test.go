package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"math"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func near(t *testing.T, p *float64, w float64) {
	t.Helper()
	if p == nil || math.Abs(*p-w) > 1e-9 {
		t.Fatalf("got %v want %v", p, w)
	}
}

// R-12LF-GSLK R-BN2B-WQID R-BOA8-AI92 R-BPI4-O9ZR R-JPOG-9EA8
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
	c, err := f.s.Judge(ctx, []string{id, id, "missing", emptyID})
	contractOK(t, err)
	contractEqual(t, c, store.Counts{Judged: 2, Unranged: 1})
	after := contractState(t, f)
	contractEqual(t, after.candidates, beforeCandidates)
	contractEqual(t, after.links[id], beforeLinks)
	contractEqual(t, len(after.rulings[id]), 0)
	got, err := f.s.GetAssessment(ctx, id)
	contractOK(t, err)
	near(t, got.Operating, .8208)
	contractEqual(t, got.OperatingKey, int64(820800))
	contractEqual(t, *got.RevenueLow, int64(2209091))
	contractEqual(t, *got.RevenueHigh, int64(3623636))
	near(t, got.RevenueConfidence, .7)
	contractEqual(t, got.Evidence, []sources.Evidence{active, activity, domain, loan})
	contractEqual(t, got.KnownSince, a.Reading.KnownSince)
	contractEqual(t, got.NewestFiling, a.Reading.Filed)
	contractEqual(t, got.Categories, []sources.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}, {Name: sources.CategoryTrucking, Source: fmcsa.Name}})
	contractEqual(t, got.GovernmentMatch, b.Reading.GovernmentMatch)
	contractEqual(t, *got.Minutes, int64(31))
	contractEqual(t, got.Rules, sources.Rules)
	contractEqual(t, got.Calculated, f.now.Truncate(time.Second))
	es, err := f.s.Estimates(ctx, id)
	contractOK(t, err)
	contractEqual(t, es, []sources.Estimate{*a.Reading.Estimate, *b.Reading.Estimate})
	blank, err := f.s.GetAssessment(ctx, emptyID)
	contractOK(t, err)
	contractEqual(t, blank.Operating, (*float64)(nil))
	contractEqual(t, blank.OperatingKey, int64(-1))
	contractEqual(t, blank.RevenueLow, (*int64)(nil))
	contractEqual(t, blank.RevenueHigh, (*int64)(nil))
	contractEqual(t, blank.RevenueConfidence, (*float64)(nil))
	contractEqual(t, blank.Evidence, []sources.Evidence{})
	c, err = f.s.Judge(ctx, nil)
	contractOK(t, err)
	contractEqual(t, c, store.Counts{})
	contractEqual(t, contractState(t, f), after)
	f.reopen()
	persisted, err := f.s.GetAssessment(ctx, id)
	contractOK(t, err)
	contractEqual(t, persisted, got)
	// Gone PPP evidence and its largest estimate no longer contribute.
	_, _, err = f.s.MarkGone(ctx, ppp.Name, "absent")
	contractOK(t, err)
	_, err = f.s.JudgeAll(ctx)
	contractOK(t, err)
	got, err = f.s.GetAssessment(ctx, id)
	contractOK(t, err)
	near(t, got.Operating, .776)
	contractEqual(t, *got.RevenueLow, int64(2100000))
	a.Digest = "aged"
	a.Reading.Filed = time.Date(2023, 4, 15, 0, 0, 0, 0, time.UTC)
	contractAttach(t, f, "aged", a)
	_, err = f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	got, err = f.s.GetAssessment(ctx, id)
	contractOK(t, err)
	near(t, got.Operating, .608)
	near(t, got.RevenueConfidence, .7)
}

// R-BUDQ-7CYJ R-2L6H-C4VW R-13TB-UKC9
func TestShortlistIncludesEmptyJudgmentsAndOwnerControlsExclusion(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 0, "55073": 120, "55074": 121}))
	ids := []string{}
	for i, weight := range []float64{.0000016, .0000015, 0, .8, .9} {
		o := contractObservation(string(rune('a'+i)), string(rune('A'+i)), f.now)
		o.Reading.Estimate = nil
		o.Reading.Fleet = false
		o.Reading.GovernmentMatch = "fixture"
		o.Reading.Cargo = []string{"genfreight"}
		if i == 0 {
			o.Reading.Zip = "55073"
		}
		if i == 1 {
			o.Reading.Zip = "55074"
		}
		if i == 2 {
			o.Reading.Zip = "99999"
		}
		if weight > 0 {
			o.Reading.Evidence = []sources.Evidence{{Name: sources.EvidenceOwnDomain, Source: o.Scraper, Weight: weight}}
		}
		ids = append(ids, contractAttach(t, f, "run", o)[0])
	}
	unjudged := contractAttach(t, f, "run", contractObservation("unjudged", "Unjudged", f.now))[0]
	_, err := f.s.Judge(ctx, ids)
	contractOK(t, err)
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids[3], Other: ids[4], Why: "same", UserID: "owner"})
	contractOK(t, err)
	rs, err := f.s.Shortlist(ctx, store.ShortlistFilter{}, 0, "", 20)
	contractOK(t, err)
	contractEqual(t, len(rs), 4)
	contractEqual(t, rs[1].Candidate.ID, ids[0])
	contractEqual(t, rs[2].Candidate.ID, ids[1])
	contractEqual(t, rs[3].Candidate.ID, ids[2])
	for _, r := range rs {
		if r.Candidate.ID == unjudged {
			t.Fatal("unjudged ranked")
		}
	}
	rs, err = f.s.Shortlist(ctx, store.ShortlistFilter{MaxMinutes: ptrInt(120)}, 0, "", 20)
	contractOK(t, err)
	contractEqual(t, len(rs), 2)
	rs, err = f.s.Shortlist(ctx, store.ShortlistFilter{MaxMinutes: ptrInt(0)}, 0, "", 20)
	contractOK(t, err)
	contractEqual(t, len(rs), 1)
	rs, err = f.s.Shortlist(ctx, store.ShortlistFilter{MinRevenue: ptrInt(0)}, 0, "", 20)
	contractOK(t, err)
	contractEqual(t, len(rs), 0)
	for _, kind := range []string{store.KindExclude, store.KindInclude} {
		_, _, err = f.s.Decide(ctx, store.Ruling{Kind: kind, Candidate: ids[0], Why: "owner decision", UserID: "owner"})
		contractOK(t, err)
		a, e := f.s.GetAssessment(ctx, ids[0])
		contractOK(t, e)
		contractEqual(t, a.Excluded, kind == store.KindExclude)
		rs, e = f.s.Shortlist(ctx, store.ShortlistFilter{}, 0, "", 20)
		contractOK(t, e)
		want := 4
		if kind == store.KindExclude {
			want = 3
		}
		contractEqual(t, len(rs), want)
	}
}

// R-5MCP-YOO7
func TestConcurrentResolutionAndJudgments(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	a := contractObservation("a", "Alpha", f.now)
	b := contractObservation("b", "Beta", f.now)
	ids := contractAttach(t, f, "run", a, b)
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for _, fn := range []func() error{func() error {
		_, _, e := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids[0], Other: ids[1], Why: "same", UserID: "owner"})
		return e
	}, func() error {
		b.SourceID = "extra"
		_, _, e := f.s.Attach(ctx, "next", false, []store.Observation{b})
		return e
	}, func() error { _, _, e := f.s.MarkGone(ctx, a.Scraper, "next"); return e }, func() error { _, e := f.s.Judge(ctx, ids); return e }, func() error { _, e := f.s.JudgeAll(ctx); return e }} {
		wg.Go(func() { errs <- fn() })
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		contractOK(t, e)
	}
	for _, id := range ids {
		c, e := f.s.GetCandidate(ctx, id)
		contractOK(t, e)
		ls, e := f.s.Links(ctx, id)
		contractOK(t, e)
		if c.Status == store.StatusMerged && len(ls) != 0 {
			t.Fatal("merged candidate retains links")
		}
	}
}

// R-38CK-LRZ3
func TestPreviousWriterAssessmentHasNoJudgment(t *testing.T) {
	f := contractDB(t)
	id := contractAttach(t, f, "run", contractObservation("old", "Old writer", f.now))[0]
	insertOldAssessment(t, f, id)
	a, e := f.s.GetAssessment(context.Background(), id)
	contractOK(t, e)
	contractEqual(t, a, store.Assessment{Candidate: id, OperatingKey: -1, Evidence: []sources.Evidence{}, Categories: []sources.Category{}, Fleet: true, Minutes: ptrInt(17), Rules: sources.Rules, Calculated: f.now.UTC().Truncate(time.Second)})
}
func insertOldAssessment(t *testing.T, f *contractFixture, id string) {
	t.Helper()
	contractOK(t, f.d.Write(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.ExecContext(context.Background(), `INSERT INTO assessments(candidate,fleet,low,high,confidence,filed,minutes,exclusions,excluded,base,rules,scored,effective,score,score_key) VALUES (?,1,900,1000,.8,'',17,'[]',0,800,?,?,.7,700,700000)`, id, sources.Rules, f.now.UTC().Truncate(time.Second).Format(time.RFC3339))
		return e
	}))
}

// R-5YW6-HWFH
func TestUpgradeClearsOldJudgmentsAndRepairsCargo(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	contractOK(t, f.d.Close())
	old := fstest.MapFS{}
	entries, e := fs.ReadDir(candidates.Migrations(), ".")
	contractOK(t, e)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "0009") {
			continue
		}
		b, err := fs.ReadFile(candidates.Migrations(), entry.Name())
		contractOK(t, err)
		old[entry.Name()] = &fstest.MapFile{Data: b}
	}
	// Use a separate old-schema path; the initialized fixture path already has 0009.
	f.path += ".previous"
	f.d, e = db.Open(ctx, db.Config{Path: f.path, Migrations: old, Now: func() time.Time { return f.now }})
	contractOK(t, e)
	id := "cand_00000000000000aa"
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO candidates(id,status,merged_into,name,names,names_lower,street,city,state,zip,county,phone,email,people,class,cargo,created,updated) VALUES (?,'active','','Old','["Old"]','old','','','MN','55072','','','','[]','','["genfreight"]',?,?)`, id, f.now.UTC().Format(time.RFC3339), f.now.UTC().Format(time.RFC3339))
		return err
	}))
	insertOldAssessment(t, f, id)
	for i, trig := range []string{store.TriggerRebuild, "old-run"} {
		delivery := ""
		if i == 1 {
			delivery = "old-event"
		}
		contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO passes(id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery) VALUES (?,?,'done','',?,?,1,2,3,4,5,6,7,8,9,'owner','','',?)`, store.PassIDPrefix+strings.Repeat([]string{"a", "b"}[i], 16), trig, f.now.UTC().Format(time.RFC3339), f.now.UTC().Format(time.RFC3339), delivery)
			return err
		}))
	}
	f.reopen()
	_, e = f.s.GetAssessment(ctx, id)
	contractError(t, e, store.ErrNotFound)
	ps, e := f.s.ListPasses(ctx, "", "", 10)
	contractOK(t, e)
	contractEqual(t, len(ps), 2)
	for i, trig := range []string{store.TriggerRebuild, "old-run"} {
		kind, origin, delivery := store.KindRebuild, store.OriginRebuild, ""
		if i == 1 {
			kind, origin, delivery = store.KindIngest, store.OriginRun, "old-event"
		}
		pid := store.PassIDPrefix + strings.Repeat([]string{"a", "b"}[i], 16)
		got, err := f.s.GetPass(ctx, pid)
		contractOK(t, err)
		stamp := f.now.UTC().Truncate(time.Second)
		contractEqual(t, got, store.Pass{ID: pid, Kind: kind, Origin: origin, Trigger: trig, Status: store.StatusDone, Started: stamp, Finished: stamp, Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, UserID: "owner", Delivery: delivery})
	}

	_, e = f.s.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "old-run", Delivery: "new-event"})
	contractError(t, e, store.ErrDelivered)
	_, e = f.s.JudgeAll(ctx)
	contractOK(t, e)
	rs, e := f.s.Shortlist(ctx, store.ShortlistFilter{Cargo: "genfreight"}, 0, "", 10)
	contractOK(t, e)
	contractEqual(t, len(rs), 1)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE candidates SET cargo='["machinery"]' WHERE id=?`, id)
		return err
	}))
	_, e = f.s.JudgeAll(ctx)
	contractOK(t, e)
	rs, e = f.s.Shortlist(ctx, store.ShortlistFilter{Cargo: "genfreight"}, 0, "", 10)
	contractOK(t, e)
	contractEqual(t, len(rs), 0)
	rs, e = f.s.Shortlist(ctx, store.ShortlistFilter{Cargo: "machinery"}, 0, "", 10)
	contractOK(t, e)
	contractEqual(t, len(rs), 1)
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

// R-2BFA-9YYC R-2HIS-6TNT R-2IQO-KLEI R-2JYK-YD57
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

// R-BN2B-WQID R-BOA8-AI92 R-BPI4-O9ZR
func TestJudgmentConflictsFollowLinkOrder(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	newer := contractObservation("newer", "Same business", f.now)
	newer.Scraper = fmcsa.Name
	newer.Reading.Estimate = nil
	newer.Reading.Fleet = false
	newer.Reading.Category = sources.CategoryTrucking
	newer.Reading.GovernmentMatch = "newer-pattern"
	newer.Reading.Evidence = []sources.Evidence{{Name: "equal", Source: "first", Weight: .4}, {Name: "maximum", Source: "weaker", Weight: .1}, {Name: "zero", Source: "zero", Weight: 0}, {Name: "negative", Source: "negative", Weight: -.1}}
	older := newer
	older.SourceID = "older"
	older.Reading.Filed = f.now.AddDate(-1, 0, 0)
	older.Reading.Estimate = &sources.Estimate{Source: fmcsa.Name, Low: 999, Confidence: .8}
	older.Reading.Fleet = true
	older.Reading.GovernmentMatch = "older-pattern"
	older.Reading.Evidence = []sources.Evidence{{Name: "equal", Source: "second", Weight: .4}, {Name: "maximum", Source: "strongest", Weight: .8}}
	oldest := older
	oldest.SourceID = "oldest"
	oldest.Reading.Filed = f.now.AddDate(-2, 0, 0)
	oldest.Reading.Evidence = []sources.Evidence{{Name: "maximum", Source: "last-weaker", Weight: .2}}
	id := contractAttach(t, f, "run", oldest, older, newer)[0]
	// Equal PPP lows select the first in link order, independent of insertion order.
	p := newer
	p.Scraper = ppp.Name
	p.SourceID = "loan-old"
	p.Reading.Filed = f.now.AddDate(-2, 0, 0)
	p.Reading.Estimate = &sources.Estimate{Source: ppp.Name, Low: 500, Confidence: .3}
	p.Reading.Fleet = false
	p.Reading.Evidence = nil
	p.Reading.GovernmentMatch = "loan-pattern"
	p.Reading.Category = sources.CategoryConstruction
	q := p
	q.SourceID = "loan-new"
	q.Reading.Filed = f.now.AddDate(-1, 0, 0)
	hi := int64(900)
	q.Reading.Estimate = &sources.Estimate{Source: ppp.Name, Low: 500, High: &hi, Confidence: .7}
	contractAttach(t, f, "run", p, q)
	_, e := f.s.Judge(ctx, []string{id})
	contractOK(t, e)
	es, e := f.s.Estimates(ctx, id)
	contractOK(t, e)
	contractEqual(t, es, []sources.Estimate{*q.Reading.Estimate})
	a, e := f.s.GetAssessment(ctx, id)
	contractOK(t, e)
	contractEqual(t, a.Fleet, false)
	contractEqual(t, a.GovernmentMatch, "newer-pattern")
	contractEqual(t, a.Evidence, []sources.Evidence{{Name: "maximum", Source: "strongest", Weight: .8}, {Name: "equal", Source: "first", Weight: .4}})
	contractEqual(t, a.Categories, []sources.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}, {Name: sources.CategoryTrucking, Source: fmcsa.Name}})
	// Fleet is true if a different scraper's freshest link supplies it.
	q.Reading.Fleet = true
	q.Digest = "fleet-change"
	contractAttach(t, f, "next", q)
	_, e = f.s.Judge(ctx, []string{id})
	contractOK(t, e)
	a, e = f.s.GetAssessment(ctx, id)
	contractOK(t, e)
	contractEqual(t, a.Fleet, true)
}

// R-BUDQ-7CYJ R-2L6H-C4VW
func TestShortlistMinimumUsesLowWithInclusiveBoundary(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := []string{}
	for i, low := range []int64{9999999, 10000000, 10000001} {
		o := contractObservation([]string{"below", "at", "above"}[i], []string{"Below", "At", "Above"}[i], f.now)
		hi := int64(999999999)
		o.Reading.Estimate.Low = low
		o.Reading.Estimate.High = &hi
		ids = append(ids, contractAttach(t, f, "run", o)[0])
	}
	_, e := f.s.JudgeAll(ctx)
	contractOK(t, e)
	for _, tc := range []struct {
		min  int64
		want []string
	}{{10000000, ids[1:]}, {10000001, ids[2:]}, {10000002, []string{}}} {
		rs, e := f.s.Shortlist(ctx, store.ShortlistFilter{MinRevenue: &tc.min}, 0, "", 20)
		contractOK(t, e)
		got := []string{}
		for _, r := range rs {
			got = append(got, r.Candidate.ID)
		}
		contractEqual(t, got, tc.want)
	}
}
