package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	appkitdb "appkit/db"

	"eventplane/outbox"
)

// TestOutboxMigrationMatchesLibraryDDL guards the routing cutover's frozen base
// table. The later correlation migration deliberately extends this exact shape
// with an ALTER, and TestOutboxCorrelationMigrationMatchesLibrarySchema below
// proves the complete migrated result matches the current library schema.
// R-PSWY-UBFW
func TestOutboxRoutingMigrationMatchesLibraryDDLAndCreatesRoutingColumns(t *testing.T) {
	body, err := migrationsFS.ReadFile("migrations/20260712160651_outbox_routing.sql")
	if err != nil {
		t.Fatalf("read newest outbox migration: %v", err)
	}
	const routingDDL = `CREATE TABLE outbox (
  seq        INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id   TEXT    NOT NULL,
  kind       TEXT    NOT NULL,
  subject    TEXT    NOT NULL DEFAULT '',
  payload    TEXT    NOT NULL,
  created_at TEXT    NOT NULL
);
CREATE INDEX idx_outbox_created_at ON outbox(created_at);`
	if !strings.Contains(string(body), routingDDL) {
		t.Fatalf("routing migration does not contain its frozen base DDL.\n--- expected DDL ---\n%s\n--- migration file ---\n%s",
			routingDDL, string(body))
	}
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "cron.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer conn.Close()
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatalf("apply embedded migrations: %v", err)
	}
	rows, err := conn.Query(`PRAGMA table_info(outbox)`)
	if err != nil {
		t.Fatalf("inspect outbox: %v", err)
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
	if !columns["kind"] || !columns["subject"] || columns["type"] {
		t.Fatalf("outbox columns = %v, want kind and subject but no type", columns)
	}
}

type outboxColumn struct {
	Name       string
	Type       string
	NotNull    bool
	DefaultSQL string
}

func outboxColumns(t *testing.T, conn *sql.DB) []outboxColumn {
	t.Helper()
	rows, err := conn.Query(`PRAGMA table_info(outbox)`)
	if err != nil {
		t.Fatalf("inspect outbox: %v", err)
	}
	defer rows.Close()
	var columns []outboxColumn
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typ string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan outbox column: %v", err)
		}
		columns = append(columns, outboxColumn{Name: name, Type: typ, NotNull: notNull != 0, DefaultSQL: defaultValue.String})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate outbox columns: %v", err)
	}
	return columns
}

// R-2V3L-RE05
func TestOutboxCorrelationMigrationMatchesLibrarySchema(t *testing.T) {
	ctx := context.Background()
	migrated, err := appkitdb.Open(filepath.Join(t.TempDir(), "migrated.db"))
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer migrated.Close()
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(ctx, migrated, migrations); err != nil {
		t.Fatalf("apply embedded migrations: %v", err)
	}

	fresh, err := appkitdb.Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("open fresh database: %v", err)
	}
	defer fresh.Close()
	if _, err := fresh.ExecContext(ctx, outbox.SchemaSQL); err != nil {
		t.Fatalf("apply outbox.SchemaSQL: %v", err)
	}

	got := outboxColumns(t, migrated)
	want := outboxColumns(t, fresh)
	if len(got) != len(want) {
		t.Fatalf("migrated columns = %+v, schema columns = %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("column %d after migration = %+v, want schema column %+v", i, got[i], want[i])
		}
	}
	var correlationColumn *outboxColumn
	for i := range got {
		if got[i].Name == "correlation_id" {
			correlationColumn = &got[i]
			break
		}
	}
	if correlationColumn == nil {
		t.Fatal("migrated outbox has no correlation_id column")
	}
	if correlationColumn.Type != "TEXT" || !correlationColumn.NotNull || correlationColumn.DefaultSQL != "''" {
		t.Fatalf("correlation_id column = %+v, want TEXT NOT NULL DEFAULT ''", *correlationColumn)
	}
}
