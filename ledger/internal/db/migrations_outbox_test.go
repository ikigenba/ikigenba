package db

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"

	appkitdb "appkit/db"
	"eventplane/outbox"
)

type outboxColumn struct {
	Name       string
	Type       string
	NotNull    int
	DefaultSQL any
}

func outboxColumns(t *testing.T, conn *sql.DB) []outboxColumn {
	t.Helper()
	rows, err := conn.Query(`PRAGMA table_info(outbox)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var columns []outboxColumn
	for rows.Next() {
		var cid, primaryKey int
		var column outboxColumn
		if err := rows.Scan(&cid, &column.Name, &column.Type, &column.NotNull, &column.DefaultSQL, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return columns
}

func TestOutboxCorrelationMigrationMatchesFreshSchema(t *testing.T) {
	// R-Y3Z7-H9BG
	ctx := context.Background()
	upgraded, err := appkitdb.Open(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	if err := migrateLedger(ctx, upgraded); err != nil {
		t.Fatalf("apply ledger migrations: %v", err)
	}

	fresh, err := appkitdb.Open(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if _, err := fresh.ExecContext(ctx, outbox.SchemaSQL); err != nil {
		t.Fatalf("apply outbox.SchemaSQL: %v", err)
	}

	upgradedColumns := outboxColumns(t, upgraded)
	freshColumns := outboxColumns(t, fresh)
	if !reflect.DeepEqual(upgradedColumns, freshColumns) {
		t.Fatalf("migrated columns = %#v, fresh SchemaSQL columns = %#v", upgradedColumns, freshColumns)
	}
	last := upgradedColumns[len(upgradedColumns)-1]
	if last.Name != "correlation_id" || last.Type != "TEXT" || last.NotNull != 1 || last.DefaultSQL != "''" {
		t.Fatalf("correlation column = %#v, want last column TEXT NOT NULL DEFAULT ''", last)
	}
}

func TestOutboxRoutingMigrationMatchesSchemaAndPreservesFrozenMigration(t *testing.T) {
	// R-G184-OOBO
	const routingMigration = "migrations/20260712184833_outbox_routing.sql"
	const correlationMigration = "migrations/20260804115341_add_correlation_id_to_outbox.sql"
	const routingSchema = `CREATE TABLE outbox (
  seq        INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id   TEXT    NOT NULL,
  kind       TEXT    NOT NULL,
  subject    TEXT    NOT NULL DEFAULT '',
  payload    TEXT    NOT NULL,
  created_at TEXT    NOT NULL
);
CREATE INDEX idx_outbox_created_at ON outbox(created_at);
`
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
	routing, err := migrationsFS.ReadFile(routingMigration)
	if err != nil {
		t.Fatalf("read %s: %v", routingMigration, err)
	}
	if !strings.Contains(string(routing), routingSchema) {
		t.Fatalf("%s does not contain the frozen pre-correlation outbox schema verbatim:\n%s", routingMigration, routing)
	}
	correlation, err := migrationsFS.ReadFile(correlationMigration)
	if err != nil {
		t.Fatalf("read %s: %v", correlationMigration, err)
	}
	if !strings.Contains(string(correlation), outbox.AddCorrelationIDSQL) {
		t.Fatalf("%s does not contain outbox.AddCorrelationIDSQL verbatim:\n%s", correlationMigration, correlation)
	}

	body, err := migrationsFS.ReadFile("migrations/003_outbox.sql")
	if err != nil {
		t.Fatalf("read 003_outbox.sql: %v", err)
	}
	if string(body) != frozen003 {
		t.Fatalf("003_outbox.sql changed from its frozen committed body:\n%s", body)
	}
	for _, want := range []string{"event_id", "type", "payload", "created_at"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("003_outbox.sql missing frozen legacy column %q:\n%s", want, string(body))
		}
	}

	conn, err := appkitdb.Open(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := migrateLedger(context.Background(), conn); err != nil {
		t.Fatalf("migrate fresh database: %v", err)
	}
	rows, err := conn.Query(`PRAGMA table_info(outbox)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	if !columns["kind"] || !columns["subject"] || !columns["correlation_id"] || columns["type"] {
		t.Errorf("outbox columns = %v, want kind+subject+correlation_id and no type", columns)
	}
}
