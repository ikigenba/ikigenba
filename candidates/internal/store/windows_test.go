package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"math"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/scoring"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-YDI7-O1ZN R-YEQ4-1TQC
func TestFindDeliveredUsesNewestMatchingPass(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 0}))
	rebuild := contractNewPass(t, f, store.TriggerRebuild)
	_, err := f.s.FinishPass(ctx, rebuild.ID, store.Counts{})
	contractOK(t, err)
	old := contractNewPass(t, f, "older")
	old, err = f.s.FinishPass(ctx, old.ID, store.Counts{})
	contractOK(t, err)
	f.now = f.now.Add(time.Hour)
	newest := contractNewPass(t, f, "newer")
	newest, err = f.s.FailPass(ctx, newest.ID, store.ReasonScrapes)
	contractOK(t, err)
	tied := contractNewPass(t, f, "same-start")
	contractEqual(t, tied.Started, newest.Started)
	if tied.ID <= newest.ID {
		t.Fatal("fixture must exercise descending id tie order")
	}
	before := contractState(t, f)
	for _, pair := range [][2]string{{old.Delivery, old.Trigger}, {old.Delivery, "unrelated"}, {"another", old.Trigger}} {
		got, e := f.s.FindDelivered(ctx, pair[0], pair[1])
		contractOK(t, e)
		contractEqual(t, got, old)
	}
	got, err := f.s.FindDelivered(ctx, old.Delivery, newest.Trigger)
	contractOK(t, err)
	contractEqual(t, got, newest)
	got, err = f.s.FindDelivered(ctx, newest.Delivery, "unrelated")
	contractOK(t, err)
	contractEqual(t, got, newest)
	got, err = f.s.FindDelivered(ctx, newest.Delivery, tied.Trigger)
	contractOK(t, err)
	contractEqual(t, got, tied)
	got, err = f.s.FindDelivered(ctx, tied.Delivery, newest.Trigger)
	contractOK(t, err)
	contractEqual(t, got, tied)
	for _, pair := range [][2]string{{"", store.TriggerRebuild}, {"", ""}, {"missing", "missing"}} {
		got, e := f.s.FindDelivered(ctx, pair[0], pair[1])
		contractError(t, e, store.ErrNotFound)
		contractEqual(t, got, store.Pass{})
	}
	contractEqual(t, contractState(t, f), before)
	f.d.SetFailing(true)
	_, err = f.s.FindDelivered(ctx, old.Delivery, old.Trigger)
	contractError(t, err, store.ErrUnreachable)
}

// R-4GM5-BBUB R-99SY-SUWV R-IUVQ-GZO0
func TestKeysetWindowsCrossTiesAndEarlierHighIDs(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	filed := f.now.UTC().Truncate(time.Second)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 0}))
	base := f.now
	for i, name := range []string{"one", "two", "three", "four", "five"} {
		f.now = base
		if i == 1 || i == 2 {
			f.now = base.Add(-time.Duration(i) * time.Hour)
		}
		mintByte := byte(100 + i)
		if i >= 3 {
			mintByte = byte(i - 2)
		}
		f.rnd.(*contractRandom).next = mintByte
		o := contractObservation(name, name, filed)
		if i >= 3 {
			o.Reading.Estimate.Low -= int64(i) * 1000000
		}
		contractAttach(t, f, "run", o)
		p := contractNewPass(t, f, name)
		_, err := f.s.FinishPass(ctx, p.ID, store.Counts{})
		contractOK(t, err)
	}
	_, err := f.s.RescoreAll(ctx)
	contractOK(t, err)
	filter := store.ShortlistFilter{MaxMinutes: 0, MinRevenue: 1}
	full, err := f.s.Shortlist(ctx, filter, 123, "", 20)
	contractOK(t, err)
	contractEqual(t, len(full), 5)
	contractEqual(t, full[0].Assessment.ScoreKey, full[1].Assessment.ScoreKey)
	contractEqual(t, full[1].Assessment.ScoreKey, full[2].Assessment.ScoreKey)
	if full[2].Assessment.ScoreKey <= full[3].Assessment.ScoreKey || full[3].Assessment.ScoreKey <= full[4].Assessment.ScoreKey {
		t.Fatal("fixture must have three equal leading scores and two lower scores")
	}
	for _, top := range full[:3] {
		if full[3].Candidate.ID >= top.Candidate.ID {
			t.Fatal("lower-score boundary id must precede every tied leading id")
		}
	}
	var ranked []store.Ranked
	var key int64
	var id string
	for {
		page, e := f.s.Shortlist(ctx, filter, key, id, 2)
		contractOK(t, e)
		if page == nil {
			t.Fatal("nil window")
		}
		if len(page) == 0 {
			break
		}
		ranked = append(ranked, page...)
		last := page[len(page)-1]
		key = last.Assessment.ScoreKey
		id = last.Candidate.ID
		offset, e := f.s.ShortlistOffset(ctx, filter, key, id)
		contractOK(t, e)
		contractEqual(t, offset, int64(len(ranked)))
	}
	contractEqual(t, ranked, full)
	cs, err := f.s.ListCandidates(ctx, store.CandidateFilter{}, "ignored", "", 20)
	contractOK(t, err)
	for _, leading := range cs[:3] {
		if cs[3].ID <= leading.ID {
			t.Fatal("earlier created boundary id must follow every leading id")
		}
	}
	var got []store.Candidate
	var created string
	id = ""
	for {
		page, e := f.s.ListCandidates(ctx, store.CandidateFilter{}, created, id, 2)
		contractOK(t, e)
		if len(page) == 0 {
			break
		}
		got = append(got, page...)
		last := page[len(page)-1]
		created = last.Created.Format(time.RFC3339)
		id = last.ID
	}
	contractEqual(t, got, cs)
	ps, err := f.s.ListPasses(ctx, "ignored", "", 20)
	contractOK(t, err)
	for _, leading := range ps[:3] {
		if ps[3].ID <= leading.ID {
			t.Fatal("earlier started boundary id must follow every leading id")
		}
	}
	var passes []store.Pass
	var started string
	id = ""
	for {
		page, e := f.s.ListPasses(ctx, started, id, 2)
		contractOK(t, e)
		if len(page) == 0 {
			break
		}
		passes = append(passes, page...)
		last := page[len(page)-1]
		started = last.Started.Format(time.RFC3339)
		id = last.ID
	}
	contractEqual(t, passes, ps)
}

// R-4HU1-P3L0
func TestNameFilterUnicodeLiteralAndNoCrossNameMatch(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	for _, name := range []string{"PETITE ÉCOLE LTD", "100% A_B", "1000 AXB", "ACME TRUCK"} {
		o := contractObservation(name, name, f.now)
		o.Reading.Names = []string{name, "ACME"}
		contractAttach(t, f, "run", o)
	}
	for _, name := range []string{"ÉCOLE", "école", "100%", "A_B"} {
		cs, e := f.s.ListCandidates(ctx, store.CandidateFilter{Name: name}, "", "", 2)
		contractOK(t, e)
		contractEqual(t, len(cs), 1)
	}
	cs, e := f.s.ListCandidates(ctx, store.CandidateFilter{Name: "TRUCK\nACME"}, "", "", 2)
	contractOK(t, e)
	if cs == nil || len(cs) != 0 {
		t.Fatal(cs)
	}
}

// R-4P5F-ZQ16 R-9DGN-Y64Y R-4MPN-86JS
func TestRefreshPersistsScoresReadsNoClockAndPreservesRecords(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.now = time.Date(2021, 6, 1, 12, 34, 56, 999, time.UTC)
	o := contractObservation("fresh", "Fresh business", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	o.Reading.Estimate.Low = 2000000
	o.Reading.Estimate.High = nil
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 20}))
	id := contractAttach(t, f, "run", o)[0]
	_, err := f.s.Rescore(ctx, []string{id})
	contractOK(t, err)
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindInclude, Candidate: id, Why: "confirmed fit", UserID: "owner"})
	contractOK(t, err)
	finished := contractNewPass(t, f, "finished-refresh-fixture")
	_, err = f.s.FinishPass(ctx, finished.ID, store.Counts{Rows: 3, Created: 1, Rescored: 1})
	contractOK(t, err)
	contractNewPass(t, f, "running-refresh-fixture")
	original, err := f.s.GetAssessment(ctx, id)
	contractOK(t, err)
	snapshot := contractState(t, f)
	f.now = time.Date(2023, 7, 3, 12, 0, 0, 123, time.UTC)
	before, err := f.s.GetAssessment(ctx, id)
	contractOK(t, err)
	contractEqual(t, before, original)
	calls := 0
	f.s = store.New(f.d, store.Config{Now: func() time.Time { calls++; return f.now }, Rand: f.rnd})
	contractOK(t, f.s.RefreshShortlist(ctx))
	contractEqual(t, calls, 1)
	a, err := f.s.GetAssessment(ctx, id)
	contractOK(t, err)
	assertStoredScores(t, a, f.now.UTC().Truncate(time.Second))
	restored := a
	restored.Effective = original.Effective
	restored.Score = original.Score
	restored.ScoreKey = original.ScoreKey
	contractEqual(t, restored, original)
	nowSnapshot := contractState(t, f)
	nowSnapshot.assessments = snapshot.assessments
	contractEqual(t, nowSnapshot, snapshot)
	_, err = f.s.Shortlist(ctx, store.ShortlistFilter{MaxMinutes: 20}, 0, "", 2)
	contractOK(t, err)
	contractEqual(t, calls, 1)
	f.reopen()
	persisted, err := f.s.GetAssessment(ctx, id)
	contractOK(t, err)
	contractEqual(t, persisted, a)
	reopened := contractState(t, f)
	reopened.assessments = snapshot.assessments
	contractEqual(t, reopened, snapshot)
	f.now = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	contractOK(t, f.s.RefreshShortlist(ctx))
	a, err = f.s.GetAssessment(ctx, id)
	contractOK(t, err)
	assertStoredScores(t, a, f.now)
	rs, err := f.s.Shortlist(ctx, store.ShortlistFilter{MaxMinutes: 20}, 0, "", 2)
	contractOK(t, err)
	contractEqual(t, len(rs), 0)
}

func assertStoredScores(t *testing.T, a store.Assessment, at time.Time) {
	t.Helper()
	e := scoring.Effective(a.Confidence, a.Filed, at)
	score := scoring.Score(scoring.Point(a.Low, a.High), e)
	if math.Abs(a.Effective-e) > 1e-9 || math.Abs(a.Score-score) > 1e-9*math.Max(1, math.Abs(score)) {
		t.Fatalf("scores disagree: %#v at %v", a, at)
	}
	scaled := a.Score * 1000000
	want := int64(scaled)
	if scaled >= 0x1p63 {
		want = math.MaxInt64
	}
	if scaled < -0x1p63 {
		want = math.MinInt64
	}
	contractEqual(t, a.ScoreKey, want)
}

// R-4J1Y-2VBP R-4K9U-GN2E
func TestOldWriterScoresAndCargoAreRepaired(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(map[bool]string{false: "old writer", true: "upgrade"}[upgrade], func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			filed := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			f.now = filed.AddDate(1, 0, 0)
			if upgrade {
				contractOK(t, f.d.Close())
				f.path += "-previous"
				files := fstest.MapFS{}
				entries, e := fs.ReadDir(candidates.Migrations(), ".")
				contractOK(t, e)
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), "0008_") {
						continue
					}
					b, e := fs.ReadFile(candidates.Migrations(), entry.Name())
					contractOK(t, e)
					files[entry.Name()] = &fstest.MapFile{Data: b, Mode: 0600}
				}
				var e2 error
				f.d, e2 = db.Open(ctx, db.Config{Path: f.path, Migrations: files, Now: func() time.Time { return f.now }})
				contractOK(t, e2)
			}
			id := store.CandidateIDPrefix + "1234567890abcdef"
			contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
				_, e := tx.ExecContext(ctx, `INSERT INTO candidates(id,status,merged_into,name,names,names_lower,street,city,state,zip,county,phone,email,people,class,cargo,created,updated) VALUES (?,?,'',?,'["Old business"]','old business','','','MN','55072','','','','[]','for_hire','["genfreight"]',?,?)`, id, store.StatusActive, "Old business", f.now.Format(time.RFC3339), f.now.Format(time.RFC3339))
				if e != nil {
					return e
				}
				_, e = tx.ExecContext(ctx, `INSERT INTO assessments(candidate,fleet,low,high,confidence,filed,minutes,exclusions,excluded,base,rules,scored) VALUES (?,1,2000000,NULL,0.8,?,20,'[]',0,1600000,?,?)`, id, filed.Format(time.RFC3339), sources.Rules, f.now.Format(time.RFC3339))
				return e
			}))
			if upgrade {
				contractOK(t, f.d.Close())
				f.open()
			} else {
				f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
			}
			a, err := f.s.GetAssessment(ctx, id)
			contractOK(t, err)
			contractEqual(t, a.Effective, 0.0)
			contractEqual(t, a.Score, 0.0)
			contractEqual(t, a.ScoreKey, int64(0))
			expected := store.Assessment{Candidate: id, Fleet: true, Low: 2000000, Confidence: 0.8, Filed: filed, Minutes: func() *int64 { n := int64(20); return &n }(), Exclusions: []sources.Exclusion{}, Base: 1600000, Rules: sources.Rules, Scored: f.now}
			contractEqual(t, a, expected)
			filter := store.ShortlistFilter{MaxMinutes: 20, Cargo: "genfreight"}
			rs, err := f.s.Shortlist(ctx, filter, 0, "", 2)
			contractOK(t, err)
			contractEqual(t, len(rs), 0)
			contractOK(t, f.s.RefreshShortlist(ctx))
			rs, err = f.s.Shortlist(ctx, filter, 0, "", 2)
			contractOK(t, err)
			contractEqual(t, len(rs), 1)
			assertStoredScores(t, rs[0].Assessment, f.now)
			contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
				_, e := tx.ExecContext(ctx, `UPDATE candidates SET cargo='["machinery"]' WHERE id=?`, id)
				if e != nil {
					return e
				}
				_, e = tx.ExecContext(ctx, `UPDATE assessments SET low=4000000,base=3200000 WHERE candidate=?`, id)
				return e
			}))
			f.now = time.Date(2023, 7, 3, 0, 0, 0, 0, time.UTC)
			contractOK(t, f.s.RefreshShortlist(ctx))
			rs, err = f.s.Shortlist(ctx, filter, 0, "", 2)
			contractOK(t, err)
			contractEqual(t, len(rs), 0)
			filter.Cargo = "machinery"
			rs, err = f.s.Shortlist(ctx, filter, 0, "", 2)
			contractOK(t, err)
			contractEqual(t, len(rs), 1)
			assertStoredScores(t, rs[0].Assessment, f.now)
			f.reopen()
			a, err = f.s.GetAssessment(ctx, id)
			contractOK(t, err)
			assertStoredScores(t, a, f.now)
			c, err := f.s.GetCandidate(ctx, id)
			contractOK(t, err)
			if !slices.Equal(c.Cargo, []string{"machinery"}) {
				t.Fatal(c.Cargo)
			}
		})
	}
}

// R-98L2-F366 R-99SY-SUWV
func TestPublishedRecordConstruction(t *testing.T) {
	a := store.Assessment{"candidate", true, 2, nil, 0.8, time.Time{}, nil, []sources.Exclusion{}, false, 1.6, 0.4, 0.8, 800000, sources.Rules, time.Time{}}
	ranked := store.Ranked{store.Candidate{ID: a.Candidate}, a}
	observation := store.Observation{"scraper", "source", "digest", sources.Reading{Fleet: ranked.Assessment.Fleet}}
	counts := store.Counts{1, 2, 3, 4, 5, 6, 7, 8, 9}
	filter := store.CandidateFilter{"name", "zip", "phone"}
	shortlist := store.ShortlistFilter{120, 10000000, "class", "cargo"}
	if ranked.Candidate.ID != a.Candidate || !observation.Reading.Fleet || counts.Unranged != 9 || filter.Phone != "phone" || shortlist.Cargo != "cargo" {
		t.Fatal("record construction failed")
	}
}

// R-4MPN-86JS R-4P5F-ZQ16
func TestStoredScoreKeysTruncateSaturateAndRefreshMerged(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 20}))
	f.now = time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)
	filed := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		low        int64
		confidence float64
		key        int64
		date       time.Time
	}{
		{"positive fraction", 1, 0.00000175, 1, filed},
		{"negative fraction", -1, 0.00000175, -1, filed},
		{"positive saturation", 10000000000000, 1, math.MaxInt64, filed},
		{"negative saturation", -10000000000000, 1, math.MinInt64, filed},
		{"zero date", 2000000, 0.8, 0, time.Time{}},
		{"leap date", 2000000, 0.8, 1600000000000, time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC)},
	}
	ids := []string{}
	for _, tc := range cases {
		o := contractObservation(tc.name, tc.name, tc.date)
		o.Reading.Estimate.Low = tc.low
		o.Reading.Estimate.High = nil
		o.Reading.Estimate.Confidence = tc.confidence
		id := contractAttach(t, f, "run", o)[0]
		ids = append(ids, id)
		_, err := f.s.Rescore(ctx, []string{id})
		contractOK(t, err)
		a, e := f.s.GetAssessment(ctx, id)
		contractOK(t, e)
		contractEqual(t, a.ScoreKey, tc.key)
		assertStoredScores(t, a, f.now)
	}
	_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids[0], Other: ids[1], Why: "same business", UserID: "owner"})
	contractOK(t, err)
	before, err := f.s.GetAssessment(ctx, ids[1])
	contractOK(t, err)
	f.now = time.Date(2022, 3, 1, 0, 0, 0, 0, time.UTC)
	contractOK(t, f.s.RefreshShortlist(ctx))
	for _, id := range ids {
		a, e := f.s.GetAssessment(ctx, id)
		contractOK(t, e)
		assertStoredScores(t, a, f.now)
	}
	merged, err := f.s.GetAssessment(ctx, ids[1])
	contractOK(t, err)
	merged.Effective = before.Effective
	merged.Score = before.Score
	merged.ScoreKey = before.ScoreKey
	contractEqual(t, merged, before)
	f.reopen()
	persisted, e := f.s.GetAssessment(ctx, ids[1])
	contractOK(t, e)
	assertStoredScores(t, persisted, f.now)
	snap := contractState(t, f)
	f.d.SetFailing(true)
	contractError(t, f.s.RefreshShortlist(ctx), store.ErrUnreachable)
	f.d.SetFailing(false)
	contractEqual(t, contractState(t, f), snap)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	contractError(t, f.s.RefreshShortlist(canceled), store.ErrUnreachable)
	contractEqual(t, contractState(t, f), snap)
}

// R-9TBC-X6RZ
func TestShortlistOrdersByTruncatedKeyRatherThanFloat(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 0}))
	ids := []string{}
	for i, name := range []string{"lower float", "higher float"} {
		o := contractObservation(name, name, f.now)
		o.Reading.Estimate.Low = 1
		o.Reading.Estimate.High = nil
		o.Reading.Estimate.Confidence = 0.00000175 + float64(i)*0.0000001
		ids = append(ids, contractAttach(t, f, "run", o)[0])
	}
	_, err := f.s.RescoreAll(ctx)
	contractOK(t, err)
	rs, err := f.s.Shortlist(ctx, store.ShortlistFilter{}, 0, "", 2)
	contractOK(t, err)
	contractEqual(t, len(rs), 2)
	contractEqual(t, rs[0].Candidate.ID, ids[0])
	contractEqual(t, rs[1].Candidate.ID, ids[1])
	contractEqual(t, rs[0].Assessment.ScoreKey, int64(1))
	contractEqual(t, rs[1].Assessment.ScoreKey, int64(1))
	if rs[0].Assessment.Score >= rs[1].Assessment.Score {
		t.Fatal("fixture must distinguish float and key order")
	}
}
