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

func TestVersionPlaneMigrationShapeAndFrozenHistory(t *testing.T) {
	// R-20IL-OWGP
	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "version-plane.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	versionPlane := migrationIndex(t, migrations, "_version_plane.sql")
	if err := appkitdb.Migrate(context.Background(), database, migrations[:versionPlane+1]); err != nil {
		t.Fatal(err)
	}
	for table, names := range map[string][]string{
		"scripts": {"body", "source_path", "name_key", "repo_seeded_at"},
		"runs":    {"repo_sha"},
	} {
		for _, name := range names {
			var count int
			if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, name).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Errorf("%s.%s count = %d, want 1", table, name, count)
			}
		}
	}
	var unique int
	if err := database.QueryRow(`SELECT [unique] FROM pragma_index_list('scripts') WHERE name = 'idx_scripts_name_key'`).Scan(&unique); err != nil {
		t.Fatal(err)
	}
	if unique != 1 {
		t.Fatalf("idx_scripts_name_key unique = %d, want 1", unique)
	}
	assertIndexColumns(t, database, "idx_scripts_name_key", []string{"name_key"})

	frozenDigests := map[string]string{
		"002_scripts.sql":                    "b71fe8a87367ea55a253c6425fa9bbc457e56ce307442a8bf659658c9f9d07cd",
		"20260609135007_add_source_path.sql": "f27a399bd7e3ae3d7270f6967e01b647d8a22c0db55b13c94295357d8b9b9d73",
		"20260720020257_owner_id_keying.sql": "9afb51a25be29983a341d06859ceb6a7cfeeddc58444d1d4f9c9eb50d2f9c4f0",
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

func TestVersionPlaneMigrationPreservesExistingScript(t *testing.T) {
	// R-21QI-2O7E
	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "version-plane-existing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	versionPlane := migrationIndex(t, migrations, "_version_plane.sql")
	ctx := context.Background()
	if err := appkitdb.Migrate(ctx, database, migrations[:versionPlane]); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO scripts
		(id, owner_id, owner_email, name, body, config_json, source_path, created_at, updated_at)
		VALUES ('script-before-plane', 'owner-before-plane', 'before@example.test', 'Before',
		        'print("preserve me")', '{}', '/scripts/before.py', 'created', 'updated')`); err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(ctx, database, migrations[:versionPlane+1]); err != nil {
		t.Fatal(err)
	}
	var id, ownerID, body, sourcePath string
	var nameKey, repoSeededAt sql.NullString
	if err := database.QueryRow(`SELECT id, owner_id, body, source_path, name_key, repo_seeded_at
		FROM scripts WHERE id = 'script-before-plane'`).Scan(
		&id, &ownerID, &body, &sourcePath, &nameKey, &repoSeededAt,
	); err != nil {
		t.Fatal(err)
	}
	if id != "script-before-plane" || ownerID != "owner-before-plane" || body != `print("preserve me")` || sourcePath != "/scripts/before.py" {
		t.Fatalf("preserved row = id %q owner %q body %q source %q", id, ownerID, body, sourcePath)
	}
	if nameKey.Valid || repoSeededAt.Valid {
		t.Fatalf("new columns = name_key %#v repo_seeded_at %#v, want NULL", nameKey, repoSeededAt)
	}
}

func TestRetireBodyMigrationIsGuardedAndPreservesStampedRows(t *testing.T) {
	// R-2YNS-EH85
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	retireBody := migrationIndex(t, migrations, "_retire_body.sql")
	ctx := context.Background()

	t.Run("fully stamped", func(t *testing.T) {
		database, err := appkitdb.Open(filepath.Join(t.TempDir(), "stamped.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		if err := appkitdb.Migrate(ctx, database, migrations[:retireBody]); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`INSERT INTO scripts
			(id, owner_id, owner_email, name, body, config_json, source_path, name_key, repo_seeded_at, created_at, updated_at)
			VALUES ('stamped-script', 'owner-1', 'owner@example.test', 'Stamped', 'print("safe")', '{}', NULL,
			        'stamped', '2026-08-08T00:00:00Z', 'created', 'updated')`); err != nil {
			t.Fatal(err)
		}
		if err := appkitdb.Migrate(ctx, database, migrations); err != nil {
			t.Fatalf("retire stamped body: %v", err)
		}
		var bodyColumns int
		if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('scripts') WHERE name = 'body'`).Scan(&bodyColumns); err != nil {
			t.Fatal(err)
		}
		if bodyColumns != 0 {
			t.Fatalf("scripts.body columns = %d, want none", bodyColumns)
		}
		var id, ownerID, nameKey, seededAt string
		if err := database.QueryRow(`SELECT id, owner_id, name_key, repo_seeded_at FROM scripts`).Scan(&id, &ownerID, &nameKey, &seededAt); err != nil {
			t.Fatal(err)
		}
		if id != "stamped-script" || ownerID != "owner-1" || nameKey != "stamped" || seededAt != "2026-08-08T00:00:00Z" {
			t.Fatalf("surviving row = (%q, %q, %q, %q)", id, ownerID, nameKey, seededAt)
		}
	})

	t.Run("unstamped", func(t *testing.T) {
		database, err := appkitdb.Open(filepath.Join(t.TempDir(), "unstamped.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		if err := appkitdb.Migrate(ctx, database, migrations[:retireBody]); err != nil {
			t.Fatal(err)
		}
		const wantBody = `print("only copy")`
		if _, err := database.Exec(`INSERT INTO scripts
			(id, owner_id, owner_email, name, body, config_json, source_path, name_key, repo_seeded_at, created_at, updated_at)
			VALUES ('unstamped-script', 'owner-2', 'owner@example.test', 'Unstamped', ?, '{}', NULL,
			        'unstamped', NULL, 'created', 'updated')`, wantBody); err != nil {
			t.Fatal(err)
		}
		if err := appkitdb.Migrate(ctx, database, migrations); err == nil {
			t.Fatal("retire_body migration succeeded with an unstamped row")
		}
		var gotBody string
		if err := database.QueryRow(`SELECT body FROM scripts WHERE id = 'unstamped-script'`).Scan(&gotBody); err != nil {
			t.Fatalf("body column did not survive failed migration: %v", err)
		}
		if gotBody != wantBody {
			t.Fatalf("body after failed migration = %q, want %q", gotBody, wantBody)
		}
	})

	frozenDigests := map[string]string{
		"001_schema_migrations.sql":          "7e8c6c19c828fbc74b68defef9844323d284938f7af921b397bf7c254454886b",
		"002_scripts.sql":                    "b71fe8a87367ea55a253c6425fa9bbc457e56ce307442a8bf659658c9f9d07cd",
		"003_feed_offset.sql":                "02430258a28ed1a7a233efb17da6d63984f2feb4a3a91655a8f4d6428c26e137",
		"004_outbox.sql":                     "76296e7ac0263423de2210e73b1968814bd70bc39d0e220ee31f11f2879e078f",
		"20260609135007_add_source_path.sql": "f27a399bd7e3ae3d7270f6967e01b647d8a22c0db55b13c94295357d8b9b9d73",
		"20260712190612_trigger_filters.sql": "4c1326c9a8ccd27ec230816a4a3714043ad965287c7ddc459b5189b6945b967a",
		"20260712192242_outbox_routing.sql":  "9348de0c347feaa2f9ce74c90d53c9fcf17eed56fd66538e528c8f729b46eb38",
		"20260720020257_owner_id_keying.sql": "9afb51a25be29983a341d06859ceb6a7cfeeddc58444d1d4f9c9eb50d2f9c4f0",
		"20260804181951_correlation_id.sql":  "8570f1445c88c9592087cea2528b53249c684da255780b245b77a30fdf6c55e5",
		"20260809000848_version_plane.sql":   "65683a0392ac1d4a494c483edae544067d319eb8586cb8efc4dddab330a703dd",
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

func migrationIndex(t *testing.T, migrations []appkitdb.Migration, suffix string) int {
	t.Helper()
	for i, migration := range migrations {
		if strings.HasSuffix(migration.Name, suffix) {
			return i
		}
	}
	t.Fatalf("migration ending %q not found", suffix)
	return -1
}

func TestCorrelationIDMigrationAddsIndexedRunColumnWithoutChangingFrozenMigrations(t *testing.T) {
	// R-4OW5-Q1ND
	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "correlation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatal(err)
	}

	var columnCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('runs') WHERE name = 'correlation_id' AND type = 'TEXT' AND [notnull] = 1 AND dflt_value = "''"`).Scan(&columnCount); err != nil {
		t.Fatal(err)
	}
	if columnCount != 1 {
		t.Fatalf("runs.correlation_id matching required shape = %d, want 1", columnCount)
	}
	var indexCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_index_list('runs') WHERE name = 'idx_runs_correlation'`).Scan(&indexCount); err != nil {
		t.Fatal(err)
	}
	if indexCount != 1 {
		t.Fatalf("idx_runs_correlation count = %d, want 1", indexCount)
	}
	assertIndexColumns(t, database, "idx_runs_correlation", []string{"correlation_id"})

	frozenDigests := map[string]string{
		"002_scripts.sql":                    "b71fe8a87367ea55a253c6425fa9bbc457e56ce307442a8bf659658c9f9d07cd",
		"004_outbox.sql":                     "76296e7ac0263423de2210e73b1968814bd70bc39d0e220ee31f11f2879e078f",
		"20260609135007_add_source_path.sql": "f27a399bd7e3ae3d7270f6967e01b647d8a22c0db55b13c94295357d8b9b9d73",
		"20260712190612_trigger_filters.sql": "4c1326c9a8ccd27ec230816a4a3714043ad965287c7ddc459b5189b6945b967a",
		"20260712192242_outbox_routing.sql":  "9348de0c347feaa2f9ce74c90d53c9fcf17eed56fd66538e528c8f729b46eb38",
		"20260720020257_owner_id_keying.sql": "9afb51a25be29983a341d06859ceb6a7cfeeddc58444d1d4f9c9eb50d2f9c4f0",
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
