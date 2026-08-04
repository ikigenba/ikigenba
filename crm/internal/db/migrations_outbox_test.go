package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	appkitdb "appkit/db"
	"eventplane/outbox"
	_ "modernc.org/sqlite"
)

// R-8JX3-TO9U
// TestOutboxMigrationsMatchLibraryDDL guards Decision 3: the outbox table DDL
// is owned by eventplane. The routing migration created the pre-correlation
// shape and the later forward-only migration applies the library's upgrade SQL.
func TestOutboxMigrationsMatchLibraryDDL(t *testing.T) {
	body, err := migrationsFS.ReadFile("migrations/20260804114457_outbox_correlation_id.sql")
	if err != nil {
		t.Fatalf("read correlation migration: %v", err)
	}
	if !strings.Contains(string(body), outbox.AddCorrelationIDSQL) {
		t.Fatalf("correlation migration does not contain the library upgrade DDL verbatim.\n--- outbox.AddCorrelationIDSQL ---\n%s\n--- migration file ---\n%s",
			outbox.AddCorrelationIDSQL, string(body))
	}
}

// R-8JX3-TO9U
func TestMigrationsCreateRoutedOutbox(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	defer conn.Close()
	migs, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(context.Background(), conn, migs); err != nil {
		t.Fatalf("apply migrations: %v", err)
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
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("column rows: %v", err)
	}
	if !columns["kind"] || !columns["subject"] || !columns["correlation_id"] || columns["type"] {
		t.Fatalf("outbox columns = %v, want kind, subject, and correlation_id without type", columns)
	}
}
