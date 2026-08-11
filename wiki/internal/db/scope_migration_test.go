package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	appdb "appkit/db"
)

func TestScopeMigrationSeedsPrivateDefaultAndConstrainsVisibility(t *testing.T) {
	// R-GSMY-FWWD
	ctx := context.Background()
	db, migrations := openScopeMigrationDB(t)
	defer db.Close()
	if err := appdb.Migrate(ctx, db, migrations); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var name, visibility string
	var count int
	if err := db.QueryRowContext(ctx, `SELECT name, visibility, COUNT(*) FROM scopes`).Scan(&name, &visibility, &count); err != nil {
		t.Fatalf("read seeded scopes: %v", err)
	}
	if name != "default" || visibility != "private" || count != 1 {
		t.Fatalf("scopes = {%q, %q}, count %d; want {default, private}, count 1", name, visibility, count)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scopes (name, visibility, created_at) VALUES ('bad', 'shared', 0)`); err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("invalid visibility insert error = %v, want CHECK constraint failure", err)
	}
}

func TestScopedSubjectUniquenessIsPerScope(t *testing.T) {
	// R-GV2R-7GDR
	ctx := context.Background()
	db, migrations := openScopeMigrationDB(t)
	defer db.Close()
	if err := appdb.Migrate(ctx, db, migrations); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scopes (name, created_at) VALUES ('team-x', 0)`); err != nil {
		t.Fatalf("create team-x: %v", err)
	}
	for _, row := range []struct{ id, scope string }{{"default-subject", "default"}, {"team-subject", "team-x"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO subjects (id, scope, name, norm_name, type) VALUES (?, ?, 'Same', 'same', 'entity')`, row.id, row.scope); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}
	if got := queryCount(t, ctx, db, `SELECT COUNT(*) FROM subjects WHERE norm_name = 'same'`); got != 2 {
		t.Fatalf("cross-scope subject count = %d, want 2", got)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subjects (id, scope, name, norm_name, type) VALUES ('duplicate', 'team-x', 'Same Again', 'same', 'entity')`); err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Fatalf("same-scope duplicate error = %v, want UNIQUE constraint failure", err)
	}
}

func TestScopeMigrationDropsPreScopeContentAndRebuildsColumns(t *testing.T) {
	// R-GWAN-L84G
	ctx := context.Background()
	db, migrations := openScopeMigrationDB(t)
	defer db.Close()
	index := scopeMigrationIndex(t, migrations)
	if err := appdb.Migrate(ctx, db, migrations[:index]); err != nil {
		t.Fatalf("migrate pre-scope schema: %v", err)
	}
	seed := []string{
		`INSERT INTO jobs (id, status, owner_id, owner_email) VALUES ('old-job', 'done', 'owner', 'owner@example.com')`,
		`INSERT INTO subjects (id, name, norm_name, type) VALUES ('old-subject', 'Old', 'old', 'entity')`,
		`INSERT INTO claims (id, subject_id, job_id, body) VALUES ('old-claim', 'old-subject', 'old-job', 'claim')`,
		`INSERT INTO pages (id, subject_id, title, body) VALUES ('old-page', 'old-subject', 'Old', 'body')`,
		`INSERT INTO aliases (norm_name, subject_id, name, owner_id, owner_email, created_at) VALUES ('older', 'old-subject', 'Older', 'owner', 'owner@example.com', 'now')`,
	}
	for _, statement := range seed {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed pre-scope content with %q: %v", statement, err)
		}
	}
	if err := appdb.Migrate(ctx, db, migrations); err != nil {
		t.Fatalf("apply scope migration: %v", err)
	}
	for _, table := range []string{"subjects", "jobs", "aliases"} {
		columns := tableColumns(t, ctx, db, table)
		if nullable, ok := columns["scope"]; !ok || nullable {
			t.Errorf("%s scope column = present %t, nullable %t; want present and NOT NULL", table, ok, nullable)
		}
		if got := queryCount(t, ctx, db, `SELECT COUNT(*) FROM `+table); got != 0 {
			t.Errorf("%s row count = %d, want 0", table, got)
		}
	}
	for _, table := range []string{"claims", "pages"} {
		if _, ok := tableColumns(t, ctx, db, table)["scope"]; ok {
			t.Errorf("%s unexpectedly has scope column", table)
		}
		if got := queryCount(t, ctx, db, `SELECT COUNT(*) FROM `+table); got != 0 {
			t.Errorf("%s row count = %d, want 0", table, got)
		}
	}
}

func TestScopeInstructionsMigrationPreservesRowsAndConstrainsInstructions(t *testing.T) {
	// R-8FVJ-PUMZ
	ctx := context.Background()
	db, migrations := openScopeMigrationDB(t)
	defer db.Close()
	index := namedMigrationIndex(t, migrations, "scope_instructions")
	if err := appdb.Migrate(ctx, db, migrations[:index]); err != nil {
		t.Fatalf("migrate pre-instructions schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE scopes SET created_at = 67890 WHERE name = 'default'`); err != nil {
		t.Fatalf("seed existing default scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scopes (name, visibility, created_at) VALUES ('team-a', 'public', 12345)`); err != nil {
		t.Fatalf("seed existing scope: %v", err)
	}
	if err := appdb.Migrate(ctx, db, migrations); err != nil {
		t.Fatalf("apply instructions migration: %v", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT name, visibility, instructions, created_at FROM scopes ORDER BY name`)
	if err != nil {
		t.Fatalf("read preserved scopes: %v", err)
	}
	defer rows.Close()
	want := []struct {
		name, visibility, instructions string
		createdAt                      int64
	}{{"default", "private", "", 67890}, {"team-a", "public", "", 12345}}
	var got []struct {
		name, visibility, instructions string
		createdAt                      int64
	}
	for rows.Next() {
		var row struct {
			name, visibility, instructions string
			createdAt                      int64
		}
		if err := rows.Scan(&row.name, &row.visibility, &row.instructions, &row.createdAt); err != nil {
			t.Fatalf("scan preserved scope: %v", err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read preserved scopes: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("preserved scopes = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("preserved scope %d = %#v, want %#v", i, got[i], want[i])
		}
	}

	var notNull int
	var defaultValue string
	if err := db.QueryRowContext(ctx, `SELECT "notnull", dflt_value FROM pragma_table_info('scopes') WHERE name = 'instructions'`).Scan(&notNull, &defaultValue); err != nil {
		t.Fatalf("inspect instructions column: %v", err)
	}
	if notNull != 1 || defaultValue != "''" {
		t.Errorf("instructions column NOT NULL = %d, default = %#v; want 1 and %q", notNull, defaultValue, "''")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scopes (name, instructions, created_at) VALUES ('too-long', ?, 0)`, strings.Repeat("界", 4001)); err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("over-cap direct insert error = %v, want CHECK constraint failure", err)
	}
}

func openScopeMigrationDB(t *testing.T) (*sql.DB, []appdb.Migration) {
	t.Helper()
	db, err := appdb.Open(t.TempDir() + "/wiki.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	migrations, err := appdb.LoadMigrations(FS, "migrations")
	if err != nil {
		db.Close()
		t.Fatalf("LoadMigrations: %v", err)
	}
	return db, migrations
}

func scopeMigrationIndex(t *testing.T, migrations []appdb.Migration) int {
	t.Helper()
	return namedMigrationIndex(t, migrations, "scope_registry")
}

func namedMigrationIndex(t *testing.T, migrations []appdb.Migration, name string) int {
	t.Helper()
	for i, migration := range migrations {
		if strings.Contains(migration.Name, name) {
			return i
		}
	}
	t.Fatalf("%s migration not found", name)
	return -1
}

func queryCount(t *testing.T, ctx context.Context, db *sql.DB, query string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		t.Fatalf("query count %q: %v", query, err)
	}
	return count
}

func tableColumns(t *testing.T, ctx context.Context, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		columns[name] = notNull == 0
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	return columns
}
