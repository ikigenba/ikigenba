package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	"github.com/ikigenba/ikigenba/repos/internal/server"
)

// R-Q9TK-A3F3 R-QB1G-NV5S R-3TO0-YS59 R-QJKR-C9CN R-4FLE-JOQW
func TestServeGracefulPushAndImmediateQueuedDrain(t *testing.T) {
	for _, duration := range []string{"1", "9999999999999999999999999"} {
		t.Run(duration, func(t *testing.T) {
			f := newServeFixture(t)
			serveHeldPushTrace(t, f)
			f.set("DRAIN_SECONDS", duration)
			marker := "SERVE_DRAIN_MARKER=" + f.dir
			f.gitEnv = append(f.gitEnv, marker)
			f.start(t)
			takeServeTimer(t, f, 24*time.Hour)
			created := f.tool(t, "create", `{"name":"alpha"}`)
			sha, pack := servePack(t, f)
			body := servePushBody(sha, pack)
			first := startServePush(t, f, "finishing", body, true)
			takeServeTimer(t, f, 600*time.Second)
			awaitServePushStarted(t, first)
			if len(serveProcesses(t, marker)) == 0 {
				t.Fatal("held real git not found by test environment marker")
			}
			second := startServePush(t, f, "waiting", body, false)
			takeServeTimer(t, f, 30*time.Second)
			select {
			case code := <-f.result:
				f.stopped = true
				t.Fatalf("Run returned before cancellation: %d", code)
			default:
			}
			cancelled := time.Now()
			f.cancel(errors.New("graceful test"))
			rejected := finishServePush(t, second)
			if rejected.err != nil || rejected.status != 503 || string(rejected.body) != "repos is stopping; try again later\n" || rejected.headers.Get("Retry-After") != "30" {
				t.Fatalf("queued drain response %+v", rejected)
			}
			// The queued request was refused while the first request remains in flight.
			select {
			case r := <-first.done:
				t.Fatalf("running push ended before completion: %+v", r)
			default:
			}
			select {
			case code := <-f.result:
				f.stopped = true
				t.Fatalf("Run returned while push in progress: %d", code)
			default:
			}
			if _, err := first.writer.Write(first.rest); err != nil {
				t.Fatal(err)
			}
			if err := first.writer.Close(); err != nil {
				t.Fatal(err)
			}
			complete := finishServePush(t, first)
			if complete.err != nil || complete.status != 200 || !bytes.Contains(complete.body, []byte("unpack ok")) {
				t.Fatalf("graceful push %+v", complete)
			}
			f.stop(t, "graceful test", cli.ExitSuccess)
			if elapsed := time.Since(cancelled); elapsed >= time.Second {
				t.Fatalf("finished push waited for drain deadline: %s", elapsed)
			}
			if processes := serveProcesses(t, marker); len(processes) != 0 {
				t.Fatalf("git remains after return: %v", processes)
			}
			refs := serveGit(t, f, f.dir, nil, "--git-dir="+filepath.Join(f.dir, "state", "repos", created["id"].(string)+".git"), "show-ref", "refs/heads/main")
			if !bytes.HasPrefix(refs, []byte(sha+" ")) {
				t.Fatalf("push ref %s", refs)
			}
			events := f.capture.Events()
			pushed, finished, stopping := -1, -1, -1
			for i, e := range events {
				if e.Name == "repo.pushed" && e.RequestID == "finishing" {
					pushed = i
				}
				if e.Name == "request.finished" && e.RequestID == "finishing" {
					finished = i
					if e.Attrs["status"] != int64(200) {
						t.Fatalf("completed push status %+v", e)
					}
				}
				if e.Name == "service.stopping" {
					if stopping != -1 {
						t.Fatal("multiple stopping events")
					}
					stopping = i
					if e.RequestID != "" || e.User != "" || e.Attrs["reason"] != "graceful test" || len(e.Attrs) != 1 {
						t.Fatalf("stop envelope %+v", e)
					}
				}
			}
			if pushed < 0 || finished <= pushed || stopping <= finished || stopping != len(events)-1 {
				t.Fatalf("push/finished/stopping order %+v", events)
			}
			if f.stderr.text() != "" || f.stdout.text() != "" {
				t.Fatalf("successful drain output %q / %q", f.stdout.text(), f.stderr.text())
			}
		})
	}
}

// serveProcesses observes only the unique test marker, without exposing any
// other process's environment. A vanished process and an unreadable unrelated
// process cannot contain evidence the test can use.
func serveProcesses(t *testing.T, marker string) []string {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		env, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
		if err != nil {
			continue
		}
		for _, value := range bytes.Split(env, []byte{0}) {
			if string(value) == marker {
				found = append(found, entry.Name())
				break
			}
		}
	}
	return found
}

type drainClose struct {
	at     time.Time
	stderr string
}
type drainConnection struct {
	net.Conn
	mu       sync.Mutex
	prefix   []byte
	observed chan drainClose
	stderr   *serveOutput
}

func (c *drainConnection) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.mu.Lock()
	if len(c.prefix) < 32768 {
		c.prefix = append(c.prefix, p[:n]...)
	}
	c.mu.Unlock()
	return n, err
}
func (c *drainConnection) Close() error {
	c.mu.Lock()
	cut := bytes.Contains(bytes.ToLower(c.prefix), []byte("x-request-id: cut-off\r\n"))
	c.mu.Unlock()
	if cut {
		select {
		case c.observed <- drainClose{time.Now(), c.stderr.text()}:
		default:
		}
	}
	return c.Conn.Close()
}

// R-QC9D-1MWH R-QM0K-3SU1 R-QJKR-C9CN R-QPO9-9424 R-4FLE-JOQW
func TestServeDeadlineFallbackBeforeClosingAndGitCleanup(t *testing.T) {
	f := newServeFixture(t)
	serveHeldPushTrace(t, f)
	marker := "SERVE_CUT_MARKER=" + f.dir
	f.gitEnv = append(f.gitEnv, marker)
	var deliveriesMu sync.Mutex
	var deliveries []struct {
		name, id string
		done     bool
	}
	entered := make(chan struct{})
	var once sync.Once
	release := make(chan struct{})
	f.p.Sink = serveSink(t, func(ctx context.Context, e telemetry.Event) error {
		deliveriesMu.Lock()
		deliveries = append(deliveries, struct {
			name, id string
			done     bool
		}{e.Name, e.RequestID, ctx.Err() != nil})
		deliveriesMu.Unlock()
		once.Do(func() { close(entered) })
		// Ignore cancellation deliberately until the test releases the sink. The
		// writer must still finish the run and record fallback without this return.
		<-release
		return errors.New("held telemetry failed")
	})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	f.notification(t)
	f.listen(t)
	base := f.listener
	closed := make(chan drainClose, 8)
	f.p.Inherit = func(uintptr) (net.Listener, error) {
		return &serveListener{Listener: base, accept: func() (net.Conn, error) {
			c, err := base.Accept()
			if err != nil {
				return nil, err
			}
			return &drainConnection{Conn: c, observed: closed, stderr: f.stderr}, nil
		}}, nil
	}
	f.launch(t)
	f.ready(t)
	takeServeTimer(t, f, 24*time.Hour)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("sink not entered")
	}
	created := f.tool(t, "create", `{"name":"alpha"}`)
	sha, pack := servePack(t, f)
	first := startServePush(t, f, "cut-off", servePushBody(sha, pack), true)
	takeServeTimer(t, f, 600*time.Second)
	awaitServePushStarted(t, first)
	if len(serveProcesses(t, marker)) == 0 {
		t.Fatal("held git process not observed")
	}
	cancelled := time.Now()
	f.cancel(errors.New("deadline test"))
	f.stop(t, "deadline test", cli.ExitServerFailed)
	elapsed := time.Since(cancelled)
	if elapsed < time.Second || elapsed >= 2*time.Second {
		t.Fatalf("drain return %s outside [1s,2s)", elapsed)
	}
	response := finishServePush(t, first)
	if response.err == nil {
		t.Fatalf("cut-off push received complete body %q", response.body)
	}
	if processes := serveProcesses(t, marker); len(processes) != 0 {
		t.Fatalf("git remains after deadline return: %v", processes)
	}
	diagnostics := f.stderr.lines()
	last := "repos: " + (&server.DrainError{Unfinished: 1}).Error() + "\n"
	if len(diagnostics) == 0 || diagnostics[len(diagnostics)-1] != last || f.stdout.text() != "" {
		t.Fatalf("drain diagnostics %q", f.stderr.text())
	}
	var fallback []struct {
		Name  string         `json:"event"`
		ID    string         `json:"request_id"`
		User  string         `json:"user"`
		Attrs map[string]any `json:"attrs"`
	}
	for _, line := range diagnostics[:len(diagnostics)-1] {
		var e struct {
			Name  string         `json:"event"`
			ID    string         `json:"request_id"`
			User  string         `json:"user"`
			Attrs map[string]any `json:"attrs"`
		}
		if !strings.HasPrefix(line, "repos: undelivered event: ") || json.Unmarshal([]byte(strings.TrimPrefix(line, "repos: undelivered event: ")), &e) != nil {
			t.Fatalf("unexpected output %q", line)
		}
		fallback = append(fallback, e)
	}
	start, stop, cutStarted := 0, 0, 0
	for _, e := range fallback {
		if e.Name == "service.started" {
			start++
		}
		if e.Name == "service.stopping" {
			stop++
			if e.ID != "" || e.User != "" || len(e.Attrs) != 1 || e.Attrs["reason"] != "deadline test" {
				t.Fatalf("stop fallback %+v", e)
			}
		}
		if e.Name == "request.started" && e.ID == "cut-off" {
			cutStarted++
		}
	}
	if start != 1 || stop != 1 || cutStarted != 1 {
		t.Fatalf("missing/repeated fallback events %+v", fallback)
	}
	select {
	case observation := <-closed:
		if observation.at.Sub(cancelled) < time.Second || !strings.Contains(observation.stderr, `"event":"service.stopping"`) {
			t.Fatalf("connection closed before stop fallback: %+v", observation)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cut-off connection closure not observed")
	}
	deliveriesMu.Lock()
	for _, d := range deliveries {
		if (d.name == "service.stopping" || (d.name == "request.finished" && d.id == "cut-off")) && !d.done {
			t.Errorf("deadline event handed active context: %+v", d)
		}
	}
	deliveriesMu.Unlock()
	// Releasing an asynchronous delivery after return must produce no Write.
	before := f.stderr.text()
	close(release)
	if err := first.writer.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	f.writer.Load().Emit(context.Background(), "request.finished", telemetry.Attrs{"status": int64(200)})
	if f.stderr.text() != before || f.stderr.late.Load() || f.stderr.overlap.Load() {
		t.Fatalf("stderr changed or overlapped after return")
	}
	refs := serveGit(t, f, f.dir, nil, "--git-dir="+filepath.Join(f.dir, "state", "repos", created["id"].(string)+".git"), "for-each-ref")
	if len(refs) != 0 {
		t.Fatalf("partial pack changed refs: %s", refs)
	}
}

// R-3TO0-YS59 R-QJKR-C9CN
func TestServeNoNotifyStaysAliveAndFinishesWithoutDeadlineWait(t *testing.T) {
	f := newServeFixture(t)
	started := make(chan struct{})
	var once sync.Once
	f.p.Sink = serveSink(t, func(ctx context.Context, e telemetry.Event) error {
		if e.Name == "service.started" {
			once.Do(func() { close(started) })
		}
		return f.capture.Deliver(ctx, e)
	})
	f.listen(t)
	f.launch(t)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("no-notify start not recorded")
	}
	if status, _, _ := f.request(t, "/", "alive"); status != 200 {
		t.Fatalf("status %d", status)
	}
	select {
	case code := <-f.result:
		f.stopped = true
		t.Fatalf("returned while serving: %d", code)
	default:
	}
	cancelled := time.Now()
	f.stop(t, "quiet stop", cli.ExitSuccess)
	if elapsed := time.Since(cancelled); elapsed >= time.Second {
		t.Fatalf("idle stop waited for deadline: %s", elapsed)
	}
	events := f.capture.Events()
	if events[len(events)-1].Name != "service.stopping" {
		t.Fatalf("last delivered event %+v", events)
	}
}

// R-QC9D-1MWH R-4FLE-JOQW
func TestServeDeadlineCountsEachUnfinishedRequest(t *testing.T) {
	f := newServeFixture(t)
	serveHeldPushTrace(t, f)
	marker := "SERVE_MANY_MARKER=" + f.dir
	f.gitEnv = append(f.gitEnv, marker)
	f.start(t)
	takeServeTimer(t, f, 24*time.Hour)
	f.tool(t, "create", `{"name":"alpha"}`)
	f.tool(t, "create", `{"name":"beta"}`)
	sha, pack := servePack(t, f)
	body := servePushBody(sha, pack)
	var pushes []*heldServePush
	for _, name := range []string{"alpha", "beta"} {
		push := startServePushNamed(t, f, name, "held-"+name, body, true)
		takeServeTimer(t, f, 600*time.Second)
		awaitServePushStarted(t, push)
		pushes = append(pushes, push)
	}
	cancelled := time.Now()
	f.cancel(errors.New("two requests"))
	f.stop(t, "two requests", cli.ExitServerFailed)
	if elapsed := time.Since(cancelled); elapsed < time.Second || elapsed >= 2*time.Second {
		t.Fatalf("two-request cutoff time %s", elapsed)
	}
	for _, push := range pushes {
		if response := finishServePush(t, push); response.err == nil {
			t.Fatalf("cut-off response complete: %q", response.body)
		}
	}
	writes := f.stderr.lines()
	if writes[len(writes)-1] != "repos: "+(&server.DrainError{Unfinished: 2}).Error()+"\n" {
		t.Fatalf("plural diagnostic %q", f.stderr.text())
	}
	if processes := serveProcesses(t, marker); len(processes) != 0 {
		t.Fatalf("unfinished git after plural stop: %v", processes)
	}
}
