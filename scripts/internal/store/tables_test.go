package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

func tableRows(t *testing.T, d *db.DB, table string) [][]any {
	t.Helper()
	out := [][]any{}
	must(t, d.Read(ctx, func(tx *sql.Tx) error {
		query := map[string]string{"scripts": "SELECT * FROM scripts ORDER BY id", "runs": "SELECT * FROM runs ORDER BY id"}[table]
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		columns, err := rows.Columns()
		if err != nil {
			return err
		}
		for rows.Next() {
			values := make([]any, len(columns))
			targets := make([]any, len(columns))
			for i := range values {
				targets[i] = &values[i]
			}
			if err := rows.Scan(targets...); err != nil {
				return err
			}
			out = append(out, values)
		}
		return rows.Err()
	}))
	return out
}

// R-B6OW-3ELR
func assertMirror(t *testing.T, s *store.Store) {
	t.Helper()
	scriptsRows := tableRows(t, handle(s), "scripts")
	runsRows := tableRows(t, handle(s), "runs")
	wantScripts := map[string][]any{}
	wantRuns := map[string][]any{}
	for _, owner := range []string{"alice", "bob"} {
		list, err := s.List(ctx, owner, "", 10000)
		must(t, err)
		for _, sc := range list {
			wantScripts[sc.ID] = []any{sc.ID, sc.Name, sc.Owner, sc.Repo, sc.Ref, sc.Created.UTC().Format(time.RFC3339)}
			runs, err := s.Runs(ctx, sc.ID, "", "", 10000)
			must(t, err)
			for _, r := range runs {
				var finished any
				if !r.Finished.IsZero() {
					finished = r.Finished.UTC().Format(time.RFC3339)
				}
				var truncated int64
				if r.Truncated {
					truncated = 1
				}
				wantRuns[r.ID] = []any{r.ID, r.Script, r.SHA, r.Ref, r.User, r.RequestID, r.Trigger, r.Event, r.Status, int64(r.ExitCode), r.Started.UTC().Format(time.RFC3339), finished, r.StdoutBytes, r.StderrBytes, truncated, r.Reason}
			}
		}
	}
	equal(t, len(scriptsRows), len(wantScripts))
	equal(t, len(runsRows), len(wantRuns))
	for _, row := range scriptsRows {
		equal(t, row, wantScripts[row[0].(string)])
	}
	for _, row := range runsRows {
		equal(t, row, wantRuns[row[0].(string)])
	}
	// Scanning storage classes verifies empty strings and all numeric columns too.
	must(t, handle(s).Read(ctx, func(tx *sql.Tx) error {
		queries := []string{
			"SELECT typeof(id),typeof(name),typeof(owner),typeof(repo),typeof(ref),typeof(created) FROM scripts",
			"SELECT typeof(id),typeof(script),typeof(sha),typeof(ref),typeof(user),typeof(request_id),typeof(trigger),typeof(event),typeof(status),typeof(exit_code),typeof(started),typeof(finished),typeof(stdout_bytes),typeof(stderr_bytes),typeof(truncated),typeof(reason) FROM runs",
		}
		for table, query := range queries {
			rows, err := tx.QueryContext(ctx, query)
			if err != nil {
				return err
			}
			columns, err := rows.Columns()
			if err != nil {
				_ = rows.Close()
				return err
			}
			for rows.Next() {
				values := make([]string, len(columns))
				targets := make([]any, len(columns))
				for i := range values {
					targets[i] = &values[i]
				}
				if err := rows.Scan(targets...); err != nil {
					_ = rows.Close()
					return err
				}
				for i, got := range values {
					want := "text"
					if table == 1 && (i == 9 || i >= 12 && i <= 14) {
						want = "integer"
					}
					if table == 1 && i == 11 && got == "null" {
						continue
					}
					equal(t, got, want)
				}
			}
			err = rows.Err()
			_ = rows.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}))
}

// R-EE5D-1U9Q
func TestTableMirrorEveryWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scripts.db")
	s := open(t, path)
	assertMirror(t, s)
	sc := create(t, s, "alice", "alpha")
	assertMirror(t, s)
	create(t, s, "bob", "beta")
	assertMirror(t, s)
	_, _, err := s.SetRef(ctx, sc.ID, "other")
	must(t, err)
	assertMirror(t, s)
	r := run(sc, 1)
	r.Status = store.StatusQueued
	r = add(t, s, r)
	assertMirror(t, s)
	_, err = s.StartRun(ctx, r.ID)
	must(t, err)
	assertMirror(t, s)
	_, err = s.FinishRun(ctx, r.ID, store.Ending{Status: store.StatusExited, ExitCode: 17, Finished: stamp.Add(time.Minute), StdoutBytes: 12, StderrBytes: 7, Truncated: true})
	must(t, err)
	assertMirror(t, s)
	r = run(sc, 2)
	r.Status = store.StatusFailed
	r.SHA = ""
	r.Reason = store.ReasonCommitMissing
	r.Finished = stamp.Add(time.Minute)
	add(t, s, r)
	assertMirror(t, s)
	r = run(sc, 3)
	r.Trigger = store.TriggerEvent
	r.Event = "delivery-one"
	add(t, s, r)
	assertMirror(t, s)
	before := tableRows(t, handle(s), "scripts")
	beforeRuns := tableRows(t, handle(s), "runs")
	_, err = s.Subscribe(ctx, sc.ID, "cron.*.fired")
	must(t, err)
	_, err = s.Unsubscribe(ctx, sc.ID, "cron.*.fired")
	must(t, err)
	_, err = s.Find(ctx, "alice", "alpha")
	must(t, err)
	equal(t, tableRows(t, handle(s), "scripts"), before)
	equal(t, tableRows(t, handle(s), "runs"), beforeRuns)
	must(t, s.DeleteRun(ctx, r.ID))
	assertMirror(t, s)
	delivered, err := s.Delivered(ctx, sc.ID, r.Event)
	must(t, err)
	equal(t, delivered, true)
	assertMirror(t, s)
	for i, status := range []string{store.StatusKilled, store.StatusTimedOut} {
		r := add(t, s, run(sc, 10+i))
		_, err := s.FinishRun(ctx, r.ID, store.Ending{Status: status, Finished: stamp.Add(time.Minute)})
		must(t, err)
		assertMirror(t, s)
	}
	other := open(t, path)
	assertMirror(t, other)
	must(t, s.Delete(ctx, sc.ID))
	assertMirror(t, s)
	assertMirror(t, other)
}

// R-X16O-9NPO
func TestMirrorUnchangedOnErrors(t *testing.T) {
	s := open(t, "")
	sc := create(t, s, "alice", "alpha")
	r := add(t, s, run(sc, 1))
	// An inconsistent mirror must remain byte-for-value unchanged on refusals.
	must(t, handle(s).Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE scripts SET ref='stale'; DELETE FROM runs")
		return err
	}))
	before := tableRows(t, handle(s), "scripts")
	beforeRuns := tableRows(t, handle(s), "runs")
	calls := []func(context.Context) error{
		func(c context.Context) error { _, err := s.Create(c, store.Draft{}); return err },
		func(c context.Context) error { _, _, err := s.SetRef(c, sc.ID, ""); return err },
		func(c context.Context) error { return s.Delete(c, "absent") },
		func(c context.Context) error { _, err := s.AddRun(c, store.Run{}); return err },
		func(c context.Context) error { _, err := s.StartRun(c, r.ID); return err },
		func(c context.Context) error { _, err := s.FinishRun(c, r.ID, store.Ending{}); return err },
		func(c context.Context) error { return s.DeleteRun(c, "absent") },
		func(c context.Context) error { _, err := s.Subscribe(c, sc.ID, "bad"); return err },
		func(c context.Context) error { _, err := s.Unsubscribe(c, sc.ID, "cron.*.fired"); return err },
	}
	check := func() {
		equal(t, tableRows(t, handle(s), "scripts"), before)
		equal(t, tableRows(t, handle(s), "runs"), beforeRuns)
	}
	for i, call := range calls {
		if call(ctx) == nil {
			t.Fatalf("call %d succeeded", i)
		}
		check()
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()

	check()
	for _, call := range calls {
		if call(canceled) == nil {
			t.Fatal("canceled call succeeded")
		}
		check()
	}
	handle(s).SetFailing(true)

	for _, call := range calls {
		if call(ctx) == nil {
			t.Fatal("failing call succeeded")
		}
	}
	handle(s).SetFailing(false)
	check()
	// A refused table insert changes neither table.
	must(t, handle(s).Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "CREATE TRIGGER refuse_mirror BEFORE INSERT ON scripts BEGIN SELECT RAISE(ABORT,'test refusal'); END")
		return err
	}))

	check()
	if _, err := s.Create(ctx, store.Draft{Owner: "bob", Name: "beta", Repo: "repo", Ref: "main"}); err == nil {
		t.Fatal("refused create succeeded")
	}
	check()
}
