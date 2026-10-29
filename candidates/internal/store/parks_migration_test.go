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
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-6ENW-TGML
func TestParksMigrationBytes(t *testing.T) {
	t.Parallel()
	b, e := fs.ReadFile(candidates.Migrations(), "0023_parks.sql")
	contractOK(t, e)
	lines := []string{
		`CREATE TABLE parks (id TEXT PRIMARY KEY, county TEXT NOT NULL, town TEXT NOT NULL, number INTEGER NOT NULL, state TEXT NOT NULL, zip TEXT NOT NULL, lat REAL NOT NULL, lon REAL NOT NULL, radius REAL NOT NULL, parcels INTEGER NOT NULL, building_value INTEGER NOT NULL, grown_from INTEGER NOT NULL DEFAULT 0, walked TEXT NOT NULL DEFAULT '', walk_run TEXT NOT NULL DEFAULT '', found INTEGER NOT NULL DEFAULT 0, run TEXT NOT NULL, created TEXT NOT NULL, updated TEXT NOT NULL);`,
		`CREATE INDEX parks_town ON parks(county,town);`,
		`CREATE TABLE park_parcels (park TEXT NOT NULL, pin TEXT NOT NULL, owners TEXT NOT NULL, street TEXT NOT NULL, city TEXT NOT NULL, zip TEXT NOT NULL, building_value INTEGER, rule TEXT NOT NULL, lat REAL NOT NULL, lon REAL NOT NULL, PRIMARY KEY(park,pin)) WITHOUT ROWID;`,
		`CREATE TABLE park_rules (id INTEGER PRIMARY KEY CHECK (id = 1), distance INTEGER NOT NULL, min_parcels INTEGER NOT NULL, value_threshold INTEGER NOT NULL, derived_run TEXT NOT NULL);`,
		`CREATE TABLE park_labels (county TEXT NOT NULL, position INTEGER NOT NULL, label TEXT NOT NULL, PRIMARY KEY(county,position)) WITHOUT ROWID;`,
		`CREATE TABLE park_counties (county TEXT PRIMARY KEY) WITHOUT ROWID;`,
		`INSERT INTO park_rules (id, distance, min_parcels, value_threshold, derived_run) VALUES (1, 600, 3, 1000000, '');`,
		`INSERT INTO park_labels (county, position, label) VALUES ('Chisago', 1, '3a Industrial Land And Building'), ('Stearns', 1, '3a Industrial Land And Building'), ('Anoka', 1, '3a INDUSTIAL PREFERENTIAL'), ('Aitkin', 1, 'Industrial Preferred'), ('St. Louis', 1, 'Industrial Preferred'), ('St. Louis', 2, 'Industrial Non-Preferred'), ('Sherburne', 1, 'Industrial Preferred'), ('Sherburne', 2, 'Industrial Non-Preferred'), ('Sherburne', 3, 'INDUSTRIAL (with Buildings)'), ('Sherburne', 4, 'Industrial - Bare Land'), ('Mille Lacs', 1, 'Indust Land/Building'), ('Isanti', 1, '3A-Commercial/Industrial/Public Utility'), ('Morrison', 1, '3A-Commercial/Industrial/Public Utility');`,
		`ALTER TABLE passes ADD COLUMN parcel_run TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE passes ADD COLUMN parcels_read INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN by_label INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN by_value INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN by_candidate INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN parks_created INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN parks_updated INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN parks_gone INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN parks_grown INTEGER NOT NULL DEFAULT 0;`,
	}
	contractEqual(t, string(b), strings.Join(lines, "\n")+"\n")
}
func insertPriorParksPass(ctx context.Context, tx *sql.Tx, id string) error {
	_, e := tx.ExecContext(ctx, `INSERT INTO passes(id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery,kind,origin,due,fetched,https,http,"none",derived,target,enrich_kind,runs,runs_failed,pages,cost,judged,scraper) VALUES (?,'ingest','done','','2026-01-01T00:00:00Z','2026-01-01T00:00:01Z',1,2,3,4,5,6,7,0,9,'owner','mail','request','','ingest','ingest',10,11,12,13,14,15,'','',16,17,18,19,8,'fmcsa')`, id)
	return e
}

// R-6FVT-78DA
func TestParksUpgradeExistingRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	d, e := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0022_headcount_trend.sql")})
	contractOK(t, e)
	id, pid := store.CandidateIDPrefix+"1234567890abcdef", store.PassIDPrefix+"1234567890abcdef"
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		if e := writePriorCandidate(ctx, tx, id); e != nil {
			return e
		}
		return insertPriorParksPass(ctx, tx, pid)
	}))
	candidate := store.Candidate{ID: id, Status: store.StatusActive, Name: "old", Names: []string{"old"}, People: []sources.Sourced[sources.Person]{}, Cargo: []string{}, Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Updated: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), OtherNames: []sources.Sourced[string]{}, OtherAddresses: []sources.Sourced[sources.Address]{}, OtherPhones: []sources.Sourced[string]{}, OtherEmails: []sources.Sourced[string]{}}
	contractOK(t, d.Close())
	d, e = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, e)
	defer func() { contractOK(t, d.Close()) }()
	st := store.New(d, store.Config{})
	got, e := st.GetCandidate(ctx, id)
	contractOK(t, e)
	contractEqual(t, got, candidate)
	p, e := st.GetPass(ctx, pid)
	contractOK(t, e)
	want := store.Pass{ID: pid, Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, Scraper: "fmcsa", Status: store.StatusDone, Started: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Finished: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC), Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, Due: 10, Fetched: 11, HTTPS: 12, HTTP: 13, None: 14, Derived: 15, Runs: 16, RunsFailed: 17, Pages: 18, Cost: 19, UserID: "owner", UserEmail: "mail", RequestID: "request"}
	contractEqual(t, p, want)
	r, cs, e := st.ParkRules(ctx)
	contractOK(t, e)
	contractEqual(t, r, seedRules())
	contractEqual(t, len(cs), 9)
	ps, e := st.ListParks(ctx, store.ParkFilter{}, "", "", "", 10)
	contractOK(t, e)
	contractEqual(t, ps, []store.Park{})
	run, e := st.ParksRun(ctx)
	contractOK(t, e)
	contractEqual(t, run, "")
}

// R-6IBL-YRUO
func TestPreviousPassWriterLeavesParks(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	ps := seedParks(t, f)
	prior, e := f.s.PriorParks(ctx)
	contractOK(t, e)
	parcels := map[string][]store.ParkParcel{}
	for _, p := range ps {
		parcels[p.ID], e = f.s.ParkParcels(ctx, p.ID)
		contractOK(t, e)
	}
	rules, counties, e := f.s.ParkRules(ctx)
	contractOK(t, e)
	read, e := f.s.ParkCountiesRead(ctx)
	contractOK(t, e)
	run, e := f.s.ParksRun(ctx)
	contractOK(t, e)
	passes, e := f.s.ListPasses(ctx, "", "", 100)
	contractOK(t, e)
	parkPassID := passes[0].ID
	oldID := store.PassIDPrefix + "abcdef0123456789"
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		if e := insertPriorParksPass(ctx, tx, oldID); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `UPDATE passes SET status='done',finished='2026-01-01T00:00:01Z',judged=42 WHERE id=?`, oldID)
		return e
	}))
	contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
		var kind string
		rows, e := tx.QueryContext(ctx, `SELECT id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery,kind,origin,due,fetched,https,http,"none",derived,target,enrich_kind,runs,runs_failed,pages,cost,judged,scraper FROM passes WHERE id=?`, parkPassID)
		if e != nil {
			return e
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return rows.Err()
		}
		vals := make([]any, 35)
		ptrs := make([]any, 35)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if e = rows.Scan(ptrs...); e != nil {
			return e
		}
		kind = vals[19].(string)
		contractEqual(t, kind, store.KindParks)
		return rows.Err()
	}))
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "DELETE FROM passes WHERE id=?", parkPassID)
		return e
	}))
	p, e := f.s.GetPass(ctx, oldID)
	contractOK(t, e)
	contractEqual(t, p.ParcelRun, "")
	contractEqual(t, []int64{p.ParcelsRead, p.ByLabel, p.ByValue, p.ByCandidate, p.ParksCreated, p.ParksUpdated, p.ParksGone, p.ParksGrown}, make([]int64, 8))
	contractEqual(t, p.Judged, int64(42))
	after, e := f.s.PriorParks(ctx)
	contractOK(t, e)
	contractEqual(t, after, prior)
	for id, want := range parcels {
		got, e := f.s.ParkParcels(ctx, id)
		contractOK(t, e)
		contractEqual(t, got, want)
	}
	rr, cc, e := f.s.ParkRules(ctx)
	contractOK(t, e)
	contractEqual(t, rr, rules)
	contractEqual(t, cc, counties)
	rd, e := f.s.ParkCountiesRead(ctx)
	contractOK(t, e)
	contractEqual(t, rd, read)
	rn, e := f.s.ParksRun(ctx)
	contractOK(t, e)
	contractEqual(t, rn, run)
}
