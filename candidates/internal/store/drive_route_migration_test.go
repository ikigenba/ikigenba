package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func priorRouteFacts(ctx context.Context, tx *sql.Tx, id string, minutes any, at string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO facts(candidate,known_since,government_match,newest_filing,categories,fleet_verdict,power_units,drivers,website,website_url,minutes,drive_minutes_at,link_facts_at,website_check_at,site_text_at,brief,brief_at,website_source,website_search_at,drive_route_at) VALUES (?,'','','','[]','unknown',NULL,NULL,'unknown','',?,'','','','','','','','',?) ON CONFLICT(candidate) DO UPDATE SET minutes=excluded.minutes,drive_route_at=excluded.drive_route_at`, id, minutes, at)
	return err
}

// R-378O-27YS
func TestDriveRouteUpgradeMarksExistingZIPMinutes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0017_prospect_lists.sql")})
	contractOK(t, err)
	for i, id := range []string{"old-known", "old-unknown"} {
		contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
			if e := writePriorCandidate(ctx, tx, id); e != nil {
				return e
			}
			var minutes any
			if i == 0 {
				minutes = int64(42)
			}
			_, e := tx.ExecContext(ctx, `INSERT INTO facts(candidate,known_since,government_match,newest_filing,categories,fleet_verdict,power_units,drivers,website,website_url,minutes,drive_minutes_at,link_facts_at,website_check_at,site_text_at,brief,brief_at,website_source,website_search_at) VALUES (?,'','','','[]','unknown',NULL,NULL,'unknown','',?,'','','','','','','','')`, id, minutes)
			return e
		}))
	}
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	s := store.New(d, store.Config{})
	for i, id := range []string{"old-known", "old-unknown"} {
		g, e := s.GetFacts(ctx, id)
		contractOK(t, e)
		if i == 0 {
			contractEqual(t, *g.Minutes, int64(42))
			contractEqual(t, g.MinutesSource, store.MinutesByZip)
		} else {
			contractEqual(t, g.Minutes, (*int64)(nil))
			contractEqual(t, g.MinutesSource, "")
		}
		contractEqual(t, g.Miles, (*float64)(nil))
		contractEqual(t, g.RouteAddress, "")
		contractEqual(t, len(g.Written), 0)
	}
}

// R-38GK-FZPH
func TestDriveRoutePreviousWriterPreservesRouteColumns(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	f.s = routeStore(f, "base")
	id := contractSeed(t, f)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "UPDATE candidates SET street='12 Main' WHERE id=?", id)
		return e
	}))
	su, err := f.s.Subject(ctx, id)
	contractOK(t, err)
	n, m := int64(47), 38.2
	appendRoute(t, f.s, id, (enrichers.DriveRoute{}).Inputs(su), enrichers.RouteFound{Minutes: &n, Miles: &m, Address: "resolved"})
	at := "2026-02-03T04:05:06+01:00"
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error { return priorRouteFacts(ctx, tx, id, int64(90), at) }))
	g, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, *g.Minutes, int64(90))
	contractEqual(t, g.MinutesSource, store.MinutesByRoute)
	contractEqual(t, *g.Miles, m)
	contractEqual(t, g.RouteAddress, "resolved")
	want, _ := time.Parse(time.RFC3339, at)
	contractEqual(t, g.Written, []store.Written{{Kind: enrichers.KindDriveRoute, At: want.UTC()}})
	for _, stamp := range []string{"", at} {
		other := enrichmentCandidate(t, f, "prior-"+stamp, "a@b.test")
		contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error { return priorRouteFacts(ctx, tx, other, int64(42), stamp) }))
		g, err = f.s.GetFacts(ctx, other)
		contractOK(t, err)
		contractEqual(t, g.RouteAddress, "")
		contractEqual(t, g.Miles, (*float64)(nil))
		if stamp == "" {
			contractEqual(t, len(g.Written), 0)
		} else {
			contractEqual(t, g.Written, []store.Written{{Kind: enrichers.KindDriveRoute, At: want.UTC()}})
		}
	}
}
