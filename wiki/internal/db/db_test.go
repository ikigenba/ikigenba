package db

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	appdb "appkit/db"
)

func TestEmbeddedMigrationsApplyToTempSQLite(t *testing.T) {
	ctx := context.Background()
	conn, err := Open(t.TempDir() + "/wiki.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	migs, err := appdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("len(migs) = 0, want at least one embedded migration")
	}

	if err := appdb.Migrate(ctx, conn, migs); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	got, err := appdb.AppliedVersion(ctx, conn)
	if err != nil {
		t.Fatalf("AppliedVersion: %v", err)
	}
	if want := appdb.MaxEmbedded(migs); got != want {
		t.Fatalf("AppliedVersion = %d, want %d", got, want)
	}
	for _, table := range []string{"wiki_ingest", "wiki_jobs", "feed_offset"} {
		var name string
		err := conn.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %s was not created: %v", table, err)
		}
	}
}

func TestPhaseTwoDataModelSchema(t *testing.T) {
	// R-7SNG-0G9A
	ctx := context.Background()
	conn, err := Open(t.TempDir() + "/wiki.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, table := range []string{"jobs", "subjects", "claims", "pages"} {
		var name string
		err := conn.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE name = ?`, table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("schema object %s was not created: %v", table, err)
		}
	}
	for _, index := range []string{"jobs_status", "claims_subject", "claims_job"} {
		var name string
		err := conn.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, index,
		).Scan(&name)
		if err != nil {
			t.Fatalf("index %s was not created: %v", index, err)
		}
	}
	for _, table := range []string{"jobs", "subjects", "claims", "pages"} {
		rows, err := conn.QueryContext(ctx, `PRAGMA foreign_key_list(`+table+`)`)
		if err != nil {
			t.Fatalf("foreign_key_list(%s): %v", table, err)
		}
		if rows.Next() {
			rows.Close()
			t.Fatalf("table %s declares a foreign key, want comments-only relationships", table)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close foreign_key_list(%s): %v", table, err)
		}
	}

	if _, err := conn.ExecContext(ctx,
		`INSERT INTO subjects (id, name, norm_name, type) VALUES ('s1', 'Alpha', 'alpha', 'entity')`); err != nil {
		t.Fatalf("insert valid subject: %v", err)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO subjects (id, name, norm_name, type) VALUES ('s2', 'Alpha 2', 'alpha', 'entity')`); err == nil {
		t.Fatal("duplicate subjects.norm_name insert succeeded, want UNIQUE failure")
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO subjects (id, name, norm_name, type) VALUES ('s3', 'Bad', 'bad', 'person')`); err == nil {
		t.Fatal("invalid subject type insert succeeded, want CHECK failure")
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO pages (id, subject_id, title, body) VALUES ('p1', 's1', 'Too Long', ?)`,
		strings.Repeat("x", 12001)); err == nil {
		t.Fatal("oversized page body insert succeeded, want CHECK failure")
	}
}

func TestPhase18MigrationsDropPagesFTSAndRecordMigration(t *testing.T) {
	// R-PH8Z-YHNX
	ctx := context.Background()
	conn, err := Open(t.TempDir() + "/wiki.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	migs, err := appdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}
	var dropVersion int
	for _, mig := range migs {
		if strings.Contains(mig.Name, "drop_pages_fts") {
			dropVersion = mig.Version
			break
		}
	}
	if dropVersion == 0 {
		t.Fatal("drop_pages_fts migration not found in embedded migrations")
	}

	if err := appdb.Migrate(ctx, conn, migs); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var tableCount int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'pages_fts'`).
		Scan(&tableCount); err != nil {
		t.Fatalf("count pages_fts table: %v", err)
	}
	if tableCount != 0 {
		t.Fatalf("pages_fts table count = %d, want 0 after drop migration", tableCount)
	}

	var recorded int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, dropVersion).
		Scan(&recorded); err != nil {
		t.Fatalf("count recorded drop migration: %v", err)
	}
	if recorded != 1 {
		t.Fatalf("drop migration records = %d, want 1", recorded)
	}

	original, err := os.ReadFile("migrations/20260620185852_phase_02_data_model.sql")
	if err != nil {
		t.Fatalf("read original phase-02 migration: %v", err)
	}
	if !strings.Contains(string(original), "CREATE VIRTUAL TABLE pages_fts") {
		t.Fatal("original phase-02 migration no longer contains the pages_fts create statement")
	}
}

func TestPhase18RetiresRetrievePackage(t *testing.T) {
	if _, err := os.Stat("../retrieve"); err == nil {
		t.Fatal("internal/retrieve exists, want retired package removed")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat internal/retrieve: %v", err)
	}

	cmd := exec.Command("go", "list", "./...")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list ./... failed: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "wiki/internal/retrieve") {
		t.Fatalf("go list still includes retired package:\n%s", out)
	}
}
