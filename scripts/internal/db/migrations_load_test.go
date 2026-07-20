package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestOwnerIDKeyingMigrationRebuildsScripts(t *testing.T) {
	// R-Q2LM-XR9W
	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "owner-id.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	ownerMigration := -1
	for i, migration := range migrations {
		if strings.HasSuffix(migration.Name, "_owner_id_keying.sql") {
			ownerMigration = i
			break
		}
	}
	if ownerMigration < 0 {
		t.Fatal("owner_id_keying migration not found")
	}
	ctx := context.Background()
	if err := appkitdb.Migrate(ctx, database, migrations[:ownerMigration]); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO scripts
		(id, owner_email, name, body, config_json, source_path, created_at, updated_at)
		VALUES ('old-script', 'old@example.test', 'old', 'print(1)', '{}', '/old.py', 'now', 'now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO runs
		(id, script_id, status, started_at, stdout_path, stderr_path)
		VALUES ('old-run', 'old-script', 'running', 'now', 'stdout', 'stderr')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO script_triggers
		(script_id, source, filter, created_at)
		VALUES ('old-script', 'dropbox', 'dropbox:**', 'now')`); err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(ctx, database, migrations); err != nil {
		t.Fatal(err)
	}

	type columnShape struct{ notNull bool }
	columns := map[string]columnShape{}
	rows, err := database.Query(`PRAGMA table_info(scripts)`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		columns[name] = columnShape{notNull: notNull == 1}
	}
	rows.Close()
	for _, name := range []string{"owner_id", "owner_email"} {
		if shape, ok := columns[name]; !ok || !shape.notNull {
			t.Errorf("scripts.%s = %#v, want present NOT NULL", name, shape)
		}
	}
	if _, ok := columns["source_path"]; !ok {
		t.Error("scripts.source_path missing")
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM scripts`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("scripts rows = %d, err = %v; want zero", count, err)
	}

	var ownerIndexUnique int
	if err := database.QueryRow(`SELECT [unique] FROM pragma_index_list('scripts') WHERE name = 'idx_scripts_owner'`).Scan(&ownerIndexUnique); err != nil {
		t.Fatal(err)
	}
	if ownerIndexUnique != 0 {
		t.Fatalf("idx_scripts_owner unique = %d, want plain index", ownerIndexUnique)
	}
	assertIndexColumns(t, database, "idx_scripts_owner", []string{"owner_id"})
	var sourceIndexUnique int
	if err := database.QueryRow(`SELECT [unique] FROM pragma_index_list('scripts') WHERE name = 'idx_scripts_source'`).Scan(&sourceIndexUnique); err != nil {
		t.Fatal(err)
	}
	if sourceIndexUnique != 1 {
		t.Fatalf("idx_scripts_source unique = %d, want UNIQUE", sourceIndexUnique)
	}
	assertIndexColumns(t, database, "idx_scripts_source", []string{"owner_id", "source_path"})

	frozenDigests := map[string]string{
		"002_scripts.sql":                    "b71fe8a87367ea55a253c6425fa9bbc457e56ce307442a8bf659658c9f9d07cd",
		"20260609135007_add_source_path.sql": "f27a399bd7e3ae3d7270f6967e01b647d8a22c0db55b13c94295357d8b9b9d73",
	}
	for name, want := range frozenDigests {
		body, err := os.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(body)); got != want {
			t.Errorf("%s digest = %s, want frozen %s", name, got, want)
		}
	}
}

func assertIndexColumns(t *testing.T, database interface {
	Query(string, ...any) (*sql.Rows, error)
}, index string, want []string) {
	t.Helper()
	rows, err := database.Query(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, index)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("%s columns = %v, want %v", index, got, want)
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
	wantTriggers := map[string]bool{"script_id": true, "source": true, "filter": true, "created_at": true}
	if len(triggers) != len(wantTriggers) {
		t.Errorf("script_triggers columns = %v, want exactly %v", triggers, wantTriggers)
	}
	for name := range wantTriggers {
		if !triggers[name] {
			t.Errorf("script_triggers missing %s", name)
		}
	}
	var pkScriptID, pkFilter int
	if err := db.QueryRow(`SELECT pk FROM pragma_table_info('script_triggers') WHERE name = 'script_id'`).Scan(&pkScriptID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT pk FROM pragma_table_info('script_triggers') WHERE name = 'filter'`).Scan(&pkFilter); err != nil {
		t.Fatal(err)
	}
	if pkScriptID != 1 || pkFilter != 2 {
		t.Fatalf("script_triggers primary key positions = (%d, %d), want (1, 2)", pkScriptID, pkFilter)
	}
	runs := columns("runs")
	if !runs["trigger_kind"] || !runs["trigger_subject"] || runs["trigger_type"] {
		t.Fatalf("runs columns = %v", runs)
	}

	// 002 is frozen: this digest is its committed body, not merely a successful
	// migration result that could hide a retrospective edit to the old schema.
	frozen, err := os.ReadFile("migrations/002_scripts.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(frozen)); got != "b71fe8a87367ea55a253c6425fa9bbc457e56ce307442a8bf659658c9f9d07cd" {
		t.Fatalf("002_scripts.sql digest = %s; frozen migration was edited", got)
	}
}
