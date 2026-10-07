package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/git"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/pages"
	"github.com/ikigenba/ikigenba/scripts/internal/runner"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/settings"
	"github.com/ikigenba/ikigenba/scripts/internal/source"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
	"github.com/ikigenba/ikigenba/scripts/internal/tools"
	"github.com/ikigenba/ikigenba/scripts/internal/web"
)

type sequence struct {
	mu sync.Mutex
	n  byte
}

func (s *sequence) Read(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range b {
		s.n++
		b[i] = s.n
	}
	return len(b), nil
}

type fixture struct {
	cfg     web.Config
	db      *db.DB
	h       http.Handler
	capture *telemetry.Capture
	stderr  *bytes.Buffer
}

func makeFixture(t *testing.T, sink telemetry.Sink) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := func() time.Time { return time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC) }
	rand := &sequence{}
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "catalog.db"), Migrations: scripts.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	st := store.New(d, store.Config{Now: now, Rand: rand})
	f := &fixture{db: d, capture: &telemetry.Capture{}, stderr: &bytes.Buffer{}}
	if sink == nil {
		sink = f.capture
	}
	writer := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: "fixture", Sink: sink, Stderr: f.stderr, Now: now, Sleep: func(context.Context, time.Duration) {}, Rand: rand})
	t.Cleanup(func() { writer.Shutdown(context.Background(), "done") })
	lim := limits.New(settings.Defaults(), limits.Clock{After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	src := source.New(source.Config{Repos: filepath.Join(t.TempDir(), "repos"), Limits: lim})
	core := runs.New(runs.Config{MaxActive: 100, MaxQueued: 100, Store: st, Source: src, Writer: writer, Runs: t.TempDir(), Now: now, Rand: rand, ScriptAfter: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	f.cfg = web.Config{func(_ page.User) page.Banner { return page.Banner{Service: pages.ServiceName, Version: "fixture"} }, mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "fixture", Telemetry: writer}), "", st, src, core, lim, writer}
	f.h = web.Handler(f.cfg)
	return f
}
func serve(h http.Handler, method, path, user, email, id string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Host = "backend"
	r.Header.Set("X-Forwarded-Proto", "https")
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	if email != "" {
		r.Header.Set("X-User-Email", email)
	}
	if id != "" {
		r.Header.Set("X-Request-Id", id)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func same(t *testing.T, a, b *httptest.ResponseRecorder) {
	t.Helper()
	if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || !bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) {
		t.Fatalf("different responses: %d %v %q versus %d %v %q", a.Code, a.Header(), a.Body.String(), b.Code, b.Header(), b.Body.String())
	}
}

// R-6RXF-6WGQ R-OLN5-ANNP
func TestMissingIdentityBeforeEveryRoute(t *testing.T) {
	f := makeFixture(t, nil)
	f.db.SetFailing(true)
	for _, p := range []string{"/", "/_appkit/theme.css", "/mcp", "/n/runs/r/stdout", "/about", "/nope", "/events/", "/Events", "/declarations/repo.pushed"} {
		for _, m := range []string{"GET", "HEAD", "POST", "DELETE"} {
			for _, email := range []string{"", "user@example.test"} {
				got := serve(f.h, m, p, "", email, "request")
				want := serve(identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("gate admitted request") })), m, p, "", email, "request")
				same(t, got, want)
				if got.Code != 500 || got.Header().Get("Allow") != "" {
					t.Fatal(got.Code, got.Header())
				}
			}
		}
	}
}

// R-6T5B-KO7F R-C5GI-R6UL R-8CIO-SOZP R-CAC4-A9TD R-8DQL-6GQE R-CDZT-FL1G R-CF7P-TCS5 R-CMJ4-3Z8B
func TestRoutingMatchesOwnedHandlers(t *testing.T) {
	f := makeFixture(t, nil)
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	s := f.cfg.Limits.Settings()
	p := identity.Require(pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: set, Store: f.cfg.Store, Source: f.cfg.Source, Runs: f.cfg.Runs, KeepDays: s.RunKeepDays, KeepCount: s.RunKeepCount, TreeMaxBytes: s.TreeMaxBytes, OperationSeconds: s.OperationSeconds}))
	files := identity.Require(web.Files(web.FilesConfig{Banner: f.cfg.Banner, Pages: set, Store: f.cfg.Store, Runs: f.cfg.Runs}))
	static := identity.Require(page.Static())
	endpoint := identity.Require(f.cfg.MCP)
	for _, failing := range []bool{false, true} {
		if failing {
			f.db.SetFailing(true)
		}
		for _, row := range []struct {
			path    string
			handler http.Handler
		}{{"/", p}, {"/about", p}, {"/nope", p}, {"/about/", p}, {"/mcp/tools", p}, {"/mcp/", p}, {"/events/", p}, {"/events/x", p}, {"/declarations/repo.pushed", p}, {"/Events", p}, {"/_appkit", p}, {"//", p}, {"/a/../b", p}, {"/n/runs/r/", p}, {"/n/runs/r", p}, {"/n/runs/r/stdout", files}, {"/n/runs/r//", files}, {"/n/runs/r/out/", files}, {"/n/runs/r/./stdout", files}, {"/n/runs/r/stdout/", files}, {"/n/%72uns/r/stdout", files}, {"/n%2Fruns/r/stdout", p}, {"/_appkit/theme.css", static}, {"/_appkit/nope.css", static}, {"/mcp", endpoint}} {
			for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
				got := serve(f.h, method, row.path, "owner", "", "request")
				want := serve(row.handler, method, row.path, "owner", "", "request")
				same(t, got, want)
				r := httptest.NewRequest(method, row.path+"?anything=1", nil)
				r.Host = "example.org"
				r.Header.Set("X-User-Id", "owner")
				r.Header.Set("X-User-Email", "")
				r.Header.Set("X-Request-Id", "request")
				r.Header.Set("X-Forwarded-Proto", "https")
				w := httptest.NewRecorder()
				f.h.ServeHTTP(w, r)
				same(t, got, w)
			}
		}
	}
}

// R-C6OF-4YLA
func TestRegistersExactlyDesignedTools(t *testing.T) {
	f := makeFixture(t, nil)
	second := mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "fixture", Telemetry: f.cfg.Telemetry})
	tools.Register(second, tools.Config{Store: f.cfg.Store, Source: f.cfg.Source, Runs: f.cfg.Runs, Telemetry: f.cfg.Telemetry})
	a := httptest.NewServer(f.h)
	defer a.Close()
	b := httptest.NewServer(identity.Require(second))
	defer b.Close()
	caller := identity.Caller{UserID: "owner", RequestID: "request"}
	got, err := mcp.NewClient(mcp.ClientConfig{Endpoint: a.URL + "/mcp"}).ListTools(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	want, err := mcp.NewClient(mcp.ClientConfig{Endpoint: b.URL}).ListTools(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || len(got) != 11 {
		t.Fatalf("tools: %v %v", got, want)
	}
}

// R-C7WB-IQBZ R-CGFM-74IU R-CHNI-KW9J R-CK3B-CFQX R-CNR0-HQZ0
func TestMiddlewareAndConcurrentIdentities(t *testing.T) {
	f := makeFixture(t, nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprint(i)
			serve(f.h, "GET", "/about", "owner-"+id, "email-"+id, "req-"+id)
		}(i)
	}
	wg.Wait()
	serve(f.h, "GET", "/mcp", "", "", "missing")
	serve(f.h, "HEAD", "/", "owner", "", "head")
	f.db.SetFailing(true)
	serve(f.h, "GET", "/", "owner", "", "failing")
	serve(f.h, "GET", "/about", "owner", "", "")
	if err := f.cfg.Telemetry.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	grouped := map[string][]telemetry.Event{}
	for _, e := range f.capture.Events() {
		grouped[e.RequestID] = append(grouped[e.RequestID], e)
	}
	if len(grouped) != 24 {
		t.Fatal("ids", len(grouped))
	}
	for id, events := range grouped {
		if id == "" || len(events) != 2 || events[0].Name != "request.started" || events[1].Name != "request.finished" {
			t.Fatal(id, events)
		}
		if len(events[0].Attrs) != 2 || len(events[1].Attrs) != 4 {
			t.Fatal(events)
		}
		if events[0].Attrs["method"] == nil || events[0].Attrs["path"] == nil || events[1].Attrs["status"] == nil || events[1].Attrs["duration_us"] == nil || events[1].Attrs["request_bytes"] == nil || events[1].Attrs["response_bytes"] == nil {
			t.Fatal("wrong event attributes", events)
		}

		method, path := "GET", "/about"
		switch id {
		case "head":
			method, path = "HEAD", "/"
		case "missing":
			path = "/mcp"
		case "failing":
			path = "/"
		}
		if !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"method": method, "path": path}) {
			t.Fatal("wrong start values", events[0])
		}
		if events[0].User != events[1].User {
			t.Fatal("identity changed", events)
		}
		if strings.HasPrefix(id, "req-") && events[0].User != "owner-"+strings.TrimPrefix(id, "req-") {
			t.Fatal("identity crossed", events)
		}
	}
	if fmt.Sprint(grouped["missing"][1].Attrs["status"]) != "500" || fmt.Sprint(grouped["head"][1].Attrs["response_bytes"]) != "0" || fmt.Sprint(grouped["failing"][1].Attrs["status"]) != "503" {
		t.Fatal("middleware metrics", grouped)
	}
	if f.stderr.Len() != 0 {
		t.Fatal(f.stderr.String())
	}
}

type blockedSink struct{ release <-chan struct{} }

func (s blockedSink) Deliver(ctx context.Context, _ telemetry.Event) error {
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type failedSink struct{}

func (failedSink) Deliver(context.Context, telemetry.Event) error { return errors.New("unavailable") }

// R-CIVE-YO08
func TestAnswersIndependentOfSink(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	good := makeFixture(t, nil)
	bad := makeFixture(t, failedSink{})
	blocked := makeFixture(t, blockedSink{release})
	for _, f := range []*fixture{bad, blocked} {
		same(t, serve(good.h, "GET", "/", "owner", "", "request"), serve(f.h, "GET", "/", "owner", "", "request"))
		a := httptest.NewServer(good.h)
		b := httptest.NewServer(f.h)
		caller := identity.Caller{UserID: "owner", RequestID: "call"}
		x, err := mcp.NewClient(mcp.ClientConfig{Endpoint: a.URL + "/mcp"}).CallTool(context.Background(), caller, "list", json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		y, err := mcp.NewClient(mcp.ClientConfig{Endpoint: b.URL + "/mcp"}).CallTool(context.Background(), caller, "list", json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		a.Close()
		b.Close()
		if !reflect.DeepEqual(x, y) {
			t.Fatal(x, y)
		}
	}
}

// R-6VL4-C7OT
func TestServicesSocketNotContacted(t *testing.T) {
	f := makeFixture(t, nil)
	repo := repositoryFixture(t, f)
	dir, err := os.MkdirTemp("", "web-socket-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	socket := filepath.Join(dir, "service.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	connected := make(chan bool, 1)
	go func() {
		c, e := ln.Accept()
		if e == nil {
			_ = c.Close()
			connected <- true
		} else {
			connected <- false
		}
	}()
	file := filepath.Join(t.TempDir(), "services.json")
	if err = os.WriteFile(file, []byte(`{"services":[{"name":"other","enabled":true,"mcp":true,"socket":"`+socket+`","url":"https://other.example.test","description":"fixture"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	list, err := services.Read(file)
	if err != nil || len(list) != 1 || list[0].Socket != socket {
		t.Fatalf("invalid services fixture: %v %v", list, err)
	}
	cfg := f.cfg
	cfg.ServicesPath = file
	cfg.MCP = mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "fixture", Telemetry: cfg.Telemetry})
	h := web.Handler(cfg)
	srv := httptest.NewServer(h)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp"})
	caller := identity.Caller{UserID: "owner", RequestID: "request"}
	call := func(name string, args any) mcp.Result {
		t.Helper()
		raw, e := json.Marshal(args)
		if e != nil {
			t.Fatal(e)
		}
		result, e := client.CallTool(context.Background(), caller, name, raw)
		if e != nil || result.IsError() {
			t.Fatalf("%s: %v %v", name, result, e)
		}
		return result
	}
	call("create", map[string]any{"name": "report", "repo": repo})
	sc, e := cfg.Store.Find(context.Background(), "owner", "report")
	if e != nil {
		t.Fatal(e)
	}
	call("subscribe", map[string]any{"name": "report", "event": "repo.pushed"})
	call("show", map[string]any{"name": "report"})
	call("list", map[string]any{})
	call("update", map[string]any{"name": "report", "ref": "alternate"})
	sc, e = cfg.Store.Find(context.Background(), "owner", "report")
	if e != nil || sc.Ref != "alternate" {
		t.Fatal(sc, e)
	}
	call("run", map[string]any{"name": "report", "input": map[string]any{}})
	records, e := cfg.Store.Runs(context.Background(), sc.ID)
	if e != nil || len(records) != 1 || records[0].Status != store.StatusRunning {
		t.Fatal(records, e)
	}
	run := records[0]
	for _, path := range []string{"/", "/report/", "/report/runs/" + run.ID + "/"} {
		if got := serve(h, "GET", path, "owner", "", "page"); got.Code != 200 {
			t.Fatal(path, got.Code, got.Body.String())
		}
	}
	call("runs", map[string]any{"name": "report"})
	call("result", map[string]any{"run": run.ID})
	call("cancel", map[string]any{"run": run.ID})
	ended, e := cfg.Store.FindRun(context.Background(), "owner", run.ID)
	if e != nil || ended.Status != store.StatusKilled {
		t.Fatal(ended, e)
	}
	raw, e := deliveryJSON(events.Delivery{Event: events.Event{ID: "evt_1111111111111111", Seq: 1, Received: cfg.Telemetry.Now(), Time: cfg.Telemetry.Now(), Service: "repos", Name: "repo.pushed", Attrs: events.Attrs{}}, Attempt: 1})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", events.EventsPath, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Request-Id", "delivery-request")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok"`) {
		t.Fatal(w)
	}
	call("unsubscribe", map[string]any{"name": "report", "event": "repo.pushed"})
	call("delete", map[string]any{"name": "report"})
	if _, e = cfg.Store.Find(context.Background(), "owner", "report"); !errors.Is(e, store.ErrNotFound) {
		t.Fatal("script was not deleted", e)
	}
	srv.Close()
	_ = ln.Close()
	if <-connected {
		t.Fatal("services socket contacted")
	}
}

// R-CBDU-XW5S R-CCLR-BNWH R-CDTN-PFN6 R-ZAMF-KHQB R-ZBUB-Y9H0 R-CF1K-37DV R-CG9G-GZ4K R-CHHC-UQV9 R-ZGPX-HCFS R-ZHXT-V46H
func TestSharedStaticFiles(t *testing.T) {
	f := makeFixture(t, nil)
	g := makeFixture(t, nil)

	baseline := map[string]*httptest.ResponseRecorder{}
	files := sharedFiles(t, f.h)
	for name := range files {
		baseline[name] = serve(f.h, "GET", page.StaticPrefix+name, "owner", "", "request")
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	bodies := map[string]string{}
	for name, mime := range files {
		cache := "no-cache"
		if mime == "font/woff2" {
			cache = "public, max-age=31536000, immutable"
		}
		p := page.StaticPrefix + name
		got := serve(f.h, "GET", p, "owner", "", "request")
		if !bytes.Equal(baseline[name].Body.Bytes(), got.Body.Bytes()) {
			t.Fatal(name, "body changed after catalog close and working-directory change")
		}
		if got.Code != 200 || got.Body.Len() == 0 || !reflect.DeepEqual(got.Header().Values("Content-Type"), []string{mime}) || !reflect.DeepEqual(got.Header().Values("Cache-Control"), []string{cache}) {
			t.Fatal(name, got.Code, got.Header())
		}
		tag := got.Header().Get("ETag")
		if len(got.Header().Values("ETag")) != 1 || len(tag) < 2 || tag[0] != '"' || tag[len(tag)-1] != '"' {
			t.Fatal("invalid strong tag", tag)
		}
		for _, b := range []byte(tag[1 : len(tag)-1]) {
			if b != 33 && (b < 35 || b == 127) {
				t.Fatal("invalid tag byte", b)
			}
		}
		if body, ok := bodies[tag]; ok && body != got.Body.String() {
			t.Fatal("etag collision")
		}
		bodies[tag] = got.Body.String()
		for _, h := range []http.Handler{f.h, g.h} {
			repeated := serve(h, "GET", p, "owner", "", "request")
			if !bytes.Equal(got.Body.Bytes(), repeated.Body.Bytes()) || !reflect.DeepEqual(repeated.Header().Values("ETag"), []string{tag}) {
				t.Fatal(name, "body or ETag changed between GET requests")
			}
		}
		head := serve(f.h, "HEAD", p, "owner", "", "request")
		if head.Code != 200 || head.Body.Len() != 0 || !reflect.DeepEqual(head.Header().Values("Content-Type"), got.Header().Values("Content-Type")) || !reflect.DeepEqual(head.Header().Values("ETag"), got.Header().Values("ETag")) || !reflect.DeepEqual(head.Header().Values("Cache-Control"), got.Header().Values("Cache-Control")) {
			t.Fatal(head)
		}
		for _, method := range []string{"GET", "HEAD"} {
			for _, modified := range []string{"", "malformed", "Tue, 04 Mar 2025 05:06:07 GMT", "Thu, 01 Jan 2099 00:00:00 GMT"} {
				for _, match := range []string{"*", tag, "W/" + tag, ` , "different", W/` + tag + `, `, " ,\tW/" + tag + "\t,", tag + ", " + tag} {
					r := httptest.NewRequest(method, p, nil)
					r.Header.Set("X-User-Id", "owner")
					r.Header.Set("If-None-Match", match)
					r.Header.Set("If-Modified-Since", modified)
					w := httptest.NewRecorder()
					f.h.ServeHTTP(w, r)
					if w.Code != 304 || w.Body.Len() != 0 || !reflect.DeepEqual(w.Header().Values("ETag"), []string{tag}) || !reflect.DeepEqual(w.Header().Values("Cache-Control"), []string{cache}) {
						t.Fatal(match, w)
					}
				}
			}
			for _, modified := range []string{"", "malformed", "Tue, 04 Mar 2025 05:06:07 GMT", "Thu, 01 Jan 2099 00:00:00 GMT"} {
				for _, match := range []string{`"absent"`, `W/"absent"`, " ,\tW/\"absent\", \"different\",\t "} {
					r := httptest.NewRequest(method, p, nil)
					r.Header.Set("X-User-Id", "owner")
					r.Header.Set("If-None-Match", match)
					if modified != "" {
						r.Header.Set("If-Modified-Since", modified)
					}
					w := httptest.NewRecorder()
					f.h.ServeHTTP(w, r)
					if w.Code != 200 || !reflect.DeepEqual(w.Header().Values("ETag"), []string{tag}) || !reflect.DeepEqual(w.Header().Values("Cache-Control"), []string{cache}) || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{mime}) || (method == "GET" && w.Body.String() != got.Body.String()) || (method == "HEAD" && w.Body.Len() != 0) {
						t.Fatal(name, method, match, modified, w)
					}
				}
			}
		}
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "custom"} {
			r := httptest.NewRequest(method, p, nil)
			r.Header.Set("X-User-Id", "owner")
			r.Header.Set("If-None-Match", tag)
			w := httptest.NewRecorder()
			f.h.ServeHTTP(w, r)
			if w.Code != 405 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"GET, HEAD"}) {
				t.Fatal(w)
			}
		}
	}
	paths := []string{"/_appkit/", "/_appkit/banner.html", "/_appkit/nope.css"}
	for name := range files {
		paths = append(paths, page.StaticPrefix+name+"/", page.StaticPrefix+name+"/x", page.StaticPrefix+strings.ToUpper(name))
	}
	for _, path := range paths {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
			for _, match := range []string{"", "*", `"absent"`, "malformed"} {
				r := httptest.NewRequest(method, path, nil)
				r.Header.Set("X-User-Id", "owner")
				r.Header.Set("If-None-Match", match)
				got := httptest.NewRecorder()
				f.h.ServeHTTP(got, r)
				if got.Code != 404 || strings.Contains(got.Body.String(), "There is nothing at this address.") {
					t.Fatal(path, got)
				}
			}
		}
	}
}

// R-CA5Y-K4F3
func sharedFiles(t *testing.T, h http.Handler) map[string]string {
	t.Helper()
	files := map[string]string{"theme.css": "text/css; charset=utf-8", "launcher.js": "text/javascript; charset=utf-8", "feedback.js": "text/javascript; charset=utf-8", "favicon.svg": "image/svg+xml", "OFL.txt": "text/plain; charset=utf-8", "TABLER-LICENSE.txt": "text/plain; charset=utf-8"}
	css := serve(h, "GET", "/_appkit/theme.css", "owner", "", "request")
	if css.Code != 200 {
		t.Fatalf("font discovery: status %d", css.Code)
	}
	files[strings.TrimPrefix(page.PreloadURL(), page.StaticPrefix)] = "font/woff2"
	for _, match := range regexp.MustCompile(`url\("([^"]*)"\)`).FindAllStringSubmatch(css.Body.String(), -1) {
		name := match[1]
		if strings.HasSuffix(name, ".woff2") && !strings.ContainsAny(name, "/\\:\"?#%") {
			files[name] = "font/woff2"
		}
	}
	return files
}

// R-CIP9-8ILY
func TestSharedStaticPlainFontAliasesMissing(t *testing.T) {
	f := makeFixture(t, nil)
	hashed := regexp.MustCompile(`\.[0-9a-fA-F]+\.woff2$`)
	for name, mime := range sharedFiles(t, f.h) {
		if mime != "font/woff2" || !hashed.MatchString(name) {
			continue
		}
		path := page.StaticPrefix + hashed.ReplaceAllString(name, ".woff2")
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "custom"} {
			for _, match := range []string{"", "*", `"absent"`, "malformed"} {
				r := httptest.NewRequest(method, path, nil)
				r.Header.Set("X-User-Id", "owner")
				r.Header.Set("If-None-Match", match)
				w := httptest.NewRecorder()
				f.h.ServeHTTP(w, r)
				if w.Code != 404 || strings.Contains(w.Body.String(), "There is nothing at this address.") {
					t.Fatalf("%s %s validator %q: %d %s", method, path, match, w.Code, w.Body.String())
				}
			}
		}
	}
}

var _ io.Reader = (*sequence)(nil)

func repositoryFixture(t *testing.T, f *fixture) string {
	t.Helper()
	root := t.TempDir()
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath(runner.Interpreter)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Dir(binary) + ":" + filepath.Dir(python)
	env := []string{"HOME=" + root, "XDG_CONFIG_HOME=" + root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "PATH=" + path, "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test", "GIT_AUTHOR_DATE=2025-03-04T05:06:07Z", "GIT_COMMITTER_DATE=2025-03-04T05:06:07Z"}
	repo := "rep_1234567890abcdef"
	dir := filepath.Join(root, repo+".git")
	command := func(input string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = root
		cmd.Env = env
		cmd.Stdin = strings.NewReader(input)
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %v %s", args, e, out)
		}
		return strings.TrimSpace(string(out))
	}
	command("", "init", "--bare", "--initial-branch=main", dir)
	command("", "--git-dir", dir, "config", "ikigenba.owner", "owner")
	command("", "--git-dir", dir, "config", "ikigenba.name", "Repository fixture")
	blob := command("while True: pass\n", "--git-dir", dir, "hash-object", "-w", "--stdin")
	tree := command("100644 blob "+blob+"\tmain.py\n", "--git-dir", dir, "mktree")
	commit := command("fixture commit\n", "--git-dir", dir, "commit-tree", tree)
	command("", "--git-dir", dir, "update-ref", "refs/heads/main", commit)
	command("", "--git-dir", dir, "update-ref", "refs/heads/alternate", commit)
	g, err := git.Find(filepath.Dir(binary), func() []string { return env })
	if err != nil {
		t.Fatal(err)
	}
	f.cfg.Source = source.New(source.Config{Repos: root, Git: g, Limits: f.cfg.Limits})
	s := f.cfg.Limits.Settings()
	f.cfg.Runs = runs.New(runs.Config{MaxActive: 100, MaxQueued: 100, Store: f.cfg.Store, Source: f.cfg.Source, Writer: f.cfg.Telemetry, Runs: t.TempDir(), Path: path, Now: f.cfg.Telemetry.Now, Rand: &sequence{}, ScriptAfter: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, ScriptSeconds: s.ScriptSeconds, OutputMaxBytes: s.OutputMaxBytes, KeepDays: s.RunKeepDays, KeepCount: s.RunKeepCount})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		f.cfg.Runs.Drain(ctx)
	})
	return repo
}

// R-8DQL-6GQE
func TestRoutesExistingRunFileToFiles(t *testing.T) {
	f := makeFixture(t, nil)
	repo := repositoryFixture(t, f)
	sc, err := f.cfg.Store.Create(context.Background(), store.Draft{Owner: "owner", Name: "report", Repo: repo, Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.cfg.Store.AddRun(context.Background(), store.Run{ID: "run_1234567890abcdef", Script: sc.ID, SHA: strings.Repeat("a", 40), Ref: "main", User: "owner", RequestID: "initial", Trigger: "manual", Status: store.StatusRunning, Started: f.cfg.Telemetry.Now()})
	if err != nil {
		t.Fatal(err)
	}
	folder := f.cfg.Runs.Folder(run)
	if err = os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(folder, runs.StdoutFile), []byte("actual run stdout\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.cfg.MCP = mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "fixture", Telemetry: f.cfg.Telemetry})
	h := web.Handler(f.cfg)
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	files := identity.Require(web.Files(web.FilesConfig{Banner: f.cfg.Banner, Pages: set, Store: f.cfg.Store, Runs: f.cfg.Runs}))
	for _, method := range []string{"GET", "HEAD"} {
		path := "/report/runs/" + run.ID + "/stdout"
		got := serve(h, method, path, "owner", "", "request")
		same(t, got, serve(files, method, path, "owner", "", "request"))
		if got.Code != 200 || got.Header().Get("Content-Type") != "text/plain; charset=utf-8" || (method == "GET" && got.Body.String() != "actual run stdout\n") {
			t.Fatal(got)
		}
	}
}

type beforeWrite struct {
	*httptest.ResponseRecorder
	check func()
}

func (w beforeWrite) WriteHeader(status int) { w.check(); w.ResponseRecorder.WriteHeader(status) }
func (w beforeWrite) Write(body []byte) (int, error) {
	w.check()
	return w.ResponseRecorder.Write(body)
}

// R-CGFM-74IU
func TestRequestStartedPrecedesFirstResponseWrite(t *testing.T) {
	f := makeFixture(t, nil)
	checked := false
	w := beforeWrite{httptest.NewRecorder(), func() {
		if checked {
			return
		}
		checked = true
		if err := f.cfg.Telemetry.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		events := f.capture.Events()
		if len(events) != 1 || events[0].Name != "request.started" || !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"method": "GET", "path": "/about"}) {
			t.Fatal(events)
		}
	}}
	r := httptest.NewRequest("GET", "/about", nil)
	r.Header.Set("X-User-Id", "owner")
	r.Header.Set("X-Request-Id", "request")
	f.h.ServeHTTP(w, r)
	if !checked {
		t.Fatal("response never written")
	}
}

// R-6WT0-PZFI R-6Y0X-3R67
func TestEventHandlersOutsideIdentity(t *testing.T) {
	f := makeFixture(t, nil)
	handlers := events.Handlers{"*": f.cfg.Runs.Deliver}
	for _, path := range []string{events.EventsPath, events.DeclarationsPath} {
		direct := events.DeliveryHandler(handlers)
		if path == events.DeclarationsPath {
			direct = events.DeclarationsHandler(nil, handlers)
		}
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE"} {
			for _, user := range []string{"", "owner"} {
				for _, body := range []string{"", "not-json"} {
					request := func() *http.Request {
						r := httptest.NewRequest(method, path+"?q=1", strings.NewReader(body))
						r.Header.Set("Content-Type", "application/json")
						r.Header.Set("X-User-Id", user)
						r.Header.Set("X-User-Email", "person@example.test")
						r.Header.Set("X-Request-Id", "chosen-request")
						return r
					}
					got, want := httptest.NewRecorder(), httptest.NewRecorder()
					f.h.ServeHTTP(got, request())
					direct.ServeHTTP(want, request().WithContext(identity.NewContext(context.Background(), identity.Caller{UserID: user, Email: "person@example.test", RequestID: "chosen-request"})))
					same(t, got, want)
				}
			}
		}
	}
	got := serve(f.h, "GET", events.DeclarationsPath, "", "", "declaration-request")
	var declarations map[string][]string
	if err := json.Unmarshal(got.Body.Bytes(), &declarations); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(declarations, map[string][]string{"emits": {}, "accepts": {"*"}}) {
		t.Fatal(declarations)
	}
	sc, err := f.cfg.Store.Create(context.Background(), store.Draft{Owner: "owner", Name: "report", Repo: "absent-repo", Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.cfg.Store.Subscribe(context.Background(), sc.ID, "repo.pushed"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"repo.created", "repo.pushed"} {
		d := events.Delivery{Event: events.Event{ID: "evt_2222222222222222", Seq: 1, Received: f.cfg.Telemetry.Now(), Time: f.cfg.Telemetry.Now(), Service: "repos", Name: name, Attrs: events.Attrs{}}, Attempt: 1}
		raw, e := deliveryJSON(d)
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest("POST", events.EventsPath, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Request-Id", "event-request")
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		var outcome map[string]string
		if e = json.Unmarshal(w.Body.Bytes(), &outcome); e != nil || w.Code != 200 {
			t.Fatal(w, e)
		}
		want := "skip"
		count := 0
		if name == "repo.pushed" {
			want = "ok"
			count = 1
		}
		if !reflect.DeepEqual(outcome, map[string]string{"outcome": want}) {
			t.Fatal(outcome)
		}
		rs, e := f.cfg.Store.Runs(context.Background(), sc.ID)
		if e != nil || len(rs) != count {
			t.Fatal(rs, e)
		}
		if count == 1 && (rs[0].Event != d.Event.ID || rs[0].Trigger != store.TriggerEvent || rs[0].RequestID != "event-request") {
			t.Fatal(rs)
		}
	}
	if err = f.cfg.Telemetry.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, event := range f.capture.Events() {
		if event.RequestID == "event-request" && (event.Name == "request.started" || event.Name == "request.finished") {
			seen[event.Name]++
		}
	}
	if seen["request.started"] != 2 || seen["request.finished"] != 2 {
		t.Fatal(seen)
	}
}

func deliveryJSON(d events.Delivery) ([]byte, error) {
	b, err := json.Marshal(d.Event)
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err = json.Unmarshal(b, &obj); err != nil {
		return nil, err
	}
	obj["attempt"] = json.RawMessage(fmt.Sprint(d.Attempt))
	return json.Marshal(obj)
}
