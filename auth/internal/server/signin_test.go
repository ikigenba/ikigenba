package server

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	googleclient "github.com/ikigenba/ikigenba/auth/internal/google"
	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
	"github.com/ikigenba/ikigenba/auth/internal/store"
	_ "modernc.org/sqlite"
)

var signInNow = time.Date(2026, 9, 20, 16, 0, 0, 0, time.UTC)

type signInRand struct{ next byte }

// signInKeyRand is a fixed byte source so the issuer RSA key is reproducible
// without math/rand.
type signInKeyRand struct{}

func (signInKeyRand) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 42
	}
	return len(p), nil
}

func (r *signInRand) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.next
		r.next++
	}
	return len(p), nil
}

func openSignInStore(t *testing.T) *store.Store {
	t.Helper()
	st, path, err := openSignInStoreAt(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	signInStorePath[st] = path
	t.Cleanup(func() { delete(signInStorePath, st) })
	return st
}

var signInStorePath = map[*store.Store]string{}

func openSignInStoreAt(t *testing.T) (*store.Store, string, error) {
	t.Helper()
	path := t.TempDir() + "/auth.db"
	st, err := store.Open(path, &signInRand{next: 1})
	return st, path, err
}

type signInIssuer struct {
	t      *testing.T
	server *httptest.Server
	key    *rsa.PrivateKey
	mu     sync.Mutex
	tokens map[string]string
	forms  []url.Values
}

func newSignInIssuer(t *testing.T) *signInIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(signInKeyRand{}, 2048)
	if err != nil {
		t.Fatalf("generate deterministic issuer key: %v", err)
	}
	f := &signInIssuer{t: t, key: key, tokens: make(map[string]string)}
	f.server = httptest.NewUnstartedServer(http.HandlerFunc(f.serveHTTP))
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.server.Listener = listener
	f.server.Start()
	t.Cleanup(f.server.Close)
	return f
}

func (f *signInIssuer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		f.writeJSON(w, map[string]any{
			"issuer": f.server.URL, "authorization_endpoint": f.server.URL + "/authorize",
			"token_endpoint": f.server.URL + "/token", "jwks_uri": f.server.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	case "/jwks":
		f.writeJSON(w, map[string]any{"keys": []any{map[string]any{
			"kty": "RSA", "kid": "signin-key", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
		}}})
	case "/token":
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.forms = append(f.forms, r.PostForm)
		token, ok := f.tokens[r.PostForm.Get("code")]
		f.mu.Unlock()
		if !ok {
			http.Error(w, "exchange rejected", http.StatusBadRequest)
			return
		}
		f.writeJSON(w, map[string]any{"access_token": "access", "token_type": "Bearer", "id_token": token})
	default:
		http.NotFound(w, r)
	}
}

func (f *signInIssuer) writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		f.t.Errorf("encode fake response: %v", err)
	}
}

func (f *signInIssuer) issue(code, subject, email, domain string, verified bool) {
	f.t.Helper()
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": "signin-key", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"iss": "https://accounts.google.com", "sub": subject, "aud": "client-id",
		"exp": 4102444800, "iat": 1700000000, "email": email,
		"email_verified": verified, "hd": domain,
	})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(nil, f.key, crypto.SHA256, digest[:])
	if err != nil {
		f.t.Fatal(err)
	}
	f.mu.Lock()
	f.tokens[code] = unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	f.mu.Unlock()
}

func (f *signInIssuer) lastForm() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.forms) == 0 {
		f.t.Fatal("no token request")
	}
	return f.forms[len(f.forms)-1]
}

func signInServer(t *testing.T, st *store.Store, issuer *signInIssuer, now func() time.Time) *Server {
	t.Helper()
	gc := googleclient.NewClient("client-id", "client-secret", "green.example", issuer.server.URL)
	return New(Config{
		Store:           st,
		Google:          gc,
		Now:             now,
		Rand:            &signInRand{next: 1},
		Stderr:          io.Discard,
		WorkspaceDomain: "green.example",
	})
}

func serveSignIn(s *Server, method, target, host string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, nil)
	req.Host = host
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(w, req)
	return w
}

func TestSignInConstantsHostRulesAndAnonymousRoot(t *testing.T) {
	// R-ICXY-ERNZ: the shared browser session cookie name is exported exactly.
	if SessionCookieName != "ikigenba_session" {
		t.Fatalf("SessionCookieName = %q", SessionCookieName)
	}
	// R-ILH9-35UU, R-IMP5-GXLJ, R-INX1-UPC8: space and local host derivations are exact.
	if space("auth.green.example") != "green.example" || redirectURI("auth.green.example") != "https://auth.green.example/login/google/callback" || ownOrigin("auth.green.example") != "https://auth.green.example" {
		t.Fatal("space host derivation is incorrect")
	}
	if redirectURI("localhost:3001") != "http://localhost:3001/login/google/callback" || ownOrigin("localhost:3001") != "http://127.0.0.1:3001" {
		t.Fatal("local host derivation is incorrect")
	}
	// R-IQCU-M8TM: only the exact space and its label-boundary subdomains pass.
	for raw, want := range map[string]bool{
		"https://green.example/path": true, "https://app.green.example/path": true,
		"https://evilgreen.example/path": false, "https://green.example.evil/path": false,
	} {
		if got := inSpace(raw, "auth.green.example"); got != want {
			t.Errorf("inSpace(%q) = %t", raw, got)
		}
	}

	st := openSignInStore(t)
	s := New(Config{Store: st, Now: func() time.Time { return signInNow }})
	w := serveSignIn(s, http.MethodGet, "/?return=https%3A%2F%2Fapp.green.example%2Fwork", "auth.green.example", nil, "")
	// R-IRKR-00KB and R-J67J-L9GN: auth's own anonymous root is HTML and carries return only in the link.
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != signInHTMLContentType || !strings.Contains(w.Body.String(), `href="/login/google?return=`) {
		t.Fatalf("anonymous root = %d %q %s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	states, err := st.ConsumeLoginState("https://app.green.example/work")
	if err == nil || states.State != "" {
		t.Fatal("anonymous root persisted return URL")
	}
}

func TestLoginStartMintsVerifierFromRandAndRedirects(t *testing.T) {
	issuer := newSignInIssuer(t)
	// The store has its own reader. The server's reader is distinct, so the
	// recorded verifier can only come from the bytes that reader yields.
	st, err := store.Open(t.TempDir()+"/auth.db", &signInRand{next: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	gc := googleclient.NewClient("client-id", "client-secret", "green.example", issuer.server.URL)
	verifierBytes := bytes.Repeat([]byte{0x2a}, pkceVerifierBytes)
	serverRand := bytes.NewReader(append([]byte(nil), verifierBytes...))
	s := New(Config{
		Store:           st,
		Google:          gc,
		Now:             func() time.Time { return signInNow },
		Rand:            serverRand,
		Stderr:          io.Discard,
		WorkspaceDomain: "green.example",
	})

	returnURL := "https://app.green.example/after"
	w := serveSignIn(s, http.MethodGet, "/login/google?return="+url.QueryEscape(returnURL), "auth.green.example", nil, "")
	// R-KY4E-8B7G: a successful start records the minted verifier and return
	// URL, then redirects to AuthCodeURL for that recorded state.
	if w.Code != http.StatusFound || len(w.Result().Cookies()) != 0 {
		t.Fatalf("login start = %d cookies %#v", w.Code, w.Result().Cookies())
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	stateValue := location.Query().Get("state")
	recorded, err := st.ConsumeLoginState(stateValue)
	if err != nil {
		t.Fatalf("login state named by Location was not recorded: %v", err)
	}
	wantVerifier := idcodec.Encode(verifierBytes)
	if recorded.Verifier != wantVerifier || recorded.ReturnURL != returnURL {
		t.Fatalf("recorded login state = %#v, want verifier %s return %s", recorded, wantVerifier, returnURL)
	}
	if location.Query().Get("redirect_uri") != redirectURI("auth.green.example") || location.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization URL = %s", location)
	}
	wantChallenge := sha256.Sum256([]byte(wantVerifier))
	if location.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(wantChallenge[:]) {
		t.Fatalf("code_challenge = %q", location.Query().Get("code_challenge"))
	}
	if serverRand.Len() != 0 {
		t.Fatalf("server Rand unread bytes = %d, want 0", serverRand.Len())
	}

	// R-UR0L-ZVDJ: the minted verifier is a function of cfg.Rand, not of the
	// store's reader and not of a global source.
	otherBytes := bytes.Repeat([]byte{0x5c}, pkceVerifierBytes)
	secondStore, err := store.Open(t.TempDir()+"/auth.db", &signInRand{next: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondStore.Close() })
	second := New(Config{
		Store:           secondStore,
		Google:          gc,
		Now:             func() time.Time { return signInNow },
		Rand:            bytes.NewReader(otherBytes),
		Stderr:          io.Discard,
		WorkspaceDomain: "green.example",
	})
	againResponse := serveSignIn(second, http.MethodGet, "/login/google", "auth.green.example", nil, "")
	againLocation, err := url.Parse(againResponse.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	againState, err := secondStore.ConsumeLoginState(againLocation.Query().Get("state"))
	wantOther := idcodec.Encode(otherBytes)
	if err != nil || againState.Verifier != wantOther || againState.Verifier == recorded.Verifier {
		t.Fatalf("different Rand did not mint a different verifier: %#v %v, want %s", againState, err, wantOther)
	}
}

func TestLoginStartDiscoveryFailureRemovesStateAndWritesDiagnostic(t *testing.T) {
	st := openSignInStore(t)
	gc := googleclient.NewClient("client-id", "client-secret", "green.example", "http://127.0.0.1:1")
	var stderr bytes.Buffer
	s := New(Config{
		Store:           st,
		Google:          gc,
		Now:             func() time.Time { return signInNow },
		Rand:            &signInRand{next: 7},
		Stderr:          &stderr,
		WorkspaceDomain: "green.example",
	})

	w := serveSignIn(s, http.MethodGet, "/login/google?return=https%3A%2F%2Fapp.green.example%2Fafter", "auth.green.example", nil, "")
	// R-L0K6-ZUOU: an AuthCodeURL error is a single-line 502, a diagnostic on
	// cfg.Stderr, and no user, session, cookie, or leftover login state.
	if w.Code != http.StatusBadGateway || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || strings.Count(w.Body.String(), "\n") != 1 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("discovery failure = %d %#v %q", w.Code, w.Header(), w.Body.String())
	}
	if stderr.Len() == 0 || !strings.Contains(stderr.String(), "discover") {
		t.Fatalf("stderr diagnostic = %q", stderr.String())
	}
	assertEmptySignInTables(t, st)
}

func TestCallbackAccessDeniedPrecedesStateValidation(t *testing.T) {
	issuer := newSignInIssuer(t)
	st := openSignInStore(t)
	s := signInServer(t, st, issuer, func() time.Time { return signInNow })
	state, err := st.CreateLoginState("verifier", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, stateValue := range []string{state.State, "unknown", ""} {
		w := serveSignIn(s, http.MethodGet, "/login/google/callback?error=access_denied&state="+url.QueryEscape(stateValue), "auth.green.example", nil, "")
		// R-G1Y2-IOUB: denial wins over absent/unknown state, returns sign-in HTML, and sets no cookie.
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != signInHTMLContentType || !strings.Contains(w.Body.String(), `href="/login/google"`) || len(w.Result().Cookies()) != 0 {
			t.Fatalf("access_denied(%q) = %d %q %s", stateValue, w.Code, w.Header().Get("Content-Type"), w.Body.String())
		}
	}
	if _, err := st.ConsumeLoginState(state.State); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("access_denied state was not consumed: %v", err)
	}
}

func TestCallbackRejectsMissingUnknownAndExchangeFailure(t *testing.T) {
	issuer := newSignInIssuer(t)
	st := openSignInStore(t)
	s := signInServer(t, st, issuer, func() time.Time { return signInNow })
	for _, target := range []string{"/login/google/callback", "/login/google/callback?state=unknown"} {
		w := serveSignIn(s, http.MethodGet, target, "auth.green.example", nil, "")
		// R-IV8G-5BSE: missing and unknown state are indistinguishable 400 single-line responses without cookies.
		if w.Code != http.StatusBadRequest || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || strings.Count(w.Body.String(), "\n") != 1 || len(w.Result().Cookies()) != 0 {
			t.Fatalf("invalid callback = %d %#v %q", w.Code, w.Header(), w.Body.String())
		}
	}
	state, err := st.CreateLoginState("verifier", "")
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	s = New(Config{
		Store:           st,
		Google:          googleclient.NewClient("client-id", "client-secret", "green.example", issuer.server.URL),
		Now:             func() time.Time { return signInNow },
		Rand:            &signInRand{next: 1},
		Stderr:          &stderr,
		WorkspaceDomain: "green.example",
	})
	w := serveSignIn(s, http.MethodGet, "/login/google/callback?state="+state.State+"&code=rejected", "auth.green.example", nil, "")
	// R-J2JU-FY8K: a failed exchange is a single-line 502, the underlying error
	// on cfg.Stderr, and no user, session, or cookie.
	if w.Code != http.StatusBadGateway || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || strings.Count(w.Body.String(), "\n") != 1 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("exchange failure = %d %#v %q", w.Code, w.Header(), w.Body.String())
	}
	if !strings.Contains(stderr.String(), "exchange rejected") {
		t.Fatalf("stderr diagnostic = %q", stderr.String())
	}
	// R-UR0L-ZVDJ: the exchange diagnostic is a function of the writer given to
	// New, not of a global stream or a writer assigned after construction.
	otherState, err := st.CreateLoginState("verifier", "")
	if err != nil {
		t.Fatal(err)
	}
	var otherStderr bytes.Buffer
	other := New(Config{
		Store:           st,
		Google:          googleclient.NewClient("client-id", "client-secret", "green.example", issuer.server.URL),
		Now:             func() time.Time { return signInNow },
		Rand:            &signInRand{next: 1},
		Stderr:          &otherStderr,
		WorkspaceDomain: "green.example",
	})
	otherResponse := serveSignIn(other, http.MethodGet, "/login/google/callback?state="+otherState.State+"&code=rejected", "auth.green.example", nil, "")
	if otherResponse.Code != http.StatusBadGateway {
		t.Fatalf("second exchange failure = %d", otherResponse.Code)
	}
	if !strings.Contains(otherStderr.String(), "exchange rejected") || strings.Count(stderr.String(), "exchange rejected") != 1 {
		t.Fatalf("diagnostics leaked across writers: first %q second %q", stderr.String(), otherStderr.String())
	}
	assertEmptySignInTables(t, st)
	if user, err := st.UpsertUserOnLogin("https://accounts.google.com", "subject-rejected", "rejected@green.example", signInNow); err != nil || user.Email != "rejected@green.example" {
		t.Fatalf("probing for a created user failed: %#v %v", user, err)
	}
}

func TestMemberCallbackCreatesIdentitySessionCookieAndSafeRedirect(t *testing.T) {
	for _, tc := range []struct {
		name, host, returnURL, wantLocation, wantDomain string
	}{
		{name: "space exact", host: "auth.green.example", returnURL: "https://green.example/after", wantLocation: "https://green.example/after", wantDomain: "green.example"},
		{name: "space subdomain", host: "auth.green.example", returnURL: "https://app.green.example/after", wantLocation: "https://app.green.example/after", wantDomain: "green.example"},
		{name: "deceptive", host: "auth.green.example", returnURL: "https://evilgreen.example/after", wantLocation: "/", wantDomain: "green.example"},
		{name: "local", host: "localhost:3001", returnURL: "", wantLocation: "/", wantDomain: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issuer := newSignInIssuer(t)
			issuer.issue("member-code", "subject-1", "fresh@green.example", "green.example", true)
			st := openSignInStore(t)
			s := signInServer(t, st, issuer, func() time.Time { return signInNow })
			state, err := st.CreateLoginState("pkce-verifier", tc.returnURL)
			if err != nil {
				t.Fatal(err)
			}
			w := serveSignIn(s, http.MethodGet, "/login/google/callback?state="+state.State+"&code=member-code", tc.host, nil, "")
			// R-IXO8-WV9S, R-J041-OER6: a member callback exchanges, provisions, creates a session, and applies exact in-space redirect rules.
			if w.Code != http.StatusFound || w.Header().Get("Location") != tc.wantLocation {
				t.Fatalf("callback = %d location %q", w.Code, w.Header().Get("Location"))
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies = %#v", cookies)
			}
			cookie := cookies[0]
			// R-IJ1G-BMDG: login cookie has its opaque session id and exact security/domain attributes.
			if cookie.Name != SessionCookieName || cookie.Value == "" || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Domain != tc.wantDomain {
				t.Fatalf("cookie = %#v", cookie)
			}
			identity, err := st.LookupSessionIdentity(cookie.Value, signInNow)
			if err != nil || identity.Email != "fresh@green.example" {
				t.Fatalf("session identity = %#v, %v", identity, err)
			}
			// R-IYW5-AN0H: exact verified issuer/subject key returns the callback-created user id.
			user, err := st.UpsertUserOnLogin("https://accounts.google.com", "subject-1", "refreshed@green.example", signInNow.Add(time.Minute))
			if err != nil || user.ID != identity.UserID {
				t.Fatalf("verified identity was not used exactly: %#v %v", user, err)
			}
			if form := issuer.lastForm(); form.Get("code") != "member-code" || form.Get("code_verifier") != "pkce-verifier" || form.Get("redirect_uri") != redirectURI(tc.host) {
				t.Fatalf("exchange form = %v", form)
			}
		})
	}
}

func TestNonMemberCallbackConsumesStateWithoutCookie(t *testing.T) {
	for _, tc := range []struct {
		name, domain string
		verified     bool
	}{{name: "wrong domain", domain: "other.example", verified: true}, {name: "missing domain", verified: true}, {name: "unverified", domain: "green.example", verified: false}} {
		t.Run(tc.name, func(t *testing.T) {
			issuer := newSignInIssuer(t)
			issuer.issue("nonmember", "subject", "person@example.test", tc.domain, tc.verified)
			st := openSignInStore(t)
			s := signInServer(t, st, issuer, func() time.Time { return signInNow })
			state, err := st.CreateLoginState("verifier", "")
			if err != nil {
				t.Fatal(err)
			}
			w := serveSignIn(s, http.MethodGet, "/login/google/callback?state="+state.State+"&code=nonmember", "auth.green.example", nil, "")
			// R-J1BY-26HV: membership requires both matching hd and verified email; rejection consumes state and sends no cookie.
			if w.Code != http.StatusForbidden || w.Header().Get("Content-Type") != signInHTMLContentType || len(w.Result().Cookies()) != 0 {
				t.Fatalf("nonmember response = %d %#v", w.Code, w.Header())
			}
			if _, err := st.ConsumeLoginState(state.State); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("state not consumed: %v", err)
			}
		})
	}
}

func TestProfileUsesLookupIgnoresReturnAndRendersFormsAndTokens(t *testing.T) {
	st := openSignInStore(t)
	user, err := st.UpsertUserOnLogin("issuer", "subject", "member@green.example", signInNow)
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(user.ID, signInNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateToken(user.ID, "profile token", store.ExpiryNever, signInNow); err != nil {
		t.Fatal(err)
	}
	s := New(Config{Store: st, Now: func() time.Time { return signInNow.Add(10 * time.Minute) }})
	w := serveSignIn(s, http.MethodGet, "/?return=https%3A%2F%2Fevil.example", "auth.green.example", &http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}, "")
	// R-ISSN-DSB0: profile lookup is read-only, ignores return, embeds logout/create forms, and integrates D07 token rows.
	body := w.Body.String()
	for _, fragment := range []string{`<form method="post" action="/logout">`, `<form method="post" action="/tokens">`, `name="name"`, `name="expires"`, "profile token"} {
		if !strings.Contains(body, fragment) {
			t.Errorf("profile missing %q: %s", fragment, body)
		}
	}
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != signInHTMLContentType || strings.Contains(body, "evil.example") {
		t.Fatalf("profile response = %d %q %s", w.Code, w.Header().Get("Content-Type"), body)
	}
	if _, err := st.LookupSessionIdentity(session.ID, signInNow.Add(16*time.Minute)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("profile touched session; later lookup error = %v", err)
	}
}

func TestLogoutOriginDeletionAndCookieAttributes(t *testing.T) {
	for _, tc := range []struct{ host, origin, domain string }{
		{host: "auth.green.example", origin: "https://auth.green.example", domain: "green.example"},
		{host: "localhost:3001", origin: "http://127.0.0.1:3001"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			st := openSignInStore(t)
			user, err := st.UpsertUserOnLogin("issuer", "subject", "member@example.test", signInNow)
			if err != nil {
				t.Fatal(err)
			}
			session, err := st.CreateSession(user.ID, signInNow)
			if err != nil {
				t.Fatal(err)
			}
			token, _, err := st.CreateToken(user.ID, "keep", store.ExpiryNever, signInNow)
			if err != nil {
				t.Fatal(err)
			}
			s := New(Config{Store: st, Now: func() time.Time { return signInNow }})
			cookie := &http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}

			bad := serveSignIn(s, http.MethodPost, "/logout", tc.host, cookie, "https://attacker.example")
			// R-J4ZN-7HPY: wrong Origin is a cookie-free 403 and leaves the session live.
			if bad.Code != http.StatusForbidden || bad.Header().Get("Content-Type") != "text/plain; charset=utf-8" || len(bad.Result().Cookies()) != 0 {
				t.Fatalf("bad-origin response = %d %#v", bad.Code, bad.Header())
			}
			if _, err := st.LookupSessionIdentity(session.ID, signInNow); err != nil {
				t.Fatalf("bad origin deleted session: %v", err)
			}

			good := serveSignIn(s, http.MethodPost, "/logout", tc.host, cookie, tc.origin)
			// R-J3RQ-TPZ9: accepted logout redirects, deletes only the session, and preserves user/token.
			if good.Code != http.StatusFound || good.Header().Get("Location") != "/" || !errors.Is(lookupSessionError(st, session.ID), store.ErrNotFound) {
				t.Fatalf("accepted logout = %d %q session=%v", good.Code, good.Header().Get("Location"), lookupSessionError(st, session.ID))
			}
			if tokens, err := st.ListTokens(user.ID); err != nil || len(tokens) != 1 || tokens[0].ID != token.ID {
				t.Fatalf("logout changed token/user: %#v %v", tokens, err)
			}
			cleared := good.Result().Cookies()[0]
			// R-IK9C-PE45: deletion cookie matches login security/domain attributes and serializes Max-Age=0.
			if cleared.Name != SessionCookieName || cleared.Value != "" || cleared.MaxAge != -1 || !cleared.Secure || !cleared.HttpOnly || cleared.SameSite != http.SameSiteLaxMode || cleared.Domain != tc.domain || !strings.Contains(good.Header().Get("Set-Cookie"), "Max-Age=0") {
				t.Fatalf("cleared cookie = %#v header=%q", cleared, good.Header().Get("Set-Cookie"))
			}
		})
	}
}

func lookupSessionError(st *store.Store, sessionID string) error {
	_, err := st.LookupSessionIdentity(sessionID, signInNow)
	return err
}

func assertEmptySignInTables(t *testing.T, st *store.Store) {
	t.Helper()
	path, ok := signInStorePath[st]
	if !ok {
		t.Fatal("sign-in store path was not recorded")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, table := range []string{"users", "sessions", "login_states"} {
		var n int
		if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%s has %d rows, want 0", table, n)
		}
	}
}
