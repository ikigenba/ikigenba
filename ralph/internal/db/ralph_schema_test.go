package db

import (
	"context"
	"testing"
)

// TestMigrate_CreatesRalphSchema verifies 002_ralph.sql lands the sessions
// and runs tables (and the run index) on a fresh DB, idempotently.
func TestMigrate_CreatesRalphSchema(t *testing.T) {
	ctx := context.Background()
	conn, err := Open(tempDB(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	// Migrate twice to confirm idempotency of the full set including 002.
	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	for _, tbl := range []string{"sessions", "runs"} {
		var name string
		err := conn.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %q missing after migrate: %v", tbl, err)
		}
	}

	// The run index must exist.
	var idx string
	err = conn.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_runs_session'`,
	).Scan(&idx)
	if err != nil {
		t.Fatalf("expected idx_runs_session: %v", err)
	}

	// Inserting a session then a run, and cascading delete, all work.
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO sessions (id, owner_email, prompt, config_json, status, created_at, updated_at)
		 VALUES ('s1', 'o@example.com', 'hi', '{}', 'idle', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO runs (id, session_id, status, started_at, log_path)
		 VALUES ('r1', 's1', 'running', '2026-01-01T00:00:00Z', 'data/runs/s1/r1.jsonl')`,
	); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM sessions WHERE id='s1'`); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	var n int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE session_id='s1'`).Scan(&n); err != nil {
		t.Fatalf("count runs after cascade: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected ON DELETE CASCADE to remove runs, got %d", n)
	}
}
