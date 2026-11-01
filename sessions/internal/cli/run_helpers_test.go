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
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/sessions/internal/cli"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
)

var runInstant = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

type safeOutput struct {
	mu    sync.Mutex
	b     bytes.Buffer
	calls int
}

func (b *safeOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	return b.b.Write(p)
}
func (b *safeOutput) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

type repeatByte byte

func (b repeatByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}

type runRig struct {
	p           cli.Process
	env         map[string]string
	out, errout *safeOutput
	ln          net.Listener
	notify      *net.UnixConn
	client      *mcp.Client
	http        *http.Client
	capture     *telemetry.Capture
	writer      *telemetry.Writer
	cancel      context.CancelCauseFunc
	done        chan int
	mcpCalls    int
	built       chan struct{}
	stopped     bool
}

func shortDir(t *testing.T) string {
	t.Helper()
	d, e := os.MkdirTemp("", "sessions-test-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := os.RemoveAll(d); e != nil {
			t.Error(e)
		}
	})
	return d
}
func newRunRig(t *testing.T) *runRig {
	t.Helper()
	t.Setenv(services.Variable, "")
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	n, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(shortDir(t), "ready"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	r := &runRig{out: new(safeOutput), errout: new(safeOutput), ln: ln, notify: n, capture: new(telemetry.Capture), done: make(chan int, 1), built: make(chan struct{})}
	r.env = map[string]string{"LISTEN_PID": "241", "LISTEN_FDS": "1", "NOTIFY_SOCKET": n.LocalAddr().String(), "DRAIN_SECONDS": "1"}
	r.p = cli.Process{LookupEnv: func(k string) (string, bool) {
		switch k {
		case "DRAIN_SECONDS", "LISTEN_PID", "LISTEN_FDS", "NOTIFY_SOCKET", services.Variable:
		default:
			panic("unexpected environment key: " + k)
		}
		v, ok := r.env[k]
		return v, ok
	}, Unsetenv: func(string) error { return nil }, Pid: 241, Stdout: r.out, Stderr: r.errout, Version: "run-test-identity", Inherit: func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			panic("wrong descriptor")
		}
		return r.ln, nil
	}, Now: func() time.Time { return runInstant }, Sleep: func(context.Context, time.Duration) {}, Rand: repeatByte(0x5a), Dir: t.TempDir(), Sink: r.capture, Banner: page.New(pages.ServiceName, version.Identity{}).Banner, MCP: func(w *telemetry.Writer) *mcp.Server {
		r.mcpCalls++
		r.writer = w
		close(r.built)
		return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: r.p.Version, Telemetry: w})
	}}
	r.http = &http.Client{Transport: &http.Transport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Timeout: 5 * time.Second}
	r.client = mcp.NewClient(mcp.ClientConfig{Endpoint: "http://" + ln.Addr().String() + "/mcp", HTTPClient: r.http, Name: "run-tests", Version: "test"})
	t.Cleanup(func() {
		if r.cancel != nil && !r.stopped {
			r.stop(t)
		}
		_ = n.Close()
		_ = ln.Close()
		r.http.CloseIdleConnections()
	})
	return r
}
func (r *runRig) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	r.cancel = cancel
	go func() { r.done <- cli.Run(ctx, r.p) }()
	r.ready(t)
}
func (r *runRig) ready(t *testing.T) {
	t.Helper()
	select {
	case <-r.built:
	case <-time.After(5 * time.Second):
		t.Fatal("MCP not built")
	}
	if e := r.notify.SetReadDeadline(time.Now().Add(5 * time.Second)); e != nil {
		t.Fatal(e)
	}
	var b [128]byte
	n, _, e := r.notify.ReadFromUnix(b[:])
	if e != nil {
		t.Fatalf("readiness: %v; stderr %s", e, r.errout.String())
	}
	if string(b[:n]) != "READY=1" {
		t.Fatalf("readiness %q", b[:n])
	}
}
func (r *runRig) stop(t *testing.T) int {
	t.Helper()
	r.cancel(errors.New("test stop"))
	select {
	case code := <-r.done:
		r.stopped = true
		assertCode(t, code)
		return code
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
		return -1
	}
}
func (r *runRig) request(t *testing.T, path string, signed bool) (int, string, http.Header) {
	t.Helper()
	req, e := http.NewRequest(http.MethodGet, "http://"+r.ln.Addr().String()+path, nil)
	if e != nil {
		t.Fatal(e)
	}
	req.Host = "sessions.example.test"
	req.Header.Set("X-Forwarded-Proto", "https")
	if signed {
		req.Header.Set("X-User-Id", "owner")
		req.Header.Set("X-User-Email", "owner@example.test")
	}
	v, e := r.http.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = v.Body.Close() }()
	b, e := io.ReadAll(v.Body)
	if e != nil {
		t.Fatal(e)
	}
	return v.StatusCode, string(b), v.Header
}
func (r *runRig) call(t *testing.T, name string, args any) mcp.Result {
	t.Helper()
	b, e := json.Marshal(args)
	if e != nil {
		t.Fatal(e)
	}
	v, e := r.client.CallTool(context.Background(), identity.Caller{UserID: "owner", Email: "owner@example.test"}, name, b)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func (r *runRig) ok(t *testing.T, name string, args any) mcp.Result {
	t.Helper()
	v := r.call(t, name, args)
	if v.IsError() {
		t.Fatalf("%s: %+v", name, v)
	}
	return v
}
func runDecode[T any](t *testing.T, v mcp.Result) T {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	var envelope struct {
		Structured json.RawMessage `json:"structuredContent"`
	}
	if e = json.Unmarshal(b, &envelope); e != nil {
		t.Fatal(e)
	}
	var out T
	if e = json.Unmarshal(envelope.Structured, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func pushRun(t *testing.T, r *runRig, id, moment string, input int64) {
	t.Helper()
	r.ok(t, tools.ToolPushTurns, tools.PushTurnsArgs{Sessions: []tools.PushSession{{Tool: "codex", ID: id, Host: "laptop", Directory: "/work", Turns: []tools.PushTurn{{Moment: moment, Whose: "person", Input: input}}}}})
}
func servicesFile(t *testing.T, entries []map[string]any) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "services.json")
	writeServices(t, p, entries)
	return p
}
func writeServices(t *testing.T, p string, entries []map[string]any) {
	t.Helper()
	b, e := json.Marshal(map[string]any{"services": entries})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func serviceEntry(name, url, socket string) map[string]any {
	return map[string]any{"name": name, "url": url, "socket": socket, "description": "fixture", "enabled": true, "mcp": true}
}
func assertRunTree(t *testing.T, dir string) {
	t.Helper()
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 1 || entries[0].Name() != "state" || !entries[0].IsDir() {
		t.Fatalf("run directory: %v", entries)
	}
	entries, e = os.ReadDir(filepath.Join(dir, "state"))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, entry := range entries {
		if len(entry.Name()) < len("sessions.db") || entry.Name()[:len("sessions.db")] != "sessions.db" || entry.IsDir() {
			t.Fatalf("unexpected state file %s", entry.Name())
		}
		if entry.Name() == "sessions.db" {
			info, e := entry.Info()
			if e != nil {
				t.Fatal(e)
			}
			if !info.Mode().IsRegular() {
				t.Fatalf("database is not a regular file: %s", info.Mode())
			}
			found = true
		}
	}
	if !found {
		t.Fatal("database absent")
	}
}

func decimal(i int) string { return strconv.Itoa(i) }

func assertNoNotification(t *testing.T, n *net.UnixConn) {
	t.Helper()
	raw, e := n.SyscallConn()
	if e != nil {
		t.Fatal(e)
	}
	var b [32]byte
	var readErr error
	if e = raw.Control(func(fd uintptr) { _, _, readErr = syscall.Recvfrom(int(fd), b[:], syscall.MSG_DONTWAIT) }); e != nil {
		t.Fatal(e)
	}
	if !errors.Is(readErr, syscall.EAGAIN) && !errors.Is(readErr, syscall.EWOULDBLOCK) {
		t.Fatalf("unexpected notification or read error: %q / %v", b[:], readErr)
	}
}
