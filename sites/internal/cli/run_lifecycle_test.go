package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
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
	"github.com/ikigenba/ikigenba/sites/internal/cli"
	"github.com/ikigenba/ikigenba/sites/internal/server"
)

type lifeListener struct {
	net.Listener
	closed   chan struct{}
	accepted chan struct{}
	once     sync.Once
	onClose  func()
}

func (l *lifeListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e == nil {
		select {
		case l.accepted <- struct{}{}:
		default:
		}
		if l.onClose != nil {
			c = &lifeConn{Conn: c, onClose: l.onClose}
		}
	}
	return c, e
}
func (l *lifeListener) Close() error {
	e := l.Listener.Close()
	l.once.Do(func() { close(l.closed) })
	return e
}

type lifeConn struct {
	net.Conn
	onClose func()
}

func (c *lifeConn) Close() error { c.onClose(); return c.Conn.Close() }
func lifeObserve(f *startFixture) *lifeListener {
	l := &lifeListener{Listener: f.ln, closed: make(chan struct{}), accepted: make(chan struct{}, 32)}
	f.ln = l
	return l
}
func lifeWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("lifecycle sync point did not arrive")
	}
}
func lifeExit(t *testing.T, f *startFixture) int {
	t.Helper()
	select {
	case code := <-f.done:
		return code
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
		return -1
	}
}
func lifeConnect(t *testing.T, f *startFixture) net.Conn {
	t.Helper()
	c, e := net.DialTimeout("tcp", f.ln.Addr().String(), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	return c
}
func lifeGet(t *testing.T, f *startFixture, path, id, user string) *http.Response {
	t.Helper()
	r, e := http.NewRequest(http.MethodGet, "http://sites"+path, nil)
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("X-Request-Id", id)
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	res, e := f.http.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	return res
}
func lifeRead(t *testing.T, res *http.Response) []byte {
	t.Helper()
	b, e := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	return b
}

// R-W1J5-2UC2
func TestRunLifecycleAcceptedHeadersWhileDraining(t *testing.T) {
	f := newStartFixture(t)
	l := lifeObserve(f)
	f.set("DRAIN_SECONDS", "2")
	root := t.TempDir()
	startRepo(t, f, root)
	f.set("REPOS_DIR", root)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	banner := f.p.Banner
	f.p.Banner = func(u page.User) page.Banner { once.Do(func() { close(entered) }); <-release; return banner(u) }
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	var gitCalls atomic.Int64
	f.p.After = func(time.Duration) <-chan time.Time { gitCalls.Add(1); return make(chan time.Time) }
	f.start(t)
	site := f.call(t, "create", `{"name":"blog","repo":"rep_0123456789abcdef","visibility":"public"}`)
	published := f.call(t, "publish", `{"name":"blog"}`)
	if e := os.RemoveAll(filepath.Join(f.p.Dir, "cache/sites", site["id"].(string), published["commit"].(string))); e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://sites/about", nil)
	req.Header.Set("X-User-Id", "owner")
	go func() {
		res, e := f.http.Do(req)
		if e == nil {
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		}
	}()
	lifeWait(t, entered)
	for len(l.accepted) > 0 {
		<-l.accepted
	}
	c := lifeConnect(t, f)
	if _, e := io.WriteString(c, "GET /blog/ HTTP/1.1\r\nHost: sites\r\nX-Request-Id: late-view\r\n"); e != nil {
		t.Fatal(e)
	}
	lifeWait(t, l.accepted)
	before := gitCalls.Load()
	f.cancel(errors.New("drain partial headers"))
	lifeWait(t, l.closed)
	if _, e := io.WriteString(c, "\r\n"); e != nil {
		t.Fatal(e)
	}
	res, e := http.ReadResponse(bufio.NewReader(c), nil)
	if e != nil {
		t.Fatalf("accepted request lost during drain: %v", e)
	}
	_ = lifeRead(t, res)
	if res.StatusCode != 503 || res.Header.Get("Retry-After") != "30" || gitCalls.Load() != before {
		t.Fatalf("status=%d retry=%q git=%d/%d", res.StatusCode, res.Header.Get("Retry-After"), gitCalls.Load(), before)
	}
	close(release)
	if lifeExit(t, f) != cli.ExitSuccess {
		t.Fatal(f.err.text())
	}
}

type lifeSink func(context.Context, telemetry.Event) error

func (s lifeSink) Deliver(ctx context.Context, e telemetry.Event) error { return s(ctx, e) }

type lifeWrites struct {
	mu      sync.Mutex
	chunks  [][]byte
	changed chan struct{}
}

func (w *lifeWrites) Write(b []byte) (int, error) {
	w.mu.Lock()
	w.chunks = append(w.chunks, bytes.Clone(b))
	w.mu.Unlock()
	select {
	case w.changed <- struct{}{}:
	default:
	}
	return len(b), nil
}
func (w *lifeWrites) snapshot() [][]byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([][]byte(nil), w.chunks...)
}
func (w *lifeWrites) text() string { return string(bytes.Join(w.snapshot(), nil)) }

type lifeRand struct{ next byte }

func (r *lifeRand) Read(b []byte) (int, error) {
	r.next++
	for i := range b {
		b[i] = r.next
	}
	return len(b), nil
}
func lifeCall(ctx context.Context, f *startFixture, id, name, args string) (mcp.Result, error) {
	return f.mcp.CallTool(ctx, identity.Caller{UserID: "owner", RequestID: id}, name, json.RawMessage(args))
}
func lifeSite(t *testing.T, f *startFixture, name string) map[string]any {
	t.Helper()
	return f.call(t, "create", fmt.Sprintf(`{"name":%q,"repo":"rep_0123456789abcdef","visibility":"public"}`, name))
}
func lifeEventLines(t *testing.T, w *lifeWrites) []telemetry.Event {
	t.Helper()
	var events []telemetry.Event
	for _, b := range w.snapshot() {
		if !bytes.HasPrefix(b, []byte("sites: undelivered event: ")) {
			continue
		}
		if bytes.Count(b, []byte("\n")) != 1 || b[len(b)-1] != '\n' {
			t.Fatalf("non-atomic event line %q", b)
		}
		var wire struct {
			Time      string          `json:"time"`
			Service   string          `json:"service"`
			Name      string          `json:"event"`
			RequestID string          `json:"request_id"`
			User      string          `json:"user"`
			Attrs     telemetry.Attrs `json:"attrs"`
		}
		if e := json.Unmarshal(bytes.TrimSuffix(bytes.TrimPrefix(b, []byte("sites: undelivered event: ")), []byte("\n")), &wire); e != nil {
			t.Fatal(e)
		}
		stamp, e := time.Parse("2006-01-02T15:04:05.000000Z", wire.Time)
		if e != nil {
			t.Fatal(e)
		}
		event := telemetry.Event{Time: stamp, Service: wire.Service, Name: wire.Name, RequestID: wire.RequestID, User: wire.User, Attrs: wire.Attrs}
		canonical, e := event.MarshalJSON()
		if e != nil {
			t.Fatal(e)
		}
		if string(b) != "sites: undelivered event: "+string(canonical)+"\n" {
			t.Fatalf("noncanonical event %q", b)
		}
		events = append(events, event)
	}
	return events
}
func lifeStopping(t *testing.T, events []telemetry.Event, reason string) {
	t.Helper()
	count := 0
	for i, e := range events {
		if e.Name == "service.stopping" {
			count++
			if i != len(events)-1 || e.RequestID != "" || e.User != "" || len(e.Attrs) != 1 || e.Attrs["reason"] != reason {
				t.Fatalf("stopping event/order=%#v index=%d/%d", e, i, len(events))
			}
		}
	}
	if count != 1 {
		t.Fatalf("stopping count=%d events=%v", count, events)
	}
}

// R-SGCR-8UGJ R-WA2F-R8IX
func TestRunLifecycleIdleStopAndEventOrder(t *testing.T) {
	for _, drain := range []string{"1", "999999999999999999999999999"} {
		t.Run(drain, func(t *testing.T) {
			f := newStartFixture(t)
			f.set("DRAIN_SECONDS", drain)
			f.start(t)
			res := lifeGet(t, f, "/about", "completed", "owner")
			_ = lifeRead(t, res)
			select {
			case code := <-f.done:
				t.Fatalf("Run returned while active context: %d", code)
			default:
			}
			started := time.Now()
			f.cancel(errors.New("precise shutdown reason"))
			if lifeExit(t, f) != cli.ExitSuccess {
				t.Fatal(f.err.text())
			}
			if time.Since(started) >= time.Second {
				t.Fatal("idle run waited for drain deadline")
			}
			events := f.capture.Events()
			lifeStopping(t, events, "precise shutdown reason")
			finished := false
			for _, e := range events {
				if e.Name == "request.finished" && e.RequestID == "completed" {
					finished = true
				}
			}
			if !finished {
				t.Fatal("completed request missing finish before stopping")
			}
			if f.err.text() != "" || f.out.text() != "" {
				t.Fatal("idle shutdown wrote output")
			}
		})
	}
}

// R-W0B8-P2LD
func TestRunLifecyclePublishCompletesWithinDrain(t *testing.T) {
	f := newStartFixture(t)
	l := lifeObserve(f)
	f.set("DRAIN_SECONDS", "2")
	root := t.TempDir()
	sha := startRepo(t, f, root)
	f.set("REPOS_DIR", root)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var enabled atomic.Bool
	f.p.After = func(time.Duration) <-chan time.Time {
		if enabled.Load() {
			once.Do(func() { close(entered); <-release })
		}
		return make(chan time.Time)
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	f.start(t)
	lifeSite(t, f, "blog")
	enabled.Store(true)
	result := make(chan struct {
		r mcp.Result
		e error
	}, 1)
	go func() {
		r, e := lifeCall(context.Background(), f, "drain-complete", "publish", `{"name":"blog"}`)
		result <- struct {
			r mcp.Result
			e error
		}{r, e}
	}()
	lifeWait(t, entered)
	f.cancel(errors.New("publish signal"))
	lifeWait(t, l.closed)
	close(release)
	var answer struct {
		r mcp.Result
		e error
	}
	select {
	case answer = <-result:
	case <-time.After(3 * time.Second):
		t.Fatal("publish response missing")
	}
	if answer.e != nil || answer.r.IsError() {
		t.Fatalf("publish=%#v %v", answer.r, answer.e)
	}
	var site map[string]any
	if e := json.Unmarshal(startStructured(t, answer.r), &site); e != nil {
		t.Fatal(e)
	}
	if site["commit"] != sha {
		t.Fatal(site)
	}
	if lifeExit(t, f) != cli.ExitSuccess {
		t.Fatal(f.err.text())
	}
	events := f.capture.Events()
	pub, finish := false, false
	for _, e := range events {
		if e.RequestID == "drain-complete" && e.Name == "site.published" {
			pub = true
		}
		if e.RequestID == "drain-complete" && e.Name == "request.finished" && e.Attrs["status"] == int64(200) {
			finish = true
		}
	}
	if !pub || !finish {
		t.Fatalf("publish/finish=%v/%v events=%v", pub, finish, events)
	}
}

// R-WBAC-509M
func TestRunLifecycleUndeliveredLines(t *testing.T) {
	f := newStartFixture(t)
	w := &lifeWrites{changed: make(chan struct{}, 32)}
	f.p.Stderr = w
	f.p.Sleep = func(context.Context, time.Duration) {}
	f.p.Sink = lifeSink(func(context.Context, telemetry.Event) error { return errors.New("delivery unavailable") })
	f.start(t)
	for len(w.snapshot()) == 0 {
		lifeWait(t, w.changed)
	}
	first := lifeEventLines(t, w)
	if len(first) != 1 || first[0].Name != "service.started" {
		t.Fatal(first)
	}
	res := lifeGet(t, f, "/about", "failed-delivery-request", "owner")
	_ = lifeRead(t, res)
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	if f.stop(t) != cli.ExitSuccess {
		t.Fatal(w.text())
	}
	events := lifeEventLines(t, w)
	if len(events) != 4 {
		t.Fatalf("events=%v", events)
	}
	for i, name := range []string{"request.started", "request.finished"} {
		e := events[i+1]
		if e.Name != name || e.RequestID != "failed-delivery-request" || e.User != "owner" {
			t.Fatal(e)
		}
	}
	lifeStopping(t, events, "test stop")
	if f.out.text() != "" {
		t.Fatal("stdout written")
	}
}

func lifeFIFO(t *testing.T, f *startFixture) string {
	t.Helper()
	p := filepath.Join(f.p.Dir, "blocked-git-trace")
	if e := syscall.Mkfifo(p, 0600); e != nil {
		t.Fatal(e)
	}
	f.set("GIT_TRACE", p)
	return p
}
func lifeProcesses(t *testing.T, marker string) []int {
	t.Helper()
	entries, e := os.ReadDir("/proc")
	if e != nil {
		t.Fatal(e)
	}
	var pids []int
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil {
			continue
		}
		b, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
		if e != nil {
			continue
		}
		for _, item := range bytes.Split(b, []byte{0}) {
			if string(item) == "GIT_TRACE="+marker {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}
func lifeWaitProcesses(t *testing.T, marker string, count int) []int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		pids := lifeProcesses(t, marker)
		if len(pids) == count {
			return pids
		}
		if time.Now().After(deadline) {
			t.Fatalf("git processes=%v want %d", pids, count)
		}
		runtime.Gosched()
	}
}
func lifeGone(t *testing.T, marker string) {
	t.Helper()
	if pids := lifeProcesses(t, marker); len(pids) != 0 {
		t.Fatalf("git processes survived Run return: %v", pids)
	}
}

// R-SCP2-3J8G
func TestRunLifecycleCanceledPublishEndsHandler(t *testing.T) {
	f := newStartFixture(t)
	f.set("DRAIN_SECONDS", "30")
	root := t.TempDir()
	startRepo(t, f, root)
	f.set("REPOS_DIR", root)
	var enabled atomic.Bool
	started := make(chan struct{}, 1)
	f.p.After = func(time.Duration) <-chan time.Time {
		if enabled.Load() {
			started <- struct{}{}
		}
		return make(chan time.Time)
	}
	f.start(t)
	lifeSite(t, f, "blog")
	marker := lifeFIFO(t, f)
	enabled.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	answered := make(chan error, 1)
	go func() { _, e := lifeCall(ctx, f, "canceled-publish", "publish", `{"name":"blog"}`); answered <- e }()
	lifeWait(t, started)
	lifeWaitProcesses(t, marker, 1)
	cancel()
	select {
	case e := <-answered:
		if !errors.Is(e, context.Canceled) {
			t.Fatalf("client error=%v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("client cancellation did not return")
	}
	begin := time.Now()
	f.cancel(errors.New("stop after client cancellation"))
	if lifeExit(t, f) != cli.ExitSuccess {
		t.Fatal(f.err.text())
	}
	if time.Since(begin) >= time.Second {
		t.Fatal("Goexit handler counted as unfinished")
	}
	if strings.Contains(f.err.text(), "stopped with") {
		t.Fatal(f.err.text())
	}
	lifeGone(t, marker)
}

// R-W2R1-GM2R R-V5RR-42IV R-WCI8-IS0B R-BH93-MQG1
func TestRunLifecycleCutoffStopsGitAndSuppressesEvents(t *testing.T) {
	f := newStartFixture(t)
	f.p.Rand = &lifeRand{}
	f.set("DRAIN_SECONDS", "1")
	l := lifeObserve(f)
	w := &lifeWrites{changed: make(chan struct{}, 128)}
	f.p.Stderr = w
	var cancelAt atomic.Int64
	var closedBeforeStopping atomic.Bool
	l.onClose = func() {
		n := cancelAt.Load()
		if n != 0 && time.Since(time.Unix(0, n)) >= time.Second && !strings.Contains(w.text(), `"event":"service.stopping"`) {
			closedBeforeStopping.Store(true)
		}
	}
	var seenMu sync.Mutex
	var seen []telemetry.Event
	var badDelivery atomic.Bool
	ids := map[string]bool{"cut-publish": true, "cut-rebuild": true, "cut-file": true}
	f.p.Sink = lifeSink(func(ctx context.Context, e telemetry.Event) error {
		seenMu.Lock()
		seen = append(seen, e)
		seenMu.Unlock()
		if e.Name == "service.stopping" || e.Name == "request.finished" && ids[e.RequestID] {
			if ctx.Err() == nil {
				badDelivery.Store(true)
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return f.capture.Deliver(ctx, e)
	})
	root := t.TempDir()
	startRepo(t, f, root)
	f.set("REPOS_DIR", root)
	lifeLargeRepo(t, f, root)
	var enabled atomic.Bool
	started := make(chan struct{}, 8)
	f.p.After = func(time.Duration) <-chan time.Time {
		if enabled.Load() {
			started <- struct{}{}
		}
		return make(chan time.Time)
	}
	f.start(t)
	lifeSite(t, f, "unpublished")
	rebuild := lifeSite(t, f, "rebuild")
	f.call(t, "publish", `{"name":"rebuild"}`)
	lifeSite(t, f, "large")
	f.call(t, "publish", `{"name":"large"}`)
	if e := os.RemoveAll(filepath.Join(f.p.Dir, "cache/sites", rebuild["id"].(string))); e != nil {
		t.Fatal(e)
	}
	marker := lifeFIFO(t, f)
	enabled.Store(true)
	publishDone := make(chan struct{})
	go func() {
		_, _ = lifeCall(context.Background(), f, "cut-publish", "publish", `{"name":"unpublished"}`)
		close(publishDone)
	}()
	viewDone := make(chan struct{})
	go func() {
		r, _ := http.NewRequest(http.MethodGet, "http://sites/rebuild/", nil)
		r.Header.Set("X-Request-Id", "cut-rebuild")
		res, e := f.http.Do(r)
		if e == nil {
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		}
		close(viewDone)
	}()
	lifeWait(t, started)
	lifeWait(t, started)
	fds := lifePidfds(t, lifeWaitProcesses(t, marker, 2))
	dialer := net.Dialer{Timeout: time.Second, Control: func(_, _ string, c syscall.RawConn) error {
		var result error
		e := c.Control(func(fd uintptr) { result = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 4096) })
		if e != nil {
			return e
		}
		return result
	}}
	c, e := dialer.DialContext(context.Background(), "tcp", f.ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(4 * time.Second))
	if _, e = io.WriteString(c, "GET /large/large.bin HTTP/1.1\r\nHost: sites\r\nX-Request-Id: cut-file\r\n\r\n"); e != nil {
		t.Fatal(e)
	}
	res, e := http.ReadResponse(bufio.NewReader(c), nil)
	if e != nil {
		t.Fatal(e)
	}
	if res.StatusCode != 200 || res.ContentLength != 16<<20 {
		t.Fatalf("backpressure fixture: status=%d size=%d", res.StatusCode, res.ContentLength)
	}
	begin := time.Now()
	cancelAt.Store(begin.UnixNano())
	f.cancel(errors.New("deadline signal"))
	code := lifeExit(t, f)
	lifeExited(t, fds)
	lifeGone(t, marker)
	elapsed := time.Since(begin)
	if code != cli.ExitServerFailed || elapsed < time.Second || elapsed >= 2*time.Second {
		t.Fatalf("code=%d elapsed=%v stderr=%s", code, elapsed, w.text())
	}
	chunks := w.snapshot()
	want := "sites: " + (&server.DrainError{Unfinished: 3}).Error() + "\n"
	if len(chunks) == 0 || string(chunks[len(chunks)-1]) != want || f.out.text() != "" {
		t.Fatalf("final writes=%q stdout=%q", chunks, f.out.text())
	}
	body, bodyErr := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if bodyErr == nil || len(body) >= 16<<20 {
		t.Fatalf("cutoff delivered complete response: bytes=%d error=%v", len(body), bodyErr)
	}
	lifeWait(t, publishDone)
	lifeWait(t, viewDone)
	if closedBeforeStopping.Load() || badDelivery.Load() {
		t.Fatalf("stopping close/delivery order violated: close=%v delivery=%v", closedBeforeStopping.Load(), badDelivery.Load())
	}
	seenMu.Lock()
	events := append([]telemetry.Event(nil), seen...)
	seenMu.Unlock()
	events = append(events, lifeEventLines(t, w)...)
	for _, event := range events {
		if ids[event.RequestID] && (event.Name == "tool.called" || strings.HasPrefix(event.Name, "site.")) {
			t.Fatalf("cut-off side effect=%#v", event)
		}
	}
	stopping := 0
	for _, event := range lifeEventLines(t, w) {
		if event.Name == "service.stopping" {
			stopping++
		}
	}
	if stopping != 1 {
		t.Fatalf("stopping fallback count=%d stderr=%s", stopping, w.text())
	}
}

type lifeTemporary struct{}

func (lifeTemporary) Error() string   { return "temporary accept error" }
func (lifeTemporary) Timeout() bool   { return false }
func (lifeTemporary) Temporary() bool { return true }

type lifeFailListener struct {
	net.Listener
	calls   atomic.Int64
	failure error
}

func (l *lifeFailListener) Accept() (net.Conn, error) {
	if l.calls.Add(1) == 1 {
		return nil, lifeTemporary{}
	}
	return nil, l.failure
}

// R-W6EQ-LXAU
func TestRunLifecycleAcceptFailure(t *testing.T) {
	f := newStartFixture(t)
	l := &lifeFailListener{Listener: f.ln, failure: errors.New("listener permanently failed")}
	f.ln = l
	w := &lifeWrites{changed: make(chan struct{}, 8)}
	f.p.Stderr = w
	f.start(t)
	if lifeExit(t, f) != cli.ExitServerFailed || l.calls.Load() != 2 || f.out.text() != "" {
		t.Fatalf("calls=%d stdout=%q", l.calls.Load(), f.out.text())
	}
	chunks := w.snapshot()
	if len(chunks) == 0 || string(chunks[len(chunks)-1]) != "sites: listener permanently failed\n" {
		t.Fatalf("last diagnostic=%q", chunks)
	}
}

// R-JETU-2OFJ
func TestRunLifecycleQuietResponses(t *testing.T) {
	f := newStartFixture(t)
	root := t.TempDir()
	startRepo(t, f, root)
	f.set("REPOS_DIR", root)
	f.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	f.start(t)
	lifeSite(t, f, "blog")
	f.call(t, "publish", `{"name":"blog"}`)
	f.call(t, "apex", `{"name":"blog"}`)
	for _, path := range []string{"/blog/", "/nope"} {
		res := lifeGet(t, f, path, "quiet"+path, "")
		_ = lifeRead(t, res)
	}
	req, _ := http.NewRequest(http.MethodPost, "http://sites/mcp", strings.NewReader("invalid"))
	res, e := f.http.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	_ = lifeRead(t, res)
	if res.StatusCode != 500 {
		t.Fatal(res.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, "http://sites/anything", nil)
	req.Host = "space.example"
	res, e = f.http.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	_ = lifeRead(t, res)
	if res.StatusCode != 302 {
		t.Fatal(res.StatusCode)
	}
	r, e := lifeCall(context.Background(), f, "quiet-refusal", "show", `{"name":"absent"}`)
	if e != nil || !r.IsError() {
		t.Fatalf("refusal=%#v %v", r, e)
	}
	if f.stop(t) != cli.ExitSuccess || f.err.text() != "" || f.out.text() != "" {
		t.Fatalf("stderr=%q stdout=%q", f.err.text(), f.out.text())
	}
}

func lifeLargeRepo(t *testing.T, f *startFixture, root string) {
	t.Helper()
	repo := filepath.Join(root, "rep_0123456789abcdef.git")
	run := func(input io.Reader, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		f.mu.Lock()
		bin := filepath.Join(f.env["PATH"], "git")
		f.mu.Unlock()
		cmd := exec.CommandContext(ctx, bin, append([]string{"--git-dir=" + repo}, args...)...)
		cmd.Env = append(f.p.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid", "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z")
		cmd.Stdin = input
		b, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git fixture %v: %s %v", args, b, e)
		}
		return strings.TrimSpace(string(b))
	}
	index := run(strings.NewReader("hello site"), "hash-object", "-w", "--stdin")
	large := run(bytes.NewReader(bytes.Repeat([]byte("x"), 16<<20)), "hash-object", "-w", "--stdin")
	tree := run(strings.NewReader("100644 blob "+index+"\tindex.html\n100644 blob "+large+"\tlarge.bin\n"), "mktree")
	sha := run(strings.NewReader("large fixture\n"), "commit-tree", tree)
	run(nil, "update-ref", "refs/heads/main", sha)
}

// R-WA2F-R8IX R-SGCR-8UGJ
func TestRunLifecyclePendingTelemetryFlushedAtDeadline(t *testing.T) {
	f := newStartFixture(t)
	f.set("DRAIN_SECONDS", "1")
	w := &lifeWrites{changed: make(chan struct{}, 16)}
	f.p.Stderr = w
	entered := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var handed []telemetry.Event
	f.p.Sink = lifeSink(func(ctx context.Context, event telemetry.Event) error {
		mu.Lock()
		handed = append(handed, event)
		mu.Unlock()
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return ctx.Err()
	})
	f.start(t)
	lifeWait(t, entered)
	res := lifeGet(t, f, "/about", "pending-trail", "owner")
	_ = lifeRead(t, res)
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	started := time.Now()
	f.cancel(errors.New("flush deadline"))
	if lifeExit(t, f) != cli.ExitSuccess {
		t.Fatal(w.text())
	}
	elapsed := time.Since(started)
	if elapsed < time.Second || elapsed >= 2*time.Second {
		t.Fatalf("telemetry drain elapsed=%v", elapsed)
	}
	events := lifeEventLines(t, w)
	if len(events) != 4 {
		t.Fatalf("fallback events=%v", events)
	}
	for i, name := range []string{"service.started", "request.started", "request.finished", "service.stopping"} {
		if events[i].Name != name {
			t.Fatalf("fallback order=%v", events)
		}
	}
	lifeStopping(t, events, "flush deadline")
	mu.Lock()
	delivered := append([]telemetry.Event(nil), handed...)
	mu.Unlock()
	for i, event := range delivered {
		if event.Name == "service.stopping" && i != len(delivered)-1 {
			t.Fatalf("event delivered after stopping: %v", delivered)
		}
	}
	if len(w.snapshot()) != len(events) || f.out.text() != "" {
		t.Fatalf("unexpected diagnostics=%q", w.text())
	}
}

func lifePidfds(t *testing.T, pids []int) []int {
	t.Helper()
	var fds []int
	for _, pid := range pids {
		fd, _, errno := syscall.Syscall(434, uintptr(pid), 0, 0)
		if errno != 0 {
			t.Fatal(errno)
		}
		fds = append(fds, int(fd))
	}
	t.Cleanup(func() {
		for _, fd := range fds {
			_ = syscall.Close(fd)
		}
	})
	return fds
}
func lifeExited(t *testing.T, fds []int) {
	t.Helper()
	for _, fd := range fds {
		var ready syscall.FdSet
		if fd >= len(ready.Bits)*64 {
			t.Fatal("pidfd exceeds select range")
		}
		ready.Bits[fd/64] |= int64(1) << uint(fd%64)
		timeout := syscall.Timeval{}
		n, e := syscall.Select(fd+1, &ready, nil, nil, &timeout)
		if e != nil || n != 1 {
			t.Fatalf("git alive at Run return: pidfd=%d readiness=%d error=%v", fd, n, e)
		}
	}
}
