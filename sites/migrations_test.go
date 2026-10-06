package sites_test

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
	"github.com/ikigenba/ikigenba/sites"
)

func TestEmbeddedMigrations(t *testing.T) {
	// R-WJRF-NZJU R-WKZC-1RAJ
	original, err := fs.ReadFile(sites.Migrations(), "0001_catalog.sql")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	for range 2 {
		files := sites.Migrations()
		entries, err := fs.ReadDir(files, ".")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "0001_catalog.sql" || !entries[0].Type().IsRegular() {
			t.Fatalf("unexpected migrations: %v", entries)
		}
		contents, err := fs.ReadFile(files, "0001_catalog.sql")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(contents, original) {
			t.Fatal("migration contents changed")
		}
	}
}

func TestCatalogSchema(t *testing.T) {
	// R-9U9Q-8OGL R-XXRB-CKYB R-Y074-44FP
	ctx := context.Background()
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(t.TempDir(), "sites.db"), Migrations: sites.Migrations(), Now: func() time.Time { return time.Unix(123, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	err = d.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
		if err != nil {
			return err
		}
		var tables []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				_ = rows.Close()
				return err
			}
			tables = append(tables, name)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if !reflect.DeepEqual(tables, []string{"schema_migrations", "settings", "sites"}) {
			t.Fatalf("tables: %v", tables)
		}
		type column struct {
			ID         int
			Name, Type string
			NotNull    int
			Default    *string
			PK         int
		}
		cases := []struct {
			table string
			want  []column
		}{
			{"sites", []column{{0, "id", "TEXT", 0, nil, 1}, {1, "name", "TEXT", 1, nil, 0}, {2, "slug", "TEXT", 1, nil, 0}, {3, "owner", "TEXT", 1, nil, 0}, {4, "payload", "TEXT", 1, nil, 0}}},
			{"settings", []column{{0, "key", "TEXT", 0, nil, 1}, {1, "value", "TEXT", 1, nil, 0}}},
		}
		for _, c := range cases {
			rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+c.table+")")
			if err != nil {
				return err
			}
			var columns []column
			for rows.Next() {
				var x column
				if err := rows.Scan(&x.ID, &x.Name, &x.Type, &x.NotNull, &x.Default, &x.PK); err != nil {
					_ = rows.Close()
					return err
				}
				columns = append(columns, x)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return err
			}
			if err := rows.Close(); err != nil {
				return err
			}
			if len(columns) != len(c.want) {
				t.Fatalf("%s columns: %#v", c.table, columns)
			}
			for i, got := range columns {
				want := c.want[i]
				if got.Name != want.Name || got.Type != want.Type || got.PK != want.PK || i > 0 && got.NotNull != want.NotNull {
					t.Fatalf("%s column %d: %#v", c.table, i, got)
				}
			}
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+c.table).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Fatalf("%s holds %d rows", c.table, count)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
