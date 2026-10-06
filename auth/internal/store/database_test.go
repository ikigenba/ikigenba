package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
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

func stripFixtureTokenPrefixes(t *testing.T, path string, tokens []Token) {
	t.Helper()
	d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: auth.Migrations(), Now: sessionTestNow})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Write(context.Background(), func(tx *sql.Tx) error {
		for _, token := range tokens {
			if _, err := tx.ExecContext(context.Background(), `UPDATE tokens SET id = ? WHERE id = ?`, strings.TrimPrefix(token.ID, idcodec.TokenIDPrefix), token.ID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(context.Background(), `DROP TABLE schema_migrations`)
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
	// R-8G2K-IEYJ
	// R-8HAG-W6P8
	// R-78DO-JF6Z
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
		if strings.Join(names, ",") != "login_states,schema_migrations,sessions,tokens,users" {
			t.Fatalf("tables: %v", names)
		}
		for _, schema := range []struct{ table, columns string }{{"users", "id,issuer,subject,email,last_google_login"}, {"sessions", "id,user_id,login_at,last_used_at"}, {"login_states", "state,verifier,return_url"}, {"tokens", "id,user_id,name,hash,enabled,created_at,expires_at,last_used_at"}} {
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
		if strings.Join(indexes, ",") != "sessions_user_id_idx:sessions,tokens_user_id_idx:tokens" {
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

func TestStorePersistsDomainStateAcrossHandles(t *testing.T) {
	// R-8IID-9YFX
	path := filepath.Join(t.TempDir(), "auth.db")
	st, err := newTestStore(t, path, strings.NewReader(strings.Repeat("a", 16)+strings.Repeat("b", 16)+strings.Repeat("c", 16)+strings.Repeat("d", 48)))
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
	_, secret, err := st.CreateToken(user.ID, "deploy", Expiry90d, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.TouchTokenIdentity(secret, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	sessionIdentity, err := st.LookupSessionIdentity(session.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	tokenIdentity, err := st.LookupTokenIdentity(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := st.ListTokens(user.ID)
	if err != nil {
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
	gotToken, err := later.LookupTokenIdentity(secret, now)
	if err != nil || gotToken != tokenIdentity {
		t.Fatalf("token identity: %#v, %v", gotToken, err)
	}
	gotTokens, err := later.ListTokens(user.ID)
	if err != nil || !reflect.DeepEqual(gotTokens, tokens) {
		t.Fatalf("tokens: %#v, %v; want %#v", gotTokens, err, tokens)
	}
	gotState, err := later.ConsumeLoginState(state.State)
	if err != nil || gotState != state {
		t.Fatalf("state: %#v, %v; want %#v", gotState, err, state)
	}
}

func TestEveryOperationFailsAndRecoversOnSameHandle(t *testing.T) {
	// R-8JQ9-NQ6M
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
	st := New(d, bytes.NewReader(sequentialStoreBytes(96)))
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
		{"list tokens", func() error { _, err := st.ListTokens(user.ID); return err }},
		{"set enabled", func() error { return st.SetTokenEnabled(user.ID, token.ID, true) }},
		{"delete token", func() error { return st.DeleteToken(user.ID, token.ID) }},
		{"lookup token", func() error { _, err := st.LookupTokenIdentity(secret, now); return err }},
		{"touch token", func() error { _, err := st.TouchTokenIdentity(secret, now); return err }},
	}
	d.SetFailing(true)
	for _, c := range calls {
		if err := c.call(); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("%s failure: %v", c.name, err)
		}
	}
	d.SetFailing(false)
	// Fresh entropy after the original records; failure must leave every record intact.
	st.rand = bytes.NewReader(sequentialStoreBytes(176)[96:])
	for _, i := range []int{0, 1, 2, 3, 5, 6, 7, 8, 9, 11, 12, 4, 10} {
		c := calls[i]
		if err := c.call(); err != nil {
			t.Errorf("%s recovery: %v", c.name, err)
		}
	}
	if _, err := st.LookupSessionIdentity(session.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session deletion: %v", err)
	}
	if _, err := st.ConsumeLoginState(state.State); !errors.Is(err, ErrNotFound) {
		t.Fatalf("consumed state: %v", err)
	}
	if _, err := st.LookupTokenIdentity(secret, now); !errors.Is(err, ErrNotFound) {
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
