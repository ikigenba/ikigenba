package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-B99N-PEQ3 R-ZVNB-PFPG
func TestRankedBasisIncludesMergedHistoryAndWholeSecondStamps(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	survivor := enrichmentCandidate(t, f, "survivor", "survivor@test.invalid")
	other := enrichmentCandidate(t, f, "other", "other@test.invalid")
	_, err := f.s.Judge(ctx, []string{survivor, other})
	contractOK(t, err)
	k := prospectListCreate(t, f, "merge basis")
	check := func(want string) {
		t.Helper()
		r, err := f.s.ReadProspectList(ctx, k.ID)
		contractOK(t, err)
		for _, m := range r.Members {
			c, err := f.s.GetCandidate(ctx, m.Candidate.ID)
			contractOK(t, err)
			contractEqual(t, m.Candidate, c)
			for _, stamp := range []time.Time{c.Created, c.Updated} {
				contractEqual(t, stamp.Location(), time.UTC)
				contractEqual(t, stamp.Nanosecond(), 0)
			}
			if c.ID == survivor {
				contractEqual(t, m.Basis, want)
				es, err := f.s.Enrichments(ctx, survivor)
				contractOK(t, err)
				contractEqual(t, m.Basis, store.BasisOf(es))
			}
		}
	}
	check(store.BasisRecords)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, survivor, "successful brief", false)})
	contractOK(t, err)
	check(store.BasisPartial)
	appendRoute(t, f.s, other, "old route inputs", enrichers.RouteFound{Minutes: ptrInt(84)})
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: survivor, Other: other, Why: "same business", UserID: "owner"})
	contractOK(t, err)
	check(store.BasisEnriched)
	f.reopen()
	check(store.BasisEnriched)
}

// R-B99N-PEQ3
func TestRankedMissingFactsUsesCompleteUnknownValue(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := enrichmentCandidate(t, f, "missing facts", "owner@unknown.test")
	_, err := f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DELETE FROM facts WHERE candidate=?", id)
		return err
	}))
	_, err = f.s.GetFacts(ctx, id)
	contractError(t, err, store.ErrNotFound)
	k := prospectListCreate(t, f, "unknown facts")
	r := prospectListRead(t, f, k)
	contractEqual(t, len(r.Members), 1)
	contractEqual(t, r.Members[0].Facts, store.Facts{
		Candidate: id, FleetVerdict: sources.FleetUnknown, Website: sources.WebsiteUnknown,
		Categories: []sources.Category{}, Records: []sources.Category{}, Headcounts: []sources.Headcount{}, BriefLabels: []string{},
	})
}

// R-ZVNB-PFPG
func TestCandidateReadsNormalizePreviousWriterTimestamps(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := enrichmentCandidate(t, f, "previous timestamps", "previous@test.invalid")
	_, err := f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	k := prospectListCreate(t, f, "previous timestamps")
	created := time.Date(2025, 2, 3, 4, 5, 6, 123456789, time.FixedZone("previous-created", 19800))
	updated := time.Date(2026, 3, 4, 5, 6, 7, 987654321, time.FixedZone("previous-updated", -21600))
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE candidates SET created=?,updated=? WHERE id=?", created.Format(time.RFC3339Nano), updated.Format(time.RFC3339Nano), id)
		return err
	}))
	check := func() {
		t.Helper()
		candidate, err := f.s.GetCandidate(ctx, id)
		contractOK(t, err)
		window, err := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
		contractOK(t, err)
		contractEqual(t, len(window), 1)
		read, err := f.s.ReadProspectList(ctx, k.ID)
		contractOK(t, err)
		contractEqual(t, len(read.Members), 1)
		opened, err := f.s.OpenProspectList(ctx, k.ID, "reader", "reader@test.invalid")
		contractOK(t, err)
		contractEqual(t, len(opened.Members), 1)
		for _, got := range []store.Candidate{candidate, window[0], read.Members[0].Candidate, opened.Members[0].Candidate} {
			contractEqual(t, got.ID, id)
			contractEqual(t, got.Created, created.UTC().Truncate(time.Second))
			contractEqual(t, got.Updated, updated.UTC().Truncate(time.Second))
			for _, stamp := range []time.Time{got.Created, got.Updated} {
				contractEqual(t, stamp.Location(), time.UTC)
				contractEqual(t, stamp.Nanosecond(), 0)
			}
		}
	}
	check()
	f.reopen()
	check()
}
