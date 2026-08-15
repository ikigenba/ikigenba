package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"eventplane/outbox"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	appkitdb "appkit/db"
)

func TestOutboxMigrationsMatchLibrarySchema(t *testing.T) {
	// R-84Q9-6QMD
	legacy, err := migrationsFS.ReadFile("migrations/004_outbox.sql")
	if err != nil {
		t.Fatalf("read 004_outbox.sql: %v", err)
	}
	if !strings.Contains(string(legacy), "type       TEXT    NOT NULL") {
		t.Fatal("004_outbox.sql must retain the frozen legacy type column")
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	newest := ""
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "outbox") && entry.Name() > newest {
			newest = entry.Name()
		}
	}
	if newest == "" {
		t.Fatal("no outbox migration found")
	}
	body, err := migrationsFS.ReadFile("migrations/" + newest)
	if err != nil {
		t.Fatalf("read newest outbox migration %s: %v", newest, err)
	}
	if !strings.Contains(string(body), outbox.SchemaSQL) {
		t.Fatalf("newest outbox migration %s must contain outbox.SchemaSQL verbatim", newest)
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
	columns := map[string]bool{}
	rows, err := conn.Query(`PRAGMA table_info(outbox)`)
	if err != nil {
		t.Fatalf("inspect outbox columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan outbox column: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate outbox columns: %v", err)
	}
	for _, name := range []string{"seq", "event_id", "kind", "subject", "payload", "created_at", "correlation_id"} {
		if !columns[name] || !strings.Contains(outbox.SchemaSQL, name) {
			t.Fatalf("migrated outbox and library schema must both contain %q: columns=%v", name, columns)
		}
	}
	if columns["type"] {
		t.Fatalf("legacy type column remains after migration: columns=%v", columns)
	}
}

func TestOutboxRoutingMigrationHasFrozenOriginalBody(t *testing.T) {
	// R-NJDQ-SLFQ
	const want = "9348de0c347feaa2f9ce74c90d53c9fcf17eed56fd66538e528c8f729b46eb38"
	body, err := migrationsFS.ReadFile("migrations/20260712192242_outbox_routing.sql")
	if err != nil {
		t.Fatalf("read frozen outbox migration: %v", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(body)); got != want {
		t.Fatalf("20260712192242_outbox_routing.sql digest = %s, want %s", got, want)
	}
}

func TestOutboxMigrationConvergesDeployedLineage(t *testing.T) {
	// R-NGXY-11YC
	ctx := context.Background()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "deployed-lineage.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	original := migrationIndex(t, migrations, "20260712192242_outbox_routing.sql")
	if err := appkitdb.Migrate(ctx, conn, migrations[:original+1]); err != nil {
		t.Fatalf("prepare deployed lineage: %v", err)
	}
	if columns := outboxColumns(t, conn); strings.Contains(","+strings.Join(columns, ",")+",", ",correlation_id,") {
		t.Fatalf("deployed lineage unexpectedly has correlation_id: %v", columns)
	}
	if err := appkitdb.Migrate(ctx, conn, migrations); err != nil {
		t.Fatalf("migrate deployed lineage to current: %v", err)
	}
	wantColumns := "seq,event_id,kind,subject,payload,created_at,correlation_id"
	if got := strings.Join(outboxColumns(t, conn), ","); got != wantColumns {
		t.Fatalf("converged outbox columns = %s, want library schema columns %s", got, wantColumns)
	}
	producer, err := outbox.New(conn, outbox.Options{Source: "scripts"})
	if err != nil {
		t.Fatalf("open outbox producer: %v", err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin append transaction: %v", err)
	}
	if err := producer.Append(ctx, tx, outbox.Event{
		Kind:    "scripts.succeeded",
		Subject: "/scripts/example/runs/example",
		Payload: json.RawMessage(`{"status":"succeeded"}`),
	}); err != nil {
		_ = tx.Rollback()
		t.Fatalf("append after convergence: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit append after convergence: %v", err)
	}
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM outbox`).Scan(&count); err != nil {
		t.Fatalf("count appended outbox row: %v", err)
	}
	if count != 1 {
		t.Fatalf("outbox row count = %d, want 1", count)
	}
}

func outboxColumns(t *testing.T, conn interface {
	Query(string, ...any) (*sql.Rows, error)
},
) []string {
	t.Helper()
	rows, err := conn.Query(`PRAGMA table_info(outbox)`)
	if err != nil {
		t.Fatalf("inspect outbox columns: %v", err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan outbox column: %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate outbox columns: %v", err)
	}
	return columns
}
