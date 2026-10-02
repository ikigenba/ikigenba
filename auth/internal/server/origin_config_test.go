package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	googleclient "github.com/ikigenba/ikigenba/auth/internal/google"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func TestConfiguredCallbackReachesAuthorizationAndExchange(t *testing.T) {
	// R-T8R1-7SBK, R-T9YX-LK29: both Google requests carry the per-request
	// callback, or the configured callback independently of either request host.
	for _, tc := range []struct{ callback, startHost, finishHost, want string }{
		{"", "auth.green.example", "green.example", "https://auth.green.example/login/google/callback"},
		{"", "auth.green.example:8443", "auth.green.example:8443", "https://auth.green.example:8443/login/google/callback"},
		{"http://auth.wip.localhost:7400", "auth.other.example", "unrelated.example:9000", "http://auth.wip.localhost:7400/login/google/callback"},
		{"https://callback.example", "localhost:3001", "auth.other.example", "https://callback.example/login/google/callback"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			issuer := newSignInIssuer(t)
			issuer.issue("configured-code", "configured-subject", "member@green.example")
			st := openSignInStore(t)
			s := New(Config{Store: st, Google: googleclient.NewClient("client-id", "client-secret", "green.example", issuer.server.URL), Now: func() time.Time { return signInNow }, Rand: &signInRand{next: 1}, Stderr: io.Discard, WorkspaceDomain: "green.example", CallbackURL: tc.callback, Banner: testPageBanner})
			w := serveSignIn(s, http.MethodGet, "/login/google", tc.startHost, nil, "")
			location, err := url.Parse(w.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusFound || location.Query().Get("redirect_uri") != tc.want {
				t.Fatalf("authorization = %d %s", w.Code, location)
			}
			finish := serveSignIn(s, http.MethodGet, "/login/google/callback?state="+location.Query().Get("state")+"&code=configured-code", tc.finishHost, nil, "")
			if finish.Code != http.StatusFound {
				t.Fatalf("callback = %d %s", finish.Code, finish.Body.String())
			}
			if got := issuer.lastForm().Get("redirect_uri"); got != tc.want {
				t.Fatalf("exchange redirect_uri = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOwnOriginControlsEveryTokenRoute(t *testing.T) {
	// R-TCEQ-D3JN, R-TDMM-QVAC: token mutations use the fallback own origin
	// or exactly PublicURL regardless of the request Host.
	for _, tc := range []struct{ public, host, own, reject string }{
		{"", "auth.green.example:8443", "https://auth.green.example:8443", "https://auth.green.example"},
		{"http://auth.wip.localhost:7400", "auth.unrelated.example:9000", "http://auth.wip.localhost:7400", "https://auth.unrelated.example:9000"},
		{"https://fixed.example", "localhost:3001", "https://fixed.example", "https://auth.localhost:3001"},
	} {
		for _, action := range []string{"create", "enable", "disable", "delete"} {
			t.Run(tc.own+"/"+action, func(t *testing.T) {
				st := openTokenTestStore(t)
				user, session := tokenTestIdentity(t, st, "owner")
				token, _, err := st.CreateToken(user.ID, "existing", store.ExpiryNever, tokenTestNow)
				if err != nil {
					t.Fatal(err)
				}
				s := New(Config{Store: st, Now: func() time.Time { return tokenTestNow }, PublicURL: tc.public, Banner: testPageBanner})
				target := "/tokens/" + token.ID + "/" + action
				form := url.Values(nil)
				if action == "create" {
					target = "/tokens"
					form = url.Values{"name": {"new"}, "expires": {"never"}}
				}
				before, err := st.ListTokens(user.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, origin := range []string{tc.reject, strings.ToUpper(tc.own)} {
					req := tokenRequest(target, session.ID, form)
					req.Host = tc.host
					req.Header.Set("Origin", origin)
					w := httptest.NewRecorder()
					s.ServeHTTP(w, req)
					if w.Code != http.StatusForbidden {
						t.Fatalf("rejected origin %q = %d", origin, w.Code)
					}
					after, err := st.ListTokens(user.ID)
					if err != nil || !reflect.DeepEqual(after, before) {
						t.Fatalf("rejected origin changed tokens: %#v %v", after, err)
					}
				}
				req := tokenRequest(target, session.ID, form)
				req.Host = tc.host
				req.Header.Set("Origin", tc.own)
				w := httptest.NewRecorder()
				s.ServeHTTP(w, req)
				want := http.StatusFound
				if action == "create" {
					want = http.StatusOK
				}
				if w.Code != want {
					t.Fatalf("own origin = %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestLogoutConfiguredOriginsAndRejectedShapes(t *testing.T) {
	// R-GQZR-C3MP, R-GS7N-PVDE: matching uses ASCII scheme/host folding,
	// an exact configured port, and the request cookie domain, not PublicURL's host.
	// R-GTFK-3N43, R-GUNG-HEUS: accepted origins clear and delete only the
	// session; every rejection leaves the live session and all persisted state intact.
	for _, tc := range []struct {
		public, host       string
		accepted, rejected []string
	}{
		{"", "auth.green.example", []string{"https://green.example", "HTTPS://AUTH.GREEN.EXAMPLE", "https://nested.app.green.example"}, []string{"", "null", "http://green.example", "https://green.example:443", "https://.app.green.example", "https://app..green.example", "https://green.example.", "https://evilgreen.example", "https://green.example.evil.com", "https://user@green.example", "https://green.example/", "https://green.example?x=1", "https://green.example#x"}},
		{"http://auth.wip.localhost:7400", "auth.wip.localhost:7400", []string{"http://auth.wip.localhost:7400", "HTTP://WIP.LOCALHOST:7400", "http://nested.app.wip.localhost:7400"}, []string{"", "null", "https://wip.localhost:7400", "http://wip.localhost", "http://wip.localhost:7401", "http://wip.localhost:07400", "http://.app.wip.localhost:7400", "http://app..wip.localhost:7400", "http://wip.localhost.:7400", "http://evilwip.localhost:7400", "http://wip.localhost.evil.com:7400", "http://user@wip.localhost:7400", "http://wip.localhost:7400/", "http://wip.localhost:7400?x=1", "http://wip.localhost:7400#x"}},
		{"https://configured.example", "auth.request.example:9000", []string{"https://request.example", "HTTPS://APP.REQUEST.EXAMPLE"}, []string{"https://configured.example", "https://request.example:9000", "https://request.example:443", "http://request.example", "https://.app.request.example"}},
		{"http://configured.example:7400", "auth.other.example:9000", []string{"http://other.example:7400", "HTTP://APP.OTHER.EXAMPLE:7400"}, []string{"http://configured.example:7400", "http://other.example:9000", "http://other.example", "http://other.example:07400"}},
		{"", "auth.green.example.", nil, []string{"https://auth.green.example.", "https://green.example."}},
		{"http://auth.green.example:7400", "auth.green.example.:7400", nil, []string{"http://auth.green.example.:7400", "http://green.example.:7400"}},
	} {
		var baseline *httptest.ResponseRecorder
		run := func(origins []string, accepted bool) {
			t.Helper()
			st := openSignInStore(t)
			user, err := st.UpsertUserOnLogin("issuer", "owner", "member@green.example", signInNow)
			if err != nil {
				t.Fatal(err)
			}
			session, err := st.CreateSession(user.ID, signInNow)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = st.CreateToken(user.ID, "keep", store.ExpiryNever, signInNow)
			if err != nil {
				t.Fatal(err)
			}
			before := profileStateSnapshot(t, st)
			beforeUsers := signInUserRows(t, st)
			beforeTokens, err := st.ListTokens(user.ID)
			if err != nil {
				t.Fatal(err)
			}
			s := New(Config{Store: st, Now: func() time.Time { return signInNow.Add(time.Minute) }, PublicURL: tc.public, Banner: testPageBanner})
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/logout", nil)
			req.Host = tc.host
			req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			for _, origin := range origins {
				req.Header.Add("Origin", origin)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			if accepted {
				if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
					t.Fatalf("accepted %q = %d %#v", origins, w.Code, w.Header())
				}
				cookies := w.Result().Cookies()
				if len(cookies) != 1 || cookies[0].Name != SessionCookieName || cookies[0].Value != "" || cookies[0].MaxAge != -1 {
					t.Fatalf("clear cookie = %#v", cookies)
				}
				if _, err := st.LookupSessionIdentity(session.ID, signInNow); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("session remains: %v", err)
				}
				afterTokens, err := st.ListTokens(user.ID)
				if err != nil || !reflect.DeepEqual(beforeTokens, afterTokens) || !reflect.DeepEqual(beforeUsers, signInUserRows(t, st)) {
					t.Fatalf("logout changed user/tokens: %#v %v", afterTokens, err)
				}
				if baseline != nil && (w.Code != baseline.Code || !reflect.DeepEqual(w.Header(), baseline.Header()) || w.Body.String() != baseline.Body.String()) {
					t.Fatalf("origin-dependent response %q", origins)
				}
				baseline = w
			} else {
				if w.Code != http.StatusForbidden || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || strings.Count(w.Body.String(), "\n") != 1 || !strings.HasSuffix(w.Body.String(), "\n") {
					t.Fatalf("rejected %q = %d %#v %q", origins, w.Code, w.Header(), w.Body.String())
				}
				assertNoSetCookie(t, w)
				if after := profileStateSnapshot(t, st); after != before {
					t.Fatalf("rejected %q mutated state", origins)
				}
				if _, err := st.LookupSessionIdentity(session.ID, signInNow.Add(time.Minute)); err != nil {
					t.Fatalf("rejected logout lost live session: %v", err)
				}
			}
		}
		for _, origin := range tc.accepted {
			run([]string{origin}, true)
		}
		for _, origin := range tc.rejected {
			run([]string{origin}, false)
		}
		run(nil, false)
		if len(tc.accepted) > 0 {
			run([]string{tc.accepted[0], tc.accepted[0]}, false)
		}
	}
}
