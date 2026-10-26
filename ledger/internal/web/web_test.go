package web_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/ledger"
	"github.com/ikigenba/ikigenba/ledger/internal/pages"
	"github.com/ikigenba/ikigenba/ledger/internal/settings"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
	"github.com/ikigenba/ikigenba/ledger/internal/trail"
	"github.com/ikigenba/ikigenba/ledger/internal/web"
)

func fixture(t *testing.T) (web.Config, *telemetry.Capture, *events.Capture) {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := func() time.Time { return time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC) }
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "db"), Migrations: ledger.Migrations(), Now: now})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = d.Close() })
	rnd := bytes.NewReader(bytes.Repeat([]byte{0x5a}, 100000))
	tc, ec := &telemetry.Capture{}, &events.Capture{}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: tc, Now: now, Rand: rnd, Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	em := events.New(events.Config{Service: pages.ServiceName, Sink: ec, Now: now, Rand: rnd, Telemetry: w, Emits: trail.Emits(), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { em.Shutdown(context.Background()); w.Shutdown(context.Background(), "test") })
	return web.Config{func(u page.User) page.Banner {
		return page.Banner{Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}, mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w}), "", store.New(d, store.Config{Now: now, Rand: rnd}), w, em, nil, settings.Defaults()}, tc, ec
}
func request(h http.Handler, method, path, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://ledger.test"+path, nil)
	r.Header.Set("X-User-Id", user)
	r.Header.Set("X-User-Email", "mgreenly+fake00@gmail.com")
	r.Header.Set("X-Request-Id", "chosen-request")
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func same(t *testing.T, a, b *httptest.ResponseRecorder) {
	t.Helper()
	if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Body.String() != b.Body.String() {
		t.Fatalf("answers differ: %d %v %q / %d %v %q", a.Code, a.Header(), a.Body.String(), b.Code, b.Header(), b.Body.String())
	}
}

// R-88TK-FJRO R-8A1G-TBID R-8B9D-7392 R-8DP5-YMQG R-8EX2-CEH5 R-8HCV-3XYJ R-8IKR-HPP8 R-8JSN-VHFX
func TestRouteDispatch(t *testing.T) {
	cfg, tc, ec := fixture(t)
	h := web.Handler(cfg)
	ps, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	ph := identity.Optional(pages.Handler(pages.Config{Banner: cfg.Banner, Pages: ps, Store: cfg.Store, MCP: cfg.MCP}))
	for _, user := range []string{"", "owner"} {
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
			for _, path := range []string{"/declarations", "/events", "/mcp", "/_appkit/theme.css", "/", "/about", "/tools", "/nope", "/t/x", "/mcp/", "/events/", "/declarations/", "/_appkit"} {
				if path == "/mcp" && user != "" {
					continue
				}
				a := request(h, method, path, user)
				if len(a.Header().Values("Set-Cookie")) != 0 {
					t.Fatal("cookie")
				}
				var expected http.Handler
				switch path {
				case "/declarations":
					expected = events.DeclarationsHandler(cfg.Events, nil)
				case "/events":
					expected = events.DeliveryHandler(nil)
				case "/mcp":
					expected = identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("guest reached MCP") }))
				case "/_appkit/theme.css":
					expected = page.Static()
				default:
					expected = ph
				}
				same(t, a, request(expected, method, path, user))
			}
		}
	}
	if e = cfg.Telemetry.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, event := range tc.Events() {
		if event.Name != "request.started" && event.Name != "request.finished" {
			t.Fatal(event)
		}
	}
	if len(ec.Events()) != 0 {
		t.Fatal("bus event")
	}
}

func TestDeclarationsIgnoreRequestInputs(t *testing.T) {
	cfg, _, _ := fixture(t)
	h := web.Handler(cfg)
	expected := events.DeclarationsHandler(cfg.Events, nil)
	for _, method := range []string{"GET", "HEAD", "POST", "DELETE", "OPTIONS"} {
		for _, users := range [][]string{nil, {""}, {"owner", "other"}} {
			for _, target := range []string{"/declarations", "/declarations?query=chosen"} {
				makeRequest := func() *http.Request {
					r := httptest.NewRequest(method, target, bytes.NewBufferString("body-chosen"))
					if users != nil {
						r.Header["X-User-Id"] = users
					}
					r.Header.Set("X-User-Email", "mgreenly+fake00@gmail.com")
					r.Header.Set("X-Request-Id", "declarations-request")
					r.Header.Set("X-Custom", "header-chosen")
					return r
				}
				a, b := httptest.NewRecorder(), httptest.NewRecorder()
				h.ServeHTTP(a, makeRequest())
				expected.ServeHTTP(b, makeRequest())
				same(t, a, b)
			}
		}
	}
}

// R-8G4Y-Q67U R-8CH9-KUZR
func TestMCPCallerAndCause(t *testing.T) {
	cfg, tc, ec := fixture(t)
	h := web.Handler(cfg)
	if len(cfg.MCP.Tools()) != 23 {
		t.Fatal(cfg.MCP.Tools())
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	transport := &causeTransport{base: http.DefaultTransport}
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: transport}})
	caller := identity.Caller{UserID: "chosen-user", Email: "chosen@example.test", RequestID: "chosen-request"}
	call := func(name, args string) {
		result, e := client.CallTool(context.Background(), caller, name, []byte(args))
		if e != nil || result.IsError() {
			t.Fatal(result, e)
		}
	}
	call("open_account", `{"name":"Assets:Bank","date":"2026-10-05"}`)
	call("open_account", `{"name":"Equity:Opening","date":"2026-10-05"}`)
	call("post", `{"date":"2026-10-05","postings":[{"account":"Assets:Bank","amount":"1.00"},{"account":"Equity:Opening","amount":"-1.00"}]}`)
	if e := cfg.Events.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	events := ec.Events()
	if len(events) != 1 || events[0].Cause != "evt_8c3f1a6e2d9b4075" || events[0].Depth != 2 {
		t.Fatal(events)
	}
	if e := cfg.Telemetry.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, e := range tc.Events() {
		if e.RequestID != caller.RequestID || e.User != caller.UserID {
			t.Fatal(e)
		}
	}
}

type causeTransport struct{ base http.RoundTripper }

func (t *causeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Event-Cause", "evt_8c3f1a6e2d9b4075")
	r.Header.Set("X-Event-Depth", "1")
	return t.base.RoundTrip(r)
}

// R-8CH9-KUZR
func TestRequestHeaders(t *testing.T) {
	for _, tc := range []struct {
		users, emails, ids []string
		user, id           string
	}{{[]string{"first", "second"}, []string{"one@example.test", "two@example.test"}, []string{"first-request", "second-request"}, "first", "first-request"}, {[]string{"", "second"}, []string{"guest@example.test"}, []string{"", "second-request"}, "", ""}, {nil, nil, nil, "", ""}} {
		cfg, capture, _ := fixture(t)
		h := web.Handler(cfg)
		req := httptest.NewRequest("GET", "http://ledger.test/", nil)
		if tc.users != nil {
			req.Header["X-User-Id"] = tc.users
		}
		if tc.emails != nil {
			req.Header["X-User-Email"] = tc.emails
		}
		if tc.ids != nil {
			req.Header["X-Request-Id"] = tc.ids
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if tc.user == "" && rec.Code != 302 {
			t.Fatal(rec.Code)
		}
		if tc.user != "" && rec.Code != 200 {
			t.Fatal(rec.Code)
		}
		if e := cfg.Telemetry.Flush(context.Background()); e != nil {
			t.Fatal(e)
		}
		events := capture.Events()
		if len(events) != 2 {
			t.Fatal(events)
		}
		for _, e := range events {
			if e.User != tc.user {
				t.Fatal(e)
			}
			if tc.id != "" {
				if e.RequestID != tc.id {
					t.Fatal(e)
				}
			} else if e.RequestID != "5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a" {
				t.Fatal(e)
			}
		}
	}
}
