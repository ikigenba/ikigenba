package store_test

import (
	"bytes"
	"database/sql"
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

// R-XVW7-CY8N R-HOIX-0C97 R-Y0RS-W17F
func TestNewStoreOverHandle(t *testing.T) {
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "unrelated"), []byte("ignored catalog content"), 0600))
	s := open(t, filepath.Join(root, "scripts.db"))
	for _, owner := range []string{"", "alice", "bob"} {
		list, err := s.List(ctx, owner, "", 10000)
		must(t, err)
		equal(t, list, []store.Script{})
	}
	for _, owner := range []string{"", "alice", "bob"} {
		for _, after := range []string{"", "z"} {
			for _, n := range []int{-1, 0, 2} {
				list, err := s.List(ctx, owner, after, n)
				must(t, err)
				equal(t, list, []store.Script{})
			}
		}
	}
	for _, script := range []string{"", "absent"} {
		for _, started := range []string{"", "2000-01-01T00:00:00Z"} {
			for _, id := range []string{"", "run_0000000000000001"} {
				for _, n := range []int{-1, 0, 2} {
					rr, err := s.Runs(ctx, script, started, id, n)
					must(t, err)
					equal(t, rr, []store.Run{})
				}
			}
		}
		count, err := s.RunCount(ctx, script)
		must(t, err)
		equal(t, count, 0)
	}
	queued, err := s.Queued(ctx)
	must(t, err)
	equal(t, queued, []store.Run{})
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

// R-ADCC-FLJZ R-AEK8-TDAO R-AFS5-751D
func TestUpgradeBaselineCatalog(t *testing.T) {
	for _, migrationCount := range []int{0, 1, 2} {
		for _, records := range []bool{false, true} {
			t.Run(fmt.Sprintf("migrations-%d-records-%t", migrationCount, records), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "scripts.db")
				migrations := fstest.MapFS{}
				for _, name := range []string{"0001_catalog.sql", "0002_subscriptions.sql"}[:migrationCount] {
					data, err := fs.ReadFile(scripts.Migrations(), name)
					must(t, err)
					migrations[name] = &fstest.MapFile{Data: data}
				}
				legacy, err := db.Open(ctx, db.Config{Path: path, Migrations: migrations, Now: func() time.Time { return stamp }})
				must(t, err)
				sc := store.Script{ID: "scr_0000000000000001", Name: "alpha", Owner: "alice", Repo: "repo", Ref: "main", Created: stamp.UTC().Truncate(time.Second)}
				r := run(sc, 1)
				encoded := []byte{0, 255, 7, 128, 0}
				must(t, legacy.Write(ctx, func(tx *sql.Tx) error {
					if migrationCount == 0 {
						if _, err := tx.ExecContext(ctx, "CREATE TABLE catalog (id INTEGER PRIMARY KEY CHECK(id=1),value BLOB NOT NULL); DROP TABLE schema_migrations"); err != nil {
							return err
						}
					}
					if !records {
						return nil
					}
					_, err := tx.ExecContext(ctx, "INSERT INTO catalog(id,value) VALUES(1,?)", encoded)
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
					equal(t, names, []string{"catalog", "event_runs", "runs", "schema_migrations", "scripts", "subscriptions"})
					if err := rows.Err(); err != nil {
						return err
					}
					var got []byte
					err = tx.QueryRowContext(ctx, "SELECT value FROM catalog WHERE id=1").Scan(&got)
					if records {
						must(t, err)
						equal(t, got, encoded)
					} else {
						equal(t, errors.Is(err, sql.ErrNoRows), true)
					}
					return nil
				}))
				if migrationCount < 2 {
					must(t, handle(s).Read(ctx, func(tx *sql.Tx) error {
						for _, query := range []string{"SELECT count(*) FROM subscriptions", "SELECT count(*) FROM event_runs"} {
							var n int
							if err := tx.QueryRowContext(ctx, query).Scan(&n); err != nil {
								return err
							}
							equal(t, n, 0)
						}
						return nil
					}))
				}
				equal(t, tableRows(t, handle(s), "scripts"), [][]any{})
				equal(t, tableRows(t, handle(s), "runs"), [][]any{})
				for _, owner := range []string{"alice", "bob", ""} {
					for _, after := range []string{"", "alpha"} {
						for _, n := range []int{-1, 0, 2} {
							got, err := s.List(ctx, owner, after, n)
							must(t, err)
							equal(t, got, []store.Script{})
						}
					}
					found, err := s.Find(ctx, owner, sc.Name)
					equal(t, found, store.Script{})
					equal(t, errors.Is(err, store.ErrNotFound), true)
				}
				taken, err := s.Taken(ctx, sc.Name)
				must(t, err)
				equal(t, taken, false)
				got, err := s.RunByID(ctx, r.ID)
				equal(t, got, store.Run{})
				equal(t, errors.Is(err, store.ErrNotFound), true)
			})
		}
	}
}
