package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

const priorTargetPassColumns = priorSearchColumns + ",search_different,search_stands,search_none"

func priorTargetPassArgs(p store.Pass) []any {
	args := priorSearchPassArgs(p)
	args[len(args)-1] = p.CeilingReached
	return append(args, p.SearchDifferent, p.SearchStands, p.SearchNone)
}
func insertPriorTargetPass(ctx context.Context, tx *sql.Tx, p store.Pass) error {
	args := priorTargetPassArgs(p)
	_, err := tx.ExecContext(ctx, `INSERT INTO passes(id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery,kind,origin,due,fetched,https,http,"none",derived,target,enrich_kind,runs,runs_failed,pages,cost,judged,scraper,parcel_run,parcels_read,by_label,by_value,by_candidate,parks_created,parks_updated,parks_gone,parks_grown,parks_walked,circles,requests,found,new_candidates,ceiling_reached,search_different,search_stands,search_none) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
	return err
}
func updatePriorTargetPass(ctx context.Context, tx *sql.Tx, p store.Pass) error {
	args := append(priorTargetPassArgs(p), p.ID)
	_, err := tx.ExecContext(ctx, `UPDATE passes SET id=?,"trigger"=?,status=?,reason=?,started=?,finished=?,"rows"=?,"new"=?,changed=?,unchanged=?,gone=?,created=?,linked=?,rescored=?,unranged=?,user_id=?,user_email=?,request_id=?,delivery=?,kind=?,origin=?,due=?,fetched=?,https=?,http=?,"none"=?,derived=?,target=?,enrich_kind=?,runs=?,runs_failed=?,pages=?,cost=?,judged=?,scraper=?,parcel_run=?,parcels_read=?,by_label=?,by_value=?,by_candidate=?,parks_created=?,parks_updated=?,parks_gone=?,parks_grown=?,parks_walked=?,circles=?,requests=?,found=?,new_candidates=?,ceiling_reached=?,search_different=?,search_stands=?,search_none=? WHERE id=?`, args...)
	return err
}
func priorTargetPass(id string) store.Pass {
	p := migrationWalkPriorPass(id)
	p.ParksWalked, p.Circles, p.Requests, p.Found, p.NewCandidates = 28, 29, 30, 31, 32
	p.SearchDifferent, p.SearchStands, p.SearchNone = 33, 34, 35
	return p
}

// R-D3P2-JSLI
func TestPassKindsUpgradePreservesEveryPreviousPassField(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	clock := func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0030_search_index.sql"), Now: clock})
	contractOK(t, err)
	candidate := store.CandidateIDPrefix + "1234567890abcdef"
	ps := []store.Pass{priorTargetPass("pass_1111111111111111"), priorTargetPass("pass_2222222222222222"), priorTargetPass("pass_3333333333333333")}
	ps[1] = store.Pass{ID: ps[1].ID, Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, Status: store.StatusRunning, Started: clock(), UserID: "owner"}
	ps[2] = store.Pass{ID: ps[2].ID, Kind: store.KindEnrichTarget, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, Target: candidate, EnrichKind: "brief", Status: store.StatusRunning, Started: clock(), UserID: "owner"}
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		if e := writePriorCandidate(ctx, tx, candidate); e != nil {
			return e
		}
		for _, p := range ps {
			if e := insertPriorTargetPass(ctx, tx, p); e != nil {
				return e
			}
		}
		return nil
	}))
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations(), Now: clock})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	s := store.New(d, store.Config{Now: clock})
	for _, want := range ps {
		got, e := s.GetPass(ctx, want.ID)
		contractOK(t, e)
		contractEqual(t, got, want)
	}
	for _, id := range []string{candidate, "unknown"} {
		owner, e := s.RunningTargetPass(ctx, id)
		contractOK(t, e)
		contractEqual(t, owner, "")
	}
}

// R-D4WY-XKC7
func TestPreviousPassWriterRunsAcrossPassKindsRollback(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := t.Context()
	ids := contractAttach(t, f, "rollback", contractObservation("a", "Alpha", f.now), contractObservation("b", "Bravo", f.now))
	p, claimed, err := f.s.AddTargetPass(ctx, targetPassIDs(ids...), ids)
	contractOK(t, err)
	contractEqual(t, claimed, ids)
	entry := store.KindEntry{Kind: "website_search", NewResults: 2, Cost: 3}
	p, err = f.s.EndPassKind(ctx, p.ID, entry)
	contractOK(t, err)
	p, err = f.s.StartPassKind(ctx, p.ID, "site_text")
	contractOK(t, err)
	q, _, err := f.s.AddTargetPass(ctx, targetPassIDs(ids...), nil)
	contractOK(t, err)
	q, err = f.s.EndPassKind(ctx, q.ID, entry)
	contractOK(t, err)
	q, err = f.s.FinishPass(ctx, q.ID, store.Counts{})
	contractOK(t, err)
	old := []store.Pass{priorTargetPass("pass_1111111111111111"), {ID: "pass_2222222222222222", Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, Status: store.StatusRunning, Started: f.now.UTC().Truncate(time.Second), UserID: "old-owner"}}
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		for _, pass := range old {
			if e := insertPriorTargetPass(ctx, tx, pass); e != nil {
				return e
			}
		}
		return nil
	}))
	old[0].Rows = 87
	old[0].SearchDifferent = 12
	old[1].Status = store.StatusDone
	old[1].Finished = f.now.UTC().Truncate(time.Second)
	old[1].Runs = 9
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		for _, pass := range old {
			if e := updatePriorTargetPass(ctx, tx, pass); e != nil {
				return e
			}
		}
		return nil
	}))
	f.now = f.now.Add(time.Hour)
	finished := f.now.UTC().Truncate(time.Second)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, "UPDATE passes SET status=?,reason=?,finished=? WHERE id=?", store.StatusFailed, store.ReasonInterrupted, finished.Format(time.RFC3339), p.ID); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, "DELETE FROM passes WHERE id=?", q.ID)
		return e
	}))
	contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
		values := make([]any, len(strings.Split(priorTargetPassColumns, ",")))
		ptrs := make([]any, len(values))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if e := tx.QueryRowContext(ctx, "SELECT "+priorTargetPassColumns+" FROM passes WHERE id=?", p.ID).Scan(ptrs...); e != nil {
			return e
		}
		contractEqual(t, values[19], any(store.KindEnrichTarget))
		contractEqual(t, values[27], any(""))
		contractEqual(t, values[28], any(""))
		return nil
	}))
	f.reopen()
	for _, want := range old {
		assertPassReads(t, f, want)
	}
	want := p
	want.Status = store.StatusFailed
	want.Reason = store.ReasonInterrupted
	want.Finished = finished
	want.RunningKind = ""
	assertPassReads(t, f, want)
	_, err = f.s.GetPass(ctx, q.ID)
	contractError(t, err, store.ErrNotFound)
	for _, id := range ids {
		owner, e := f.s.RunningTargetPass(ctx, id)
		contractOK(t, e)
		contractEqual(t, owner, "")
	}
	_, claimed, err = f.s.AddTargetPass(ctx, targetPassIDs(ids...), ids)
	contractOK(t, err)
	contractEqual(t, claimed, ids)
}

// R-CIYS-1OZP R-CMMH-707S R-D4WY-XKC7
func TestRollbackPrunedPassIDCanBeReusedWithoutOldProgress(t *testing.T) {
	t.Parallel()
	for _, targeted := range []bool{false, true} {
		t.Run(fmt.Sprint(targeted), func(t *testing.T) {
			t.Parallel()
			f := contractDB(t)
			ctx := t.Context()
			candidate := contractSeed(t, f)
			p, _, err := f.s.AddTargetPass(ctx, targetPassIDs(candidate), []string{candidate})
			contractOK(t, err)
			p, err = f.s.EndPassKind(ctx, p.ID, store.KindEntry{Kind: "old", NewResults: 4})
			contractOK(t, err)
			p, err = f.s.FinishPass(ctx, p.ID, store.Counts{})
			contractOK(t, err)
			contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error { _, e := tx.ExecContext(ctx, "DELETE FROM passes WHERE id=?", p.ID); return e }))
			random, err := hex.DecodeString(strings.TrimPrefix(p.ID, store.PassIDPrefix))
			contractOK(t, err)
			f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(random)})
			var fresh store.Pass
			if targeted {
				fresh, _, err = f.s.AddTargetPass(ctx, targetPassIDs("unknown"), nil)
			} else {
				fresh, err = f.s.AddPass(ctx, enrichPass())
			}
			contractOK(t, err)
			contractEqual(t, fresh.ID, p.ID)
			contractEqual(t, fresh.KindEntries, []store.KindEntry(nil))
			assertPassReads(t, f, fresh)
			owner, err := f.s.RunningTargetPass(ctx, candidate)
			contractOK(t, err)
			contractEqual(t, owner, "")
		})
	}
}
