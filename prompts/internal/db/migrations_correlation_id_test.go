package db

import (
	"context"
	"testing"

	appkitdb "appkit/db"
)

func TestCorrelationIDMigrationAddsRunAndCallColumnsAndIndexes(t *testing.T) {
	// R-HIAG-2MGL
	conn, err := appkitdb.Open(tempDB(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, table := range []string{"runs", "calls"} {
		var count int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'correlation_id'`, table).Scan(&count); err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%s correlation_id columns = %d, want 1", table, count)
		}
	}
	for _, index := range []string{"runs_correlation", "calls_correlation"} {
		var count int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&count); err != nil {
			t.Fatalf("inspect %s: %v", index, err)
		}
		if count != 1 {
			t.Fatalf("index %s count = %d, want 1", index, count)
		}
	}
}
