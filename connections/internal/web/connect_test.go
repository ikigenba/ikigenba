package web_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	connections "github.com/ikigenba/ikigenba/connections"
	"github.com/ikigenba/ikigenba/connections/internal/broker"
	"github.com/ikigenba/ikigenba/connections/internal/pages"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/trail"
	"github.com/ikigenba/ikigenba/connections/internal/urls"
	"github.com/ikigenba/ikigenba/connections/internal/web"
)

type oauthFixture struct {
	cfg    web.Config
	d      *db.DB
	st     *store.Store
	now    time.Time
	tc     *telemetry.Capture
	ec     *events.Capture
	forms  []url.Values
	status int
	body   string
	hook   func()
}

func oauthSetup(t *testing.T) *oauthFixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	f := &oauthFixture{now: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC), status: 200, body: `{"access_token":"new-access-token-sensitive","refresh_token":"new-refresh-token-sensitive","expires_in":3599,"id_token":"x.eyJlbWFpbCI6ImFjY291bnRAZXhhbXBsZSIsInN1YiI6InN1YmplY3QifQ.x"}`, tc: &telemetry.Capture{}, ec: &events.Capture{}}
	now := func() time.Time { return f.now }
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "state", "connections.db"), Migrations: connections.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	f.d = d
	t.Cleanup(func() { _ = d.Close() })
	f.st = store.New(d, store.Config{Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{12}, 1024))})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		f.forms = append(f.forms, r.Form)
		if f.hook != nil {
			f.hook()
		}
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, f.body)
	}))
	t.Cleanup(srv.Close)
	p := providers.Table()[0]
	p.AuthorizeURL = srv.URL + "/authorize"
	p.TokenURL = srv.URL + "/token"
	p.RevokeURL = srv.URL + "/revoke"
	tw := telemetry.New(telemetry.Config{Service: "connections", Sink: f.tc, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{13}, 4096))})
	em := events.New(events.Config{Service: "connections", Sink: f.ec, Emits: trail.Emits(), Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{14}, 4096))})
	t.Cleanup(func() { em.Shutdown(context.Background()); tw.Shutdown(context.Background(), "test") })
	f.cfg = web.Config{Banner: func(u page.User) page.Banner {
		return page.Banner{Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}, MCP: mcp.NewServer(mcp.ServerConfig{Name: "connections", Telemetry: tw}), Store: f.st, Clients: []providers.Client{{Provider: p, ID: "client", Secret: "client-secret-sensitive", HTTP: srv.Client(), Now: now}}, CallbackURL: "https://callback.example", Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{90}, 1024)), Telemetry: tw, Events: em}
	return f
}
func oauthRequest(h http.Handler, method, path, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://connections.example"+path, nil)
	r.Header.Set("X-User-Id", "user")
	r.Header.Set("X-User-Email", "user@example")
	r.Header.Set("X-Request-Id", "request")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func (f *oauthFixture) flush(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := f.cfg.Telemetry.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.cfg.Events.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}
func (f *oauthFixture) seed(t *testing.T) store.Connection {
	t.Helper()
	c, _, err := f.st.Upsert(context.Background(), store.Connection{UserID: "user", Provider: "google", AccountEmail: "account@example", RefreshToken: "old-refresh-token-sensitive", AccessToken: "old-access-token-sensitive", AccessExpires: f.now.Add(time.Minute), Scopes: []string{"openid"}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestOAuthConnectAndCallback(t *testing.T) {
	// R-RXXL-O39B R-RZ5I-1V00 R-QVH5-8UPI
	for _, method := range []string{"GET", "POST"} {
		t.Run(method, func(t *testing.T) {
			f := oauthSetup(t)
			if method == "POST" {
				f.cfg.CallbackURL = ""
				p := filepath.Join(t.TempDir(), "services.json")
				if err := os.WriteFile(p, []byte(`{"services":[{"name":"connections","url":"https://registered.example/","description":"","socket":"","enabled":true,"mcp":true}]}`), 0600); err != nil {
					t.Fatal(err)
				}
				f.cfg.ServicesPath = p
			}
			h := web.Handler(f.cfg)
			w := oauthRequest(h, method, "/google/connect", "https://connections.example")
			if w.Code != 302 || w.Body.Len() != 0 {
				t.Fatal(w.Code, w.Body.String())
			}
			u, err := url.Parse(w.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			want := f.cfg.CallbackURL
			if method == "POST" {
				want = "https://registered.example"
			}
			if u.Query().Get("redirect_uri") != want+"/google/callback" || !strings.HasPrefix(u.Query().Get("state"), broker.StatePrefix) {
				t.Fatal(u)
			}
			w = oauthRequest(h, "GET", "/google/callback?"+url.Values{"state": {u.Query().Get("state")}, "code": {"authorization-code-sensitive"}}.Encode(), "")
			if w.Code != 302 || w.Header().Get("Location") != "/" || w.Body.Len() != 0 {
				t.Fatal(w.Code, w.Body.String())
			}
			c, err := f.st.Get(context.Background(), "user", "google")
			if err != nil || c.AccountEmail != "account@example" {
				t.Fatal(c, err)
			}
			f.flush(t)
			if len(f.ec.Events()) != 1 || f.ec.Events()[0].Name != trail.Created {
				t.Fatal("events")
			}
		})
	}
}
func TestOAuthConnectRefusals(t *testing.T) {
	// R-S0DE-FMQP R-S1LA-TEHE
	for _, kind := range []string{"origin", "origin-healthy", "origin-no-callback", "callback", "random", "partial-random", "store"} {
		t.Run(kind, func(t *testing.T) {
			f := oauthSetup(t)
			rnd := &oauthReader{Reader: bytes.NewReader(bytes.Repeat([]byte{90}, 1024))}
			f.cfg.Rand = rnd
			method := "GET"
			origin := ""
			status := 500
			line := broker.NoRandom
			switch kind {
			case "origin", "origin-healthy", "origin-no-callback":
				method = "POST"
				if kind != "origin-healthy" {
					f.cfg.CallbackURL = ""
				}
				if kind == "origin" {
					f.d.SetFailing(true)
				}
				status = 403
				line = urls.CrossOrigin
			case "callback":
				f.cfg.CallbackURL = ""
				line = broker.NoCallback
			case "random":
				f.cfg.Rand = bytes.NewReader(nil)
			case "partial-random":
				f.cfg.Rand = bytes.NewReader(bytes.Repeat([]byte{90}, 47))
			case "store":
				f.d.SetFailing(true)
				status = 503
				line = store.Unreachable
			}
			w := oauthRequest(web.Handler(f.cfg), method, "/google/connect", origin)
			if w.Code != status || w.Body.String() != line+"\n" || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || len(f.forms) != 0 {
				t.Fatal(w.Code, w.Body.String())
			}
			if kind == "partial-random" {
				_, err := f.st.TakePending(context.Background(), broker.StatePrefix+strings.Repeat("5a", 16), "user", "google")
				if !errors.Is(err, store.ErrNotFound) {
					t.Fatal("pending stored", err)
				}
			}
			if strings.HasPrefix(kind, "origin") {
				if rnd.reads != 0 {
					t.Fatal("random read")
				}
				f.d.SetFailing(false)
				_, err := f.st.TakePending(context.Background(), broker.StatePrefix+strings.Repeat("5a", 16), "user", "google")
				if !errors.Is(err, store.ErrNotFound) {
					t.Fatal("pending stored", err)
				}
			}
			f.flush(t)
			for _, ev := range f.tc.Events() {
				if strings.HasPrefix(ev.Name, "connection.") {
					t.Fatal("connection telemetry")
				}
			}
			if len(f.ec.Events()) != 0 {
				t.Fatal("bus event")
			}
		})
	}
}
func TestOAuthCallbackFailures(t *testing.T) {
	// R-S413-KXYS R-QWP1-MMG7 R-S6GW-CHG6
	for _, kind := range []string{pages.FailureMissing, pages.FailureUnknown, pages.FailureExpired, pages.FailureForeign, pages.FailureDenied, pages.FailureExchange, pages.FailureAccount, "store"} {
		t.Run(kind, func(t *testing.T) {
			f := oauthSetup(t)
			q := url.Values{"state": {"pending-sensitive-state"}, "code": {"authorization-code-sensitive"}}
			pd := store.Pending{State: q.Get("state"), Verifier: "verifier-sensitive-value", UserID: "user", Provider: "google", Expires: f.now.Add(time.Hour)}
			switch kind {
			case pages.FailureMissing:
				q.Del("state")
			case pages.FailureForeign:
				pd.UserID = "other"
			case pages.FailureDenied:
				q.Set("error", "provider-secret-error-sensitive")
			case pages.FailureExchange:
				f.status = 400
			case pages.FailureAccount:
				f.body = `{"access_token":"access","refresh_token":"refresh","expires_in":60}`
			}
			if kind != pages.FailureUnknown {
				if err := f.st.PutPending(context.Background(), pd); err != nil {
					t.Fatal(err)
				}
			}
			if kind == pages.FailureExpired {
				f.now = f.now.Add(2 * time.Hour)
			}
			if kind == "store" {
				f.d.SetFailing(true)
			}
			w := oauthRequest(web.Handler(f.cfg), "GET", "/google/callback?"+q.Encode(), "")
			if kind == "store" {
				if w.Code != 503 || w.Body.String() != store.Unreachable+"\n" {
					t.Fatal(w.Code, w.Body.String())
				}
			} else {
				status := 400
				if kind == pages.FailureExchange || kind == pages.FailureAccount {
					status = 502
				}
				set, err := pages.Load()
				if err != nil {
					t.Fatal(err)
				}
				expected := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "https://connections.example/", nil)
				set.Write(expected, r, status, "notice", pages.NoticeData{Banner: f.cfg.Banner(page.User{Email: "user@example", ProfileURL: "https://auth.example/", LogoutURL: "https://auth.example/logout"}), DisplayName: f.cfg.Clients[0].Provider.DisplayName, Failure: kind})
				if w.Code != status || w.Body.String() != expected.Body.String() {
					t.Fatal(w.Code, "notice differs")
				}
				for _, secret := range []string{q.Get("state"), q.Get("code"), q.Get("error")} {
					if secret != "" && strings.Contains(w.Body.String(), secret) {
						t.Fatal("leak")
					}
				}
			}
			f.flush(t)
			for _, ev := range f.tc.Events() {
				if strings.HasPrefix(ev.Name, "connection.") {
					t.Fatal("connection telemetry")
				}
			}
			if len(f.ec.Events()) != 0 {
				t.Fatal("bus event")
			}
		})
	}
}

type oauthReader struct {
	Reader io.Reader
	reads  int
}

func (r *oauthReader) Read(p []byte) (int, error) { r.reads++; return r.Reader.Read(p) }
