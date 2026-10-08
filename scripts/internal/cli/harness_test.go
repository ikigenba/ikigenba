package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts/internal/cli"
	"github.com/ikigenba/ikigenba/scripts/internal/pages"
	"github.com/ikigenba/ikigenba/scripts/internal/runner"
)

const testVersion = "test display"

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
	writes [][]byte
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writes = append(b.writes, bytes.Clone(p))
	return b.Buffer.Write(p)
}
func (b *lockedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Buffer.String() }
func (b *lockedBuffer) snapshot() [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([][]byte(nil), b.writes...)
}

type countingRandom struct{ n byte }

func (r *countingRandom) Read(b []byte) (int, error) {
	for i := range b {
		r.n++
		b[i] = r.n
	}
	return len(b), nil
}

type repeatingRandom byte

func (r repeatingRandom) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = byte(r)
	}
	return len(b), nil
}

type recordingSink struct {
	capture telemetry.Capture
	events  chan telemetry.Event
}

func (s *recordingSink) Deliver(ctx context.Context, e telemetry.Event) error {
	_ = s.capture.Deliver(ctx, e)
	s.events <- e
	return nil
}

type runHarness struct {
	t                            *testing.T
	p                            cli.Process
	root, git, python, repo, sha string
	envMu                        sync.Mutex
	env                          map[string]string
	stdout, stderr               lockedBuffer
	sink                         *recordingSink
	notify                       *net.UnixConn
	listener                     net.Listener
	http                         *http.Client
	client                       *mcp.Client
	cancel                       context.CancelCauseFunc
	done                         chan int
	timers, gitTimers            chan chan time.Time
	durations, gitDurations      chan time.Duration
	now                          time.Time
	started, stopped             bool
}

func newHarness(t *testing.T) *runHarness {
	t.Helper()
	t.Setenv(services.Variable, "")
	root := t.TempDir()
	git, e := exec.LookPath("git")
	mustCLI(t, e)
	python, e := exec.LookPath(runner.Interpreter)
	mustCLI(t, e)
	h := &runHarness{t: t, root: root, git: git, python: python, env: map[string]string{}, sink: &recordingSink{events: make(chan telemetry.Event, 4096)}, timers: make(chan chan time.Time, 128), gitTimers: make(chan chan time.Time, 1024), durations: make(chan time.Duration, 128), gitDurations: make(chan time.Duration, 1024), now: time.Date(2025, 2, 3, 4, 5, 6, 123456789, time.FixedZone("east", 3600))}
	h.env = map[string]string{"PATH": filepath.Dir(git) + ":" + filepath.Dir(python), "HOME": root, "XDG_CONFIG_HOME": root, "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_TERMINAL_PROMPT": "0", "GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "credential.helper", "GIT_CONFIG_VALUE_0": "", "GIT_AUTHOR_NAME": "Fixture", "GIT_AUTHOR_EMAIL": "fixture@example.test", "GIT_COMMITTER_NAME": "Fixture", "GIT_COMMITTER_EMAIL": "fixture@example.test", "GIT_AUTHOR_DATE": "2025-01-01T00:00:00Z", "GIT_COMMITTER_DATE": "2025-01-01T00:00:00Z", "LANG": "C.UTF-8", "LISTEN_PID": "123", "LISTEN_FDS": "1", "DRAIN_SECONDS": "1", "REPOS_DIR": filepath.Join(root, "repositories")}
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	mustCLI(t, e)
	h.listener = ln
	short, e := os.MkdirTemp("", "scripts-notify-")
	mustCLI(t, e)
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	h.notify, e = net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(short, "ready"), Net: "unixgram"})
	mustCLI(t, e)
	h.env["NOTIFY_SOCKET"] = h.notify.LocalAddr().String()
	cgroup := filepath.Join(root, "cgroup")
	mustCLI(t, os.Mkdir(cgroup, 0700))
	mustCLI(t, os.WriteFile(filepath.Join(cgroup, "cgroup.procs"), []byte("123\n"), 0600))
	h.p = cli.Process{Version: testVersion, Cgroup: cgroup, Pid: 123, Dir: filepath.Join(root, "scripts"), LookupEnv: h.lookup, Environ: h.environ, Unsetenv: h.unset, Inherit: func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			return nil, errors.New("unexpected descriptor")
		}
		return h.listener, nil
	}, Stdout: &h.stdout, Stderr: &h.stderr, Now: func() time.Time { return h.now }, Rand: &countingRandom{}, Sink: h.sink, Sleep: func(context.Context, time.Duration) {}, After: func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		h.gitTimers <- ch
		h.gitDurations <- d
		return ch
	}, ScriptAfter: func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		h.timers <- ch
		h.durations <- d
		return ch
	}, Banner: page.New(pages.ServiceName, testVersion).Banner, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: pages.ServiceName, Version: testVersion, Telemetry: w})
	}}
	h.http = &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	h.client = mcp.NewClient(mcp.ClientConfig{Endpoint: "http://" + ln.Addr().String() + "/mcp", HTTPClient: h.http})
	t.Cleanup(func() {
		if h.started && !h.stopped {
			h.stop()
		}
		_ = h.listener.Close()
		_ = h.notify.Close()
		h.http.CloseIdleConnections()
		scoped, e := os.OpenRoot(root)
		if e != nil {
			t.Error(e)
			return
		}
		defer func() { _ = scoped.Close() }()
		_ = fs.WalkDir(scoped.FS(), ".", func(p string, d fs.DirEntry, e error) error {
			if e != nil || d.Type()&os.ModeSymlink != 0 {
				return e
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			return scoped.Chmod(p, info.Mode().Perm()|0700)
		})
	})
	return h
}
func mustCLI(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func (h *runHarness) lookup(key string) (string, bool) {
	h.envMu.Lock()
	defer h.envMu.Unlock()
	v, ok := h.env[key]
	return v, ok
}
func (h *runHarness) set(key, v string) { h.envMu.Lock(); defer h.envMu.Unlock(); h.env[key] = v }
func (h *runHarness) unset(key string) error {
	h.envMu.Lock()
	defer h.envMu.Unlock()
	delete(h.env, key)
	return nil
}
func (h *runHarness) environ() []string {
	h.envMu.Lock()
	defer h.envMu.Unlock()
	keys := make([]string, 0, len(h.env))
	for k := range h.env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, k := range keys {
		result = append(result, k+"="+h.env[k])
	}
	return result
}
func (h *runHarness) start() {
	h.t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	h.cancel = cancel
	h.done = make(chan int, 1)
	h.started = true
	go func() { h.done <- cli.Run(ctx, h.p) }()
	mustCLI(h.t, h.notify.SetReadDeadline(time.Now().Add(10*time.Second)))
	b := make([]byte, 64)
	n, _, e := h.notify.ReadFromUnix(b)
	mustCLI(h.t, e)
	if string(b[:n]) != "READY=1" {
		h.t.Fatalf("notify %q", b[:n])
	}
}
func (h *runHarness) finish() int {
	h.t.Helper()
	select {
	case code := <-h.done:
		h.stopped = true
		return code
	case <-time.After(10 * time.Second):
		h.t.Fatal("Run did not stop")
		return -1
	}
}
func (h *runHarness) stop() {
	h.t.Helper()
	h.cancel(errors.New("test stop"))
	if code := h.finish(); code != cli.ExitSuccess {
		h.t.Fatalf("stop %d: %s", code, h.stderr.String())
	}
}
func (h *runHarness) result(name string, args any) (mcp.Result, error) {
	h.t.Helper()
	var raw json.RawMessage
	if args != nil {
		b, e := json.Marshal(args)
		mustCLI(h.t, e)
		raw = b
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return h.client.CallTool(ctx, identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "request-" + name}, name, raw)
}
func (h *runHarness) call(name string, args any) map[string]any {
	h.t.Helper()
	r, e := h.result(name, args)
	mustCLI(h.t, e)
	b, e := r.MarshalJSON()
	mustCLI(h.t, e)
	if r.IsError() {
		h.t.Fatalf("%s: %s", name, b)
	}
	var v struct {
		Structured map[string]any `json:"structuredContent"`
	}
	mustCLI(h.t, json.Unmarshal(b, &v))
	return v.Structured
}
func (h *runHarness) request(method, path string) (int, string) {
	h.t.Helper()
	req, e := http.NewRequest(method, "http://"+h.listener.Addr().String()+path, nil)
	mustCLI(h.t, e)
	req.Header.Set("X-User-Id", "owner")
	req.Header.Set("X-User-Email", "owner@example.test")
	req.Header.Set("X-Request-Id", "http-request")
	req.Header.Set("X-Forwarded-Proto", "https")
	res, e := h.http.Do(req)
	mustCLI(h.t, e)
	defer func() { _ = res.Body.Close() }()
	b, e := io.ReadAll(res.Body)
	mustCLI(h.t, e)
	return res.StatusCode, string(b)
}
func (h *runHarness) event(name string) telemetry.Event {
	h.t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-h.sink.events:
			if e.Name == name {
				return e
			}
		case <-timer.C:
			h.t.Fatalf("missing event %s", name)
			return telemetry.Event{}
		}
	}
}
func (h *runHarness) command(dir string, args ...string) string {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.git, args...)
	cmd.Dir = dir
	cmd.Env = h.environ()
	b, e := cmd.CombinedOutput()
	if e != nil {
		h.t.Fatalf("git %v: %s: %v", args, b, e)
	}
	return string(b)
}
func (h *runHarness) repository(script string) {
	h.t.Helper()
	repos, _ := h.lookup("REPOS_DIR")
	if !filepath.IsAbs(repos) {
		repos = filepath.Join(h.p.Dir, repos)
	}
	h.repo = filepath.Join(repos, "rep_0102030405060708.git")
	work := filepath.Join(h.root, "work")
	mustCLI(h.t, os.MkdirAll(work, 0700))
	h.command(work, "init", "--initial-branch=main")
	mustCLI(h.t, os.WriteFile(filepath.Join(work, "main.py"), []byte(script), 0600))
	h.command(work, "add", ".")
	h.command(work, "commit", "-m", "fixture")
	mustCLI(h.t, os.MkdirAll(repos, 0700))
	h.command(h.root, "clone", "--bare", work, h.repo)
	h.command(h.repo, "config", "ikigenba.owner", "owner")
	h.command(h.repo, "config", "ikigenba.name", "fixture")
	h.sha = strings.TrimSpace(h.command(h.repo, "rev-parse", "HEAD"))
}
func (h *runHarness) create(name string) map[string]any {
	return h.call("create", map[string]any{"name": name, "repo": "rep_0102030405060708"})
}
