package web_test

import (
	"bytes"
	"context"
	"encoding/hex"
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

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	cron "github.com/ikigenba/ikigenba/cron"
	"github.com/ikigenba/ikigenba/cron/internal/pages"
	"github.com/ikigenba/ikigenba/cron/internal/scheduler"
	"github.com/ikigenba/ikigenba/cron/internal/store"
	"github.com/ikigenba/ikigenba/cron/internal/tools"
	"github.com/ikigenba/ikigenba/cron/internal/trail"
	"github.com/ikigenba/ikigenba/cron/internal/web"
)

type fixture struct {
	ctx    context.Context
	d      *db.DB
	cfg    web.Config
	h      http.Handler
	tc     *telemetry.Capture
	ec     *events.Capture
	stderr *bytes.Buffer
	rand   []byte
}

// R-I4ZK-FVKS R-I67G-TNBH
func setup(t *testing.T) *fixture { return setupSinks(t, nil, nil) }

func setupSinks(t *testing.T, ts telemetry.Sink, es events.Sink) *fixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	now := func() time.Time { return time.Date(2026, 10, 5, 9, 32, 0, 0, time.UTC) }
	f := &fixture{ctx: ctx, tc: &telemetry.Capture{}, ec: &events.Capture{}, stderr: &bytes.Buffer{}, rand: bytes.Repeat([]byte{0x3c}, 32000)}
	for i := range f.rand {
		f.rand[i] = byte(i/8 + 1)
	}
	d, err := db.Open(ctx, db.Config{Path: filepath.Join(t.TempDir(), "state", "cron.db"), Migrations: cron.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	f.d = d
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	st := store.New(d, store.Config{Now: now, Rand: bytes.NewReader(f.rand)})
	var output io.Writer = f.stderr
	if ts == nil {
		ts = f.tc
	} else {
		output = io.Discard
	}
	if es == nil {
		es = f.ec
	} else {
		output = io.Discard
	}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: ts, Now: now, Rand: bytes.NewReader(f.rand), Stderr: output, Sleep: func(context.Context, time.Duration) {}})
	em := events.New(events.Config{Service: pages.ServiceName, Sink: es, Now: now, Rand: bytes.NewReader(f.rand), Stderr: output, Telemetry: w, Emits: trail.Emits()})
	sch, err := scheduler.Start(ctx, scheduler.Config{Store: st, Events: em, Telemetry: w, Now: now, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, Rand: bytes.NewReader(f.rand)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sch.Stop)
	t.Cleanup(func() {
		em.Shutdown(ctx)
		w.Shutdown(ctx, "test complete")
	})
	f.cfg = web.Config{Banner: page.New(pages.ServiceName, version.Identity{Release: "test release", Commit: "test commit"}).Banner, MCP: mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "test display", Telemetry: w}), ServicesPath: "", Store: st, Scheduler: sch, Telemetry: w, Events: em}
	f.h = web.Handler(f.cfg)
	return f
}
func (f *fixture) flush(t *testing.T) {
	t.Helper()
	if err := f.cfg.Events.Flush(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.cfg.Telemetry.Flush(f.ctx); err != nil {
		t.Fatal(err)
	}
}
func request(method, path, user, id string) *http.Request {
	r := httptest.NewRequest(method, "https://cron.example.test"+path, nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	if id != "" {
		r.Header.Set("X-Request-Id", id)
	}
	return r
}
func answer(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func sameAnswer(t *testing.T, a, b *httptest.ResponseRecorder) {
	t.Helper()
	if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Body.String() != b.Body.String() {
		t.Fatalf("answers differ: %d %v %q versus %d %v %q", a.Code, a.Header(), a.Body.String(), b.Code, b.Header(), b.Body.String())
	}
}

// R-JWEC-BY4W R-JXM8-PPVL R-K19X-V13O R-ZAZT-J3XI
// R-K65J-E42G R-KC91-AYRX R-KDGX-OQIM R-KB14-X718
func TestExactRoutesAndIdentity(t *testing.T) {
	f := setup(t)
	set, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	ph := identity.Require(pages.Handler(pages.Config{Banner: f.cfg.Banner, Pages: set, ServicesPath: f.cfg.ServicesPath, Store: f.cfg.Store, Scheduler: f.cfg.Scheduler, MCP: f.cfg.MCP}))
	routes := []string{"/", "/about", "/tools", "/nope", "/mcp/", "/about/", "/tools/", "/_appkit", "//", "/events/", "/declarations/", "/About", "/about?target=/mcp", "/%61bout", "/%74ools", "/tools?target=/mcp", "/a/../about"}
	paths := append(append([]string{}, routes...), "/events", "/declarations", "/mcp", "/_appkit/theme.css", "/_appkit/nope.css")
	before, err := f.cfg.Store.List(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, path := range paths {
			for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH"} {
				for _, user := range []string{"", "signed_in"} {
					r := request(method, path, user, fmt.Sprintf("route_%d", count))
					count++
					r.Header.Set("X-User-Email", "")
					r.Header["X-User-Id"] = []string{user, "ignored_user"}
					r.Header["X-Request-Id"] = []string{r.Header.Get("X-Request-Id"), "ignored_request"}
					got := answer(f.h, r)
					if got.Header().Get("Set-Cookie") != "" {
						t.Fatal("cookie sent")
					}
					assertNoRedirect(t, got)
					var expected http.Handler
					switch {
					case r.URL.Path == "/events":
						expected = events.DeliveryHandler(nil)
					case r.URL.Path == "/declarations":
						expected = events.DeclarationsHandler(f.cfg.Events, nil)
					case user == "":
						expected = identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("missing identity admitted") }))
					case strings.HasPrefix(r.URL.Path, page.StaticPrefix):
						expected = page.Static()
					case r.URL.Path == "/mcp":
						expected = identity.Require(f.cfg.MCP)
					default:
						expected = ph
					}
					sameAnswer(t, got, answer(expected, r.Clone(f.ctx)))
					if user != "" {
						r.Header.Del("X-User-Email")
						sameAnswer(t, got, answer(f.h, r))
					}
				}
			}
		}
	}
	f.d.SetFailing(false)
	after, err := f.cfg.Store.List(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("routing changed store")
	}
	f.flush(t)
	if len(f.ec.Events()) != 0 {
		t.Fatal("non-tool request emitted bus event")
	}
	for _, e := range f.tc.Events() {
		if e.Name != "request.started" && e.Name != "request.finished" {
			t.Fatalf("unexpected event %s", e.Name)
		}
	}
	if f.stderr.Len() != 0 {
		t.Fatalf("request diagnostics: %s", f.stderr.String())
	}
}

// R-JRIQ-SV64 R-JTYJ-KENI R-JV6F-Y6E7
func TestRequestEnvelopeAndBodyCounts(t *testing.T) {
	f := setup(t)
	for i, tc := range []struct{ method, path, user, id string }{{"GET", "/", "", ""}, {"GET", "/", "user", "provided"}, {"GET", "/about", "user", ""}, {"HEAD", "/", "user", "head_root"}, {"HEAD", "/about", "user", "head_about"}, {"GET", "/tools", "user", "tools"}, {"HEAD", "/tools", "user", "head_tools"}, {"POST", "/events", "", "bus"}, {"GET", "/_appkit/theme.css", "user", "css"}} {
		r := request(tc.method, tc.path, tc.user, tc.id)
		if i == 0 {
			r.Header["X-Request-Id"] = []string{"", "ignored"}
		}
		got := httptest.NewRecorder()
		probe := &firstWriteProbe{ResponseWriter: got, before: func() {
			f.flush(t)
			es := f.tc.Events()
			if len(es) != 2*i+1 || es[len(es)-1].Name != "request.started" {
				t.Fatalf("events before first response write: %v", es)
			}
		}}
		f.h.ServeHTTP(probe, r)
		f.flush(t)
		es := f.tc.Events()
		if len(es) != 2*(i+1) {
			t.Fatalf("request event count %d want %d", len(es), 2*(i+1))
		}
		es = es[2*i:]
		id := tc.id
		if id == "" {
			offset := 0
			if i == 2 {
				offset = 16
			}
			id = hex.EncodeToString(f.rand[offset : offset+16])
		}
		if es[0].Name != "request.started" || es[1].Name != "request.finished" {
			t.Fatalf("events %v", es)
		}
		for _, e := range es {
			if e.RequestID != id || e.User != tc.user {
				t.Fatalf("envelope %+v", e)
			}
		}
		if !reflect.DeepEqual(es[0].Attrs, telemetry.Attrs{"method": tc.method, "path": r.URL.Path}) {
			t.Fatalf("start attrs %v", es[0].Attrs)
		}
		want := telemetry.Attrs{"status": int64(got.Code), "duration_us": int64(0), "request_bytes": int64(0), "response_bytes": int64(got.Body.Len())}
		if !reflect.DeepEqual(es[1].Attrs, want) {
			t.Fatalf("finish attrs %v want %v", es[1].Attrs, want)
		}
	}
}

func client(t *testing.T, h http.Handler) *mcp.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp"})
}

// R-N5S6-SZ8G
func TestRegisteredTools(t *testing.T) {
	f := setup(t)
	got, err := client(t, f.h).ListTools(f.ctx, identity.Caller{UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	srv := mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: f.cfg.Telemetry})
	tools.Register(srv, tools.Config{Store: f.cfg.Store, Scheduler: f.cfg.Scheduler})
	want, err := client(t, identity.Require(srv)).ListTools(f.ctx, identity.Caller{UserID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 || !reflect.DeepEqual(got, want) {
		t.Fatalf("tools mismatch: %v %v", got, want)
	}
}

// R-JYU5-3HMA R-JSQN-6MWT R-KFWQ-GA00 R-KH4M-U1QP R-K7DF-RVT5
func TestToolRequestIdentityAndCause(t *testing.T) {
	f := setup(t)
	c := client(t, f.h)
	cases := []struct {
		cause, depth string
		wantDepth    int
		valid        bool
	}{
		{"evt_0123456789abcdef", "0", 1, true}, {"evt_0123456789abcdef", "00012", 13, true}, {"evt_0123456789abcdef", "1000000", 1000001, true},
		{"", "", 0, false}, {"evt_0123456789abcdef", "", 0, false}, {"", "0", 0, false}, {"evt_0123456789abcdef", "-1", 0, false}, {"evt_0123456789abcdef", "one", 0, false}, {"evt_0123456789ABCDEF", "0", 0, false}, {"evt_0123456789abcdef", "+1", 0, false}, {"evt_0123456789abcdef", " 1", 0, false}, {"evt_0123456789abcdef", "1 ", 0, false}, {"evt_0123456789abcdef", "١", 0, false}, {"evt_0123456789abcde", "0", 0, false},
	}
	// A client transport supplies header edge cases; the call still uses appkit's client.
	for i, tc := range cases {
		transport := &headerTransport{base: http.DefaultTransport, cause: tc.cause, depth: tc.depth, host: "backend"}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Preserve leading/trailing whitespace that HTTP transport trims.
			if tc.cause != "" {
				r.Header["X-Event-Cause"] = []string{tc.cause, "evt_fedcba9876543210"}
			}
			if tc.depth != "" {
				r.Header["X-Event-Depth"] = []string{tc.depth, "42"}
			}
			f.h.ServeHTTP(w, r)
		}))
		cc := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: transport}})
		caller := identity.Caller{UserID: "owner", Email: "owner@example.test"}
		if i != 0 {
			caller.RequestID = fmt.Sprintf("tool_%d", i)
		}
		slug := fmt.Sprintf("item%d", i)
		result, err := cc.CallTool(f.ctx, caller, "create", json.RawMessage(fmt.Sprintf(`{"slug":%q,"when":"@hourly"}`, slug)))
		srv.Close()
		if err != nil || result.IsError() {
			t.Fatalf("create: %v %v", result, err)
		}
		f.flush(t)
		bus := f.ec.Events()
		if len(bus) != i+1 {
			t.Fatalf("bus count %d want %d", len(bus), i+1)
		}
		e := bus[i]
		id := caller.RequestID
		if id == "" {
			id = hex.EncodeToString(f.rand[:16])
		}
		cause := ""
		if tc.valid {
			cause = tc.cause
		}
		if e.Cause != cause || e.Depth != tc.wantDepth || e.User != caller.UserID || e.RequestID != id {
			t.Fatalf("bus envelope %+v", e)
		}
		es := f.tc.Events()
		if len(es) != 4*(i+1) {
			t.Fatalf("tool event count %d want %d", len(es), 4*(i+1))
		}
		es = es[4*i:]
		names := []string{"request.started", trail.Name(slug, trail.Created), "tool.called", "request.finished"}
		for j, event := range es {
			if event.Name != names[j] || event.RequestID != id || event.User != caller.UserID {
				t.Fatalf("tool telemetry %+v", es)
			}
		}
		x, err := f.cfg.Store.Get(f.ctx, slug)
		if err != nil || x.OwnerID != caller.UserID || x.OwnerEmail != caller.Email {
			t.Fatalf("caller lost: %+v %v", x, err)
		}
	}
	// The same tool under the public host produces the same refusal as backend.
	a, err := c.CallTool(f.ctx, identity.Caller{UserID: "owner"}, "show", json.RawMessage(`{"slug":"missing"}`))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(f.h)
	defer srv.Close()
	b, err := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: &headerTransport{base: http.DefaultTransport, host: "backend"}}}).CallTool(f.ctx, identity.Caller{UserID: "owner"}, "show", json.RawMessage(`{"slug":"missing"}`))
	if err != nil {
		t.Fatal(err)
	}
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if !bytes.Equal(aa, bb) {
		t.Fatal("host changed result")
	}
}

type headerTransport struct {
	base               http.RoundTripper
	cause, depth, host string
}

func (h *headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if h.cause != "" {
		r.Header.Set("X-Event-Cause", h.cause)
	}
	if h.depth != "" {
		r.Header.Set("X-Event-Depth", h.depth)
	}
	if h.host != "" {
		r.Host = h.host
	}
	return h.base.RoundTrip(r)
}

// R-KEOU-2I9B
func TestConcurrentRequestIsolation(t *testing.T) {
	f := setup(t)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Go(func() {
			r := request("GET", "/about", fmt.Sprintf("user%d", i), fmt.Sprintf("request%d", i))
			if got := answer(f.h, r); got.Code != 200 {
				t.Errorf("status %d", got.Code)
			}
		})
	}
	wg.Wait()
	f.flush(t)
	es := f.tc.Events()
	if len(es) != 64 {
		t.Fatalf("event count %d", len(es))
	}
	grouped := map[string][]telemetry.Event{}
	for _, e := range es {
		grouped[e.RequestID] = append(grouped[e.RequestID], e)
	}
	for i := 0; i < 32; i++ {
		pair := grouped[fmt.Sprintf("request%d", i)]
		if len(pair) != 2 || pair[0].Name != "request.started" || pair[1].Name != "request.finished" {
			t.Fatalf("bad pair %v", pair)
		}
		for _, e := range pair {
			if e.User != fmt.Sprintf("user%d", i) {
				t.Fatalf("mixed user %+v", e)
			}
		}
	}
}

// R-K9T8-JFAJ
func TestRequestsDoNotConnectToServices(t *testing.T) {
	f := setup(t)
	dir, err := os.MkdirTemp("", "cron-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(dir, "sibling.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ln.Close(); err != nil {
			t.Error(err)
		}
	}()
	services := filepath.Join(t.TempDir(), "services.json")
	data := fmt.Sprintf(`{"services":[{"name":"auth","url":"https://auth.example.test","description":"Auth","socket":%q,"enabled":true,"mcp":true},{"name":"scripts","url":"https://scripts.example.test","description":"Scripts","socket":%q,"enabled":true,"mcp":true}]}`, path, path)
	if err := os.WriteFile(services, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	// Rebuild using a fresh MCP server because registration occurs once.
	f.cfg.ServicesPath = services
	f.cfg.MCP = mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Telemetry: f.cfg.Telemetry})
	f.h = web.Handler(f.cfg)
	c := client(t, f.h)
	for _, call := range []struct{ name, args string }{{"create", `{"slug":"task","when":"@hourly"}`}, {"list", `{}`}, {"show", `{"slug":"task"}`}, {"update", `{"slug":"task","when":"@daily"}`}, {"pause", `{"slug":"task"}`}, {"resume", `{"slug":"task"}`}, {"delete", `{"slug":"task"}`}} {
		res, err := c.CallTool(f.ctx, identity.Caller{UserID: "u"}, call.name, json.RawMessage(call.args))
		if err != nil || res.IsError() {
			t.Fatalf("%s: %v %v", call.name, res, err)
		}
	}
	for _, path := range []string{"/", "/about", "/tools", "/nope", "/_appkit/theme.css", "/events", "/declarations"} {
		answer(f.h, request("GET", path, "u", "socket_probe"))
	}
	if err := ln.SetDeadline(time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	conn, err := ln.Accept()
	if conn != nil {
		if closeErr := conn.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal("request connected to sibling")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("accept: %v", err)
	}
	f.flush(t)
}

type telemetrySink func(context.Context, telemetry.Event) error

func (s telemetrySink) Deliver(ctx context.Context, e telemetry.Event) error { return s(ctx, e) }

type eventSink func(context.Context, events.Event) error

func (s eventSink) Deliver(ctx context.Context, e events.Event) error { return s(ctx, e) }

// R-K8LC-5NJU
func TestAnswersIndependentOfDelivery(t *testing.T) {
	var baselinePage *httptest.ResponseRecorder
	var baselineResult []byte
	for _, mode := range []string{"success", "telemetry_fails", "events_fail", "telemetry_blocks", "events_block", "both_block"} {
		t.Run(mode, func(t *testing.T) {
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			telemetryEntered := make(chan struct{}, 1)
			eventsEntered := make(chan struct{}, 1)
			var ts telemetry.Sink
			var es events.Sink
			switch mode {
			case "telemetry_fails":
				ts = telemetrySink(func(context.Context, telemetry.Event) error { return telemetry.ErrRejected })
			case "events_fail":
				es = eventSink(func(context.Context, events.Event) error { return events.ErrRejected })
			}
			if mode == "telemetry_blocks" || mode == "both_block" {
				ts = telemetrySink(func(_ context.Context, _ telemetry.Event) error {
					select {
					case telemetryEntered <- struct{}{}:
					default:
					}
					<-release
					return nil
				})
			}
			if mode == "events_block" || mode == "both_block" {
				es = eventSink(func(_ context.Context, _ events.Event) error {
					select {
					case eventsEntered <- struct{}{}:
					default:
					}
					<-release
					return nil
				})
			}
			f := setupSinks(t, ts, es)
			pageAnswer := make(chan *httptest.ResponseRecorder, 1)
			go func() { pageAnswer <- answer(f.h, request("GET", "/", "owner", "page")) }()
			var got *httptest.ResponseRecorder
			select {
			case got = <-pageAnswer:
			case <-f.ctx.Done():
				t.Fatal("page waited for delivery")
			}
			if baselinePage == nil {
				baselinePage = got
			} else {
				sameAnswer(t, got, baselinePage)
			}
			if mode == "telemetry_blocks" || mode == "both_block" {
				select {
				case <-telemetryEntered:
				case <-f.ctx.Done():
					t.Fatal("writer did not enter blocked sink")
				}
			}
			result, err := client(t, f.h).CallTool(f.ctx, identity.Caller{UserID: "owner", RequestID: "create"}, "create", json.RawMessage(`{"slug":"queued","when":"@hourly"}`))
			if err != nil || result.IsError() {
				t.Fatalf("create while %s: %v %v", mode, result, err)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if baselineResult == nil {
				baselineResult = encoded
			} else if !bytes.Equal(baselineResult, encoded) {
				t.Fatalf("sink changed result %s versus %s", encoded, baselineResult)
			}
			if mode == "events_block" || mode == "both_block" {
				select {
				case <-eventsEntered:
				case <-f.ctx.Done():
					t.Fatal("emitter did not enter blocked sink")
				}
			}
			unblock()
			f.flush(t)
		})
	}
}

// R-K7DF-RVT5 R-KB14-X718 R-KC91-AYRX
func TestToolTrailAlphabetAndQuietRefusals(t *testing.T) {
	f := setup(t)
	// Record every route's response cookies, including client-driven tool requests.
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.h.ServeHTTP(w, r)
		if w.Header().Get("Set-Cookie") != "" {
			t.Error("tool response set cookie")
		}
	})
	c := client(t, h)
	for _, call := range []struct {
		name, args string
		refused    bool
	}{
		{"create", `{"slug":"trail","when":"@hourly"}`, false},
		{"list", `{}`, false}, {"show", `{"slug":"trail"}`, false},
		{"update", `{"slug":"trail","when":"@daily"}`, false},
		{"pause", `{"slug":"trail"}`, false}, {"resume", `{"slug":"trail"}`, false},
		{"delete", `{"slug":"trail"}`, false}, {"show", `{"slug":"missing"}`, true},
		{"create", `{"slug":"BAD","when":"@hourly"}`, true},
	} {
		res, err := c.CallTool(f.ctx, identity.Caller{UserID: "owner", RequestID: "alphabet"}, call.name, json.RawMessage(call.args))
		if err != nil || res.IsError() != call.refused {
			t.Fatalf("%s: %v %v", call.name, res, err)
		}
	}
	f.d.SetFailing(true)
	res, err := c.CallTool(f.ctx, identity.Caller{UserID: "owner", RequestID: "failure"}, "list", json.RawMessage(`{}`))
	if err != nil || !res.IsError() {
		t.Fatalf("database refusal: %v %v", res, err)
	}
	f.d.SetFailing(false)
	f.flush(t)
	allowed := map[string]bool{"request.started": true, "request.finished": true, "tool.called": true}
	for _, kind := range []string{trail.Created, trail.Paused, trail.Resumed, trail.Deleted} {
		allowed[trail.Name("trail", kind)] = true
	}
	seen := map[string]bool{}
	for _, e := range f.tc.Events() {
		if !allowed[e.Name] {
			t.Fatalf("unexpected trail event %s", e.Name)
		}
		seen[e.Name] = true
	}
	for name := range allowed {
		if !seen[name] {
			t.Fatalf("event missing %s", name)
		}
	}
	if f.stderr.Len() != 0 {
		t.Fatalf("request wrote diagnostics: %s", f.stderr.String())
	}
}

// firstWriteProbe observes telemetry immediately before the first answer byte.
type firstWriteProbe struct {
	http.ResponseWriter
	before func()
	wrote  bool
}

func (p *firstWriteProbe) check() {
	if !p.wrote {
		p.wrote = true
		p.before()
	}
}
func (p *firstWriteProbe) WriteHeader(status int)      { p.check(); p.ResponseWriter.WriteHeader(status) }
func (p *firstWriteProbe) Write(b []byte) (int, error) { p.check(); return p.ResponseWriter.Write(b) }
