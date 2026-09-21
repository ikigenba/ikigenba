package store

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
)

var (
	// R-4JTL-E30V
	_ func(*Store, string, string, string, time.Time) (User, error) = (*Store).UpsertUserOnLogin
	// R-4L1H-RURK
	_ func(*Store, string, time.Time) (Session, error) = (*Store).CreateSession
	// R-4M9E-5MI9
	_ func(*Store, string, time.Time) (Identity, error) = (*Store).LookupSessionIdentity
	// R-4NHA-JE8Y
	_ func(*Store, string, time.Time) (Identity, error) = (*Store).TouchSession
	// R-4OP6-X5ZN
	_ func(*Store, string) error = (*Store).DeleteSession
)

func TestUpsertUserOnLoginCreatesThenRefreshesOnePersistentUser(t *testing.T) {
	// R-5AND-T1C5
	// R-5BVA-6T2U
	random := sequentialStoreBytes(16)
	path := filepath.Join(t.TempDir(), "auth.db")
	st, err := Open(path, bytes.NewReader(random))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	firstNow := time.Date(2026, time.March, 1, 2, 3, 4, 5, time.UTC)
	first, err := st.UpsertUserOnLogin("https://issuer.example", "subject-1", "old@example.com", firstNow)
	if err != nil {
		t.Fatalf("first UpsertUserOnLogin() error = %v", err)
	}
	wantFirst := User{
		ID:              idcodec.Encode(random),
		Issuer:          "https://issuer.example",
		Subject:         "subject-1",
		Email:           "old@example.com",
		LastGoogleLogin: firstNow,
	}
	if !reflect.DeepEqual(first, wantFirst) {
		t.Fatalf("first UpsertUserOnLogin() = %#v, want %#v", first, wantFirst)
	}

	// Updating an existing identity must not need or consume fresh randomness.
	st.rand = bytes.NewReader(nil)
	secondNow := firstNow.Add(7 * time.Hour)
	second, err := st.UpsertUserOnLogin("https://issuer.example", "subject-1", "new@example.com", secondNow)
	if err != nil {
		t.Fatalf("second UpsertUserOnLogin() error = %v", err)
	}
	wantSecond := wantFirst
	wantSecond.Email = "new@example.com"
	wantSecond.LastGoogleLogin = secondNow
	if !reflect.DeepEqual(second, wantSecond) {
		t.Fatalf("second UpsertUserOnLogin() = %#v, want %#v", second, wantSecond)
	}

	var count int
	if err := st.db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM users WHERE issuer = ? AND subject = ?`,
		first.Issuer,
		first.Subject,
	).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("user row count = %d, want 1", count)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	reopened, err := Open(path, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	var stored User
	var storedLogin int64
	if err := reopened.db.QueryRowContext(
		context.Background(),
		`SELECT id, issuer, subject, email, last_google_login FROM users`,
	).Scan(&stored.ID, &stored.Issuer, &stored.Subject, &stored.Email, &storedLogin); err != nil {
		t.Fatalf("read reopened user: %v", err)
	}
	stored.LastGoogleLogin = time.Unix(0, storedLogin).UTC()
	if !reflect.DeepEqual(stored, wantSecond) {
		t.Fatalf("reopened user = %#v, want %#v", stored, wantSecond)
	}
}

func TestUpsertUserOnLoginRandomFailurePersistsNothing(t *testing.T) {
	st := openUserSessionTestStore(t, bytes.NewReader(make([]byte, 15)))
	got, err := st.UpsertUserOnLogin("issuer", "subject", "member@example.com", sessionTestNow())
	if err == nil || got != (User{}) {
		t.Fatalf("UpsertUserOnLogin() = %#v, %v; want zero user and error", got, err)
	}
	assertTableCount(t, st, "users", 0)
}

func TestCreateSessionUsesInjectedIDAndPersistsExactTimes(t *testing.T) {
	// R-5D36-KKTJ
	random := sequentialStoreBytes(32)
	path := filepath.Join(t.TempDir(), "auth.db")
	st, err := Open(path, bytes.NewReader(random))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	now := sessionTestNow()
	user, err := st.UpsertUserOnLogin("issuer", "subject", "member@example.com", now)
	if err != nil {
		t.Fatalf("UpsertUserOnLogin() error = %v", err)
	}

	got, err := st.CreateSession(user.ID, now)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	want := Session{
		ID:         idcodec.Encode(random[16:]),
		UserID:     user.ID,
		LoginAt:    now,
		LastUsedAt: now,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CreateSession() = %#v, want %#v", got, want)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	reopened, err := Open(path, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	assertStoredSession(t, reopened, want.ID, want.UserID, now.UnixNano(), now.UnixNano())
}

func TestSessionLivenessBoundariesAndLookupNeverMutates(t *testing.T) {
	// R-5EB2-YCK8
	// R-5GQV-PW1M
	st := openUserSessionTestStore(t, bytes.NewReader(sequentialStoreBytes(16)))
	now := sessionTestNow()
	user, err := st.UpsertUserOnLogin("issuer", "subject", "identity@example.com", now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("UpsertUserOnLogin() error = %v", err)
	}

	cases := []struct {
		name       string
		id         string
		loginAt    time.Time
		lastUsedAt time.Time
		live       bool
	}{
		{name: "idle boundary inclusive", id: "idle-boundary", loginAt: now.Add(-time.Hour), lastUsedAt: now.Add(-SessionIdle), live: true},
		{name: "max boundary inclusive", id: "max-boundary", loginAt: now.Add(-SessionMax), lastUsedAt: now.Add(-time.Minute), live: true},
		{name: "idle exceeded by nanosecond", id: "idle-expired", loginAt: now.Add(-time.Hour), lastUsedAt: now.Add(-SessionIdle - time.Nanosecond)},
		{name: "max exceeded by nanosecond", id: "max-expired", loginAt: now.Add(-SessionMax - time.Nanosecond), lastUsedAt: now.Add(-time.Minute)},
	}
	for _, tt := range cases {
		insertSessionFixture(t, st, tt.id, user.ID, tt.loginAt, tt.lastUsedAt)
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := st.LookupSessionIdentity(tt.id, now)
			if tt.live {
				if err != nil {
					t.Fatalf("LookupSessionIdentity() error = %v", err)
				}
				want := Identity{UserID: user.ID, Email: user.Email}
				if got != want {
					t.Fatalf("LookupSessionIdentity() = %#v, want %#v", got, want)
				}
			} else if !errors.Is(err, ErrNotFound) || got != (Identity{}) {
				t.Fatalf("LookupSessionIdentity() = %#v, %v; want zero identity and ErrNotFound", got, err)
			}
			assertStoredSession(t, st, tt.id, user.ID, tt.loginAt.UnixNano(), tt.lastUsedAt.UnixNano())
		})
	}

	if got, err := st.LookupSessionIdentity("unknown-session", now); !errors.Is(err, ErrNotFound) || got != (Identity{}) {
		t.Fatalf("unknown LookupSessionIdentity() = %#v, %v; want zero identity and ErrNotFound", got, err)
	}
	assertTableCount(t, st, "sessions", len(cases))
	assertStoredUser(t, st, user)
}

func TestTouchSessionConditionallyUpdatesOnlyLiveSession(t *testing.T) {
	// R-5HYS-3NSB
	st := openUserSessionTestStore(t, bytes.NewReader(sequentialStoreBytes(16)))
	now := sessionTestNow()
	user, err := st.UpsertUserOnLogin("issuer", "subject", "identity@example.com", now)
	if err != nil {
		t.Fatalf("UpsertUserOnLogin() error = %v", err)
	}

	insertSessionFixture(t, st, "live", user.ID, now.Add(-SessionMax), now.Add(-SessionIdle))
	got, err := st.TouchSession("live", now)
	if err != nil {
		t.Fatalf("live TouchSession() error = %v", err)
	}
	want := Identity{UserID: user.ID, Email: user.Email}
	if got != want {
		t.Fatalf("live TouchSession() = %#v, want %#v", got, want)
	}
	assertStoredSession(t, st, "live", user.ID, now.Add(-SessionMax).UnixNano(), now.UnixNano())

	invalid := []struct {
		id         string
		loginAt    time.Time
		lastUsedAt time.Time
	}{
		{id: "idle-expired", loginAt: now.Add(-time.Hour), lastUsedAt: now.Add(-SessionIdle - time.Nanosecond)},
		{id: "max-expired", loginAt: now.Add(-SessionMax - time.Nanosecond), lastUsedAt: now.Add(-time.Minute)},
	}
	for _, tt := range invalid {
		insertSessionFixture(t, st, tt.id, user.ID, tt.loginAt, tt.lastUsedAt)
		identity, err := st.TouchSession(tt.id, now)
		if !errors.Is(err, ErrNotFound) || identity != (Identity{}) {
			t.Errorf("TouchSession(%q) = %#v, %v; want zero identity and ErrNotFound", tt.id, identity, err)
		}
		assertStoredSession(t, st, tt.id, user.ID, tt.loginAt.UnixNano(), tt.lastUsedAt.UnixNano())
	}

	if identity, err := st.TouchSession("unknown", now); !errors.Is(err, ErrNotFound) || identity != (Identity{}) {
		t.Fatalf("unknown TouchSession() = %#v, %v; want zero identity and ErrNotFound", identity, err)
	}
	assertTableCount(t, st, "sessions", 3)
	assertStoredUser(t, st, user)
}

func TestDeleteSessionIsIdempotentAndLeavesUser(t *testing.T) {
	// R-5J6O-HFJ0
	st := openUserSessionTestStore(t, bytes.NewReader(sequentialStoreBytes(32)))
	now := sessionTestNow()
	user, err := st.UpsertUserOnLogin("issuer", "subject", "member@example.com", now)
	if err != nil {
		t.Fatalf("UpsertUserOnLogin() error = %v", err)
	}
	session, err := st.CreateSession(user.ID, now)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if err := st.DeleteSession(session.ID); err != nil {
		t.Fatalf("first DeleteSession() error = %v", err)
	}
	if _, err := st.LookupSessionIdentity(session.ID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("lookup after delete error = %v, want ErrNotFound", err)
	}
	if err := st.DeleteSession(session.ID); err != nil {
		t.Fatalf("absent DeleteSession() error = %v", err)
	}
	assertTableCount(t, st, "sessions", 0)
	assertStoredUser(t, st, user)
}

func openUserSessionTestStore(t *testing.T, random io.Reader) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "auth.db"), random)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	return st
}

func insertSessionFixture(t *testing.T, st *Store, id, userID string, loginAt, lastUsedAt time.Time) {
	t.Helper()
	if _, err := st.db.ExecContext(
		context.Background(),
		`INSERT INTO sessions (id, user_id, login_at, last_used_at) VALUES (?, ?, ?, ?)`,
		id,
		userID,
		loginAt.UnixNano(),
		lastUsedAt.UnixNano(),
	); err != nil {
		t.Fatalf("insert session fixture %q: %v", id, err)
	}
}

func assertStoredSession(t *testing.T, st *Store, id, wantUserID string, wantLoginAt, wantLastUsedAt int64) {
	t.Helper()
	var gotUserID string
	var gotLoginAt, gotLastUsedAt int64
	if err := st.db.QueryRowContext(
		context.Background(),
		`SELECT user_id, login_at, last_used_at FROM sessions WHERE id = ?`,
		id,
	).Scan(&gotUserID, &gotLoginAt, &gotLastUsedAt); err != nil {
		t.Fatalf("read stored session %q: %v", id, err)
	}
	if gotUserID != wantUserID || gotLoginAt != wantLoginAt || gotLastUsedAt != wantLastUsedAt {
		t.Fatalf(
			"stored session %q = (%q, %d, %d), want (%q, %d, %d)",
			id,
			gotUserID,
			gotLoginAt,
			gotLastUsedAt,
			wantUserID,
			wantLoginAt,
			wantLastUsedAt,
		)
	}
}

func assertStoredUser(t *testing.T, st *Store, want User) {
	t.Helper()
	var got User
	var lastGoogleLogin int64
	if err := st.db.QueryRowContext(
		context.Background(),
		`SELECT id, issuer, subject, email, last_google_login FROM users WHERE id = ?`,
		want.ID,
	).Scan(&got.ID, &got.Issuer, &got.Subject, &got.Email, &lastGoogleLogin); err != nil {
		t.Fatalf("read stored user %q: %v", want.ID, err)
	}
	got.LastGoogleLogin = time.Unix(0, lastGoogleLogin).UTC()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stored user = %#v, want %#v", got, want)
	}
}

func assertTableCount(t *testing.T, st *Store, table string, want int) {
	t.Helper()
	var got int
	if err := st.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s row count = %d, want %d", table, got, want)
	}
}

func sessionTestNow() time.Time {
	return time.Date(2026, time.September, 20, 17, 30, 0, 123456789, time.UTC)
}

func sequentialStoreBytes(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i)
	}

	return data
}
