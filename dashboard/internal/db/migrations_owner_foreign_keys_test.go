package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appkitdb "appkit/db"
)

const ownerForeignKeysMigrationName = "add_owner_id_foreign_keys"

func openBeforeOwnerForeignKeysMigration(t *testing.T) (*sql.DB, []appkitdb.Migration) {
	t.Helper()
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}
	migrationAt := -1
	for i, migration := range migrations {
		if strings.Contains(migration.Name, ownerForeignKeysMigrationName) {
			migrationAt = i
			break
		}
	}
	if migrationAt < 0 {
		t.Fatalf("migration %q not found", ownerForeignKeysMigrationName)
	}

	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := appkitdb.Migrate(context.Background(), database, migrations[:migrationAt]); err != nil {
		t.Fatalf("migrate before owner foreign keys: %v", err)
	}
	return database, migrations
}

func seedOwnerForeignKeyRows(t *testing.T, database *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO identities (id, iss, sub, email, name, picture, created_at, updated_at) VALUES ('identity-1', 'issuer', 'subject', 'owner@example.com', 'Owner', 'https://example.com/picture', '2026-01-01T00:00:00.123Z', '2026-01-02T00:00:00.456Z')`,
		`INSERT INTO web_sessions (id, owner_email, cookie_hash, issued_at, expires_at, last_seen_at, revoked_at, owner_id) VALUES ('session-1', 'owner@example.com', 'cookie-hash', '2026-01-01T00:00:00.123Z', '2027-01-01T00:00:00.456Z', '2026-01-02T00:00:00.789Z', '2026-02-01T00:00:00Z', 'identity-1')`,
		`INSERT INTO oauth_chains (id, public_id, client_id, owner_email, resource, created_at, revoked_at, owner_id) VALUES ('chain-1', 'chain-public-1', 'client-1', 'owner@example.com', 'https://example.com/mcp?x=1&y=2', '2026-01-01T00:00:00.123Z', '2026-02-01T00:00:00Z', 'identity-1')`,
		`INSERT INTO oauth_tokens (id, chain_id, kind, token_hash, issued_at, expires_at) VALUES ('token-1', 'chain-1', 'access', 'access-hash', '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z')`,
		`INSERT INTO oauth_authcodes (id, code_hash, client_id, owner_email, code_challenge, code_challenge_method, redirect_uri, resource, original_state, issued_at, expires_at, used_at, chain_id, owner_id) VALUES ('code-1', 'code-hash', 'client-1', 'owner@example.com', 'challenge+/=', 'S256', 'https://example.com/callback?a=1&b=2', 'https://example.com/mcp', 'state-value', '2026-01-01T00:00:00.123Z', '2027-01-01T00:00:00.456Z', '2026-01-03T00:00:00.789Z', 'chain-1', 'identity-1')`,
		`INSERT INTO personal_tokens (id, public_id, owner_email, label, token_hash, created_at, last_used_at, expires_at, revoked_at, owner_id) VALUES ('pat-1', 'pat-public-1', 'owner@example.com', 'deploy token', 'pat-hash', '2026-01-01T00:00:00.123Z', '2026-01-02T00:00:00.456Z', '2027-01-01T00:00:00.789Z', '2026-02-01T00:00:00Z', 'identity-1')`,
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("seed owner foreign-key rows with %q: %v", statement, err)
		}
	}
}

func queryTextRows(t *testing.T, database *sql.DB, query string, columnCount int) [][]any {
	t.Helper()
	rows, err := database.QueryContext(context.Background(), query)
	if err != nil {
		t.Fatalf("query snapshot: %v", err)
	}
	defer rows.Close()

	var result [][]any
	for rows.Next() {
		values := make([]any, columnCount)
		destinations := make([]any, columnCount)
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			t.Fatalf("scan snapshot: %v", err)
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate snapshot: %v", err)
	}
	return result
}

// R-HYL8-V30T
func TestOwnerForeignKeysMigrationPreservesEveryCarrierRow(t *testing.T) {
	database, migrations := openBeforeOwnerForeignKeysMigration(t)
	seedOwnerForeignKeyRows(t, database)

	queries := map[string]struct {
		query   string
		columns int
	}{
		"web_sessions":    {`SELECT id, owner_email, cookie_hash, issued_at, expires_at, last_seen_at, revoked_at, owner_id FROM web_sessions ORDER BY id`, 8},
		"oauth_authcodes": {`SELECT id, code_hash, client_id, owner_email, code_challenge, code_challenge_method, redirect_uri, resource, original_state, issued_at, expires_at, used_at, chain_id, owner_id FROM oauth_authcodes ORDER BY id`, 14},
		"oauth_chains":    {`SELECT id, public_id, client_id, owner_email, resource, created_at, revoked_at, owner_id FROM oauth_chains ORDER BY id`, 8},
		"personal_tokens": {`SELECT id, public_id, owner_email, label, token_hash, created_at, last_used_at, expires_at, revoked_at, owner_id FROM personal_tokens ORDER BY id`, 10},
	}
	before := make(map[string][][]any, len(queries))
	for table, snapshot := range queries {
		before[table] = queryTextRows(t, database, snapshot.query, snapshot.columns)
	}

	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatalf("apply owner foreign-key migration: %v", err)
	}

	for table, snapshot := range queries {
		after := queryTextRows(t, database, snapshot.query, snapshot.columns)
		if !reflect.DeepEqual(after, before[table]) {
			t.Errorf("%s rows changed during rebuild:\nbefore: %#v\nafter:  %#v", table, before[table], after)
		}
	}
	if got := countRows(t, database, `SELECT COUNT(*) FROM oauth_tokens WHERE id = 'token-1' AND chain_id = 'chain-1'`); got != 1 {
		t.Errorf("dependent oauth token count = %d, want 1", got)
	}
}

func requireForeignKeyError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("operation with a dangling foreign key succeeded")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "foreign key constraint failed") {
		t.Fatalf("error = %q, want a foreign-key constraint error", err)
	}
}

// R-HZT5-8URI
func TestOwnerForeignKeysRejectDanglingCarrierInserts(t *testing.T) {
	database, migrations := openBeforeOwnerForeignKeysMigration(t)
	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatalf("apply owner foreign-key migration: %v", err)
	}

	tests := []struct {
		name  string
		query string
	}{
		{"web_sessions", `INSERT INTO web_sessions (id, owner_email, cookie_hash, issued_at, expires_at, last_seen_at, owner_id) VALUES ('s', 'owner@example.com', 'hash-s', '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 'missing-identity')`},
		{"oauth_authcodes", `INSERT INTO oauth_authcodes (id, code_hash, client_id, owner_email, code_challenge, code_challenge_method, redirect_uri, resource, original_state, issued_at, expires_at, owner_id) VALUES ('a', 'hash-a', 'client', 'owner@example.com', 'challenge', 'S256', 'https://example.com/callback', 'https://example.com/mcp', 'state', '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z', 'missing-identity')`},
		{"oauth_chains", `INSERT INTO oauth_chains (id, public_id, client_id, owner_email, resource, created_at, owner_id) VALUES ('c', 'public-c', 'client', 'owner@example.com', 'https://example.com/mcp', '2026-01-01T00:00:00Z', 'missing-identity')`},
		{"personal_tokens", `INSERT INTO personal_tokens (id, public_id, owner_email, label, token_hash, created_at, owner_id) VALUES ('p', 'public-p', 'owner@example.com', 'label', 'hash-p', '2026-01-01T00:00:00Z', 'missing-identity')`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := database.ExecContext(context.Background(), test.query)
			requireForeignKeyError(t, err)
		})
	}
}

// R-I111-MMI7
func TestOwnerForeignKeysPreventDeletingOwnersWithArtifacts(t *testing.T) {
	database, migrations := openBeforeOwnerForeignKeysMigration(t)
	seedOwnerForeignKeyRows(t, database)
	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatalf("apply owner foreign-key migration: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO identities (id, iss, sub, email, created_at, updated_at) VALUES ('identity-free', 'issuer', 'free-subject', 'free@example.com', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed identity without artifacts: %v", err)
	}

	_, err := database.Exec(`DELETE FROM identities WHERE id = 'identity-1'`)
	requireForeignKeyError(t, err)
	if got := countRows(t, database, `SELECT COUNT(*) FROM identities WHERE id = 'identity-1'`); got != 1 {
		t.Errorf("dependent identity count after rejected delete = %d, want 1", got)
	}
	for _, table := range []string{"web_sessions", "oauth_authcodes", "oauth_chains", "personal_tokens"} {
		if got := countRows(t, database, `SELECT COUNT(*) FROM `+table+` WHERE owner_id = 'identity-1'`); got != 1 {
			t.Errorf("%s artifact count after rejected delete = %d, want 1", table, got)
		}
	}

	if _, err := database.Exec(`DELETE FROM identities WHERE id = 'identity-free'`); err != nil {
		t.Fatalf("delete identity without artifacts: %v", err)
	}
	if got := countRows(t, database, `SELECT COUNT(*) FROM identities WHERE id = 'identity-free'`); got != 0 {
		t.Errorf("independent identity count after delete = %d, want 0", got)
	}
}
