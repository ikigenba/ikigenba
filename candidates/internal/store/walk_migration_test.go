package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-P745-GGX4
func TestWalkMigrationBytes(t *testing.T) {
	t.Parallel()
	got, err := fs.ReadFile(candidates.Migrations(), "0024_walk.sql")
	contractOK(t, err)
	lines := []string{
		`ALTER TABLE links ADD COLUMN walk TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE links ADD COLUMN park TEXT NOT NULL DEFAULT '';`,
		`CREATE INDEX links_domain ON links(domain_key);`,
		`ALTER TABLE park_rules ADD COLUMN walk_ceiling INTEGER NOT NULL DEFAULT 100;`,
		`CREATE TABLE places (walk TEXT NOT NULL, place_id TEXT NOT NULL, park TEXT NOT NULL, name TEXT NOT NULL, address TEXT NOT NULL, primary_type TEXT NOT NULL, website TEXT NOT NULL, phone TEXT NOT NULL, lat REAL NOT NULL, lon REAL NOT NULL, radius REAL NOT NULL, candidate TEXT NOT NULL, founded INTEGER NOT NULL, at TEXT NOT NULL, PRIMARY KEY(walk,place_id)) WITHOUT ROWID;`,
		`CREATE INDEX places_park ON places(park,at);`,
		`CREATE TABLE park_walks (seq INTEGER PRIMARY KEY, pass TEXT NOT NULL, park TEXT NOT NULL, walked TEXT NOT NULL, circles INTEGER NOT NULL, requests INTEGER NOT NULL, found INTEGER NOT NULL, new_candidates INTEGER NOT NULL, cost INTEGER NOT NULL, ceiling_reached INTEGER NOT NULL);`,
		`CREATE INDEX park_walks_park ON park_walks(park,walked);`,
		`CREATE INDEX park_walks_pass ON park_walks(pass,seq);`,
		`ALTER TABLE passes ADD COLUMN parks_walked INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN circles INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN requests INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN found INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN new_candidates INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE passes ADD COLUMN ceiling_reached INTEGER NOT NULL DEFAULT 0;`,
	}
	contractEqual(t, string(got), strings.Join(lines, "\n")+"\n")
}

func migrationWalkPriorPass(id string) store.Pass {
	return store.Pass{
		ID: id, Kind: store.KindParks, Origin: store.OriginStart, Trigger: "prior-parcel-run", ParcelRun: "prior-parcel-run", Status: store.StatusDone,
		Started: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Finished: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
		Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9,
		Due: 10, Fetched: 11, HTTPS: 12, HTTP: 13, None: 14, Derived: 15, Runs: 16, RunsFailed: 17, Pages: 18, Cost: 19,
		ParcelsRead: 20, ByLabel: 21, ByValue: 22, ByCandidate: 23, ParksCreated: 24, ParksUpdated: 25, ParksGone: 26, ParksGrown: 27,
		UserID: "prior-owner", UserEmail: "prior@example.test", RequestID: "prior-request",
	}
}

// The rollback writer names every column available to the preceding store,
// including columns that its current record types no longer expose.
func migrationWalkInsertPass(ctx context.Context, tx *sql.Tx, p store.Pass) error {
	args := []any{p.ID, p.Trigger, p.Status, p.Reason, p.Started.Format(time.RFC3339), p.Finished.Format(time.RFC3339), p.Rows, p.New, p.Changed, p.Unchanged, p.Gone, p.Created, p.Linked, p.Judged, p.Unranged, p.UserID, p.UserEmail, p.RequestID, p.Delivery, p.Kind, p.Origin, p.Due, p.Fetched, p.HTTPS, p.HTTP, p.None, p.Derived, p.Target, p.EnrichKind, p.Runs, p.RunsFailed, p.Pages, p.Cost, p.Judged, p.Scraper, p.ParcelRun, p.ParcelsRead, p.ByLabel, p.ByValue, p.ByCandidate, p.ParksCreated, p.ParksUpdated, p.ParksGone, p.ParksGrown}
	_, err := tx.ExecContext(ctx, `INSERT INTO passes(id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery,kind,origin,due,fetched,https,http,"none",derived,target,enrich_kind,runs,runs_failed,pages,cost,judged,scraper,parcel_run,parcels_read,by_label,by_value,by_candidate,parks_created,parks_updated,parks_gone,parks_grown) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
	return err
}

func migrationWalkInsertLink(ctx context.Context, tx *sql.Tx, l store.Link) error {
	raw, err := json.Marshal(l.Reading)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO links(scraper,source_id,candidate,run,digest,name_key,state,zip,phone_key,domain_key,reading,reason,confidence,first_seen,last_seen,gone,ein_key) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, l.Scraper, l.SourceID, l.Candidate, l.Run, l.Digest, l.Reading.NameKey, l.Reading.State, l.Reading.Zip, l.Reading.PhoneKey, l.Reading.DomainKey, string(raw), l.Reason, l.Confidence, l.FirstSeen.Format(time.RFC3339), l.LastSeen.Format(time.RFC3339), l.Gone, l.Reading.EINKey)
	return err
}

func migrationWalkPriorPark(id, town, state string) store.Park {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return store.Park{ID: id, Name: town, County: "prior-county", Town: town, Zip: "55072", State: state, Number: 1, Lat: 45, Lon: -93, Radius: 250, Parcels: 4, BuildingValue: 750000, GrownFrom: 3, Run: "prior-parcel-run", Created: at, Updated: at.Add(time.Hour)}
}

func migrationWalkInsertPark(ctx context.Context, tx *sql.Tx, p store.Park) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO parks(id,county,town,number,state,zip,lat,lon,radius,parcels,building_value,grown_from,walked,walk_run,found,run,created,updated) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.ID, p.County, p.Town, p.Number, p.State, p.Zip, p.Lat, p.Lon, p.Radius, p.Parcels, p.BuildingValue, p.GrownFrom, "", "", 0, p.Run, p.Created.Format(time.RFC3339), p.Updated.Format(time.RFC3339))
	return err
}

// R-P9JY-80EI
func TestWalkUpgradePreservesPreviousRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0023_parks.sql"), Now: func() time.Time { return now }})
	contractOK(t, err)
	links := make([]store.Link, 2)
	passes := []store.Pass{migrationWalkPriorPass(store.PassIDPrefix + "1234567890abcdef"), migrationWalkPriorPass(store.PassIDPrefix + "abcdef1234567890")}
	passes[1].Kind, passes[1].Origin, passes[1].Trigger, passes[1].ParcelRun = store.KindEnrichTarget, store.OriginEnrich, store.TriggerEnrich, ""
	passes[1].Target, passes[1].EnrichKind = store.CandidateIDPrefix+"abcdef1234567890", "brief"
	passes[1].Delivery = "prior-delivery"
	ps := []store.Park{migrationWalkPriorPark(store.ParkIDPrefix+"1234567890abcdef", "Prior Town", parks.StateActive), migrationWalkPriorPark(store.ParkIDPrefix+"abcdef1234567890", "Other Town", parks.StateGone)}
	for i, id := range []string{store.CandidateIDPrefix + "1234567890abcdef", store.CandidateIDPrefix + "abcdef1234567890"} {
		links[i], _ = priorLink(t, id, []string{"first", "second"}[i], i == 1)
		links[i].Reading.EINKey = "123456789"
		links[i].Reading.DomainKey = "prior.example.test"
		contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
			if e := writePriorCandidate(ctx, tx, id); e != nil {
				return e
			}
			if e := migrationWalkInsertLink(ctx, tx, links[i]); e != nil {
				return e
			}
			if e := migrationWalkInsertPass(ctx, tx, passes[i]); e != nil {
				return e
			}
			return migrationWalkInsertPark(ctx, tx, ps[i])
		}))
	}
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "UPDATE park_rules SET distance=875,min_parcels=5,value_threshold=2300000,derived_run='prior-parcel-run' WHERE id=1")
		return e
	}))
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations(), Now: func() time.Time { return now }})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	s := store.New(d, store.Config{Now: func() time.Time { return now }})
	for i := range links {
		got, e := s.Links(ctx, links[i].Candidate)
		contractOK(t, e)
		contractEqual(t, got, []store.Link{links[i]})
		p, e := s.GetPass(ctx, passes[i].ID)
		contractOK(t, e)
		contractEqual(t, p, passes[i])
		park, e := s.GetPark(ctx, ps[i].ID)
		contractOK(t, e)
		contractEqual(t, park, ps[i])
		walks, e := s.ParkWalks(ctx, park.ID)
		contractOK(t, e)
		contractEqual(t, walks, []store.ParkWalk{})
		found, e := s.ParkPlaces(ctx, park.ID)
		contractOK(t, e)
		contractEqual(t, found, []store.Place{})
		cs, e := s.ParkCandidates(ctx, park.ID)
		contractOK(t, e)
		contractEqual(t, cs, []store.FoundCandidate{})
	}
	ceiling, err := s.WalkCeiling(ctx)
	contractOK(t, err)
	contractEqual(t, ceiling, places.SeedCeiling)
	rules, counties, err := s.ParkRules(ctx)
	contractOK(t, err)
	wantRules := seedRules()
	wantRules.Distance, wantRules.MinParcels, wantRules.ValueThreshold = 875, 5, 2300000
	contractEqual(t, rules, wantRules)
	contractEqual(t, counties, []string{"Aitkin", "Anoka", "Chisago", "Isanti", "Mille Lacs", "Morrison", "Sherburne", "St. Louis", "Stearns"})
}

// R-PARU-LS57
func TestWalkPreviousReleaseWriterPreservesWalks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := contractDB(t)
	editPass, err := f.s.SetParkRules(ctx, store.ParkRulesEdit{WalkCeiling: i64(17)}, parkPass(store.OriginRules))
	contractOK(t, err)
	_, err = f.s.FailPass(ctx, editPass.ID, store.ReasonInterrupted)
	contractOK(t, err)
	park := seedParks(t, f)[0]
	walk, err := f.s.AddPass(ctx, store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: "walker"})
	contractOK(t, err)
	_, err = f.s.RecordSweep(ctx, store.Sweep{Pass: walk.ID, Park: park.ID, Found: []places.Found{{Place: places.Place{ID: "walk-place", Name: "Walk business", Address: "1 Main St, Town, MN 55056, USA", PrimaryType: "manufacturer", Website: "https://walk.example.test", Phone: "(320) 555-0100"}, Circle: places.Circle{Lat: park.Lat, Lon: park.Lon, Radius: park.Radius}}}, Circles: 1, Requests: 1, Cost: places.PriceNanos, CeilingReached: true})
	contractOK(t, err)
	_, err = f.s.FinishWalk(ctx, walk.ID, store.Counts{ParksWalked: 1, Circles: 1, Requests: 1, Found: 1, NewCandidates: 1, Cost: places.PriceNanos}, true)
	contractOK(t, err)
	ceiling, err := f.s.WalkCeiling(ctx)
	contractOK(t, err)
	beforePlaces, err := f.s.ParkPlaces(ctx, park.ID)
	contractOK(t, err)
	beforeParkWalks, err := f.s.ParkWalks(ctx, park.ID)
	contractOK(t, err)
	beforePassWalks, err := f.s.PassWalks(ctx, walk.ID)
	contractOK(t, err)
	candidate := contractAttach(t, f, "previous-attach", contractObservation("previous-source", "Previous business", f.now))[0]
	link, _ := priorLink(t, candidate, "rollback-source", false)
	priorPass := migrationWalkPriorPass(store.PassIDPrefix + "abcdef0123456789")
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		if e := migrationWalkInsertLink(ctx, tx, link); e != nil {
			return e
		}
		return migrationWalkInsertPass(ctx, tx, priorPass)
	}))
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, "UPDATE park_rules SET distance=?,min_parcels=?,value_threshold=? WHERE id=1", 925, 6, 3100000); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, "UPDATE passes SET judged=?,cost=? WHERE id=?", 42, 81, priorPass.ID)
		return e
	}))
	priorPass.Judged, priorPass.Cost = 42, 81
	contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
		values := make([]any, 44)
		ptrs := make([]any, len(values))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if e := tx.QueryRowContext(ctx, `SELECT id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery,kind,origin,due,fetched,https,http,"none",derived,target,enrich_kind,runs,runs_failed,pages,cost,judged,scraper,parcel_run,parcels_read,by_label,by_value,by_candidate,parks_created,parks_updated,parks_gone,parks_grown FROM passes WHERE id=?`, walk.ID).Scan(ptrs...); e != nil {
			return e
		}
		contractEqual(t, values[19], any(store.KindWalk))
		return nil
	}))
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "DELETE FROM passes WHERE id=?", walk.ID)
		return e
	}))
	check := func() {
		t.Helper()
		links, e := f.s.Links(ctx, candidate)
		contractOK(t, e)
		found := false
		for _, got := range links {
			if got.SourceID == link.SourceID && got.Scraper == link.Scraper {
				contractEqual(t, got, link)
				found = true
			}
		}
		contractEqual(t, found, true)
		p, e := f.s.GetPass(ctx, priorPass.ID)
		contractOK(t, e)
		contractEqual(t, p, priorPass)
		c, e := f.s.WalkCeiling(ctx)
		contractOK(t, e)
		contractEqual(t, c, ceiling)
		ps, e := f.s.ParkPlaces(ctx, park.ID)
		contractOK(t, e)
		contractEqual(t, ps, beforePlaces)
		pws, e := f.s.ParkWalks(ctx, park.ID)
		contractOK(t, e)
		contractEqual(t, pws, beforeParkWalks)
		ws, e := f.s.PassWalks(ctx, walk.ID)
		contractOK(t, e)
		contractEqual(t, ws, beforePassWalks)
	}
	check()
	f.reopen()
	check()
}

func migrationWalkCheckPriorParkColumns(t *testing.T, f *contractFixture, ps []store.Park) {
	t.Helper()
	ctx := context.Background()
	for _, p := range ps {
		walks, err := f.s.ParkWalks(ctx, p.ID)
		contractOK(t, err)
		var wantAt, wantPass string
		var wantFound int64
		if len(walks) > 0 {
			wantAt = walks[0].Walked.UTC().Format(time.RFC3339)
			wantPass, wantFound = walks[0].Pass, walks[0].Found
		}
		contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
			var at, pass string
			var found int64
			if e := tx.QueryRowContext(ctx, "SELECT walked,walk_run,found FROM parks WHERE id=?", p.ID).Scan(&at, &pass, &found); e != nil {
				return e
			}
			contractEqual(t, at, wantAt)
			contractEqual(t, pass, wantPass)
			contractEqual(t, found, wantFound)
			return nil
		}))
	}
}

// R-OAZO-GODN
func TestWalkPreviousReleaseParkColumns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := contractDB(t)
	ps := seedParks(t, f)
	migrationWalkCheckPriorParkColumns(t, f, ps)
	first := places.Found{Place: places.Place{ID: "first-walk-place", Name: "First business", Address: "3 Main St, Town, MN 55056, USA"}, Circle: places.Circle{Lat: ps[0].Lat, Lon: ps[0].Lon, Radius: ps[0].Radius}}
	second := places.Found{Place: places.Place{ID: "second-walk-place", Name: "Second business", Address: "7 Main St, Town, MN 55056, USA"}, Circle: first.Circle}
	for i, found := range [][]places.Found{{first}, {first, second}} {
		f.now = f.now.Add(time.Hour)
		p, err := f.s.AddPass(ctx, store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: "walker"})
		contractOK(t, err)
		migrationWalkCheckPriorParkColumns(t, f, ps)
		_, err = f.s.RecordSweep(ctx, store.Sweep{Pass: p.ID, Park: ps[0].ID, Found: found, Circles: 1, Requests: 1, Cost: places.PriceNanos})
		contractOK(t, err)
		migrationWalkCheckPriorParkColumns(t, f, ps)
		_, err = f.s.FinishWalk(ctx, p.ID, store.Counts{ParksWalked: 1, Circles: 1, Requests: 1, Found: int64(i + 1), NewCandidates: 1, Cost: places.PriceNanos}, false)
		contractOK(t, err)
		migrationWalkCheckPriorParkColumns(t, f, ps)
	}
	f.now = f.now.Add(time.Hour)
	applyParkPlan(t, f, parks.Plan{Updates: []parks.Update{
		{ID: ps[0].ID, Cluster: parkCluster("updated-walked", ps[0].Town, ps[0].County, ps[0].Zip, 750000)},
		{ID: ps[1].ID, Cluster: parkCluster("updated-unwalked", ps[1].Town, ps[1].County, ps[1].Zip, 450000)},
	}, Gone: []string{ps[2].ID}})
	migrationWalkCheckPriorParkColumns(t, f, ps)
	f.now = f.now.Add(time.Hour)
	applyParkPlan(t, f, parks.Plan{Gone: []string{ps[0].ID}})
	migrationWalkCheckPriorParkColumns(t, f, ps)
	f.reopen()
	migrationWalkCheckPriorParkColumns(t, f, ps)
}
