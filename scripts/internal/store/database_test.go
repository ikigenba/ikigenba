package store_test

import (
	"bytes"
	"database/sql"
	"encoding/gob"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

var _ func(*db.DB, store.Config) *store.Store = store.New

// R-XVW7-CY8N R-XYC0-4HQ1 R-Y0RS-W17F
func TestNewStoreOverHandle(t *testing.T) {
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "unrelated"), []byte("ignored catalog content"), 0600))
	s := open(t, filepath.Join(root, "scripts.db"))
	for _, owner := range []string{"", "alice", "bob"} {
		list, err := s.List(ctx, owner)
		must(t, err)
		equal(t, list, []store.Script{})
	}
	running, err := s.Running(ctx)
	must(t, err)
	equal(t, running, []store.Run{})
	for _, name := range []string{"", "alpha", "unrelated"} {
		taken, err := s.Taken(ctx, name)
		must(t, err)
		equal(t, taken, false)
		r, err := s.RunByID(ctx, name)
		equal(t, r, store.Run{})
		equal(t, errors.Is(err, store.ErrNotFound), true)
	}
	cfg := store.Config{Now: func() time.Time { return stamp }, Rand: bytes.NewReader([]byte{1, 2, 3, 4, 5, 6, 7, 8})}
	other := store.New(handle(s), cfg)
	sc := create(t, other, "alice", "alpha")
	equal(t, sc.ID, "scr_0102030405060708")
	equal(t, sc.Created, stamp.UTC().Truncate(time.Second))
	got, err := s.Find(ctx, "alice", "alpha")
	must(t, err)
	equal(t, got, sc)
	must(t, closeStore(s))
	reopened := open(t, filepath.Join(root, "scripts.db"))
	got, err = reopened.Find(ctx, "alice", "alpha")
	must(t, err)
	equal(t, got, sc)
}

// R-94EX-UQKT
func TestAdoptEarlierGobCatalog(t *testing.T) {
	for _, shape := range []string{"no-row", "empty-maps", "records"} {
		t.Run(shape, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "scripts.db")
			clock := func() time.Time { return stamp }
			legacy, err := db.Open(ctx, db.Config{Path: path, Migrations: fstest.MapFS{}, Now: clock})
			must(t, err)
			t.Cleanup(func() { must(t, legacy.Close()) })
			stored := struct {
				Scripts map[string]store.Script
				Runs    map[string]store.Run
			}{Scripts: map[string]store.Script{}, Runs: map[string]store.Run{}}
			if shape == "records" {
				for i, owner := range []string{"alice", "bob"} {
					sc := store.Script{ID: []string{"scr_0000000000000001", "scr_0000000000000002"}[i], Name: []string{"alpha", "beta"}[i], Owner: owner, Repo: "repo", Ref: "main", Created: stamp.UTC().Truncate(time.Second)}
					stored.Scripts[sc.ID] = sc
					for j, status := range []string{store.StatusQueued, store.StatusRunning, store.StatusExited, store.StatusKilled, store.StatusTimedOut, store.StatusFailed} {
						r := run(sc, i*10+j+1)
						r.Started = stamp.UTC().Truncate(time.Second).Add(-time.Duration(j) * time.Hour)
						r.Status = status
						if status != store.StatusRunning && status != store.StatusQueued {
							r.Finished = r.Started.Add(time.Minute)
							r.StdoutBytes = 12
							r.StderrBytes = 7
							r.Truncated = true
						}
						if status == store.StatusExited {
							r.ExitCode = 17
						}
						if status == store.StatusFailed {
							r.SHA = ""
							r.Reason = store.ReasonCommitMissing
						}
						stored.Runs[r.ID] = r
					}
				}
			}
			must(t, legacy.Write(ctx, func(tx *sql.Tx) error {
				if _, err := tx.ExecContext(ctx, "CREATE TABLE catalog (id INTEGER PRIMARY KEY CHECK(id=1), value BLOB NOT NULL); DROP TABLE schema_migrations"); err != nil {
					return err
				}
				if shape == "no-row" {
					return nil
				}
				var encoded bytes.Buffer
				if err := gob.NewEncoder(&encoded).Encode(stored); err != nil {
					return err
				}
				_, err := tx.ExecContext(ctx, "INSERT INTO catalog(id,value) VALUES(1,?)", encoded.Bytes())
				return err
			}))
			must(t, legacy.Close())
			s := open(t, path)
			for _, owner := range []string{"alice", "bob"} {
				got, err := s.List(ctx, owner)
				must(t, err)
				wantCount := 0
				for _, sc := range stored.Scripts {
					if sc.Owner != owner {
						continue
					}
					wantCount++
					equal(t, len(got), 1)
					actual := got[0]
					actual.Last = nil
					actual.Subscriptions = nil
					equal(t, actual, sc)
					rr, err := s.Runs(ctx, sc.ID)
					must(t, err)
					count := 0
					for _, r := range stored.Runs {
						if r.Script == sc.ID {
							count++
							found := false
							for _, candidate := range rr {
								if candidate.ID == r.ID {
									equal(t, candidate, r)
									found = true
								}
							}
							if !found {
								t.Fatalf("missing legacy run %s", r.ID)
							}
						}
					}
					equal(t, len(rr), count)
					found, err := s.Find(ctx, owner, sc.Name)
					must(t, err)
					equal(t, *found.Last, rr[0])
				}
				equal(t, len(got), wantCount)
			}
		})
	}
}

// R-95MU-8IBI R-G6AU-28DY
func TestUpgradeBaselineCatalog(t *testing.T) {
	for _, baseline := range []bool{false, true} {
		for _, records := range []bool{false, true} {
			t.Run(fmt.Sprintf("baseline-%t-records-%t", baseline, records), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "scripts.db")
				migrations := fstest.MapFS{}
				if baseline {
					data, err := fs.ReadFile(scripts.Migrations(), "0001_catalog.sql")
					must(t, err)
					migrations["0001_catalog.sql"] = &fstest.MapFile{Data: data}
				}
				legacy, err := db.Open(ctx, db.Config{Path: path, Migrations: migrations, Now: func() time.Time { return stamp }})
				must(t, err)
				sc := store.Script{ID: "scr_0000000000000001", Name: "alpha", Owner: "alice", Repo: "repo", Ref: "main", Created: stamp.UTC().Truncate(time.Second), Subscriptions: []store.Subscription{{Event: "cron.*.fired", Created: stamp.UTC().Truncate(time.Second)}}}
				r := run(sc, 1)
				r.Started = r.Started.UTC().Truncate(time.Second)
				must(t, legacy.Write(ctx, func(tx *sql.Tx) error {
					if !baseline {
						if _, err := tx.ExecContext(ctx, "CREATE TABLE catalog (id INTEGER PRIMARY KEY CHECK(id=1),value BLOB NOT NULL); DROP TABLE schema_migrations"); err != nil {
							return err
						}
					}
					if !records {
						return nil
					}
					stored := struct {
						Scripts map[string]store.Script
						Runs    map[string]store.Run
					}{map[string]store.Script{sc.ID: sc}, map[string]store.Run{r.ID: r}}
					var encoded bytes.Buffer
					if err := gob.NewEncoder(&encoded).Encode(stored); err != nil {
						return err
					}
					_, err := tx.ExecContext(ctx, "INSERT INTO catalog(id,value) VALUES(1,?)", encoded.Bytes())
					return err
				}))
				must(t, legacy.Close())
				s := open(t, path)
				must(t, handle(s).Read(ctx, func(tx *sql.Tx) error {
					rows, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT GLOB 'sqlite_*' ORDER BY name")
					if err != nil {
						return err
					}
					defer func() { _ = rows.Close() }()
					names := []string{}
					for rows.Next() {
						var name string
						if err := rows.Scan(&name); err != nil {
							return err
						}
						names = append(names, name)
					}
					equal(t, names, []string{"catalog", "event_runs", "schema_migrations", "subscriptions"})
					return rows.Err()
				}))
				if !records {
					equal(t, lenList(t, s, "alice"), 0)
					return
				}
				actual, err := s.Find(ctx, sc.Owner, sc.Name)
				must(t, err)
				equal(t, actual.Subscriptions, []store.Subscription{})
				actual.Last = nil
				sc.Subscriptions = []store.Subscription{}
				equal(t, actual, sc)
				rr, err := s.Runs(ctx, sc.ID)
				must(t, err)
				equal(t, rr, []store.Run{r})
				delivered, err := s.Delivered(ctx, sc.ID, "event")
				must(t, err)
				equal(t, delivered, false)
				actual, err = s.Subscribe(ctx, sc.ID, "cron.*.fired")
				must(t, err)
				must(t, closeStore(s))
				s = open(t, path)
				again, err := s.Find(ctx, sc.Owner, sc.Name)
				must(t, err)
				equal(t, again, actual)
			})
		}
	}
}

func lenList(t *testing.T, s *store.Store, owner string) int {
	t.Helper()
	list, err := s.List(ctx, owner)
	must(t, err)
	return len(list)
}
