package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/server"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func TestRunOriginValues(t *testing.T) {
	// R-GDKV-4MH2 R-GG0N-W5YG R-GH8K-9XP5
	accepted := []string{"http://auth.wip.localhost:7400", "http://localhost:7400", "https://localhost:80", "http://localhost:443", "http://localhost:1", "http://localhost:65535", "https://localhost", "http://a-1.b2", "http://127.1:7400", "http://-"}
	rejected := []string{"", "http://localhost:7400/", "http://localhost/path", "http://localhost?q=x", "http://localhost#x", "http://user@localhost", " http://localhost", "http://local host", "http://", "http://:7400", "http://localhost:", "http://localhost:a", "http://localhost:1:2", "ftp://localhost", "HTTP://localhost", "Https://localhost", "http://auth.green.example.:7400", "http://.wip.localhost:7400", "http://auth..localhost:7400", "http://Auth.wip.localhost:7400", "http://localhost:07400", "http://localhost:0", "http://localhost:65536", "http://localhost:80", "https://localhost:443", "http://localhost:+1", "http://localhost:99999999999999999999", "http://local_host", "http://lócálhost", "http://[::1]:7400", "http://localhost\n"}
	dir, err := os.MkdirTemp("", "auth-origin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	addr := filepath.Join(dir, "notify.sock")
	notifications, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = notifications.Close() })
	for _, key := range []string{"IKIGENBA_PUBLIC_URL", "IKIGENBA_CALLBACK_URL"} {
		for _, valid := range []bool{false, true} {
			values := rejected
			if valid {
				values = accepted
			}
			for _, value := range values {
				t.Run(key+"/"+value, func(t *testing.T) {
					for _, drain := range []string{"unset", "", "1"} {
						env := goodEnv()
						delete(env, "LISTEN_PID")
						env[key] = value
						env["NOTIFY_SOCKET"] = addr
						if drain != "unset" {
							env["DRAIN_SECONDS"] = drain
						}
						p := baseProcess(env, testSource(t), nil)
						p.Inherit = func(uintptr) (net.Listener, error) {
							t.Fatal("inherited listener on refused start")
							return nil, errors.New("unexpected inheritance")
						}
						p.Unsetenv = func(string) error { t.Fatal("unset variable on refused start"); return nil }
						want := "auth: " + key + " is '" + value + "', not an origin\n"
						if valid {
							want = "auth: no socket was passed in" + wantSocketHint
						}
						if code := Run(t.Context(), p); code != 2 || p.Stdout.(*bytes.Buffer).Len() != 0 || p.Stderr.(*countWriter).String() != want {
							t.Fatalf("code=%d out=%q diagnostic=%q want=%q", code, p.Stdout, p.Stderr, want)
						}
						if _, err := os.Stat(p.DBSource); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("database opened: %v", err)
						}
						assertNoNotification(t, notifications)
					}
				})
			}
		}
	}
}

func TestRunValidationPrecedence(t *testing.T) {
	// R-GESR-IE7R
	keys := []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "WORKSPACE_DOMAIN", "DRAIN_SECONDS", "IKIGENBA_PUBLIC_URL", "IKIGENBA_CALLBACK_URL"}
	for i, first := range keys {
		t.Run(first, func(t *testing.T) {
			env := goodEnv()
			for _, key := range keys[i:] {
				env[key] = ""
			}
			env["DRAIN_SECONDS"] = "bad"
			if i > 3 {
				env["DRAIN_SECONDS"] = "1"
			}
			p := baseProcess(env, testSource(t), nil)
			p.LookupEnv = func(key string) (string, bool) {
				if key == "LISTEN_PID" || key == "LISTEN_FDS" {
					t.Fatalf("socket lookup before %s validation", first)
				}
				v, ok := env[key]
				return v, ok
			}
			want := "auth: " + first + " is not set\n"
			if i == 3 {
				want = "auth: DRAIN_SECONDS is 'bad', not a positive whole number of seconds\n"
			}
			if i > 3 {
				want = "auth: " + first + " is '', not an origin\n"
			}
			if code := Run(t.Context(), p); code != 2 || p.Stderr.(*countWriter).String() != want {
				t.Fatalf("code=%d diagnostic=%q want=%q", code, p.Stderr, want)
			}
		})
	}
}

func TestRunOptionalOriginsThroughHTTP(t *testing.T) {
	// R-SO0Q-POPR R-SP8N-3GGG R-T3VF-OPCS R-GOJY-KK5B R-GPRU-YBW0
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer.URL, "authorization_endpoint": issuer.URL + "/authorize", "token_endpoint": issuer.URL + "/token", "jwks_uri": issuer.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
	}))
	defer issuer.Close()
	for _, tc := range []struct{ public, callback string }{{"", ""}, {"http://auth.wip.localhost:7400", ""}, {"", "http://localhost:7400"}, {"http://auth.wip.localhost:7400", "http://localhost:7400"}} {
		t.Run(tc.public+"/"+tc.callback, func(t *testing.T) {
			ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ln.Close() })
			dir, err := os.MkdirTemp("", "auth-optional-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			addr := filepath.Join(dir, "ready.sock")
			ready, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: addr, Net: "unixgram"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ready.Close() })
			env := goodEnv()
			env["NOTIFY_SOCKET"] = addr
			if tc.public != "" {
				env["IKIGENBA_PUBLIC_URL"] = tc.public
			}
			if tc.callback != "" {
				env["IKIGENBA_CALLBACK_URL"] = tc.callback
			}
			p := baseProcess(env, testSource(t), ln)
			var lookups []string
			p.LookupEnv = func(key string) (string, bool) { lookups = append(lookups, key); v, ok := env[key]; return v, ok }
			p.OIDCIssuer = issuer.URL
			p.Rand = &synchronizedRand{}
			st, err := store.Open(p.DBSource, &synchronizedRand{})
			if err != nil {
				t.Fatal(err)
			}
			u, err := st.UpsertUserOnLogin("issuer", "subject", "user@example.test", p.Now())
			if err != nil {
				t.Fatal(err)
			}
			session, err := st.CreateSession(u.ID, p.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan int, 1)
			go func() { done <- Run(ctx, p) }()
			if err := ready.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			if _, _, err := ready.ReadFromUnix(make([]byte, 32)); err != nil {
				t.Fatal(err)
			}
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Timeout: 5 * time.Second}
			for _, host := range []string{"unrelated.example.test", "different.example.test:1234"} {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+ln.Addr().String()+"/login/google", nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Host = host
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusFound {
					t.Fatalf("login status=%d", resp.StatusCode)
				}
				if tc.callback != "" {
					loc, err := url.Parse(resp.Header.Get("Location"))
					if err != nil {
						t.Fatal(err)
					}
					if got := loc.Query().Get("redirect_uri"); got != tc.callback+"/login/google/callback" {
						t.Fatalf("redirect_uri=%q", got)
					}
				}
			}
			if tc.public != "" {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+ln.Addr().String()+"/logout", nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Host = strings.TrimPrefix(tc.public, "http://")
				req.Header.Set("Origin", tc.public)
				req.AddCookie(&http.Cookie{Name: server.SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusFound {
					t.Fatalf("logout status=%d", resp.StatusCode)
				}
			}
			cancel()
			if code := <-done; code != 0 {
				t.Fatalf("Run=%d diagnostic=%q", code, p.Stderr)
			}
			for _, key := range []string{"IKIGENBA_PUBLIC_URL", "IKIGENBA_CALLBACK_URL"} {
				if !slices.Contains(lookups, key) {
					t.Fatalf("missing lookup %s", key)
				}
			}
			if slices.Contains(lookups, "IKIGENBA_SANDBOX") {
				t.Fatal("sandbox variable looked up")
			}
		})
	}
}
