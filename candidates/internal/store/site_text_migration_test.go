package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-OZ33-SANJ
func TestSiteTextMigrationContract(t *testing.T) {
	b, err := fs.ReadFile(candidates.Migrations(), "0013_site_text.sql")
	contractOK(t, err)
	lines := []string{
		`CREATE TABLE pages (seq INTEGER PRIMARY KEY, candidate TEXT NOT NULL, url TEXT NOT NULL, title TEXT NOT NULL DEFAULT '', text TEXT NOT NULL DEFAULT '', size INTEGER NOT NULL DEFAULT 0, fetched TEXT NOT NULL, user_id TEXT NOT NULL DEFAULT '', user_email TEXT NOT NULL DEFAULT '');`,
		`CREATE INDEX pages_candidate ON pages(candidate,url,seq);`,
		`ALTER TABLE passes ADD COLUMN target TEXT;`,
		`ALTER TABLE passes ADD COLUMN enrich_kind TEXT;`,
		`ALTER TABLE passes ADD COLUMN runs INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN runs_failed INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN pages INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN cost INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE facts ADD COLUMN site_text_at TEXT NOT NULL DEFAULT '';`,
	}
	contractEqual(t, string(b), strings.Join(lines, "\n")+"\n")
}

func writeBeforeSiteText(ctx context.Context, tx *sql.Tx, id, pid string) error {
	if err := writePriorCandidate(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO facts(candidate,known_since,government_match,newest_filing,categories,fleet_verdict,power_units,drivers,website,website_url,minutes,drive_minutes_at,link_facts_at,website_check_at) VALUES (?,'2020-01-01T00:00:00Z','pattern','2026-01-01T00:00:00Z','[{"Name":"category","Source":"registry"}]','pass',3,2,'https','https://old.test',42,'2026-01-02T00:00:00Z','','2026-01-03T00:00:00Z')`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO passes(id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery,kind,origin,due,fetched,https,http,"none",derived) VALUES (?,'rebuild','done','','2026-01-01T00:00:00Z','2026-01-01T00:00:01Z',1,2,3,4,5,6,7,8,9,'owner','mail','request','','rebuild','rebuild',10,11,12,13,14,15)`, pid); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO enrichments(candidate,kind,inputs,found,source,at,cost) VALUES (?,?,?,?,?,?,?)`, id, sources.WebsiteCheck, "business.test", string(websiteResult(id, sources.WebsiteHTTPS).Found), "business.test", "2026-01-03T00:00:00Z", .25)
	return err
}
func assertBeforeSiteText(t *testing.T, s *store.Store, id, pid string) {
	t.Helper()
	ctx := context.Background()
	p, err := s.GetPass(ctx, pid)
	contractOK(t, err)
	contractEqual(t, p, store.Pass{ID: pid, Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, Status: store.StatusDone, Started: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Finished: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC), Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, Due: 10, Fetched: 11, HTTPS: 12, HTTP: 13, None: 14, Derived: 15, UserID: "owner", UserEmail: "mail", RequestID: "request"})
	f, err := s.GetFacts(ctx, id)
	contractOK(t, err)
	units, drivers, minutes := int64(3), int64(2), int64(42)
	contractEqual(t, f, store.Facts{Candidate: id, KnownSince: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), GovernmentMatch: "pattern", NewestFiling: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Categories: []sources.Category{{Name: "category", Source: "registry"}}, FleetVerdict: sources.FleetPass, PowerUnits: &units, Drivers: &drivers, Website: sources.WebsiteHTTPS, WebsiteURL: "https://old.test", Minutes: &minutes, Written: []store.Written{{Kind: enrichers.KindDriveMinutes, At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}, {Kind: sources.WebsiteCheck, At: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}}})
	es, err := s.Enrichments(ctx, id)
	contractOK(t, err)
	e := websiteResult(id, sources.WebsiteHTTPS)
	e.Cost = .25
	e.At = time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	contractEqual(t, es, []store.Enrichment{e})
	ps, err := s.Pages(ctx, id)
	contractOK(t, err)
	contractEqual(t, ps, []store.Page{})
}

// R-P0B0-62E8 R-P1IW-JU4X
func TestSiteTextMigrationAndPreviousWriter(t *testing.T) {
	for _, old := range []bool{true, false} {
		t.Run(map[bool]string{true: "upgrade", false: "rollback-writer"}[old], func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "state.db")
			migrations := candidates.Migrations()
			if old {
				migrations = earlierMigrations(t, "0012_facts.sql")
			}
			d, err := db.Open(ctx, db.Config{Path: path, Migrations: migrations})
			contractOK(t, err)
			id, pid := "cand_1111111111111111", "pass_1111111111111111"
			contractOK(t, d.Write(ctx, func(tx *sql.Tx) error { return writeBeforeSiteText(ctx, tx, id, pid) }))
			contractOK(t, d.Close())
			d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
			contractOK(t, err)
			defer func() { contractOK(t, d.Close()) }()
			s := store.New(d, store.Config{Now: func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) }})
			assertBeforeSiteText(t, s, id, pid)
		})
	}
}
