package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	appkitdb "appkit/db"
)

const purgeMigrationName = "purge_auth_and_enforce_owner_id"

func openBeforePurgeMigration(t *testing.T) (*sql.DB, []appkitdb.Migration) {
	t.Helper()
	ctx := context.Background()
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}
	purgeAt := -1
	for i, migration := range migrations {
		if strings.Contains(migration.Name, purgeMigrationName) {
			purgeAt = i
			break
		}
	}
	if purgeAt < 0 {
		t.Fatalf("migration %q not found", purgeMigrationName)
	}
	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := appkitdb.Migrate(ctx, database, migrations[:purgeAt]); err != nil {
		t.Fatalf("migrate before purge: %v", err)
	}
	return database, migrations
}

func seedLegacyAuthGraph(t *testing.T, database *sql.DB) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		`INSERT INTO identities (id, iss, sub, email, created_at, updated_at) VALUES ('identity-1', 'issuer', 'subject', 'owner@example.com', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO web_sessions (id, owner_email, cookie_hash, issued_at, expires_at, last_seen_at, owner_id) VALUES ('session-1', 'owner@example.com', 'cookie-hash', '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 'identity-1')`,
		`INSERT INTO dcr_clients (id, client_id, client_name, redirect_uris, registered_at) VALUES ('dcr-1', 'client-1', 'Client', '["https://example.com/callback"]', '2026-01-01T00:00:00Z')`,
		`INSERT INTO oauth_chains (id, public_id, client_id, owner_email, resource, created_at, owner_id) VALUES ('chain-1', 'chain-public-1', 'client-1', 'owner@example.com', 'https://example.com/mcp', '2026-01-01T00:00:00Z', NULL)`,
		`INSERT INTO oauth_tokens (id, chain_id, kind, token_hash, issued_at, expires_at) VALUES ('token-1', 'chain-1', 'access', 'access-hash', '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z')`,
		`INSERT INTO oauth_authcodes (id, code_hash, client_id, owner_email, code_challenge, code_challenge_method, redirect_uri, resource, original_state, issued_at, expires_at, chain_id, owner_id) VALUES ('code-1', 'code-hash', 'client-1', 'owner@example.com', 'challenge', 'S256', 'https://example.com/callback', 'https://example.com/mcp', 'state', '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z', 'chain-1', 'identity-1')`,
		`INSERT INTO oauth_state (id, binding_cookie_hash, created_at, expires_at) VALUES ('state-1', 'binding-hash', '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z')`,
		`INSERT INTO personal_tokens (id, public_id, owner_email, label, token_hash, created_at, owner_id) VALUES ('pat-1', 'pat-public-1', 'owner@example.com', 'legacy', 'pat-hash', '2026-01-01T00:00:00Z', NULL)`,
		`INSERT INTO audit_log (id, event_type, occurred_at, owner_email, chain_id) VALUES ('audit-1', 'legacy.event', '2026-01-01T00:00:00Z', 'owner@example.com', 'chain-1')`,
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed legacy auth graph with %q: %v", statement, err)
		}
	}
}

// R-6QJD-1MUY
func TestPurgeMigrationClearsAuthGraphAndPreservesAuditLog(t *testing.T) {
	database, migrations := openBeforePurgeMigration(t)
	seedLegacyAuthGraph(t, database)

	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatalf("apply purge migration: %v", err)
	}

	for _, table := range []string{
		"identities", "web_sessions", "oauth_authcodes", "oauth_chains",
		"oauth_tokens", "oauth_state", "personal_tokens", "dcr_clients",
	} {
		if got := countRows(t, database, `SELECT COUNT(*) FROM `+table); got != 0 {
			t.Errorf("%s has %d rows after purge, want 0", table, got)
		}
	}
	if got := countRows(t, database, `SELECT COUNT(*) FROM audit_log WHERE id = 'audit-1'`); got != 1 {
		t.Errorf("preserved audit row count = %d, want 1", got)
	}
}

// R-6RR9-FELN
func TestPurgeMigrationMakesEveryOwnerIDCarrierNotNull(t *testing.T) {
	database, migrations := openBeforePurgeMigration(t)
	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatalf("apply purge migration: %v", err)
	}

	tests := []struct {
		table string
		query string
	}{
		{"web_sessions", `INSERT INTO web_sessions (id, owner_email, cookie_hash, issued_at, expires_at, last_seen_at, owner_id) VALUES ('s', 'owner@example.com', 'hash', '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z', '2026-01-01T00:00:00Z', NULL)`},
		{"oauth_authcodes", `INSERT INTO oauth_authcodes (id, code_hash, client_id, owner_email, code_challenge, code_challenge_method, redirect_uri, resource, original_state, issued_at, expires_at, owner_id) VALUES ('c', 'hash', 'client', 'owner@example.com', 'challenge', 'S256', 'https://example.com/callback', 'https://example.com/mcp', 'state', '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z', NULL)`},
		{"oauth_chains", `INSERT INTO oauth_chains (id, public_id, client_id, owner_email, resource, created_at, owner_id) VALUES ('c', 'public', 'client', 'owner@example.com', 'https://example.com/mcp', '2026-01-01T00:00:00Z', NULL)`},
		{"personal_tokens", `INSERT INTO personal_tokens (id, public_id, owner_email, label, token_hash, created_at, owner_id) VALUES ('p', 'public', 'owner@example.com', 'label', 'hash', '2026-01-01T00:00:00Z', NULL)`},
	}
	for _, test := range tests {
		t.Run(test.table, func(t *testing.T) {
			_, err := database.ExecContext(context.Background(), test.query)
			if err == nil {
				t.Fatal("INSERT with NULL owner_id succeeded")
			}
			want := "NOT NULL constraint failed: " + test.table + ".owner_id"
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("INSERT error = %q, want it to contain %q", err, want)
			}
		})
	}
}
