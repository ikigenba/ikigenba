package db

import (
	"context"
	"path/filepath"
	"testing"

	appkitdb "appkit/db"
)

// TestLoadMigrations asserts that this service's real embedded migration set
// loads through appkit's shared runner without error (versions parse, are
// unique, and order correctly). An in-service duplicate or malformed migration
// file fails this test (docs/adr-migration-timestamps.md).
func TestLoadMigrations(t *testing.T) {
	migs, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("no migrations embedded")
	}
}

func TestTriggerFilterMigrationShape(t *testing.T) {
	// R-7TR5-QSY4
	db, err := appkitdb.Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migs, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(context.Background(), db, migs); err != nil {
		t.Fatal(err)
	}
	columns := func(table string) map[string]bool {
		rows, err := db.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		got := map[string]bool{}
		for rows.Next() {
			var cid int
			var name, typ string
			var notNull, pk int
			var def any
			if err := rows.Scan(&cid, &name, &typ, &notNull, &def, &pk); err != nil {
				t.Fatal(err)
			}
			got[name] = true
		}
		return got
	}
	triggers := columns("script_triggers")
	for _, name := range []string{"script_id", "source", "filter", "created_at"} {
		if !triggers[name] {
			t.Errorf("script_triggers missing %s", name)
		}
	}
	if triggers["event"+"_filter"] {
		t.Error("script_triggers still has the retired filter column")
	}
	runs := columns("runs")
	if !runs["trigger_kind"] || !runs["trigger_subject"] || runs["trigger_type"] {
		t.Fatalf("runs columns = %v", runs)
	}
}
