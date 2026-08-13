package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appdb "appkit/db"
	"eventplane/outbox"
)

func TestEmbeddedMigrationsCreateV2MetadataSchema(t *testing.T) {
	// R-IWEY-SUZH
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations and drift guard: %v", err)
	}
	var outboxMigration appdb.Migration
	for _, migration := range migrations {
		if strings.Contains(migration.Name, "outbox") {
			outboxMigration = migration
		}
	}
	if !strings.Contains(outboxMigration.SQL, outbox.SchemaSQL) {
		t.Fatal("newest outbox migration does not contain outbox.SchemaSQL verbatim")
	}

	conn := migratedDatabase(t, migrations)
	want := map[string][]string{
		"repositories": {"id", "kind", "name", "owner_id", "owner_email", "default_branch", "archived_at", "archived_path", "created_at"},
		"statuses":     {"kind", "name", "sha", "check_name", "state", "detail", "actor", "updated_at"},
		"run_tokens":   {"token_sha256", "kind", "name", "expires_at", "created_at"},
		"outbox":       {"seq", "event_id", "kind", "subject", "payload", "created_at", "correlation_id"},
	}
	if got := applicationTables(t, conn); !reflect.DeepEqual(got, []string{"outbox", "repositories", "run_tokens", "statuses"}) {
		t.Fatalf("application tables = %v, want exactly the v2 tables", got)
	}
	for table, expected := range want {
		if got := tableColumns(t, conn, table); !reflect.DeepEqual(got, expected) {
			t.Errorf("%s columns = %v, want %v", table, got, expected)
		}
	}
	assertPrimaryKey(t, conn, "repositories", map[string]int{"id": 1})
	assertPrimaryKey(t, conn, "statuses", map[string]int{"kind": 1, "name": 2, "sha": 3, "check_name": 4})
	assertPrimaryKey(t, conn, "run_tokens", map[string]int{"token_sha256": 1})
	assertNotNull(t, conn, "repositories", "owner_id")
	assertNotNull(t, conn, "repositories", "owner_email")
	assertIndex(t, conn, "repositories", "idx_repositories_owner")
	assertIndex(t, conn, "repositories", "idx_repositories_live_key")
	for _, removed := range []string{"repos", "sessions", "feed_offset"} {
		if tableExists(t, conn, removed) {
			t.Errorf("legacy table %s still exists", removed)
		}
	}
}

func TestV2RebuildDropsSeededV1State(t *testing.T) {
	// R-IYUR-KEGV
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	rebuild := -1
	for i, migration := range migrations {
		if strings.Contains(migration.Name, "rebuild_v2_store") {
			rebuild = i
			break
		}
	}
	if rebuild < 1 {
		t.Fatal("v2 rebuild migration not found")
	}
	conn, err := appdb.Open(filepath.Join(t.TempDir(), "repos.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := appdb.Migrate(context.Background(), conn, migrations[:rebuild]); err != nil {
		t.Fatalf("apply frozen v1 migrations: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO repos (name,owner_id,owner_email,clone_url,default_branch,created_at) VALUES ('old','owner','same@example.com','file:///old','main','2026-08-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO sessions (id,repo_name,owner_id,owner_email,attempt,branch,instructions,status,created_at,log_path,correlation_id) VALUES ('run','old','owner','same@example.com',1,'work','do it','done','2026-08-01T00:00:00Z','run.jsonl','corr')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO feed_offset (source,cursor,subscribed,updated_at) VALUES ('github','1',1,'2026-08-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := appdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatalf("apply full migration set over seeded v1 shape: %v", err)
	}
	for _, removed := range []string{"repos", "sessions", "feed_offset"} {
		if tableExists(t, conn, removed) {
			t.Errorf("legacy table %s still exists", removed)
		}
	}
	for _, table := range []string{"repositories", "statuses", "run_tokens"} {
		var count int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s row count = %d, want zero", table, count)
		}
	}
}

func migratedDatabase(t *testing.T, migrations []appdb.Migration) *sql.DB {
	t.Helper()
	conn, err := appdb.Open(filepath.Join(t.TempDir(), "repos.db"))
	if err != nil {
		t.Fatalf("open temp database: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := appdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatalf("apply full embedded migration set: %v", err)
	}
	return conn
}

func applicationTables(t *testing.T, conn *sql.DB) []string {
	t.Helper()
	rows, err := conn.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name != 'schema_migrations' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return tables
}

func tableExists(t *testing.T, conn *sql.DB, table string) bool {
	t.Helper()
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}

func tableColumns(t *testing.T, conn *sql.DB, table string) []string {
	t.Helper()
	rows, err := conn.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatalf("query %s columns: %v", table, err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan %s column: %v", table, err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read %s columns: %v", table, err)
	}
	return columns
}

func assertPrimaryKey(t *testing.T, conn *sql.DB, table string, want map[string]int) {
	t.Helper()
	rows, err := conn.Query(`SELECT name, pk FROM pragma_table_info(?) WHERE pk > 0`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := make(map[string]int)
	for rows.Next() {
		var name string
		var position int
		if err := rows.Scan(&name, &position); err != nil {
			t.Fatal(err)
		}
		got[name] = position
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s primary key = %v, want %v", table, got, want)
	}
}

func assertNotNull(t *testing.T, conn *sql.DB, table, column string) {
	t.Helper()
	var notNull int
	if err := conn.QueryRow(`SELECT "notnull" FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&notNull); err != nil || notNull != 1 {
		t.Errorf("%s.%s NOT NULL = %d, %v", table, column, notNull, err)
	}
}

func assertIndex(t *testing.T, conn *sql.DB, table, index string) {
	t.Helper()
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_index_list(?) WHERE name = ?`, table, index).Scan(&count); err != nil || count != 1 {
		t.Errorf("%s index %s count = %d, %v", table, index, count, err)
	}
}
