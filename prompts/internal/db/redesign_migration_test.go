package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	appkitdb "appkit/db"
)

// TestMigrate006_UpgradesOldSchemaPreservingData simulates a deployed box at
// migration version 5 (the ORIGINAL session-oriented schema) and proves that
// applying version 6 upgrades the schema in place while preserving existing
// rows: sessions->prompts, runs reshaped without FK + denormalized owner/name +
// trigger columns, session_triggers->prompt_triggers, old tables dropped, and
// tombstone (no-cascade) delete semantics.
func TestMigrate006_UpgradesOldSchemaPreservingData(t *testing.T) {
	ctx := context.Background()
	conn, err := appkitdb.Open(tempDB(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	// 1. Stand up the box's pre-upgrade state: ONLY versions <= 5.
	migs, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var pre []appkitdb.Migration
	for _, m := range migs {
		if m.Version <= 5 {
			pre = append(pre, m)
		}
	}
	if err := appkitdb.Migrate(ctx, conn, pre); err != nil {
		t.Fatalf("migrate to v5 (old schema): %v", err)
	}
	for _, tbl := range []string{"sessions", "runs", "session_triggers"} {
		var name string
		if err := conn.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&name); err != nil {
			t.Fatalf("old table %q missing after v5 migrate: %v", tbl, err)
		}
	}

	// 2. Insert representative OLD-schema rows.
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO sessions (id, owner_email, name, prompt, system_prompt, config_json, status, created_at, updated_at)
		 VALUES ('s1', 'o@example.com', 'nightly', 'do the thing', 'be brief', '{}', 'idle',
		         '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO runs (id, session_id, status, started_at, ended_at, usage_json, error, log_path)
		 VALUES ('r1', 's1', 'succeeded', '2026-01-01T00:01:00Z', '2026-01-01T00:02:00Z', '{}', NULL,
		         'data/runs/s1/r1.jsonl')`,
	); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO session_triggers (session_id, trigger_event, max_staleness_secs, max_attempts, created_at, updated_at)
		 VALUES ('s1', 'cron.nightly', 300, 3, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert session_trigger: %v", err)
	}

	// 3. Apply the FULL set (now including version 6) — the in-place upgrade.
	if err := appkitdb.Migrate(ctx, conn, migs); err != nil {
		t.Fatalf("full migrate (incl. v6): %v", err)
	}

	// 4a. prompts: carried over, prompt->user_prompt, no status column.
	var userPrompt, systemPrompt string
	if err := conn.QueryRowContext(ctx,
		`SELECT user_prompt, system_prompt FROM prompts WHERE id='s1'`,
	).Scan(&userPrompt, &systemPrompt); err != nil {
		t.Fatalf("select prompt s1: %v", err)
	}
	if userPrompt != "do the thing" {
		t.Fatalf("user_prompt = %q, want %q", userPrompt, "do the thing")
	}
	if systemPrompt != "be brief" {
		t.Fatalf("system_prompt = %q, want %q", systemPrompt, "be brief")
	}
	if _, err := conn.ExecContext(ctx, `SELECT status FROM prompts WHERE id='s1'`); err == nil {
		t.Fatalf("expected prompts to have NO status column, but query succeeded")
	} else if !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected error about missing status column, got: %v", err)
	}

	// 4b. runs: prompt_id, denormalized owner_email/prompt_name, log_path kept,
	//     trigger_* NULL for a pre-redesign run.
	var promptID, ownerEmail, promptName, logPath string
	var trigSource, trigType, trigEventID sql.NullString
	if err := conn.QueryRowContext(ctx,
		`SELECT prompt_id, owner_email, prompt_name, log_path, trigger_source, trigger_type, trigger_event_id
		 FROM runs WHERE id='r1'`,
	).Scan(&promptID, &ownerEmail, &promptName, &logPath, &trigSource, &trigType, &trigEventID); err != nil {
		t.Fatalf("select run r1: %v", err)
	}
	if promptID != "s1" {
		t.Fatalf("run prompt_id = %q, want s1", promptID)
	}
	if ownerEmail != "o@example.com" {
		t.Fatalf("run owner_email = %q, want o@example.com", ownerEmail)
	}
	if promptName != "nightly" {
		t.Fatalf("run prompt_name = %q, want nightly", promptName)
	}
	if logPath != "data/runs/s1/r1.jsonl" {
		t.Fatalf("run log_path = %q, want data/runs/s1/r1.jsonl", logPath)
	}
	if trigSource.Valid || trigType.Valid || trigEventID.Valid {
		t.Fatalf("expected trigger_* NULL, got source=%v type=%v event=%v", trigSource, trigType, trigEventID)
	}

	// 4c. prompt_triggers: exactly one row, mapped from the old session_trigger.
	var nTrig int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM prompt_triggers`).Scan(&nTrig); err != nil {
		t.Fatalf("count prompt_triggers: %v", err)
	}
	if nTrig != 1 {
		t.Fatalf("prompt_triggers count = %d, want 1", nTrig)
	}
	var tPrompt, tSource, tFilter, tCreated string
	if err := conn.QueryRowContext(ctx,
		`SELECT prompt_id, source, event_filter, created_at FROM prompt_triggers`,
	).Scan(&tPrompt, &tSource, &tFilter, &tCreated); err != nil {
		t.Fatalf("select prompt_trigger: %v", err)
	}
	if tPrompt != "s1" || tSource != "cron" || tFilter != "cron.nightly" {
		t.Fatalf("prompt_trigger = (%q,%q,%q), want (s1,cron,cron.nightly)", tPrompt, tSource, tFilter)
	}
	if tCreated != "2026-01-01T00:00:00Z" {
		t.Fatalf("prompt_trigger created_at = %q, want preserved 2026-01-01T00:00:00Z", tCreated)
	}

	// 4d. The old tables are GONE.
	for _, tbl := range []string{"sessions", "session_triggers"} {
		var name string
		err := conn.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&name)
		if err != sql.ErrNoRows {
			t.Fatalf("expected old table %q to be dropped, got name=%q err=%v", tbl, name, err)
		}
	}

	// 4e. Tombstone: deleting the prompt leaves its run (no cascade).
	if _, err := conn.ExecContext(ctx, `DELETE FROM prompts WHERE id='s1'`); err != nil {
		t.Fatalf("delete prompt: %v", err)
	}
	var nRuns int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE prompt_id='s1'`).Scan(&nRuns); err != nil {
		t.Fatalf("count runs after tombstone: %v", err)
	}
	if nRuns != 1 {
		t.Fatalf("expected NO cascade: run should survive prompt delete, got %d", nRuns)
	}
}
