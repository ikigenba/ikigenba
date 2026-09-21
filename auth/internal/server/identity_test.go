package server

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
	"github.com/ikigenba/ikigenba/auth/internal/store"
	"modernc.org/sqlite"
)

var identityNow = time.Date(2026, time.September, 20, 15, 0, 0, 123, time.UTC)

var identitySessionProbeCalls atomic.Int64

var (
	identitySessionProbeRegisterOnce sync.Once
	identitySessionProbeRegisterErr  error
)

type identityRand struct{ next byte }

func (r *identityRand) Read(p []byte) (int, error) {
	r.next++
	for i := range p {
		p[i] = r.next
	}
	return len(p), nil
}

type identityFixture struct {
	store *store.Store
	path  string
}

func openIdentityFixture(t *testing.T) identityFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.db")
	st, err := store.Open(path, &identityRand{})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("Store.Close() error = %v", err)
		}
	})
	return identityFixture{store: st, path: path}
}

func (f identityFixture) server() *Server {
	return New(Config{Store: f.store, Now: func() time.Time { return identityNow }})
}

func (f identityFixture) user(t *testing.T, subject, email string, login time.Time) store.User {
	t.Helper()
	user, err := f.store.UpsertUserOnLogin("issuer", subject, email, login)
	if err != nil {
		t.Fatalf("UpsertUserOnLogin() error = %v", err)
	}
	return user
}

func (f identityFixture) session(t *testing.T, userID string, login, lastUsed time.Time) store.Session {
	t.Helper()
	session, err := f.store.CreateSession(userID, login)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	f.exec(t, `UPDATE sessions SET login_at = ?, last_used_at = ? WHERE id = ?`, login.UnixNano(), lastUsed.UnixNano(), session.ID)
	session.LoginAt = login
	session.LastUsedAt = lastUsed
	return session
}

func (f identityFixture) token(t *testing.T, userID, name string, expiry store.Expiry, created time.Time) (store.Token, string) {
	t.Helper()
	token, secret, err := f.store.CreateToken(userID, name, expiry, created)
	if err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}
	return token, secret
}

func (f identityFixture) setTokenSecret(t *testing.T, tokenID, secret string) {
	t.Helper()
	f.exec(t, `UPDATE tokens SET hash = ? WHERE id = ?`, idcodec.HashSecret(secret), tokenID)
}

func (f identityFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", f.path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("database fixture update: %v", err)
	}
}

func identityRequest(target, sessionID, bearer string) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if sessionID != "" {
		req.AddCookie(&http.Cookie{
			Name:     SessionCookieName,
			Value:    sessionID,
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return req
}

func serveIdentity(handler func(http.ResponseWriter, *http.Request), req *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler(response, req)
	return response
}

func TestIdentityHeaderConstants(t *testing.T) {
	// R-F4UP-FSRL: the exported header names have their exact wire spelling.
	if HeaderUserID != "X-User-Id" || HeaderUserEmail != "X-User-Email" {
		t.Fatalf("identity headers = %q, %q", HeaderUserID, HeaderUserEmail)
	}
}

func TestCheckSessionOutcomes(t *testing.T) {
	fixture := openIdentityFixture(t)
	user := fixture.user(t, "session-user", "session@example.com", identityNow)
	live := fixture.session(t, user.ID, identityNow.Add(-3*time.Hour), identityNow.Add(-time.Minute))
	idle := fixture.session(t, user.ID, identityNow.Add(-3*time.Hour), identityNow.Add(-20*time.Minute))
	capped := fixture.session(t, user.ID, identityNow.Add(-18*time.Hour-30*time.Minute), identityNow.Add(-time.Minute))
	srv := fixture.server()

	t.Run("live", func(t *testing.T) {
		before := fixture.snapshot(t)
		response := serveIdentity(srv.handleCheck, identityRequest("/check", live.ID, ""))

		// R-F8IE-L3ZO: a live cookie authenticates, emits both identity headers,
		// and advances last use to the injected request time.
		if response.Code != http.StatusOK || response.Header().Get(HeaderUserID) != user.ID || response.Header().Get(HeaderUserEmail) != user.Email {
			t.Fatalf("response = %d, headers %#v", response.Code, response.Header())
		}
		after := fixture.snapshot(t)
		assertOnlySessionTouch(t, before, after, live.ID, identityNow)
	})

	t.Run("missing", func(t *testing.T) {
		before := fixture.snapshot(t)
		response := serveIdentity(srv.handleCheck, identityRequest("/check", "", ""))
		// R-F9QA-YVQD: no credential is a headerless 401 and changes nothing.
		assertCheckRefusal(t, response, http.StatusUnauthorized)
		assertSnapshotEqual(t, fixture.snapshot(t), before)
	})

	t.Run("idle", func(t *testing.T) {
		before := fixture.snapshot(t)
		response := serveIdentity(srv.handleCheck, identityRequest("/check", idle.ID, ""))
		// R-FAY7-CNH2: 20 idle minutes inside the 18-hour cap is a non-mutating 401.
		assertCheckRefusal(t, response, http.StatusUnauthorized)
		assertSnapshotEqual(t, fixture.snapshot(t), before)
	})

	t.Run("capped", func(t *testing.T) {
		before := fixture.snapshot(t)
		response := serveIdentity(srv.handleCheck, identityRequest("/check", capped.ID, ""))
		// R-FC63-QF7R: 18.5 hours since login is a non-mutating 401 despite recent use.
		assertCheckRefusal(t, response, http.StatusUnauthorized)
		assertSnapshotEqual(t, fixture.snapshot(t), before)
	})

	// R-FI9L-N9X8: every /check assertion above observes identity only in the
	// two headers; the response body's unspecified content is never inspected.
}

func TestCheckBearerWinsAndTouchesOnlyToken(t *testing.T) {
	fixture := openIdentityFixture(t)
	sessionUser := fixture.user(t, "cookie-owner", "cookie@example.com", identityNow)
	session := fixture.session(t, sessionUser.ID, identityNow.Add(-time.Hour), identityNow.Add(-time.Minute))
	tokenUser := fixture.user(t, "token-owner", "token@example.com", identityNow)
	token, secret := fixture.token(t, tokenUser.ID, "deploy", store.ExpiryNever, identityNow.Add(-time.Hour))
	secret += " "
	fixture.setTokenSecret(t, token.ID, secret)
	before := fixture.snapshot(t)

	response := serveIdentity(fixture.server().handleCheck, identityRequest("/check", session.ID, secret))

	// R-F62L-TKIA: the complete ikp_-prefixed bearer suffix, including its
	// trailing space, is accepted because exactly that value is hashed in the
	// real store; trimming or normalizing it would make authentication fail.
	// R-FDE0-46YG: an honored bearer emits its identity and records request-time use.
	// R-FH1P-9I6J: the honored bearer wins over a different live session, which is untouched.
	// R-F7AI-7C8Z: token resolution is exclusive whenever the Bearer scheme is present.
	if response.Code != http.StatusOK || response.Header().Get(HeaderUserID) != tokenUser.ID || response.Header().Get(HeaderUserEmail) != tokenUser.Email {
		t.Fatalf("response = %d, headers %#v", response.Code, response.Header())
	}
	after := fixture.snapshot(t)
	assertOnlyTokenTouch(t, before, after, token.ID, identityNow)
}

func TestCheckRefusedBearersAreIdenticalAndDoNotMutate(t *testing.T) {
	type result struct {
		code   int
		header http.Header
	}
	var want *result
	for _, cause := range []string{"unknown", "disabled", "expired", "stale-owner"} {
		t.Run(cause, func(t *testing.T) {
			fixture := openIdentityFixture(t)
			cookieUser := fixture.user(t, "cookie", "cookie@example.com", identityNow)
			session := fixture.session(t, cookieUser.ID, identityNow.Add(-time.Hour), identityNow.Add(-time.Minute))
			ownerLogin := identityNow
			if cause == "stale-owner" {
				ownerLogin = identityNow.Add(-store.TokenLoginWindow - time.Second)
			}
			owner := fixture.user(t, "owner", "owner@example.com", ownerLogin)
			secret := "ikp_unknown"
			if cause != "unknown" {
				created := identityNow.Add(-time.Hour)
				expiry := store.ExpiryNever
				if cause == "expired" {
					created = identityNow.Add(-31 * 24 * time.Hour)
					expiry = store.Expiry30d
				}
				token, minted := fixture.token(t, owner.ID, cause, expiry, created)
				secret = minted
				if cause == "disabled" {
					if err := fixture.store.SetTokenEnabled(owner.ID, token.ID, false); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := fixture.snapshot(t)
			response := serveIdentity(fixture.server().handleCheck, identityRequest("/check", session.ID, secret))

			// R-FFTS-VQFU: every refusal cause has identical contracted status and
			// headers, no identity disclosure, and no database mutation.
			// R-F7AI-7C8Z: even a refused bearer prevents fallback to the live cookie.
			// R-2W27-FZGG: a refused bearer is 403, never the session-path 401.
			assertCheckRefusal(t, response, http.StatusForbidden)
			assertSnapshotEqual(t, fixture.snapshot(t), before)
			got := result{code: response.Code, header: response.Header().Clone()}
			if want == nil {
				want = &got
			} else if !reflect.DeepEqual(got, *want) {
				t.Fatalf("response = %#v, want byte-identical %#v", got, *want)
			}
		})
	}
}

func TestBearerDoesNotConsultSession(t *testing.T) {
	identitySessionProbeRegisterOnce.Do(func() {
		identitySessionProbeRegisterErr = sqlite.RegisterScalarFunction(
			"identity_session_probe",
			0,
			func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
				identitySessionProbeCalls.Add(1)
				return "", nil
			},
		)
	})
	if err := identitySessionProbeRegisterErr; err != nil {
		t.Fatalf("RegisterScalarFunction() error = %v", err)
	}

	fixture := openIdentityFixture(t)
	cookieOwner := fixture.user(t, "cookie-probe", "cookie-probe@example.com", identityNow)
	session := fixture.session(t, cookieOwner.ID, identityNow.Add(-time.Hour), identityNow.Add(-time.Minute))
	tokenOwner := fixture.user(t, "token-probe", "token-probe@example.com", identityNow)
	_, secret := fixture.token(t, tokenOwner.ID, "probe", store.ExpiryNever, identityNow.Add(-time.Hour))
	// Make every session-id comparison invoke a non-mutating counter. This
	// instruments the real SQLite-backed store rather than replacing its API.
	fixture.exec(t, `ALTER TABLE sessions RENAME TO identity_sessions_data`)
	fixture.exec(t, `CREATE VIEW sessions AS
		SELECT identity_session_probe() || id AS id, user_id, login_at, last_used_at
		FROM identity_sessions_data`)
	identitySessionProbeCalls.Store(0)
	if _, err := fixture.store.LookupSessionIdentity(session.ID, identityNow); err != nil {
		t.Fatalf("probe LookupSessionIdentity() error = %v", err)
	}
	if identitySessionProbeCalls.Load() == 0 {
		t.Fatal("session-query probe did not observe the control lookup")
	}

	for _, test := range []struct {
		name, target, bearer string
		handler              func(http.ResponseWriter, *http.Request)
		status               int
	}{
		{name: "me honored", target: "/me", bearer: secret, handler: fixture.server().handleMe, status: http.StatusOK},
		{name: "me refused", target: "/me", bearer: "ikp_unknown", handler: fixture.server().handleMe, status: http.StatusForbidden},
		{name: "check honored", target: "/check", bearer: secret, handler: fixture.server().handleCheck, status: http.StatusOK},
		{name: "check refused", target: "/check", bearer: "ikp_unknown", handler: fixture.server().handleCheck, status: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			identitySessionProbeCalls.Store(0)
			response := serveIdentity(test.handler, identityRequest(test.target, session.ID, test.bearer))
			// R-F7AI-7C8Z: with either an honored or refused bearer, the
			// instrumented real session query is never reached by /me or /check
			// despite a live cookie.
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if calls := identitySessionProbeCalls.Load(); calls != 0 {
				t.Fatalf("session identity was consulted %d times", calls)
			}
		})
	}
}

func TestMeHonoredCredentialsReturnCompactJSONWithoutMutation(t *testing.T) {
	for _, credential := range []string{"bearer", "session"} {
		t.Run(credential, func(t *testing.T) {
			fixture := openIdentityFixture(t)
			sessionUser := fixture.user(t, "session-owner", "session@example.com", identityNow)
			session := fixture.session(t, sessionUser.ID, identityNow.Add(-time.Hour), identityNow.Add(-time.Minute))
			tokenUser := fixture.user(t, "token-owner", "token@example.com", identityNow)
			token, secret := fixture.token(t, tokenUser.ID, "deploy", store.ExpiryNever, identityNow.Add(-time.Hour))
			before := fixture.snapshot(t)
			if credential == "session" {
				secret = ""
			} else {
				secret += " "
				fixture.setTokenSecret(t, token.ID, secret)
				before = fixture.snapshot(t)
			}

			response := serveIdentity(fixture.server().handleMe, identityRequest("/me", session.ID, secret))
			wantUser := tokenUser
			if credential == "session" {
				wantUser = sessionUser
			}
			// R-FJHI-11NX: an honored token yields exact compact token-owner JSON.
			// R-FKPE-ETEM: a live session yields exact compact session-owner JSON.
			// R-F62L-TKIA: the bearer case succeeds only if its trailing-space
			// suffix reaches the store unmodified; the session case likewise uses
			// the exact opaque cookie value minted by the store.
			// R-F7AI-7C8Z: in the bearer case, the token owner wins over the cookie owner.
			// R-2UUB-27PR: successful /me paths do not change any stored row.
			wantBody := fmt.Sprintf(`{"id":%q,"email":%q}`, wantUser.ID, wantUser.Email)
			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" || response.Body.String() != wantBody {
				t.Fatalf("response = %d %q %q, want 200 JSON %q", response.Code, response.Header().Get("Content-Type"), response.Body.String(), wantBody)
			}
			assertSnapshotEqual(t, fixture.snapshot(t), before)
		})
	}
}

func TestMeMissingAndDeadSessionsArePlain401WithoutMutation(t *testing.T) {
	for _, sessionID := range []string{"", "unknown-session"} {
		t.Run(sessionID, func(t *testing.T) {
			fixture := openIdentityFixture(t)
			before := fixture.snapshot(t)
			response := serveIdentity(fixture.server().handleMe, identityRequest("/me", sessionID, ""))

			// R-GAHD-7316: absent and non-live sessions return a single plain-text line, not JSON.
			// R-2W27-FZGG: a failed session path is 401, never bearer-path 403.
			// R-2UUB-27PR: unsuccessful session /me paths perform no write.
			assertPlainRefusal(t, response, http.StatusUnauthorized, "sign in required\n")
			assertSnapshotEqual(t, fixture.snapshot(t), before)
		})
	}
}

func TestMeRefusedBearersAreIdenticalAndDoNotMutate(t *testing.T) {
	type result struct {
		code   int
		header http.Header
		body   string
	}
	var want *result
	for _, cause := range []string{"unknown", "disabled", "expired", "stale-owner"} {
		t.Run(cause, func(t *testing.T) {
			fixture := openIdentityFixture(t)
			cookieOwner := fixture.user(t, "cookie", "cookie@example.com", identityNow)
			session := fixture.session(t, cookieOwner.ID, identityNow.Add(-time.Hour), identityNow.Add(-time.Minute))
			ownerLogin := identityNow
			if cause == "stale-owner" {
				ownerLogin = identityNow.Add(-store.TokenLoginWindow - time.Second)
			}
			owner := fixture.user(t, "owner", "owner@example.com", ownerLogin)
			secret := "ikp_unknown"
			if cause != "unknown" {
				created := identityNow.Add(-time.Hour)
				expiry := store.ExpiryNever
				if cause == "expired" {
					created = identityNow.Add(-31 * 24 * time.Hour)
					expiry = store.Expiry30d
				}
				token, minted := fixture.token(t, owner.ID, cause, expiry, created)
				secret = minted
				if cause == "disabled" {
					if err := fixture.store.SetTokenEnabled(owner.ID, token.ID, false); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := fixture.snapshot(t)
			response := serveIdentity(fixture.server().handleMe, identityRequest("/me", session.ID, secret))

			// R-2TME-OFZ2: all refusal causes yield the same single-line token-refused response.
			// R-F7AI-7C8Z: a refused bearer does not fall back to a live session.
			// R-2W27-FZGG: a refused bearer is 403, never session-path 401.
			// R-2UUB-27PR: refused /me paths do not change any stored row.
			assertPlainRefusal(t, response, http.StatusForbidden, "token refused\n")
			assertSnapshotEqual(t, fixture.snapshot(t), before)
			got := result{code: response.Code, header: response.Header().Clone(), body: response.Body.String()}
			if want == nil {
				want = &got
			} else if !reflect.DeepEqual(got, *want) {
				t.Fatalf("response = %#v, want byte-identical %#v", got, *want)
			}
		})
	}
}

func assertCheckRefusal(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status || response.Header().Get(HeaderUserID) != "" || response.Header().Get(HeaderUserEmail) != "" {
		t.Fatalf("response = %d, headers %#v; want %d without identity", response.Code, response.Header(), status)
	}
}

func assertPlainRefusal(t *testing.T, response *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if response.Code != status || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" || response.Body.String() != body || !strings.HasSuffix(body, "\n") || strings.HasPrefix(strings.TrimSpace(body), "{") {
		t.Fatalf("response = %d %q %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

type identitySnapshot struct {
	users       []identityUserRow
	sessions    []identitySessionRow
	loginStates []identityLoginStateRow
	tokens      []identityTokenRow
}

type identityUserRow struct {
	id, issuer, subject, email string
	lastGoogleLogin            int64
}

type identitySessionRow struct {
	id, userID          string
	loginAt, lastUsedAt int64
}

type identityLoginStateRow struct{ state, verifier, returnURL string }

type identityTokenRow struct {
	id, userID, name, hash string
	enabled                bool
	createdAt              int64
	expiresAt, lastUsedAt  sql.NullInt64
}

func (f identityFixture) snapshot(t *testing.T) identitySnapshot {
	t.Helper()
	db, err := sql.Open("sqlite", f.path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()
	var snapshot identitySnapshot
	readRows(t, db, `SELECT id, issuer, subject, email, last_google_login FROM users ORDER BY id`, func(rows *sql.Rows) error {
		var row identityUserRow
		if err := rows.Scan(&row.id, &row.issuer, &row.subject, &row.email, &row.lastGoogleLogin); err != nil {
			return err
		}
		snapshot.users = append(snapshot.users, row)
		return nil
	})
	readRows(t, db, `SELECT id, user_id, login_at, last_used_at FROM sessions ORDER BY id`, func(rows *sql.Rows) error {
		var row identitySessionRow
		if err := rows.Scan(&row.id, &row.userID, &row.loginAt, &row.lastUsedAt); err != nil {
			return err
		}
		snapshot.sessions = append(snapshot.sessions, row)
		return nil
	})
	readRows(t, db, `SELECT state, verifier, return_url FROM login_states ORDER BY state`, func(rows *sql.Rows) error {
		var row identityLoginStateRow
		if err := rows.Scan(&row.state, &row.verifier, &row.returnURL); err != nil {
			return err
		}
		snapshot.loginStates = append(snapshot.loginStates, row)
		return nil
	})
	readRows(t, db, `SELECT id, user_id, name, hash, enabled, created_at, expires_at, last_used_at FROM tokens ORDER BY id`, func(rows *sql.Rows) error {
		var row identityTokenRow
		if err := rows.Scan(&row.id, &row.userID, &row.name, &row.hash, &row.enabled, &row.createdAt, &row.expiresAt, &row.lastUsedAt); err != nil {
			return err
		}
		snapshot.tokens = append(snapshot.tokens, row)
		return nil
	})
	return snapshot
}

func readRows(t *testing.T, db *sql.DB, query string, scan func(*sql.Rows) error) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), query)
	if err != nil {
		t.Fatalf("snapshot query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			t.Fatalf("snapshot scan: %v", err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("snapshot rows: %v", err)
	}
}

func assertSnapshotEqual(t *testing.T, got, want identitySnapshot) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("database changed:\n got  %#v\n want %#v", got, want)
	}
}

func assertOnlySessionTouch(t *testing.T, before, after identitySnapshot, id string, now time.Time) {
	t.Helper()
	want := before
	want.sessions = append([]identitySessionRow(nil), before.sessions...)
	for i := range want.sessions {
		if want.sessions[i].id == id {
			want.sessions[i].lastUsedAt = now.UnixNano()
		}
	}
	assertSnapshotEqual(t, after, want)
}

func assertOnlyTokenTouch(t *testing.T, before, after identitySnapshot, id string, now time.Time) {
	t.Helper()
	want := before
	want.tokens = append([]identityTokenRow(nil), before.tokens...)
	for i := range want.tokens {
		if want.tokens[i].id == id {
			want.tokens[i].lastUsedAt = sql.NullInt64{Int64: now.UnixNano(), Valid: true}
		}
	}
	assertSnapshotEqual(t, after, want)
}
