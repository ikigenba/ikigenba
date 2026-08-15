package db

import (
	"context"
	"database/sql"
	"eventplane/outbox"
	"reflect"
	"strings"
	"testing"

	appkitdb "appkit/db"
)

// TestOutboxMigrationsMatchLibrarySchema guards the forward migration from the
// frozen legacy outbox DDL to the eventplane library's current kind/subject
// contract.
func TestOutboxMigrationsMatchLibrarySchema(t *testing.T) {
	// R-X9ED-THOL
	legacy, err := migrationsFS.ReadFile("migrations/003_outbox.sql")
	if err != nil {
		t.Fatalf("read 003_outbox.sql: %v", err)
	}
	const frozen003 = `-- Event-plane outbox (event-protocol.md §4.5). The DDL is OWNED by the
-- eventplane library (outbox.SchemaSQL); this file must stay byte-identical to
-- that constant — internal/db/migrations_outbox_test.go asserts it. ledger's own
-- migration runner applies it so there is a single migration authority per DB
-- file, even though the schema's source of truth lives in the library.
CREATE TABLE outbox (
  seq        INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id   TEXT    NOT NULL,
  type       TEXT    NOT NULL,
  payload    TEXT    NOT NULL,
  created_at TEXT    NOT NULL
);
CREATE INDEX idx_outbox_created_at ON outbox(created_at);
`
	if string(legacy) != frozen003 {
		t.Fatal("003_outbox.sql differs from its frozen committed body")
	}
	body, err := migrationsFS.ReadFile("migrations/20260804175028_outbox_correlation_id.sql")
	if err != nil {
		t.Fatalf("read correlation migration: %v", err)
	}
	if !strings.Contains(string(body), outbox.AddCorrelationIDSQL) {
		t.Fatal("correlation migration must contain outbox.AddCorrelationIDSQL")
	}

	conn, err := appkitdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migs, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(context.Background(), conn, migs); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	columns := tableColumns(t, conn)

	want, err := appkitdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open canonical database: %v", err)
	}
	t.Cleanup(func() { want.Close() })
	if _, err := want.Exec(outbox.SchemaSQL); err != nil {
		t.Fatalf("create canonical outbox: %v", err)
	}
	wantColumns := tableColumns(t, want)
	if !reflect.DeepEqual(columns, wantColumns) {
		t.Fatalf("migrated outbox columns = %v, want canonical %v", columns, wantColumns)
	}
	if columns["type"] {
		t.Fatalf("legacy type column remains after migration: columns=%v", columns)
	}
	var kind, subject string
	if err := conn.QueryRow(`INSERT INTO outbox (event_id, kind, subject, payload, created_at)
		VALUES ('event', 'received', '', '{}', 'now')
		RETURNING kind, subject`).Scan(&kind, &subject); err != nil {
		t.Fatalf("insert current outbox shape: %v", err)
	}
	if kind != "received" || subject != "" {
		t.Fatalf("routed row = (%q, %q), want (received, empty subject)", kind, subject)
	}
}

func tableColumns(t *testing.T, conn interface {
	Query(string, ...any) (*sql.Rows, error)
},
) map[string]bool {
	t.Helper()
	columns := map[string]bool{}
	rows, err := conn.Query(`PRAGMA table_info(outbox)`)
	if err != nil {
		t.Fatalf("inspect outbox columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan outbox column: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate outbox columns: %v", err)
	}
	return columns
}
