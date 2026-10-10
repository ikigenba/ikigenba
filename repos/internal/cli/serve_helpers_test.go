package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	"github.com/ikigenba/ikigenba/repos/internal/web"
)

type serveOutput struct {
	t        *testing.T
	mu       sync.Mutex
	b        bytes.Buffer
	writes   []string
	busy     atomic.Bool
	overlap  atomic.Bool
	returned atomic.Bool
	late     atomic.Bool
}

func (o *serveOutput) Write(p []byte) (int, error) {
	if !o.busy.CompareAndSwap(false, true) {
		o.overlap.Store(true)
	}
	defer o.busy.Store(false)
	if prefix := []byte("repos: undelivered event: "); bytes.HasPrefix(p, prefix) {
		var event struct {
			Name string `json:"event"`
		}
		if err := json.Unmarshal(bytes.TrimPrefix(p, prefix), &event); err != nil {
			o.t.Errorf("invalid fallback event: %v", err)
		} else {
			assertServeEventName(o.t, event.Name)
		}
	}
	if o.returned.Load() {
		o.late.Store(true)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.writes = append(o.writes, string(p))
	return o.b.Write(p)
}
func (o *serveOutput) text() string { o.mu.Lock(); defer o.mu.Unlock(); return o.b.String() }
func (o *serveOutput) lines() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.writes...)
}

// R-E436-7IWS: every captured or undelivered event in the serve flows uses D11's vocabulary.
func assertServeEventName(t *testing.T, name string) {
	t.Helper()
	switch name {
	case "request.started", "request.finished", "tool.called",
		"repo.created", "repo.renamed", "repo.deleted", "repo.pushed", "repo.fetched",
		"operation.waited", "operation.rejected", "operation.timed_out",
		"maintenance.finished", "repo.unavailable", "service.started", "service.stopping", "event.lost":
	default:
		t.Errorf("unexpected event name %q", name)
	}
}

type serveCapture struct {
	telemetry.Capture
	t *testing.T
}

func (s *serveCapture) Deliver(ctx context.Context, event telemetry.Event) error {
	assertServeEventName(s.t, event.Name)
	return s.Capture.Deliver(ctx, event)
}

type checkedServeSink struct {
	t       *testing.T
	deliver func(context.Context, telemetry.Event) error
}

func serveSink(t *testing.T, deliver func(context.Context, telemetry.Event) error) telemetry.Sink {
	return checkedServeSink{t: t, deliver: deliver}
}

func (s checkedServeSink) Deliver(ctx context.Context, event telemetry.Event) error {
	assertServeEventName(s.t, event.Name)
	return s.deliver(ctx, event)
}

type serveRandom struct {
	busy    atomic.Bool
	overlap atomic.Bool
	n       byte
}

func (r *serveRandom) Read(p []byte) (int, error) {
	if !r.busy.CompareAndSwap(false, true) {
		r.overlap.Store(true)
	}
	defer r.busy.Store(false)
	r.n++
	for i := range p {
		p[i] = r.n
	}
	return len(p), nil
}

type serveTimer struct {
	duration time.Duration
	ch       chan time.Time
}

func assertServeScheduleStopped(t *testing.T, f *serveFixture, interval serveTimer) {
	t.Helper()
	// Fire the saved fake interval after Run returns. A live scheduler rearms
	// even while Limits is draining, so lack of repository work cannot mask it.
	interval.ch <- f.p.Now()
	observation, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	select {
	case timer := <-f.timers:
		t.Fatalf("maintenance scheduled after Run: %s", timer.duration)
	case <-observation.Done():
		if observation.Err() != context.DeadlineExceeded {
			t.Fatalf("schedule observation interrupted: %v", observation.Err())
		}
	}
}

type serveFixture struct {
	p              cli.Process
	env            map[string]string
	envMu          sync.Mutex
	dir, gitPath   string
	gitEnv         []string
	stdout, stderr *serveOutput
	capture        *serveCapture
	random         *serveRandom
	timers         chan serveTimer
	notify         *net.UnixConn
	listener       net.Listener
	cancel         context.CancelCauseFunc
	result         chan int
	stopped        bool
	mcpCalls       atomic.Int64
	bannerCalls    atomic.Int64
	envCalls       atomic.Int64
	writer         atomic.Pointer[telemetry.Writer]
}

func newServeFixture(t *testing.T, knownGit ...string) *serveFixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &serveFixture{dir: t.TempDir(), stdout: &serveOutput{t: t}, stderr: &serveOutput{t: t}, capture: &serveCapture{t: t}, random: &serveRandom{}, timers: make(chan serveTimer, 64), env: map[string]string{"LISTEN_PID": "71", "LISTEN_FDS": "1", "DRAIN_SECONDS": "1"}}
	var err error
	if len(knownGit) > 0 {
		f.gitPath = knownGit[0]
	} else {
		f.gitPath, err = exec.LookPath("git")
		if err != nil {
			t.Fatal(err)
		}
	}
	f.env["PATH"] = filepath.Dir(f.gitPath)
	f.gitEnv = []string{"PATH=" + filepath.Dir(f.gitPath), "HOME=" + f.dir, "XDG_CONFIG_HOME=" + f.dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.test", "GIT_AUTHOR_DATE=2001-02-03T04:05:06Z", "GIT_COMMITTER_DATE=2001-02-03T04:05:06Z"}
	f.p = cli.Process{Pid: 71, Version: "fixture-display", Dir: f.dir, Stdout: f.stdout, Stderr: f.stderr, Sink: f.capture, EventSink: &events.Capture{}, Rand: f.random,
		LookupEnv: func(key string) (string, bool) {
			f.envMu.Lock()
			defer f.envMu.Unlock()
			v, ok := f.env[key]
			return v, ok
		},
		Environ:  func() []string { f.envCalls.Add(1); return append([]string(nil), f.gitEnv...) },
		Unsetenv: func(string) error { return nil }, Inherit: func(uintptr) (net.Listener, error) { panic("listener not supplied") },
		Now:   func() time.Time { return time.Date(2001, 2, 3, 4, 5, 6, 123456789, time.FixedZone("fixture", 3600)) },
		Sleep: func(context.Context, time.Duration) {},
		After: func(d time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			f.timers <- serveTimer{d, ch}
			return ch
		},
		Banner: func(u page.User) page.Banner {
			f.bannerCalls.Add(1)
			return page.Banner{Service: web.ServiceName, Release: f.p.Version, Commit: "fixture-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
		},
		MCP: func(w *telemetry.Writer) *mcp.Server {
			f.mcpCalls.Add(1)
			f.writer.Store(w)
			return mcp.NewServer(mcp.ServerConfig{Name: web.ServiceName, Version: f.p.Version, Telemetry: w, Instructions: func(context.Context) string { return "" }})
		},
	}
	return f
}
func (f *serveFixture) set(key, value string) {
	f.envMu.Lock()
	defer f.envMu.Unlock()
	f.env[key] = value
}
func (f *serveFixture) notification(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("", "scratch.")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	name := filepath.Join(dir, "notify.sock")
	f.notify, err = net.ListenUnixgram("unixgram", &net.UnixAddr{Name: name, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.notify.Close() })
	f.set("NOTIFY_SOCKET", name)
}
func (f *serveFixture) listen(t *testing.T) {
	t.Helper()
	var err error
	f.listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.p.Inherit = func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			t.Errorf("inherited descriptor %d", fd)
		}
		return f.listener, nil
	}
	t.Cleanup(func() { _ = f.listener.Close() })
}
func (f *serveFixture) launch(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(t.Context())
	f.cancel = cancel
	f.result = make(chan int, 1)
	go func() {
		code := cli.Run(ctx, f.p)
		f.stderr.returned.Store(true)
		f.stdout.returned.Store(true)
		f.result <- code
	}()
	t.Cleanup(func() {
		if !f.stopped {
			f.stop(t, "test cleanup", cli.ExitSuccess)
		}
	})
}
func (f *serveFixture) ready(t *testing.T) {
	t.Helper()
	if err := f.notify.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 64)
	n, _, err := f.notify.ReadFromUnix(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(b[:n]) != "READY=1" {
		t.Fatalf("notification %q", b[:n])
	}
}
func (f *serveFixture) start(t *testing.T) {
	t.Helper()
	f.notification(t)
	f.listen(t)
	f.launch(t)
	f.ready(t)
}
func (f *serveFixture) stop(t *testing.T, reason string, want int) {
	t.Helper()
	if f.stopped {
		return
	}
	f.cancel(errors.New(reason))
	select {
	case code := <-f.result:
		f.stopped = true
		if code != want {
			t.Errorf("exit %d want %d: %s", code, want, f.stderr.text())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}
func (f *serveFixture) client() *http.Client { return &http.Client{Timeout: 5 * time.Second} }
func (f *serveFixture) request(t *testing.T, path, id string) (int, http.Header, []byte) {
	t.Helper()
	r, err := http.NewRequestWithContext(t.Context(), "GET", "http://"+f.listener.Addr().String()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-User-Id", "owner")
	if id != "" {
		r.Header.Set("X-Request-Id", id)
	}
	resp, err := f.client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, b
}
func (f *serveFixture) tool(t *testing.T, name, args string) map[string]any {
	t.Helper()
	c := mcp.NewClient(mcp.ClientConfig{Endpoint: "http://" + f.listener.Addr().String() + "/mcp", HTTPClient: f.client()})
	result, err := c.CallTool(t.Context(), identity.Caller{UserID: "owner", RequestID: "tool-" + name}, name, json.RawMessage(args))
	if err != nil || result.IsError() {
		b, _ := result.MarshalJSON()
		t.Fatalf("%s result %s: %v", name, b, err)
	}
	b, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var e struct {
		Structured map[string]any `json:"structuredContent"`
	}
	if err = json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	return e.Structured
}
func (f *serveFixture) flush(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := f.writer.Load().Flush(ctx); err != nil {
		t.Fatal(err)
	}
}

func noServeNotification(t *testing.T, conn *net.UnixConn) {
	t.Helper()
	raw, err := conn.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var readErr error
	if err = raw.Control(func(fd uintptr) { _, _, readErr = syscall.Recvfrom(int(fd), make([]byte, 64), syscall.MSG_DONTWAIT) }); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(readErr, syscall.EAGAIN) && !errors.Is(readErr, syscall.EWOULDBLOCK) {
		t.Fatalf("unexpected datagram/read error: %v", readErr)
	}
}

type serveListener struct {
	net.Listener
	accept func() (net.Conn, error)
}

func (l *serveListener) Accept() (net.Conn, error) { return l.accept() }
