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
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
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
	f.issueClaims(code, map[string]any{
		"iss": "https://accounts.google.com", "sub": subject, "aud": "client-id",
		"exp": 4102444800, "iat": 1700000000, "email": email,
		"email_verified": verified, "hd": domain,
	})
}

func (f *signInIssuer) issueClaims(code string, claims map[string]any) {
	f.t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "RS256", "kid": "signin-key", "typ": "JWT"})
	if err != nil {
		f.t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		f.t.Fatal(err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
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

func (f *signInIssuer) formCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.forms)
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
	// R-J67J-L9GN: auth's own anonymous root is HTML and carries return only in the link.
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != signInHTMLContentType || !strings.Contains(w.Body.String(), `href="/login/google?return=`) {
		t.Fatalf("anonymous root = %d %q %s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	states, err := st.ConsumeLoginState("https://app.green.example/work")
	if err == nil || states.State != "" {
		t.Fatal("anonymous root persisted return URL")
	}
}

func TestAnonymousRootCarriesReturnOnlyInTheLoginLink(t *testing.T) {
	// R-IRKR-00KB: an anonymous GET / is the HTML sign-in page. Its link target
	// is /login/google, and a return query is carried only on that link.
	returnURL := `https://evil.example/steal?x=1&y=2"`
	issuer := newSignInIssuer(t)
	for _, host := range []string{"auth.green.example", "localhost:3001"} {
		t.Run(host, func(t *testing.T) {
			st := openSignInStore(t)
			s := signInServer(t, st, issuer, func() time.Time { return signInNow })

			bare := serveSignIn(s, http.MethodGet, "/", host, nil, "")
			assertHTMLStatus(t, bare, http.StatusOK)
			assertNoSetCookie(t, bare)
			if !hasExactSignInLink(bare.Body.String(), "/login/google") || strings.Contains(bare.Body.String(), "return=") {
				t.Fatalf("bare root links = %#v body %s", signInLinkHrefs(bare.Body.String()), bare.Body.String())
			}
			assertEmptySignInTables(t, st)

			bogus := &http.Cookie{Name: SessionCookieName, Value: "not-a-session", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
			carried := serveSignIn(s, http.MethodGet, "/?return="+url.QueryEscape(returnURL), host, bogus, "")
			assertHTMLStatus(t, carried, http.StatusOK)
			assertNoSetCookie(t, carried)
			if strings.Contains(carried.Body.String(), `action="/logout"`) {
				t.Fatalf("unknown cookie rendered the profile: %s", carried.Body.String())
			}
			href := requireLoginReturnLink(t, carried.Body.String(), returnURL)
			assertEmptySignInTables(t, st)
			assertReturnNotPersisted(t, st, returnURL)

			user, err := st.UpsertUserOnLogin("https://accounts.google.com", "root-subject", "root@green.example", signInNow)
			if err != nil {
				t.Fatal(err)
			}
			session, err := st.CreateSession(user.ID, signInNow)
			if err != nil {
				t.Fatal(err)
			}
			before := formatSignInIdentity(t, st)
			deadAt := signInNow.Add(store.SessionIdle + time.Nanosecond)
			dead := New(Config{Store: st, Now: func() time.Time { return deadAt }})
			deadCookie := &http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
			deadPage := serveSignIn(dead, http.MethodGet, "/?return="+url.QueryEscape(returnURL), host, deadCookie, "")
			assertHTMLStatus(t, deadPage, http.StatusOK)
			assertNoSetCookie(t, deadPage)
			if strings.Contains(deadPage.Body.String(), `action="/logout"`) {
				t.Fatalf("dead session rendered the profile: %s", deadPage.Body.String())
			}
			if got := requireLoginReturnLink(t, deadPage.Body.String(), returnURL); got != href {
				t.Fatalf("dead-session link = %q, want %q", got, href)
			}
			if formatSignInIdentity(t, st) != before {
				t.Fatalf("dead-session root changed identity rows:\n%s", formatSignInIdentity(t, st))
			}
			assertReturnNotPersisted(t, st, returnURL)

			started := serveSignIn(s, http.MethodGet, href, host, nil, "")
			if started.Code != http.StatusFound {
				t.Fatalf("login start = %d %s", started.Code, started.Body.String())
			}
			location, err := url.Parse(started.Header().Get("Location"))
			if err != nil || location.Query().Get("state") == "" {
				t.Fatalf("login start location %q: %v", started.Header().Get("Location"), err)
			}
			recorded, err := st.ConsumeLoginState(location.Query().Get("state"))
			if err != nil || recorded.ReturnURL != returnURL {
				t.Fatalf("login start recorded %#v %v, want return %s", recorded, err, returnURL)
			}
			if formatSignInIdentity(t, st) != before {
				t.Fatalf("carrying the return persisted it on a user or session:\n%s", formatSignInIdentity(t, st))
			}
		})
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
	// R-G1Y2-IOUB: access_denied is a cookie-free sign-in page, consumes only
	// the named login state, and does not create a user or session. It wins
	// over the unknown-state 400.
	issuer := newSignInIssuer(t)
	issuer.issue("denied-code", "subject-denied", "denied@green.example", "green.example", true)
	st := openSignInStore(t)
	s := signInServer(t, st, issuer, func() time.Time { return signInNow })
	user, err := st.UpsertUserOnLogin("https://accounts.google.com", "subject-denied", "before@green.example", signInNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSession(user.ID, signInNow); err != nil {
		t.Fatal(err)
	}
	kept, err := st.CreateLoginState("kept-verifier", "https://app.green.example/kept")
	if err != nil {
		t.Fatal(err)
	}
	named, err := st.CreateLoginState("named-verifier", "https://app.green.example/should-not-leak")
	if err != nil {
		t.Fatal(err)
	}
	before := formatSignInIdentity(t, st)

	unknown := serveSignIn(s, http.MethodGet, "/login/google/callback?state=unknown&code=denied-code", "auth.green.example", nil, "")
	if unknown.Code != http.StatusBadRequest || unknown.Header().Get("Content-Type") != "text/plain; charset=utf-8" || strings.Count(unknown.Body.String(), "\n") != 1 {
		t.Fatalf("unknown state without access_denied = %d %q %q", unknown.Code, unknown.Header().Get("Content-Type"), unknown.Body.String())
	}
	assertNoSetCookie(t, unknown)
	requireLoginState(t, st, named.State, "named-verifier", "https://app.green.example/should-not-leak")
	requireLoginState(t, st, kept.State, "kept-verifier", "https://app.green.example/kept")
	if issuer.formCount() != 0 || formatSignInIdentity(t, st) != before {
		t.Fatalf("unknown state exchanged or changed identity: forms=%d\n%s", issuer.formCount(), formatSignInIdentity(t, st))
	}

	targets := []string{
		"/login/google/callback?error=access_denied&state=" + url.QueryEscape(named.State) + "&code=denied-code",
		"/login/google/callback?error=access_denied&state=unknown&code=denied-code",
		"/login/google/callback?error=access_denied&code=denied-code",
	}
	for i, target := range targets {
		w := serveSignIn(s, http.MethodGet, target, "auth.green.example", nil, "")
		assertHTMLStatus(t, w, http.StatusOK)
		assertNoSetCookie(t, w)
		if !hasExactSignInLink(w.Body.String(), "/login/google") || strings.Contains(w.Body.String(), "should-not-leak") || strings.Contains(w.Body.String(), "return=") {
			t.Fatalf("access_denied links = %#v body %s", signInLinkHrefs(w.Body.String()), w.Body.String())
		}
		if formatSignInIdentity(t, st) != before || issuer.formCount() != 0 {
			t.Fatalf("access_denied created a user, session, or exchange: forms=%d\n%s", issuer.formCount(), formatSignInIdentity(t, st))
		}
		requireLoginState(t, st, kept.State, "kept-verifier", "https://app.green.example/kept")
		_, err := st.ConsumeLoginState(named.State)
		if i == 0 && !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("named login state was not consumed: %v", err)
		}
		if i > 0 && !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("later access_denied revived named state: %v", err)
		}
	}
	keptRow, err := st.ConsumeLoginState(kept.State)
	if err != nil || keptRow.Verifier != "kept-verifier" || keptRow.ReturnURL != "https://app.green.example/kept" {
		t.Fatalf("unrelated login state = %#v %v", keptRow, err)
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
			// R-J041-OER6: a member callback exchanges, provisions, creates a session, and applies exact in-space redirect rules.
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
			if _, err := st.ConsumeLoginState(state.State); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("member callback left login state %s: %v", state.State, err)
			}
		})
	}
}

func TestMemberCallbackExchangesUpsertsCreatesSessionAndConsumesState(t *testing.T) {
	// R-IXO8-WV9S: a member callback exchanges the recorded verifier, upserts
	// the verified identity, stores a session, sets that session cookie,
	// consumes only that login state, and responds 302.
	issuer := newSignInIssuer(t)
	const (
		code     = "ixo8-code"
		verifier = "verifier-from-state"
		subject  = "subject-member"
		email    = "member@gmail.com"
	)
	issuer.issue(code, subject, email, "green.example", true)
	st := openSignInStore(t)
	s := signInServer(t, st, issuer, func() time.Time { return signInNow })
	sibling, err := st.CreateLoginState("sibling-verifier", "https://app.green.example/stay")
	if err != nil {
		t.Fatal(err)
	}
	state, err := st.CreateLoginState(verifier, "https://evil.example/ignored")
	if err != nil {
		t.Fatal(err)
	}

	w := serveSignIn(s, http.MethodGet, "/login/google/callback?state="+state.State+"&code="+code, "auth.green.example", nil, "")
	if w.Code != http.StatusFound {
		t.Fatalf("callback = %d body %s", w.Code, w.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == SessionCookieName {
			if sessionCookie != nil {
				t.Fatalf("duplicate session cookies: %#v", w.Result().Cookies())
			}
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("session cookie = %#v header %q", sessionCookie, w.Header().Values("Set-Cookie"))
	}
	if issuer.formCount() != 1 {
		t.Fatalf("token requests = %d, want 1", issuer.formCount())
	}
	form := issuer.lastForm()
	if form.Get("code") != code || form.Get("code_verifier") != verifier || form.Get("redirect_uri") != "https://auth.green.example/login/google/callback" {
		t.Fatalf("exchange form = %v", form)
	}

	users := signInUserRows(t, st)
	if len(users) != 1 || users[0].issuer != "https://accounts.google.com" || users[0].subject != subject || users[0].email != email || users[0].last != signInNow.UnixNano() {
		t.Fatalf("users = %#v", users)
	}
	sessions := signInSessionRows(t, st)
	if len(sessions) != 1 || sessions[0].id != sessionCookie.Value || sessions[0].userID != users[0].id || sessions[0].loginAt != signInNow.UnixNano() || sessions[0].lastUsed != signInNow.UnixNano() {
		t.Fatalf("sessions = %#v cookie %s", sessions, sessionCookie.Value)
	}
	identity, err := st.LookupSessionIdentity(sessionCookie.Value, signInNow)
	if err != nil || identity.UserID != users[0].id || identity.Email != email {
		t.Fatalf("session identity = %#v %v", identity, err)
	}
	if _, err := st.ConsumeLoginState(state.State); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("login state was not consumed: %v", err)
	}
	kept, err := st.ConsumeLoginState(sibling.State)
	if err != nil || kept.Verifier != "sibling-verifier" || kept.ReturnURL != "https://app.green.example/stay" {
		t.Fatalf("sibling login state = %#v %v", kept, err)
	}
}

func TestCallbackMembershipIsHostedDomainAndVerifiedEmail(t *testing.T) {
	// R-J1BY-26HV: membership is exactly WORKSPACE_DOMAIN plus a verified
	// email. The request host derives a different domain (space(host) is
	// hostSpace), so an hd equal to that space is not a member. Anything
	// else, including an absent hd or an email whose domain is the
	// workspace, is a cookie-free 403 that consumes the login state and
	// leaves users and sessions unchanged.
	const (
		workspace   = "acme.test"
		requestHost = "auth.green.example"
		hostSpace   = "green.example"
	)
	if got := space(requestHost); got != hostSpace || got == workspace || requestHost == workspace {
		t.Fatalf("request host %q derives %q, want %q distinct from WORKSPACE_DOMAIN %q", requestHost, got, hostSpace, workspace)
	}
	base := func(email string) map[string]any {
		return map[string]any{
			"iss": "https://accounts.google.com", "sub": "subject-person", "aud": "client-id",
			"exp": 4102444800, "iat": 1700000000, "email": email,
			"email_verified": true, "hd": workspace,
		}
	}
	member := base("person@gmail.com")
	wrongDomain := base("person@acme.test")
	wrongDomain["hd"] = hostSpace
	wrongCase := base("person@gmail.com")
	wrongCase["hd"] = "Acme.test"
	emptyHD := base("person@gmail.com")
	emptyHD["hd"] = ""
	absentHD := base("person@gmail.com")
	delete(absentHD, "hd")
	unverified := base("person@gmail.com")
	unverified["email_verified"] = false
	missingVerified := base("person@gmail.com")
	delete(missingVerified, "email_verified")

	tests := []struct {
		name   string
		claims map[string]any
		member bool
	}{
		{name: "member", claims: member, member: true},
		{name: "wrong domain", claims: wrongDomain},
		{name: "domain case", claims: wrongCase},
		{name: "empty hosted domain", claims: emptyHD},
		{name: "absent hosted domain", claims: absentHD},
		{name: "unverified", claims: unverified},
		{name: "missing email_verified", claims: missingVerified},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			issuer := newSignInIssuer(t)
			issuer.issueClaims("callback-code", tc.claims)
			st := openSignInStore(t)
			gc := googleclient.NewClient("client-id", "client-secret", "not-the-workspace.example", issuer.server.URL)
			callbackNow := signInNow.Add(time.Minute)
			s := New(Config{
				Store:           st,
				Google:          gc,
				Now:             func() time.Time { return callbackNow },
				Rand:            &signInRand{next: 1},
				Stderr:          io.Discard,
				WorkspaceDomain: workspace,
			})
			sibling, err := st.CreateLoginState("sibling-verifier", "https://app.green.example/stay")
			if err != nil {
				t.Fatal(err)
			}
			var seededEmail string
			if !tc.member {
				seeded, err := st.UpsertUserOnLogin("https://accounts.google.com", "subject-person", "before@example.test", signInNow)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := st.CreateSession(seeded.ID, signInNow); err != nil {
					t.Fatal(err)
				}
				seededEmail = seeded.Email
			}
			named, err := st.CreateLoginState("pkce-verifier", "https://evil.example/nope")
			if err != nil {
				t.Fatal(err)
			}
			before := formatSignInIdentity(t, st)

			w := serveSignIn(s, http.MethodGet, "/login/google/callback?state="+named.State+"&code=callback-code", requestHost, nil, "")
			form := issuer.lastForm()
			if issuer.formCount() != 1 || form.Get("code") != "callback-code" || form.Get("code_verifier") != "pkce-verifier" || form.Get("redirect_uri") != "https://auth.green.example/login/google/callback" {
				t.Fatalf("exchange form count %d values %v", issuer.formCount(), form)
			}
			if _, err := st.ConsumeLoginState(named.State); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("login state was not consumed: %v", err)
			}
			kept, err := st.ConsumeLoginState(sibling.State)
			if err != nil || kept.Verifier != "sibling-verifier" || kept.ReturnURL != "https://app.green.example/stay" {
				t.Fatalf("sibling login state = %#v %v", kept, err)
			}

			if tc.member {
				if w.Code != http.StatusFound {
					t.Fatalf("member callback = %d %q %s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
				}
				var sessionCookie *http.Cookie
				for _, cookie := range w.Result().Cookies() {
					if cookie.Name == SessionCookieName {
						sessionCookie = cookie
					}
				}
				if sessionCookie == nil || sessionCookie.Value == "" {
					t.Fatalf("member cookie = %#v", w.Header().Values("Set-Cookie"))
				}
				users := signInUserRows(t, st)
				tokenEmail, ok := tc.claims["email"].(string)
				if !ok {
					t.Fatalf("email claim = %#v", tc.claims["email"])
				}
				if len(users) != 1 || users[0].issuer != "https://accounts.google.com" || users[0].subject != "subject-person" || users[0].email != tokenEmail || users[0].last != callbackNow.UnixNano() {
					t.Fatalf("member user = %#v", users)
				}
				sessions := signInSessionRows(t, st)
				if len(sessions) != 1 || sessions[0].id != sessionCookie.Value || sessions[0].userID != users[0].id || sessions[0].loginAt != callbackNow.UnixNano() || sessions[0].lastUsed != callbackNow.UnixNano() {
					t.Fatalf("member session = %#v", sessions)
				}
				return
			}

			assertHTMLStatus(t, w, http.StatusForbidden)
			assertNoSetCookie(t, w)
			if formatSignInIdentity(t, st) != before {
				t.Fatalf("non-member changed identity rows:\n%s\nwant:\n%s", formatSignInIdentity(t, st), before)
			}
			users := signInUserRows(t, st)
			if len(users) != 1 || users[0].email != seededEmail || users[0].last != signInNow.UnixNano() {
				t.Fatalf("non-member user = %#v seeded %s", users, seededEmail)
			}
			if _, err := st.LookupSessionIdentity(signInSessionRows(t, st)[0].id, callbackNow); err != nil {
				t.Fatalf("non-member removed the existing session: %v", err)
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

var signInLinkPattern = regexp.MustCompile(`(?i)<a\b[^>]*\bhref\s*=\s*(?:"([^"]*)"|'([^']*)')`)

func signInLinkHrefs(body string) []string {
	matches := signInLinkPattern.FindAllStringSubmatch(body, -1)
	hrefs := make([]string, 0, len(matches))
	for _, match := range matches {
		raw := match[1]
		if raw == "" {
			raw = match[2]
		}
		hrefs = append(hrefs, html.UnescapeString(raw))
	}
	return hrefs
}

func hasExactSignInLink(body, href string) bool {
	for _, got := range signInLinkHrefs(body) {
		if got == href {
			return true
		}
	}
	return false
}

func requireLoginReturnLink(t *testing.T, body, returnURL string) string {
	t.Helper()
	for _, href := range signInLinkHrefs(body) {
		parsed, err := url.Parse(href)
		if err != nil || parsed.Host != "" || parsed.Path != "/login/google" {
			continue
		}
		if parsed.Query().Get("return") == returnURL {
			return href
		}
	}
	t.Fatalf("no /login/google link carries %q in %s (links %#v)", returnURL, body, signInLinkHrefs(body))
	return ""
}

func assertHTMLStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("response = %d %q body %s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
}

func assertNoSetCookie(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if got := w.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("Set-Cookie = %#v", got)
	}
}

type signInUserRow struct {
	id, issuer, subject, email string
	last                       int64
}

type signInSessionRow struct {
	id, userID string
	loginAt    int64
	lastUsed   int64
}

func openSignInSQL(t *testing.T, st *store.Store) *sql.DB {
	t.Helper()
	path, ok := signInStorePath[st]
	if !ok {
		t.Fatal("sign-in store path was not recorded")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func signInUserRows(t *testing.T, st *store.Store) []signInUserRow {
	t.Helper()
	db := openSignInSQL(t, st)
	defer func() { _ = db.Close() }()
	rows, err := db.QueryContext(context.Background(), `SELECT id, issuer, subject, email, last_google_login FROM users ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []signInUserRow
	for rows.Next() {
		var row signInUserRow
		if err := rows.Scan(&row.id, &row.issuer, &row.subject, &row.email, &row.last); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func signInSessionRows(t *testing.T, st *store.Store) []signInSessionRow {
	t.Helper()
	db := openSignInSQL(t, st)
	defer func() { _ = db.Close() }()
	rows, err := db.QueryContext(context.Background(), `SELECT id, user_id, login_at, last_used_at FROM sessions ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []signInSessionRow
	for rows.Next() {
		var row signInSessionRow
		if err := rows.Scan(&row.id, &row.userID, &row.loginAt, &row.lastUsed); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func formatSignInIdentity(t *testing.T, st *store.Store) string {
	t.Helper()
	var b strings.Builder
	for _, user := range signInUserRows(t, st) {
		fmt.Fprintf(&b, "user %s %s %s %s %d\n", user.id, user.issuer, user.subject, user.email, user.last)
	}
	for _, session := range signInSessionRows(t, st) {
		fmt.Fprintf(&b, "session %s %s %d %d\n", session.id, session.userID, session.loginAt, session.lastUsed)
	}
	return b.String()
}

func requireLoginState(t *testing.T, st *store.Store, state, verifier, returnURL string) {
	t.Helper()
	db := openSignInSQL(t, st)
	defer func() { _ = db.Close() }()
	var gotVerifier, gotReturn string
	err := db.QueryRowContext(context.Background(), `SELECT verifier, return_url FROM login_states WHERE state = ?`, state).Scan(&gotVerifier, &gotReturn)
	if err != nil || gotVerifier != verifier || gotReturn != returnURL {
		t.Fatalf("login state %s = %q %q err %v, want verifier %q return %q", state, gotVerifier, gotReturn, err, verifier, returnURL)
	}
}

func assertReturnNotPersisted(t *testing.T, st *store.Store, returnURL string) {
	t.Helper()
	db := openSignInSQL(t, st)
	defer func() { _ = db.Close() }()
	var n int
	err := db.QueryRowContext(context.Background(), `
		SELECT
			(SELECT COUNT(*) FROM login_states WHERE return_url = ? OR state = ? OR verifier = ?)
			+ (SELECT COUNT(*) FROM users WHERE email = ? OR id = ? OR issuer = ? OR subject = ?)
			+ (SELECT COUNT(*) FROM sessions WHERE id = ? OR user_id = ?)`,
		returnURL, returnURL, returnURL,
		returnURL, returnURL, returnURL, returnURL,
		returnURL, returnURL,
	).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("return URL persisted in %d rows", n)
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
