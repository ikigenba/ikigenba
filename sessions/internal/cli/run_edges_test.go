package cli_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions/internal/cli"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
)

// R-2C4E-L0GG R-29OL-TGZ2
func TestRunCallsNoSibling(t *testing.T) {
	r := newRunRig(t)
	dir := shortDir(t)
	var connections atomic.Int32
	var wg sync.WaitGroup
	var listeners []net.Listener
	var entries []map[string]any
	for _, name := range []string{"auth", "telemetry", "mcp", "repos", "sites", "scripts", "events", "cron", "home", "webhooks", "prompts", "artifacts", "connections", "gdrive", "gmail", "scrapes", "candidates", "campaigns", "ledger", "accounts", "dummy"} {
		socket := filepath.Join(dir, name)
		ln, e := net.Listen("unix", socket)
		if e != nil {
			t.Fatal(e)
		}
		listeners = append(listeners, ln)
		entries = append(entries, serviceEntry(name, "https://"+name+".example.test", socket))
		wg.Go(func() {
			for {
				c, e := ln.Accept()
				if e != nil {
					return
				}
				connections.Add(1)
				_ = c.Close()
			}
		})
	}
	closeListeners := sync.OnceFunc(func() {
		for _, ln := range listeners {
			_ = ln.Close()
		}
		wg.Wait()
	})
	t.Cleanup(closeListeners)
	r.env[services.Variable] = servicesFile(t, entries)
	r.start(t)
	pushRun(t, r, "only-push", "2026-10-27T09:00:00Z", 1)
	r.ok(t, tools.ToolGetLatestTurns, tools.LatestTurnsArgs{Sessions: []tools.SessionRef{{Tool: "codex", ID: "only-push"}}})
	r.ok(t, tools.ToolListSessions, tools.ListSessionsArgs{})
	r.ok(t, tools.ToolTotalSessions, tools.TotalSessionsArgs{Period: "week", From: "2026-10-27", Group: "kind"})
	r.ok(t, tools.ToolListRules, tools.NoArgs{})
	r.ok(t, tools.ToolAddRule, tools.AddRuleArgs{Host: "fixture-host", Directory: "/fixture", Kind: "build"})
	r.ok(t, tools.ToolRemoveRule, tools.RemoveRuleArgs{Host: "fixture-host", Directory: "/fixture"})
	r.ok(t, tools.ToolListPrices, tools.NoArgs{})
	r.ok(t, tools.ToolSetPrice, tools.SetPriceArgs{Model: "fixture-model", Effective: "2026-10-01", Input: "1", Cached: "0", Output: "2", Reasoning: "0"})
	r.ok(t, tools.ToolRemovePrice, tools.RemovePriceArgs{Model: "fixture-model", Effective: "2026-10-01"})
	r.ok(t, tools.ToolGetAwayLimit, tools.NoArgs{})
	r.ok(t, tools.ToolSetAwayLimit, tools.SetAwayLimitArgs{Minutes: 20})
	for _, path := range []string{"/", "/tools", "/about"} {
		if status, _, _ := r.request(t, path, true); status != 200 {
			t.Fatal(status)
		}
	}
	if r.stop(t) != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	closeListeners()
	if n := connections.Load(); n != 0 {
		t.Fatalf("sibling connections %d", n)
	}
}

// R-1ZXE-RB1I
func TestRunNilDefaults(t *testing.T) {
	r := newRunRig(t)
	dir := t.TempDir()
	t.Chdir(dir)
	r.p.Dir = ""
	r.p.Unsetenv = nil
	r.p.Sink = nil
	socket := filepath.Join(shortDir(t), "telemetry")
	ln, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	sink := new(telemetry.Capture)
	srv := &http.Server{Handler: telemetry.IngestHandler(sink), ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close(); <-done })
	path := servicesFile(t, []map[string]any{serviceEntry("telemetry", "https://telemetry.example.test", socket)})
	t.Setenv(services.Variable, path)
	r.env[services.Variable] = path
	r.start(t)
	pushRun(t, r, "defaults", "2026-10-27T09:00:00Z", 1)
	if r.stop(t) != cli.ExitSuccess {
		t.Fatal("stop failed")
	}
	assertRunTree(t, dir)
	events := sink.Events()
	if len(events) < 3 || events[0].Name != "service.started" || events[len(events)-1].Name != "service.stopping" {
		t.Fatalf("socket sink events %+v", events)
	}
	if r.errout.String() != "" {
		t.Fatal(r.errout.String())
	}
}

type failedListener struct{ closed atomic.Bool }

func (*failedListener) Accept() (net.Conn, error) {
	return nil, errors.New("listener gone\nsecond line")
}
func (l *failedListener) Close() error { l.closed.Store(true); return nil }
func (*failedListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

// R-VE0R-SFRN
func TestRunListenerFailureIsSilentAfterReturn(t *testing.T) {
	r := newRunRig(t)
	ln := &failedListener{}
	r.p.Inherit = func(uintptr) (net.Listener, error) { return ln, nil }
	code := cli.Run(context.Background(), r.p)
	assertCode(t, code)
	before := r.errout.String()
	if code != cli.ExitServerFailed || r.out.String() != "" || r.errout.calls != 1 || !strings.HasPrefix(before, "sessions: ") || !strings.Contains(before, "listener gone second line") || strings.Count(before, "\n") != 1 || !ln.closed.Load() {
		t.Fatalf("code %d diagnostic %q", code, before)
	}
	r.writer.Emit(context.Background(), "test.after", nil)
	if r.errout.String() != before {
		t.Fatal("diagnostic after return")
	}
}

type blockingSink struct {
	entered chan struct{}
	release chan struct{}
	exited  chan struct{}
	calls   atomic.Int32
	once    sync.Once
}

func (s *blockingSink) Deliver(context.Context, telemetry.Event) error {
	s.calls.Add(1)
	s.once.Do(func() { close(s.entered) })
	<-s.release
	close(s.exited)
	return nil
}

// R-VHOG-XQZQ
func TestRunBoundsBlockedSinkAndDeliversCleanQueue(t *testing.T) {
	t.Run("blocked sink", func(t *testing.T) {
		r := newRunRig(t)
		sink := &blockingSink{entered: make(chan struct{}), release: make(chan struct{}), exited: make(chan struct{})}
		r.p.Sink = sink
		r.start(t)
		select {
		case <-sink.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("sink not called")
		}
		r.request(t, "/about", true)
		start := time.Now()
		if code := r.stop(t); code != cli.ExitSuccess {
			t.Fatalf("code %d", code)
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("blocked sink delayed Run %s", elapsed)
		}
		before := r.errout.String()
		calls := sink.calls.Load()
		r.writer.Emit(context.Background(), "test.after", nil)
		close(sink.release)
		select {
		case <-sink.exited:
		case <-time.After(3 * time.Second):
			t.Fatal("sink did not leave")
		}
		if sink.calls.Load() != calls || r.errout.String() != before {
			t.Fatalf("post-stop delivery or diagnostics %q -> %q", before, r.errout.String())
		}
	})
	t.Run("clean queue", func(t *testing.T) {
		r := newRunRig(t)
		r.start(t)
		for i := range 50 {
			r.writer.Emit(context.Background(), "test.queued", telemetry.Attrs{"sequence": i})
		}
		if r.stop(t) != cli.ExitSuccess {
			t.Fatal("stop failed")
		}
		count := 0
		for _, event := range r.capture.Events() {
			if event.Name == "test.queued" {
				count++
			}
		}
		if count != 50 {
			t.Fatalf("delivered %d of 50", count)
		}
	})
}

// R-RPAF-XFBU R-XJOJ-AMJO
func TestRunCutsOffUnfinishedBodies(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(decimalBool(blocked), func(t *testing.T) {
			r := newRunRig(t)
			var sink *blockingSink
			if blocked {
				sink = &blockingSink{entered: make(chan struct{}), release: make(chan struct{}), exited: make(chan struct{})}
				r.p.Sink = sink
				t.Cleanup(func() { close(sink.release); <-sink.exited })
			}
			r.start(t)
			// A ready HTTP response on each connection proves its handler is in the
			// body read: Expect causes net/http to send 100 Continue at the first read.
			var connections []net.Conn
			for range 2 {
				c, e := net.Dial("tcp", r.ln.Addr().String())
				if e != nil {
					t.Fatal(e)
				}
				connections = append(connections, c)
				t.Cleanup(func() { _ = c.Close() })
				if e := c.SetDeadline(time.Now().Add(5 * time.Second)); e != nil {
					t.Fatal(e)
				}
				_, e = io.WriteString(c, "POST /mcp HTTP/1.1\r\nHost: sessions.example.test\r\nX-User-Id: owner\r\nContent-Type: application/json\r\nContent-Length: 9999\r\nExpect: 100-continue\r\n\r\n")
				if e != nil {
					t.Fatal(e)
				}
				b := make([]byte, len("HTTP/1.1 100 Continue\r\n\r\n"))
				if _, e = io.ReadFull(c, b); e != nil {
					t.Fatal(e)
				}
				if string(b) != "HTTP/1.1 100 Continue\r\n\r\n" {
					t.Fatalf("interim %q", b)
				}
			}
			start := time.Now()
			if code := r.stop(t); code != cli.ExitServerFailed {
				t.Fatalf("code %d", code)
			}
			elapsed := time.Since(start)
			if elapsed < time.Second || elapsed > 3*time.Second {
				t.Fatalf("drain %s", elapsed)
			}
			before := r.errout.String()
			want := "sessions: stopped with 2 requests unfinished\n"
			if !strings.HasSuffix(before, want) {
				t.Fatalf("drain diagnostic %q", before)
			}
			r.writer.Emit(context.Background(), "test.after", nil)
			if r.errout.String() != before || r.out.String() != "" {
				t.Fatalf("post-return output %q/%q", r.out.String(), r.errout.String())
			}
			for _, c := range connections {
				if _, e := c.Write([]byte("{}")); e == nil {
					var b [1]byte
					if _, e = c.Read(b[:]); e == nil {
						t.Fatal("unfinished request remained live")
					}
				}
			}
		})
	}
}
func decimalBool(v bool) string {
	if v {
		return "blocked sink"
	}
	return "capture"
}

// R-CQQ4-W9Q7
func TestRunWithoutNotificationSocket(t *testing.T) {
	for _, present := range []bool{false, true} {
		r := newRunRig(t)
		if present {
			r.env["NOTIFY_SOCKET"] = ""
		} else {
			delete(r.env, "NOTIFY_SOCKET")
		}
		entered := make(chan struct{})
		ln := &signalledListener{Listener: r.ln, entered: entered}
		r.p.Inherit = func(uintptr) (net.Listener, error) { return ln, nil }
		ctx, cancel := context.WithCancelCause(context.Background())
		r.cancel = cancel
		go func() { r.done <- cli.Run(ctx, r.p) }()
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("not serving")
		}
		r.ok(t, tools.ToolListRules, tools.NoArgs{})
		r.stop(t)
		assertNoNotification(t, r.notify)
	}
}

type signalledListener struct {
	net.Listener
	entered chan struct{}
	once    sync.Once
}

func (l *signalledListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.entered) })
	return l.Listener.Accept()
}

// R-CQQ4-W9Q7
func TestRunNotifiesBeforeAcceptingQueuedConnection(t *testing.T) {
	r := newRunRig(t)
	ln := &notificationListener{Listener: r.ln, notify: r.notify, checked: make(chan error, 1)}
	r.p.Inherit = func(uintptr) (net.Listener, error) { return ln, nil }
	// A connection is waiting before Run can accept it.
	c, e := net.Dial("tcp", r.ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithCancelCause(context.Background())
	r.cancel = cancel
	go func() { r.done <- cli.Run(ctx, r.p) }()
	select {
	case e := <-ln.checked:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listener was not reached")
	}
	r.ready(t)
	r.ok(t, tools.ToolListRules, tools.NoArgs{})
	r.stop(t)
}

type notificationListener struct {
	net.Listener
	notify  *net.UnixConn
	once    sync.Once
	checked chan error
}

func (l *notificationListener) Accept() (net.Conn, error) {
	l.once.Do(func() {
		raw, e := l.notify.SyscallConn()
		if e != nil {
			l.checked <- e
			return
		}
		var b [32]byte
		var n int
		var readErr error
		e = raw.Control(func(fd uintptr) {
			n, _, readErr = syscall.Recvfrom(int(fd), b[:], syscall.MSG_PEEK|syscall.MSG_DONTWAIT)
		})
		switch {
		case e != nil:
			l.checked <- e
		case readErr != nil:
			l.checked <- readErr
		case string(b[:n]) != "READY=1":
			l.checked <- errors.New("readiness was not queued before accepting")
		default:
			l.checked <- nil
		}
	})
	return l.Listener.Accept()
}
