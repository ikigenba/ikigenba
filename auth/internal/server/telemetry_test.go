package server

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/auth/internal/google"
	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

type trailFixture struct {
	server  *Server
	writer  *telemetry.Writer
	capture *telemetry.Capture
	stderr  bytes.Buffer
}

func newTrail(t *testing.T, cfg Config, random io.Reader) *trailFixture {
	t.Helper()
	f := &trailFixture{capture: &telemetry.Capture{}}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return signInNow }
	}
	if cfg.Rand == nil {
		cfg.Rand = &identityRand{}
	}
	if cfg.Banner == nil {
		cfg.Banner = testPageBanner
	}
	if random == nil {
		random = &identityRand{}
	}
	f.writer = telemetry.New(telemetry.Config{Service: "auth", Sink: f.capture, Stderr: &f.stderr, Now: cfg.Now, Rand: random})
	cfg.Telemetry = f.writer
	f.server = New(cfg)
	t.Cleanup(func() { f.writer.Shutdown(context.Background(), "test") })
	return f
}

func (f *trailFixture) events(t *testing.T) []telemetry.Event {
	t.Helper()
	if err := f.writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.stderr.Len() != 0 {
		t.Fatalf("request stderr: %q", f.stderr.String())
	}
	return f.capture.Events()
}

func (f *trailFixture) request(t *testing.T, r *http.Request) (*httptest.ResponseRecorder, []telemetry.Event) {
	t.Helper()
	before := len(f.events(t))
	w := httptest.NewRecorder()
	f.server.ServeHTTP(w, r)
	return w, f.events(t)[before:]
}

func trailRequest(method, target string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), method, target, nil)
	r.Host = "auth.green.example"
	r.Header.Set("X-Request-Id", "request-one")
	return r
}

func assertTrail(t *testing.T, events []telemetry.Event, r *http.Request, status int, names ...string) {
	t.Helper()
	// R-UEN3-Y8ZA R-UFV0-C0PZ R-T6HN-3SJG R-BD85-ZXX8: the writer's captured envelope defines the ordered request trail.
	want := append([]string{"request.started"}, names...)
	want = append(want, "request.finished")
	got := make([]string, len(events))
	for i, e := range events {
		got[i] = e.Name
		if e.RequestID != events[0].RequestID {
			t.Fatalf("request IDs differ: %#v", events)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event names=%v want %v", got, want)
	}
	if id := r.Header.Get("X-Request-Id"); id != "" && events[0].RequestID != id {
		t.Fatalf("request ID=%q want %q", events[0].RequestID, id)
	}
	if !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"method": r.Method, "path": r.URL.Path}) {
		t.Fatalf("start attrs=%#v", events[0].Attrs)
	}
	end := events[len(events)-1]
	if !reflect.DeepEqual(end.Attrs, telemetry.Attrs{"status": int64(status), "duration_us": int64(0)}) {
		t.Fatalf("finish attrs=%#v", end.Attrs)
	}
	for _, e := range []telemetry.Event{events[0], end} {
		if e.User != r.Header.Get("X-User-Id") {
			t.Fatalf("boundary user=%q", e.User)
		}
	}
}

func TestRequestTrailEveryRoute(t *testing.T) {
	// R-2HW5-XKDN R-3HP0-MB1R: shared files, pages, refusals, unknown routes, and handled store failures share the outer middleware.
	st := openSignInStore(t)
	f := newTrail(t, Config{Store: st, WorkspaceDomain: "green.example"}, nil)
	for _, tc := range []struct {
		method, path string
		status       int
		domain       []string
	}{
		{"GET", "/", 200, nil}, {"GET", "/_appkit/theme.css", 200, nil}, {"PATCH", "/missing", 404, nil},
		{"GET", "/check", 401, []string{"check.refused"}}, {"GET", "/me", 401, nil}, {"POST", "/tokens", 403, nil},
		{"POST", "/logout", 403, nil}, {"GET", "/login/google/callback?state=unknown", 400, []string{"sign_in.refused"}},
	} {
		r := trailRequest(tc.method, tc.path)
		r.Header.Add("X-Request-Id", "ignored-id")
		r.Header.Add("X-User-Id", "upstream-user")
		r.Header.Add("X-User-Id", "ignored-user")
		w, events := f.request(t, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.path, w.Code)
		}
		assertTrail(t, events, r, w.Code, tc.domain...)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	r := trailRequest("GET", "/me")
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w, events := f.request(t, r)
	if w.Code != 500 {
		t.Fatalf("closed store=%d", w.Code)
	}
	assertTrail(t, events, r, 500)
}

func TestConfigTelemetryPublicShape(t *testing.T) {
	// R-B3GY-XRZO: positional construction proves field names/types/order through the public construction seam.
	f := newTrail(t, Config{}, nil)
	cfg := Config{nil, nil, func() time.Time { return signInNow }, &identityRand{}, f.writer, "green.example", "http://auth.green.example:7400", "http://localhost:7400", testPageBanner}
	s := New(cfg)
	r := trailRequest("GET", "/_appkit/theme.css")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || len(f.events(t)) != 2 {
		t.Fatalf("configured request=%d", w.Code)
	}
}

type failedTrailReader struct{}

func (failedTrailReader) Read([]byte) (int, error) { return 0, errors.New("random failed") }

func TestRequestIDRandomness(t *testing.T) {
	// R-DWO7-CX19 R-DRSL-TU2H: minted request ids read the writer's random source; a failed read still yields an opaque 32-digit id.
	for _, random := range []io.Reader{bytes.NewReader(bytes.Repeat([]byte{0xab}, 16)), failedTrailReader{}} {
		f := newTrail(t, Config{}, random)
		r := trailRequest("GET", "/_appkit/theme.css")
		r.Header.Del("X-Request-Id")
		w, events := f.request(t, r)
		assertTrail(t, events, r, w.Code)
		id := events[0].RequestID
		if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) {
			t.Fatalf("id=%q", id)
		}
		if _, ok := random.(*bytes.Reader); ok && id != strings.Repeat("ab", 16) {
			t.Fatalf("deterministic id=%q", id)
		}
	}
}

func TestSignInSuccessAndSignOutTrail(t *testing.T) {
	// R-TBD8-MVI8 R-TDT1-EEZM R-TF0X-S6QB R-THGQ-JQ7P: only successful identity changes emit user events; creation occurs once.
	issuer := newSignInIssuer(t)
	issuer.issue("member-code", "trail-subject", "member@green.example")
	st := openSignInStore(t)
	f := newTrail(t, Config{Store: st, Google: google.NewClient("client-id", "client-secret", "green.example", issuer.server.URL), WorkspaceDomain: "green.example"}, nil)
	var cookie *http.Cookie
	for i := 0; i < 2; i++ {
		state, err := st.CreateLoginState("verifier", "https://app.green.example/private?secret-return")
		if err != nil {
			t.Fatal(err)
		}
		r := trailRequest("GET", "/login/google/callback?state="+state.State+"&code=member-code")
		w, events := f.request(t, r)
		if w.Code != 302 {
			t.Fatalf("callback=%d %s", w.Code, w.Body.String())
		}
		names := []string{"user.signed_in"}
		if i == 0 {
			names = []string{"user.created", "user.signed_in"}
		}
		assertTrail(t, events, r, w.Code, names...)
		cookie = w.Result().Cookies()[0]
		who, err := st.LookupSessionIdentity(cookie.Value, signInNow)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range events[1 : len(events)-1] {
			if e.User != who.UserID || len(e.Attrs) != 0 {
				t.Fatalf("user event=%#v", e)
			}
		}
		assertTrailPrivate(t, events, cookie.Value, state.State, "member-code", "member@green.example", "secret-return")
	}
	for _, value := range []string{cookie.Value, cookie.Value, "unknown", ""} {
		r := trailRequest("POST", "/logout")
		r.Header.Set("Origin", "https://auth.green.example")
		if value != "" {
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: value, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		}
		live, _, err := f.server.sessionIdentity(r)
		if err != nil {
			t.Fatal(err)
		}
		w, events := f.request(t, r)
		if w.Code != 302 {
			t.Fatalf("logout=%d", w.Code)
		}
		assertTrail(t, events, r, 302, "user.signed_out")
		if events[1].User != live.UserID || len(events[1].Attrs) != 0 {
			t.Fatalf("logout event=%#v", events[1])
		}
	}
}

func assertTrailPrivate(t *testing.T, events []telemetry.Event, secrets ...string) {
	t.Helper()
	// R-TIOM-XHYE: secret values never enter event envelopes or attributes.
	for _, e := range events {
		values := []string{e.RequestID, e.User}
		for _, v := range e.Attrs {
			if s, ok := v.(string); ok {
				values = append(values, s)
			}
		}
		for _, value := range values {
			for _, secret := range secrets {
				if secret != "" && strings.Contains(value, secret) {
					t.Fatalf("private value %q in %#v", secret, e)
				}
			}
		}
	}
}

func TestSignInRefusalTrail(t *testing.T) {
	// R-TG8U-5YH0 R-THGQ-JQ7P: unknown, cancelled, not-member and provider failures have precise, anonymous reasons.
	issuer := newSignInIssuer(t)
	issuer.issueClaims("outside-code", map[string]any{"iss": "https://accounts.google.com", "sub": "outside", "aud": "client-id", "exp": 4102444800, "iat": 1700000000, "email": "outside@elsewhere.test", "email_verified": true, "hd": "elsewhere.test"})
	st := openSignInStore(t)
	f := newTrail(t, Config{Store: st, Google: google.NewClient("client-id", "client-secret", "green.example", issuer.server.URL), WorkspaceDomain: "green.example"}, nil)
	for _, tc := range []struct {
		query, reason string
		status        int
	}{
		{"state=unknown", "unknown_state", 400}, {"error=access_denied", "cancelled", 200}, {"code=outside-code", "not_member", 403}, {"code=missing", "provider_failed", 502},
	} {
		query := tc.query
		if tc.reason == "not_member" || tc.reason == "provider_failed" {
			state, err := st.CreateLoginState("verifier", "")
			if err != nil {
				t.Fatal(err)
			}
			query += "&state=" + state.State
		}
		r := trailRequest("GET", "/login/google/callback?"+query)
		w, events := f.request(t, r)
		if w.Code != tc.status {
			t.Fatalf("%s=%d", tc.reason, w.Code)
		}
		assertTrail(t, events, r, w.Code, "sign_in.refused")
		if events[1].User != "" || !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{"reason": tc.reason}) {
			t.Fatalf("refusal=%#v", events[1])
		}
	}
	// R-2LJV-2VLQ: PKCE bytes are injected independently from the writer's request-id source.
	for i := 0; i < 2; i++ {
		g := newTrail(t, Config{Store: st, Google: google.NewClient("client-id", "client-secret", "green.example", issuer.server.URL), Rand: bytes.NewReader(bytes.Repeat([]byte{3}, 32))}, nil)
		r := trailRequest("GET", "/login/google")
		w, events := g.request(t, r)
		if w.Code != 302 {
			t.Fatalf("login=%d", w.Code)
		}
		assertTrail(t, events, r, 302)
		u, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		state, err := st.ConsumeLoginState(u.Query().Get("state"))
		if err != nil {
			t.Fatal(err)
		}
		if state.Verifier != idcodec.Encode(bytes.Repeat([]byte{3}, 32)) {
			t.Fatalf("verifier=%q", state.Verifier)
		}
	}
}

func TestTokenAndCheckTrail(t *testing.T) {
	// R-TW3J-4Z41 R-9RMT-85YC R-TZR8-AAC4 R-9SUP-LXP1: token changes emit exactly the token ID, and repeated toggles emit nothing.
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"), &identityRand{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	user, _, err := st.UpsertUserOnLogin("issuer", "owner", "owner@green.example", signInNow)
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(user.ID, signInNow)
	if err != nil {
		t.Fatal(err)
	}
	f := newTrail(t, Config{Store: st}, nil)
	r := tokenRequest("/tokens", session.ID, url.Values{"name": {"private-token-name"}, "expires": {"never"}})
	r.Header.Set("X-Request-Id", "request-one")
	w, events := f.request(t, r)
	if w.Code != 200 {
		t.Fatalf("create=%d", w.Code)
	}
	assertTrail(t, events, r, 200, "token.minted")
	tokens, err := st.ListTokens(user.ID)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("tokens=%#v %v", tokens, err)
	}
	token := tokens[0]
	if !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{"token": token.ID}) || events[1].User != user.ID {
		t.Fatalf("mint=%#v", events[1])
	}
	secret := regexp.MustCompile(`ikp_[0-9A-HJKMNP-TV-Z]{52}`).FindString(w.Body.String())
	if secret == "" {
		t.Fatal("no secret")
	}
	for _, tc := range []struct{ action, event string }{{"disable", "token.disabled"}, {"disable", ""}, {"enable", "token.enabled"}, {"enable", ""}} {
		r := tokenRequest("/tokens/"+token.ID+"/"+tc.action, session.ID, nil)
		r.Header.Set("X-Request-Id", "request-one")
		w, events := f.request(t, r)
		if w.Code != 302 {
			t.Fatalf("toggle=%d", w.Code)
		}
		names := []string{}
		if tc.event != "" {
			names = append(names, tc.event)
		}
		assertTrail(t, events, r, 302, names...)
		if tc.event != "" && (events[1].User != user.ID || !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{"token": token.ID})) {
			t.Fatalf("toggle=%#v", events[1])
		}
	}
	testCheckTrail(t, f, st, user, session, token, secret)
	r = tokenRequest("/tokens/"+token.ID+"/delete", session.ID, nil)
	r.Header.Set("X-Request-Id", "request-one")
	w, events = f.request(t, r)
	if w.Code != 302 {
		t.Fatalf("delete=%d", w.Code)
	}
	assertTrail(t, events, r, 302, "token.deleted")
	if events[1].User != user.ID || !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{"token": token.ID}) {
		t.Fatalf("delete=%#v", events[1])
	}
	assertTrailPrivate(t, f.events(t), secret, session.ID, user.Email, token.Name)
}

func testCheckTrail(t *testing.T, f *trailFixture, st *store.Store, user store.User, session store.Session, token store.Token, secret string) {
	t.Helper()
	// R-TJWJ-B9P3 R-TL4F-P1FS R-TMCC-2T6H R-TNK8-GKX6 R-TOS4-UCNV R-TQ01-84EK R-TSFT-ZNVY R-TUVM-R7DC: checks resolve credentials; /me only has boundary events.
	for _, tc := range []struct {
		credential, value, outcome string
		status                     int
	}{{"token", secret, "allowed", 200}, {"session", session.ID, "allowed", 200}, {"none", "", "unauthenticated", 401}, {"token", "unknown", "forbidden", 403}} {
		r := trailRequest("GET", "/check")
		r.Header.Set("X-Original-Method", "POST")
		r.Header.Add("X-Original-Method", "ignored")
		r.Header.Set("X-Original-Host", "app.green.example")
		r.Header.Set("X-Original-URI", "/private?hide-this")
		r.Header.Add("X-Original-URI", "ignored")
		if tc.credential == "token" {
			r.Header.Set("Authorization", "Bearer "+tc.value)
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		}
		if tc.credential == "session" {
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: tc.value, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		}
		w, events := f.request(t, r)
		if w.Code != tc.status {
			t.Fatalf("check=%d", w.Code)
		}
		name := "check.refused"
		wantUser := ""
		if w.Code == 200 {
			name = "check.allowed"
			wantUser = user.ID
		}
		assertTrail(t, events, r, w.Code, name)
		attrs := telemetry.Attrs{"outcome": tc.outcome, "credential": tc.credential, "method": "POST", "host": "app.green.example", "path": "/private"}
		if tc.credential == "token" && w.Code == 200 {
			attrs["token"] = token.ID
		}
		if events[1].User != wantUser || !reflect.DeepEqual(events[1].Attrs, attrs) {
			t.Fatalf("check event=%#v want %#v", events[1], attrs)
		}
		r.URL.Path = "/me"
		w, events = f.request(t, r)
		assertTrail(t, events, r, w.Code)
	}
	var previous *telemetry.Event
	// R-2XQU-WL0O: refusal causes expose the same event.
	for _, reason := range []string{"unknown", "disabled", "expired", "stale"} {
		r := trailRequest("GET", "/check")
		value := secret
		if reason == "unknown" {
			value = "unknown"
		}
		now := signInNow
		if reason == "disabled" {
			if err := st.SetTokenEnabled(user.ID, token.ID, false); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := st.SetTokenEnabled(user.ID, token.ID, true); err != nil {
				t.Fatal(err)
			}
		}
		if reason == "expired" {
			_, valueErr, e := st.CreateToken(user.ID, "expired", store.Expiry30d, signInNow.Add(-31*24*time.Hour))
			if e != nil {
				t.Fatal(e)
			}
			value = valueErr
		}
		if reason == "stale" {
			now = signInNow.Add(31 * 24 * time.Hour)
		}
		f.server.now = func() time.Time { return now }
		r.Header.Set("Authorization", "Bearer "+value)
		r.Header.Set("X-Original-URI", "/path?private")
		w, events := f.request(t, r)
		if w.Code != 403 {
			t.Fatalf("%s=%d", reason, w.Code)
		}
		assertTrail(t, events, r, 403, "check.refused")
		current := events[1]
		current.Time = time.Time{}
		if previous != nil && !reflect.DeepEqual(*previous, current) {
			t.Fatalf("refusal differs %s: %#v %#v", reason, *previous, current)
		}
		previous = &current
	}
	f.server.now = func() time.Time { return signInNow }
	failedStore := openSignInStore(t)
	failed := newTrail(t, Config{Store: failedStore}, nil)
	if err := failedStore.Close(); err != nil {
		t.Fatal(err)
	}
	r := trailRequest("GET", "/check")
	r.Header.Set("Authorization", "Bearer "+secret)
	w, events := failed.request(t, r)
	if w.Code != 500 {
		t.Fatalf("check failed=%d", w.Code)
	}
	assertTrail(t, events, r, 500, "check.failed")
	if events[1].User != "" || !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{"outcome": "failed", "credential": "token", "method": "", "host": "", "path": ""}) {
		t.Fatalf("failed=%#v", events[1])
	}
}

func TestMigratedTokenRoutesHaveNoBareAlias(t *testing.T) {
	// R-9VAI-DH6F: reopening the old token shape makes only the prefixed route actionable.
	for _, action := range []string{"enable", "disable", "delete"} {
		t.Run(action, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.db")
			st, err := store.Open(path, &identityRand{})
			if err != nil {
				t.Fatal(err)
			}
			user, _, err := st.UpsertUserOnLogin("issuer", "owner", "owner@green.example", signInNow)
			if err != nil {
				t.Fatal(err)
			}
			session, err := st.CreateSession(user.ID, signInNow)
			if err != nil {
				t.Fatal(err)
			}
			token, _, err := st.CreateToken(user.ID, "migration", store.ExpiryNever, signInNow)
			if err != nil {
				t.Fatal(err)
			}
			if action == "enable" {
				if err := st.SetTokenEnabled(user.ID, token.ID, false); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := db.QueryContext(context.Background(), `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
			if err != nil {
				t.Fatal(err)
			}
			var tables []string
			for rows.Next() {
				var table string
				if err := rows.Scan(&table); err != nil {
					t.Fatal(err)
				}
				tables = append(tables, table)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			_ = rows.Close()
			bare := strings.TrimPrefix(token.ID, idcodec.TokenIDPrefix)
			for _, table := range tables {
				quotedTable := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
				cols, err := db.QueryContext(context.Background(), "PRAGMA table_info("+quotedTable+")")
				if err != nil {
					t.Fatal(err)
				}
				var columns []string
				for cols.Next() {
					var cid, notNull, pk int
					var name, kind string
					var defaultValue any
					if err := cols.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
						t.Fatal(err)
					}
					columns = append(columns, name)
				}
				if err := cols.Err(); err != nil {
					t.Fatal(err)
				}
				_ = cols.Close()
				for _, column := range columns {
					quotedColumn := `"` + strings.ReplaceAll(column, `"`, `""`) + `"`
					if _, err := db.ExecContext(context.Background(), strings.Join([]string{"UPDATE", quotedTable, "SET", quotedColumn, "= ? WHERE", quotedColumn, "= ?"}, " "), bare, token.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			st, err = store.Open(path, &identityRand{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = st.Close() })
			f := newTrail(t, Config{Store: st}, nil)
			before, err := st.ListTokens(user.ID)
			if err != nil {
				t.Fatal(err)
			}
			r := tokenRequest("/tokens/"+bare+"/"+action, session.ID, nil)
			r.Header.Set("X-Request-Id", "request-one")
			w, events := f.request(t, r)
			if w.Code != 404 {
				t.Fatalf("bare route=%d", w.Code)
			}
			assertTrail(t, events, r, 404)
			after, err := st.ListTokens(user.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("bare route changed tokens: %#v %#v %v", before, after, err)
			}
			r = tokenRequest("/tokens/"+token.ID+"/"+action, session.ID, nil)
			r.Header.Set("X-Request-Id", "request-one")
			w, events = f.request(t, r)
			if w.Code != 302 {
				t.Fatalf("prefixed route=%d", w.Code)
			}
			event := "token." + action + "d"
			if action == "delete" {
				event = "token.deleted"
			}
			assertTrail(t, events, r, 302, event)
			after, err = st.ListTokens(user.ID)
			if err != nil {
				t.Fatal(err)
			}
			if action == "delete" {
				if len(after) != 0 {
					t.Fatalf("delete left %#v", after)
				}
			} else if len(after) != 1 || after[0].Enabled != (action == "enable") {
				t.Fatalf("toggle state=%#v", after)
			}
		})
	}
}

func TestDomainFailuresRecordOnlyRequiredEvents(t *testing.T) {
	// R-9SUP-LXP1 R-THGQ-JQ7P R-TUVM-R7DC: rejected operations and store failures do not masquerade as state changes.
	st := openSignInStore(t)
	user, _, err := st.UpsertUserOnLogin("issuer", "owner", "owner@green.example", signInNow)
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(user.ID, signInNow)
	if err != nil {
		t.Fatal(err)
	}
	f := newTrail(t, Config{Store: st}, nil)
	for _, tc := range []struct {
		target string
		form   url.Values
		status int
	}{{"/tokens", url.Values{"name": {""}, "expires": {"bad"}}, 400}, {"/tokens/unknown/delete", nil, 404}, {"/tokens/unknown/enable", nil, 404}} {
		r := tokenRequest(tc.target, session.ID, tc.form)
		r.Header.Set("X-Request-Id", "request-one")
		w, events := f.request(t, r)
		if w.Code != tc.status {
			t.Fatalf("rejected %s=%d", tc.target, w.Code)
		}
		assertTrail(t, events, r, w.Code)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, target string }{{"GET", "/"}, {"GET", "/login/google"}, {"GET", "/login/google/callback?state=unknown"}, {"POST", "/logout"}, {"POST", "/tokens"}, {"POST", "/tokens/unknown/delete"}, {"GET", "/me"}} {
		r := trailRequest(tc.method, tc.target)
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		r.Header.Set("Origin", "https://auth.green.example")
		w, events := f.request(t, r)
		if w.Code != 500 {
			t.Fatalf("failed %s=%d", tc.target, w.Code)
		}
		assertTrail(t, events, r, w.Code)
	}
}

func TestRequestTrailPanicStatus(t *testing.T) {
	// R-3HP0-MB1R R-2HW5-XKDN: a panic before writing a response finishes the recorded request with status 500.
	st := openSignInStore(t)
	user, _, err := st.UpsertUserOnLogin("issuer", "panic-user", "panic@green.example", signInNow)
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(user.ID, signInNow)
	if err != nil {
		t.Fatal(err)
	}
	f := newTrail(t, Config{Store: st, Banner: func(page.User) page.Banner { panic("banner failed") }}, nil)
	r := trailRequest("GET", "/")
	r.AddCookie(cookieForHost(r.Host, session.ID, false))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected banner panic")
			}
		}()
		f.server.ServeHTTP(httptest.NewRecorder(), r)
	}()
	assertTrail(t, f.events(t), r, 500)
}

func TestOriginalMetadataPreservesCredentialStateChanges(t *testing.T) {
	// R-TR7X-LW59: each equivalent fresh credential must be touched at the advanced clock with or without original-request metadata.
	advanced := signInNow.Add(10 * time.Minute)
	variants := []map[string]string{
		nil,
		{"X-Original-Method": "DELETE"},
		{"X-Original-Host": "untrusted"},
		{"X-Original-URI": "/different?private"},
		{"X-Original-Method": "PATCH", "X-Original-Host": "", "X-Original-URI": "?private"},
	}
	var baselineTokens []store.Token
	var baselineSessions []signInSessionRow
	for i, headers := range variants {
		st := openSignInStore(t)
		user, _, err := st.UpsertUserOnLogin("issuer", "metadata-owner", "owner@green.example", signInNow)
		if err != nil {
			t.Fatal(err)
		}
		session, err := st.CreateSession(user.ID, signInNow)
		if err != nil {
			t.Fatal(err)
		}
		token, secret, err := st.CreateToken(user.ID, "metadata-token", store.Expiry90d, signInNow)
		if err != nil {
			t.Fatal(err)
		}
		f := newTrail(t, Config{Store: st, Now: func() time.Time { return advanced }}, nil)
		for _, credential := range []string{"token", "session"} {
			r := trailRequest("GET", "/check")
			if credential == "token" {
				r.Header.Set("Authorization", "Bearer "+secret)
			} else {
				r.AddCookie(cookieForHost(r.Host, session.ID, false))
			}
			for name, value := range headers {
				r.Header.Set(name, value)
			}
			w, _ := f.request(t, r)
			if w.Code != 200 || w.Header().Get(HeaderUserID) != user.ID || w.Header().Get(HeaderUserEmail) != user.Email {
				t.Fatalf("variant %d %s answer=%d %#v", i, credential, w.Code, w.Header())
			}
		}
		rows, err := st.ListTokens(user.ID)
		if err != nil || len(rows) != 1 {
			t.Fatalf("variant %d tokens=%#v %v", i, rows, err)
		}
		expected := token
		expected.LastUsedAt = &advanced
		if !reflect.DeepEqual(rows[0], expected) {
			t.Fatalf("variant %d token state=%#v want %#v", i, rows[0], expected)
		}
		// The public session lookup proves that its idle window moved with the touch.
		identity, err := st.LookupSessionIdentity(session.ID, advanced.Add(14*time.Minute))
		if err != nil || identity.UserID != user.ID {
			t.Fatalf("variant %d touched session not live: %#v %v", i, identity, err)
		}
		if _, err := st.LookupSessionIdentity(session.ID, advanced.Add(15*time.Minute+time.Nanosecond)); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("variant %d idle window changed: %v", i, err)
		}
		sessions := signInSessionRows(t, st)
		if len(sessions) != 1 || sessions[0].lastUsed != advanced.UnixNano() {
			t.Fatalf("variant %d session state=%#v", i, sessions)
		}
		if i == 0 {
			baselineTokens = rows
			baselineSessions = sessions
		} else if !reflect.DeepEqual(rows, baselineTokens) || !reflect.DeepEqual(sessions, baselineSessions) {
			t.Fatalf("variant %d stored state differs: tokens=%#v sessions=%#v", i, rows, sessions)
		}
	}
}
