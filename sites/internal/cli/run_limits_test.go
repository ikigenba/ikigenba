package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites"
	"github.com/ikigenba/ikigenba/sites/internal/cli"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
)

func limitWait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("synchronization timed out")
		var zero T
		return zero
	}
}
func limitResultText(t *testing.T, r mcp.Result) string {
	t.Helper()
	b, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Content []struct{ Text string } `json:"content"`
	}
	if err = json.Unmarshal(b, &body); err != nil || len(body.Content) != 1 {
		t.Fatalf("result=%s error=%v", b, err)
	}
	return body.Content[0].Text
}
func limitCall(t *testing.T, f *startFixture, name, args string) mcp.Result {
	t.Helper()
	r, err := f.mcp.CallTool(context.Background(), identity.Caller{UserID: "owner"}, name, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func limitResponse(t *testing.T, f *startFixture, path string, status int, body string) {
	t.Helper()
	res := f.get(t, path)
	b, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil || res.StatusCode != status || (body != "" && string(b) != body) {
		t.Fatalf("response=%d %q error=%v", res.StatusCode, b, err)
	}
}

// R-ZJIT-LFW1 R-YAZ0-C8YJ
func TestRunLimitsBelongToCurrentRun(t *testing.T) {
	first := newStartFixture(t)
	root := filepath.Join(t.TempDir(), "repos")
	sha := startRepo(t, first, root)
	dir := first.p.Dir
	first.set("REPOS_DIR", root)
	first.set("SITE_MAX_BYTES", "10")
	first.set("OPERATION_SECONDS", "11")
	first.start(t)
	site := first.call(t, "create", `{"name":"blog","repo":"rep_0123456789abcdef","visibility":"public","listed":true}`)
	first.call(t, "publish", `{"name":"blog"}`)
	if first.stop(t) != cli.ExitSuccess {
		t.Fatal("first run failed")
	}
	tree := filepath.Join(dir, "cache/sites", site["id"].(string), sha)
	for _, size := range []string{"5", "9", "10"} {
		f := newStartFixture(t)
		f.p.Dir = dir
		f.set("REPOS_DIR", root)
		f.set("SITE_MAX_BYTES", size)
		f.set("OPERATION_SECONDS", "23")
		f.start(t)
		if size == "5" {
			limitResponse(t, f, "/blog/", 200, "hello site")
		}
		if err := os.RemoveAll(tree); err != nil {
			t.Fatal(err)
		}
		r := limitCall(t, f, "publish", `{"name":"blog"}`)
		if size == "10" {
			if r.IsError() {
				t.Fatal(limitResultText(t, r))
			}
			limitResponse(t, f, "/blog/", 200, "hello site")
		} else {
			if !r.IsError() || limitResultText(t, r) != fmt.Sprintf(tools.TooLarge, mustParseLimit(t, size)) {
				t.Fatalf("size %s: %s", size, limitResultText(t, r))
			}
			limitResponse(t, f, "/blog/", 503, "")
			if _, err := os.Stat(tree); !os.IsNotExist(err) {
				t.Fatalf("refused tree remains: %v", err)
			}
		}
		if f.stop(t) != cli.ExitSuccess {
			t.Fatal("later run failed")
		}
	}
	for _, seconds := range []string{"31", "47"} {
		f := newStartFixture(t)
		f.p.Dir = dir
		f.set("REPOS_DIR", root)
		f.set("OPERATION_SECONDS", seconds)
		trace := filepath.Join(t.TempDir(), "ready-deadline-trace")
		f.set("GIT_TRACE2_EVENT", trace)
		var timerCalls atomic.Int32
		f.p.After = func(d time.Duration) <-chan time.Time {
			timerCalls.Add(1)
			n, _ := strconv.Atoi(seconds)
			if d != time.Duration(n)*time.Second {
				t.Errorf("duration %s", d)
			}
			ch := make(chan time.Time, 1)
			ch <- time.Time{}
			return ch
		}
		f.start(t)
		r := limitCall(t, f, "publish", `{"name":"blog"}`)
		if !r.IsError() || limitResultText(t, r) != fmt.Sprintf(tools.TimedOut, mustParseLimit(t, seconds)) {
			t.Fatal(limitResultText(t, r))
		}
		if f.stop(t) != cli.ExitSuccess {
			t.Fatal("timeout run failed")
		}
		if timerCalls.Load() != 1 {
			t.Fatalf("ready deadline timers=%d", timerCalls.Load())
		}
		if _, err := os.Stat(trace); !os.IsNotExist(err) {
			t.Fatalf("ready deadline started git: %v", err)
		}
	}
}

// R-YAZ0-C8YJ
func TestRunGitTimerAccounting(t *testing.T) {
	for _, seconds := range []string{"17", strconv.FormatInt(math.MaxInt64, 10)} {
		t.Run(seconds, func(t *testing.T) {
			f := newStartFixture(t)
			root := filepath.Join(t.TempDir(), "repos")
			sha := startRepo(t, f, root)
			f.set("REPOS_DIR", root)
			f.set("OPERATION_SECONDS", seconds)
			trace := filepath.Join(t.TempDir(), "trace")
			f.set("GIT_TRACE2_EVENT", trace)
			var mu sync.Mutex
			var durations []time.Duration
			f.p.After = func(d time.Duration) <-chan time.Time {
				mu.Lock()
				durations = append(durations, d)
				mu.Unlock()
				return make(chan time.Time)
			}
			f.start(t)
			limitResponse(t, f, "/about", 200, "")
			f.call(t, "list", "{}")
			mu.Lock()
			n := len(durations)
			mu.Unlock()
			if n != 0 {
				t.Fatal("timer without git", n)
			}
			site := f.call(t, "create", `{"name":"blog","repo":"rep_0123456789abcdef","visibility":"public","listed":true}`)
			f.call(t, "publish", `{"name":"blog"}`)
			limitResponse(t, f, "/blog/", 200, "hello site")
			if err := os.RemoveAll(filepath.Join(f.p.Dir, "cache/sites", site["id"].(string), sha)); err != nil {
				t.Fatal(err)
			}
			limitResponse(t, f, "/blog/", 200, "hello site")
			if f.stop(t) != cli.ExitSuccess {
				t.Fatal("stop failed")
			}
			b, err := startReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			starts := 0
			for _, line := range bytes.Split(b, []byte{'\n'}) {
				if len(line) == 0 {
					continue
				}
				var event struct {
					Event string `json:"event"`
				}
				if err = json.Unmarshal(line, &event); err != nil {
					t.Fatal(err)
				}
				if event.Event == "start" {
					starts++
				}
			}
			want := 17 * time.Second
			if seconds != "17" {
				want = time.Duration(math.MaxInt64)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(durations) != starts || starts == 0 {
				t.Fatalf("timers=%d git starts=%d", len(durations), starts)
			}
			for _, d := range durations {
				if d != want {
					t.Fatalf("duration=%s want=%s", d, want)
				}
			}
		})
	}
}

type limitHeldGit struct {
	event *os.File
	pid   int
	pidfd int
}

func limitHoldGit(t *testing.T, f *startFixture) (*limitHeldGit, chan chan time.Time) {
	t.Helper()
	dir := t.TempDir()
	events := filepath.Join(dir, "events")
	hold := filepath.Join(dir, "hold")
	if err := syscall.Mkfifo(events, 0600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(hold, 0600); err != nil {
		t.Fatal(err)
	}
	// RDWR avoids a blocking open and keeps the event reader cancellable on failure.
	eventRoot, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eventRoot.Close() })
	event, err := eventRoot.OpenFile("events", os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = event.Close() })
	f.set("GIT_TRACE2_EVENT", events)
	f.set("GIT_TRACE", hold)
	timers := make(chan chan time.Time, 16)
	f.p.After = func(time.Duration) <-chan time.Time { timer := make(chan time.Time, 1); timers <- timer; return timer }
	return &limitHeldGit{event: event, pidfd: -1}, timers
}
func (h *limitHeldGit) started(t *testing.T) {
	t.Helper()
	started := make(chan error, 1)
	go func() {
		dec := json.NewDecoder(h.event)
		for {
			var event struct{ Event, SID string }
			if err := dec.Decode(&event); err != nil {
				started <- err
				return
			}
			if event.Event != "start" {
				continue
			}
			at := strings.LastIndex(event.SID, "-P")
			if at < 0 {
				started <- errors.New("git trace lacks PID")
				return
			}
			pid, err := strconv.ParseInt(event.SID[at+2:], 16, 32)
			if err != nil {
				started <- err
				return
			}
			h.pid = int(pid)
			fd, _, errno := syscall.Syscall(434, uintptr(h.pid), 0, 0)
			if errno != 0 {
				started <- errno
				return
			}
			h.pidfd = int(fd)
			started <- nil
			return
		}
	}()
	if err := limitWait(t, started); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(h.pidfd) })
}
func (h *limitHeldGit) exited(t *testing.T) {
	t.Helper()
	var fds syscall.FdSet
	if h.pidfd < 0 || h.pidfd >= len(fds.Bits)*64 {
		t.Fatal("pidfd outside select range")
	}
	fds.Bits[h.pidfd/64] |= int64(1) << uint(h.pidfd%64)
	n, err := syscall.Select(h.pidfd+1, &fds, nil, nil, &syscall.Timeval{})
	if err != nil || n != 1 {
		t.Fatalf("git alive when answer received: %d %v", n, err)
	}
	if err = syscall.Kill(-h.pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("git process group still alive: %v", err)
	}
}

// R-YAZ0-C8YJ
func TestRunLiveGitDeadlineEndsBeforeAnswer(t *testing.T) {
	for _, request := range []string{"publish", "rebuild"} {
		t.Run(request, func(t *testing.T) {
			f := newStartFixture(t)
			root := filepath.Join(t.TempDir(), "repos")
			sha := startRepo(t, f, root)
			f.set("REPOS_DIR", root)
			f.set("OPERATION_SECONDS", "19")
			// Configure the FIFO after setup; Environ is read anew for each git.
			f.start(t)
			site := f.call(t, "create", `{"name":"blog","repo":"rep_0123456789abcdef","visibility":"public","listed":true}`)
			f.call(t, "publish", `{"name":"blog"}`)
			// Restart with the controlled timer and the existing catalog.
			if f.stop(t) != cli.ExitSuccess {
				t.Fatal("setup stop")
			}
			g := newStartFixture(t)
			g.p.Dir = f.p.Dir
			g.set("REPOS_DIR", root)
			g.set("OPERATION_SECONDS", "19")
			held, timers := limitHoldGit(t, g)
			g.start(t)
			if request == "rebuild" {
				if err := os.RemoveAll(filepath.Join(g.p.Dir, "cache/sites", site["id"].(string), sha)); err != nil {
					t.Fatal(err)
				}
			}
			answered := make(chan string, 1)
			go func() {
				if request == "publish" {
					r, e := g.mcp.CallTool(context.Background(), identity.Caller{UserID: "owner"}, "publish", json.RawMessage(`{"name":"blog"}`))
					if e != nil {
						answered <- e.Error()
						return
					}
					answered <- limitResultText(t, r)
				} else {
					req, _ := http.NewRequest(http.MethodGet, "http://sites/blog/", nil)
					res, e := g.http.Do(req)
					if e != nil {
						answered <- e.Error()
						return
					}
					b, e := io.ReadAll(res.Body)
					_ = res.Body.Close()
					answered <- fmt.Sprintf("%d %s %v", res.StatusCode, b, e)
				}
			}()
			timer := limitWait(t, timers)
			held.started(t)
			timer <- time.Time{}
			answer := limitWait(t, answered)
			held.exited(t)
			if request == "publish" && answer != fmt.Sprintf(tools.TimedOut, 19) {
				t.Fatal(answer)
			}
			if request == "rebuild" {
				templates, err := page.Templates().ParseFS(sites.Assets(), "*.html")
				if err != nil {
					t.Fatal(err)
				}
				var expected bytes.Buffer
				if err := templates.ExecuteTemplate(&expected, "unavailable", pages.NoticeData{Banner: g.p.Banner(page.User{})}); err != nil {
					t.Fatal(err)
				}
				if answer != fmt.Sprintf("%d %s %v", http.StatusServiceUnavailable, expected.String(), nil) {
					t.Fatal("rebuild answer differs from unavailable template")
				}
			}
			if g.stop(t) != cli.ExitSuccess {
				t.Fatal("deadline stop failed")
			}
			if request == "rebuild" {
				found := false
				for _, event := range g.capture.Events() {
					if event.Name == "site.unavailable" {
						found = true
						if event.Attrs["reason"] != "timed_out" {
							t.Fatalf("unavailable reason: %v", event.Attrs)
						}
					}
				}
				if !found {
					t.Fatal("missing unavailable event")
				}
			}
		})
	}
}

type limitSink struct{}

func (limitSink) Deliver(context.Context, telemetry.Event) error { return errors.New("rejected") }

type limitGuard struct {
	active   atomic.Int32
	overlap  atomic.Bool
	returned atomic.Bool
	late     atomic.Bool
	calls    atomic.Int64
}

func (g *limitGuard) enter() {
	if g.active.Add(1) != 1 {
		g.overlap.Store(true)
	}
	if g.returned.Load() {
		g.late.Store(true)
	}
	runtime.Gosched()
	g.calls.Add(1)
}
func (g *limitGuard) leave() { g.active.Add(-1) }

type limitOutput struct{ limitGuard }

func (g *limitOutput) Write(p []byte) (int, error) { g.enter(); defer g.leave(); return len(p), nil }

type limitRandom struct {
	limitGuard
	serial uint64
}

func (g *limitRandom) Read(p []byte) (int, error) {
	g.enter()
	defer g.leave()
	g.serial++
	for i := range p {
		p[i] = byte(((g.serial >> uint((i%8)*8)) + uint64(i)) & 255)
	}
	return len(p), nil
}

// R-WEY1-ABHP
func TestRunSerializesRandomAndStderr(t *testing.T) {
	f := newStartFixture(t)
	root := filepath.Join(t.TempDir(), "repos")
	startRepo(t, f, root)
	f.set("REPOS_DIR", root)
	stderr := &limitOutput{}
	random := &limitRandom{}
	f.p.Stderr = stderr
	f.p.Rand = random
	f.p.Sink = limitSink{}
	f.p.Sleep = func(context.Context, time.Duration) {}
	f.start(t)
	f.call(t, "create", `{"name":"base","repo":"rep_0123456789abcdef","visibility":"public","listed":true}`)
	f.call(t, "publish", `{"name":"base"}`)
	var wg sync.WaitGroup
	failures := make(chan error, 32)
	begin := make(chan struct{})
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-begin
			r, e := f.mcp.CallTool(context.Background(), identity.Caller{UserID: "owner"}, "create", json.RawMessage(fmt.Sprintf(`{"name":"site%d","repo":"rep_0123456789abcdef","visibility":"public","listed":false}`, i)))
			if e != nil || r.IsError() {
				failures <- fmt.Errorf("create result %v: %w", r, e)
				return
			}
			req, _ := http.NewRequest(http.MethodGet, "http://sites/base/", nil)
			res, e := f.http.Do(req)
			if e != nil {
				failures <- e
				return
			}
			_, e = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			if e != nil {
				failures <- e
			}
		}(i)
	}
	close(begin)
	wg.Wait()
	close(failures)
	for e := range failures {
		t.Error(e)
	}
	if f.stop(t) != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	stderr.returned.Store(true)
	if stderr.overlap.Load() || random.overlap.Load() || stderr.late.Load() || stderr.calls.Load() == 0 || random.calls.Load() < 24 {
		t.Fatalf("stderr overlap=%v late=%v calls=%d; rand overlap=%v calls=%d", stderr.overlap.Load(), stderr.late.Load(), stderr.calls.Load(), random.overlap.Load(), random.calls.Load())
	}
}

type limitRetryError struct{}

func (limitRetryError) Error() string   { return "temporary accept failure" }
func (limitRetryError) Temporary() bool { return true }
func (limitRetryError) Timeout() bool   { return false }

type limitRetryListener struct {
	net.Listener
	once    sync.Once
	retried chan struct{}
}

func (l *limitRetryListener) Accept() (net.Conn, error) {
	first := false
	l.once.Do(func() { first = true })
	if first {
		return nil, limitRetryError{}
	}
	select {
	case <-l.retried:
	default:
		close(l.retried)
	}
	return l.Listener.Accept()
}

// R-WG5X-O38E
func TestRunNeverUsesDefaultLogger(t *testing.T) {
	f := newStartFixture(t)
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	retry := &limitRetryListener{Listener: f.ln, retried: make(chan struct{})}
	f.p.Inherit = func(uintptr) (net.Listener, error) { return retry, nil }
	panicked := make(chan struct{})
	f.p.Banner = func(page.User) page.Banner { close(panicked); panic("banner failure") }
	f.start(t)
	request, _ := http.NewRequest(http.MethodGet, "http://sites/", nil)
	request.Header.Set("X-User-Id", "owner")
	res, _ := f.http.Do(request)
	if res != nil {
		_ = res.Body.Close()
	}
	limitWait(t, panicked)
	limitWait(t, retry.retried)
	if f.stop(t) != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	if output.Len() != 0 {
		t.Fatalf("default logger: %s", output.String())
	}
}

type limitLateListener struct {
	net.Listener
	returned *atomic.Bool
	closed   chan struct{}
	once     sync.Once
}

func (l *limitLateListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return &limitLateConn{Conn: c, listener: l}, nil
}

type limitLateConn struct {
	net.Conn
	listener *limitLateListener
}

func (c *limitLateConn) Close() error {
	err := c.Conn.Close()
	if c.listener.returned.Load() {
		c.listener.once.Do(func() { close(c.listener.closed) })
	}
	return err
}

// R-WEY1-ABHP
func TestRunMakesNoLateStderrWrite(t *testing.T) {
	f := newStartFixture(t)
	f.set("DRAIN_SECONDS", "1")
	output := &limitOutput{}
	f.p.Stderr = output
	f.p.Sink = limitSink{}
	f.p.Sleep = func(context.Context, time.Duration) {}
	listener := &limitLateListener{Listener: f.ln, returned: &output.returned, closed: make(chan struct{})}
	f.p.Inherit = func(uintptr) (net.Listener, error) { return listener, nil }
	entered := make(chan struct{})
	release := make(chan struct{})
	f.p.Banner = func(page.User) page.Banner { close(entered); <-release; panic("late banner") }
	f.start(t)
	answered := make(chan struct{})
	go func() {
		req, _ := http.NewRequest(http.MethodGet, "http://sites/", nil)
		req.Header.Set("X-User-Id", "owner")
		res, _ := f.http.Do(req)
		if res != nil {
			_ = res.Body.Close()
		}
		close(answered)
	}()
	limitWait(t, entered)
	if code := f.stop(t); code != cli.ExitServerFailed {
		t.Fatalf("cutoff exit=%d", code)
	}
	output.returned.Store(true)
	close(release)
	// The serving connection's final close follows the held handler's exit.
	limitWait(t, listener.closed)
	limitWait(t, answered)
	if output.late.Load() || output.overlap.Load() {
		t.Fatalf("late=%v overlap=%v", output.late.Load(), output.overlap.Load())
	}
}

func mustParseLimit(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
