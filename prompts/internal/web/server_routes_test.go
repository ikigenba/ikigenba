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
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/pages"
	"github.com/ikigenba/ikigenba/prompts/internal/runs"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
	"github.com/ikigenba/ikigenba/prompts/internal/tools"
	"github.com/ikigenba/ikigenba/prompts/internal/web"
	"golang.org/x/sys/unix"
)

type routeFixture struct {
	f       *fixture
	cfg     web.Config
	h       http.Handler
	capture *telemetry.Capture
	stderr  bytes.Buffer
}

func routes(t *testing.T, sink telemetry.Sink, servicesPath string) *routeFixture {
	t.Helper()
	t.Setenv(services.Variable, servicesPath)
	f := setup(t)
	q := &routeFixture{f: f, capture: &telemetry.Capture{}}
	if sink == nil {
		sink = q.capture
	}
	clock := func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC) }
	writer := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: sink, Stderr: &q.stderr, Now: clock, Rand: bytes.NewReader(make([]byte, 65536)), Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { writer.Shutdown(context.Background(), "fixture done") })
	var random []byte
	for i := 0; i < 128; i++ {
		random = append(random, 0, 0, 0, 0, 0, 0, 0, byte(i))
	}
	core := runs.New(runs.Config{Store: f.cfg.Store, Writer: writer, Runs: f.root, Now: clock, Rand: bytes.NewReader(random), KeepDays: 3, KeepCount: 7, MaxActive: 1, MaxQueued: 2, PromptSeconds: 1, OutputMaxBytes: 100, MaxToolCalls: 1, RunMemoryMaxBytes: 1, RunPidsMax: 1, Cgroup: filepath.Join(t.TempDir(), "absent-group"), ScriptAfter: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	// R-FG5U-MY9W: use every exported dependency and retention field.
	q.cfg = web.Config{Banner: f.cfg.Banner, MCP: mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "fixture", Telemetry: writer}), ServicesPath: servicesPath, Store: f.cfg.Store, Runs: core, KeepDays: 3, KeepCount: 7, Telemetry: writer}
	q.h = web.Handler(q.cfg)
	return q
}

func routeRequest(method, path string) *http.Request {
	r := req(method, path)
	r.Host = "prompts.fixture.test"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-User-Email", "route@example.test")
	return r
}
func routeResponse(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func routeSame(t *testing.T, a, b *httptest.ResponseRecorder) {
	t.Helper()
	if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || !bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) {
		t.Fatalf("routing differs: %d %v %q versus %d %v %q", a.Code, a.Header(), a.Body.Bytes(), b.Code, b.Header(), b.Body.Bytes())
	}
}
func routePages(t *testing.T, q *routeFixture) http.Handler {
	t.Helper()
	set, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	return identity.Require(pages.Handler(pages.Config{Banner: q.cfg.Banner, Pages: set, ServicesPath: q.cfg.ServicesPath, Store: q.cfg.Store, Runs: q.cfg.Runs, MCP: q.cfg.MCP, KeepDays: q.cfg.KeepDays, KeepCount: q.cfg.KeepCount}))
}

// R-Y4TG-OMI3: gate every non-exempt spelling before its route or method.
func TestRoutesMissingIdentity(t *testing.T) {
	q := routes(t, nil, "")
	for _, state := range []string{"open", "failing", "closed"} {
		q.f.d.SetFailing(state == "failing")
		if state == "closed" {
			if err := q.f.d.Close(); err != nil {
				t.Fatal(err)
			}
		}
		for _, path := range []string{"/", "/_appkit/theme.css", "/mcp", "/nightly-report/runs/prr_0102030405060708/stdout", "/about", "/tools", "/tools/", "/nope", "/events/", "/Events", "/declarations/repo.pushed", "/_appkit", "//", "/a/../b"} {
			for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
				for _, empty := range []bool{false, true} {
					r := routeRequest(method, path+"?route=/events")
					r.Body = io.NopCloser(strings.NewReader("fixture-body"))
					r.Header.Set("X-Other", "fixture")
					r.Header.Del("X-User-Id")
					if empty {
						r.Header["X-User-Id"] = []string{"", "later"}
					}
					want := routeResponse(identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("gate reached downstream") })), r.Clone(r.Context()))
					routeSame(t, routeResponse(q.h, r), want)
				}
			}
		}
	}
}

// R-HYEE-XZ5A R-I0U7-PIMO: MCP is mounted exactly, with exactly the registered discovery surface.
func TestRoutesMCP(t *testing.T) {
	q := routes(t, nil, "")
	r := routeRequest("GET", "/mcp?fixture=1")
	routeSame(t, routeResponse(q.h, r), routeResponse(identity.Require(q.cfg.MCP), r.Clone(r.Context())))
	srv := httptest.NewServer(q.h)
	t.Cleanup(srv.Close)
	other := mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "fixture", Telemetry: q.cfg.Telemetry})
	tools.Register(other, tools.Config{Store: q.cfg.Store, Runs: q.cfg.Runs, Telemetry: q.cfg.Telemetry})
	ref := httptest.NewServer(identity.Require(other))
	t.Cleanup(ref.Close)
	caller := identity.Caller{UserID: "alice", RequestID: "discover-fixture"}
	got, e := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp"}).ListTools(context.Background(), caller)
	if e != nil {
		t.Fatal(e)
	}
	want, e := mcp.NewClient(mcp.ClientConfig{Endpoint: ref.URL}).ListTools(context.Background(), caller)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tool registration differs: %#v versus %#v", got, want)
	}
}

// R-Y3LK-AURE R-Y61D-2E8S R-Y799-G5ZH R-Y8H5-TXQ6: use path alone and preserve delegated answers, including redirects.
func TestRoutesPages(t *testing.T) {
	q := routes(t, nil, "")
	p := create(t, q.f, "nightly-report", "alice")
	ref := routePages(t, q)
	for _, path := range []string{"/", "/about", "/tools", "/tools/", "/%74ools", "/%61bout", "/" + p.Name + "/", "/" + p.Name, "/nope", "/about/", "/mcp/tools", "/_appkit", "/mcp/", "/events/", "/events/x", "/declarations/repo.pushed", "/Events", "//", "/a/../b"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			for _, host := range []string{"prompts.sbx.ikigenba.dev", "backend", "example.org"} {
				r := routeRequest(method, path+"?route=/mcp")
				r.Host = host
				routeSame(t, routeResponse(q.h, r), routeResponse(ref, r.Clone(r.Context())))
			}
		}
	}
}

// R-I5PT-8LLG: preserve the file handler's results for files, absent records and unsupported methods.
func TestRoutesRunFiles(t *testing.T) {
	q := routes(t, nil, "")
	p := create(t, q.f, "nightly-report", "alice")
	run := add(t, q.f, p, 17, store.StatusRunning)
	put(t, filepath.Join(q.cfg.Runs.Folder(run), runs.StdoutFile), []byte("fixture file bytes"))
	set, e := pages.Load()
	if e != nil {
		t.Fatal(e)
	}
	ref := identity.Require(web.Files(web.FilesConfig{Banner: q.cfg.Banner, Pages: set, Store: q.cfg.Store, Runs: q.cfg.Runs}))
	for _, path := range []string{base(p, run) + runs.StdoutFile, base(p, run) + "files/report.txt", base(p, run) + "absent", "/absent/runs/" + run.ID + "/stdout"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			r := routeRequest(method, path)
			routeSame(t, routeResponse(q.h, r), routeResponse(ref, r.Clone(r.Context())))
		}
	}
}

// R-I6XP-MDC5: shared assets use appkit's complete response, independent of prompt routing.
func TestRoutesAppkit(t *testing.T) {
	q := routes(t, nil, "")
	for _, path := range []string{page.StaticPrefix + "theme.css", page.StaticPrefix + "nope.css", page.StaticPrefix} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			r := routeRequest(method, path)
			routeSame(t, routeResponse(q.h, r), routeResponse(page.Static(), r.Clone(r.Context())))
		}
	}
}

// R-IFH0-ARJ0 R-IGOW-OJ9P: only exact events paths bypass identity and preserve appkit delivery/declarations.
func TestRoutesEvents(t *testing.T) {
	q := routes(t, nil, "")
	handlers := events.Handlers{"*": q.cfg.Runs.Deliver}
	for path, ref := range map[string]http.Handler{events.EventsPath: events.DeliveryHandler(handlers), events.DeclarationsPath: events.DeclarationsHandler(nil, handlers)} {
		for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
			for _, user := range []string{"", "alice"} {
				r := routeRequest(method, path)
				r.Header.Set("X-User-Id", user)
				r.Header.Set("Content-Type", "application/json")
				r.Body = io.NopCloser(bytes.NewBufferString("invalid-json"))
				rr := r.Clone(identity.NewContext(r.Context(), identity.Caller{UserID: user, Email: r.Header.Get("X-User-Email"), RequestID: r.Header.Get("X-Request-Id")}))
				rr.Body = io.NopCloser(bytes.NewBufferString("invalid-json"))
				routeSame(t, routeResponse(q.h, r), routeResponse(ref, rr))
			}
		}
	}
	p := create(t, q.f, "delivery-prompt", "alice")
	if _, e := q.cfg.Store.Subscribe(context.Background(), p.ID, "repo.pushed"); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"repo.created", "repo.pushed"} {
		body, e := routeEvent(events.Event{ID: "evt_0123456789abcdef", Name: name, Service: "repos", Time: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), Attrs: events.Attrs{}})
		if e != nil {
			t.Fatal(e)
		}
		r := routeRequest("POST", events.EventsPath)
		r.Header.Del("X-User-Id")
		r.Header.Set("Content-Type", "application/json")
		r.Body = io.NopCloser(bytes.NewReader(body))
		w := routeResponse(q.h, r)
		var out map[string]string
		if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		want := "skip"
		count := 0
		if name == "repo.pushed" {
			want = "ok"
			count = 1
		}
		if w.Code != 200 || out["outcome"] != want {
			t.Fatalf("delivery: %d %v", w.Code, out)
		}
		records, e := q.cfg.Store.Runs(context.Background(), p.ID)
		if e != nil {
			t.Fatal(e)
		}
		if len(records) != count {
			t.Fatalf("delivery runs: %#v", records)
		}
		if count == 1 && (records[0].Trigger != store.TriggerEvent || records[0].Event != "evt_0123456789abcdef" || records[0].RequestID != r.Header.Get("X-Request-Id")) {
			t.Fatalf("delivery identity: %#v", records[0])
		}
	}
}

// R-IHWT-2B0E: email is optional for signed-in callers, including unsupported methods.
func TestRoutesOptionalEmail(t *testing.T) {
	q := routes(t, nil, "")
	for _, path := range []string{"/", "/about", "/mcp", "/_appkit/theme.css", "/absent"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			a := routeRequest(method, path)
			a.Header.Del("X-User-Email")
			b := a.Clone(a.Context())
			b.Header.Set("X-User-Email", "")
			wa, wb := routeResponse(q.h, a), routeResponse(q.h, b)
			routeSame(t, wa, wb)
			if wa.Code == 500 {
				t.Fatal("empty email refused")
			}
		}
	}
}

// R-I224-3ADD R-IALE-ROK8 R-I9DI-DWTJ R-ID17-J81M: middleware encloses every response and preserves each request's identity and measurements.
func TestRoutesTelemetry(t *testing.T) {
	q := routes(t, nil, "")
	for i, tc := range []struct {
		method, path string
		user         bool
		failing      bool
	}{{"GET", "/mcp", false, false}, {"HEAD", "/", true, false}, {"GET", "/", true, true}, {"GET", "/absent", true, false}, {"POST", "/about", true, false}, {"GET", events.DeclarationsPath, false, false}} {
		q.f.d.SetFailing(tc.failing)
		r := routeRequest(tc.method, tc.path)
		if !tc.user {
			r.Header.Del("X-User-Id")
		}
		id := fmt.Sprintf("request-fixture-%d", i)
		r.Header.Set("X-Request-Id", id)
		if i == 5 {
			r.Header["X-Request-Id"] = []string{"", "ignored"}
		}
		before := len(q.capture.Events())
		w := routeResponse(q.h, r)
		if e := q.cfg.Telemetry.Flush(context.Background()); e != nil {
			t.Fatal(e)
		}
		es := q.capture.Events()[before:]
		if len(es) != 2 || es[0].Name != "request.started" || es[1].Name != "request.finished" {
			t.Fatalf("request envelope %#v", es)
		}
		if i == 5 {
			id = es[0].RequestID
			if id == "" || id == "ignored" {
				t.Fatalf("unminted request id %q", id)
			}
		}
		for _, e := range es {
			if e.RequestID != id || e.User != r.Header.Get("X-User-Id") {
				t.Fatalf("request identity %#v", e)
			}
		}
		if !reflect.DeepEqual(es[0].Attrs, telemetry.Attrs{"method": tc.method, "path": r.URL.Path}) {
			t.Fatalf("start attrs %#v", es[0].Attrs)
		}
		want := telemetry.Attrs{"status": int64(w.Code), "duration_us": int64(0), "request_bytes": int64(0), "response_bytes": int64(w.Body.Len())}
		if !reflect.DeepEqual(es[1].Attrs, want) {
			t.Fatalf("finish attrs %#v want %#v", es[1].Attrs, want)
		}
	}
	if q.stderr.Len() != 0 {
		t.Fatalf("request diagnostics %q", q.stderr.String())
	}
}

// R-IJ4P-G2R3: concurrent answers retain their own caller and request id under the race detector.
func TestRoutesConcurrent(t *testing.T) {
	q := routes(t, nil, "")
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := routeRequest("GET", "/about")
			r.Header.Set("X-User-Id", fmt.Sprintf("user-%d", i))
			r.Header.Set("X-Request-Id", fmt.Sprintf("request-%d", i))
			w := routeResponse(q.h, r)
			if w.Code != 200 {
				t.Errorf("concurrent status %d", w.Code)
			}
		}(i)
	}
	wg.Wait()
	if e := q.cfg.Telemetry.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	es := q.capture.Events()
	if len(es) != 48 {
		t.Fatalf("event count %d", len(es))
	}
	seen := map[string][]string{}
	for _, e := range es {
		seen[e.RequestID] = append(seen[e.RequestID], e.Name)
		var i int
		if _, err := fmt.Sscanf(e.RequestID, "request-%d", &i); err != nil || e.User != fmt.Sprintf("user-%d", i) {
			t.Fatalf("crossed identity %#v", e)
		}
	}
	for _, names := range seen {
		if !reflect.DeepEqual(names, []string{"request.started", "request.finished"}) {
			t.Fatalf("crossed sequence %v", names)
		}
	}
}

type routeSink struct {
	mode    string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *routeSink) Deliver(ctx context.Context, _ telemetry.Event) error {
	if s.mode == "error" {
		return errors.New("fixture sink refusal")
	}
	if s.mode == "block" {
		s.once.Do(func() { close(s.entered) })
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// R-IBTB-5GAX: a sink's failure or blocked delivery cannot delay either pages or MCP product.
func TestRoutesSinkIndependent(t *testing.T) {
	var wantPage *httptest.ResponseRecorder
	var wantResult mcp.Result
	for _, mode := range []string{"ok", "error", "block"} {
		t.Run(mode, func(t *testing.T) {
			sink := &routeSink{mode: mode, entered: make(chan struct{}), release: make(chan struct{})}
			if mode == "block" {
				defer close(sink.release)
			}
			q := routes(t, sink, "")
			srv := httptest.NewServer(q.h)
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan struct{})
			var gotPage *httptest.ResponseRecorder
			var gotResult mcp.Result
			var err error
			go func() {
				gotPage = routeResponse(q.h, routeRequest("GET", "/"))
				gotResult, err = mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp"}).CallTool(ctx, identity.Caller{UserID: "alice", RequestID: "sink-fixture"}, "list", json.RawMessage(`{}`))
				close(done)
			}()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("sink blocked product")
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "ok" {
				wantPage = gotPage
				wantResult = gotResult
			} else {
				routeSame(t, gotPage, wantPage)
				if !reflect.DeepEqual(gotResult, wantResult) {
					t.Fatalf("sink changed tool result %#v versus %#v", gotResult, wantResult)
				}
			}
			if mode == "block" {
				select {
				case <-sink.entered:
				case <-ctx.Done():
					t.Fatal("sink never entered")
				}
			}
		})
	}
}

// R-IE93-WZSB: reading services, pages, all tools and subscribed deliveries never dials service sockets.
func TestRoutesNoServiceDial(t *testing.T) {
	dir, e := os.MkdirTemp("", "routes-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := os.RemoveAll(dir); e != nil {
			t.Error(e)
		}
	})
	socket := filepath.Join(dir, "unused.sock")
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if err := l.Close(); err != nil {
			t.Error(err)
		}
	}()
	// Prove that the queue probe observes a completed and already closed dial.
	control, e := net.DialUnix("unix", nil, &net.UnixAddr{Name: socket, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	if e = control.Close(); e != nil {
		t.Fatal(e)
	}
	if !routeSocketPending(t, l) {
		t.Fatal("socket queue probe missed the positive control")
	}
	if routeSocketPending(t, l) {
		t.Fatal("positive control left another pending connection")
	}
	path := filepath.Join(dir, "services.json")
	put(t, path, []byte(fmt.Sprintf(`{"services":[{"name":"dummy","description":"fixture service","enabled":true,"url":"https://dummy.fixture.test","socket":%q,"mcp":true}]}`, socket)))
	entries, e := services.Read(path)
	if e != nil || len(entries) != 1 || entries[0].Socket != socket {
		t.Fatalf("invalid services fixture: %v %#v", e, entries)
	}
	q := routes(t, nil, path)
	srv := httptest.NewServer(q.h)
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp"})
	model := agentkit.Catalog()[0].Model
	p, e := q.cfg.Store.Create(context.Background(), store.Draft{Name: "socket-prompt", Owner: "alice", Model: model, Prompt: "fixture prompt"})
	if e != nil {
		t.Fatal(e)
	}
	run := add(t, q.f, p, 41, store.StatusRunning)
	call := func(name string, args any) {
		t.Helper()
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err = client.CallTool(ctx, identity.Caller{UserID: "alice", RequestID: "socket-fixture"}, name, b); err != nil {
			t.Fatal(err)
		}
	}
	call("list", map[string]any{})
	call("show", map[string]any{"name": p.Name})
	call("create", map[string]any{"name": "socket-created", "model": model, "prompt": "fixture prompt"})
	call("update", map[string]any{"name": p.Name, "prompt": "fixture updated"})
	call("subscribe", map[string]any{"name": p.Name, "event": "repo.pushed"})
	call("unsubscribe", map[string]any{"name": p.Name, "event": "repo.pushed"})
	call("run", map[string]any{"name": p.Name})
	call("runs", map[string]any{"name": p.Name})
	call("result", map[string]any{"run": run.ID})
	call("cancel", map[string]any{"run": run.ID})
	call("delete", map[string]any{"name": "socket-created"})
	for _, path := range []string{"/", "/" + p.Name + "/", base(p, run)} {
		routeResponse(q.h, routeRequest("GET", path))
	}
	if _, e = q.cfg.Store.Subscribe(context.Background(), p.ID, "repo.pushed"); e != nil {
		t.Fatal(e)
	}
	body, e := routeEvent(events.Event{ID: "evt_fedcba9876543210", Name: "repo.pushed", Service: "repos", Time: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), Attrs: events.Attrs{}})
	if e != nil {
		t.Fatal(e)
	}
	r := routeRequest("POST", events.EventsPath)
	r.Header.Del("X-User-Id")
	r.Header.Set("Content-Type", "application/json")
	r.Body = io.NopCloser(bytes.NewReader(body))
	routeResponse(q.h, r)
	if routeSocketPending(t, l) {
		t.Fatal("handler connected to service socket")
	}

}

func routeEvent(e events.Event) ([]byte, error) {
	e.Seq = 1
	e.Received = e.Time
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err = json.Unmarshal(b, &body); err != nil {
		return nil, err
	}
	body["attempt"] = json.RawMessage("1")
	return json.Marshal(body)
}

// The recorder checks at the first write, so the start cannot be emitted late.
type routeWriteObserver struct {
	*httptest.ResponseRecorder
	t       *testing.T
	q       *routeFixture
	id      string
	checked bool
}

func (o *routeWriteObserver) check() {
	if o.checked {
		return
	}
	o.checked = true
	if err := o.q.cfg.Telemetry.Flush(context.Background()); err != nil {
		o.t.Fatal(err)
	}
	var names []string
	for _, e := range o.q.capture.Events() {
		if e.RequestID == o.id {
			names = append(names, e.Name)
		}
	}
	if !reflect.DeepEqual(names, []string{"request.started"}) {
		o.t.Fatalf("events before first write: %v", names)
	}
}
func (o *routeWriteObserver) WriteHeader(code int) { o.check(); o.ResponseRecorder.WriteHeader(code) }
func (o *routeWriteObserver) Write(b []byte) (int, error) {
	o.check()
	return o.ResponseRecorder.Write(b)
}

// R-I9DI-DWTJ R-IALE-ROK8 R-ID17-J81M: the enclosing events precede writes and enclose successful tool events and refusals without diagnostics.
func TestRoutesTelemetryWritesAndTools(t *testing.T) {
	q := routes(t, nil, "")
	r := routeRequest("GET", "/about")
	id := r.Header.Get("X-Request-Id")
	observer := &routeWriteObserver{ResponseRecorder: httptest.NewRecorder(), t: t, q: q, id: id}
	q.h.ServeHTTP(observer, r)
	if !observer.checked {
		t.Fatal("response never written")
	}
	srv := httptest.NewServer(q.h)
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp"})
	for _, name := range []string{"create", "show"} {
		caller := identity.Caller{UserID: "tool-caller", Email: "tool@example.test", RequestID: "tool-request-" + name}
		args := map[string]any{"name": "tool-prompt"}
		if name == "create" {
			args["model"] = agentkit.Catalog()[0].Model
			args["prompt"] = "fixture authored prompt"
		} else {
			args["name"] = "absent"
		}
		b, e := json.Marshal(args)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = client.CallTool(context.Background(), caller, name, b); e != nil {
			t.Fatal(e)
		}
		if e = q.cfg.Telemetry.Flush(context.Background()); e != nil {
			t.Fatal(e)
		}
		var es []telemetry.Event
		for _, e := range q.capture.Events() {
			if e.RequestID == caller.RequestID {
				es = append(es, e)
			}
		}
		if len(es) < 3 || es[0].Name != "request.started" || es[len(es)-1].Name != "request.finished" {
			t.Fatalf("tool event envelope %#v", es)
		}
		counts := map[string]int{}
		for _, e := range es {
			counts[e.Name]++
			if e.User != caller.UserID {
				t.Fatalf("tool caller lost %#v", e)
			}
		}
		if counts["request.started"] != 1 || counts["request.finished"] != 1 {
			t.Fatalf("duplicate request envelope %#v", es)
		}
		if name == "create" && counts["prompt.created"] != 1 {
			t.Fatalf("missing domain event %#v", es)
		}
	}
	if q.stderr.Len() != 0 {
		t.Fatalf("request diagnostics %q", q.stderr.String())
	}
}

// Accept directly from the nonblocking listener, without net's deadline shortcut.
func routeSocketPending(t *testing.T, l *net.UnixListener) bool {
	t.Helper()
	raw, err := l.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var acceptErr error
	accepted := -1
	if err = raw.Control(func(fd uintptr) { accepted, _, acceptErr = unix.Accept4(int(fd), unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK) }); err != nil {
		t.Fatal(err)
	}
	if acceptErr == nil {
		if err = unix.Close(accepted); err != nil {
			t.Fatal(err)
		}
		return true
	}
	if !errors.Is(acceptErr, unix.EAGAIN) && !errors.Is(acceptErr, unix.EWOULDBLOCK) {
		t.Fatal(acceptErr)
	}
	return false
}

// R-Y9P2-7PGV: render the same discovery surface the mounted MCP client sees.
func TestRoutesToolsDiscoveryPage(t *testing.T) {
	q := routes(t, nil, "")
	srv := httptest.NewServer(q.h)
	t.Cleanup(srv.Close)
	infos, err := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp"}).ListTools(context.Background(), identity.Caller{UserID: "alice", RequestID: "tools-discovery"})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"list", "show", "create", "update", "delete", "subscribe", "unsubscribe", "run", "runs", "result", "cancel"}
	if len(infos) != len(names) {
		t.Fatalf("discovered %d tools", len(infos))
	}
	data := pages.ToolsData{Banner: page.Banner{Service: pages.ServiceName, Version: "fixture", Trail: []page.Level{{Name: "tools", URL: "/tools"}}}}
	for i, info := range infos {
		if info.Name != names[i] {
			t.Fatalf("tool %d: %q", i, info.Name)
		}
		first, _, _ := strings.Cut(info.Description, "\n")
		data.Tools = append(data.Tools, pages.Tool{Name: info.Name, Description: first})
	}
	templates, err := page.Templates().ParseFS(prompts.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := templates.ExecuteTemplate(&want, "tools", data); err != nil {
		t.Fatal(err)
	}
	for _, failing := range []bool{false, true} {
		q.f.d.SetFailing(failing)
		r := routeRequest("GET", "/tools?route=/mcp")
		r.Header.Set("X-Other", "fixture")
		r.Body = io.NopCloser(strings.NewReader("fixture-body"))
		got := routeResponse(q.h, r)
		if got.Code != http.StatusOK || got.Body.String() != want.String() {
			t.Fatalf("tools page: %d %q want %q", got.Code, got.Body.String(), want.String())
		}
	}
}

// R-Y8H5-TXQ6 R-Y799-G5ZH R-I5PT-8LLG: one decoded prompt route preserves each resource's escaped segments.
func TestRoutesEncodedPromptSeparator(t *testing.T) {
	q := routes(t, nil, "")
	q.f.d.SetFailing(true)
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	files := identity.Require(web.Files(web.FilesConfig{Banner: q.cfg.Banner, Pages: set, Store: q.cfg.Store, Runs: q.cfg.Runs}))
	ordinary := routeRequest("GET", "/n/runs/r/stdout")
	encoded := routeRequest("GET", "/n%2Fruns/r/stdout")
	if ordinary.URL.Path != encoded.URL.Path {
		t.Fatal("fixtures need the same decoded path")
	}
	for _, method := range []string{"GET", "HEAD"} {
		ordinary.Method, encoded.Method = method, method
		fileResponse := routeResponse(q.h, ordinary.Clone(ordinary.Context()))
		pageResponse := routeResponse(q.h, encoded.Clone(encoded.Context()))
		routeSame(t, fileResponse, routeResponse(files, ordinary.Clone(ordinary.Context())))
		routeSame(t, pageResponse, routeResponse(routePages(t, q), encoded.Clone(encoded.Context())))
		if fileResponse.Code != http.StatusServiceUnavailable || fileResponse.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Fatalf("file refusal: %d %v", fileResponse.Code, fileResponse.Header())
		}
		if pageResponse.Code != http.StatusServiceUnavailable || pageResponse.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("page refusal: %d %v", pageResponse.Code, pageResponse.Header())
		}
	}
}
