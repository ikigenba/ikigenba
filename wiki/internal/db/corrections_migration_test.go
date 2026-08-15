package db

import (
	"context"
	"strings"
	"testing"

	appdb "appkit/db"
)

func TestCorrectionsMigrationPreservesClaimsAndCreatesSuppressions(t *testing.T) {
	// R-74CQ-9083
	ctx := context.Background()
	db, migrations := openScopeMigrationDB(t)
	defer db.Close()
	index := namedMigrationIndex(t, migrations, "corrections")
	if err := appdb.Migrate(ctx, db, migrations[:index]); err != nil {
		t.Fatalf("migrate pre-corrections schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO claims (id, subject_id, job_id, body) VALUES ('existing', 'subject', 'job', 'body')`); err != nil {
		t.Fatalf("seed existing claim: %v", err)
	}
	if err := appdb.Migrate(ctx, db, migrations); err != nil {
		t.Fatalf("apply corrections migration: %v", err)
	}

	var kind string
	if err := db.QueryRowContext(ctx, `SELECT kind FROM claims WHERE id = 'existing'`).Scan(&kind); err != nil {
		t.Fatalf("read preserved claim: %v", err)
	}
	if kind != "claim" {
		t.Fatalf("existing claim kind = %q, want claim", kind)
	}
	var notNull int
	var defaultValue string
	if err := db.QueryRowContext(ctx, `SELECT "notnull", dflt_value FROM pragma_table_info('claims') WHERE name = 'kind'`).Scan(&notNull, &defaultValue); err != nil {
		t.Fatalf("inspect claims.kind: %v", err)
	}
	if notNull != 1 || defaultValue != "'claim'" {
		t.Errorf("claims.kind NOT NULL = %d, default = %q; want 1 and 'claim'", notNull, defaultValue)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO claims (id, subject_id, job_id, body, kind) VALUES ('bad', 'subject', 'job', 'body', 'x')`); err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("invalid kind insert error = %v, want CHECK constraint failure", err)
	}

	rows, err := db.QueryContext(ctx, `PRAGMA table_info(suppressions)`)
	if err != nil {
		t.Fatalf("inspect suppressions: %v", err)
	}
	defer rows.Close()
	primaryKeys := map[string]int{}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, required, primaryKey int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &required, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan suppressions columns: %v", err)
		}
		columns[name] = required == 1
		primaryKeys[name] = primaryKey
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate suppressions columns: %v", err)
	}
	if !columns["correction"] || !columns["suppressed"] || !columns["created_at"] || primaryKeys["correction"] != 1 || primaryKeys["suppressed"] != 2 {
		t.Fatalf("suppressions columns = %#v, primary keys = %#v; want three required columns and (correction, suppressed) PK", columns, primaryKeys)
	}
	if got := queryCount(t, ctx, db, `SELECT COUNT(*) FROM suppressions`); got != 0 {
		t.Fatalf("suppressions row count = %d, want 0", got)
	}
}
