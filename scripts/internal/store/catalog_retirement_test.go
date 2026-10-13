package store_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

func catalogRows(t *testing.T, s *store.Store) [][]any {
	t.Helper()
	out := [][]any{}
	must(t, handle(s).Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT id,value FROM catalog ORDER BY id")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int64
			var value []byte
			if err := rows.Scan(&id, &value); err != nil {
				return err
			}
			out = append(out, []any{id, value})
		}
		return rows.Err()
	}))
	return out
}

// R-AC4G-1TTA
func TestEveryStoreCallLeavesCatalogUntouched(t *testing.T) {
	for _, row := range []bool{false, true} {
		t.Run(fmt.Sprint(row), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "scripts.db")
			s := open(t, path)
			observer := open(t, path)
			if row {
				must(t, handle(s).Write(ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, "INSERT INTO catalog(id,value) VALUES(1,?)", []byte{0, 255, 128, 1, 0})
					return err
				}))
			}
			before := catalogRows(t, observer)
			call := func(name string, fn func() error) {
				t.Helper()
				if err := fn(); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				equal(t, catalogRows(t, observer), before)
			}
			var sc store.Script
			call("create", func() error {
				var err error
				sc, err = s.Create(ctx, store.Draft{Owner: "alice", Name: "alpha", Repo: "repo", Ref: "main"})
				return err
			})
			call("set ref", func() error { _, _, err := s.SetRef(ctx, sc.ID, "other"); return err })
			call("subscribe", func() error { _, err := s.Subscribe(ctx, sc.ID, "cron.*.fired"); return err })
			call("unsubscribe", func() error { _, err := s.Unsubscribe(ctx, sc.ID, "cron.*.fired"); return err })
			call("subscribe again", func() error { _, err := s.Subscribe(ctx, sc.ID, "repo.pushed"); return err })
			for i, status := range []string{store.StatusRunning, store.StatusQueued, store.StatusFailed, store.StatusRunning, store.StatusQueued, store.StatusRunning} {
				r := run(sc, i+1)
				r.Status = status
				if status == store.StatusFailed {
					r.Reason = store.ReasonGitFailed
					r.Finished = stamp.Add(time.Minute)
				}
				if i == 3 || i == 5 {
					r.Trigger, r.Event = store.TriggerEvent, fmt.Sprintf("delivery-%d", i)
				}
				call("add run", func() error { _, err := s.AddRun(ctx, r); return err })
			}
			call("start run", func() error { _, err := s.StartRun(ctx, run(sc, 2).ID); return err })
			call("finish running", func() error {
				_, err := s.FinishRun(ctx, run(sc, 1).ID, store.Ending{Status: store.StatusExited, Finished: stamp.Add(time.Minute)})
				return err
			})
			call("finish queued", func() error {
				_, err := s.FinishRun(ctx, run(sc, 5).ID, store.Ending{Status: store.StatusKilled, Finished: stamp.Add(time.Minute)})
				return err
			})
			reads := []func() error{
				func() error { _, err := s.Find(ctx, sc.Owner, sc.Name); return err },
				func() error { _, err := s.List(ctx, sc.Owner, "", 20); return err },
				func() error { _, err := s.Taken(ctx, sc.Name); return err },
				func() error { _, err := s.FindRun(ctx, sc.Owner, run(sc, 1).ID); return err },
				func() error { _, err := s.RunByID(ctx, run(sc, 1).ID); return err },
				func() error { _, err := s.Runs(ctx, sc.ID, "", "", 20); return err },
				func() error { _, err := s.RunCount(ctx, sc.ID); return err },
				func() error { _, err := s.Running(ctx); return err },
				func() error { _, err := s.Queued(ctx); return err },
				func() error { _, err := s.PastKeeping(ctx, stamp.Add(48*time.Hour), 1, 1); return err },
				func() error { _, err := s.Subscribers(ctx, "repo.pushed"); return err },
				func() error { _, err := s.Delivered(ctx, sc.ID, "delivery-3"); return err },
			}
			for _, read := range reads {
				call("read", read)
			}
			if _, err := s.Create(ctx, store.Draft{}); err == nil {
				t.Fatal("invalid create succeeded")
			}
			equal(t, catalogRows(t, observer), before)
			writes := []func() error{
				func() error {
					_, err := s.Create(ctx, store.Draft{Owner: "alice", Name: "beta", Repo: "repo", Ref: "main"})
					return err
				},
				func() error { _, _, err := s.SetRef(ctx, sc.ID, "third"); return err },
				func() error { _, err := s.Subscribe(ctx, sc.ID, "cron.hourly.fired"); return err },
				func() error { _, err := s.Unsubscribe(ctx, sc.ID, "repo.pushed"); return err },
				func() error { _, err := s.AddRun(ctx, run(sc, 99)); return err },
				func() error { _, err := s.StartRun(ctx, run(sc, 2).ID); return err },
				func() error {
					_, err := s.FinishRun(ctx, run(sc, 2).ID, store.Ending{Status: store.StatusExited, Finished: stamp.Add(time.Minute)})
					return err
				},
				func() error { return s.DeleteRun(ctx, run(sc, 2).ID) },
				func() error { return s.Delete(ctx, sc.ID) },
			}
			handle(s).SetFailing(true)
			for _, fn := range append(reads, writes...) {
				if err := fn(); err == nil {
					t.Fatal("failing call succeeded")
				}
				equal(t, catalogRows(t, observer), before)
			}
			handle(s).SetFailing(false)
			call("delete manual run", func() error { return s.DeleteRun(ctx, run(sc, 1).ID) })
			call("delete event run", func() error { return s.DeleteRun(ctx, run(sc, 4).ID) })
			call("delete script with subscriptions and event runs", func() error { return s.Delete(ctx, sc.ID) })
			must(t, closeStore(s))
			reopened := open(t, path)
			equal(t, catalogRows(t, reopened), before)
		})
	}
}
