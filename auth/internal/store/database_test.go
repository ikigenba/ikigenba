package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/auth"
	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
)

func newTestStore(t *testing.T, path string, random io.Reader) (*Store, error) {
	t.Helper()
	d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: auth.Migrations(), Now: sessionTestNow})
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return New(d, random), nil
}

func stripFixtureTokenPrefixes(t *testing.T, path string, tokens []Token, bare bool) {
	t.Helper()
	d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: auth.Migrations(), Now: sessionTestNow})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Write(context.Background(), func(tx *sql.Tx) error {
		for _, token := range tokens {
			if !bare {
				break
			}
			if _, err := tx.ExecContext(context.Background(), `UPDATE tokens SET id = ? WHERE id = ?`, strings.TrimPrefix(token.ID, idcodec.TokenIDPrefix), token.ID); err != nil {
				return err
			}
		}
		for _, stmt := range []string{"DROP INDEX tokens_profile_idx", "DROP TABLE clients", "DROP TABLE auth_codes", "ALTER TABLE tokens DROP COLUMN kind", "ALTER TABLE tokens DROP COLUMN host"} {
			if _, err := tx.ExecContext(context.Background(), stmt); err != nil {
				return err
			}
		}
		statement := "DELETE FROM schema_migrations WHERE version IN (3, 4)"
		if bare {
			statement = "DROP TABLE schema_migrations"
		}
		_, err := tx.ExecContext(context.Background(), statement)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
}

// R-8CEV-D3QG
var _ func(*db.DB, io.Reader) *Store = New

func TestSchemaTablesColumnsAndIndexes(t *testing.T) {
	// R-FAO6-AQQ4
	// R-9BV7-YAIH
	// R-F9G9-WYZF
	st, err := newTestStore(t, filepath.Join(t.TempDir(), "auth.db"), strings.NewReader(strings.Repeat("r", 64)))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.db.Read(context.Background(), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(context.Background(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
		if err != nil {
			return err
		}
		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			names = append(names, name)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if strings.Join(names, ",") != "auth_codes,clients,login_states,schema_migrations,sessions,tokens,users" {
			t.Fatalf("tables: %v", names)
		}
		for _, schema := range []struct{ table, columns string }{{"users", "id,issuer,subject,email,last_google_login"}, {"sessions", "id,user_id,login_at,last_used_at"}, {"login_states", "state,verifier,return_url"}, {"tokens", "id,user_id,name,hash,enabled,created_at,expires_at,last_used_at,kind,host"}, {"clients", "id,name,redirect_uris,created_at,received_token"}, {"auth_codes", "code,client_id,user_id,redirect_uri,challenge,resource,issued_at"}} {
			table, want := schema.table, schema.columns
			var count int
			if err := tx.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Fatalf("%s has %d rows", table, count)
			}
			rows, err := tx.QueryContext(context.Background(), `PRAGMA table_info('`+table+`')`)
			if err != nil {
				return err
			}
			var columns []string
			for rows.Next() {
				var cid, nn, pk int
				var name, typ string
				var def any
				if err := rows.Scan(&cid, &name, &typ, &nn, &def, &pk); err != nil {
					return err
				}
				columns = append(columns, name)
			}
			if err := rows.Err(); err != nil {
				return err
			}
			if err := rows.Close(); err != nil {
				return err
			}
			if strings.Join(columns, ",") != want {
				t.Fatalf("%s columns: %v", table, columns)
			}
		}
		rows, err = tx.QueryContext(context.Background(), `SELECT name, tbl_name FROM sqlite_master WHERE type = 'index' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
		if err != nil {
			return err
		}
		var indexes []string
		for rows.Next() {
			var name, table string
			if err := rows.Scan(&name, &table); err != nil {
				return err
			}
			indexes = append(indexes, name+":"+table)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if strings.Join(indexes, ",") != "sessions_user_id_idx:sessions,tokens_profile_idx:tokens,tokens_user_id_idx:tokens" {
			t.Fatalf("indexes: %v", indexes)
		}
		for _, index := range []string{"sessions_user_id_idx", "tokens_user_id_idx"} {
			rows, err := tx.QueryContext(context.Background(), `PRAGMA index_info('`+index+`')`)
			if err != nil {
				return err
			}
			var columns []string
			for rows.Next() {
				var seq, cid int
				var name string
				if err := rows.Scan(&seq, &cid, &name); err != nil {
					return err
				}
				columns = append(columns, name)
			}
			if err := rows.Err(); err != nil {
				return err
			}
			if err := rows.Close(); err != nil {
				return err
			}
			if strings.Join(columns, ",") != "user_id" {
				t.Fatalf("%s columns: %v", index, columns)
			}
		}
		rows, err = tx.QueryContext(context.Background(), `PRAGMA index_xinfo('tokens_profile_idx')`)
		if err != nil {
			return err
		}
		var keys []string
		for rows.Next() {
			var seq, cid, desc, key int
			var name sql.NullString
			var coll string
			if err := rows.Scan(&seq, &cid, &name, &desc, &coll, &key); err != nil {
				return err
			}
			if key == 1 {
				keys = append(keys, fmt.Sprintf("%d:%s:%d", seq, name.String, desc))
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if strings.Join(keys, ",") != "0:user_id:0,1:kind:0,2:last_used_at:1,3:created_at:1,4:id:0" {
			t.Fatalf("profile index keys: %v", keys)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	user, _, err := st.UpsertUserOnLogin("issuer", "subject", "member@example.com", sessionTestNow())
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := st.CreateToken(user.ID, "token", ExpiryNever, sessionTestNow())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.db.Read(context.Background(), func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM tokens WHERE id = ?`, token.ID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("token rows: %d", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProfileIndexMigrationPreservesEveryDomainRow(t *testing.T) {
	st, _, _, _, _ := clientFixture(t)
	now := tokenTestNow()
	if _, err := st.CreateSession("owner", now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateLoginState("verifier", "/return"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateToken("owner", "personal", ExpiryNever, now); err != nil {
		t.Fatal(err)
	}
	before := domainRows(t, st)
	if err := st.db.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(context.Background(), "DROP INDEX tokens_profile_idx"); err != nil {
			return err
		}
		_, err := tx.ExecContext(context.Background(), "DELETE FROM schema_migrations WHERE version = 4")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Open the same test-owned database through the public migration seam.
	var path string
	if err := st.db.Read(context.Background(), func(tx *sql.Tx) error {
		var seq int
		var name string
		return tx.QueryRowContext(context.Background(), "PRAGMA database_list").Scan(&seq, &name, &path)
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.db.Close(); err != nil {
		t.Fatal(err)
	}
	later, err := newTestStore(t, path, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if after := domainRows(t, later); !reflect.DeepEqual(before, after) {
		t.Fatalf("profile index migration changed rows: %#v; want %#v", after, before)
	}
}

func TestStorePersistsDomainStateAcrossHandles(t *testing.T) {
	// R-9J6M-8WYN
	path := filepath.Join(t.TempDir(), "auth.db")
	st, err := newTestStore(t, path, bytes.NewReader(sequentialStoreBytes(240)))
	if err != nil {
		t.Fatal(err)
	}
	now := sessionTestNow()
	user, _, err := st.UpsertUserOnLogin("issuer", "subject", "member@example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(user.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	state, err := st.CreateLoginState("verifier", "https://example.com/return")
	if err != nil {
		t.Fatal(err)
	}
	token, secret, err := st.CreateToken(user.ID, "deploy", Expiry90d, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.TouchTokenIdentity(secret, "", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	client, err := st.RegisterClient("persistent client", []string{"https://client.test/cb"}, now)
	if err != nil {
		t.Fatal(err)
	}
	code, err := st.CreateAuthCode(client.ID, user.ID, "https://client.test/cb", "challenge", "https://mcp.test", now)
	if err != nil {
		t.Fatal(err)
	}
	clientToken, clientSecret, err := st.CreateClientToken(code, "mcp.test", now)
	if err != nil {
		t.Fatal(err)
	}
	clientIdentity, err := st.LookupTokenIdentity(clientSecret, "MCP.TEST", now)
	if err != nil {
		t.Fatal(err)
	}
	foundClient, err := st.FindToken(user.ID, clientToken.ID)
	if err != nil {
		t.Fatal(err)
	}
	sessionIdentity, err := st.LookupSessionIdentity(session.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	tokenIdentity, err := st.LookupTokenIdentity(secret, "", now)
	if err != nil {
		t.Fatal(err)
	}
	found, err := st.FindToken(user.ID, token.ID)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := st.ListTokens(user.ID, TokenPersonal, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	window, err := st.ListTokens(user.ID, TokenPersonal, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	clients, err := st.ListTokens(user.ID, TokenClient, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindToken(user.ID, "unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := st.db.Close(); err != nil {
		t.Fatal(err)
	}
	later, err := newTestStore(t, path, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	gotSession, err := later.LookupSessionIdentity(session.ID, now)
	if err != nil || gotSession != sessionIdentity {
		t.Fatalf("session: %#v, %v", gotSession, err)
	}
	gotToken, err := later.LookupTokenIdentity(secret, "", now)
	if err != nil || gotToken != tokenIdentity {
		t.Fatalf("token identity: %#v, %v", gotToken, err)
	}
	gotTokens, err := later.ListTokens(user.ID, TokenPersonal, nil, 100)
	if err != nil || !reflect.DeepEqual(gotTokens, tokens) {
		t.Fatalf("tokens: %#v, %v; want %#v", gotTokens, err, tokens)
	}
	gotWindow, err := later.ListTokens(user.ID, TokenPersonal, nil, 1)
	if err != nil || !reflect.DeepEqual(gotWindow, window) {
		t.Fatalf("persisted window: %#v,%v; want %#v", gotWindow, err, window)
	}
	gotClients, err := later.ListTokens(user.ID, TokenClient, nil, 100)
	if err != nil || !reflect.DeepEqual(gotClients, clients) {
		t.Fatalf("persisted client list: %#v,%v; want %#v", gotClients, err, clients)
	}
	if _, err := later.FindToken(user.ID, "unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	gotClientIdentity, err := later.LookupTokenIdentity(clientSecret, "MCP.TEST", now)
	if err != nil || gotClientIdentity != clientIdentity {
		t.Fatalf("client identity: %#v,%v; want %#v", gotClientIdentity, err, clientIdentity)
	}
	gotClient, err := later.FindToken(user.ID, clientToken.ID)
	if err != nil || !reflect.DeepEqual(gotClient, foundClient) {
		t.Fatalf("client token: %#v,%v; want %#v", gotClient, err, foundClient)
	}
	gotFound, err := later.FindToken(user.ID, token.ID)
	if err != nil || !reflect.DeepEqual(gotFound, found) {
		t.Fatalf("found token: %#v,%v; want %#v", gotFound, err, found)
	}
	gotState, err := later.ConsumeLoginState(state.State)
	if err != nil || gotState != state {
		t.Fatalf("state: %#v, %v; want %#v", gotState, err, state)
	}
}

func TestEveryOperationFailsAndRecoversOnSameHandle(t *testing.T) {
	// R-9KEI-MOPC
	// R-8CEV-D3QG
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "auth.db"), Migrations: auth.Migrations(), Now: sessionTestNow})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	st := New(d, bytes.NewReader(sequentialStoreBytes(256)))
	now := sessionTestNow()
	// Initial domain records make recovery observable for every lookup and mutation.
	user, _, err := st.UpsertUserOnLogin("issuer", "subject", "member@example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(user.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	state, err := st.CreateLoginState("verifier", "/return")
	if err != nil {
		t.Fatal(err)
	}
	token, secret, err := st.CreateToken(user.ID, "token", ExpiryNever, now)
	if err != nil {
		t.Fatal(err)
	}
	client, err := st.RegisterClient("Agent", []string{"https://client/cb"}, now)
	if err != nil {
		t.Fatal(err)
	}
	code, err := st.CreateAuthCode(client.ID, user.ID, "redirect", "challenge", "resource", now)
	if err != nil {
		t.Fatal(err)
	}
	clientToken, _, err := st.CreateClientToken(code, "host", now)
	if err != nil {
		t.Fatal(err)
	}
	calls := []struct {
		name string
		call func() error
	}{
		{"upsert", func() error {
			_, _, err := st.UpsertUserOnLogin("issuer", "subject", "refreshed@example.com", now)
			return err
		}},
		{"create session", func() error { _, err := st.CreateSession(user.ID, now); return err }},
		{"lookup session", func() error { _, err := st.LookupSessionIdentity(session.ID, now); return err }},
		{"touch session", func() error { _, err := st.TouchSession(session.ID, now); return err }},
		{"delete session", func() error { return st.DeleteSession(session.ID) }},
		{"create state", func() error { _, err := st.CreateLoginState("new verifier", "/new"); return err }},
		{"consume state", func() error { _, err := st.ConsumeLoginState(state.State); return err }},
		{"create token", func() error { _, _, err := st.CreateToken(user.ID, "new token", ExpiryNever, now); return err }},
		{"list tokens", func() error { _, err := st.ListTokens(user.ID, TokenPersonal, nil, 100); return err }},
		{"set enabled", func() error { return st.SetTokenEnabled(user.ID, token.ID, true) }},
		{"delete token", func() error { return st.DeleteToken(user.ID, token.ID) }},
		{"lookup token", func() error { _, err := st.LookupTokenIdentity(secret, "", now); return err }},
		{"touch token", func() error { _, err := st.TouchTokenIdentity(secret, "", now); return err }},
		{"register client", func() error { _, err := st.RegisterClient("new", nil, now); return err }},
		{"lookup client", func() error { _, err := st.LookupClient(client.ID, now); return err }},
		{"create code", func() error {
			_, err := st.CreateAuthCode(client.ID, user.ID, "redirect", "challenge", "resource", now)
			return err
		}},
		{"consume code", func() error { _, err := st.ConsumeAuthCode(code.Code, now); return err }},
		{"create client token", func() error { _, _, err := st.CreateClientToken(code, "host", now); return err }},
		{"revoke token", func() error { return st.RevokeToken(user.ID, clientToken.ID) }},
		{"find token", func() error { _, err := st.FindToken(user.ID, token.ID); return err }},
		{"list empty window", func() error { _, err := st.ListTokens(user.ID, TokenClient, nil, 0); return err }},
	}
	d.SetFailing(true)
	for _, c := range calls {
		if err := c.call(); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("%s failure: %v", c.name, err)
		}
	}
	d.SetFailing(false)
	// Fresh entropy after the original records; failure must leave every record intact.
	st.rand = bytes.NewReader(tokenSequentialBytes(4096)[256:])
	for _, c := range calls {
		if c.name == "delete session" || c.name == "delete token" {
			continue
		}
		if err := c.call(); err != nil {
			t.Errorf("%s recovery: %v", c.name, err)
		}
	}
	for _, c := range calls {
		if c.name == "delete session" || c.name == "delete token" {
			if err := c.call(); err != nil {
				t.Errorf("%s recovery: %v", c.name, err)
			}
		}
	}
	if _, err := st.LookupSessionIdentity(session.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session deletion: %v", err)
	}
	if _, err := st.ConsumeLoginState(state.State); !errors.Is(err, ErrNotFound) {
		t.Fatalf("consumed state: %v", err)
	}
	if _, err := st.LookupTokenIdentity(secret, "", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token deletion: %v", err)
	}
}

func domainRows(t *testing.T, st *Store) map[string][][]any {
	t.Helper()
	result := map[string][][]any{}
	if err := st.db.Read(context.Background(), func(tx *sql.Tx) error {
		for _, fixture := range []struct{ table, query string }{
			{"users", `SELECT * FROM users ORDER BY id`},
			{"sessions", `SELECT * FROM sessions ORDER BY id`},
			{"login_states", `SELECT * FROM login_states ORDER BY state`},
			{"tokens", `SELECT * FROM tokens ORDER BY id`},
			{"clients", `SELECT * FROM clients ORDER BY id`},
			{"auth_codes", `SELECT * FROM auth_codes ORDER BY code`},
		} {
			table := fixture.table
			rows, err := tx.QueryContext(context.Background(), fixture.query)
			if err != nil {
				return err
			}
			columns, err := rows.Columns()
			if err != nil {
				return err
			}
			for rows.Next() {
				row := make([]any, len(columns))
				dest := make([]any, len(columns))
				for i := range row {
					dest[i] = &row[i]
				}
				if err := rows.Scan(dest...); err != nil {
					return err
				}
				result[table] = append(result[table], row)
			}
			if err := rows.Err(); err != nil {
				return err
			}
			if err := rows.Close(); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}
