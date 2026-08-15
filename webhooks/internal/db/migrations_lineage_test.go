package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"eventplane/outbox"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	chassis "appkit/db"
)

type outboxColumn struct {
	Position     int
	Name         string
	Type         string
	NotNull      int
	DefaultValue sql.NullString
	PrimaryKey   int
}

// R-NLTJ-K4X4 — a database that already recorded the original column-less
// routing migration converges through the new migration and accepts an append.
func TestDeployedOutboxLineageConvergesAndAcceptsAppend(t *testing.T) {
	ctx := context.Background()
	conn, err := chassis.Open(filepath.Join(t.TempDir(), "deployed-lineage.db"))
	if err != nil {
		t.Fatalf("open deployed-lineage sqlite: %v", err)
	}
	defer conn.Close()

	migrations, err := chassis.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}
	const frozenVersion = 20260712201504
	for _, migration := range migrations {
		if migration.Version > frozenVersion {
			break
		}
		if _, err := conn.ExecContext(ctx, migration.SQL); err != nil {
			t.Fatalf("apply deployed migration %d_%s: %v", migration.Version, migration.Name, err)
		}
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`,
			migration.Version, "2026-07-12T20:15:04Z"); err != nil {
			t.Fatalf("record deployed migration %d_%s: %v", migration.Version, migration.Name, err)
		}
	}
	before := readOutboxColumns(t, conn)
	if hasColumn(before, "correlation_id") {
		t.Fatalf("deployed lineage unexpectedly has correlation_id before current migrations: %v", before)
	}

	if err := chassis.Migrate(ctx, conn, migrations); err != nil {
		t.Fatalf("migrate deployed lineage with current embedded set: %v", err)
	}

	canonical, err := chassis.Open(filepath.Join(t.TempDir(), "canonical-outbox.db"))
	if err != nil {
		t.Fatalf("open canonical sqlite: %v", err)
	}
	defer canonical.Close()
	if _, err := canonical.ExecContext(ctx, outbox.SchemaSQL); err != nil {
		t.Fatalf("apply outbox.SchemaSQL to canonical sqlite: %v", err)
	}
	wantColumns := readOutboxColumns(t, canonical)
	gotColumns := readOutboxColumns(t, conn)
	if !reflect.DeepEqual(gotColumns, wantColumns) {
		t.Fatalf("migrated outbox columns = %#v, want outbox.SchemaSQL columns %#v", gotColumns, wantColumns)
	}
	if !hasColumn(gotColumns, "correlation_id") {
		t.Fatalf("migrated outbox columns lack correlation_id: %v", gotColumns)
	}

	fixedNow := time.Date(2026, 8, 9, 5, 30, 0, 0, time.UTC)
	ob, err := outbox.New(conn, outbox.Options{
		Source: "webhooks",
		Now:    func() time.Time { return fixedNow },
		Registry: outbox.Registry{{
			Kind:        "lineage.test",
			Subject:     "/lineage",
			Description: "Migration lineage convergence probe.",
			Sample:      map[string]bool{"ok": true},
		}},
	})
	if err != nil {
		t.Fatalf("open real outbox after convergence: %v", err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin append transaction: %v", err)
	}
	if err := ob.Append(ctx, tx, outbox.Event{
		Kind:    "lineage.test",
		Subject: "/lineage",
		Payload: json.RawMessage(`{"ok":true}`),
	}); err != nil {
		tx.Rollback()
		t.Fatalf("append after convergence: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit append after convergence: %v", err)
	}
}

func readOutboxColumns(t *testing.T, conn *sql.DB) []outboxColumn {
	t.Helper()
	rows, err := conn.Query(`PRAGMA table_info(outbox)`)
	if err != nil {
		t.Fatalf("table_info(outbox): %v", err)
	}
	defer rows.Close()
	var columns []outboxColumn
	for rows.Next() {
		var column outboxColumn
		if err := rows.Scan(&column.Position, &column.Name, &column.Type, &column.NotNull, &column.DefaultValue, &column.PrimaryKey); err != nil {
			t.Fatalf("scan outbox column: %v", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate outbox columns: %v", err)
	}
	return columns
}

func hasColumn(columns []outboxColumn, name string) bool {
	for _, column := range columns {
		if column.Name == name {
			return true
		}
	}
	return false
}
