package db

import (
	"context"
	"testing"

	appdb "appkit/db"
)

func TestEmbeddedMigrationsApplyToTempSQLite(t *testing.T) {
	// R-6RVX-P1IG
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
