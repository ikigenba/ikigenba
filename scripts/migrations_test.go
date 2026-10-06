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

// R-94AT-P0GM R-95IQ-2S7B
func TestMigrations(t *testing.T) {
	first := scripts.Migrations()
	assertEntries(t, first, ".", []string{"0001_catalog.sql"}, false)
	contents, err := fs.ReadFile(first, "0001_catalog.sql")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	for range 3 {
		next := scripts.Migrations()
		assertEntries(t, next, ".", []string{"0001_catalog.sql"}, false)
		got, err := fs.ReadFile(next, "0001_catalog.sql")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, contents) {
			t.Fatal("migration changed")
		}
	}
}

// R-YBQW-BYVO R-YCYS-PQMD
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
		if !reflect.DeepEqual(names, []string{"catalog", "schema_migrations"}) {
			t.Fatalf("tables: %v", names)
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM catalog").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("seeded %d catalog rows", count)
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
