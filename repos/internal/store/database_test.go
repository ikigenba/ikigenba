package store_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

// R-Z99N-UALV R-ZAHK-82CK: The migration produces only the catalog schema and no data.
func TestCatalogSchema(t *testing.T) {
	f := setup(t)
	d := f.handle(t)
	must(t, d.Read(testContext(t), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(testContext(t), "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
		if err != nil {
			return err
		}
		var tables []string
		for rows.Next() {
			var name string
			if err = rows.Scan(&name); err != nil {
				return err
			}
			tables = append(tables, name)
		}
		if err = rows.Close(); err != nil {
			return err
		}
		same(t, tables, []string{"repos", "schema_migrations"})
		var count int
		if err = tx.QueryRowContext(testContext(t), "SELECT COUNT(*) FROM repos").Scan(&count); err != nil {
			return err
		}
		same(t, count, 0)
		rows, err = tx.QueryContext(testContext(t), "PRAGMA table_info(repos)")
		if err != nil {
			return err
		}
		type column struct {
			ID         int
			Name, Type string
			NotNull    int
			Default    any
			PK         int
		}
		var columns []column
		for rows.Next() {
			var c column
			if err = rows.Scan(&c.ID, &c.Name, &c.Type, &c.NotNull, &c.Default, &c.PK); err != nil {
				return err
			}
			columns = append(columns, c)
		}
		if err = rows.Close(); err != nil {
			return err
		}
		same(t, columns, []column{{0, "id", "TEXT", 0, nil, 1}, {1, "name", "TEXT", 1, nil, 0}, {2, "owner", "TEXT", 1, nil, 0}, {3, "created", "TEXT", 1, nil, 0}, {4, "available", "INTEGER", 1, nil, 0}})
		rows, err = tx.QueryContext(testContext(t), "PRAGMA index_list(repos)")
		if err != nil {
			return err
		}
		var uniqueNames []string
		for rows.Next() {
			var seq, unique, partial int
			var name, origin string
			if err = rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
				return err
			}
			if origin == "u" {
				same(t, unique, 1)
				uniqueNames = append(uniqueNames, name)
			}
		}
		if err = rows.Close(); err != nil {
			return err
		}
		same(t, len(uniqueNames), 1)
		rows, err = tx.QueryContext(testContext(t), "SELECT name FROM pragma_index_info(?) ORDER BY seqno", uniqueNames[0])
		if err != nil {
			return err
		}
		var indexed []string
		for rows.Next() {
			var name string
			if err = rows.Scan(&name); err != nil {
				return err
			}
			indexed = append(indexed, name)
		}
		if err = rows.Close(); err != nil {
			return err
		}
		same(t, indexed, []string{"owner", "name"})
		return nil
	}))
}

// R-UU8W-FALR: A pre-migration catalog keeps every stored repository before verification.
func TestAdoptLegacyCatalog(t *testing.T) {
	f := setup(t)
	d, err := db.Open(testContext(t), db.Config{Path: f.source, Migrations: fstest.MapFS{}, Now: f.cfg.Now})
	must(t, err)
	wanted := []store.Repo{{ID: id(3), Name: "notes", Owner: "owner", Created: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC), Available: false}, {ID: id(7), Name: "journal", Owner: "other", Created: time.Date(2021, 2, 3, 4, 5, 6, 0, time.UTC), Available: true}}
	must(t, d.Write(testContext(t), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(testContext(t), "CREATE TABLE repos (id TEXT PRIMARY KEY,name TEXT NOT NULL,owner TEXT NOT NULL,created TEXT NOT NULL,available INTEGER NOT NULL,UNIQUE(owner,name))"); err != nil {
			return err
		}
		for _, r := range wanted {
			if _, err := tx.ExecContext(testContext(t), "INSERT INTO repos VALUES(?,?,?,?,?)", r.ID, r.Name, r.Owner, r.Created.Format("2006-01-02T15:04:05Z"), r.Available); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(testContext(t), "DROP TABLE schema_migrations")
		return err
	}))
	must(t, d.Close())
	s := f.open(t)
	same(t, all(t, s), wanted)
}

// R-UJ9S-ZCXI R-UMXI-4O5L: Successful Open settles even an empty catalog permanently.
func TestSettledEmptyCatalogDoesNotRebuild(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "deleted"}[deleted], func(t *testing.T) {
			f := setup(t)
			s := f.open(t)
			if deleted {
				r := create(t, s, "owner", "notes")
				must(t, s.Delete(testContext(t), r.ID))
			}
			f.close(t)
			f.identity(t, id(99), "foreign", "owner", "2024-01-01T00:00:00Z")
			before := snapshot(t, f.cfg.Root)
			s = f.open(t)
			same(t, all(t, s), []store.Repo{})
			same(t, snapshot(t, f.cfg.Root), before)
		})
	}
}

// R-UO5E-IFWA R-UJ9S-ZCXI: Failed and interrupted first starts leave the catalog unsettled.
func TestFailedOpenCanRebuildOnRetry(t *testing.T) {
	for _, failure := range []string{"failing", "root", "interrupted"} {
		t.Run(failure, func(t *testing.T) {
			f := setup(t)
			f.identity(t, id(1), "notes", "owner", "2024-01-01T00:00:00Z")
			d := f.handle(t)
			ctx, cancel := context.WithCancel(testContext(t))
			defer cancel()
			cfg := f.cfg
			switch failure {
			case "failing":
				d.SetFailing(true)
			case "root":
				cfg.Root = filepath.Join(f.base, "file")
				write(t, cfg.Root, "blocking")
			case "interrupted":
				calls := 0
				g, err := git.Find(filepath.Dir(f.path), func() []string {
					calls++
					if calls == 2 {
						cancel()
					}
					return append([]string(nil), f.env...)
				})
				must(t, err)
				cfg.Git = g
			}
			s, err := store.Open(ctx, d, cfg)
			if s != nil || err == nil {
				t.Fatalf("Open=%v,%v", s, err)
			}
			if failure == "failing" && errors.Is(err, store.ErrRoot) {
				t.Fatal(err)
			}
			if failure == "interrupted" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			d.SetFailing(false)
			s, err = store.Open(testContext(t), d, f.cfg)
			must(t, err)
			got := all(t, s)
			same(t, len(got), 1)
			same(t, got[0].ID, id(1))
		})
	}
}

// R-UPDA-W7MZ: Mixed catalog operations survive a later handle without re-verifying disk.
func TestPersistMixedCatalogOperations(t *testing.T) {
	f := setup(t)
	s := f.open(t)
	a := create(t, s, "owner", "first")
	b := create(t, s, "owner", "second")
	c := create(t, s, "other", "third")
	_, err := s.Rename(testContext(t), a.ID, "renamed")
	must(t, err)
	must(t, s.Delete(testContext(t), b.ID))
	damaged(t, s, c)
	w, _ := writer(t)
	must(t, s.Verify(testContext(t), w))
	want := all(t, s)
	f.close(t)
	must(t, os.RemoveAll(f.cfg.Root))
	s = f.open(t)
	same(t, all(t, s), want)
}
