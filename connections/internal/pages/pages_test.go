package pages_test

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	connections "github.com/ikigenba/ikigenba/connections"
	"github.com/ikigenba/ikigenba/connections/internal/broker"
	"github.com/ikigenba/ikigenba/connections/internal/pages"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/tools"
	"github.com/ikigenba/ikigenba/connections/internal/trail"
	"github.com/ikigenba/ikigenba/connections/internal/urls"
)

func fixedBanner(u page.User) page.Banner {
	return page.Banner{Service: pages.ServiceName, Release: "test-release", Commit: "test-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Tools: true, Trail: []page.Level{{Name: "previous", URL: "/previous"}}}
}
func templates(t *testing.T) *template.Template {
	t.Helper()
	s, err := page.Templates().ParseFS(connections.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func rendered(t *testing.T, s *template.Template, name string, data any) string {
	t.Helper()
	var b bytes.Buffer
	if err := s.ExecuteTemplate(&b, name, data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
func request(method, path string) *http.Request {
	r := httptest.NewRequest(method, "https://connections.sbx.ikigenba.dev:443"+path, nil)
	r.Header.Set("X-User-Id", "caller")
	r.Header.Set("X-User-Email", "caller@example.test")
	r.Header.Set("X-Request-Id", "request-example")
	r.Header.Set("X-Forwarded-Proto", "https")
	return r
}
func answer(t *testing.T, h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatal("page answer set cookie", w.Header())
	}
	return w
}
func assertPage(t *testing.T, w *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if w.Code != status || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Body.String() != body {
		t.Fatalf("status %d headers %v body differs %t", w.Code, w.Header(), w.Body.String() != body)
	}
	if len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatal(w.Header())
	}
}
func assertPlain(t *testing.T, w *httptest.ResponseRecorder, r *http.Request, status int, line string) {
	t.Helper()
	want := line + "\n"
	if r.Method == http.MethodHead {
		want = ""
	}
	if w.Code != status || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || w.Body.String() != want {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
}

// R-B48J-W8ZW R-B6OC-NSHA R-B7W9-1K7Z R-B945-FBYO R-BAC1-T3PD
func TestTemplatesAndWrite(t *testing.T) {
	if pages.ServiceName != "connections" || pages.Description != "Accounts at outside providers, connected for the suite's connectors" || pages.ToolsLevel != (page.Level{Name: "tools", URL: "/tools"}) || pages.AboutLevel != (page.Level{Name: "about", URL: "/about"}) {
		t.Fatal("public constants")
	}
	s, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	own := templates(t)
	b := fixedBanner(page.User{Email: "chosen@example.test"})
	data := []struct {
		name  string
		value any
	}{{"landing", pages.LandingData{Banner: b}}, {"tools", pages.ToolsData{Banner: b, Tools: []pages.ToolRow{{Name: "supplied-name", Description: "supplied-description"}}}}, {"about", pages.AboutData{Banner: b, Description: pages.Description}}, {"notfound", pages.NotFoundData{Banner: b}}}
	for _, status := range []string{tools.StatusConnected, tools.StatusBroken, tools.StatusNotConnected} {
		data = append(data, struct {
			name  string
			value any
		}{"landing", pages.LandingData{Banner: b, Connections: []pages.ConnectionRow{{Name: "chosen-provider", DisplayName: "chosen-display", Status: status, Account: "chosen-account@example.test", Scopes: []string{"chosen-scope"}, Connected: "chosen-connected", ConnectedText: "chosen-time"}}}})
	}
	failures := []string{pages.FailureMissing, pages.FailureUnknown, pages.FailureExpired, pages.FailureForeign, pages.FailureDenied, pages.FailureExchange, pages.FailureAccount}
	if !reflect.DeepEqual(failures, []string{"missing", "unknown", "expired", "foreign", "denied", "exchange", "account"}) {
		t.Fatal(failures)
	}
	for _, f := range failures {
		data = append(data, struct {
			name  string
			value any
		}{"notice", pages.NoticeData{Banner: b, DisplayName: "chosen-provider", Failure: f}})
	}
	for _, d := range data {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			r := request(method, "/")
			w := httptest.NewRecorder()
			s.Write(w, r, 409, d.name, d.value)
			want := rendered(t, own, d.name, d.value)
			if method == http.MethodHead {
				want = ""
			}
			assertPage(t, w, 409, want)
		}
	}
	if rendered(t, own, "notice", pages.NoticeData{Banner: b, DisplayName: "chosen-provider", Failure: failures[0]}) == rendered(t, own, "notice", pages.NoticeData{Banner: b, DisplayName: "chosen-provider", Failure: failures[1]}) {
		t.Fatal("failures indistinguishable")
	}
}

type fixture struct {
	cfg        pages.Config
	d          *db.DB
	st         *store.Store
	tc         *telemetry.Capture
	ec         *events.Capture
	w          *telemetry.Writer
	em         *events.Emitter
	p          providers.Provider
	calls      []string
	status     int
	failDelete bool
	now        time.Time
	own        *template.Template
	upstream   *httptest.Server
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{tc: &telemetry.Capture{}, ec: &events.Capture{}, status: 200, now: time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC), own: templates(t)}
	now := func() time.Time { return f.now }
	var err error
	f.d, err = db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "connections.db"), Migrations: connections.Migrations(), Now: now, Service: "connections", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.d.Close() })
	f.st = store.New(f.d, store.Config{Now: now, Rand: bytes.NewReader([]byte("abcdefghABCDEFGHijklmnopIJKLMNOPqrstuvwxQRSTUVWX"))})
	f.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		f.calls = append(f.calls, r.Form.Get("token"))
		if f.failDelete {
			f.d.SetFailing(true)
		}
		w.WriteHeader(f.status)
	}))
	t.Cleanup(f.upstream.Close)
	f.p = providers.Provider{Name: "google", DisplayName: "chosen-display", RevokeURL: f.upstream.URL, Scopes: []string{"scope-one"}}
	f.w = telemetry.New(telemetry.Config{Service: "connections", Sink: f.tc, Now: now, Stderr: io.Discard, Rand: bytes.NewReader(make([]byte, 4096))})
	f.em = events.New(events.Config{Service: "connections", Sink: f.ec, Emits: trail.Emits(), Now: now, Stderr: io.Discard, Rand: bytes.NewReader(make([]byte, 4096))})
	t.Cleanup(func() { f.em.Shutdown(context.Background()); f.w.Shutdown(context.Background(), "") })
	b := broker.New(broker.Config{Clients: []providers.Client{{Provider: f.p, HTTP: f.upstream.Client(), Now: now}}, Store: f.st, Telemetry: f.w, Events: f.em, Now: now})
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	f.cfg = pages.Config{Banner: fixedBanner, Pages: set, Providers: []providers.Provider{f.p, {Name: "second", DisplayName: "second-display"}}, Store: f.st, Broker: b}
	return f
}
func (f *fixture) handler() http.Handler { return identity.Require(pages.Handler(f.cfg)) }
func (f *fixture) put(t *testing.T, user string) store.Connection {
	t.Helper()
	c, _, err := f.st.Upsert(context.Background(), store.Connection{UserID: user, UserEmail: user + "@example.test", Provider: f.p.Name, AccountEmail: user + "-account@example.test", RefreshToken: user + "-refresh-secret", AccessToken: user + "-access-secret", Scopes: []string{"scope-one", "extra"}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func (f *fixture) banner(r *http.Request) page.Banner {
	a := urls.Auth(r, f.cfg.ServicesPath)
	return fixedBanner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: a + "/", LogoutURL: a + "/logout"})
}
func (f *fixture) landing(r *http.Request, c *store.Connection) pages.LandingData {
	b := f.banner(r)
	b.Trail = nil
	d := pages.LandingData{Banner: b, Connections: []pages.ConnectionRow{{Name: f.p.Name, DisplayName: f.p.DisplayName, Status: tools.StatusNotConnected}, {Name: "second", DisplayName: "second-display", Status: tools.StatusNotConnected}}}
	if c != nil {
		row := &d.Connections[0]
		row.Status = tools.StatusOf(f.p, *c)
		row.Account = c.AccountEmail
		row.Scopes = c.Scopes
		row.Connected = c.Created.UTC().Format(time.RFC3339)
		row.ConnectedText = c.Created.UTC().Format("2006-01-02 15:04")
	}
	return d
}
func (f *fixture) flush(t *testing.T) {
	t.Helper()
	if err := f.w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.em.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// R-B5GG-A0QL R-BBJY-6VG2 R-BDZQ-YEXG R-BF7N-C6O5 R-BHNG-3Q5J R-BIVC-HHW8 R-BK38-V9MX R-BLB5-91DM R-BOYU-ECLP
func TestReadPages(t *testing.T) {
	f := setup(t)
	other := f.put(t, "other")
	h := f.handler()
	r := request(http.MethodGet, "/")
	w := answer(t, h, r)
	assertPage(t, w, 200, rendered(t, f.own, "landing", f.landing(r, nil)))
	if strings.Contains(w.Body.String(), other.AccountEmail) {
		t.Fatal("other user leaked")
	}
	c := f.put(t, "caller")
	pending := store.Pending{State: "pending-example", Verifier: "private-verifier", UserID: "caller", Provider: "google", Expires: f.now.Add(time.Hour)}
	if err := f.st.PutPending(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/tools", "/about", "/nope/disconnect", "/goo%67le/disconnect", "/unknown"} {
		r = request(http.MethodGet, path)
		b := f.banner(r)
		name := ""
		var data any
		status := 200
		switch path {
		case "/":
			name = "landing"
			data = f.landing(r, &c)
		case "/tools":
			name = "tools"
			b.Trail = []page.Level{pages.ToolsLevel}
			d := pages.ToolsData{Banner: b}
			for _, e := range tools.Catalog() {
				d.Tools = append(d.Tools, pages.ToolRow{Name: e.Name, Description: tools.FirstLine(e.Description)})
			}
			data = d
		case "/about":
			name = "about"
			b.Trail = []page.Level{pages.AboutLevel}
			data = pages.AboutData{Banner: b, Description: pages.Description}
		default:
			name = "notfound"
			status = 404
			data = pages.NotFoundData{Banner: b}
		}
		get := answer(t, h, r)
		assertPage(t, get, status, rendered(t, f.own, name, data))
		head := answer(t, h, request(http.MethodHead, path))
		assertPage(t, head, status, "")
		if !reflect.DeepEqual(get.Header(), head.Header()) {
			t.Fatal("HEAD headers")
		}
		if status == 404 {
			assertPage(t, answer(t, h, request(http.MethodPost, path)), status, rendered(t, f.own, name, data))
		}
	}
	got, err := f.st.Get(context.Background(), "caller", "google")
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatal(got, err)
	}
	got, err = f.st.Get(context.Background(), "other", "google")
	if err != nil || !reflect.DeepEqual(got, other) {
		t.Fatal(got, err)
	}
	pd, err := f.st.TakePending(context.Background(), pending.State, "caller", "google")
	if err != nil || !reflect.DeepEqual(pd, pending) {
		t.Fatal(pd, err)
	}
	f.flush(t)
	if len(f.calls) != 0 || len(f.tc.Events()) != 0 || len(f.ec.Events()) != 0 {
		t.Fatal("read side effects")
	}
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "inferred", true: "configured"}[configured], func(t *testing.T) {
			if configured {
				f.cfg.ServicesPath = filepath.Join(t.TempDir(), "services.json")
				if err := os.WriteFile(f.cfg.ServicesPath, []byte(`{"services":[{"name":"auth","url":"https://auth.example.test/","description":"","socket":"","enabled":true,"mcp":false}]}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var user page.User
			f.cfg.Banner = func(u page.User) page.Banner { user = u; return fixedBanner(u) }
			r := request(http.MethodGet, "/about")
			answer(t, f.handler(), r)
			profile := "https://auth.sbx.ikigenba.dev/"
			if configured {
				profile = "https://auth.example.test/"
			}
			if user != (page.User{Email: "caller@example.test", ProfileURL: profile, LogoutURL: strings.TrimSuffix(profile, "/") + "/logout"}) {
				t.Fatal(user)
			}
		})
	}
}

// R-BCRU-KN6R R-BMJ1-MT4B R-BNQY-0KV0 R-BSMJ-JNTS R-WK8D-09P9 R-QNA0-U0KK
func TestRefusalsAndMethods(t *testing.T) {
	f := setup(t)
	c := f.put(t, "caller")
	f.d.SetFailing(true)
	h := f.handler()
	for _, path := range []string{"/", "/tools", "/about", "/google/disconnect"} {
		for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
			w := answer(t, h, request(method, path))
			allow := "GET, HEAD"
			if path == "/google/disconnect" {
				allow = "POST"
			}
			if w.Code != 405 || w.Header().Get("Allow") != allow || w.Body.Len() != 0 {
				t.Fatal(w.Code, w.Header(), w.Body.String())
			}
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r := request(method, "/")
		assertPlain(t, answer(t, h, r), r, 503, store.Unreachable)
	}
	for _, path := range []string{"/tools", "/about", "/unknown"} {
		r := request(http.MethodGet, path)
		got := answer(t, h, r)
		f.d.SetFailing(false)
		want := answer(t, h, r)
		f.d.SetFailing(true)
		if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
			t.Fatal(path)
		}
	}
	r := request(http.MethodPost, "/google/disconnect")
	r.Header.Set("Origin", "https://connections.sbx.ikigenba.dev:443")
	assertPlain(t, answer(t, h, r), r, 503, store.Unreachable)
	for _, origin := range []string{"", "https://other.example.test", "https://connections.sbx.ikigenba.dev:444"} {
		for _, failing := range []bool{true, false} {
			f.d.SetFailing(failing)
			r := request(http.MethodPost, "/google/disconnect")
			r.Header.Set("Origin", origin)
			assertPlain(t, answer(t, h, r), r, 403, urls.CrossOrigin)
		}
	}
	f.flush(t)
	if len(f.calls) != 0 || len(f.tc.Events()) != 0 || len(f.ec.Events()) != 0 {
		t.Fatal("refusal side effects")
	}
	f.d.SetFailing(false)
	got, err := f.st.Get(context.Background(), "caller", "google")
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatal(got, err)
	}
	f.cfg.Providers = nil
	f.d.SetFailing(true)
	r = request(http.MethodPost, "/google/disconnect")
	assertPage(t, answer(t, f.handler(), r), 404, rendered(t, f.own, "notfound", pages.NotFoundData{Banner: f.banner(r)}))
}

// R-WHSK-8Q7V R-QKU8-2H36 R-WLG9-E1FY
func TestDisconnect(t *testing.T) {
	for _, status := range []int{200, 400, 0} {
		for _, fail := range []bool{false, true} {
			t.Run(strconv.Itoa(status)+map[bool]string{true: "fail", false: "ok"}[fail], func(t *testing.T) {
				f := setup(t)
				c := f.put(t, "caller")
				f.status = status
				f.failDelete = fail
				if status == 0 {
					f.upstream.Close()
					if fail {
						f.cfg.Broker = broker.New(broker.Config{Clients: []providers.Client{{Provider: f.p, HTTP: &http.Client{Transport: failingTransport{d: f.d}}, Now: func() time.Time { return f.now }}}, Store: f.st, Telemetry: f.w, Events: f.em, Now: func() time.Time { return f.now }})
					}
				}
				r := request(http.MethodPost, "/google/disconnect")
				r.Header.Set("Origin", "https://connections.sbx.ikigenba.dev:443")
				cause := events.Cause{ID: "evt_0123456789abcdef", Depth: 2}
				r = r.WithContext(events.NewContext(r.Context(), cause))
				w := answer(t, f.handler(), r)
				if fail {
					assertPlain(t, w, r, 503, store.Unreachable)
				} else {
					if w.Code != 302 || w.Header().Get("Location") != "/" || w.Body.Len() != 0 {
						t.Fatal(w.Code, w.Header(), w.Body.String())
					}
					get := request(http.MethodGet, "/")
					assertPage(t, answer(t, f.handler(), get), 200, rendered(t, f.own, "landing", f.landing(get, nil)))
				}
				f.flush(t)
				if status != 0 && !reflect.DeepEqual(f.calls, []string{c.RefreshToken}) {
					t.Fatal(f.calls)
				}
				deleted, revoke := 0, 0
				for _, e := range f.tc.Events() {
					switch e.Name {
					case trail.Deleted:
						deleted++
						if !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"provider": c.Provider, "account": c.AccountEmail, "scopes": strings.Join(c.Scopes, " ")}) || e.User != "caller" || e.RequestID != "request-example" {
							t.Fatal(e)
						}
					case trail.RevokeFailed:
						revoke++
						if !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"provider": c.Provider, "account": c.AccountEmail, "status": int64(status)}) || e.User != "caller" || e.RequestID != "request-example" {
							t.Fatal(e)
						}
					}
				}
				wantDeleted := 1
				if fail {
					wantDeleted = 0
				}
				wantRevoke := 0
				if status != 200 {
					wantRevoke = 1
				}
				if deleted != wantDeleted || revoke != wantRevoke || len(f.ec.Events()) != wantDeleted {
					t.Fatal(deleted, revoke, f.ec.Events())
				}
				if !fail {
					e := f.ec.Events()[0]
					if !reflect.DeepEqual(e.Attrs, events.Attrs{"provider": c.Provider, "account": c.AccountEmail, "scopes": strings.Join(c.Scopes, " ")}) || e.Name != trail.Deleted || e.Cause != cause.ID || e.Depth != 3 || e.User != "caller" || e.RequestID != "request-example" {
						t.Fatal(e)
					}
				}
				f.d.SetFailing(false)
				_, err := f.st.Get(context.Background(), "caller", "google")
				if fail && err != nil || !fail && !errors.Is(err, store.ErrNotFound) {
					t.Fatal(err)
				}
			})
		}
	}
	f := setup(t)
	other := f.put(t, "other")
	r := request(http.MethodPost, "/google/disconnect")
	r.Header.Set("Origin", "https://connections.sbx.ikigenba.dev:443")
	w := answer(t, f.handler(), r)
	if w.Code != 302 || w.Header().Get("Location") != "/" || w.Body.Len() != 0 {
		t.Fatal(w.Code, w.Header())
	}
	f.flush(t)
	got, err := f.st.Get(context.Background(), "other", "google")
	if err != nil || !reflect.DeepEqual(got, other) || len(f.calls) != 0 || len(f.tc.Events()) != 0 || len(f.ec.Events()) != 0 {
		t.Fatal(got, err)
	}
}

type failingTransport struct{ d *db.DB }

func (f failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.d.SetFailing(true)
	return nil, errors.New("injected transport failure")
}

func TestLandingBrokenStatus(t *testing.T) {
	f := setup(t)
	c := f.put(t, "caller")
	for _, broken := range []bool{false, true} {
		if broken {
			var err error
			c, err = f.st.MarkBroken(context.Background(), "caller", "google")
			if err != nil {
				t.Fatal(err)
			}
		} else {
			var err error
			c, err = f.st.SetAccess(context.Background(), "caller", "google", store.Access{Token: "supplied-token", Expires: f.now.Add(time.Hour), Scopes: []string{"extra"}})
			if err != nil {
				t.Fatal(err)
			}
		}
		r := request(http.MethodGet, "/")
		assertPage(t, answer(t, f.handler(), r), 200, rendered(t, f.own, "landing", f.landing(r, &c)))
	}
}
