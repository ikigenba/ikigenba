package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

var tokenTestNow = time.Date(2026, time.September, 20, 14, 30, 0, 123, time.UTC)

type tokenTestRand struct{ next byte }

func (r *tokenTestRand) Read(p []byte) (int, error) {
	if r.next == 0 {
		r.next = 1
	}
	for i := range p {
		p[i] = r.next
	}
	r.next++
	return len(p), nil
}

func openTokenTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir()+"/auth.db", &tokenTestRand{})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("Store.Close() error = %v", err)
		}
	})
	return st
}

func tokenTestIdentity(t *testing.T, st *store.Store, subject string) (store.User, store.Session) {
	t.Helper()
	user, err := st.UpsertUserOnLogin("issuer", subject, subject+"@example.com", tokenTestNow)
	if err != nil {
		t.Fatalf("UpsertUserOnLogin() error = %v", err)
	}
	session, err := st.CreateSession(user.ID, tokenTestNow)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	return user, session
}

func tokenTestServer(st *store.Store) *Server {
	return New(Config{Banner: testPageBanner, Store: st, Now: func() time.Time { return tokenTestNow }})
}

func tokenRequest(target, sessionID string, form url.Values) *http.Request {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Host = "auth.green.example:8443"
	req.Header.Set("Origin", "https://auth.green.example:8443")
	req.AddCookie(&http.Cookie{
		Name:     SessionCookieName,
		Value:    sessionID,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return req
}

func tokenActionRequest(sessionID, tokenID, action string) *http.Request {
	req := tokenRequest("/tokens/"+tokenID+"/"+action, sessionID, nil)
	req.SetPathValue("id", tokenID)
	req.SetPathValue("action", action)
	return req
}

func TestTokenRoutesReportClosedStoreOnce(t *testing.T) {
	st := openTokenTestStore(t)
	_, session := tokenTestIdentity(t, st, "closed")
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	_, reason := st.LookupSessionIdentity(session.ID, tokenTestNow)
	if reason == nil {
		t.Fatal("closed store unexpectedly succeeded")
	}
	for _, tc := range []struct {
		name string
		req  *http.Request
	}{
		{name: "create", req: tokenRequest("/tokens", session.ID, url.Values{"name": {"new"}, "expires": {"never"}})},
		{name: "action", req: tokenActionRequest(session.ID, "missing", "delete")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stderr identityDiagnosticWrites
			srv := New(Config{Banner: testPageBanner, Store: st, Now: func() time.Time { return tokenTestNow }, Stderr: &stderr})
			tc.req.Header.Set("X-Request-Id", "token-42")
			response := httptest.NewRecorder()
			srv.ServeHTTP(response, tc.req)

			// R-CCQE-EHNR: a failed store lookup is 500 plain text with no identity headers.
			if response.Code != http.StatusInternalServerError || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" || response.Body.String() != "internal server error\n" {
				t.Fatalf("response = %d %q %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
			}
			if response.Header().Get(HeaderUserID) != "" || response.Header().Get(HeaderUserEmail) != "" {
				t.Fatalf("identity headers on 500: %v", response.Header())
			}
			want := "auth: request token-42: " + reason.Error() + "\n"
			// R-XV9R-80CN: the request id and underlying error are written together.
			// R-XWHN-LS3C: one 5xx request produces one diagnostic Write call.
			if len(stderr.writes) != 1 || string(stderr.writes[0]) != want {
				t.Fatalf("stderr writes = %q, want one %q", stderr.writes, want)
			}
		})
	}
}

func TestTokenMutationStoreFailuresReport500(t *testing.T) {
	path := t.TempDir() + "/auth.db"
	st, err := store.Open(path, &tokenTestRand{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	user, session := tokenTestIdentity(t, st, "mutations")
	token, _, err := st.CreateToken(user.ID, "existing", store.ExpiryNever, tokenTestNow)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, trigger := range []string{
		`CREATE TRIGGER fail_token_insert BEFORE INSERT ON tokens BEGIN SELECT RAISE(FAIL, 'injected token insert failure'); END`,
		`CREATE TRIGGER fail_token_update BEFORE UPDATE ON tokens BEGIN SELECT RAISE(FAIL, 'injected token update failure'); END`,
		`CREATE TRIGGER fail_token_delete BEFORE DELETE ON tokens BEGIN SELECT RAISE(FAIL, 'injected token delete failure'); END`,
	} {
		if _, err := db.ExecContext(context.Background(), trigger); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		req  *http.Request
		err  error
	}{
		{name: "create", req: tokenRequest("/tokens", session.ID, url.Values{"name": {"new"}, "expires": {"never"}}), err: func() error { _, _, err := st.CreateToken(user.ID, "new", store.ExpiryNever, tokenTestNow); return err }()},
		{name: "enable", req: tokenActionRequest(session.ID, token.ID, "enable"), err: st.SetTokenEnabled(user.ID, token.ID, true)},
		{name: "disable", req: tokenActionRequest(session.ID, token.ID, "disable"), err: st.SetTokenEnabled(user.ID, token.ID, false)},
		{name: "delete", req: tokenActionRequest(session.ID, token.ID, "delete"), err: st.DeleteToken(user.ID, token.ID)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil {
				t.Fatal("trigger did not fail store operation")
			}
			var stderr identityDiagnosticWrites
			srv := New(Config{Banner: testPageBanner, Store: st, Now: func() time.Time { return tokenTestNow }, Stderr: &stderr})
			response := httptest.NewRecorder()
			srv.ServeHTTP(response, tc.req)

			// R-CCQE-EHNR: token write failures return one plain-text line.
			if response.Code != http.StatusInternalServerError || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" || response.Body.String() != "internal server error\n" {
				t.Fatalf("response = %d %q %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
			}
			// R-XV9R-80CN: the actual SQLite failure is retained in the diagnostic.
			// R-XWHN-LS3C: the response causes exactly one Write call.
			want := "auth: request -: " + tc.err.Error() + "\n"
			if len(stderr.writes) != 1 || string(stderr.writes[0]) != want {
				t.Fatalf("stderr writes = %q, want one %q", stderr.writes, want)
			}
		})
	}
}

func TestTokenRefusalsWriteNoDiagnostic(t *testing.T) {
	st := openTokenTestStore(t)
	_, session := tokenTestIdentity(t, st, "refused")
	var stderr identityDiagnosticWrites
	srv := New(Config{Banner: testPageBanner, Store: st, Now: func() time.Time { return tokenTestNow }, Stderr: &stderr})
	missing := tokenActionRequest(session.ID, "missing", "delete")
	badOrigin := tokenRequest("/tokens", session.ID, url.Values{"name": {"new"}, "expires": {"never"}})
	badOrigin.Header.Set("Origin", "https://foreign.example")
	for _, tc := range []struct {
		req    *http.Request
		status int
	}{{missing, http.StatusNotFound}, {badOrigin, http.StatusForbidden}} {
		response := httptest.NewRecorder()
		srv.ServeHTTP(response, tc.req)
		if response.Code != tc.status {
			t.Fatalf("status = %d, want %d", response.Code, tc.status)
		}
	}
	// R-XWHN-LS3C: 403 and 404 responses write nothing to cfg.Stderr.
	if len(stderr.writes) != 0 {
		t.Fatalf("stderr writes for refusals = %q", stderr.writes)
	}
}

func TestCreateTokenAcceptsTrimmedNameAndEveryExpiry(t *testing.T) {
	tests := []struct {
		value string
		want  *time.Time
	}{
		{value: "never"},
		{value: "30d", want: timePointer(tokenTestNow.Add(30 * 24 * time.Hour))},
		{value: "90d", want: timePointer(tokenTestNow.Add(90 * 24 * time.Hour))},
		{value: "365d", want: timePointer(tokenTestNow.Add(365 * 24 * time.Hour))},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			st := openTokenTestStore(t)
			user, session := tokenTestIdentity(t, st, "member-"+tt.value)
			srv := tokenTestServer(st)
			req := tokenRequest("/tokens", session.ID, url.Values{
				"name":    {"  deploy token  "},
				"expires": {tt.value},
			})
			response := httptest.NewRecorder()

			srv.handleCreateToken(response, req)

			// R-N5RR-K5GT: creation accepts all four expiry values and uses the trimmed name.
			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != htmlDocumentContentType {
				t.Fatalf("response = %d %q, want 200 HTML", response.Code, response.Header().Get("Content-Type"))
			}
			tokens, err := st.ListTokens(user.ID)
			if err != nil {
				t.Fatalf("ListTokens() error = %v", err)
			}
			if len(tokens) != 1 {
				t.Fatalf("len(ListTokens()) = %d, want 1", len(tokens))
			}
			if tokens[0].Name != "deploy token" || !equalTimePointer(tokens[0].ExpiresAt, tt.want) {
				t.Errorf("created token = %#v, want trimmed name and expiry %v", tokens[0], tt.want)
			}

			body := response.Body.String()
			secret := regexp.MustCompile(`ikp_[0-9A-HJKMNP-TV-Z]{52}`).FindString(body)
			if secret == "" || strings.Count(body, secret) != 1 {
				t.Fatalf("creation body contains secret %q %d times, want one valid secret once", secret, strings.Count(body, secret))
			}
			if !strings.Contains(body, "<button") || !strings.Contains(body, "clipboard.writeText") || !strings.Contains(body, `<a href="/">`) {
				t.Errorf("creation body lacks copy button or profile link: %s", body)
			}
		})
	}
}

func TestCreateTokenRejectsInvalidNameAndExpiryWithoutMutation(t *testing.T) {
	tests := []struct {
		name    string
		expires string
	}{
		{name: "", expires: "never"},
		{name: "   \t\n", expires: "30d"},
		{name: strings.Repeat("界", 65), expires: "90d"},
		{name: "valid", expires: ""},
		{name: "valid", expires: "tomorrow"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("name=%q/expires=%q", tt.name, tt.expires), func(t *testing.T) {
			st := openTokenTestStore(t)
			user, session := tokenTestIdentity(t, st, "invalid")
			srv := tokenTestServer(st)
			req := tokenRequest("/tokens", session.ID, url.Values{
				"name": {tt.name}, "expires": {tt.expires},
			})
			response := httptest.NewRecorder()

			srv.handleCreateToken(response, req)

			// R-N87K-BOY7: invalid names/expiries return the complete form and create nothing.
			if response.Code != http.StatusBadRequest || response.Header().Get("Content-Type") != htmlDocumentContentType {
				t.Fatalf("response = %d %q, want 400 HTML", response.Code, response.Header().Get("Content-Type"))
			}
			body := response.Body.String()
			for _, fragment := range []string{`<form method="post" action="/tokens">`, `name="name"`, `name="expires"`} {
				if !strings.Contains(body, fragment) {
					t.Errorf("response body missing %q", fragment)
				}
			}
			tokens, err := st.ListTokens(user.ID)
			if err != nil || len(tokens) != 0 {
				t.Errorf("ListTokens() = %#v, %v; want empty", tokens, err)
			}
		})
	}
}

func TestTokenToggleAndDeleteEffects(t *testing.T) {
	st := openTokenTestStore(t)
	owner, session := tokenTestIdentity(t, st, "owner")
	token, secret, err := st.CreateToken(owner.ID, "managed", store.ExpiryNever, tokenTestNow)
	if err != nil {
		t.Fatal(err)
	}
	srv := tokenTestServer(st)

	for _, tt := range []struct {
		action      string
		wantEnabled bool
	}{{action: "disable", wantEnabled: false}, {action: "enable", wantEnabled: true}} {
		response := httptest.NewRecorder()
		srv.handleTokenAction(response, tokenActionRequest(session.ID, token.ID, tt.action))

		// R-ND35-URWZ: enable and disable update the owned token and redirect to the profile.
		if response.Code != http.StatusFound || response.Header().Get("Location") != "/" {
			t.Fatalf("%s response = %d Location %q", tt.action, response.Code, response.Header().Get("Location"))
		}
		tokens, err := st.ListTokens(owner.ID)
		if err != nil || len(tokens) != 1 || tokens[0].Enabled != tt.wantEnabled {
			t.Fatalf("after %s ListTokens() = %#v, %v", tt.action, tokens, err)
		}
	}

	response := httptest.NewRecorder()
	srv.handleTokenAction(response, tokenActionRequest(session.ID, token.ID, "delete"))

	// R-NFIY-MBED: deletion redirects, removes the row, and makes the secret unauthenticating.
	if response.Code != http.StatusFound || response.Header().Get("Location") != "/" {
		t.Fatalf("delete response = %d Location %q", response.Code, response.Header().Get("Location"))
	}
	tokens, err := st.ListTokens(owner.ID)
	if err != nil || len(tokens) != 0 {
		t.Fatalf("after delete ListTokens() = %#v, %v; want empty", tokens, err)
	}
	if _, err := st.LookupTokenIdentity(secret, tokenTestNow); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted secret LookupTokenIdentity() error = %v, want ErrNotFound", err)
	}
}

func TestTokenActionsHideOwnershipAndDoNotMutateOnNotFound(t *testing.T) {
	for _, action := range []string{"enable", "disable", "delete"} {
		for _, target := range []string{"foreign", "missing"} {
			t.Run(action+"/"+target, func(t *testing.T) {
				st := openTokenTestStore(t)
				owner, session := tokenTestIdentity(t, st, "owner")
				other, _ := tokenTestIdentity(t, st, "other")
				ownerToken, _, err := st.CreateToken(owner.ID, "owner", store.ExpiryNever, tokenTestNow)
				if err != nil {
					t.Fatal(err)
				}
				foreignToken, _, err := st.CreateToken(other.ID, "foreign", store.ExpiryNever, tokenTestNow)
				if err != nil {
					t.Fatal(err)
				}
				tokenID := "00000000000000000000000000"
				if target == "foreign" {
					tokenID = foreignToken.ID
				}
				beforeOwner, _ := st.ListTokens(owner.ID)
				beforeOther, _ := st.ListTokens(other.ID)

				response := httptest.NewRecorder()
				tokenTestServer(st).handleTokenAction(response, tokenActionRequest(session.ID, tokenID, action))

				// R-NGQV-0352: foreign and nonexistent ids are indistinguishable 404s with no mutation.
				if response.Code != http.StatusNotFound || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
					t.Fatalf("response = %d %q, want 404 plain text", response.Code, response.Header().Get("Content-Type"))
				}
				afterOwner, _ := st.ListTokens(owner.ID)
				afterOther, _ := st.ListTokens(other.ID)
				if !reflect.DeepEqual(afterOwner, beforeOwner) || !reflect.DeepEqual(afterOther, beforeOther) {
					t.Errorf("tokens changed: owner %#v -> %#v; other %#v -> %#v", beforeOwner, afterOwner, beforeOther, afterOther)
				}
				if ownerToken.ID == tokenID {
					t.Fatal("test setup accidentally targeted owned token")
				}
			})
		}
	}
}

func TestTokenMutationsRejectBadOrMissingOriginWithoutMutation(t *testing.T) {
	origins := []struct{ name, value string }{
		{name: "missing", value: ""},
		{name: "foreign", value: "https://evil.example"},
		{name: "scheme", value: "http://auth.green.example:8443"},
		{name: "prefix", value: "https://auth.green.example:8443.evil"},
		{name: "missing port", value: "https://auth.green.example"},
		{name: "space sibling", value: "https://app.green.example:8443"},
	}
	for _, action := range []string{"create", "enable", "disable", "delete"} {
		for _, origin := range origins {
			t.Run(action+"/"+origin.name, func(t *testing.T) {
				st := openTokenTestStore(t)
				owner, session := tokenTestIdentity(t, st, "owner")
				token, secret, err := st.CreateToken(owner.ID, "existing", store.ExpiryNever, tokenTestNow)
				if err != nil {
					t.Fatal(err)
				}
				// Enable must start disabled, and disable must start enabled, so a
				// store call with the action's own flag cannot hide as a no-op.
				if action == "enable" {
					if err := st.SetTokenEnabled(owner.ID, token.ID, false); err != nil {
						t.Fatal(err)
					}
				}
				before, err := st.ListTokens(owner.ID)
				if err != nil {
					t.Fatal(err)
				}
				var req *http.Request
				if action == "create" {
					req = tokenRequest("/tokens", session.ID, url.Values{"name": {"new"}, "expires": {"30d"}})
				} else {
					req = tokenRequest("/tokens/"+token.ID+"/"+action, session.ID, nil)
				}
				req.Header.Set("Origin", origin.value)
				response := httptest.NewRecorder()
				tokenTestServer(st).httpServer.Handler.ServeHTTP(response, req)

				// R-NHYR-DUVR: an Origin that is not the service's own is 403 plain
				// text and does not create, enable, disable, or delete.
				if response.Code != http.StatusForbidden || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
					t.Fatalf("response = %d %q, want 403 plain text", response.Code, response.Header().Get("Content-Type"))
				}
				after, err := st.ListTokens(owner.ID)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(after, before) {
					t.Errorf("tokens changed: %#v -> %#v", before, after)
				}
				_, lookupErr := st.LookupTokenIdentity(secret, tokenTestNow)
				if action == "enable" {
					if !errors.Is(lookupErr, store.ErrNotFound) {
						t.Errorf("disabled secret became usable: %v", lookupErr)
					}
				} else if lookupErr != nil {
					t.Errorf("existing secret stopped authenticating: %v", lookupErr)
				}
			})
		}
	}
}

func TestCreateTokenRejectsMissingAndNonMemberExpiry(t *testing.T) {
	cases := []struct {
		name   string
		form   url.Values
		detail string
	}{
		{name: "missing", detail: "omitted", form: url.Values{"name": {"  kept name  "}}},
		{name: "empty", detail: "empty", form: url.Values{"name": {"n"}, "expires": {""}}},
		{name: "word", detail: "tomorrow", form: url.Values{"name": {strings.Repeat("n", 64)}, "expires": {"tomorrow"}}},
		{name: "case", detail: "Never", form: url.Values{"name": {"kept"}, "expires": {"Never"}}},
		{name: "plural", detail: "30D", form: url.Values{"name": {"kept"}, "expires": {"30D"}}},
		{name: "padded", detail: "space", form: url.Values{"name": {"kept"}, "expires": {" never"}}},
		{name: "bare", detail: "365", form: url.Values{"name": {"kept"}, "expires": {"365"}}},
	}
	for _, tc := range cases {
		t.Run(tc.detail, func(t *testing.T) {
			st := openTokenTestStore(t)
			user, session := tokenTestIdentity(t, st, "member")
			response := httptest.NewRecorder()
			tokenTestServer(st).httpServer.Handler.ServeHTTP(response, tokenRequest("/tokens", session.ID, tc.form))

			// R-G35Y-WGL0: a valid name with a missing or non-member expires is a
			// 400 HTML create form and stores nothing.
			if response.Code != http.StatusBadRequest || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("response = %d %q, want 400 text/html; charset=utf-8", response.Code, response.Header().Get("Content-Type"))
			}
			if !bodyHasTokenCreateForm(response.Body.String()) {
				t.Fatalf("body lacks the create form: %s", response.Body.String())
			}
			tokens, err := st.ListTokens(user.ID)
			if err != nil || len(tokens) != 0 {
				t.Fatalf("ListTokens() = %#v, %v; want no CreateToken result", tokens, err)
			}
		})
	}
}

var secretPattern = regexp.MustCompile(`^ikp_[0-9A-HJKMNP-TV-Z]{52}$`)

func presentedSecret(body, hash string) (string, bool) {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	var found string
	for i := 0; i+56 <= len(body); i++ {
		if body[i:i+4] != "ikp_" {
			continue
		}
		candidate := body[i : i+56]
		if !secretPattern.MatchString(candidate) {
			continue
		}
		if i+56 < len(body) && strings.ContainsRune(alphabet, rune(body[i+56])) {
			continue
		}
		if idcodec.HashSecret(candidate) != hash {
			continue
		}
		if found != "" && found != candidate {
			return "", false
		}
		found = candidate
	}
	return found, found != ""
}

func bodyHasTokenCreateForm(body string) bool {
	formRE := regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
	for _, form := range formRE.FindAllStringSubmatch(body, -1) {
		if !strings.EqualFold(tagAttr(form[1], "method"), "post") || tagAttr(form[1], "action") != "/tokens" {
			continue
		}
		if namedControl(form[2], "name") && namedControl(form[2], "expires") {
			return true
		}
	}
	return false
}

func namedControl(inner, name string) bool {
	return regexp.MustCompile(`(?i)<(?:input|select|textarea)\b[^>]*\bname\s*=\s*(?:"` + regexp.QuoteMeta(name) + `"|'` + regexp.QuoteMeta(name) + `')`).MatchString(inner)
}

func tagAttr(tag, name string) string {
	match := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\s*=\s*(?:"([^"]*)"|'([^']*)')`).FindStringSubmatch(tag)
	if match == nil {
		return ""
	}
	if match[1] != "" {
		return match[1]
	}
	return match[2]
}

func timePointer(value time.Time) *time.Time { return &value }

func equalTimePointer(got, want *time.Time) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return got.Equal(*want)
}
