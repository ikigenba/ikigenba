package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func seedBoundedCandidates(t *testing.T, f *contractFixture, n int) []string {
	t.Helper()
	ctx := context.Background()
	reading, err := json.Marshal(sources.Reading{Name: "fixture", NameKey: "fixture", State: "MN", Zip: "55072"})
	contractOK(t, err)
	found, err := json.Marshal(enrichers.LinkFound{Headcounts: []enrichers.Headcount{{Scraper: "fixture", Kind: sources.HeadcountEmployees, Count: 3}}})
	contractOK(t, err)
	ids := make([]string, n)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		cs, err := tx.PrepareContext(ctx, `INSERT INTO candidates(id,status,merged_into,name,names,names_lower,street,city,state,zip,county,phone,email,people,class,cargo,created,updated) VALUES (?,?, '', 'fixture','[]','fixture','','','MN','55072','','','','[]','','[]',?,?)`)
		if err != nil {
			return err
		}
		defer func() { _ = cs.Close() }()
		ls, err := tx.PrepareContext(ctx, `INSERT INTO links(scraper,source_id,candidate,run,digest,name_key,state,zip,phone_key,domain_key,reading,reason,confidence,first_seen,last_seen,gone) VALUES ('fixture',?,?,'old','digest','fixture','MN','55072','','',?,'source_key',1,?,?,0)`)
		if err != nil {
			return err
		}
		defer func() { _ = ls.Close() }()
		es, err := tx.PrepareContext(ctx, `INSERT INTO enrichments(candidate,kind,found,source,at,cost,inputs) VALUES (?,?,?,'fixture',?,0,'older')`)
		if err != nil {
			return err
		}
		defer func() { _ = es.Close() }()
		at := f.now.UTC().Truncate(time.Second).Format(time.RFC3339)
		for i := range n {
			id := fmt.Sprintf("cand_%016x", i+1)
			ids[i] = id
			if _, err = cs.ExecContext(ctx, id, store.StatusActive, at, at); err != nil {
				return err
			}
			if _, err = ls.ExecContext(ctx, fmt.Sprintf("%08d", i), id, string(reading), at, at); err != nil {
				return err
			}
			if _, err = es.ExecContext(ctx, id, enrichers.KindLinkFacts, string(found), at); err != nil {
				return err
			}
		}
		return nil
	}))
	return ids
}

// R-5LEB-MYTX
func TestPagedWorkAllocationBound(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	seedBoundedCandidates(t, f, 100000)
	calls := []struct {
		name string
		call func() error
	}{
		{"ActiveAddressKeys", func() error { _, _, e := f.s.ActiveAddressKeys(ctx, "", 10); return e }},
		{"JudgeAll", func() error { _, _, e := f.s.JudgeAll(ctx, "", 10, f.now); return e }},
		{"FillFacts", func() error { _, e := f.s.FillFacts(ctx, "", 10); return e }},
		{"DueEnrichments", func() error { _, _, e := f.s.DueEnrichments(ctx, enrichers.KindLinkFacts, "", 10); return e }},
		{"MarkGone", func() error { _, _, e := f.s.MarkGone(ctx, "fixture", "new", "", 10); return e }},
	}
	for _, c := range calls {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		err := c.call()
		runtime.ReadMemStats(&after)
		contractOK(t, err)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("%s allocated %d", c.name, allocated)
		if allocated >= 1048576 {
			t.Fatalf("%s allocated %d, bound %d", c.name, allocated, 1048576)
		}
	}
}

// R-KUDX-LYIQ R-5K6F-9738 R-LA8M-KZ5R R-RPQ4-8ONN R-L7ST-TFOD R-PU7U-YTLV R-QM9J-RJNU R-QHDY-8GP2 R-HYGP-I8LU
func TestWalksTouchOnlyTheirPages(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 42}))
	allIDs := seedBoundedCandidates(t, f, 6)
	ids := allIDs[:5]
	merged := allIDs[5]
	_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: ids[2], Why: "fixture", UserID: "owner"})
	contractOK(t, err)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE links SET run=CASE source_id WHEN '00000000' THEN 'new' ELSE run END,gone=CASE source_id WHEN '00000001' THEN 1 ELSE gone END`)
		return e
	}))
	_, err = f.s.FillFacts(ctx, "", 10)
	contractOK(t, err)
	_, err = f.s.Judge(ctx, allIDs)
	contractOK(t, err)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `UPDATE candidates SET status=?,merged_into=? WHERE id=?`, store.StatusMerged, ids[0], merged); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE links SET scraper='merged-fixture' WHERE candidate=?`, merged); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `UPDATE facts SET headcounts='[]'`)
		return e
	}))
	mergedFacts, err := f.s.GetFacts(ctx, merged)
	contractOK(t, err)
	mergedJudge, err := f.s.GetJudgments(ctx, merged)
	contractOK(t, err)
	after := ""
	linkAfter := ""
	old := f.now.Add(-time.Hour)
	_, err = f.s.Judge(ctx, ids)
	contractOK(t, err)
	for page := 0; page < 4; page++ {
		beforeEstimates := map[string][]sources.Estimate{}
		beforeLinks := map[string][]store.Link{}
		beforeCandidates := map[string]store.Candidate{}
		for _, id := range allIDs {
			var e error
			beforeEstimates[id], e = f.s.Estimates(ctx, id)
			contractOK(t, e)
			beforeLinks[id], e = f.s.Links(ctx, id)
			contractOK(t, e)
			beforeCandidates[id], e = f.s.GetCandidate(ctx, id)
			contractOK(t, e)
		}
		next, count, e := f.s.JudgeAll(ctx, after, 2, old)
		contractOK(t, e)
		end := min((page+1)*2, len(ids))
		start := min(page*2, len(ids))
		want := ""
		if start < end {
			want = ids[end-1]
		}
		contractEqual(t, next, want)
		contractEqual(t, count.Judged, int64(end-start))
		fill, e := f.s.FillFacts(ctx, after, 2)
		contractOK(t, e)
		contractEqual(t, fill, want)
		due, ds, e := f.s.DueEnrichments(ctx, enrichers.KindLinkFacts, after, 2)
		contractOK(t, e)
		contractEqual(t, due, want)
		keys, cursor, e := f.s.ActiveAddressKeys(ctx, after, 2)
		contractOK(t, e)
		contractEqual(t, cursor, want)
		contractEqual(t, keys, []string{})
		contractEqual(t, len(ds), end-start)
		for i, d := range ds {
			su, e := f.s.Subject(ctx, ids[start+i])
			contractOK(t, e)
			contractEqual(t, d, store.Due{Candidate: ids[start+i], Inputs: (enrichers.LinkFacts{}).Inputs(su)})
		}
		mark, gone, e := f.s.MarkGone(ctx, "fixture", "new", linkAfter, 2)
		contractOK(t, e)
		markWant := ""
		if start < end {
			markWant = fmt.Sprintf("%08d", end-1)
		}
		contractEqual(t, mark, markWant)
		expectedGone := int64(end - start)
		if page == 0 {
			expectedGone = 0
		}
		contractEqual(t, gone.Gone, expectedGone)
		for i, id := range ids {
			j, e := f.s.GetJudgments(ctx, id)
			contractOK(t, e)
			stamp := f.now.UTC().Truncate(time.Second)
			if i < end {
				stamp = old.UTC().Truncate(time.Second)
			}
			contractEqual(t, j.Calculated, stamp)
			facts, e := f.s.GetFacts(ctx, id)
			if i < end {
				contractOK(t, e)
				contractEqual(t, len(facts.Headcounts), 1)
			} else {
				contractOK(t, e)
				contractEqual(t, len(facts.Headcounts), 0)
				estimates, e := f.s.Estimates(ctx, id)
				contractOK(t, e)
				contractEqual(t, estimates, beforeEstimates[id])
			}
			links, e := f.s.Links(ctx, id)
			contractOK(t, e)
			expected := beforeLinks[id]
			for j := range expected {
				key := expected[j].SourceID
				if key > linkAfter && key <= mark && expected[j].Run != "new" {
					expected[j].Gone = true
				}
			}
			contractEqual(t, links, expected)
			if i >= end {
				c, e := f.s.GetCandidate(ctx, id)
				contractOK(t, e)
				contractEqual(t, c, beforeCandidates[id])
			}
		}
		facts, e := f.s.GetFacts(ctx, merged)
		contractOK(t, e)
		contractEqual(t, facts, mergedFacts)
		judgment, e := f.s.GetJudgments(ctx, merged)
		contractOK(t, e)
		contractEqual(t, judgment, mergedJudge)
		for _, d := range ds {
			if d.Candidate == merged {
				t.Fatal(d)
			}
		}
		after = next
		linkAfter = mark
	}
	beforeInvalid := contractState(t, f)
	beforeInvalidFacts := map[string]store.Facts{}
	for _, id := range allIDs {
		facts, e := f.s.GetFacts(ctx, id)
		contractOK(t, e)
		beforeInvalidFacts[id] = facts
	}
	for _, n := range []int{0, -1} {
		cursor, c, e := f.s.JudgeAll(ctx, "", n, f.now)
		contractError(t, e, store.ErrUnreachable)
		contractEqual(t, cursor, "")
		contractEqual(t, c, store.Counts{})
		cursor, c, e = f.s.MarkGone(ctx, "fixture", "new", "", n)
		contractError(t, e, store.ErrUnreachable)
		contractEqual(t, cursor, "")
		contractEqual(t, c, store.Counts{})
		cursor, e = f.s.FillFacts(ctx, "", n)
		contractError(t, e, store.ErrUnreachable)
		contractEqual(t, cursor, "")
	}
	cursor, counts, err := f.s.JudgeAll(ctx, "", 1, time.Time{})
	contractError(t, err, store.ErrUnreachable)
	contractEqual(t, cursor, "")
	contractEqual(t, counts, store.Counts{})
	for _, arg := range []struct {
		kind string
		n    int
	}{{"absent", 1}, {enrichers.KindLinkFacts, 0}} {
		cursor, ds, e := f.s.DueEnrichments(ctx, arg.kind, "", arg.n)
		contractError(t, e, store.ErrUnreachable)
		contractEqual(t, cursor, "")
		if ds != nil {
			t.Fatal(ds)
		}
	}
	contractEqual(t, contractState(t, f), beforeInvalid)
	for _, id := range allIDs {
		facts, e := f.s.GetFacts(ctx, id)
		contractOK(t, e)
		contractEqual(t, facts, beforeInvalidFacts[id])
	}
}

// R-NMZK-A971 R-I9FS-Y6A3
func TestJudgeWalkUsesOneSuppliedTime(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := seedBoundedCandidates(t, f, 501)
	at := f.now
	clockReads := 0
	f.s = store.New(f.d, store.Config{Now: func() time.Time { clockReads++; return at.Add(time.Duration(clockReads) * time.Second) }, Rand: f.rnd})
	before := make([][]store.Enrichment, len(ids))
	for i, id := range ids {
		var e error
		before[i], e = f.s.Enrichments(ctx, id)
		contractOK(t, e)
	}
	after := ""
	counts := []int64{}
	for {
		f.now = f.now.Add(time.Hour)
		next, c, e := f.s.JudgeAll(ctx, after, 500, at)
		contractOK(t, e)
		counts = append(counts, c.Judged)
		if next == "" {
			break
		}
		after = next
	}
	contractEqual(t, counts, []int64{500, 1, 0})
	contractEqual(t, clockReads, 0)
	for i, id := range ids {
		j, e := f.s.GetJudgments(ctx, id)
		contractOK(t, e)
		contractEqual(t, j.Calculated, at.UTC().Truncate(time.Second))
		es, e := f.s.Enrichments(ctx, id)
		contractOK(t, e)
		contractEqual(t, es, before[i])
		_, e = f.s.GetFacts(ctx, id)
		contractError(t, e, store.ErrNotFound)
	}
}

// R-Y3SS-OY52
func TestTargetedWorkLeavesOtherCandidateAlone(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := seedBoundedCandidates(t, f, 2)
	sort.Strings(ids)
	_, e := f.s.FillFacts(ctx, "", 10)
	contractOK(t, e)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error { _, e := tx.ExecContext(ctx, `UPDATE facts SET headcounts='[]'`); return e }))
	for i := range ids {
		_, _, err := f.s.Attach(ctx, "changed", true, []store.Observation{{Scraper: "fixture", SourceID: fmt.Sprintf("%08d", i), Digest: "changed", Reading: sources.Reading{Name: fmt.Sprintf("changed-%d", i), State: "MN", Zip: "55072"}}})
		contractOK(t, err)
	}
	before, e := f.s.Enrichments(ctx, ids[1])
	contractOK(t, e)
	due, e := f.s.DueAmong(ctx, enrichers.KindLinkFacts, ids[:1], false)
	contractOK(t, e)
	contractEqual(t, len(due), 1)
	contractEqual(t, due[0].Candidate, ids[0])
	count, e := f.s.DeriveAmong(ctx, enrichers.KindLinkFacts, ids[:1], true)
	contractOK(t, e)
	contractEqual(t, count, int64(1))
	after, e := f.s.Enrichments(ctx, ids[1])
	contractOK(t, e)
	contractEqual(t, after, before)
	facts, e := f.s.GetFacts(ctx, ids[1])
	contractOK(t, e)
	contractEqual(t, len(facts.Headcounts), 0)
}

// R-LA8M-KZ5R R-KUDX-LYIQ
func TestDuePageCanBeEmptyBeforeMoreCandidates(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := seedBoundedCandidates(t, f, 3)
	_, e := f.s.DeriveAmong(ctx, enrichers.KindLinkFacts, ids[:2], true)
	contractOK(t, e)
	next, due, e := f.s.DueEnrichments(ctx, enrichers.KindLinkFacts, "", 2)
	contractOK(t, e)
	contractEqual(t, next, ids[1])
	if due == nil || len(due) != 0 {
		t.Fatal(due)
	}
	next, due, e = f.s.DueEnrichments(ctx, enrichers.KindLinkFacts, next, 2)
	contractOK(t, e)
	contractEqual(t, next, ids[2])
	contractEqual(t, len(due), 1)
	contractEqual(t, due[0].Candidate, ids[2])
}
