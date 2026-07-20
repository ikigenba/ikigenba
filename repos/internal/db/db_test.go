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

func TestEmbeddedMigrationsCreateExactDomainAndOutboxSchemas(t *testing.T) {
	// R-EMGN-7X72
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
		t.Fatal("outbox migration does not contain outbox.SchemaSQL verbatim")
	}

	conn, err := appdb.Open(filepath.Join(t.TempDir(), "repos.db"))
	if err != nil {
		t.Fatalf("open temp database: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := appdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatalf("apply full embedded migration set: %v", err)
	}

	want := map[string][]string{
		"repos":    {"name", "owner_id", "owner_email", "clone_url", "default_branch", "created_at"},
		"sessions": {"id", "repo_name", "owner_id", "owner_email", "issue_number", "attempt", "branch", "instructions", "status", "error", "pr_url", "created_at", "started_at", "ended_at", "log_path"},
		"outbox":   {"seq", "event_id", "kind", "subject", "payload", "created_at"},
	}
	for table, expected := range want {
		if got := tableColumns(t, conn, table); !reflect.DeepEqual(got, expected) {
			t.Errorf("%s columns = %v, want %v", table, got, expected)
		}
	}
	for _, column := range tableColumns(t, conn, "outbox") {
		if column == "type" {
			t.Fatal("outbox unexpectedly has legacy type column")
		}
	}
}

func TestOwnerIDMigrationRebuildsAndDropsEmailKeyedRows(t *testing.T) {
	// R-ICIJ-13TA
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := appdb.Open(filepath.Join(t.TempDir(), "repos.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := appdb.Migrate(context.Background(), conn, migrations[:len(migrations)-1]); err != nil {
		t.Fatalf("apply pre-conversion migrations: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO repos VALUES ('old','same@example.com','file:///old','main','2026-07-19T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO sessions (id,repo_name,owner_email,attempt,branch,instructions,status,created_at,log_path) VALUES ('old','old','same@example.com',1,'old','old','queued','2026-07-19T00:00:00Z','old.jsonl')`); err != nil {
		t.Fatal(err)
	}
	if err := appdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatalf("apply owner-id rebuild: %v", err)
	}
	for _, table := range []string{"repos", "sessions"} {
		var count int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s rows after rebuild = %d, %v; want zero", table, count, err)
		}
		for _, column := range []string{"owner_id", "owner_email"} {
			var notNull int
			if err := conn.QueryRow(`SELECT "notnull" FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&notNull); err != nil || notNull != 1 {
				t.Fatalf("%s.%s NOT NULL = %d, %v", table, column, notNull, err)
			}
		}
		var indexes int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_index_list(?) WHERE name = ?`, table, "idx_"+table+"_owner").Scan(&indexes); err != nil || indexes != 1 {
			t.Fatalf("%s owner index count = %d, %v", table, indexes, err)
		}
	}
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
