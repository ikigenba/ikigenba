package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-XO15-GHZ7 R-RYPS-0SO4
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
