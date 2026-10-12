package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	app "github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

type outputBuffer struct {
	mu     sync.Mutex
	b      bytes.Buffer
	writes int
}

func (b *outputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writes++
	return b.b.Write(p)
}
func (b *outputBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

type runRandom struct{ n byte }

func (r *runRandom) Read(p []byte) (int, error) {
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

type fixedRandom byte

func (r fixedRandom) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureNow() time.Time                                               { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) }
func jsonResponse(r *http.Request, v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(b)), Request: r}
}

type fakeSource struct {
	mu       sync.Mutex
	requests []*http.Request
	page     func(*http.Request) (*http.Response, error)
}

func (f *fakeSource) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Clone(context.Background()))
	f.mu.Unlock()
	if r.URL.Query().Get("$select") != "" {
		return jsonResponse(r, []any{map[string]string{"count": "1"}}), nil
	}
	if f.page != nil {
		return f.page(r)
	}
	return jsonResponse(r, []any{map[string]string{"dot_number": r.URL.Query().Get("$where"), "legal_name": "Fixture carrier", "phy_state": "MN"}}), nil
}
func (f *fakeSource) Requests() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*http.Request{}, f.requests...)
}

type runOptions struct {
	WithoutNotify          bool
	CheckReadyBeforeAccept bool
	Dir                    string
	Env                    map[string]string
	Rand                   io.Reader
	After                  func(time.Duration) <-chan time.Time
	Source                 *fakeSource
	Sink                   telemetry.Sink
	EventSink              events.Sink
	DefaultSinks           bool
	Modify                 func(*cli.Process)
}
type runFixture struct {
	Notify     *net.UnixConn
	t          *testing.T
	Dir, Base  string
	Process    cli.Process
	Cancel     context.CancelCauseFunc
	Done       chan int
	Client     *mcp.Client
	HTTP       *http.Client
	Telemetry  *telemetry.Capture
	Events     *events.Capture
	Out, Err   *outputBuffer
	Source     *fakeSource
	stopped    bool
	siblingsMu sync.Mutex
	siblings   map[string]int
}

func startRun(t *testing.T, o runOptions) *runFixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &runFixture{t: t, Dir: o.Dir, Done: make(chan int, 1), Telemetry: &telemetry.Capture{}, Events: &events.Capture{}, Out: &outputBuffer{}, Err: &outputBuffer{}, Source: o.Source, siblings: map[string]int{}}
	if f.Dir == "" {
		f.Dir = t.TempDir()
	}
	if f.Source == nil {
		f.Source = &fakeSource{}
	}
	sd, e := os.MkdirTemp("", "scrapes-test-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sd) })
	entries := []map[string]any{}
	sibling := func(name string, h http.Handler) {
		ln, e := net.Listen("unix", filepath.Join(sd, name))
		if e != nil {
			t.Fatal(e)
		}
		srv := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
		go func() { _ = srv.Serve(ln) }()
		t.Cleanup(func() { _ = srv.Close() })
		entries = append(entries, map[string]any{"name": name, "socket": ln.Addr().String(), "url": "https://" + name + ".fixture", "enabled": true, "mcp": false, "description": "fixture"})
	}
	for _, name := range []string{"auth", "artifacts", "connections", "cron"} {
		sibling(name, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			f.siblingsMu.Lock()
			f.siblings[name]++
			f.siblingsMu.Unlock()
			w.WriteHeader(204)
		}))
	}
	sibling("telemetry", telemetry.IngestHandler(f.Telemetry))
	sibling("events", events.EmitHandler(f.Events))
	path := filepath.Join(sd, "services.json")
	b, _ := json.Marshal(map[string]any{"services": entries})
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	if o.DefaultSinks {
		t.Setenv(services.Variable, path)
	}
	notify, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(sd, "notify"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = notify.Close() })
	f.Notify = notify
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	env := map[string]string{"LISTEN_PID": "123", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notify.LocalAddr().String(), services.Variable: path}
	for k, v := range o.Env {
		env[k] = v
	}
	rnd := o.Rand
	if rnd == nil {
		rnd = &runRandom{}
	}
	after := o.After
	if after == nil {
		after = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	}
	sink := o.Sink
	if sink == nil {
		sink = f.Telemetry
	}
	es := o.EventSink
	if es == nil {
		es = f.Events
	}
	f.Process = cli.Process{LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }, Unsetenv: func(string) error { return nil }, Pid: 123, Stdout: f.Out, Stderr: f.Err, Version: "fixture-version", Inherit: func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			t.Errorf("descriptor %d", fd)
		}
		return ln, nil
	}, Now: fixtureNow, Sleep: func(ctx context.Context, _ time.Duration) { <-ctx.Done() }, After: after, Rand: rnd, Dir: f.Dir, Sink: sink, EventSink: es, Banner: func(page.User) page.Banner { return page.Banner{Service: "fixture"} }, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: "fixture", Version: "fixture-version", Telemetry: w})
	}, HTTP: &http.Client{Transport: f.Source}}
	if o.DefaultSinks {
		f.Process.Sink = nil
		f.Process.EventSink = nil
	}
	if o.Modify != nil {
		o.Modify(&f.Process)
	}
	readiness := make(chan readyObservation, 1)
	if o.CheckReadyBeforeAccept {
		inherited := f.Process.Inherit
		f.Process.Inherit = func(fd uintptr) (net.Listener, error) {
			ln, e := inherited(fd)
			return &readyFirstListener{Listener: ln, notify: notify, observed: readiness}, e
		}
	}
	accepted := make(chan struct{})
	if o.WithoutNotify {
		inherited := f.Process.Inherit
		f.Process.Inherit = func(fd uintptr) (net.Listener, error) {
			ln, e := inherited(fd)
			return &acceptSignal{Listener: ln, ready: accepted}, e
		}
		lookup := f.Process.LookupEnv
		f.Process.LookupEnv = func(k string) (string, bool) {
			if k == "NOTIFY_SOCKET" {
				return "", false
			}
			return lookup(k)
		}
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	f.Cancel = cancel
	go func() { f.Done <- cli.Run(ctx, f.Process) }()
	switch {
	case o.WithoutNotify:
		select {
		case <-accepted:
		case <-time.After(10 * time.Second):
			t.Fatal("listener not served")
		}
	case o.CheckReadyBeforeAccept:
		select {
		case observation := <-readiness:
			if observation.err != nil || observation.data != "READY=1" {
				cancel(fmt.Errorf("readiness order failure"))
				t.Fatalf("notification before Accept: %q %v", observation.data, observation.err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("listener did not Accept")
		}
	default:
		_ = notify.SetReadDeadline(time.Now().Add(10 * time.Second))
		buf := make([]byte, 64)
		n, _, e := notify.ReadFromUnix(buf)
		if e != nil {
			cancel(fmt.Errorf("fixture failed"))
			t.Fatalf("ready: %v stderr %s", e, f.Err.String())
		}
		if string(buf[:n]) != "READY=1" {
			t.Fatalf("notify %q", buf[:n])
		}
	}
	f.Base = "http://" + ln.Addr().String()
	f.HTTP = &http.Client{Timeout: 5 * time.Second}
	f.Client = mcp.NewClient(mcp.ClientConfig{Endpoint: f.Base + "/mcp", HTTPClient: f.HTTP, Name: "fixture", Version: "fixture"})
	t.Cleanup(func() {
		if !f.stopped {
			f.stop()
		}
	})
	return f
}
func (f *runFixture) stop() int {
	f.t.Helper()
	f.Cancel(fmt.Errorf("fixture stop"))
	select {
	case code := <-f.Done:
		f.stopped = true
		return code
	case <-time.After(10 * time.Second):
		f.t.Fatal("run did not stop")
		return -1
	}
}

var fixtureCaller = identity.Caller{UserID: "fixture-owner", Email: "fixture@example.test", RequestID: "fixture-request"}

func (f *runFixture) call(c identity.Caller, name string, args any) mcp.Result {
	f.t.Helper()
	b, e := json.Marshal(args)
	if e != nil {
		f.t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, e := f.Client.CallTool(ctx, c, name, b)
	if e != nil {
		f.t.Fatal(e)
	}
	return r
}
func resultJSON(t *testing.T, r mcp.Result) map[string]any {
	t.Helper()
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var envelope struct {
		Structured json.RawMessage `json:"structuredContent"`
		Content    []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if e = json.Unmarshal(b, &envelope); e != nil {
		t.Fatal(e)
	}
	payload := envelope.Structured
	if len(payload) == 0 && len(envelope.Content) > 0 {
		payload = []byte(envelope.Content[0].Text)
	}
	var v map[string]any
	if e = json.Unmarshal(payload, &v); e != nil {
		t.Fatalf("result %s: %v", b, e)
	}
	return v
}
func (f *runFixture) awaitRun(id string) map[string]any {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			f.t.Fatal("run did not end")
		default:
		}
		v := resultJSON(f.t, f.call(fixtureCaller, "run", map[string]any{"run": id}))
		if v["status"] == "done" || v["status"] == "failed" {
			return v
		}
	}
}
func (f *runFixture) request(method, path, body string, user bool) *http.Response {
	f.t.Helper()
	r, e := http.NewRequest(method, f.Base+path, bytes.NewBufferString(body))
	if e != nil {
		f.t.Fatal(e)
	}
	r.Header.Set("X-Request-Id", fixtureCaller.RequestID)
	if user {
		r.Header.Set("X-User-Id", fixtureCaller.UserID)
		r.Header.Set("X-User-Email", fixtureCaller.Email)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	resp, e := f.HTTP.Do(r)
	if e != nil {
		f.t.Fatal(e)
	}
	f.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}
func openStore(t *testing.T, dir string) (*store.Store, *db.DB) {
	t.Helper()
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "scrapes.db"), Migrations: app.Migrations(), Now: fixtureNow})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = d.Close() })
	return store.New(d, store.Config{Now: fixtureNow, Rand: &runRandom{}}), d
}

func pendingNotify(t *testing.T, n *net.UnixConn) bool {
	t.Helper()
	raw, e := n.SyscallConn()
	if e != nil {
		t.Fatal(e)
	}
	var readErr error
	if e = raw.Read(func(fd uintptr) bool {
		_, _, readErr = syscall.Recvfrom(int(fd), make([]byte, 128), syscall.MSG_DONTWAIT)
		return true
	}); e != nil {
		t.Fatal(e)
	}
	if errors.Is(readErr, syscall.EAGAIN) || errors.Is(readErr, syscall.EWOULDBLOCK) {
		return false
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	return true
}

type acceptSignal struct {
	net.Listener
	ready chan struct{}
	once  sync.Once
}

func (l *acceptSignal) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.ready) })
	return l.Listener.Accept()
}

func notificationSocket(t *testing.T) *net.UnixConn {
	t.Helper()
	dir, e := os.MkdirTemp("", "scrapes-notify-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	n, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(dir, "notify"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = n.Close() })
	return n
}

type readyObservation struct {
	data string
	err  error
}
type readyFirstListener struct {
	net.Listener
	notify   *net.UnixConn
	observed chan readyObservation
	once     sync.Once
}

func (l *readyFirstListener) Accept() (net.Conn, error) {
	l.once.Do(func() {
		buf := make([]byte, 64)
		raw, e := l.notify.SyscallConn()
		n := 0
		if e == nil {
			var readErr error
			e = raw.Read(func(fd uintptr) bool {
				n, _, readErr = syscall.Recvfrom(int(fd), buf, syscall.MSG_DONTWAIT)
				return true
			})
			if e == nil {
				e = readErr
			}
		}
		if n < 0 {
			n = 0
		}
		l.observed <- readyObservation{string(buf[:n]), e}
	})
	return l.Listener.Accept()
}
