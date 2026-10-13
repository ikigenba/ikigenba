package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/pages"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
	"github.com/ikigenba/ikigenba/candidates/internal/web"
)

func fixture(t *testing.T) (web.Config, *db.DB, *telemetry.Capture, *events.Capture) {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := func() time.Time { return time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC) }
	rnd := func() io.Reader { return bytes.NewReader(bytes.Repeat([]byte{0x5a}, 100000)) }
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "state.db"), Migrations: candidates.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	st := store.New(d, store.Config{Now: now, Rand: rnd()})
	tc := &telemetry.Capture{}
	ec := &events.Capture{}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: tc, Now: now, Rand: rnd(), Stderr: io.Discard})
	w.Ready()
	em := events.New(events.Config{Service: pages.ServiceName, Sink: ec, Now: now, Rand: rnd(), Telemetry: w, Emits: trail.Emits(), Stderr: io.Discard})
	eng := passes.New(passes.Config{Store: st, Sources: sources.New(fmcsa.New()), Telemetry: w, Events: em})
	t.Cleanup(func() {
		eng.Drain(context.Background())
		em.Shutdown(context.Background())
		w.Shutdown(context.Background(), "test")
	})
	return web.Config{func(u page.User) page.Banner {
		return page.Banner{Service: pages.ServiceName, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}, mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w}), "", st, eng, w, em, now}, d, tc, ec
}
func request(h http.Handler, method, path, body, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://candidates.example.test"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Request-Id", "request-fixture")
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

type unread struct{ reads int }

func (u *unread) Read([]byte) (int, error) { u.reads++; return 0, io.EOF }
func (*unread) Close() error               { return nil }

// R-3RDS-CKCH R-2E19-KER9 R-4GZO-DQX2 R-4LV9-WTVU
func TestIdentityGate(t *testing.T) {
	cfg, d, capture, bus := fixture(t)
	h := web.Handler(cfg)
	d.SetFailing(true)
	for _, path := range []string{"/", "/about", "/tools", "/candidates/cand_5a5a5a5a5a5a5a5a", "/passes/pass_5a5a5a5a5a5a5a5a", "/_appkit/theme.css", "/mcp", "/events/"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			body := &unread{}
			r := httptest.NewRequest(method, path, body)
			r.Header.Set("X-Request-Id", "gate-request")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			expected := identity.MissingBody
			if method == "HEAD" {
				expected = ""
			}
			if w.Code != 500 || w.Body.String() != expected || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || body.reads != 0 || len(w.Header().Values("Set-Cookie")) != 0 {
				t.Fatalf("%s %s: %d reads %d", method, path, w.Code, body.reads)
			}
		}
	}
	if err := cfg.Telemetry.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, e := range capture.Events() {
		if e.Name != "request.started" && e.Name != "request.finished" && e.Name != "service.started" {
			t.Fatal(e)
		}
	}
	if len(bus.Events()) != 0 {
		t.Fatal(bus.Events())
	}
	d.SetFailing(false)
	ps, err := cfg.Store.ListPasses(context.Background(), 0, 100)
	if err != nil || len(ps) != 0 {
		t.Fatal(ps, err)
	}
}

// R-4EJV-M7FO R-4FRR-ZZ6D
func TestEventRoutes(t *testing.T) {
	cfg, _, _, _ := fixture(t)
	h := web.Handler(cfg)
	for _, tc := range []struct{ method, path, body string }{{"GET", "/events", ""}, {"POST", "/events", "not json"}, {"POST", "/events", `{"id":"evt_0000000000000000","source":"scrapes","name":"repo.pushed","time":"2026-10-05T09:00:07Z","attrs":{},"depth":0}`}, {"GET", "/declarations", ""}, {"POST", "/declarations", ""}} {
		var direct http.Handler
		if tc.path == "/events" {
			direct = events.DeliveryHandler(cfg.Passes.Handlers())
		} else {
			direct = events.DeclarationsHandler(cfg.Events, cfg.Passes.Handlers())
		}
		a := request(h, tc.method, tc.path, tc.body, "")
		r := httptest.NewRequest(tc.method, "https://candidates.example.test"+tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(identity.NewContext(r.Context(), identity.Caller{RequestID: "request-fixture"}))
		b := httptest.NewRecorder()
		direct.ServeHTTP(b, r)
		if a.Code != b.Code || a.Body.String() != b.Body.String() || !reflect.DeepEqual(a.Header(), b.Header()) {
			t.Fatalf("%s %s differs: %d %d", tc.method, tc.path, a.Code, b.Code)
		}
	}
}

// R-4JFH-5AEG R-4KND-J255 R-4N36-ALMJ
func TestPageAndStaticRoutes(t *testing.T) {
	cfg, d, _, _ := fixture(t)
	h := web.Handler(cfg)
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	ph := identity.Require(pages.Handler(pages.Config{Banner: cfg.Banner, Pages: set, Store: cfg.Store, Now: cfg.Now}))
	for _, path := range []string{"/", "/about", "/tools", "/candidates/cand_5a5a5a5a5a5a5a5a", "/passes/pass_5a5a5a5a5a5a5a5a", "/nope", "/candidates", "/candidates/", "/passes/", "/mcp/", "/events/", "/declarations/", "/_appkit"} {
		a := request(h, "GET", path, "", "reader")
		b := request(ph, "GET", path, "", "reader")
		if a.Code != b.Code || a.Body.String() != b.Body.String() || !reflect.DeepEqual(a.Header(), b.Header()) {
			t.Fatal(path)
		}
	}
	a := request(h, "GET", "/_appkit/theme.css", "", "reader")
	b := request(page.Static(), "GET", "/_appkit/theme.css", "", "reader")
	if a.Code != 200 || a.Body.String() != b.Body.String() {
		t.Fatal("static")
	}
	d.SetFailing(true)
	for _, method := range []string{"GET", "HEAD"} {
		a := request(h, method, "/", "", "reader")
		expected := store.Unreachable + "\n"
		if method == "HEAD" {
			expected = ""
		}
		if a.Code != 503 || a.Body.String() != expected || !reflect.DeepEqual(a.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) {
			t.Fatal(a)
		}
	}
}

type causeTransport struct{ base http.RoundTripper }

func (t causeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set(events.CauseHeader, "evt_"+strings.Repeat("a", 16))
	r.Header.Set(events.DepthHeader, "3")
	return t.base.RoundTrip(r)
}

// R-4DBZ-8FOZ R-4I7K-RINR
func TestMCPContextAndTools(t *testing.T) {
	cfg, _, capture, _ := fixture(t)
	type observed struct {
		Caller identity.Caller
		Cause  events.Cause
	}
	seen := make(chan observed, 2)
	mcp.AddRawTool(cfg.MCP, mcp.RawTool[struct{}]{Name: "observe", Description: tools.DescribePass, Effect: mcp.Read, Handler: func(ctx context.Context, c identity.Caller, _ struct{}) (mcp.Result, error) {
		cause, _ := events.FromContext(ctx)
		v := observed{c, cause}
		seen <- v
		return mcp.ErrorResult("observed"), nil
	}})
	srv := httptest.NewServer(web.Handler(cfg))
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: causeTransport{http.DefaultTransport}}})
	caller := identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "chosen-request"}
	list, err := client.ListTools(context.Background(), caller)
	if err != nil || len(list) != 8 {
		t.Fatal(list, err)
	}
	for i, e := range tools.Catalog() {
		if list[i+1].Name != e.Name || list[i+1].Description != e.Description {
			t.Fatal(list)
		}
	}
	result, err := client.CallTool(context.Background(), caller, "observe", json.RawMessage(`{}`))
	if err != nil || !result.IsError() {
		t.Fatal(result, err)
	}
	v := <-seen
	if v.Caller != caller || v.Cause.ID != "evt_"+strings.Repeat("a", 16) || v.Cause.Depth != 3 {
		t.Fatal(v)
	}
	caller.RequestID = ""
	if _, err := client.CallTool(context.Background(), caller, "observe", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	v = <-seen
	if v.Caller.RequestID != strings.Repeat("5a", 16) {
		t.Fatal(v)
	}
	if err := cfg.Telemetry.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range capture.Events() {
		if e.Name == "request.started" && e.RequestID == "chosen-request" && e.User == "owner" {
			found = true
		}
	}
	if !found {
		t.Fatal(capture.Events())
	}
}

// R-P0W3-KWS2: the failing-handle web clause; Run's other clauses are covered at its seam.
func TestFailingStoreEmitsNoPassEvent(t *testing.T) {
	cfg, d, capture, bus := fixture(t)
	srv := httptest.NewServer(web.Handler(cfg))
	defer srv.Close()
	d.SetFailing(true)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp"})
	result, err := client.CallTool(context.Background(), identity.Caller{UserID: "caller"}, "rebuild", json.RawMessage(`{}`))
	if err != nil || !result.IsError() {
		t.Fatal(result, err)
	}
	event := events.Event{ID: "evt_0000000000000000", Time: cfg.Now(), Received: cfg.Now(), Seq: 1, Service: "scrapes", Name: trail.Accepted, Attrs: events.Attrs{"run": "run_fixture", "scraper": "fmcsa", "status": "done"}}
	resultDelivery, err := events.Send(context.Background(), srv.Client(), strings.TrimPrefix(srv.URL, "http://"), events.Delivery{Event: event, Attempt: 1})
	if err != nil {
		t.Fatal(err)
	}
	if resultDelivery.Outcome.Kind() != events.OutcomeError {
		t.Fatal(resultDelivery)
	}
	if err := cfg.Telemetry.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Events.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, e := range capture.Events() {
		if e.Name == trail.Started || e.Name == trail.Finished {
			t.Fatal(e)
		}
	}
	for _, e := range bus.Events() {
		if e.Name == trail.Started || e.Name == trail.Finished {
			t.Fatal(e)
		}
	}
}
