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
	// R-59FH-F9LG: a file that exists but is not this store's database returns
	// a non-nil error and no usable store.
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

func TestExpiryIsDefinedStringTypeWithTypedConstants(t *testing.T) {
	// R-4EXZ-V023
	const (
		_ = ExpiryNever
		_ = Expiry30d
		_ = Expiry90d
		_ = Expiry365d
	)

	expiryType := reflect.TypeFor[Expiry]()
	if expiryType.Kind() != reflect.String || expiryType.Name() != "Expiry" || expiryType.PkgPath() != "github.com/ikigenba/ikigenba/auth/internal/store" {
		t.Fatalf("Expiry type = %s kind %s pkg %q", expiryType, expiryType.Kind(), expiryType.PkgPath())
	}

	constants := []struct {
		name string
		typ  reflect.Type
		got  string
		want string
	}{
		{name: "ExpiryNever", typ: reflect.TypeOf(ExpiryNever), got: string(ExpiryNever), want: "never"},
		{name: "Expiry30d", typ: reflect.TypeOf(Expiry30d), got: string(Expiry30d), want: "30d"},
		{name: "Expiry90d", typ: reflect.TypeOf(Expiry90d), got: string(Expiry90d), want: "90d"},
		{name: "Expiry365d", typ: reflect.TypeOf(Expiry365d), got: string(Expiry365d), want: "365d"},
	}
	seen := make(map[string]string, len(constants))
	for _, constant := range constants {
		if constant.typ != expiryType || constant.got != constant.want {
			t.Errorf("%s = %q type %s, want Expiry %q", constant.name, constant.got, constant.typ, constant.want)
		}
		if previous, ok := seen[constant.got]; ok {
			t.Errorf("%s duplicates %s value %q", constant.name, previous, constant.got)
		}
		seen[constant.got] = constant.name
	}
}

func TestOpenAbsentFileSucceedsAndExistingNonDatabaseDoesNot(t *testing.T) {
	// R-59FH-F9LG
	dir := t.TempDir()
	absent := filepath.Join(dir, "missing.db")
	if _, err := os.Stat(absent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent path Stat() error = %v, want not exist", err)
	}

	st, err := Open(absent, bytes.NewReader(nil))
	if err != nil || st == nil {
		t.Fatalf("Open(absent) = (%v, %v); a missing file must not be the unopenable-database failure", st, err)
	}
	insertFoundationRows(t, st)
	var users int
	if err := st.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		t.Fatalf("count users in absent-file store: %v", err)
	}
	if users != 1 {
		t.Fatalf("absent-file store user count = %d, want 1", users)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close(absent) error = %v", err)
	}

	bad := filepath.Join(dir, "bad.db")
	payload := []byte("this is not sqlite")
	if err := os.WriteFile(bad, payload, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		st, err = Open(bad, bytes.NewReader(nil))
		if err == nil || st != nil {
			if st != nil {
				_ = st.Close()
			}
			t.Fatalf("Open(existing non-database) attempt %d = (%v, %v); want error and no store", attempt, st, err)
		}
	}
	got, err := os.ReadFile(filepath.Clean(bad))
	if err != nil {
		t.Fatalf("ReadFile(bad) error = %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("unopenable database bytes = %q, want unchanged %q", got, payload)
	}
}

func TestErrNotFoundForMissingAndOtherOwnerRows(t *testing.T) {
	// R-4HDS-MJJH
	st := openTokenTestStore(t, bytes.NewReader(nil))
	now := tokenTestNow()
	insertTokenUser(t, st, "owner", "owner@example.com", now)
	insertTokenUser(t, st, "other", "other@example.com", now)
	insertToken(t, st, Token{
		ID:        "owned",
		UserID:    "owner",
		Name:      "owned",
		Hash:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Enabled:   true,
		CreatedAt: now,
	})
	insertSessionFixture(t, st, "kept-session", "owner", now, now)
	if _, err := st.db.ExecContext(
		context.Background(),
		`INSERT INTO login_states (state, verifier, return_url) VALUES (?, ?, ?)`,
		"kept-state",
		"verifier",
		"/return",
	); err != nil {
		t.Fatalf("insert login state: %v", err)
	}
	beforeTokens := allTokenStates(t, st)

	if got, err := st.LookupSessionIdentity("missing-session", now); err == nil || !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Errorf("LookupSessionIdentity(missing) = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	if got, err := st.TouchSession("missing-session", now); err == nil || !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Errorf("TouchSession(missing) = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	if got, err := st.ConsumeLoginState("missing-state"); err == nil || !errors.Is(err, ErrNotFound) || got != (LoginState{}) {
		t.Errorf("ConsumeLoginState(missing) = %#v, %v; want zero login state and ErrNotFound", got, err)
	}
	if got, err := st.LookupTokenIdentity("missing-secret", now); err == nil || !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Errorf("LookupTokenIdentity(missing) = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	if got, err := st.TouchTokenIdentity("missing-secret", now); err == nil || !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Errorf("TouchTokenIdentity(missing) = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	if err := st.SetTokenEnabled("owner", "missing-token", false); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("SetTokenEnabled(missing) error = %v, want ErrNotFound", err)
	}
	if err := st.SetTokenEnabled("other", "owned", false); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("SetTokenEnabled(other owner) error = %v, want ErrNotFound", err)
	}
	if err := st.DeleteToken("owner", "missing-token"); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteToken(missing) error = %v, want ErrNotFound", err)
	}
	if err := st.DeleteToken("other", "owned"); err == nil || !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteToken(other owner) error = %v, want ErrNotFound", err)
	}

	if after := allTokenStates(t, st); !reflect.DeepEqual(after, beforeTokens) {
		t.Errorf("missing and other-owner calls changed tokens: before %#v, after %#v", beforeTokens, after)
	}
	assertStoredSession(t, st, "kept-session", "owner", now.UnixNano(), now.UnixNano())
	var verifier, returnURL string
	if err := st.db.QueryRowContext(
		context.Background(),
		`SELECT verifier, return_url FROM login_states WHERE state = ?`,
		"kept-state",
	).Scan(&verifier, &returnURL); err != nil || verifier != "verifier" || returnURL != "/return" {
		t.Errorf("kept login state = (%q, %q, %v), want verifier and /return", verifier, returnURL, err)
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
