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

// R-UW50-EWE1 R-RYPS-0SO4
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

// R-UW50-EWE1
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
