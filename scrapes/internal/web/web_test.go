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
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/pages"
	"github.com/ikigenba/ikigenba/scrapes/internal/runs"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/tools"
	"github.com/ikigenba/ikigenba/scrapes/internal/trail"
	"github.com/ikigenba/ikigenba/scrapes/internal/web"
)

type random struct{ n byte }

func (r *random) Read(p []byte) (int, error) {
	r.n++
	for i := range p {
		p[i] = r.n
	}
	return len(p), nil
}

type fixed byte

func (r fixed) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}

type scraper struct{ calls atomic.Int64 }

func (*scraper) Name() string                                             { return "fmcsa" }
func (s *scraper) Scrape(context.Context, func([]source.Row) error) error { s.calls.Add(1); return nil }

type fixture struct {
	cfg  web.Config
	d    *db.DB
	tc   *telemetry.Capture
	ec   *events.Capture
	fake *scraper
	h    http.Handler
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	now := func() time.Time { return time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC) }
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(t.TempDir(), "scrapes.db"), Migrations: scrapes.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	tc := new(telemetry.Capture)
	ec := new(events.Capture)
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: tc, Now: now, Rand: fixed(0x5a), Stderr: io.Discard})
	em := events.New(events.Config{Service: pages.ServiceName, Sink: ec, Now: now, Rand: fixed(0x5a), Telemetry: w, Emits: trail.Emits(), Stderr: io.Discard})
	st := store.New(d, store.Config{Now: now, Rand: &random{}})
	fake := new(scraper)
	reg := scrapers.New(fake)
	eng := runs.New(runs.Config{Store: st, Scrapers: reg, Telemetry: w, Events: em})
	var handler func(web.Config) http.Handler
	cfg := web.Config{func(u page.User) page.Banner {
		return page.Banner{Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}, mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "test", Telemetry: w}), "", st, reg, eng, w, em}
	handler = web.Handler
	f := &fixture{cfg: cfg, d: d, tc: tc, ec: ec, fake: fake, h: handler(cfg)}
	t.Cleanup(func() {
		eng.Drain(context.Background())
		em.Shutdown(context.Background())
		w.Shutdown(context.Background(), "test")
		_ = d.Close()
	})
	return f
}
func (f *fixture) seed(t *testing.T, status string, rows ...source.Row) store.Run {
	t.Helper()
	r, err := f.cfg.Store.AddRun(t.Context(), store.Run{Scraper: "fmcsa", UserID: "user-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.cfg.Store.PutRows(t.Context(), r.ID, rows); err != nil {
		t.Fatal(err)
	}
	switch status {
	case store.StatusDone:
		r, err = f.cfg.Store.FinishRun(t.Context(), r.ID)
	case store.StatusFailed:
		r, err = f.cfg.Store.FailRun(t.Context(), r.ID, store.ReasonSource)
	}
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func request(h http.Handler, method, path, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	r.Header.Set("X-Request-Id", "request-test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func assertResponse(t *testing.T, w *httptest.ResponseRecorder, status int, typ, body string) {
	t.Helper()
	if w.Code != status || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{typ}) || w.Body.String() != body {
		t.Fatalf("response %d %v %q; want %d %q %q", w.Code, w.Header(), w.Body.String(), status, typ, body)
	}
}

// R-DBC9-ELV5 R-DCK5-SDLU R-E4LU-L3NT R-DTVP-C0IR R-DV3L-PS9G
func TestCurrentRefusals(t *testing.T) {
	f := setup(t)
	if web.NotFound == "" || web.NoRun == "" || web.NotFound == web.NoRun || strings.ContainsAny(web.NotFound+web.NoRun, "\n") {
		t.Fatal("copy constants")
	}
	for _, status := range []string{"", store.StatusFailed, store.StatusRunning} {
		if status != "" {
			f.seed(t, status)
		}
		for _, method := range []string{"GET", "HEAD"} {
			body := web.NoRun + "\n"
			if method == "HEAD" {
				body = ""
			}
			assertResponse(t, request(f.h, method, "/scrapers/fmcsa/current", "user-test"), 404, "text/plain; charset=utf-8", body)
		}
	}
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, method := range []string{"GET", "HEAD"} {
			body := web.NotFound + "\n"
			if method == "HEAD" {
				body = ""
			}
			assertResponse(t, request(f.h, method, "/scrapers/unknown/current", "user-test"), 404, "text/plain; charset=utf-8", body)
		}
	}
}

// R-DWBI-3K05 R-DXJE-HBQU R-E173-MMYX R-E2F0-0EPM R-DSNS-Y8S2
func TestReadRecordsAndRows(t *testing.T) {
	f := setup(t)
	row := func(id string) source.Row {
		return source.Row{SourceID: id, Name: "fixture-" + id, Facts: json.RawMessage(`{}`), Raw: json.RawMessage(`{"fixture":true}`)}
	}
	old := f.seed(t, store.StatusDone, row("old"))
	done := f.seed(t, store.StatusDone, row("c"), row("a"), row("b"))
	failed := f.seed(t, store.StatusFailed, row("lost"))
	running := f.seed(t, store.StatusRunning, row("partial"))
	for _, r := range []store.Run{old, done, failed, running} {
		b, _ := json.Marshal(runs.RecordOf(r))
		paths := []string{"/runs/" + r.ID + "/record"}
		if r.ID == done.ID {
			paths = append(paths, "/scrapers/fmcsa/current")
		}
		for _, p := range paths {
			g := request(f.h, "GET", p, "user-test")
			assertResponse(t, g, 200, "application/json", string(b)+"\n")
			h := request(f.h, "HEAD", p, "user-test")
			if h.Code != g.Code || !reflect.DeepEqual(h.Header(), g.Header()) || h.Body.Len() != 0 {
				t.Fatal("HEAD record differs")
			}
		}
		var expected bytes.Buffer
		if err := f.cfg.Store.EachRow(t.Context(), r.ID, func(row source.Row) error {
			b, e := json.Marshal(row)
			expected.Write(b)
			expected.WriteByte('\n')
			return e
		}); err != nil {
			t.Fatal(err)
		}
		p := "/runs/" + r.ID + "/rows"
		g := request(f.h, "GET", p, "user-test")
		assertResponse(t, g, 200, "application/x-ndjson", expected.String())
		h := request(f.h, "HEAD", p, "user-test")
		if h.Code != g.Code || !reflect.DeepEqual(h.Header(), g.Header()) || h.Body.Len() != 0 {
			t.Fatal("HEAD rows differs")
		}
		if r.ID == done.ID && strings.Count(g.Body.String(), "\n") != 3 {
			t.Fatal("rows count")
		}
	}
}

// R-DYRA-V3HJ R-DZZ7-8V88 R-E3MW-E6GB
func TestMissingPrunedAndFailing(t *testing.T) {
	f := setup(t)
	pruned := f.seed(t, store.StatusDone)
	done := f.seed(t, store.StatusDone)
	if _, err := f.cfg.Store.PruneRuns(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"invalid", "run_ffffffffffffffff", pruned.ID} {
		for _, suffix := range []string{"record", "rows"} {
			for _, method := range []string{"GET", "HEAD"} {
				body := web.NotFound + "\n"
				if method == "HEAD" {
					body = ""
				}
				assertResponse(t, request(f.h, method, "/runs/"+id+"/"+suffix, "user-test"), 404, "text/plain; charset=utf-8", body)
			}
		}
	}
	f.d.SetFailing(true)
	for _, path := range []string{"/runs/" + done.ID + "/record", "/runs/" + done.ID + "/rows", "/scrapers/fmcsa/current", "/runs/invalid/rows"} {
		for _, method := range []string{"GET", "HEAD"} {
			body := store.Unreachable + "\n"
			if method == "HEAD" {
				body = ""
			}
			assertResponse(t, request(f.h, method, path, "user-test"), 503, "text/plain; charset=utf-8", body)
		}
	}
}

// R-U1H9-FNX9
func TestRowsStreamChunked(t *testing.T) {
	f := setup(t)
	empty := f.seed(t, store.StatusDone)
	filled := f.seed(t, store.StatusDone, source.Row{SourceID: "one", Facts: json.RawMessage(`{}`), Raw: json.RawMessage(`{}`)})
	running := f.seed(t, store.StatusRunning)
	srv := httptest.NewServer(f.h)
	defer srv.Close()
	for _, r := range []store.Run{empty, filled, running} {
		req, _ := http.NewRequest("GET", srv.URL+"/runs/"+r.ID+"/rows", nil)
		req.Header.Set("X-User-Id", "user-test")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 || !reflect.DeepEqual(resp.TransferEncoding, []string{"chunked"}) || len(resp.Header.Values("Content-Length")) != 0 {
			t.Fatalf("stream: %d %v %v", resp.StatusCode, resp.TransferEncoding, resp.Header)
		}
	}
}

type unread struct{ reads int }

func (b *unread) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (*unread) Close() error               { return nil }

// R-U2P5-TFNY R-U3X2-77EN R-U09D-1W6K R-E3DY-7BX4
func TestReadOnlyAndIdentity(t *testing.T) {
	f := setup(t)
	done := f.seed(t, store.StatusDone, source.Row{SourceID: "one", Facts: json.RawMessage(`{}`), Raw: json.RawMessage(`{}`)})
	rowBefore, err := f.cfg.Store.GetRow(t.Context(), done.ID, "one")
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.cfg.Store.ListRuns(t.Context(), "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"/scrapers/fmcsa/current", "/runs/" + done.ID + "/record", "/runs/" + done.ID + "/rows"}
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, path := range paths {
			for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "OPTIONS"} {
				for _, user := range []string{"", "user-test"} {
					b := new(unread)
					r := httptest.NewRequest(method, path, b)
					if user != "" {
						r.Header.Set("X-User-Id", user)
					}
					w := httptest.NewRecorder()
					f.h.ServeHTTP(w, r)
					if user == "" {
						body := identity.MissingBody
						if method == "HEAD" {
							body = ""
						}
						assertResponse(t, w, 500, "text/plain; charset=utf-8", body)
					} else if method != "GET" && method != "HEAD" {
						if w.Code != 405 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"GET, HEAD"}) || w.Body.Len() != 0 {
							t.Fatalf("method response %d %v %q", w.Code, w.Header(), w.Body.String())
						}
					}
					if b.reads != 0 || len(w.Header().Values("Set-Cookie")) != 0 {
						t.Fatal("body read or cookie")
					}
				}
			}
		}
	}
	f.d.SetFailing(false)
	rowAfter, err := f.cfg.Store.GetRow(t.Context(), done.ID, "one")
	if err != nil || !reflect.DeepEqual(rowBefore, rowAfter) {
		t.Fatal("read path changed row")
	}
	after, err := f.cfg.Store.ListRuns(t.Context(), "", 0, 100)
	if err != nil || !reflect.DeepEqual(before, after) || f.fake.calls.Load() != 0 {
		t.Fatal("read path mutated store")
	}
	for _, path := range []string{"/", "/about", "/tools", "/runs/" + done.ID, "/runs/" + done.ID + "%2Frows", "/_appkit/theme.css", "/mcp", "/events/"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			assertResponse(t, request(f.h, method, path, ""), 500, "text/plain; charset=utf-8", func() string {
				if method == "HEAD" {
					return ""
				}
				return identity.MissingBody
			}())
		}
	}
	if err = f.cfg.Telemetry.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = f.cfg.Events.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, ev := range f.tc.Events() {
		if ev.Name != "request.started" && ev.Name != "request.finished" {
			t.Fatalf("read event %s", ev.Name)
		}
	}
	if len(f.ec.Events()) != 0 {
		t.Fatal("read emitted event")
	}
}

// R-TXTK-ACP6 R-TZ1G-O4FV R-E0Y5-FSFQ
func TestPageAndStaticRouting(t *testing.T) {
	f := setup(t)
	done := f.seed(t, store.StatusDone, source.Row{SourceID: "one", Facts: json.RawMessage(`{}`), Raw: json.RawMessage(`{}`)})
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	ph := identity.Require(pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: set, ServicesPath: f.cfg.ServicesPath, Store: f.cfg.Store, Scrapers: f.cfg.Scrapers}))
	for _, path := range []string{"/", "/about", "/tools", "/runs/" + done.ID, "/runs/" + done.ID + "/rows/one", "/runs/" + done.ID + "%2Frows", "/nope", "/runs", "/runs/", "/scrapers/fmcsa", "/scrapers//current", "/scrapers/fmcsa/current/", "/scrapers/fmcsa%2Fcurrent", "/runs//record", "/runs//rows", "/runs/" + done.ID + "/record/", "/runs/" + done.ID + "/rows/", "/Runs/" + done.ID + "/rows", "/mcp/", "/events/", "/declarations/", "/_appkit"} {
		g := request(f.h, "GET", path, "user-test")
		want := request(ph, "GET", path, "user-test")
		if g.Code != want.Code || !bytes.Equal(g.Body.Bytes(), want.Body.Bytes()) || !reflect.DeepEqual(g.Header(), want.Header()) {
			t.Fatalf("page routing %s: %d versus %d", path, g.Code, want.Code)
		}
	}
	got := request(f.h, "GET", "/_appkit/theme.css", "user-test")
	want := request(page.Static(), "GET", "/_appkit/theme.css", "user-test")
	if got.Code != 200 || !bytes.Equal(got.Body.Bytes(), want.Body.Bytes()) {
		t.Fatal("static differs")
	}
	if f.fake.calls.Load() != 0 {
		t.Fatal("page called scraper")
	}
}

func delivery(name, id string) string {
	return `{"id":"` + id + `","time":"2024-02-03T04:05:06.000000Z","service":"cron","event":"` + name + `","request_id":"origin-test","user":"","attrs":{"trigger":"scrape_fmcsa","when":"fixture","scheduled":"fixture"},"cause":"evt_ffffffffffffffff","depth":2,"seq":1,"received":"2024-02-03T04:05:06.000000Z","attempt":1}`
}
func post(h http.Handler, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/events", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Request-Id", "delivery-request")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// R-2NQK-HXYD R-2OYG-VPP2 R-2MIO-467O
func TestBusRoutes(t *testing.T) {
	f := setup(t)
	for _, method := range []string{"GET", "HEAD", "POST", "DELETE", "OPTIONS", "PATCH"} {
		for _, user := range []string{"", "user-test"} {
			g := request(f.h, method, "/declarations?fixture=query", user)
			want := request(events.DeclarationsHandler(f.cfg.Events, f.cfg.Runs.Handlers()), method, "/declarations?fixture=query", user)
			if g.Code != want.Code || !reflect.DeepEqual(g.Header(), want.Header()) || g.Body.String() != want.Body.String() {
				t.Fatal("declarations route differs")
			}
		}
	}
	expected := `{"emits":[{"event":"scrape.finished","attrs":["scraper","run","status","reason","rows","new","changed","unchanged","gone"]},{"event":"scrape.started","attrs":["scraper","run"]}],"accepts":["cron.scrape_fmcsa.fired"]}`
	assertResponse(t, request(f.h, "GET", "/declarations", ""), 200, "application/json", expected)
	assertResponse(t, request(events.DeclarationsHandler(f.cfg.Events, f.cfg.Runs.Handlers()), "GET", "/declarations", ""), 200, "application/json", expected)
	for i, body := range []string{"invalid", delivery("repo.pushed", "evt_dddddddddddddddd"), delivery("cron.scrape_bogus.fired", "evt_cccccccccccccccc")} {
		g := post(f.h, body)
		want := post(events.DeliveryHandler(f.cfg.Runs.Handlers()), body)
		if g.Code != []int{400, 404, 404}[i] || g.Code != want.Code || g.Body.String() != want.Body.String() || !reflect.DeepEqual(g.Header(), want.Header()) {
			t.Fatal("delivery route differs")
		}
	}
	rs, err := f.cfg.Store.ListRuns(t.Context(), "", 0, 100)
	if err != nil || len(rs) != 0 || f.fake.calls.Load() != 0 {
		t.Fatalf("rejected deliveries started work: %+v %v", rs, err)
	}
	for _, method := range []string{"GET", "HEAD", "DELETE", "OPTIONS", "PATCH"} {
		if g := request(f.h, method, "/events", ""); g.Code != 405 {
			t.Fatalf("events %s: %d", method, g.Code)
		}
	}
	g := post(f.h, delivery("cron.scrape_fmcsa.fired", "evt_bbbbbbbbbbbbbbbb"))
	if g.Code != 200 || g.Body.String() != `{"outcome":"ok"}` {
		t.Fatalf("delivery %d %s", g.Code, g.Body.String())
	}
	rs, err = f.cfg.Store.ListRuns(t.Context(), "", 0, 100)
	if err != nil || len(rs) != 1 || rs[0].Scraper != "fmcsa" || rs[0].Trigger != "scrape_fmcsa" || rs[0].Delivery != "evt_bbbbbbbbbbbbbbbb" || rs[0].RequestID != "delivery-request" || rs[0].UserID != "" {
		t.Fatalf("delivery run %+v %v", rs, err)
	}
}

// R-2NQK-HXYD R-2OYG-VPP2
func TestBusRoutesWithHeadersAndBodies(t *testing.T) {
	for _, path := range []string{"/events", "/declarations"} {
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE", "OPTIONS", "PATCH"} {
			for _, user := range []string{"absent", "", "user-test"} {
				t.Run(path+"/"+method+"/"+user, func(t *testing.T) {
					f, reference := setup(t), setup(t)
					serve := func(h http.Handler, withCaller bool) *httptest.ResponseRecorder {
						r := httptest.NewRequest(method, path+"?fixture=query", strings.NewReader(delivery("cron.scrape_fmcsa.fired", "evt_eeeeeeeeeeeeeeee")))
						c := identity.Caller{Email: "fixture@example.invalid", RequestID: "matrix-request"}
						if user != "absent" {
							r.Header.Set("X-User-Id", user)
							c.UserID = user
						}
						r.Header.Set("X-User-Email", c.Email)
						r.Header.Set("X-Request-Id", c.RequestID)
						r.Header.Set("Content-Type", "application/json")
						if withCaller {
							r = r.WithContext(identity.NewContext(r.Context(), c))
						}
						w := httptest.NewRecorder()
						h.ServeHTTP(w, r)
						return w
					}
					target := events.DeclarationsHandler(reference.cfg.Events, reference.cfg.Runs.Handlers())
					if path == "/events" {
						target = events.DeliveryHandler(reference.cfg.Runs.Handlers())
					}
					got, want := serve(f.h, false), serve(target, true)
					if got.Code != want.Code || got.Body.String() != want.Body.String() || !reflect.DeepEqual(got.Header(), want.Header()) {
						t.Fatalf("bus response differs: %d %s versus %d %s", got.Code, got.Body.String(), want.Code, want.Body.String())
					}
					if path == "/events" && method == "POST" {
						rs, err := f.cfg.Store.ListRuns(t.Context(), "", 0, 100)
						wantUser := user
						if user == "absent" {
							wantUser = ""
						}
						if err != nil || len(rs) != 1 || rs[0].UserID != wantUser || rs[0].RequestID != "matrix-request" {
							t.Fatalf("delivery caller %+v %v", rs, err)
						}
					}
				})
			}
		}
	}
}

// R-DZQ9-20P1
type causeTransport struct{ base http.RoundTripper }

func (c causeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Event-Cause", "evt_aaaaaaaaaaaaaaaa")
	r.Header.Set("X-Event-Depth", "4")
	return c.base.RoundTrip(r)
}

func TestMCPWiringAndCause(t *testing.T) {
	f := setup(t)
	srv := httptest.NewServer(f.h)
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: causeTransport{base: srv.Client().Transport}}, Name: "test", Version: "test"})
	caller := identity.Caller{UserID: "user-test", Email: "test@example.invalid", RequestID: "tool-request"}
	list, err := client.ListTools(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	catalog := tools.Catalog()
	if len(list) != len(catalog) {
		t.Fatal("tools not registered")
	}
	for i, entry := range catalog {
		if list[i].Name != entry.Name || list[i].Description != entry.Description {
			t.Fatal("tool registration")
		}
	}
	ctx := events.NewContext(t.Context(), events.Cause{ID: "evt_aaaaaaaaaaaaaaaa", Depth: 4})
	result, err := client.CallTool(ctx, caller, "scrape", json.RawMessage(`{"scraper":"fmcsa"}`))
	if err != nil || result.IsError() {
		t.Fatalf("scrape %v %+v", err, result)
	}
	f.cfg.Runs.Drain(context.Background())
	if err = f.cfg.Events.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	evs := f.ec.Events()
	if len(evs) != 2 {
		t.Fatalf("bus events %d", len(evs))
	}
	for _, ev := range evs {
		if ev.Cause != "evt_aaaaaaaaaaaaaaaa" || ev.Depth != 5 || ev.RequestID != caller.RequestID || ev.User != caller.UserID {
			t.Fatalf("cause or caller lost %+v", ev)
		}
	}
}

// R-DTMR-55ZK R-EO48-PFIX
func TestRequestPairAndCaller(t *testing.T) {
	f := setup(t)
	paths := []string{"/", "/nope", "/_appkit/theme.css", "/scrapers/unknown/current", "/events", "/declarations", "/mcp"}
	for i, path := range paths {
		r := httptest.NewRequest("GET", path, nil)
		if i%2 == 0 {
			r.Header["X-User-Id"] = []string{"user-test", "ignored"}
			r.Header["X-User-Email"] = []string{"test@example.invalid", "ignored"}
		}
		rid := "request-" + path
		if i%3 == 0 {
			r.Header["X-Request-Id"] = []string{"", "ignored"}
			rid = strings.Repeat("5a", 16)
		} else {
			r.Header["X-Request-Id"] = []string{rid, "ignored"}
		}
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		if err := f.cfg.Telemetry.Flush(t.Context()); err != nil {
			t.Fatal(err)
		}
		events := f.tc.Events()
		var selected []telemetry.Event
		for _, ev := range events {
			if ev.RequestID == rid {
				selected = append(selected, ev)
			}
		}
		if i%3 == 0 {
			selected = selected[len(selected)-2:]
		}
		if len(selected) != 2 || selected[0].Name != "request.started" || selected[1].Name != "request.finished" || !reflect.DeepEqual(selected[0].Attrs, telemetry.Attrs{"method": "GET", "path": r.URL.Path}) || selected[1].Attrs["status"] != int64(w.Code) {
			t.Fatalf("request pair %+v", selected)
		}
		user := ""
		if i%2 == 0 {
			user = "user-test"
		}
		for _, ev := range selected {
			if ev.User != user || ev.RequestID != rid {
				t.Fatalf("caller %+v", ev)
			}
		}
		if len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("cookie")
		}
	}
}

// R-2YN2-G6QC
func TestFailingHandlerEmitsNoRunEvents(t *testing.T) {
	f := setup(t)
	f.d.SetFailing(true)
	srv := httptest.NewServer(f.h)
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client(), Name: "test", Version: "test"})
	result, err := client.CallTool(t.Context(), identity.Caller{UserID: "user-test", RequestID: "failure-request"}, "scrape", json.RawMessage(`{"scraper":"fmcsa"}`))
	if err != nil || !result.IsError() {
		t.Fatalf("failing scrape %v %+v", err, result)
	}
	w := post(f.h, delivery("cron.scrape_fmcsa.fired", "evt_eeeeeeeeeeeeeeee"))
	if w.Code != 500 {
		t.Fatalf("failing delivery %d", w.Code)
	}
	if err = f.cfg.Telemetry.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = f.cfg.Events.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(f.ec.Events()) != 0 || f.fake.calls.Load() != 0 {
		t.Fatal("failed request started work")
	}
	for _, ev := range f.tc.Events() {
		if ev.Name == trail.Started || ev.Name == trail.Finished {
			t.Fatal("failed request recorded run event")
		}
	}
}
