package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-TIPT-NOAU
func TestProspectListMigrationContract(t *testing.T) {
	b, err := fs.ReadFile(candidates.Migrations(), "0010_cohorts.sql")
	contractOK(t, err)
	lines := []string{
		`CREATE TABLE cohorts (id TEXT PRIMARY KEY, name TEXT NOT NULL, filters TEXT NOT NULL, ordering TEXT NOT NULL, size INTEGER NOT NULL, created_user_id TEXT NOT NULL, created_user_email TEXT NOT NULL, created TEXT NOT NULL, changed_user_id TEXT NOT NULL, changed_user_email TEXT NOT NULL, changed TEXT NOT NULL);`,
		`CREATE INDEX cohorts_window ON cohorts(name,id);`,
		`CREATE TABLE rejects (seq INTEGER PRIMARY KEY, cohort TEXT NOT NULL, candidate TEXT NOT NULL, kind TEXT NOT NULL, user_id TEXT NOT NULL, user_email TEXT NOT NULL, at TEXT NOT NULL, why TEXT NOT NULL);`,
		`CREATE INDEX rejects_cohort ON rejects(cohort,candidate,seq);`,
		`CREATE INDEX rejects_candidate ON rejects(candidate,seq);`,
		`ALTER TABLE assessments ADD COLUMN fleet_verdict TEXT NOT NULL DEFAULT 'unknown';`,
		`ALTER TABLE assessments ADD COLUMN power_units INTEGER;`,
		`ALTER TABLE assessments ADD COLUMN drivers INTEGER;`,
		`DROP INDEX assessments_base;`,
		`DROP INDEX assessments_score;`,
		`ALTER TABLE assessments DROP COLUMN confidence;`,
		`ALTER TABLE assessments DROP COLUMN base;`,
		`ALTER TABLE assessments DROP COLUMN effective;`,
		`ALTER TABLE assessments DROP COLUMN score;`,
		`ALTER TABLE assessments DROP COLUMN score_key;`,
		`ALTER TABLE assessments DROP COLUMN filed;`,
		`ALTER TABLE assessments DROP COLUMN exclusions;`,
	}
	contractEqual(t, string(b), strings.Join(lines, "\n")+"\n")
}
func earlierMigrations(t *testing.T, last string) fs.FS {
	t.Helper()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(candidates.Migrations(), ".")
	contractOK(t, err)
	for _, e := range entries {
		if e.Name() <= last {
			b, e2 := fs.ReadFile(candidates.Migrations(), e.Name())
			contractOK(t, e2)
			out[e.Name()] = &fstest.MapFile{Data: b}
		}
	}
	return out
}
func writePriorCandidate(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO candidates(id,status,merged_into,name,names,names_lower,street,city,state,zip,county,phone,email,people,class,cargo,created,updated) VALUES (?,'active','','old','["old"]','old','','','','','','','','[]','','[]','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, id)
	return err
}
func writePriorJudgment(ctx context.Context, tx *sql.Tx, id string, fleet bool) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO assessments(candidate,operating,operating_key,evidence,revenue_low,revenue_high,revenue_confidence,known_since,government_match,newest_filing,categories,fleet,minutes,excluded,rules,scored) VALUES (?,0.6,600000,'[]',100,200,0.7,'2020-01-01T00:00:00Z','pattern','2026-01-01T00:00:00Z','[]',?,42,1,?,'2026-10-01T00:00:00Z')`, id, fleet, sources.Rules)
	return err
}
func assertPriorJudgment(t *testing.T, s *store.Store, id string) {
	t.Helper()
	a, err := s.GetJudgments(context.Background(), id)
	contractOK(t, err)
	op, conf := 0.6, 0.7
	low, high := int64(100), int64(200)
	contractEqual(t, a, store.Judgments{HeadcountSeries: []judging.Series{}, Trend: judging.TrendUnknown, Candidate: id, Operating: &op, OperatingKey: 600000, Evidence: []sources.Evidence{}, RevenueLow: &low, RevenueHigh: &high, RevenueConfidence: &conf, Excluded: true, Rules: sources.Rules, Calculated: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
}

// R-Z5J8-OA8U
func TestPriorJudgmentsAcrossMigration(t *testing.T) {
	for _, fleet := range []bool{false, true} {
		t.Run(map[bool]string{false: "false", true: "true"}[fleet], func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "state.db")
			clock := func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) }
			d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0009_judgments.sql")})
			contractOK(t, err)
			first := "cand_1111111111111111"
			contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
				if e := writePriorCandidate(ctx, tx, first); e != nil {
					return e
				}
				return writePriorJudgment(ctx, tx, first, fleet)
			}))
			contractOK(t, d.Close())
			d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
			contractOK(t, err)
			defer func() { contractOK(t, d.Close()) }()
			s := store.New(d, store.Config{Now: clock, Rand: &contractRandom{next: 1}})
			assertPriorJudgment(t, s, first)
		})
	}
}

// R-YM0U-JYDQ
func TestOldJudgmentsClearWhilePassesRetainTheirMeaning(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0008_windows.sql")})
	contractOK(t, err)
	id := "cand_1111111111111111"
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		if e := writePriorCandidate(ctx, tx, id); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO assessments(candidate,fleet,low,high,confidence,filed,minutes,exclusions,excluded,base,rules,scored,effective,score,score_key) VALUES (?,1,100,200,0.7,'2026-01-01T00:00:00Z',42,'[]',0,0.7,?,'2026-01-01T00:00:00Z',0.7,0.7,700000)`, id, sources.Rules); e != nil {
			return e
		}
		for i, trigger := range []string{"rebuild", "historic-run"} {
			delivery := ""
			if i > 0 {
				delivery = "historic-delivery"
			}
			if _, e := tx.ExecContext(ctx, `INSERT INTO passes(id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery) VALUES (?,?,'done','','2026-01-01T00:00:00Z','2026-01-01T00:00:01Z',1,2,3,4,5,6,7,8,9,'owner','mail','request',?)`, []string{"pass_1111111111111111", "pass_2222222222222222"}[i], trigger, delivery); e != nil {
				return e
			}
		}
		return nil
	}))
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	s := store.New(d, store.Config{Now: func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) }, Rand: &contractRandom{next: 1}})
	a, err := s.GetJudgments(ctx, id)
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, a, store.Judgments{})
	for i, pid := range []string{"pass_1111111111111111", "pass_2222222222222222"} {
		p, e := s.GetPass(ctx, pid)
		contractOK(t, e)
		kind, origin, trigger, delivery := store.KindIngest, store.OriginIngest, store.TriggerIngest, ""
		if i > 0 {
			kind, origin, trigger, delivery = store.KindIngest, store.OriginRun, "historic-run", "historic-delivery"
		}
		contractEqual(t, p, store.Pass{ID: pid, Kind: kind, Origin: origin, Trigger: trigger, Status: store.StatusDone, Started: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Finished: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC), Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, UserID: "owner", UserEmail: "mail", RequestID: "request", Delivery: delivery})
	}
	_, err = s.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "historic-run", Delivery: "another"})
	contractError(t, err, store.ErrDelivered)
}

// R-YZFQ-RFJD
func TestWebsiteMigrationKeepsPreviousJudgments(t *testing.T) {
	for _, fleet := range []bool{false, true} {
		t.Run(map[bool]string{false: "false", true: "true"}[fleet], func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "state.db")
			d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0010_cohorts.sql")})
			contractOK(t, err)
			id, pid := "cand_1111111111111111", "pass_1111111111111111"
			contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
				if e := writePriorCandidate(ctx, tx, id); e != nil {
					return e
				}
				if _, e := tx.ExecContext(ctx, `INSERT INTO assessments(candidate,operating,operating_key,evidence,revenue_low,revenue_high,revenue_confidence,known_since,government_match,newest_filing,categories,fleet,fleet_verdict,power_units,drivers,minutes,excluded,rules,scored) VALUES (?,0.6,600000,'[]',100,200,0.7,'2020-01-01T00:00:00Z','pattern','2026-01-01T00:00:00Z','[]',?,'pass',3,2,42,1,?,'2026-10-01T00:00:00Z')`, id, fleet, sources.Rules); e != nil {
					return e
				}
				_, e := tx.ExecContext(ctx, `INSERT INTO passes(id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery,kind,origin) VALUES (?,'rebuild','done','','2026-01-01T00:00:00Z','2026-01-01T00:00:01Z',1,2,3,4,5,6,7,8,9,'owner','mail','request','','rebuild','rebuild')`, pid)
				return e
			}))
			contractOK(t, d.Close())
			d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
			contractOK(t, err)
			defer func() { contractOK(t, d.Close()) }()
			s := store.New(d, store.Config{Now: func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) }, Rand: &contractRandom{next: 1}})
			a, err := s.GetJudgments(ctx, id)
			contractOK(t, err)
			op, conf := 0.6, 0.7
			low, high := int64(100), int64(200)
			contractEqual(t, a, store.Judgments{HeadcountSeries: []judging.Series{}, Trend: judging.TrendUnknown, Candidate: id, Operating: &op, OperatingKey: 600000, Evidence: []sources.Evidence{}, RevenueLow: &low, RevenueHigh: &high, RevenueConfidence: &conf, Excluded: true, Rules: sources.Rules, Calculated: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
			p, err := s.GetPass(ctx, pid)
			contractOK(t, err)
			contractEqual(t, p, store.Pass{ID: pid, Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, Status: store.StatusDone, Started: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Finished: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC), Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, UserID: "owner", UserEmail: "mail", RequestID: "request"})
			es, err := s.Enrichments(ctx, id)
			contractOK(t, err)
			contractEqual(t, es, []store.Enrichment{})
		})
	}
}
