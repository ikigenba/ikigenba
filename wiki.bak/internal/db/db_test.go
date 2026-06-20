package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"testing"

	appkitdb "appkit/db"
)

func tempDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

func tableExists(t *testing.T, ctx context.Context, conn *sql.DB, name string) bool {
	t.Helper()
	var got string
	err := conn.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&got)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("tableExists(%q): %v", name, err)
	}
	return got == name
}

func indexExists(t *testing.T, ctx context.Context, conn *sql.DB, name string) bool {
	t.Helper()
	var got string
	err := conn.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&got)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("indexExists(%q): %v", name, err)
	}
	return got == name
}

func tableColumns(t *testing.T, ctx context.Context, conn *sql.DB, table string) []string {
	t.Helper()
	rows, err := conn.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%q): %v", table, err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column name: %v", err)
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	return cols
}

func TestOpenAndMigrate(t *testing.T) {
	ctx := context.Background()
	conn, err := Open(tempDB(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("first migrate: %v", err)
	}

	var n int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n < 1 {
		t.Fatalf("want >=1 applied migration after first run, got %d", n)
	}
}

func TestMigrate_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	conn, err := Open(tempDB(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	var before int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&before); err != nil {
		t.Fatalf("count before: %v", err)
	}
	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var after int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&after); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if before != after {
		t.Fatalf("idempotent migrate changed count: %d -> %d", before, after)
	}
}

func TestMigrate_RefusesDowngrade(t *testing.T) {
	ctx := context.Background()
	conn, err := Open(tempDB(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("baseline migrate: %v", err)
	}
	// Simulate a future migration having run against this DB.
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		99999999999999, "2099-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("inject future version: %v", err)
	}

	err = Migrate(ctx, conn)
	if err == nil {
		t.Fatal("expected downgrade refusal, got nil")
	}
}

// TestDropLegacy_TablesGone asserts the drop-legacy migration (design §12.1)
// removes the dead wiki-specific tables (002_wiki.sql replays forward then this
// migration drops them) while PRESERVING the library-owned feed_offset cursor
// store the consume side still depends on.
func TestDropLegacy_TablesGone(t *testing.T) {
	ctx := context.Background()
	conn, err := Open(tempDB(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{"wiki_ingest", "wiki_jobs"} {
		if tableExists(t, ctx, conn, table) {
			t.Errorf("legacy table %q should be dropped by the drop-legacy migration, but it still exists", table)
		}
	}
	// feed_offset (consumer cursor, library-owned) and outbox (producer) both survive.
	for _, table := range []string{"feed_offset", "outbox"} {
		if !tableExists(t, ctx, conn, table) {
			t.Errorf("table %q must exist after migrate (feed_offset preserved, outbox added)", table)
		}
	}
}

// TestSchema_MatchesDesign12 is the §12 schema test. The expectations below are
// transcribed from design §12.2 — the EXTERNAL authoritative spec — so an
// omission cannot hide in both the DDL and this test (the test is checked against
// §12, not against the migration's own reading). It asserts every §12.2
// application table, its columns, and the named indexes/constraints §12 pins.
func TestSchema_MatchesDesign12(t *testing.T) {
	ctx := context.Background()
	conn, err := Open(tempDB(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Every §12.2 application table + pages_fts must exist.
	wantTables := []string{
		"inbox", "subjects", "aliases", "pages", "pages_fts",
		"runs", "dup_flags", "stale_notes", "page_vectors", "asks",
	}
	for _, tbl := range wantTables {
		if !tableExists(t, ctx, conn, tbl) {
			t.Errorf("§12 table %q missing after migrate", tbl)
		}
	}

	// Column sets, transcribed column-for-column from §12.2.
	wantCols := map[string][]string{
		"inbox": {
			"id", "owner", "kind", "source", "sha256", "size", "mime",
			"content", "blob", "title", "tags", "received_at", "integrated_by",
			"ineligible_until", "dead_at", "requeued_at",
		},
		"subjects": {"id", "type", "kind", "canonical_name", "created_by_run", "occurred_at"},
		"aliases":  {"type", "norm", "subject_id"},
		"pages":    {"subject", "title", "body", "version"},
		"runs": {
			"id", "job", "caused_by", "status", "started_at",
			"finished_at", "usage", "conflicts", "error",
		},
		"dup_flags":   {"subject_a", "subject_b", "status", "judged_version_a", "judged_version_b", "run_id"},
		"stale_notes": {"id", "subject", "note", "cites", "run_id", "status"},
		"page_vectors": {"subject", "embedded_version", "model", "vector"},
		"asks":        {"id", "owner", "question", "status", "started_at", "finished_at", "usage", "error"},
	}
	for tbl, cols := range wantCols {
		got := tableColumns(t, ctx, conn, tbl)
		want := append([]string(nil), cols...)
		sort.Strings(got)
		sort.Strings(want)
		if len(got) != len(want) {
			t.Errorf("table %q: column count mismatch\n got: %v\nwant: %v", tbl, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("table %q: column set mismatch\n got: %v\nwant: %v", tbl, got, want)
				break
			}
		}
	}

	// Named indexes §12.2 pins.
	for _, idx := range []string{"inbox_integrated_by", "inbox_sha256", "runs_caused_by"} {
		if !indexExists(t, ctx, conn, idx) {
			t.Errorf("§12 index %q missing", idx)
		}
	}

	// dup_flags UNIQUE(subject_a, subject_b) + CHECK(subject_a < subject_b):
	// a mis-ordered or duplicate insert is rejected.
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO dup_flags (subject_a, subject_b) VALUES (?, ?)`, "AAA", "BBB"); err != nil {
		t.Fatalf("first dup_flags insert: %v", err)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO dup_flags (subject_a, subject_b) VALUES (?, ?)`, "AAA", "BBB"); err == nil {
		t.Error("expected UNIQUE(subject_a, subject_b) to reject a duplicate pair")
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO dup_flags (subject_a, subject_b) VALUES (?, ?)`, "ZZZ", "AAA"); err == nil {
		t.Error("expected CHECK(subject_a < subject_b) to reject a mis-ordered pair")
	}

	// aliases UNIQUE(type, norm): the duplicate-mint guard.
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO aliases (type, norm, subject_id) VALUES (?,?,?)`, "entity", "acme", "s1"); err != nil {
		t.Fatalf("first alias insert: %v", err)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO aliases (type, norm, subject_id) VALUES (?,?,?)`, "entity", "acme", "s2"); err == nil {
		t.Error("expected UNIQUE(type, norm) to reject a duplicate (type, norm)")
	}
}

// TestLoadMigrations_Order guards that wiki's embedded migration set is
// well-formed: versions parse, are unique, and sort into strictly ascending
// order. Contiguity (no gaps) is NOT required — new migrations use sparse
// 14-digit timestamps (docs/adr-migration-timestamps.md).
func TestLoadMigrations_Order(t *testing.T) {
	migs, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("no migrations embedded")
	}
	for i := 1; i < len(migs); i++ {
		if migs[i].Version <= migs[i-1].Version {
			t.Errorf("migration %d version %d not strictly after prior %d", i, migs[i].Version, migs[i-1].Version)
		}
	}
}
