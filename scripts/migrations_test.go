package scripts_test

import (
	"bytes"
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/scripts"
)

var _ func() fs.FS = scripts.Migrations

// R-BLBO-ONI3 R-BMJL-2F8S
func TestMigrations(t *testing.T) {
	first := scripts.Migrations()
	assertEntries(t, first, ".", []string{"0001_catalog.sql", "0002_subscriptions.sql", "0003_tables.sql"}, false)
	contents, err := fs.ReadFile(first, "0001_catalog.sql")
	second, secondErr := fs.ReadFile(first, "0002_subscriptions.sql")
	if secondErr != nil {
		t.Fatal(secondErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	third, err := fs.ReadFile(first, "0003_tables.sql")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	for range 3 {
		next := scripts.Migrations()
		assertEntries(t, next, ".", []string{"0001_catalog.sql", "0002_subscriptions.sql", "0003_tables.sql"}, false)
		last, err := fs.ReadFile(next, "0003_tables.sql")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(last, third) {
			t.Fatal("third migration changed")
		}
		got, err := fs.ReadFile(next, "0001_catalog.sql")
		if err != nil {
			t.Fatal(err)
		}
		again, err := fs.ReadFile(next, "0002_subscriptions.sql")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(again, second) {
			t.Fatal("second migration changed")
		}
		if !bytes.Equal(got, contents) {
			t.Fatal("migration changed")
		}
	}
}

// R-BGG3-5KJB R-YCYS-PQMD
func TestCatalogSchema(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(t.TempDir(), "scripts.db"), Migrations: scripts.Migrations(), Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	err = d.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT GLOB 'sqlite_*' ORDER BY name")
		if err != nil {
			return err
		}
		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			names = append(names, name)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if !reflect.DeepEqual(names, []string{"catalog", "event_runs", "runs", "schema_migrations", "scripts", "subscriptions"}) {
			t.Fatalf("tables: %v", names)
		}
		for _, table := range []string{"catalog", "subscriptions", "event_runs", "scripts", "runs"} {
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Fatalf("seeded %s", table)
			}
		}
		rows, err = tx.QueryContext(ctx, "PRAGMA table_info(catalog)")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
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
			if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.NotNull, &c.Default, &c.PK); err != nil {
				return err
			}
			columns = append(columns, c)
		}
		if len(columns) != 2 || columns[0].Name != "id" || columns[0].Type != "INTEGER" || columns[0].PK != 1 || columns[1].Name != "value" || columns[1].Type != "BLOB" || columns[1].NotNull != 1 || columns[1].PK != 0 {
			t.Fatalf("columns: %#v", columns)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

// R-RX08-F5IC R-RY84-SX91
func TestSubscriptionTableShapes(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(t.TempDir(), "scripts.db"), Migrations: scripts.Migrations(), Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	err = d.Read(ctx, func(tx *sql.Tx) error {
		for _, table := range []string{"subscriptions", "event_runs"} {
			rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+table+")")
			if err != nil {
				return err
			}
			names := []string{}
			for rows.Next() {
				var id, notnull, pk int
				var name, typ string
				var def any
				if err = rows.Scan(&id, &name, &typ, &notnull, &def, &pk); err != nil {
					_ = rows.Close()
					return err
				}
				names = append(names, name)
				wantType := "TEXT"
				wantPK := id + 1
				if id == 2 {
					wantType = "INTEGER"
					wantPK = 0
				}
				if typ != wantType || notnull != 1 || pk != wantPK {
					t.Fatalf("%s column %s shape %s %d %d", table, name, typ, notnull, pk)
				}
			}
			err = rows.Err()
			_ = rows.Close()
			if err != nil {
				return err
			}
			want := []string{"script", "event"}
			if table == "subscriptions" {
				want = append(want, "created")
			}
			if !reflect.DeepEqual(names, want) {
				t.Fatalf("%s columns %v", table, names)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// R-BBKH-MHKJ R-BCSE-09B8 R-BE0A-E11X
func TestMirrorTableShapes(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(t.TempDir(), "scripts.db"), Migrations: scripts.Migrations(), Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	err = d.Read(ctx, func(tx *sql.Tx) error {
		for table, want := range map[string][]string{"scripts": {"id", "name", "owner", "repo", "ref", "created"}, "runs": {"id", "script", "sha", "ref", "user", "request_id", "trigger", "event", "status", "exit_code", "started", "finished", "stdout_bytes", "stderr_bytes", "truncated", "reason"}} {
			rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+table+")")
			if err != nil {
				return err
			}
			names := []string{}
			for rows.Next() {
				var id, notnull, pk int
				var name, typ string
				var def any
				if err := rows.Scan(&id, &name, &typ, &notnull, &def, &pk); err != nil {
					_ = rows.Close()
					return err
				}
				names = append(names, name)
				wantType := "TEXT"
				if table == "runs" && (id == 9 || id >= 12 && id <= 14) {
					wantType = "INTEGER"
				}
				wantNull := 1
				if table == "runs" && id == 11 {
					wantNull = 0
				}
				wantPK := 0
				if id == 0 {
					wantPK = 1
				}
				if id != len(names)-1 || typ != wantType || notnull != wantNull || pk != wantPK {
					t.Fatalf("%s column %s: %d %s %d %d", table, name, id, typ, notnull, pk)
				}
			}
			err = rows.Err()
			_ = rows.Close()
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(names, want) {
				t.Fatalf("%s columns %v", table, names)
			}
		}
		type index struct {
			name, origin string
			unique       int
		}
		for table, wantName := range map[string]string{"scripts": "scripts_owner_name", "runs": "runs_script_started_id"} {
			rows, err := tx.QueryContext(ctx, "PRAGMA index_list("+table+")")
			if err != nil {
				return err
			}
			indexes := []index{}
			for rows.Next() {
				var seq, partial int
				var i index
				if err := rows.Scan(&seq, &i.name, &i.unique, &i.origin, &partial); err != nil {
					_ = rows.Close()
					return err
				}
				indexes = append(indexes, i)
			}
			err = rows.Err()
			_ = rows.Close()
			if err != nil {
				return err
			}
			found, uniqueName := false, false
			for _, i := range indexes {
				rows, err := tx.QueryContext(ctx, "PRAGMA index_info("+i.name+")")
				if err != nil {
					return err
				}
				columns := []string{}
				for rows.Next() {
					var seq, cid int
					var name string
					if err := rows.Scan(&seq, &cid, &name); err != nil {
						_ = rows.Close()
						return err
					}
					columns = append(columns, name)
				}
				err = rows.Err()
				_ = rows.Close()
				if err != nil {
					return err
				}
				if table == "scripts" && i.unique == 1 && reflect.DeepEqual(columns, []string{"name"}) {
					uniqueName = true
				}
				if i.name == wantName {
					found = true
					want := []string{"owner", "name"}
					if table == "runs" {
						want = []string{"script", "started", "id"}
					}
					if i.origin != "c" || i.unique != 0 || !reflect.DeepEqual(columns, want) {
						t.Fatalf("index %#v columns %v", i, columns)
					}
				}
			}
			if !found || table == "scripts" && !uniqueName {
				t.Fatalf("missing indexes of %s", table)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
