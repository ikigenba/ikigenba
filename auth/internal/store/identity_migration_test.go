package store

import (
	"bytes"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
)

// R-2SV9-DI1W: taking the variable's address proves its declared error type.
var _ *error = &ErrNotFound

func TestUpsertUserReportsCreation(t *testing.T) {
	// R-T0E5-6XTZ
	st := openUserSessionTestStore(t, bytes.NewReader(sequentialStoreBytes(16)))
	now := sessionTestNow()
	first, created, err := st.UpsertUserOnLogin("issuer", "subject", "first@example.com", now)
	if err != nil || !created {
		t.Fatalf("first upsert = %#v, %v, %v; want created", first, created, err)
	}
	second, created, err := st.UpsertUserOnLogin("issuer", "subject", "second@example.com", now.Add(time.Hour))
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("existing upsert = %#v, %v, %v; want same user, not created", second, created, err)
	}
}

func TestIdentitiesNameOnlyHonoredTokens(t *testing.T) {
	// R-SXYC-FECL
	st := openUserSessionTestStore(t, bytes.NewReader(sequentialStoreBytes(128)))
	now := sessionTestNow()
	user, _, err := st.UpsertUserOnLogin("issuer", "subject", "member@example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(user.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, lookup := range []func(string, time.Time) (Identity, error){st.LookupSessionIdentity, st.TouchSession} {
		identity, err := lookup(session.ID, now)
		if err != nil || identity.TokenID != "" {
			t.Fatalf("session identity = %#v, %v; want empty token id", identity, err)
		}
	}
	for range 2 {
		token, secret, err := st.CreateToken(user.ID, "token", ExpiryNever, now)
		if err != nil {
			t.Fatal(err)
		}
		for _, lookup := range []func(string, string, time.Time) (Identity, error){st.LookupTokenIdentity, st.TouchTokenIdentity} {
			identity, err := lookup(secret, "", now)
			if err != nil || identity.TokenID != token.ID {
				t.Fatalf("token identity = %#v, %v; want token id %q", identity, err, token.ID)
			}
		}
	}
}

func TestOpenMigratesBareTokenIDsWithoutChangingRecords(t *testing.T) {
	testMigrationPreservesRecords(t, true)
}
func TestOpenMigratesVersionTwoWithoutChangingRecords(t *testing.T) {
	// R-F70H-5FI1
	// R-F88D-J78Q
	testMigrationPreservesRecords(t, false)
}
func testMigrationPreservesRecords(t *testing.T, bare bool) {
	// R-F4KO-DW0N
	// R-F5SK-RNRC
	path := filepath.Join(t.TempDir(), "auth.db")
	st, err := newTestStore(t, path, bytes.NewReader(sequentialStoreBytes(240)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.db.Close() })
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
	var tokens []Token
	var secrets []string
	for i, expiry := range []Expiry{ExpiryNever, Expiry30d, Expiry90d, Expiry365d} {
		token, secret, err := st.CreateToken(user.ID, "preserved name", expiry, now)
		if err != nil {
			t.Fatal(err)
		}
		if i%2 == 1 {
			if _, err := st.TouchTokenIdentity(secret, "", now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			touched := now.Add(time.Second)
			token.LastUsedAt = &touched
		}
		if i == 2 {
			if err := st.SetTokenEnabled(user.ID, token.ID, false); err != nil {
				t.Fatal(err)
			}
			token.Enabled = false
		}
		tokens = append(tokens, token)
		secrets = append(secrets, secret)
	}
	before := domainRows(t, st)
	if err := st.db.Close(); err != nil {
		t.Fatal(err)
	}
	// Keep one already-prefixed token alongside three old-shape tokens.
	stripFixtureTokenPrefixes(t, path, tokens[:3], bare)
	for range 2 {
		st, err = newTestStore(t, path, bytes.NewReader(nil))
		if err != nil {
			t.Fatal(err)
		}
		if rowCountAll(t, st, "clients") != 0 || rowCountAll(t, st, "auth_codes") != 0 {
			t.Fatal("migration must create empty client/code tables")
		}
		if after := domainRows(t, st); !reflect.DeepEqual(after, before) {
			t.Fatalf("migration changed domain rows: before %#v, after %#v", before, after)
		}
		for _, want := range tokens {
			if got := readToken(t, st, want.ID); !reflect.DeepEqual(got, want) {
				t.Fatalf("reopened token = %#v, want unchanged %#v", got, want)
			}
			bare := strings.TrimPrefix(want.ID, idcodec.TokenIDPrefix)
			if err := st.SetTokenEnabled(user.ID, bare, true); !errors.Is(err, ErrNotFound) {
				t.Fatalf("bare id %q still names a token: %v", bare, err)
			}
			if err := st.DeleteToken(user.ID, bare); !errors.Is(err, ErrNotFound) {
				t.Fatalf("bare id %q still deletes a token: %v", bare, err)
			}
		}
		listed, err := st.ListTokens(user.ID)
		if err != nil || len(listed) != len(tokens) {
			t.Fatalf("ListTokens() = %#v, %v; want four tokens", listed, err)
		}
		for _, token := range listed {
			if !strings.HasPrefix(token.ID, idcodec.TokenIDPrefix) {
				t.Fatalf("bare token id survived: %q", token.ID)
			}
		}
		assertStoredUser(t, st, user)
		assertStoredSession(t, st, session.ID, user.ID, now.UnixNano(), now.UnixNano())
		if err := st.db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	st, err = newTestStore(t, path, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	gotState, err := st.ConsumeLoginState(state.State)
	if err != nil || gotState != state {
		t.Fatalf("login state after migration = %#v, %v; want %#v", gotState, err, state)
	}
	for i, token := range tokens {
		for _, lookup := range []func(string, string, time.Time) (Identity, error){st.LookupTokenIdentity, st.TouchTokenIdentity} {
			identity, err := lookup(secrets[i], "ANY.HOST", now.Add(time.Minute))
			if token.Enabled {
				want := Identity{UserID: user.ID, Email: user.Email, TokenID: token.ID}
				if err != nil || identity != want {
					t.Fatalf("migrated authentication = %#v, %v; want %#v", identity, err, want)
				}
			} else if !errors.Is(err, ErrNotFound) {
				t.Fatalf("disabled migrated token authenticated: %#v, %v", identity, err)
			}
		}
	}
}
