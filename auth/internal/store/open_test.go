package store

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var (
	_ func(string, io.Reader) (*Store, error) = Open
	_ func(*Store) error                      = (*Store).Close
)

func TestFoundationContract(t *testing.T) {
	// R-47ML-KDLX
	assertStructFields(t, User{}, []fieldSpec{
		{"ID", reflect.TypeFor[string]()},
		{"Issuer", reflect.TypeFor[string]()},
		{"Subject", reflect.TypeFor[string]()},
		{"Email", reflect.TypeFor[string]()},
		{"LastGoogleLogin", reflect.TypeFor[time.Time]()},
	})

	// R-48UH-Y5CM
	assertStructFields(t, Session{}, []fieldSpec{
		{"ID", reflect.TypeFor[string]()},
		{"UserID", reflect.TypeFor[string]()},
		{"LoginAt", reflect.TypeFor[time.Time]()},
		{"LastUsedAt", reflect.TypeFor[time.Time]()},
	})

	// R-4A2E-BX3B
	assertStructFields(t, LoginState{}, []fieldSpec{
		{"State", reflect.TypeFor[string]()},
		{"Verifier", reflect.TypeFor[string]()},
		{"ReturnURL", reflect.TypeFor[string]()},
	})

	// R-4CI7-3GKP
	assertStructFields(t, Token{}, []fieldSpec{
		{"ID", reflect.TypeFor[string]()},
		{"UserID", reflect.TypeFor[string]()},
		{"Name", reflect.TypeFor[string]()},
		{"Hash", reflect.TypeFor[string]()},
		{"Enabled", reflect.TypeFor[bool]()},
		{"CreatedAt", reflect.TypeFor[time.Time]()},
		{"ExpiresAt", reflect.TypeFor[*time.Time]()},
		{"LastUsedAt", reflect.TypeFor[*time.Time]()},
	})

	// R-4DQ3-H8BE
	assertStructFields(t, Identity{}, []fieldSpec{
		{"UserID", reflect.TypeFor[string]()},
		{"Email", reflect.TypeFor[string]()},
	})

	if ExpiryNever != "never" || Expiry30d != "30d" || Expiry90d != "90d" || Expiry365d != "365d" {
		t.Fatalf("expiry constants = %q, %q, %q, %q", ExpiryNever, Expiry30d, Expiry90d, Expiry365d)
	}

	// R-G4DV-A8BP
	if SessionIdle != 15*time.Minute {
		t.Fatalf("SessionIdle = %v", SessionIdle)
	}
	// R-G5LR-O02E
	if SessionMax != 18*time.Hour {
		t.Fatalf("SessionMax = %v", SessionMax)
	}
	// R-G81K-FJJS
	if TokenLoginWindow != 30*24*time.Hour {
		t.Fatalf("TokenLoginWindow = %v", TokenLoginWindow)
	}

	if ErrNotFound == nil || !errors.Is(ErrNotFound, ErrNotFound) || !errors.Is(errors.Join(errors.New("context"), ErrNotFound), ErrNotFound) {
		t.Fatalf("ErrNotFound is not a usable errors.Is sentinel")
	}
}

func TestOpenCreatesSchemaAndUsableStore(t *testing.T) {
	// R-4ILP-0BA6
	// R-56ZO-NQ42
	path := filepath.Join(t.TempDir(), "state", "auth.db")
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database exists before Open: %v", err)
	}

	st, err := Open(path, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if st == nil {
		t.Fatal("Open() returned nil store")
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("created database Stat() error = %v", err)
	}
	insertFoundationRows(t, st)
	assertNoPlaintextSecretColumn(t, st)

	if err := st.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := st.db.PingContext(context.Background()); err == nil {
		t.Fatal("database remains usable after Close")
	}
}

func TestOpenExistingDatabasePreservesRows(t *testing.T) {
	// R-587L-1HUR
	path := filepath.Join(t.TempDir(), "auth.db")
	first, err := Open(path, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	insertFoundationRows(t, first)
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	second, err := Open(path, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer func() { _ = second.Close() }()

	for table, want := range map[string]int{"users": 1, "sessions": 1, "login_states": 1, "tokens": 1} {
		var got int
		if err := second.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s row count = %d, want %d", table, got, want)
		}
	}
}

func TestOpenRejectsExistingInvalidDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.db")
	if err := os.WriteFile(path, []byte("this is not sqlite"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	st, err := Open(path, bytes.NewReader(nil))
	if err == nil {
		if st != nil {
			_ = st.Close()
		}
		t.Fatal("Open() error = nil")
	}
	if st != nil {
		_ = st.Close()
		t.Fatalf("Open() returned usable store on error: %#v", st)
	}
}

type fieldSpec struct {
	name string
	typ  reflect.Type
}

func assertStructFields(t *testing.T, value any, want []fieldSpec) {
	t.Helper()
	typ := reflect.TypeOf(value)
	if typ.NumField() != len(want) {
		t.Fatalf("%s has %d fields, want %d", typ, typ.NumField(), len(want))
	}
	for i, field := range want {
		got := typ.Field(i)
		if got.Name != field.name || got.Type != field.typ || !got.IsExported() {
			t.Fatalf("%s field %d = %s %v (exported %v), want %s %v", typ, i, got.Name, got.Type, got.IsExported(), field.name, field.typ)
		}
	}
}

func insertFoundationRows(t *testing.T, st *Store) {
	t.Helper()
	statements := []string{
		`INSERT INTO users (id, issuer, subject, email, last_google_login) VALUES ('user', 'issuer', 'subject', 'member@example.com', 100)`,
		`INSERT INTO sessions (id, user_id, login_at, last_used_at) VALUES ('session', 'user', 100, 200)`,
		`INSERT INTO login_states (state, verifier, return_url) VALUES ('state', 'verifier', '')`,
		`INSERT INTO tokens (id, user_id, name, hash, enabled, created_at, expires_at, last_used_at) VALUES ('token', 'user', 'deploy', '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef', 1, 100, NULL, NULL)`,
	}
	for _, statement := range statements {
		if _, err := st.db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("schema insert error = %v", err)
		}
	}
}

func assertNoPlaintextSecretColumn(t *testing.T, st *Store) {
	t.Helper()
	rows, err := st.db.QueryContext(context.Background(), `PRAGMA table_info(tokens)`)
	if err != nil {
		t.Fatalf("token schema query error = %v", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("token schema scan error = %v", err)
		}
		if name == "secret" || name == "plaintext_secret" {
			t.Fatalf("tokens schema persists plaintext in column %q", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("token schema rows error = %v", err)
	}
}
