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
	"strconv"
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
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
	"github.com/ikigenba/ikigenba/candidates/internal/web"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func fixtureTime() time.Time { return time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC) }

func fixture(t *testing.T) (web.Config, *db.DB, *telemetry.Capture, *events.Capture) {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := fixtureTime
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
	}, mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: w}), nil, "", st, eng, w, em, nil}, d, tc, ec
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

// R-4GZO-DQX2 R-4LV9-WTVU
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
	ps, err := cfg.Store.ListPasses(context.Background(), "", "", 100)
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

// R-4JFH-5AEG R-D02D-JQBY R-4N36-ALMJ
func TestPageAndStaticRoutes(t *testing.T) {
	cfg, d, _, _ := fixture(t)
	h := web.Handler(cfg)
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	ph := identity.Require(pages.Handler(pages.Config{Banner: cfg.Banner, Pages: set, Store: cfg.Store}))
	for _, path := range []string{"/", "/about", "/tools", "/candidates/cand_5a5a5a5a5a5a5a5a", "/passes/pass_5a5a5a5a5a5a5a5a", "/nope", "/candidates", "/candidates/", "/parks", "/parks/missing", "/parks/", "/passes/", "/mcp/", "/events/", "/declarations/", "/_appkit"} {
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

// R-4DBZ-8FOZ
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
	if err != nil || len(list) != len(tools.Catalog())+1 {
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

// R-S35A-G1HS: a failing store prevents a pass from starting.
func TestFailingStoreEmitsNoPassEvent(t *testing.T) {
	cfg, d, capture, bus := fixture(t)
	cfg.Website = &website.Checker{}
	cfg.Passes = passes.New(passes.Config{Store: cfg.Store, Sources: sources.New(fmcsa.New()), Website: cfg.Website, Telemetry: cfg.Telemetry, Events: cfg.Events})
	defer cfg.Passes.Drain(context.Background())
	srv := httptest.NewServer(web.Handler(cfg))
	defer srv.Close()
	d.SetFailing(true)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp"})
	result, err := client.CallTool(context.Background(), identity.Caller{UserID: "caller"}, "ingest", json.RawMessage(`{}`))
	if err != nil || !result.IsError() {
		t.Fatal(result, err)
	}
	result, err = client.CallTool(context.Background(), identity.Caller{UserID: "caller"}, "enrich", json.RawMessage(`{"kind":"`+sources.WebsiteCheck+`"}`))
	if err != nil || !result.IsError() {
		t.Fatal(result, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), "unavailable: "+tools.RefuseUnavailable) {
		t.Fatal(result, err)
	}
	event := events.Event{ID: "evt_0000000000000000", Time: fixtureTime(), Received: fixtureTime(), Seq: 1, Service: "scrapes", Name: trail.Accepted, Attrs: events.Attrs{"run": "run_fixture", "scraper": "fmcsa", "status": "done"}}
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

// R-3GUX-8I1H
func TestLifecycleDeclarationsIncludeDerived(t *testing.T) {
	cfg, _, _, _ := fixture(t)
	cfg.Passes = passes.New(passes.Config{Store: cfg.Store, Sources: sources.New(fmcsa.New(), ppp.New()), Telemetry: cfg.Telemetry, Events: cfg.Events})
	r := request(web.Handler(cfg), http.MethodGet, "/declarations", "", "")
	want := `{"emits":[{"event":"pass.finished","attrs":["pass","kind","origin","trigger","target","enrich_kind","status","reason","parcel_run","rows","new","changed","unchanged","gone","created","linked","judged","unranged","derived","due","fetched","https","http","none","runs","runs_failed","pages","cost","parcels_read","by_label","by_value","by_candidate","parks_created","parks_updated","parks_gone","parks_grown"]},{"event":"pass.started","attrs":["pass","kind","origin","trigger","target","enrich_kind"]}],"accepts":["campaign.member.added","campaign.member.rejected","campaign.piece.delivered","cron.judge_candidates.fired","scrape.finished"]}`
	if r.Code != http.StatusOK || r.Body.String() != want {
		t.Fatal(r.Code, r.Body.String())
	}
}

// R-CXMK-S6UK R-JOJI-0XTC
func TestMCPToolsReceiveConfiguredWebsite(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "nil", true: "checker"}[enabled], func(t *testing.T) {
			cfg, _, _, _ := fixture(t)
			if enabled {
				cfg.Website = &website.Checker{Client: &http.Client{Transport: webTransport(func(*http.Request) (*http.Response, error) { t.Error("missing candidate fetched"); return nil, io.EOF })}}
			}
			cfg.Passes = passes.New(passes.Config{Store: cfg.Store, Sources: sources.New(fmcsa.New()), Telemetry: cfg.Telemetry, Events: cfg.Events, Website: cfg.Website})
			defer cfg.Passes.Drain(t.Context())
			srv := httptest.NewServer(web.Handler(cfg))
			defer srv.Close()
			client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp"})
			result, err := client.CallTool(t.Context(), identity.Caller{UserID: "owner"}, "add_page", json.RawMessage(`{"candidate":"cand_5a5a5a5a5a5a5a5a","url":"https://acme.test/"}`))
			want := "unavailable: " + tools.RefuseUnavailable
			if enabled {
				want = "not_found: " + tools.RefuseNotFound
			}
			b, _ := json.Marshal(result)
			if err != nil || !result.IsError() || !strings.Contains(string(b), want) {
				t.Fatal(result, err, want)
			}
		})
	}
}

type webTransport func(*http.Request) (*http.Response, error)

func (f webTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// R-XTQK-SDMD R-CXMK-S6UK
func TestLegacyDispatcherConfiguration(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			cfg, _, _, _ := fixture(t)
			pl, err := cfg.Store.CreateProspectList(t.Context(), store.ProspectList{Name: "configured legacy", Size: 10, CreatedUserID: "legacy-owner"})
			if err != nil {
				t.Fatal(err)
			}
			if enabled {
				cfg.LegacyMCP = mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: cfg.Telemetry})
			}
			srv := httptest.NewServer(web.Handler(cfg))
			defer srv.Close()
			client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp"})
			caller := identity.Caller{UserID: "legacy-owner"}
			list, err := client.ListTools(t.Context(), caller)
			if err != nil || len(list) != len(tools.Catalog()) {
				t.Fatal(list, err)
			}
			for i, entry := range tools.Catalog() {
				if list[i].Name != entry.Name {
					t.Fatal(list)
				}
			}
			result, err := client.CallTool(t.Context(), caller, "cohort", json.RawMessage(`{"cohort":"coh_`+strings.TrimPrefix(pl.ID, "pl_")+`"}`))
			if enabled && (err != nil || result.IsError()) || !enabled && err == nil && !result.IsError() {
				t.Fatal(result, err)
			}
			if enabled {
				normal, err := client.CallTool(t.Context(), caller, "prospect_list", json.RawMessage(`{"prospect_list":"`+pl.ID+`"}`))
				if err != nil || !reflect.DeepEqual(normal, result) {
					t.Fatal(normal, result, err)
				}
			}
		})
	}
}
