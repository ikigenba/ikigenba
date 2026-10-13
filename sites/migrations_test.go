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
	// R-481H-77QR R-499D-KZHG
	second, err := fs.ReadFile(sites.Migrations(), "0002_listed.sql")
	if err != nil {
		t.Fatal(err)
	}
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
		if len(entries) != 2 || entries[0].Name() != "0001_catalog.sql" || entries[1].Name() != "0002_listed.sql" || !entries[0].Type().IsRegular() || !entries[1].Type().IsRegular() {
			t.Fatalf("unexpected migrations: %v", entries)
		}
		secondContents, err := fs.ReadFile(files, "0002_listed.sql")
		if err != nil || !bytes.Equal(second, secondContents) {
			t.Fatalf("second migration changed: %v", err)
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
	// R-9U9Q-8OGL R-4AH9-YR85 R-Y074-44FP
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
			{"sites", []column{{0, "id", "TEXT", 0, nil, 1}, {1, "name", "TEXT", 1, nil, 0}, {2, "slug", "TEXT", 1, nil, 0}, {3, "owner", "TEXT", 1, nil, 0}, {4, "payload", "TEXT", 1, nil, 0}, {5, "listed", "INTEGER", 1, new("0"), 0}}},
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
				if got.Name != want.Name || got.Type != want.Type || got.PK != want.PK || c.table == "sites" && got.Name == "listed" && !reflect.DeepEqual(got.Default, want.Default) || i > 0 && got.NotNull != want.NotNull {
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

func TestCatalogIndexes(t *testing.T) {
	// R-4CX2-QAPJ
	ctx := context.Background()
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(t.TempDir(), "sites.db"), Migrations: sites.Migrations(), Now: func() time.Time { return time.Unix(123, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	err = d.Read(ctx, func(tx *sql.Tx) error {
		for _, table := range []string{"sites", "settings"} {
			rows, err := tx.QueryContext(ctx, "PRAGMA index_list("+table+")")
			if err != nil {
				return err
			}
			indexes := map[string][]string{}
			pk := 0
			for rows.Next() {
				var seq, unique, partial int
				var name, origin string
				if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
					_ = rows.Close()
					return err
				}
				if origin == "pk" {
					pk++
					continue
				}
				if origin != "c" || unique != 0 {
					t.Fatalf("unexpected index: %s %s %d %d", name, origin, unique, partial)
				}
				indexes[name] = nil
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return err
			}
			if err := rows.Close(); err != nil {
				return err
			}
			for name := range indexes {
				cols, err := tx.QueryContext(ctx, "PRAGMA index_info("+name+")")
				if err != nil {
					return err
				}
				for cols.Next() {
					var seq, cid int
					var col string
					if err := cols.Scan(&seq, &cid, &col); err != nil {
						_ = cols.Close()
						return err
					}
					indexes[name] = append(indexes[name], col)
				}
				if err := cols.Err(); err != nil {
					_ = cols.Close()
					return err
				}
				if err := cols.Close(); err != nil {
					return err
				}
			}
			want := map[string][]string{}
			if table == "sites" {
				want = map[string][]string{"sites_owner_name": {"owner", "name"}, "sites_slug": {"slug"}, "sites_listed_name": {"listed", "name"}}
			}
			if pk != 1 || !reflect.DeepEqual(indexes, want) {
				t.Fatalf("%s indexes: pk=%d %#v", table, pk, indexes)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
