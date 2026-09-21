package server

import (
	"bytes"
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
	return New(Config{Store: st, Now: func() time.Time { return tokenTestNow }})
}

func tokenRequest(method, target, sessionID string, form url.Values) *http.Request {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Host = "127.0.0.1:3001"
	req.Header.Set("Origin", "http://127.0.0.1:3001")
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessionID})
	return req
}

func tokenActionRequest(sessionID, tokenID, action string) *http.Request {
	req := tokenRequest(http.MethodPost, "/tokens/"+tokenID+"/"+action, sessionID, nil)
	req.SetPathValue("id", tokenID)
	req.SetPathValue("action", action)
	return req
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
			req := tokenRequest(http.MethodPost, "/tokens", session.ID, url.Values{
				"name":    {"  deploy token  "},
				"expires": {tt.value},
			})
			response := httptest.NewRecorder()

			srv.handleCreateToken(response, req)

			// R-N5RR-K5GT: creation accepts all four expiry values and uses the trimmed name.
			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != tokenHTMLContentType {
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

			// R-N6ZN-XX7I: the one-time page contains one secret, once, a copy button, and a profile link.
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
			req := tokenRequest(http.MethodPost, "/tokens", session.ID, url.Values{
				"name": {tt.name}, "expires": {tt.expires},
			})
			response := httptest.NewRecorder()

			srv.handleCreateToken(response, req)

			// R-N87K-BOY7 and R-G35Y-WGL0: invalid names/expiries return the complete form and create nothing.
			if response.Code != http.StatusBadRequest || response.Header().Get("Content-Type") != tokenHTMLContentType {
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

func TestRenderTokenRowsShowsMetadataActionsAndNoSecrets(t *testing.T) {
	st := openTokenTestStore(t)
	owner, _ := tokenTestIdentity(t, st, "owner")
	other, _ := tokenTestIdentity(t, st, "other")
	enabled, enabledSecret, err := st.CreateToken(owner.ID, "enabled <token>", store.Expiry30d, tokenTestNow)
	if err != nil {
		t.Fatal(err)
	}
	disabled, disabledSecret, err := st.CreateToken(owner.ID, "disabled token", store.ExpiryNever, tokenTestNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTokenEnabled(owner.ID, disabled.ID, false); err != nil {
		t.Fatal(err)
	}
	lastUsed := tokenTestNow.Add(2 * time.Hour)
	if _, err := st.TouchTokenIdentity(enabledSecret, lastUsed); err != nil {
		t.Fatal(err)
	}
	foreign, foreignSecret, err := st.CreateToken(other.ID, "foreign token", store.Expiry365d, tokenTestNow)
	if err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	if err := tokenTestServer(st).renderTokenRows(&body, owner.ID); err != nil {
		t.Fatalf("renderTokenRows() error = %v", err)
	}
	htmlBody := body.String()

	// R-N9FG-PGOW: every owned row exposes name, created, last-used, expiry, and enabled state.
	for _, fragment := range []string{
		"enabled &lt;token&gt;", "disabled token",
		enabled.CreatedAt.Format(time.RFC3339Nano), disabled.CreatedAt.Format(time.RFC3339Nano),
		lastUsed.Format(time.RFC3339Nano), "never", enabled.ExpiresAt.Format(time.RFC3339Nano),
		`class="enabled">true`, `class="enabled">false`,
	} {
		if !strings.Contains(htmlBody, fragment) {
			t.Errorf("profile rows missing metadata %q: %s", fragment, htmlBody)
		}
	}
	if strings.Contains(htmlBody, foreign.Name) || strings.Contains(htmlBody, foreign.ID) {
		t.Errorf("profile rows contain another user's token: %s", htmlBody)
	}

	// R-NAND-38FL: action forms use Token.ID and reflect enabled state.
	for _, action := range []string{
		`action="/tokens/` + enabled.ID + `/disable"`,
		`action="/tokens/` + enabled.ID + `/delete"`,
		`action="/tokens/` + disabled.ID + `/enable"`,
		`action="/tokens/` + disabled.ID + `/delete"`,
	} {
		if !strings.Contains(htmlBody, action) {
			t.Errorf("profile rows missing action %q", action)
		}
	}

	// R-NBV9-H06A: no owner's or foreign token plaintext is rendered later.
	for _, secret := range []string{enabledSecret, disabledSecret, foreignSecret} {
		if strings.Contains(htmlBody, secret) {
			t.Errorf("profile rows leaked plaintext secret %q", secret)
		}
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
	for _, action := range []string{"create", "enable", "disable", "delete"} {
		for _, origin := range []string{"", "https://evil.example"} {
			t.Run(action+"/"+origin, func(t *testing.T) {
				st := openTokenTestStore(t)
				owner, session := tokenTestIdentity(t, st, "owner")
				token, secret, err := st.CreateToken(owner.ID, "existing", store.ExpiryNever, tokenTestNow)
				if err != nil {
					t.Fatal(err)
				}
				before, _ := st.ListTokens(owner.ID)
				var req *http.Request
				if action == "create" {
					req = tokenRequest(http.MethodPost, "/tokens", session.ID, url.Values{"name": {"new"}, "expires": {"30d"}})
				} else {
					req = tokenActionRequest(session.ID, token.ID, action)
				}
				req.Header.Set("Origin", origin)
				response := httptest.NewRecorder()
				if action == "create" {
					tokenTestServer(st).handleCreateToken(response, req)
				} else {
					tokenTestServer(st).handleTokenAction(response, req)
				}

				// R-NHYR-DUVR: every mutation rejects missing or foreign Origin before changing state.
				if response.Code != http.StatusForbidden || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
					t.Fatalf("response = %d %q, want 403 plain text", response.Code, response.Header().Get("Content-Type"))
				}
				after, _ := st.ListTokens(owner.ID)
				if !reflect.DeepEqual(after, before) {
					t.Errorf("tokens changed: %#v -> %#v", before, after)
				}
				if _, err := st.LookupTokenIdentity(secret, tokenTestNow); err != nil {
					t.Errorf("existing secret stopped authenticating: %v", err)
				}
			})
		}
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func equalTimePointer(got, want *time.Time) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return got.Equal(*want)
}
