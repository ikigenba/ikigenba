package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func routeStore(f *contractFixture, origin string) *store.Store {
	return store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd, Origin: origin})
}
func appendRoute(t *testing.T, s *store.Store, id, inputs string, found enrichers.RouteFound) {
	t.Helper()
	b, err := json.Marshal(found)
	contractOK(t, err)
	_, err = s.AddEnrichments(context.Background(), []store.Enrichment{{Candidate: id, Kind: enrichers.KindDriveRoute, Inputs: inputs, Source: enrichers.SourceGoogleRoutes, Found: b}})
	contractOK(t, err)
}

// R-4C6W-T76H R-3156-5D9B R-4AZ0-FFFS R-L1PB-WKYW R-LCOF-CIN5 R-49R4-1NP3
func TestDriveRouteProjectionAndOrigin(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	f.s = routeStore(f, "base")
	su, err := f.s.Subject(ctx, id)
	contractOK(t, err)
	contractEqual(t, su.Origin, "base")
	// This fixture owns its address, so changing it exercises stale-route invalidation.
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "UPDATE candidates SET street='12 Main',city='Town',state='MN',zip='55072' WHERE id=?", id)
		return e
	}))
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 80}))
	_, err = f.s.DeriveAmong(ctx, enrichers.KindDriveMinutes, []string{id}, true)
	contractOK(t, err)
	su, err = f.s.Subject(ctx, id)
	contractOK(t, err)
	inputs := (enrichers.DriveRoute{}).Inputs(su)
	minutes, miles := int64(47), 38.2
	appendRoute(t, f.s, id, inputs, enrichers.RouteFound{Minutes: &minutes, Miles: &miles, Address: "resolved"})
	check := func(source string, n int64) {
		t.Helper()
		g, e := f.s.GetFacts(ctx, id)
		contractOK(t, e)
		contractEqual(t, g.MinutesSource, source)
		contractEqual(t, *g.Minutes, n)
		if source == store.MinutesByRoute {
			contractEqual(t, *g.Miles, miles)
			contractEqual(t, g.RouteAddress, "resolved")
		} else {
			contractEqual(t, g.Miles, (*float64)(nil))
			contractEqual(t, g.RouteAddress, "")
		}
	}
	check(store.MinutesByRoute, 47)
	_, err = f.s.DeriveAmong(ctx, enrichers.KindDriveMinutes, []string{id}, true)
	contractOK(t, err)
	check(store.MinutesByRoute, 47)
	appendRoute(t, f.s, id, inputs, enrichers.RouteFound{Error: "request failed"})
	check(store.MinutesByRoute, 47)
	appendRoute(t, f.s, id, inputs, enrichers.RouteFound{Why: "coarse"})
	check(store.MinutesByRoute, 47)
	f.reopen()
	f.s = routeStore(f, "base")
	check(store.MinutesByRoute, 47)
	// Origin changes make the candidate due and FillFacts repairs its materialized row.
	f.s = routeStore(f, "other base")
	su, err = f.s.Subject(ctx, id)
	contractOK(t, err)
	contractEqual(t, su.Origin, "other base")
	if (enrichers.DriveRoute{}).Inputs(su) == inputs {
		t.Fatal("changed origin kept prior route inputs")
	}
	due, err := f.s.DueAmong(ctx, enrichers.KindDriveRoute, []string{id}, false)
	contractOK(t, err)
	contractEqual(t, len(due), 1)
	contractOK(t, fillFactsWalk(ctx, f.s))
	check(store.MinutesByZip, 80)
	f.s = routeStore(f, "")
	su, err = f.s.Subject(ctx, id)
	contractOK(t, err)
	contractEqual(t, su.Origin, "")
	due, err = f.s.DueAmong(ctx, enrichers.KindDriveRoute, []string{id}, true)
	contractOK(t, err)
	contractEqual(t, len(due), 0)
	f.s = routeStore(f, "base")
	contractOK(t, fillFactsWalk(ctx, f.s))
	check(store.MinutesByRoute, 47)
	obs := contractObservation("evidence", "Seed business", f.now.UTC().Truncate(time.Second))
	obs.Reading.Street = "new street"
	obs.Digest = "changed digest"
	_, _, err = f.s.Attach(ctx, "changed-address", false, []store.Observation{obs})
	contractOK(t, err)
	_, err = f.s.DeriveAmong(ctx, enrichers.KindDriveMinutes, []string{id}, true)
	contractOK(t, err)
	check(store.MinutesByZip, 80)
	before, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractOK(t, fillFactsWalk(ctx, f.s))
	after, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, after, before)
	// Rollback writers can leave either direction of disagreement.
	for _, q := range []string{"UPDATE facts SET minutes_source='' WHERE candidate=?", "UPDATE facts SET minutes=NULL,minutes_source='zip' WHERE candidate=?"} {
		contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error { _, e := tx.ExecContext(ctx, q, id); return e }))
		contractOK(t, fillFactsWalk(ctx, f.s))
		check(store.MinutesByZip, 80)
	}
	f.reopen()
	check(store.MinutesByZip, 80)
}
