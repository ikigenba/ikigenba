package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-WIIB-6TNQ
func TestSearchCasesMigrationBytes(t *testing.T) {
	b, err := fs.ReadFile(candidates.Migrations(), "0025_search_cases.sql")
	contractOK(t, err)
	contractEqual(t, string(b), "ALTER TABLE passes ADD COLUMN search_different INTEGER NOT NULL DEFAULT 0;\nALTER TABLE passes ADD COLUMN search_stands INTEGER NOT NULL DEFAULT 0;\nALTER TABLE passes ADD COLUMN search_none INTEGER NOT NULL DEFAULT 0;\n")
}

const priorSearchColumns = `id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery,kind,origin,due,fetched,https,http,"none",derived,target,enrich_kind,runs,runs_failed,pages,cost,judged,scraper,parcel_run,parcels_read,by_label,by_value,by_candidate,parks_created,parks_updated,parks_gone,parks_grown,parks_walked,circles,requests,found,new_candidates,ceiling_reached`

func priorSearchPassArgs(p store.Pass) []any {
	return []any{p.ID, p.Trigger, p.Status, p.Reason, p.Started.Format(time.RFC3339), p.Finished.Format(time.RFC3339), p.Rows, p.New, p.Changed, p.Unchanged, p.Gone, p.Created, p.Linked, p.Judged, p.Unranged, p.UserID, p.UserEmail, p.RequestID, p.Delivery, p.Kind, p.Origin, p.Due, p.Fetched, p.HTTPS, p.HTTP, p.None, p.Derived, p.Target, p.EnrichKind, p.Runs, p.RunsFailed, p.Pages, p.Cost, p.Judged, p.Scraper, p.ParcelRun, p.ParcelsRead, p.ByLabel, p.ByValue, p.ByCandidate, p.ParksCreated, p.ParksUpdated, p.ParksGone, p.ParksGrown, p.ParksWalked, p.Circles, p.Requests, p.Found, p.NewCandidates, int64(0)}
}

func insertPriorSearchPass(ctx context.Context, tx *sql.Tx, p store.Pass) error {
	args := priorSearchPassArgs(p)
	_, err := tx.ExecContext(ctx, `INSERT INTO passes(id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery,kind,origin,due,fetched,https,http,"none",derived,target,enrich_kind,runs,runs_failed,pages,cost,judged,scraper,parcel_run,parcels_read,by_label,by_value,by_candidate,parks_created,parks_updated,parks_gone,parks_grown,parks_walked,circles,requests,found,new_candidates,ceiling_reached) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
	return err
}

// R-WM60-C4VT
func TestSearchCasesUpgradePreservesEveryPriorPassField(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	now := func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0024_walk.sql"), Now: now})
	contractOK(t, err)
	passes := []store.Pass{migrationWalkPriorPass("pass_1111111111111111"), migrationWalkPriorPass("pass_2222222222222222")}
	passes[0].ParksWalked, passes[0].Circles, passes[0].Requests, passes[0].Found, passes[0].NewCandidates = 28, 29, 30, 31, 32
	passes[1].Status, passes[1].Reason = store.StatusFailed, store.ReasonStore
	for _, p := range passes {
		contractOK(t, d.Write(ctx, func(tx *sql.Tx) error { return insertPriorSearchPass(ctx, tx, p) }))
	}
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations(), Now: now})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	s := store.New(d, store.Config{})
	for _, p := range passes {
		got, e := s.GetPass(ctx, p.ID)
		contractOK(t, e)
		contractEqual(t, got, p)
	}
}

// R-WOLT-3OD7
func TestSearchCasesRollbackWriterLeavesNewCounts(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	p, err := f.s.AddPass(ctx, enrichPass())
	contractOK(t, err)
	done, err := f.s.FinishPass(ctx, p.ID, store.Counts{Rows: 2, SearchDifferent: 3, SearchStands: 4, SearchNone: 5})
	contractOK(t, err)
	old := []store.Pass{migrationWalkPriorPass("pass_1111111111111111"), migrationWalkPriorPass("pass_2222222222222222")}
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		for _, p := range old {
			if e := insertPriorSearchPass(ctx, tx, p); e != nil {
				return e
			}
		}
		for i := range old {
			old[i].Status, old[i].Reason = store.StatusFailed, store.ReasonScrapes
			args := priorSearchPassArgs(old[i])
			args = append(args, old[i].ID)
			if _, e := tx.ExecContext(ctx, `UPDATE passes SET id=?,"trigger"=?,status=?,reason=?,started=?,finished=?,"rows"=?,"new"=?,changed=?,unchanged=?,gone=?,created=?,linked=?,rescored=?,unranged=?,user_id=?,user_email=?,request_id=?,delivery=?,kind=?,origin=?,due=?,fetched=?,https=?,http=?,"none"=?,derived=?,target=?,enrich_kind=?,runs=?,runs_failed=?,pages=?,cost=?,judged=?,scraper=?,parcel_run=?,parcels_read=?,by_label=?,by_value=?,by_candidate=?,parks_created=?,parks_updated=?,parks_gone=?,parks_grown=?,parks_walked=?,circles=?,requests=?,found=?,new_candidates=?,ceiling_reached=? WHERE id=?`, args...); e != nil {
				return e
			}
		}
		_, e := tx.ExecContext(ctx, "DELETE FROM passes WHERE id=?", old[1].ID)
		return e
	}))
	contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
		args := priorSearchPassArgs(done)
		got := make([]any, len(args))
		dest := make([]any, len(args))
		for i := range got {
			dest[i] = &got[i]
		}
		if e := tx.QueryRowContext(ctx, "SELECT "+priorSearchColumns+" FROM passes WHERE id=?", done.ID).Scan(dest...); e != nil {
			return e
		}
		contractEqual(t, got, args)
		return nil
	}))
	f.reopen()
	got, err := f.s.GetPass(ctx, done.ID)
	contractOK(t, err)
	contractEqual(t, got, done)
	got, err = f.s.GetPass(ctx, old[0].ID)
	contractOK(t, err)
	contractEqual(t, got, old[0])
	_, err = f.s.GetPass(ctx, old[1].ID)
	contractError(t, err, store.ErrNotFound)
}
