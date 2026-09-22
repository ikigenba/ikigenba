package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
)

var (
	_ func(*Store, string, string, Expiry, time.Time) (Token, string, error) = (*Store).CreateToken
	// R-4TKS-G8YF
	_ func(*Store, string) ([]Token, error) = (*Store).ListTokens
	// R-4X8H-LK6I
	_ func(*Store, string, string, bool) error = (*Store).SetTokenEnabled
	// R-4YGD-ZBX7
	_ func(*Store, string, string) error = (*Store).DeleteToken
	// R-4ZOA-D3NW
	_ func(*Store, string, time.Time) (Identity, error) = (*Store).LookupTokenIdentity
	// R-50W6-QVEL
	_ func(*Store, string, time.Time) (Identity, error) = (*Store).TouchTokenIdentity
)

func TestCreateTokenExactValuesExpiryAndHashPersistence(t *testing.T) {
	// R-5MUD-MQR3
	// R-5O2A-0IHS
	// R-G99G-TBAH (persisted representation only)
	now := tokenTestNow()
	tests := []struct {
		name   string
		expiry Expiry
		add    time.Duration
	}{
		{name: "never", expiry: ExpiryNever},
		{name: "30 days", expiry: Expiry30d, add: 30 * 24 * time.Hour},
		{name: "90 days", expiry: Expiry90d, add: 90 * 24 * time.Hour},
		{name: "365 days", expiry: Expiry365d, add: 365 * 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			random := tokenSequentialBytes(48)
			path := filepath.Join(t.TempDir(), "auth.db")
			st := openTokenTestStoreAt(t, path, bytes.NewReader(random))
			insertTokenUser(t, st, "owner", "owner@example.com", now)

			got, secret, err := st.CreateToken("owner", "deploy token", tt.expiry, now)
			if err != nil {
				t.Fatalf("CreateToken() error = %v", err)
			}
			wantSecret := idcodec.SecretPrefix + idcodec.Encode(random[16:])
			want := Token{
				ID:        idcodec.Encode(random[:16]),
				UserID:    "owner",
				Name:      "deploy token",
				Hash:      idcodec.HashSecret(wantSecret),
				Enabled:   true,
				CreatedAt: now,
			}
			if tt.expiry != ExpiryNever {
				expiresAt := now.Add(tt.add)
				want.ExpiresAt = &expiresAt
			}
			if secret != wantSecret || !reflect.DeepEqual(got, want) {
				t.Fatalf("CreateToken() = (%#v, %q), want (%#v, %q)", got, secret, want, wantSecret)
			}

			stored := readToken(t, st, got.ID)
			if !reflect.DeepEqual(stored, want) {
				t.Fatalf("stored token = %#v, want %#v", stored, want)
			}
			if stored.Hash == secret || len(stored.Hash) != 64 || stored.Hash != strings.ToLower(stored.Hash) {
				t.Fatalf("stored hash = %q for secret %q", stored.Hash, secret)
			}

			if err := st.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			reopened, err := Open(path, bytes.NewReader(nil))
			if err != nil {
				t.Fatalf("reopen store: %v", err)
			}
			if reopenedToken := readToken(t, reopened, got.ID); !reflect.DeepEqual(reopenedToken, want) {
				t.Fatalf("reopened token = %#v, want %#v", reopenedToken, want)
			}
			if err := reopened.Close(); err != nil {
				t.Fatalf("close reopened store: %v", err)
			}
			databaseBytes, err := fs.ReadFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
			if err != nil {
				t.Fatalf("ReadFile(database) error = %v", err)
			}
			if bytes.Contains(databaseBytes, []byte(secret)) {
				t.Fatalf("database contains plaintext secret %q", secret)
			}
		})
	}
}

func TestCreateTokenRandomAndDatabaseFailuresPersistNothing(t *testing.T) {
	now := tokenTestNow()
	tests := []struct {
		name   string
		random []byte
	}{
		{name: "id read fails", random: make([]byte, 15)},
		{name: "secret read fails after id", random: make([]byte, 16+31)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openTokenTestStore(t, bytes.NewReader(tt.random))
			insertTokenUser(t, st, "owner", "owner@example.com", now)
			token, secret, err := st.CreateToken("owner", "failure", ExpiryNever, now)
			if err == nil || token != (Token{}) || secret != "" {
				t.Fatalf("CreateToken() = (%#v, %q, %v), want zero values and error", token, secret, err)
			}
			assertTokenCount(t, st, 0)
		})
	}

	t.Run("database rejection after both random values", func(t *testing.T) {
		random := &countingReader{reader: bytes.NewReader(tokenSequentialBytes(48))}
		st := openTokenTestStore(t, random)
		token, secret, err := st.CreateToken("missing-user", "failure", ExpiryNever, now)
		if err == nil || token != (Token{}) || secret != "" {
			t.Fatalf("CreateToken() = (%#v, %q, %v), want zero values and error", token, secret, err)
		}
		if random.read != 48 {
			t.Fatalf("CreateToken() consumed %d random bytes, want 48", random.read)
		}
		assertTokenCount(t, st, 0)
	})
}

func TestListTokensOwnerScopeExactFieldsAndOrder(t *testing.T) {
	// R-5PA6-EA8H
	st := openTokenTestStore(t, bytes.NewReader(nil))
	now := tokenTestNow()
	insertTokenUser(t, st, "owner", "owner@example.com", now)
	insertTokenUser(t, st, "other", "other@example.com", now)
	lastUsed := now.Add(-time.Hour)
	expires := now.Add(24 * time.Hour)
	insertToken(t, st, Token{ID: "older", UserID: "owner", Name: "older name", Hash: idcodec.HashSecret("older-secret"), Enabled: false, CreatedAt: now.Add(-time.Hour), ExpiresAt: &expires, LastUsedAt: &lastUsed})
	insertToken(t, st, Token{ID: "newer-b", UserID: "owner", Name: "newer b", Hash: idcodec.HashSecret("newer-b-secret"), Enabled: true, CreatedAt: now})
	insertToken(t, st, Token{ID: "newer-a", UserID: "owner", Name: "newer a", Hash: idcodec.HashSecret("newer-a-secret"), Enabled: true, CreatedAt: now})
	insertToken(t, st, Token{ID: "foreign", UserID: "other", Name: "foreign", Hash: idcodec.HashSecret("foreign-secret"), Enabled: true, CreatedAt: now.Add(time.Hour)})

	got, err := st.ListTokens("owner")
	if err != nil {
		t.Fatalf("ListTokens() error = %v", err)
	}
	want := []Token{readToken(t, st, "newer-a"), readToken(t, st, "newer-b"), readToken(t, st, "older")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTokens() = %#v, want %#v", got, want)
	}
	empty, err := st.ListTokens("missing-owner")
	if err != nil || len(empty) != 0 || empty == nil {
		t.Fatalf("ListTokens(missing) = %#v, %v; want non-nil empty slice", empty, err)
	}
}

func TestSetTokenEnabledStrictOwnerScope(t *testing.T) {
	// R-5RPZ-5TPV
	st, ownerToken, foreignToken := tokenMutationFixture(t)
	if err := st.SetTokenEnabled("owner", ownerToken, false); err != nil {
		t.Fatalf("disable owned token: %v", err)
	}
	if readToken(t, st, ownerToken).Enabled {
		t.Fatal("owned token remains enabled")
	}
	if err := st.SetTokenEnabled("owner", ownerToken, true); err != nil {
		t.Fatalf("enable owned token: %v", err)
	}
	if !readToken(t, st, ownerToken).Enabled {
		t.Fatal("owned token remains disabled")
	}

	for _, tokenID := range []string{foreignToken, "missing"} {
		before := allTokenStates(t, st)
		if err := st.SetTokenEnabled("owner", tokenID, false); !errors.Is(err, ErrNotFound) {
			t.Errorf("SetTokenEnabled(%q) error = %v, want ErrNotFound", tokenID, err)
		}
		if after := allTokenStates(t, st); !reflect.DeepEqual(after, before) {
			t.Errorf("SetTokenEnabled(%q) changed tokens: before %#v, after %#v", tokenID, before, after)
		}
	}
}

func TestDeleteTokenStrictOwnerScope(t *testing.T) {
	// R-5SXV-JLGK
	st, ownerToken, foreignToken := tokenMutationFixture(t)
	for _, tokenID := range []string{foreignToken, "missing"} {
		before := allTokenStates(t, st)
		if err := st.DeleteToken("owner", tokenID); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteToken(%q) error = %v, want ErrNotFound", tokenID, err)
		}
		if after := allTokenStates(t, st); !reflect.DeepEqual(after, before) {
			t.Errorf("DeleteToken(%q) changed tokens: before %#v, after %#v", tokenID, before, after)
		}
	}
	if err := st.DeleteToken("owner", ownerToken); err != nil {
		t.Fatalf("DeleteToken(owned) error = %v", err)
	}
	if _, err := tokenLastUsed(t, st, ownerToken); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted token query error = %v, want sql.ErrNoRows", err)
	}
	if _, err := st.LookupTokenIdentity("owner-secret", tokenTestNow()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted token still authenticates: %v", err)
	}
}

func TestLookupTokenIdentityBoundariesAndNoMutation(t *testing.T) {
	// R-5U5R-XD79
	// R-5VDO-B4XY
	st := openTokenTestStore(t, bytes.NewReader(nil))
	now := tokenTestNow()
	insertTokenUser(t, st, "fresh", "fresh@example.com", now.Add(-TokenLoginWindow))
	insertTokenUser(t, st, "stale", "stale@example.com", now.Add(-TokenLoginWindow-time.Nanosecond))

	cases := []struct {
		name      string
		secret    string
		userID    string
		enabled   bool
		expiresAt *time.Time
		live      bool
	}{
		{name: "never expires at login boundary", secret: "never", userID: "fresh", enabled: true, live: true},
		{name: "expiry after now", secret: "future", userID: "fresh", enabled: true, expiresAt: tokenTimePointer(now.Add(time.Nanosecond)), live: true},
		{name: "expiry equal now", secret: "equal", userID: "fresh", enabled: true, expiresAt: tokenTimePointer(now)},
		{name: "expired before now", secret: "expired", userID: "fresh", enabled: true, expiresAt: tokenTimePointer(now.Add(-time.Nanosecond))},
		{name: "disabled", secret: "disabled", userID: "fresh"},
		{name: "owner over login boundary", secret: "stale", userID: "stale", enabled: true},
	}
	for index, tt := range cases {
		insertToken(t, st, Token{ID: tt.name, UserID: tt.userID, Name: tt.name, Hash: idcodec.HashSecret(tt.secret), Enabled: tt.enabled, CreatedAt: now.Add(-time.Duration(index+1) * time.Hour), ExpiresAt: tt.expiresAt})
	}

	before := allTokenStates(t, st)
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := st.LookupTokenIdentity(tt.secret, now)
			if tt.live {
				want := Identity{UserID: "fresh", Email: "fresh@example.com"}
				if err != nil || got != want {
					t.Fatalf("LookupTokenIdentity() = %#v, %v; want %#v, nil", got, err, want)
				}
			} else if !errors.Is(err, ErrNotFound) || got != (Identity{}) {
				t.Fatalf("LookupTokenIdentity() = %#v, %v; want zero identity and ErrNotFound", got, err)
			}
		})
	}
	if got, err := st.LookupTokenIdentity("unknown", now); !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Fatalf("unknown LookupTokenIdentity() = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	if after := allTokenStates(t, st); !reflect.DeepEqual(after, before) {
		t.Fatalf("LookupTokenIdentity changed tokens: before %#v, after %#v", before, after)
	}
}

func TestTouchTokenIdentityUpdatesOnlyAuthenticatingToken(t *testing.T) {
	// R-5WLK-OWON
	st := openTokenTestStore(t, bytes.NewReader(nil))
	now := tokenTestNow()
	insertTokenUser(t, st, "fresh", "fresh@example.com", now.Add(-TokenLoginWindow))
	insertTokenUser(t, st, "stale", "stale@example.com", now.Add(-TokenLoginWindow-time.Nanosecond))
	oldUse := now.Add(-time.Hour)
	insertToken(t, st, Token{ID: "live", UserID: "fresh", Name: "live", Hash: idcodec.HashSecret("live-secret"), Enabled: true, CreatedAt: now.Add(-time.Hour), LastUsedAt: &oldUse})
	insertToken(t, st, Token{ID: "disabled", UserID: "fresh", Name: "disabled", Hash: idcodec.HashSecret("disabled-secret"), CreatedAt: now.Add(-time.Hour), LastUsedAt: &oldUse})
	insertToken(t, st, Token{ID: "stale", UserID: "stale", Name: "stale", Hash: idcodec.HashSecret("stale-secret"), Enabled: true, CreatedAt: now.Add(-time.Hour), LastUsedAt: &oldUse})
	equalExpiry := now
	insertToken(t, st, Token{ID: "expired", UserID: "fresh", Name: "expired", Hash: idcodec.HashSecret("expired-secret"), Enabled: true, CreatedAt: now.Add(-time.Hour), ExpiresAt: &equalExpiry, LastUsedAt: &oldUse})
	nonTargetBefore := make(map[string]sql.NullInt64)
	for _, id := range []string{"disabled", "stale", "expired"} {
		lastUsed, err := tokenLastUsed(t, st, id)
		if err != nil {
			t.Fatalf("read %q last_used_at before live touch: %v", id, err)
		}
		nonTargetBefore[id] = lastUsed
	}

	got, err := st.TouchTokenIdentity("live-secret", now)
	want := Identity{UserID: "fresh", Email: "fresh@example.com"}
	if err != nil || got != want {
		t.Fatalf("TouchTokenIdentity(live) = %#v, %v; want %#v, nil", got, err, want)
	}
	if lastUsed, err := tokenLastUsed(t, st, "live"); err != nil || !lastUsed.Valid || lastUsed.Int64 != now.UnixNano() {
		t.Fatalf("live last_used_at = %#v, %v; want %d", lastUsed, err, now.UnixNano())
	}
	for id, wantLastUsed := range nonTargetBefore {
		gotLastUsed, err := tokenLastUsed(t, st, id)
		if err != nil {
			t.Fatalf("read %q last_used_at after live touch: %v", id, err)
		}
		if gotLastUsed != wantLastUsed {
			t.Errorf("successful touch changed non-target %q last_used_at from %#v to %#v", id, wantLastUsed, gotLastUsed)
		}
	}

	for _, secret := range []string{"disabled-secret", "expired-secret", "stale-secret", "unknown-secret"} {
		before := allTokenStates(t, st)
		identity, err := st.TouchTokenIdentity(secret, now)
		if !errors.Is(err, ErrNotFound) || identity != (Identity{}) {
			t.Errorf("TouchTokenIdentity(%q) = %#v, %v; want zero identity and ErrNotFound", secret, identity, err)
		}
		if after := allTokenStates(t, st); !reflect.DeepEqual(after, before) {
			t.Errorf("TouchTokenIdentity(%q) changed tokens: before %#v, after %#v", secret, before, after)
		}
	}
}

func tokenMutationFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	st := openTokenTestStore(t, bytes.NewReader(nil))
	now := tokenTestNow()
	insertTokenUser(t, st, "owner", "owner@example.com", now)
	insertTokenUser(t, st, "other", "other@example.com", now)
	insertToken(t, st, Token{ID: "owned", UserID: "owner", Name: "owned", Hash: idcodec.HashSecret("owner-secret"), Enabled: true, CreatedAt: now})
	insertToken(t, st, Token{ID: "foreign", UserID: "other", Name: "foreign", Hash: idcodec.HashSecret("foreign-secret"), Enabled: true, CreatedAt: now})
	return st, "owned", "foreign"
}

func openTokenTestStore(t *testing.T, random io.Reader) *Store {
	t.Helper()
	return openTokenTestStoreAt(t, filepath.Join(t.TempDir(), "auth.db"), random)
}

func openTokenTestStoreAt(t *testing.T, path string, random io.Reader) *Store {
	t.Helper()
	st, err := Open(path, random)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func insertTokenUser(t *testing.T, st *Store, id, email string, lastGoogleLogin time.Time) {
	t.Helper()
	if _, err := st.db.ExecContext(
		context.Background(),
		`INSERT INTO users (id, issuer, subject, email, last_google_login) VALUES (?, ?, ?, ?, ?)`,
		id,
		"issuer-"+id,
		"subject-"+id,
		email,
		lastGoogleLogin.UnixNano(),
	); err != nil {
		t.Fatalf("insert user %q: %v", id, err)
	}
}

func insertToken(t *testing.T, st *Store, token Token) {
	t.Helper()
	if _, err := st.db.ExecContext(
		context.Background(),
		`INSERT INTO tokens (id, user_id, name, hash, enabled, created_at, expires_at, last_used_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		token.ID,
		token.UserID,
		token.Name,
		token.Hash,
		token.Enabled,
		token.CreatedAt.UnixNano(),
		nullableUnixNano(token.ExpiresAt),
		nullableUnixNano(token.LastUsedAt),
	); err != nil {
		t.Fatalf("insert token %q: %v", token.ID, err)
	}
}

func readToken(t *testing.T, st *Store, id string) Token {
	t.Helper()
	token, err := scanToken(st.db.QueryRowContext(
		context.Background(),
		`SELECT id, user_id, name, hash, enabled, created_at, expires_at, last_used_at FROM tokens WHERE id = ?`,
		id,
	))
	if err != nil {
		t.Fatalf("read token %q: %v", id, err)
	}
	return token
}

func allTokenStates(t *testing.T, st *Store) []Token {
	t.Helper()
	rows, err := st.db.QueryContext(
		context.Background(),
		`SELECT id, user_id, name, hash, enabled, created_at, expires_at, last_used_at FROM tokens ORDER BY id`,
	)
	if err != nil {
		t.Fatalf("query all tokens: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var tokens []Token
	for rows.Next() {
		token, err := scanToken(rows)
		if err != nil {
			t.Fatalf("scan token state: %v", err)
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("token state rows: %v", err)
	}
	return tokens
}

func tokenLastUsed(t *testing.T, st *Store, id string) (sql.NullInt64, error) {
	t.Helper()
	var lastUsed sql.NullInt64
	err := st.db.QueryRowContext(context.Background(), `SELECT last_used_at FROM tokens WHERE id = ?`, id).Scan(&lastUsed)
	return lastUsed, err
}

func assertTokenCount(t *testing.T, st *Store, want int) {
	t.Helper()
	var got int
	if err := st.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM tokens`).Scan(&got); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if got != want {
		t.Fatalf("token count = %d, want %d", got, want)
	}
}

func tokenTestNow() time.Time {
	return time.Date(2026, time.April, 5, 6, 7, 8, 901234567, time.UTC)
}

func tokenTimePointer(value time.Time) *time.Time {
	return &value
}

func tokenSequentialBytes(length int) []byte {
	value := make([]byte, length)
	for index := range value {
		value[index] = byte(index)
	}
	return value
}

type countingReader struct {
	reader io.Reader
	read   int
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += n
	return n, err
}
