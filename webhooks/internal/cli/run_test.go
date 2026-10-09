package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
	"github.com/ikigenba/ikigenba/webhooks"
	"github.com/ikigenba/ikigenba/webhooks/internal/cli"
	"github.com/ikigenba/ikigenba/webhooks/internal/pages"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
	"github.com/ikigenba/ikigenba/webhooks/internal/sweeper"
)

type runOutput struct {
	mu     sync.Mutex
	writes []string
}

func (w *runOutput) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, string(b))
	return len(b), nil
}
func (w *runOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.writes, "")
}
func (w *runOutput) calls() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.writes...)
}

// runBytes yields one byte value for every byte read.
type runBytes byte

func (r runBytes) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = byte(r)
	}
	return len(b), nil
}

// runCounter yields an ever-increasing byte sequence, so every id differs.
type runCounter struct {
	mu sync.Mutex
	n  byte
}

func (r *runCounter) Read(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range b {
		r.n++
		b[i] = r.n
	}
	return len(b), nil
}

type runClock struct {
	mu        sync.Mutex
	now       time.Time
	timers    []chan time.Time
	requested chan time.Duration
}

func newRunClock() *runClock {
	return &runClock{now: runTime("2026-10-09T09:32:00Z"), requested: make(chan time.Duration, 100)}
}
func runTime(s string) time.Time {
	v, e := time.Parse(time.RFC3339, s)
	if e != nil {
		panic(e)
	}
	return v
}
func (c *runClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *runClock) set(v time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = v
}
func (c *runClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, ch)
	select {
	case c.requested <- d:
	default:
	}
	return ch
}

// fire delivers on every channel After has returned and not yet delivered on.
func (c *runClock) fire() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.timers {
		select {
		case ch <- c.now:
		default:
		}
	}
	c.timers = nil
}

type runHarness struct {
	t        *testing.T
	p        cli.Process
	clock    *runClock
	out, err *runOutput
	tc       *telemetry.Capture
	ec       *events.Capture
	listener net.Listener
	notify   *net.UnixConn
	ctx      context.Context
	cancel   context.CancelCauseFunc
	done     chan int
	url      string
	client   *mcp.Client
	keysMu   sync.Mutex
	keys     []string
	unsets   []string
	inherits []uintptr
	stopped  bool
	mcpCalls int
}

func newRunHarness(t *testing.T, dir string) *runHarness {
	t.Helper()
	t.Setenv(services.Variable, "")
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	short, e := os.MkdirTemp("", "webhooks-ready-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := os.RemoveAll(short); e != nil {
			t.Error(e)
		}
	})
	a := filepath.Join(short, "notify")
	nc, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: a, Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	h := &runHarness{t: t, clock: newRunClock(), out: &runOutput{}, err: &runOutput{}, tc: &telemetry.Capture{}, ec: &events.Capture{}, listener: ln, notify: nc, done: make(chan int, 1), url: "http://" + ln.Addr().String()}
	h.ctx, h.cancel = context.WithCancelCause(context.Background())
	env := map[string]string{"LISTEN_PID": "321", "LISTEN_FDS": "1", "NOTIFY_SOCKET": a}
	h.p = cli.Process{Pid: 321, Dir: dir, Version: "injected-code-identity", Stdout: h.out, Stderr: h.err, Now: h.clock.Now, After: h.clock.After, Rand: &runCounter{}, Sink: h.tc, EventSink: h.ec,
		LookupEnv: func(k string) (string, bool) {
			h.keysMu.Lock()
			h.keys = append(h.keys, k)
			h.keysMu.Unlock()
			v, ok := env[k]
			return v, ok
		},
		Unsetenv: func(k string) error { h.unsets = append(h.unsets, k); return nil },
		Inherit:  func(fd uintptr) (net.Listener, error) { h.inherits = append(h.inherits, fd); return ln, nil },
		Banner:   page.New(pages.ServiceName, "injected-code-identity").Banner,
		MCP: func(w *telemetry.Writer) *mcp.Server {
			h.mcpCalls++
			return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: "injected-code-identity", Telemetry: w})
		}}
	h.client = mcp.NewClient(mcp.ClientConfig{Endpoint: h.url + "/mcp"})
	t.Cleanup(func() {
		h.cancel(errors.New("cleanup"))
		_ = ln.Close()
		_ = nc.Close()
		if !h.stopped {
			select {
			case <-h.done:
			case <-time.After(5 * time.Second):
				t.Error("Run did not stop")
			}
		}
	})
	return h
}

func (h *runHarness) setEnv(key, value string) {
	lookup := h.p.LookupEnv
	h.p.LookupEnv = func(k string) (string, bool) {
		if k == key {
			lookup(k)
			return value, true
		}
		return lookup(k)
	}
}

func (h *runHarness) start() {
	h.t.Helper()
	go func() { h.done <- cli.Run(h.ctx, h.p) }()
	if e := h.notify.SetReadDeadline(time.Now().Add(5 * time.Second)); e != nil {
		h.t.Fatal(e)
	}
	b := make([]byte, 64)
	n, _, e := h.notify.ReadFromUnix(b)
	if e != nil {
		select {
		case code := <-h.done:
			h.stopped = true
			h.t.Fatalf("Run exited %d before READY: %s", code, h.err.String())
		default:
			h.t.Fatal(e)
		}
	}
	if string(b[:n]) != "READY=1" {
		h.t.Fatalf("ready %q", b[:n])
	}
}

func (h *runHarness) stop(reason string) int {
	h.t.Helper()
	h.cancel(errors.New(reason))
	select {
	case code := <-h.done:
		h.stopped = true
		return code
	case <-time.After(10 * time.Second):
		h.t.Fatal("Run did not return")
		return -1
	}
}

func (h *runHarness) result(user, tool string, args any) (map[string]json.RawMessage, bool) {
	h.t.Helper()
	var b []byte
	if args != nil {
		var e error
		if b, e = json.Marshal(args); e != nil {
			h.t.Fatal(e)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, e := h.client.CallTool(ctx, identity.Caller{UserID: user, Email: user + "@example.com", RequestID: "run-call"}, tool, b)
	if e != nil {
		h.t.Fatal(e)
	}
	raw, e := r.MarshalJSON()
	if e != nil {
		h.t.Fatal(e)
	}
	var obj map[string]json.RawMessage
	if e = json.Unmarshal(raw, &obj); e != nil {
		h.t.Fatal(e)
	}
	return obj, r.IsError()
}

func (h *runHarness) call(tool string, args any) map[string]json.RawMessage {
	h.t.Helper()
	obj, refused := h.result("owner", tool, args)
	if refused {
		h.t.Fatalf("%s refused: %s", tool, obj["content"])
	}
	return obj
}

func runContent(t *testing.T, obj map[string]json.RawMessage, out any) {
	t.Helper()
	if e := json.Unmarshal(obj["structuredContent"], out); e != nil {
		t.Fatal(e)
	}
}

type runMinted struct {
	ID, Slug, Scheme, URL, Owner, Created, Secret string
}

func (h *runHarness) create(slug string) runMinted {
	h.t.Helper()
	args := map[string]string{"slug": slug}
	var m runMinted
	runContent(h.t, h.call("create", args), &m)
	return m
}

func (h *runHarness) do(method, path string, body []byte, headers map[string]string) (int, string) {
	h.t.Helper()
	req, e := http.NewRequestWithContext(h.t.Context(), method, h.url+path, bytes.NewReader(body))
	if e != nil {
		h.t.Fatal(e)
	}
	req.Host = "webhooks.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, e := c.Do(req)
	if e != nil {
		h.t.Fatal(e)
	}
	defer func() { _ = r.Body.Close() }()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		h.t.Fatal(e)
	}
	return r.StatusCode, string(b)
}

func (h *runHarness) get(path string) (int, string) {
	return h.do("GET", path, nil, map[string]string{"X-User-Id": "owner", "X-User-Email": "owner@example.com"})
}

func (h *runHarness) post(slug, secret string, body []byte) int {
	h.t.Helper()
	code, _ := h.do("POST", "/in/"+slug, body, map[string]string{"X-Webhook-Secret": secret, "Content-Type": "text/plain"})
	return code
}

type runTelemetrySink func(context.Context, telemetry.Event) error

func (s runTelemetrySink) Deliver(c context.Context, e telemetry.Event) error { return s(c, e) }

type runEventSink func(context.Context, events.Event) error

func (s runEventSink) Deliver(c context.Context, e events.Event) error { return s(c, e) }

type runForbiddenReader struct{ touch func() }

func (r runForbiddenReader) Read([]byte) (int, error) { r.touch(); return 0, errors.New("unreachable") }

func assertNoRunNotification(h *runHarness) {
	h.t.Helper()
	if e := h.notify.SetReadDeadline(time.Now()); e != nil {
		h.t.Fatal(e)
	}
	if _, _, e := h.notify.ReadFromUnix(make([]byte, 64)); e == nil {
		h.t.Fatal("unexpected readiness notification")
	}
}

// R-X4XA-3C1M R-Y1UK-F52D
func TestRunStartupRefusalsTouchNothing(t *testing.T) {
	noSocket := "webhooks: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"
	for _, tc := range []struct {
		name, drain, retention, pid, fds, want string
	}{
		{"bad drain", "abc", "", "321", "1", "webhooks: DRAIN_SECONDS is 'abc', not a positive whole number of seconds\n"},
		{"bad drain no socket", "abc", "", "", "", "webhooks: DRAIN_SECONDS is 'abc', not a positive whole number of seconds\n"},
		{"bad retention", "", "0", "321", "1", "webhooks: WEBHOOKS_RETENTION_DAYS is '0', not a positive whole number of days\n"},
		{"both bad", "5s", "2d", "321", "1", "webhooks: DRAIN_SECONDS is '5s', not a positive whole number of seconds\n"},
		{"unset sockets", "", "", "321", "", noSocket},
		{"wrong pid", "", "", "999", "1", noSocket},
		{"zero sockets", "", "", "321", "0", noSocket},
		{"signed sockets", "", "", "321", "+1", noSocket},
		{"multiple", "", "", "321", "002", "webhooks: 002 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"},
		{"huge", "", "", "321", strings.Repeat("9", 50), "webhooks: " + strings.Repeat("9", 50) + " sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRunHarness(t, t.TempDir())
			touch := func() { t.Error("startup touched a forbidden seam") }
			h.p.LookupEnv = func(k string) (string, bool) {
				switch k {
				case "DRAIN_SECONDS":
					return tc.drain, tc.drain != ""
				case "WEBHOOKS_RETENTION_DAYS":
					return tc.retention, tc.retention != ""
				case "LISTEN_PID":
					return tc.pid, tc.pid != ""
				case "LISTEN_FDS":
					return tc.fds, tc.fds != ""
				}
				t.Errorf("early lookup %s", k)
				return "", false
			}
			h.p.Inherit = func(uintptr) (net.Listener, error) { touch(); return h.listener, nil }
			h.p.Unsetenv = func(string) error { touch(); return nil }
			h.p.Now = func() time.Time { touch(); return h.clock.Now() }
			h.p.After = func(time.Duration) <-chan time.Time { touch(); return nil }
			h.p.Sleep = func(context.Context, time.Duration) { touch() }
			h.p.Rand = runForbiddenReader{touch}
			h.p.Banner = func(page.User) page.Banner { touch(); return page.Banner{} }
			h.p.MCP = func(*telemetry.Writer) *mcp.Server { touch(); return nil }
			h.p.Sink = runTelemetrySink(func(context.Context, telemetry.Event) error { touch(); return nil })
			h.p.EventSink = runEventSink(func(context.Context, events.Event) error { touch(); return nil })
			code := cli.Run(context.Background(), h.p)
			h.stopped = true
			if code != cli.ExitUsage || h.out.String() != "" || !reflect.DeepEqual(h.err.calls(), []string{tc.want}) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, h.out.String(), h.err.calls())
			}
			assertNoRunNotification(h)
			entries, e := os.ReadDir(h.p.Dir)
			if e != nil || len(entries) != 0 {
				t.Fatalf("Dir touched: %v %v", entries, e)
			}
		})
	}
}

// R-Y32G-SWT2
func TestRunActivationUnsetsAndInheritsDescriptorThree(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	h.start()
	if code, _ := h.get("/about"); code != 200 {
		t.Fatalf("not serving on the inherited listener: %d", code)
	}
	if h.stop("done") != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	sort.Strings(h.unsets)
	if !reflect.DeepEqual(h.unsets, []string{"LISTEN_FDNAMES", "LISTEN_FDS", "LISTEN_PID"}) || !reflect.DeepEqual(h.inherits, []uintptr{3}) {
		t.Fatalf("activation unsets=%v inherits=%v", h.unsets, h.inherits)
	}
	failed := newRunHarness(t, t.TempDir())
	failed.p.Inherit = func(fd uintptr) (net.Listener, error) {
		failed.inherits = append(failed.inherits, fd)
		return nil, errors.New("descriptor is not a socket")
	}
	code := cli.Run(failed.ctx, failed.p)
	failed.stopped = true
	if code != cli.ExitServerFailed || !reflect.DeepEqual(failed.err.calls(), []string{"webhooks: descriptor is not a socket\n"}) || !reflect.DeepEqual(failed.inherits, []uintptr{3}) {
		t.Fatalf("inherit failure %d %q %v", code, failed.err.calls(), failed.inherits)
	}
	assertNoRunNotification(failed)
}

// R-Y4AD-6OJR
func TestRunDatabaseFailureBeforeReadiness(t *testing.T) {
	for _, bad := range []string{"state", "state/webhooks.db"} {
		t.Run(bad, func(t *testing.T) {
			h := newRunHarness(t, t.TempDir())
			path := filepath.Join(h.p.Dir, bad)
			if bad != "state" {
				if e := os.Mkdir(filepath.Dir(path), 0700); e != nil {
					t.Fatal(e)
				}
			}
			original := []byte("fixture, not a database\nunchanged")
			if e := os.WriteFile(path, original, 0600); e != nil {
				t.Fatal(e)
			}
			_, expected := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "webhooks.db"), Migrations: webhooks.Migrations(), Now: h.clock.Now})
			if expected == nil {
				t.Fatal("fixture unexpectedly opened")
			}
			var accepted bool
			h.p.Inherit = func(uintptr) (net.Listener, error) {
				return runListener{Listener: h.listener, accept: func() error { accepted = true; return nil }}, nil
			}
			code := cli.Run(h.ctx, h.p)
			h.stopped = true
			want := "webhooks: cannot open database state/webhooks.db: " + strings.ReplaceAll(expected.Error(), "\n", " ") + "\n"
			if code != cli.ExitServerFailed || h.out.String() != "" || !reflect.DeepEqual(h.err.calls(), []string{want}) || h.mcpCalls != 0 || accepted {
				t.Fatalf("db failure %d %q", code, h.err.calls())
			}
			b, e := fs.ReadFile(os.DirFS(h.p.Dir), bad)
			if e != nil || !bytes.Equal(b, original) {
				t.Fatalf("fixture changed %q %v", b, e)
			}
			assertNoRunNotification(h)
		})
	}
}

type runListener struct {
	net.Listener
	accept func() error
}

func (l runListener) Accept() (net.Conn, error) {
	if e := l.accept(); e != nil {
		return nil, e
	}
	return l.Listener.Accept()
}

// R-X656-H3SB R-X7D2-UVJ0 R-X9SV-MF0E R-Y95Y-PRIJ R-YBLR-HAZX R-WSQA-9MMO
func TestRunPersistenceSinksAndLifecycle(t *testing.T) {
	dir := t.TempDir()
	h := newRunHarness(t, dir)
	h.start()
	minted := h.create("tick")
	if minted.Created != "2026-10-09T09:32:00Z" {
		t.Fatalf("created not read from Now: %+v", minted)
	}
	// Requests of every kind: pages, a guest, the ingress accepting and refusing, /mcp without identity.
	if code := h.post("tick", minted.Secret, []byte("payload")); code != http.StatusAccepted {
		t.Fatalf("accept %d", code)
	}
	if code := h.post("tick", "wrong", []byte("payload")); code != http.StatusNotFound {
		t.Fatalf("refuse %d", code)
	}
	if code := h.post("nobody", "wrong", nil); code != http.StatusNotFound {
		t.Fatalf("unknown %d", code)
	}
	if code, _ := h.do("GET", "/in/tick", nil, nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("get ingress %d", code)
	}
	if code, _ := h.do("GET", "/", nil, nil); code != http.StatusFound {
		t.Fatalf("guest %d", code)
	}
	if code, _ := h.do("POST", "/mcp", nil, nil); code != http.StatusInternalServerError {
		t.Fatalf("mcp without identity %d", code)
	}
	if code, body := h.get("/"); code != 200 || !strings.Contains(body, "tick") {
		t.Fatalf("landing %d %s", code, body)
	}
	if code, _ := h.get("/nope"); code != 404 {
		t.Fatal(code)
	}
	if code := h.stop("SIGINT"); code != cli.ExitSuccess {
		t.Fatalf("stop %d %s", code, h.err.String())
	}
	if h.out.String() != "" || h.err.String() != "" {
		t.Fatalf("not quiet %q %q", h.out.String(), h.err.String())
	}
	h.keysMu.Lock()
	for _, k := range h.keys {
		switch k {
		case "DRAIN_SECONDS", "WEBHOOKS_RETENTION_DAYS", services.Variable, "LISTEN_PID", "LISTEN_FDS", "NOTIFY_SOCKET":
		default:
			t.Errorf("unexpected lookup %s", k)
		}
	}
	h.keysMu.Unlock()
	tes := h.tc.Events()
	if len(tes) < 2 {
		t.Fatalf("events %v", tes)
	}
	first, last := tes[0], tes[len(tes)-1]
	if first.Name != "service.started" || first.User != "" || first.RequestID != "" || !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"version": h.p.Version}) {
		t.Fatalf("first %+v", first)
	}
	if last.Name != "service.stopping" || last.User != "" || last.RequestID != "" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": "SIGINT"}) {
		t.Fatalf("last %+v", last)
	}
	counts := map[string]int{}
	for _, e := range tes {
		counts[e.Name]++
		if e.Service != pages.ServiceName || !e.Time.Equal(h.clock.Now()) {
			t.Errorf("writer clock/service %+v", e)
		}
	}
	if counts["service.started"] != 1 || counts["service.stopping"] != 1 {
		t.Fatalf("counts %v", counts)
	}
	var names []string
	for _, e := range h.ec.Events() {
		names = append(names, e.Name)
	}
	if !reflect.DeepEqual(names, []string{"webhook.tick.created", "webhook.tick.received"}) {
		t.Fatalf("bus %v", names)
	}
	for _, same := range []bool{true, false} {
		nextDir := dir
		if !same {
			nextDir = t.TempDir()
		}
		later := newRunHarness(t, nextDir)
		later.start()
		var listed struct{ Webhooks []struct{ Slug string } }
		runContent(t, later.call("list", nil), &listed)
		want := 0
		if same {
			want = 1
		}
		if len(listed.Webhooks) != want {
			t.Fatalf("persistence same=%v %+v", same, listed)
		}
		if code := later.stop("SIGTERM"); code != 0 {
			t.Fatal(code)
		}
	}
	info, e := os.Stat(filepath.Join(dir, "state", "webhooks.db"))
	if e != nil || !info.Mode().IsRegular() {
		t.Fatalf("database %v %v", info, e)
	}
}

// R-X8KZ-8N9P R-X9SV-MF0E
func TestRunInjectedRandomMintsIDAndOnlyCreatedIsEmitted(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	h.p.Rand = runBytes(0x5a)
	h.start()
	minted := h.create("tick")
	if minted.ID != "whk_5a5a5a5a5a5a5a5a" {
		t.Fatalf("id %q", minted.ID)
	}
	if h.stop("done") != cli.ExitSuccess {
		t.Fatal("stop")
	}
	bus := h.ec.Events()
	if len(bus) != 1 || bus[0].Name != "webhook.tick.created" {
		t.Fatalf("bus %+v", bus)
	}
	tes := h.tc.Events()
	if tes[0].Name != "service.started" || tes[len(tes)-1].Name != "service.stopping" {
		t.Fatalf("trail order %+v", tes)
	}
}

// seedDelivery makes a webhook and one delivery received at the given time, through the store.
func seedDelivery(t *testing.T, dir string, at time.Time) string {
	t.Helper()
	clock := func() time.Time { return at }
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "webhooks.db"), Migrations: webhooks.Migrations(), Now: clock})
	if e != nil {
		t.Fatal(e)
	}
	st := store.New(d, store.Config{Now: clock, Rand: &runCounter{n: 200}})
	w, _, e := st.Create(context.Background(), store.Draft{Slug: "seeded", Scheme: store.Bearer, OwnerID: "owner", OwnerEmail: "owner@example.com"})
	if e != nil {
		t.Fatal(e)
	}
	got, e := st.Receive(context.Background(), w.ID, store.Arrival{ContentType: "text/plain", Body: []byte("old")})
	if e != nil {
		t.Fatal(e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	return got.ID
}

func (h *runHarness) fetchable(id string) bool {
	h.t.Helper()
	_, refused := h.result("owner", "delivery", map[string]string{"id": id})
	return !refused
}

// R-Y6Q5-Y815
func TestRunSweepsBeforeReadiness(t *testing.T) {
	for _, tc := range []struct {
		name, now string
		kept      bool
		retention string
	}{
		{"within window", "2026-10-11T09:32:00Z", true, ""},
		{"past window", "2026-10-11T09:32:01Z", false, ""},
		{"past a set window", "2026-10-10T09:32:01Z", false, "1"},
		{"within a set window", "2026-10-12T09:32:00Z", true, "3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			id := seedDelivery(t, dir, runTime("2026-10-09T09:32:00Z"))
			h := newRunHarness(t, dir)
			if tc.retention != "" {
				h.setEnv("WEBHOOKS_RETENTION_DAYS", tc.retention)
			}
			h.clock.set(runTime(tc.now))
			h.start()
			if got := h.fetchable(id); got != tc.kept {
				t.Fatalf("fetchable=%v want %v", got, tc.kept)
			}
			if h.stop("done") != 0 {
				t.Fatal("stop")
			}
			first := h.tc.Events()[0]
			if first.Name != "service.started" || !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"version": h.p.Version}) {
				t.Fatalf("first %+v", first)
			}
		})
	}
}

func (h *runHarness) awaitTimer() {
	h.t.Helper()
	select {
	case d := <-h.clock.requested:
		if d != sweeper.Interval {
			h.t.Fatalf("timer for %s", d)
		}
	case <-time.After(5 * time.Second):
		h.t.Fatal("no sweep timer")
	}
}

// R-Y7Y2-BZRU
func TestRunSweepsOnInjectedTimer(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	h.start()
	h.awaitTimer()
	minted := h.create("tick")
	if code := h.post("tick", minted.Secret, []byte("kept for a while")); code != http.StatusAccepted {
		t.Fatal(code)
	}
	var created events.Event
	for _, e := range h.ec.Events() {
		if e.Name == "webhook.tick.received" {
			created = e
		}
	}
	id, _ := created.Attrs["delivery"].(string)
	if id == "" {
		// The bus event may still be in the emitter's queue; read it from the trail instead.
		for _, e := range h.tc.Events() {
			if e.Name == "webhook.tick.received" {
				id, _ = e.Attrs["delivery"].(string)
			}
		}
	}
	if id == "" {
		t.Fatal("no delivery id")
	}
	// The window has passed, but no sweep runs until the timer delivers.
	h.clock.set(runTime("2026-10-11T09:32:01Z"))
	if !h.fetchable(id) {
		t.Fatal("swept without the timer")
	}
	// At exactly the window's end a sweep keeps it.
	h.clock.set(runTime("2026-10-11T09:32:00Z"))
	h.clock.fire()
	h.awaitTimer()
	if !h.fetchable(id) {
		t.Fatal("swept at the window's end")
	}
	h.clock.set(runTime("2026-10-11T09:32:01Z"))
	h.clock.fire()
	h.awaitTimer()
	if h.fetchable(id) {
		t.Fatal("not swept past the window")
	}
	var shown struct {
		LastReceived string `json:"last_received"`
	}
	runContent(t, h.call("show", map[string]string{"slug": "tick"}), &shown)
	if shown.LastReceived != "2026-10-09T09:32:00Z" {
		t.Fatalf("sweep changed the webhook %q", shown.LastReceived)
	}
	if h.stop("done") != 0 || h.err.String() != "" {
		t.Fatalf("stop %q", h.err.String())
	}
}

// R-WSQA-9MMO
func TestRunEmptyDirNilUnsetenvAndNilAfter(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	h := newRunHarness(t, "")
	h.p.Unsetenv = nil
	h.p.After = nil
	h.start()
	h.create("in_cwd")
	if h.stop("done") != 0 {
		t.Fatal("stop failed")
	}
	info, e := os.Stat(filepath.Join(dir, "state", "webhooks.db"))
	if e != nil || !info.Mode().IsRegular() {
		t.Fatalf("empty Dir database %v %v", info, e)
	}
	if len(h.unsets) != 0 {
		t.Fatalf("nil Unsetenv removed %v", h.unsets)
	}
}

// R-WSQA-9MMO
func TestRunNilSinksDeliverToSocketSinks(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	short, e := os.MkdirTemp("", "webhooks-sinks-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := os.RemoveAll(short); e != nil {
			t.Error(e)
		}
	})
	var servers []*http.Server
	var group sync.WaitGroup
	var mu sync.Mutex
	var trailNames, busNames []string
	for _, name := range []string{"telemetry", "events"} {
		ln, err := net.Listen("unix", filepath.Join(short, name))
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			if name == "telemetry" {
				var ev struct {
					Event string `json:"event"`
				}
				if err := json.Unmarshal(body, &ev); err != nil {
					t.Error(err)
				}
				trailNames = append(trailNames, ev.Event)
			} else {
				var ev events.Event
				if err := json.Unmarshal(body, &ev); err != nil {
					t.Error(err)
				}
				busNames = append(busNames, ev.Name)
			}
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		})}
		servers = append(servers, srv)
		group.Go(func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error(err)
			}
		})
	}
	defer func() {
		for _, srv := range servers {
			if e := srv.Close(); e != nil {
				t.Error(e)
			}
		}
		group.Wait()
	}()
	path := filepath.Join(h.p.Dir, "services.json")
	data := fmt.Sprintf(`{"services":[{"name":"telemetry","url":"http://telemetry.test","description":"fixture","socket":%q,"enabled":true,"mcp":false},{"name":"events","url":"http://events.test","description":"fixture","socket":%q,"enabled":true,"mcp":false}]}`, filepath.Join(short, "telemetry"), filepath.Join(short, "events"))
	if e := os.WriteFile(path, []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv(services.Variable, path)
	h.p.Sink = nil
	h.p.EventSink = nil
	h.start()
	h.create("socket_sink")
	if h.stop("done") != 0 || h.err.String() != "" {
		t.Fatalf("default sinks %q", h.err.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(trailNames) < 2 || trailNames[0] != "service.started" || trailNames[len(trailNames)-1] != "service.stopping" || !reflect.DeepEqual(busNames, []string{"webhook.socket_sink.created"}) {
		t.Fatalf("default sink events trail=%v bus=%v", trailNames, busNames)
	}
}

// R-Y95Y-PRIJ
func TestRunDeliversHeldBusEventsWithinDrain(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	var stopping sync.Once
	released := make(chan struct{})
	attempted := make(chan struct{}, 1)
	h.p.EventSink = runEventSink(func(c context.Context, e events.Event) error {
		select {
		case <-released:
			return h.ec.Deliver(c, e)
		default:
		}
		select {
		case attempted <- struct{}{}:
		default:
		}
		return errors.New("bus offline")
	})
	h.p.Sleep = func(ctx context.Context, _ time.Duration) {
		select {
		case <-ctx.Done():
		case <-released:
		}
	}
	h.start()
	h.create("held")
	select {
	case <-attempted:
	case <-time.After(5 * time.Second):
		t.Fatal("no attempt")
	}
	stopping.Do(func() { close(released) })
	if code := h.stop("SIGTERM"); code != cli.ExitSuccess {
		t.Fatalf("stop %d %q", code, h.err.String())
	}
	bus := h.ec.Events()
	if len(bus) != 1 || bus[0].Name != "webhook.held.created" {
		t.Fatalf("held event not delivered %+v", bus)
	}
	tes := h.tc.Events()
	stops := 0
	for _, e := range tes {
		if e.Name == "service.stopping" {
			stops++
		}
	}
	if stops != 1 || tes[len(tes)-1].Name != "service.stopping" || !reflect.DeepEqual(tes[len(tes)-1].Attrs, telemetry.Attrs{"reason": "SIGTERM"}) {
		t.Fatalf("stopping %+v", tes[len(tes)-1])
	}
}

// R-YADV-3J98
func TestRunCutsOffRequestsAtDrainDeadline(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			h := newRunHarness(t, t.TempDir())
			h.setEnv("DRAIN_SECONDS", "1")
			entered := make(chan struct{}, count)
			release := make(chan struct{})
			banner := h.p.Banner
			h.p.Banner = func(u page.User) page.Banner { entered <- struct{}{}; <-release; return banner(u) }
			defer close(release)
			h.start()
			responses := make(chan error, count)
			for range count {
				go func() {
					req, err := http.NewRequestWithContext(t.Context(), "GET", h.url+"/about", nil)
					if err != nil {
						responses <- err
						return
					}
					req.Header.Set("X-User-Id", "owner")
					resp, err := (&http.Client{Timeout: 4 * time.Second}).Do(req)
					if err == nil {
						_, err = io.ReadAll(resp.Body)
						_ = resp.Body.Close()
						if err == nil {
							err = errors.New("cut-off request got complete response")
						}
					}
					responses <- err
				}()
			}
			for range count {
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("request did not begin")
				}
			}
			start := time.Now()
			h.cancel(errors.New("SIGINT"))
			select {
			case code := <-h.done:
				h.stopped = true
				if code != cli.ExitServerFailed {
					t.Fatalf("cutoff exit %d", code)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("drain did not end")
			}
			if elapsed := time.Since(start); elapsed < time.Second || elapsed >= 2*time.Second {
				t.Fatalf("drain duration %s", elapsed)
			}
			want := "webhooks: stopped with 1 request unfinished\n"
			if count == 2 {
				want = "webhooks: stopped with 2 requests unfinished\n"
			}
			lines := h.err.calls()
			if len(lines) == 0 || lines[len(lines)-1] != want {
				t.Fatalf("cutoff diagnostic %q", lines)
			}
			for range count {
				select {
				case err := <-responses:
					if err == nil {
						t.Fatal("cutoff had no error")
					}
				case <-time.After(2 * time.Second):
					t.Fatal("request connection not closed")
				}
			}
			if h.out.String() != "" {
				t.Fatal(h.out.String())
			}
		})
	}
}
