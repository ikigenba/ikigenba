package cli_test

import (
	"bytes"
	"context"
	"database/sql"
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
	cron "github.com/ikigenba/ikigenba/cron"
	"github.com/ikigenba/ikigenba/cron/internal/cli"
	"github.com/ikigenba/ikigenba/cron/internal/pages"
	"github.com/ikigenba/ikigenba/cron/internal/store"
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

type runBytes byte

func (r runBytes) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = byte(r)
	}
	return len(b), nil
}

type runClock struct {
	mu        sync.Mutex
	now       time.Time
	timers    []chan time.Time
	requested chan time.Duration
	driving   bool
}

func newRunClock() *runClock {
	return &runClock{now: runTime("2026-10-05T09:32:00Z"), requested: make(chan time.Duration, 100)}
}
func runTime(s string) time.Time {
	v, e := time.Parse(time.RFC3339, s)
	if e != nil {
		panic(e)
	}
	return v
}
func (c *runClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *runClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, ch)
	if c.driving {
		ch <- c.now
	}
	select {
	case c.requested <- d:
	default:
	}
	return ch
}
func (c *runClock) advance(v time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = v
	for _, ch := range c.timers {
		select {
		case ch <- v:
		default:
		}
	}
	c.timers = nil
}

type runHarness struct {
	t           *testing.T
	p           cli.Process
	clock       *runClock
	out, err    *runOutput
	tc          *telemetry.Capture
	ec          *events.Capture
	listener    net.Listener
	notify      *net.UnixConn
	ctx         context.Context
	cancel      context.CancelCauseFunc
	done        chan int
	url         string
	client      *mcp.Client
	unsets      []string
	keys        []string
	inherits    []uintptr
	stopped     bool
	mcpCalls    int
	writer      *telemetry.Writer
	writerReady chan *telemetry.Writer
}

func newRunHarness(t *testing.T, dir string) *runHarness {
	t.Helper()
	t.Setenv(services.Variable, "")
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	short, e := os.MkdirTemp("", "cron-ready-")
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
	h := &runHarness{t: t, clock: newRunClock(), out: &runOutput{}, err: &runOutput{}, tc: &telemetry.Capture{}, ec: &events.Capture{}, listener: ln, notify: nc, done: make(chan int, 1), writerReady: make(chan *telemetry.Writer, 1), url: "http://" + ln.Addr().String()}
	h.ctx, h.cancel = context.WithCancelCause(context.Background())
	env := map[string]string{"LISTEN_PID": "321", "LISTEN_FDS": "1", "NOTIFY_SOCKET": a}
	h.p = cli.Process{Pid: 321, Dir: dir, Version: "injected-code-identity", Stdout: h.out, Stderr: h.err, Now: h.clock.Now, After: h.clock.After, Rand: runBytes(0x5a), Sink: h.tc, EventSink: h.ec,
		LookupEnv: func(k string) (string, bool) { h.keys = append(h.keys, k); v, ok := env[k]; return v, ok }, Unsetenv: func(k string) error { h.unsets = append(h.unsets, k); return nil }, Inherit: func(fd uintptr) (net.Listener, error) { h.inherits = append(h.inherits, fd); return ln, nil }, Banner: page.New(pages.ServiceName, "injected-code-identity").Banner,
		MCP: func(w *telemetry.Writer) *mcp.Server {
			h.mcpCalls++
			h.writerReady <- w
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
	select {
	case h.writer = <-h.writerReady:
	case <-time.After(5 * time.Second):
		h.t.Fatal("READY arrived before MCP constructor")
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
	case <-time.After(5 * time.Second):
		h.t.Fatal("Run did not return")
		return -1
	}
}
func (h *runHarness) call(tool string, args any) map[string]json.RawMessage {
	h.t.Helper()
	b, e := json.Marshal(args)
	if args == nil {
		b = nil
	}
	if e != nil {
		h.t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, e := h.client.CallTool(ctx, identity.Caller{UserID: "owner", Email: "owner@example.com", RequestID: "run-call"}, tool, b)
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
	if r.IsError() {
		h.t.Fatalf("%s refused: %s", tool, raw)
	}
	return obj
}
func runContent(t *testing.T, obj map[string]json.RawMessage, out any) {
	t.Helper()
	if e := json.Unmarshal(obj["structuredContent"], out); e != nil {
		t.Fatal(e)
	}
}
func (h *runHarness) get(path string) (int, string) {
	h.t.Helper()
	req, e := http.NewRequestWithContext(h.t.Context(), "GET", h.url+path, nil)
	if e != nil {
		h.t.Fatal(e)
	}
	req.Header.Set("X-User-Id", "owner")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Host = "cron.example.com"
	c := &http.Client{Timeout: 5 * time.Second}
	r, e := c.Do(req)
	if e != nil {
		h.t.Fatal(e)
	}
	defer func() {
		if err := r.Body.Close(); err != nil {
			h.t.Error(err)
		}
	}()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		h.t.Fatal(e)
	}
	return r.StatusCode, string(b)
}

// R-84C3-727N R-IH6K-9KZQ R-IJMD-14H4 R-IKU9-EW7T R-IM25-SNYI
func TestRunStartupRefusalsTouchNothing(t *testing.T) {
	for _, tc := range []struct {
		name, drain, pid, fds, want string
		code                        int
	}{
		{"bad setting", "abc", "321", "1", "cron: DRAIN_SECONDS is 'abc', not a positive whole number of seconds\n", cli.ExitUsage},
		{"bad setting no socket", "abc", "", "", "cron: DRAIN_SECONDS is 'abc', not a positive whole number of seconds\n", cli.ExitUsage},
		{"unset sockets", "", "321", "", "cron: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
		{"wrong pid", "", "999", "1", "cron: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
		{"zero sockets", "", "321", "0", "cron: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
		{"signed sockets", "", "321", "+1", "cron: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
		{"spaces", "", "321", " 1", "cron: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
		{"multiple", "", "321", "002", "cron: 002 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
		{"huge", "", "321", strings.Repeat("9", 50), "cron: " + strings.Repeat("9", 50) + " sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n", cli.ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRunHarness(t, t.TempDir())
			touch := func() { t.Error("startup touched a forbidden seam") }
			h.p.LookupEnv = func(k string) (string, bool) {
				if k == "NOTIFY_SOCKET" || k == services.Variable {
					t.Errorf("early lookup %s", k)
				}
				switch k {
				case "DRAIN_SECONDS":
					return tc.drain, tc.drain != ""
				case "LISTEN_PID":
					return tc.pid, tc.pid != ""
				case "LISTEN_FDS":
					return tc.fds, tc.fds != ""
				}
				return "", false
			}
			h.p.Inherit = func(uintptr) (net.Listener, error) {
				touch()
				return runListener{Listener: h.listener, accept: func() error { touch(); return errors.New("unexpected accept") }}, nil
			}
			h.p.Unsetenv = func(string) error { touch(); return nil }
			h.p.Now = func() time.Time { touch(); return h.clock.Now() }
			h.p.After = func(time.Duration) <-chan time.Time { touch(); return nil }
			h.p.Sleep = func(context.Context, time.Duration) { touch() }
			h.p.Rand = runForbiddenReader{touch}
			h.p.Banner = func(page.User) page.Banner { touch(); return page.Banner{} }
			h.p.MCP = func(*telemetry.Writer) *mcp.Server { touch(); return nil }
			h.p.Sink = runTelemetrySink(func(context.Context, telemetry.Event) error { touch(); return nil })
			h.p.EventSink = runEventSink(func(context.Context, events.Event) error { touch(); return nil })
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			code := cli.Run(ctx, h.p)
			h.stopped = true
			if code != tc.code || h.out.String() != "" || !reflect.DeepEqual(h.err.calls(), []string{tc.want}) {
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

type runForbiddenReader struct{ touch func() }

func (r runForbiddenReader) Read([]byte) (int, error) { r.touch(); return 0, errors.New("unreachable") }

type runTelemetrySink func(context.Context, telemetry.Event) error

func (s runTelemetrySink) Deliver(c context.Context, e telemetry.Event) error { return s(c, e) }

type runEventSink func(context.Context, events.Event) error

func (s runEventSink) Deliver(c context.Context, e events.Event) error { return s(c, e) }

// R-INA2-6FP7 R-IOHY-K7FW R-IPPU-XZ6L R-IULG-H25D
func TestRunInheritFailureAndCancelledStart(t *testing.T) {
	for _, done := range []bool{false, true} {
		t.Run(fmt.Sprint(done), func(t *testing.T) {
			h := newRunHarness(t, t.TempDir())
			if done {
				h.cancel(errors.New("already cancelled"))
			}
			h.p.Inherit = func(fd uintptr) (net.Listener, error) {
				h.inherits = append(h.inherits, fd)
				return nil, errors.New("descriptor is not a socket")
			}
			forbidRunServices(h)
			code := cli.Run(h.ctx, h.p)
			h.stopped = true
			assertNoRunNotification(h)
			if code != cli.ExitServerFailed || !reflect.DeepEqual(h.err.calls(), []string{"cron: descriptor is not a socket\n"}) || h.out.String() != "" {
				t.Fatalf("failure %d %q", code, h.err.calls())
			}
			sort.Strings(h.unsets)
			if !reflect.DeepEqual(h.unsets, []string{"LISTEN_FDNAMES", "LISTEN_FDS", "LISTEN_PID"}) || !reflect.DeepEqual(h.inherits, []uintptr{3}) {
				t.Fatalf("activation unsets=%v inherits=%v", h.unsets, h.inherits)
			}
			entries, e := os.ReadDir(h.p.Dir)
			if e != nil || len(entries) != 0 {
				t.Fatalf("Dir touched %v %v", entries, e)
			}
			if len(h.tc.Events())+len(h.ec.Events()) != 0 || h.mcpCalls != 0 {
				t.Fatal("failed inherit started services")
			}
		})
	}
	t.Run("cancelled with socket", func(t *testing.T) {
		h := newRunHarness(t, t.TempDir())
		h.cancel(errors.New("before startup"))
		code := cli.Run(h.ctx, h.p)
		h.stopped = true
		if code != cli.ExitSuccess || h.out.String() != "" || h.err.String() != "" || len(h.tc.Events())+len(h.ec.Events()) != 0 {
			t.Fatalf("cancelled startup %d %q %q", code, h.out.String(), h.err.String())
		}
		if e := h.notify.SetReadDeadline(time.Now()); e != nil {
			t.Fatal(e)
		}
		b := make([]byte, 32)
		if _, _, e := h.notify.ReadFromUnix(b); e == nil {
			t.Fatal("cancelled startup notified")
		}
	})
}

// R-IQXR-BQXA
func TestRunDatabaseFailureBeforeReadiness(t *testing.T) {
	for _, bad := range []string{"state", "state/cron.db"} {
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
			_, expected := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "cron.db"), Migrations: cron.Migrations(), Now: h.clock.Now})
			if expected == nil {
				t.Fatal("fixture unexpectedly opened")
			}
			h.p.After = func(time.Duration) <-chan time.Time { t.Error("timer before database"); return nil }
			h.p.Banner = func(page.User) page.Banner { t.Error("banner before database"); return page.Banner{} }
			code := cli.Run(h.ctx, h.p)
			h.stopped = true
			want := "cron: cannot open database state/cron.db: " + strings.ReplaceAll(expected.Error(), "\n", " ") + "\n"
			if code != cli.ExitServerFailed || h.out.String() != "" || !reflect.DeepEqual(h.err.calls(), []string{want}) || h.mcpCalls != 0 || len(h.tc.Events())+len(h.ec.Events()) != 0 {
				t.Fatalf("db failure %d %q", code, h.err.calls())
			}
			b, e := fs.ReadFile(os.DirFS(h.p.Dir), bad)
			if e != nil || !bytes.Equal(b, original) {
				t.Fatalf("fixture changed %q %v", b, e)
			}
			if e := h.notify.SetReadDeadline(time.Now()); e != nil {
				t.Fatal(e)
			}
			if _, _, e := h.notify.ReadFromUnix(make([]byte, 32)); e == nil {
				t.Fatal("db failure notified")
			}
		})
	}
}

// R-86RV-YLP1 R-87ZS-CDFQ R-897O-Q56F R-8AFL-3WX4 R-8BNH-HONT R-8CVD-VGEI R-IZH2-0545 R-J1WU-ROLJ R-J34R-5GC8 R-J4CN-J82X R-JAG5-G2SE R-JBO1-TUJ3 R-JMN5-9S7C
func TestRunPersistenceInjectedSourcesAndLifecycle(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "work")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	outside := filepath.Join(parent, "keep")
	if e := os.WriteFile(outside, []byte("unchanged"), 0600); e != nil {
		t.Fatal(e)
	}
	h := newRunHarness(t, dir)
	h.start()
	var created struct{ ID, Created string }
	runContent(t, h.call("create", map[string]string{"slug": "crm_sync", "when": "*/15 * * * *"}), &created)
	if created.ID != "crn_5a5a5a5a5a5a5a5a" || created.Created != "2026-10-05T09:32:00Z" {
		t.Fatalf("injected create %+v", created)
	}
	var listed struct{ Triggers []struct{ Slug string } }
	runContent(t, h.call("list", nil), &listed)
	if len(listed.Triggers) != 1 || listed.Triggers[0].Slug != "crm_sync" {
		t.Fatalf("list %+v", listed)
	}
	if code, body := h.get("/about"); code != 200 || !strings.Contains(body, "injected-code-identity") {
		t.Fatalf("about %d %s", code, body)
	}
	if code, body := h.get("/"); code != 200 || !strings.Contains(body, "crm_sync") {
		t.Fatalf("page %d %s", code, body)
	}
	select {
	case code := <-h.done:
		h.stopped = true
		t.Fatalf("Run returned alive %d", code)
	default:
	}
	start := time.Now()
	if code := h.stop("SIGINT"); code != cli.ExitSuccess {
		t.Fatalf("stop %d %s", code, h.err.String())
	}
	if time.Since(start) > time.Second {
		t.Fatal("idle stop waited for drain")
	}
	if h.out.String() != "" || h.err.String() != "" {
		t.Fatalf("not quiet %q %q", h.out.String(), h.err.String())
	}
	if h.mcpCalls != 1 || h.writer == nil || !reflect.DeepEqual(h.inherits, []uintptr{3}) {
		t.Fatalf("wiring MCP %d inherit %v", h.mcpCalls, h.inherits)
	}
	for _, k := range h.keys {
		if k != "DRAIN_SECONDS" && k != services.Variable && k != "LISTEN_PID" && k != "LISTEN_FDS" && k != "NOTIFY_SOCKET" {
			t.Errorf("unexpected lookup %s", k)
		}
	}
	tes := h.tc.Events()
	if len(tes) < 2 {
		t.Fatalf("events %v", tes)
	}
	first, last := tes[0], tes[len(tes)-1]
	if first.Name != "service.started" || first.User != "" || first.RequestID != "" || !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"version": h.p.Version}) || last.Name != "service.stopping" || last.User != "" || last.RequestID != "" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": "SIGINT"}) {
		t.Fatalf("lifecycle first=%+v last=%+v", first, last)
	}
	counts := map[string]int{}
	for _, e := range tes {
		counts[e.Name]++
		if e.Service != pages.ServiceName || !e.Time.Equal(h.clock.Now().UTC().Truncate(time.Microsecond)) {
			t.Errorf("writer clock/service %+v", e)
		}
	}
	if counts["service.started"] != 1 || counts["service.stopping"] != 1 {
		t.Fatalf("counts %v", counts)
	}
	bes := h.ec.Events()
	if len(bes) != 1 || bes[0].Name != "cron.crm_sync.created" || bes[0].ID != "evt_5a5a5a5a5a5a5a5a" || bes[0].Service != pages.ServiceName || !bes[0].Time.Equal(h.clock.Now()) {
		t.Fatalf("bus %+v", bes)
	}
	entries, e := os.ReadDir(parent)
	if e != nil || len(entries) != 2 {
		t.Fatalf("outside writes %v %v", entries, e)
	}
	b, e := fs.ReadFile(os.DirFS(parent), "keep")
	if e != nil || string(b) != "unchanged" {
		t.Fatalf("outside changed %q %v", b, e)
	}
	for _, same := range []bool{true, false} {
		nextDir := dir
		if !same {
			nextDir = t.TempDir()
		}
		later := newRunHarness(t, nextDir)
		later.start()
		runContent(t, later.call("list", nil), &listed)
		want := 0
		if same {
			want = 1
		}
		if len(listed.Triggers) != want {
			t.Fatalf("persistence same=%v %+v", same, listed)
		}
		if code := later.stop("SIGTERM"); code != 0 {
			t.Fatal(code)
		}
	}
}

// R-ITDK-3AEO
func TestRunCreatesDatabaseBeforeReady(t *testing.T) {
	for _, emptyState := range []bool{false, true} {
		t.Run(fmt.Sprint(emptyState), func(t *testing.T) {
			h := newRunHarness(t, t.TempDir())
			if emptyState {
				if e := os.Mkdir(filepath.Join(h.p.Dir, "state"), 0700); e != nil {
					t.Fatal(e)
				}
			}
			h.start()
			info, e := os.Stat(filepath.Join(h.p.Dir, "state", "cron.db"))
			if e != nil || !info.Mode().IsRegular() {
				t.Fatalf("database %v %v", info, e)
			}
			var actual bytes.Buffer
			if e := db.Status(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "cron.db"), Migrations: cron.Migrations()}, &actual); e != nil {
				t.Fatal(e)
			}
			reference := filepath.Join(t.TempDir(), "state", "cron.db")
			d, e := db.Open(context.Background(), db.Config{Path: reference, Migrations: cron.Migrations(), Now: h.clock.Now})
			if e != nil {
				t.Fatal(e)
			}
			if e = d.Close(); e != nil {
				t.Fatal(e)
			}
			var expected bytes.Buffer
			if e = db.Status(context.Background(), db.Config{Path: reference, Migrations: cron.Migrations()}, &expected); e != nil {
				t.Fatal(e)
			}
			if actual.String() != expected.String() {
				t.Fatalf("schema status %q want %q", actual.String(), expected.String())
			}
			var listed struct{ Triggers []any }
			runContent(t, h.call("list", nil), &listed)
			if listed.Triggers == nil || len(listed.Triggers) != 0 {
				t.Fatalf("empty list %+v", listed)
			}
			if h.stop("done") != 0 {
				t.Fatal("stop failed")
			}
		})
	}
}

// R-IS5N-PINZ
func TestRunAheadDatabaseWarningPreservesStatus(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	path := filepath.Join(h.p.Dir, "state", "cron.db")
	d, e := db.Open(context.Background(), db.Config{Path: path, Migrations: cron.Migrations(), Now: h.clock.Now})
	if e != nil {
		t.Fatal(e)
	}
	st := store.New(d, store.Config{Now: h.clock.Now, Rand: runBytes(0x5a)})
	if _, e = st.Create(context.Background(), store.Draft{Slug: "existing", When: "@daily", OwnerID: "owner", OwnerEmail: "owner@example.com"}); e != nil {
		t.Fatal(e)
	}
	if e = d.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES(2, '2026-10-05T09:32:00Z')")
		return err
	}); e != nil {
		t.Fatal(e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	var before, warning bytes.Buffer
	cfg := db.Config{Path: path, Migrations: cron.Migrations(), Now: h.clock.Now, Service: pages.ServiceName, Stderr: &warning}
	if e = db.Status(context.Background(), cfg, &before); e != nil {
		t.Fatal(e)
	}
	d, e = db.Open(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	h.start()
	if !reflect.DeepEqual(h.err.calls(), []string{warning.String()}) {
		t.Fatalf("warning before READY %q want %q", h.err.calls(), warning.String())
	}
	var listed struct{ Triggers []struct{ Slug string } }
	runContent(t, h.call("list", nil), &listed)
	if len(listed.Triggers) != 1 || listed.Triggers[0].Slug != "existing" {
		t.Fatalf("existing %+v", listed)
	}
	if h.stop("done") != 0 {
		t.Fatal("stop failed")
	}
	var after bytes.Buffer
	if e = db.Status(context.Background(), cfg, &after); e != nil {
		t.Fatal(e)
	}
	if after.String() != before.String() || !reflect.DeepEqual(h.err.calls(), []string{warning.String()}) {
		t.Fatalf("ahead status %q %q diagnostics %q", before.String(), after.String(), h.err.calls())
	}
	started := 0
	for _, ev := range h.tc.Events() {
		if ev.Name == "service.started" {
			started++
		}
		if ev.Name != "service.started" && ev.Name != "service.stopping" && ev.Name != "request.started" && ev.Name != "request.finished" && ev.Name != "tool.called" {
			t.Fatalf("warning emitted %+v", ev)
		}
	}
	if started != 1 {
		t.Fatalf("started count %d", started)
	}
}

// R-J5KJ-WZTM
func TestRunServicesFileDoesNotRefuseStart(t *testing.T) {
	for _, kind := range []string{"absent", "missing", "invalid", "directory"} {
		t.Run(kind, func(t *testing.T) {
			h := newRunHarness(t, t.TempDir())
			servicesPath := ""
			if kind != "absent" {
				servicesPath = filepath.Join(t.TempDir(), "services")
				if kind == "invalid" {
					if e := os.WriteFile(servicesPath, []byte("this is not TOML ["), 0600); e != nil {
						t.Fatal(e)
					}
				}
				if kind == "directory" {
					if e := os.Mkdir(servicesPath, 0700); e != nil {
						t.Fatal(e)
					}
				}
			}
			lookup := h.p.LookupEnv
			h.p.LookupEnv = func(k string) (string, bool) {
				if k == services.Variable {
					return servicesPath, kind != "absent"
				}
				return lookup(k)
			}
			h.start()
			if code, _ := h.get("/"); code != 200 {
				t.Fatal(code)
			}
			if h.stop("done") != 0 || h.err.String() != "" {
				t.Fatalf("services %s %q", kind, h.err.String())
			}
		})
	}
}

// R-IVTC-UTW2
func TestRunLoadsActiveAndPausedSlotsWithoutFiring(t *testing.T) {
	dir := t.TempDir()
	clock := newRunClock()
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "cron.db"), Migrations: cron.Migrations(), Now: clock.Now})
	if e != nil {
		t.Fatal(e)
	}
	data := make([]byte, 128)
	for i := range data {
		data[i] = byte(i)
	}
	st := store.New(d, store.Config{Now: clock.Now, Rand: bytes.NewReader(data)})
	for _, a := range []struct{ slug, when, last, status string }{{"hourly", "@hourly", "2026-10-05T09:00:00Z", store.Active}, {"month_end", "@monthly", "", store.Active}, {"nightly_backup", "30 2 * * *", "2026-10-05T02:30:00Z", store.Active}, {"weekly_digest", "0 8 * * 1", "2026-09-28T08:00:00Z", store.Paused}} {
		v, err := st.Create(context.Background(), store.Draft{Slug: a.slug, When: a.when, OwnerID: "owner", OwnerEmail: "owner@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if a.last != "" {
			if _, err = st.SetLastFired(context.Background(), v.ID, runTime(a.last)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = st.SetStatus(context.Background(), v.ID, a.status); err != nil {
			t.Fatal(err)
		}
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	for _, at := range []string{"2026-10-05T09:32:00Z", "2026-10-05T12:10:00Z"} {
		h := newRunHarness(t, dir)
		h.clock.advance(runTime(at))
		h.start()
		var raw struct{ Triggers []map[string]json.RawMessage }
		runContent(t, h.call("list", nil), &raw)
		expected := map[string]string{"hourly": "2026-10-05T10:00:00Z", "month_end": "2026-11-01T00:00:00Z", "nightly_backup": "2026-10-06T02:30:00Z", "weekly_digest": ""}
		if at == "2026-10-05T12:10:00Z" {
			expected["hourly"] = "2026-10-05T13:00:00Z"
		}
		if len(raw.Triggers) != 4 {
			t.Fatalf("loaded %+v", raw)
		}
		for _, r := range raw.Triggers {
			var slug, next, last string
			if e = json.Unmarshal(r["slug"], &slug); e != nil {
				t.Fatal(e)
			}
			if len(r["next"]) != 0 && string(r["next"]) != "null" {
				if e = json.Unmarshal(r["next"], &next); e != nil {
					t.Fatal(e)
				}
			}
			if next != expected[slug] {
				t.Fatalf("%s next=%q expected %q", slug, next, expected[slug])
			}
			if slug == "hourly" {
				if e = json.Unmarshal(r["last_fired"], &last); e != nil {
					t.Fatal(e)
				}
				if last != "2026-10-05T09:00:00Z" {
					t.Fatal(last)
				}
			}
		}
		if h.stop("done") != 0 {
			t.Fatal("stop failed")
		}
		if len(h.ec.Events()) != 0 {
			t.Fatal("start emitted bus event")
		}
		for _, ev := range h.tc.Events() {
			if strings.HasPrefix(ev.Name, "cron.") {
				t.Fatalf("start fired %+v", ev)
			}
		}
	}
}

// R-C5NU-6F4R R-K7DF-RVT5
func TestRunFiresOnInjectedTimer(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	fired := make(chan events.Event, 1)
	h.p.EventSink = runEventSink(func(c context.Context, e events.Event) error {
		if e.Name == "cron.crm_sync.fired" {
			fired <- e
		}
		return h.ec.Deliver(c, e)
	})
	h.start()
	h.call("create", map[string]string{"slug": "crm_sync", "when": "*/15 * * * *"})
	select {
	case <-h.clock.requested:
	case <-time.After(5 * time.Second):
		t.Fatal("no schedule timer")
	}
	h.clock.mu.Lock()
	h.clock.driving = true
	h.clock.mu.Unlock()
	h.clock.advance(runTime("2026-10-05T09:45:00Z"))
	select {
	case ev := <-fired:
		h.clock.mu.Lock()
		h.clock.driving = false
		h.clock.mu.Unlock()
		if ev.Attrs["scheduled"] != "2026-10-05T09:45:00Z" {
			t.Fatalf("fire %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("injected timer did not fire")
	}
	if h.stop("done") != 0 || h.err.String() != "" {
		t.Fatalf("fire stop %q", h.err.String())
	}
	allowed := map[string]bool{"service.started": true, "service.stopping": true, "request.started": true, "request.finished": true, "tool.called": true, "cron.crm_sync.created": true, "cron.crm_sync.fired": true}
	for _, ev := range h.tc.Events() {
		if !allowed[ev.Name] {
			t.Fatalf("unexpected runtime event %+v", ev)
		}
	}
}

// R-JQAU-F3FF
func TestRunDeclarationsWithAndWithoutIdentity(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	h.start()
	h.call("create", map[string]string{"slug": "existing", "when": "@daily"})
	before := h.call("list", nil)["structuredContent"]
	want := `{"emits":[{"event":"cron.*.created","attrs":["trigger","when"]},{"event":"cron.*.deleted","attrs":["trigger","when"]},{"event":"cron.*.fired","attrs":["trigger","when","scheduled"]},{"event":"cron.*.paused","attrs":["trigger","when"]},{"event":"cron.*.resumed","attrs":["trigger","when"]}],"accepts":[]}`
	for _, user := range []string{"", "owner"} {
		req, e := http.NewRequestWithContext(t.Context(), "GET", h.url+"/declarations", nil)
		if e != nil {
			t.Fatal(e)
		}
		if user != "" {
			req.Header.Set("X-User-Id", user)
		}
		resp, e := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		if e = resp.Body.Close(); e != nil {
			t.Fatal(e)
		}
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/json" || string(b) != want {
			t.Fatalf("declarations %d %q %s", resp.StatusCode, resp.Header.Get("Content-Type"), b)
		}
	}
	var listed struct{ Triggers []any }
	runContent(t, h.call("list", nil), &listed)
	after := h.call("list", nil)["structuredContent"]
	if !bytes.Equal(before, after) {
		t.Fatal("declarations mutated store")
	}
	if h.stop("done") != 0 || len(h.ec.Events()) != 1 {
		t.Fatal("declarations emitted bus event")
	}
}

func forbidRunServices(h *runHarness) {
	h.t.Helper()
	touch := func() { h.t.Error("failed inheritance touched service seam") }
	h.p.After = func(time.Duration) <-chan time.Time { touch(); return nil }
	h.p.Sleep = func(context.Context, time.Duration) { touch() }
	h.p.Rand = runForbiddenReader{touch}
	h.p.Banner = func(page.User) page.Banner { touch(); return page.Banner{} }
	h.p.MCP = func(*telemetry.Writer) *mcp.Server { touch(); return nil }
	h.p.Sink = runTelemetrySink(func(context.Context, telemetry.Event) error { touch(); return nil })
	h.p.EventSink = runEventSink(func(context.Context, events.Event) error { touch(); return nil })
}
func assertNoRunNotification(h *runHarness) {
	h.t.Helper()
	if e := h.notify.SetReadDeadline(time.Now()); e != nil {
		h.t.Fatal(e)
	}
	if _, _, e := h.notify.ReadFromUnix(make([]byte, 64)); e == nil {
		h.t.Fatal("unexpected readiness notification")
	}
}
