package db

import (
	"database/sql"
	"strings"
	"testing"

	appkitdatabase "appkit/db"

	"eventplane/outbox"

	_ "modernc.org/sqlite"
)

// TestOutboxMigrationMatchesLibraryDDL guards the event-plane decision: the
// outbox table DDL is OWNED by the eventplane library (outbox.SchemaSQL);
// dropbox's newest outbox migration applies it. If the two drift, every
// producer's outbox is no longer identical — so this test fails loudly the
// moment they diverge.
func TestOutboxMigrationMatchesLibraryDDL(t *testing.T) {
	// R-QDLM-84SK
	body, err := migrationsFS.ReadFile("migrations/20260712185200_outbox_routing.sql")
	if err != nil {
		t.Fatalf("read newest outbox migration: %v", err)
	}
	if !strings.Contains(string(body), outbox.SchemaSQL) {
		t.Fatalf("newest outbox migration does not contain the library DDL verbatim.\n--- outbox.SchemaSQL ---\n%s\n--- migration file ---\n%s",
			outbox.SchemaSQL, string(body))
	}
	legacy, err := migrationsFS.ReadFile("migrations/003_outbox.sql")
	if err != nil {
		t.Fatalf("read frozen 003_outbox.sql: %v", err)
	}
	const frozenOutboxMigration = `-- Event-plane outbox (event-protocol.md §4.5). The DDL is OWNED by the
-- eventplane library (outbox.SchemaSQL); this file must stay byte-identical to
-- that constant — internal/db/migrations_outbox_test.go asserts it. ledger's own
-- migration runner applies it so there is a single migration authority per DB
-- file, even though the schema's source of truth lives in the library.
CREATE TABLE outbox (
  seq        INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id   TEXT    NOT NULL,
  kind       TEXT    NOT NULL,
  subject    TEXT    NOT NULL DEFAULT '',
  payload    TEXT    NOT NULL,
  created_at TEXT    NOT NULL
);
CREATE INDEX idx_outbox_created_at ON outbox(created_at);
`
	if string(legacy) != frozenOutboxMigration {
		t.Fatalf("frozen 003_outbox.sql drifted from its committed body")
	}

	conn, err := sql.Open("sqlite", "file:"+t.TempDir()+"/dropbox.db?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	migrations, err := appkitdatabase.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdatabase.Migrate(t.Context(), conn, migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rows, err := conn.Query(`PRAGMA table_info(outbox)`)
	if err != nil {
		t.Fatalf("outbox columns: %v", err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns[name] = true
	}
	if !columns["kind"] || !columns["subject"] || columns["type"] {
		t.Fatalf("outbox columns = %v, want kind and subject but no type", columns)
	}
}
