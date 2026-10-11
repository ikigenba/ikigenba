package web_test

import (
	"context"
	"encoding/json"
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
	"github.com/ikigenba/ikigenba/gdrive"
	"github.com/ikigenba/ikigenba/gdrive/internal/pages"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
	"github.com/ikigenba/ikigenba/gdrive/internal/tools"
	"github.com/ikigenba/ikigenba/gdrive/internal/trail"
	"github.com/ikigenba/ikigenba/gdrive/internal/transfers"
	"github.com/ikigenba/ikigenba/gdrive/internal/web"
)

type sequence struct{ n byte }

func (s *sequence) Read(p []byte) (int, error) {
	for i := range p {
		s.n++
		p[i] = s.n
	}
	return len(p), nil
}

type fixture struct {
	cfg     web.Config
	capture *telemetry.Capture
	d       *db.DB
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	capture := &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: "gdrive", Sink: capture, Stderr: io.Discard, Now: func() time.Time { return now }, Rand: &sequence{}})
	t.Cleanup(func() { w.Shutdown(context.Background(), "done") })
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "gdrive.db"), Migrations: gdrive.Migrations(), Now: func() time.Time { return now }})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = d.Close() })
	st := store.New(d, store.Config{Now: func() time.Time { return now }, Rand: &sequence{}})
	em := events.New(events.Config{Service: "gdrive", Sink: &events.Capture{}, Stderr: io.Discard, Telemetry: w, Now: func() time.Time { return now }, Rand: &sequence{}, Emits: trail.Emits()})
	t.Cleanup(func() { em.Shutdown(context.Background()) })
	srv := mcp.NewServer(mcp.ServerConfig{Name: "gdrive", Telemetry: w})
	return fixture{cfg: web.Config{Banner: func(u page.User) page.Banner {
		return page.Banner{Service: pages.ServiceName, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}, MCP: srv, Store: st, Transfers: transfers.New(transfers.Config{Store: st, Telemetry: w, Events: em, Now: func() time.Time { return now }}), Telemetry: w, Events: em}, capture: capture, d: d}
}
func request(h http.Handler, method, path, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://gdrive.example.test"+path, nil)
	if user != "" {
		r.Header.Set("X-User-Id", user)
		r.Header.Set("X-User-Email", user+"@example.test")
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Request-Id", "fixture-request")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func same(t *testing.T, a, b *httptest.ResponseRecorder) {
	t.Helper()
	if a.Code != b.Code || a.Body.String() != b.Body.String() || !reflect.DeepEqual(a.Header(), b.Header()) {
		t.Fatalf("responses differ: %d/%d %s/%s", a.Code, b.Code, a.Body.String(), b.Body.String())
	}
}

type unread struct{ t *testing.T }

func (r unread) Read([]byte) (int, error) { r.t.Fatal("unauthenticated body read"); return 0, io.EOF }
func (unread) Close() error               { return nil }
func TestRoutingIdentityAndCookies(t *testing.T) {
	// R-6KIB-YA5D R-B0TN-IWMD R-B21J-WOD2 R-B4HC-O7UG R-B6X5-FRBU
	f := newFixture(t)
	h := web.Handler(f.cfg)
	f.d.SetFailing(true)
	for _, path := range []string{"/", "/about", "/tools", "/xfr_5a5a5a5a5a5a5a5a", "/_appkit/theme.css", "/mcp", "/events/"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			r := httptest.NewRequest(method, path, unread{t})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := identity.MissingBody
			if method == "HEAD" {
				want = ""
			}
			if w.Code != 500 || w.Body.String() != want || len(w.Header().Values("Content-Type")) != 1 || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Header().Get("Set-Cookie") != "" {
				t.Fatal("identity", method, path)
			}
		}
	}
	if e := f.cfg.Telemetry.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	ev := f.capture.Events()
	if len(ev) != 42 {
		t.Fatal("missing identity events", len(ev))
	}
	for _, e := range ev {
		if e.Name != "request.started" && e.Name != "request.finished" {
			t.Fatal("missing identity side effect", e)
		}
	}
	for _, path := range []string{"/events", "/declarations"} {
		expected := events.DeliveryHandler(nil)
		if path == "/declarations" {
			expected = events.DeclarationsHandler(f.cfg.Events, nil)
		}
		for _, user := range []string{"", "alice"} {
			for _, method := range []string{"GET", "HEAD", "POST"} {
				same(t, request(h, method, path, user), request(expected, method, path, user))
			}
		}
	}
	f.d.SetFailing(false)
	s, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	ph := identity.Require(pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: s, Store: f.cfg.Store}))
	for _, path := range []string{"/", "/about", "/tools", "/xfr_5a5a5a5a5a5a5a5a", "/nope", "/mcp/", "/events/", "/declarations/", "/_appkit"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			got := request(h, method, path, "alice")
			same(t, got, request(ph, method, path, "alice"))
			if got.Header().Get("Set-Cookie") != "" {
				t.Fatal("cookie")
			}
		}
	}
	for _, path := range []string{"/_appkit/theme.css", "/_appkit/unknown"} {
		same(t, request(h, "GET", path, "alice"), request(page.Static(), "GET", path, "alice"))
	}
}
func TestMCPRegistrationCauseAndRequestTrail(t *testing.T) {
	// R-6LQ8-C1W2 R-AZLR-54VO R-B5P9-1ZL5
	f := newFixture(t)
	h := web.Handler(f.cfg)
	srv := httptest.NewServer(h)
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client()})
	caller := identity.Caller{UserID: "alice", Email: "alice@example.test", RequestID: "fixture-mcp"}
	infos, e := client.ListTools(context.Background(), caller)
	if e != nil {
		t.Fatal(e)
	}
	if len(infos) != 8 {
		t.Fatal("registration count")
	}
	for i, info := range infos {
		entry := tools.Catalog()[i]
		if info.Name != entry.Name || info.Description != entry.Description+"\n\n"+tools.TransferGuide {
			t.Fatal("registration metadata")
		}
	}
	r, e := client.CallTool(context.Background(), caller, "transfers", json.RawMessage(`{}`))
	if e != nil || r.IsError() {
		t.Fatal(r, e)
	}
	for _, path := range []string{"/about", "/declarations", "/_appkit/theme.css", "/events/"} {
		request(h, "GET", path, "alice")
	}
	for _, id := range []string{"", "explicit-first"} {
		r := httptest.NewRequest("GET", "/about", nil)
		r.Header["X-Request-Id"] = []string{id, "ignored-second"}
		r.Header["X-User-Id"] = []string{"first-user", "second-user"}
		r.Header["X-User-Email"] = []string{"first@example.test", "second@example.test"}
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
	if e = f.cfg.Telemetry.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	ev := f.capture.Events()
	started := map[string]telemetry.Event{}
	finished := map[string]telemetry.Event{}
	minted := false
	startCount, finishCount := 0, 0
	for _, e := range ev {
		if e.Name == "request.started" {
			startCount++
			if _, ok := started[e.RequestID]; ok && e.RequestID != "fixture-request" && e.RequestID != "fixture-mcp" {
				t.Fatal("duplicate started")
			}
			started[e.RequestID] = e
			if e.RequestID != "fixture-request" && e.RequestID != "fixture-mcp" && e.RequestID != "explicit-first" {
				if e.RequestID == "" || e.RequestID == "ignored-second" || e.User != "first-user" {
					t.Fatal("minted identity", e)
				}
				minted = true
			}
			if e.Attrs["method"] != "GET" && e.Attrs["method"] != "POST" {
				t.Fatal("request method")
			}
			if _, ok := e.Attrs["path"]; !ok {
				t.Fatal("path")
			}
		}
		if e.Name == "request.finished" {
			finishCount++
			finished[e.RequestID] = e
			if _, ok := e.Attrs["status"]; !ok {
				t.Fatal("status")
			}
		}
		if e.RequestID == "fixture-mcp" && e.User != caller.UserID {
			t.Fatal("tool identity")
		}
	}
	if startCount != 8 || finishCount != 8 {
		t.Fatal("request event counts", startCount, finishCount)
	}
	if !minted || started["explicit-first"].User != "first-user" {
		t.Fatal("request id selection")
	}
	for id := range started {
		if _, ok := finished[id]; !ok {
			t.Fatal("unfinished request", id)
		}
	}
	// A contextual probe uses the same MCP handler to observe the event middleware at the tool boundary.
	f2 := newFixture(t)
	h2 := web.Handler(f2.cfg)
	var seen events.Cause
	var gotCaller identity.Caller
	mcp.AddRawTool(f2.cfg.MCP, mcp.RawTool[tools.NoArgs]{Name: "probe", Description: "Observe the supplied event cause.", Effect: mcp.Read, Handler: func(ctx context.Context, c identity.Caller, _ tools.NoArgs) (mcp.Result, error) {
		seen, _ = events.FromContext(ctx)
		gotCaller = c
		return mcp.TextResult("fixture"), nil
	}})
	hs := httptest.NewServer(h2)
	defer hs.Close()
	transport := causeTransport{base: hs.Client().Transport}
	c2 := mcp.NewClient(mcp.ClientConfig{Endpoint: hs.URL + "/mcp", HTTPClient: &http.Client{Transport: transport}})
	if _, e = c2.CallTool(context.Background(), caller, "probe", json.RawMessage(`{}`)); e != nil {
		t.Fatal(e)
	}
	if seen.ID != "evt_0123456789abcdef" || seen.Depth != 3 || gotCaller != caller {
		t.Fatal("cause/caller", seen, gotCaller)
	}
}

type causeTransport struct{ base http.RoundTripper }

func (c causeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Event-Cause", "evt_0123456789abcdef")
	r.Header.Set("X-Event-Depth", "3")
	return c.base.RoundTrip(r)
}
