package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	appkitdb "appkit/db"
)

func TestMigrateBackfillsProviderFromCanonicalModelIDs(t *testing.T) {
	// R-KBLR-VBHQ
	ctx := context.Background()
	conn, err := appkitdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	defer conn.Close()

	migrateBeforeProviderModelAliasBackfill(t, ctx, conn)
	insertPromptConfig(t, ctx, conn, "anthropic", `{"model":"claude-haiku-4-5","temperature":0.2}`)
	insertPromptConfig(t, ctx, conn, "openai", `{"model":"gpt-5.5"}`)
	insertPromptConfig(t, ctx, conn, "google", `{"model":"gemini-3.1-pro-preview"}`)
	insertPromptConfig(t, ctx, conn, "zai", `{"model":"glm-5.2"}`)

	migs, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	migs = throughNamedMigration(migs, "backfill_provider_and_model_aliases")
	if err := appkitdb.Migrate(ctx, conn, migs); err != nil {
		t.Fatalf("migrate backfill: %v", err)
	}

	assertConfigField(t, ctx, conn, "anthropic", "provider", "anthropic")
	assertConfigField(t, ctx, conn, "openai", "provider", "openai")
	assertConfigField(t, ctx, conn, "google", "provider", "google")
	assertConfigField(t, ctx, conn, "zai", "provider", "zai")
	cfg := readPromptConfig(t, ctx, conn, "anthropic")
	if cfg["temperature"] != 0.2 {
		t.Fatalf("temperature = %#v, want preserved 0.2", cfg["temperature"])
	}
}

func TestMigrateCanonicalizesLegacyModelAliases(t *testing.T) {
	// R-KCTO-938F
	ctx := context.Background()
	conn, err := appkitdb.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	defer conn.Close()

	migrateBeforeProviderModelAliasBackfill(t, ctx, conn)
	insertPromptConfig(t, ctx, conn, "sonnet", `{"model":"sonnet","max_tokens":123}`)
	insertPromptConfig(t, ctx, conn, "haiku", `{"provider":"anthropic","model":"haiku"}`)
	insertPromptConfig(t, ctx, conn, "pro", `{"model":"pro"}`)

	migs, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	migs = throughNamedMigration(migs, "backfill_provider_and_model_aliases")
	if err := appkitdb.Migrate(ctx, conn, migs); err != nil {
		t.Fatalf("migrate backfill: %v", err)
	}

	assertConfigField(t, ctx, conn, "sonnet", "provider", "anthropic")
	assertConfigField(t, ctx, conn, "sonnet", "model", "claude-sonnet-4-6")
	assertConfigField(t, ctx, conn, "haiku", "provider", "anthropic")
	assertConfigField(t, ctx, conn, "haiku", "model", "claude-haiku-4-5")
	assertConfigField(t, ctx, conn, "pro", "provider", "google")
	assertConfigField(t, ctx, conn, "pro", "model", "gemini-3.1-pro-preview")
	cfg := readPromptConfig(t, ctx, conn, "sonnet")
	if cfg["max_tokens"] != float64(123) {
		t.Fatalf("max_tokens = %#v, want preserved 123", cfg["max_tokens"])
	}
}

func throughNamedMigration(migs []appkitdb.Migration, name string) []appkitdb.Migration {
	for i, m := range migs {
		if strings.Contains(m.Name, name) {
			return migs[:i+1]
		}
	}
	return migs
}

func migrateBeforeProviderModelAliasBackfill(t *testing.T, ctx context.Context, conn *sql.DB) {
	t.Helper()
	migs, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var targetVersion int
	for _, m := range migs {
		if strings.Contains(m.Name, "backfill_provider_and_model_aliases") {
			targetVersion = m.Version
			break
		}
	}
	if targetVersion == 0 {
		t.Fatalf("backfill_provider_and_model_aliases migration not found")
	}
	var pre []appkitdb.Migration
	for _, m := range migs {
		if m.Version < targetVersion {
			pre = append(pre, m)
		}
	}
	if err := appkitdb.Migrate(ctx, conn, pre); err != nil {
		t.Fatalf("migrate before provider/model alias backfill: %v", err)
	}
}

func insertPromptConfig(t *testing.T, ctx context.Context, conn *sql.DB, id, configJSON string) {
	t.Helper()
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO prompts (id, owner_email, user_prompt, config_json, created_at, updated_at)
		 VALUES (?, 'owner@example.com', 'do the thing', ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		id, configJSON,
	); err != nil {
		t.Fatalf("insert prompt %q: %v", id, err)
	}
}

func assertConfigField(t *testing.T, ctx context.Context, conn *sql.DB, id, key string, want any) {
	t.Helper()
	cfg := readPromptConfig(t, ctx, conn, id)
	if got := cfg[key]; got != want {
		t.Fatalf("prompt %s config[%q] = %#v, want %#v", id, key, got, want)
	}
}

func readPromptConfig(t *testing.T, ctx context.Context, conn *sql.DB, id string) map[string]any {
	t.Helper()
	var raw string
	if err := conn.QueryRowContext(ctx, `SELECT config_json FROM prompts WHERE id = ?`, id).Scan(&raw); err != nil {
		t.Fatalf("select prompt %q config: %v", id, err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal prompt %q config %s: %v", id, raw, err)
	}
	return cfg
}
