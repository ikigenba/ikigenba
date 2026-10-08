package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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
	"github.com/ikigenba/ikigenba/cron/internal/store"
)

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

// R-KICJ-7THE
func TestRunReadinessBeforeAccept(t *testing.T) {
	for _, kind := range []string{"filesystem", "abstract", "unset", "empty"} {
		t.Run(kind, func(t *testing.T) {
			h := newRunHarness(t, t.TempDir())
			lookup := h.p.LookupEnv
			address := h.notify.LocalAddr().String()
			if kind == "abstract" {
				var e error
				_ = h.notify.Close()
				address = "@" + filepath.Base(filepath.Dir(address))
				h.notify, e = net.ListenUnixgram("unixgram", &net.UnixAddr{Net: "unixgram", Name: address})
				if e != nil {
					t.Fatal(e)
				}
			}
			h.p.LookupEnv = func(k string) (string, bool) {
				if k == "NOTIFY_SOCKET" {
					switch kind {
					case "unset":
						return "", false
					case "empty":
						return "", true
					default:
						return address, true
					}
				}
				return lookup(k)
			}
			accepted := make(chan string, 1)
			var first sync.Once
			h.p.Inherit = func(fd uintptr) (net.Listener, error) {
				h.inherits = append(h.inherits, fd)
				return runListener{Listener: h.listener, accept: func() error {
					first.Do(func() {
						if kind == "filesystem" || kind == "abstract" {
							if e := h.notify.SetReadDeadline(time.Now().Add(time.Second)); e != nil {
								accepted <- e.Error()
								return
							}
							b := make([]byte, 32)
							n, _, e := h.notify.ReadFromUnix(b)
							if e != nil {
								accepted <- e.Error()
								return
							}
							accepted <- string(b[:n])
						} else {
							accepted <- "no notification"
						}
					})
					return nil
				}}, nil
			}
			go func() { h.done <- cli.Run(h.ctx, h.p) }()
			select {
			case got := <-accepted:
				want := "READY=1"
				if kind == "unset" || kind == "empty" {
					want = "no notification"
				}
				if got != want {
					t.Fatalf("first accept notification %q", got)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no accept")
			}
			if code, _ := h.get("/"); code != 200 {
				t.Fatal(code)
			}
			if h.stop("done") != 0 {
				t.Fatal("stop failed")
			}
			if e := h.notify.SetReadDeadline(time.Now()); e != nil {
				t.Fatal(e)
			}
			if _, _, e := h.notify.ReadFromUnix(make([]byte, 32)); e == nil {
				t.Fatal("unexpected further READY")
			}
		})
	}
}

// R-IY95-MDDG
func TestRunNotificationFailure(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	address := filepath.Join(t.TempDir(), "absent-socket")
	lookup := h.p.LookupEnv
	h.p.LookupEnv = func(k string) (string, bool) {
		if k == "NOTIFY_SOCKET" {
			return address, true
		}
		return lookup(k)
	}
	var accept atomic.Int64
	h.p.Inherit = func(uintptr) (net.Listener, error) {
		return runListener{Listener: h.listener, accept: func() error { accept.Add(1); return nil }}, nil
	}
	_, want := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
	if want == nil {
		t.Fatal("missing socket connected")
	}
	code := cli.Run(h.ctx, h.p)
	h.stopped = true
	if code != cli.ExitServerFailed || h.out.String() != "" || !reflect.DeepEqual(h.err.calls(), []string{"cron: " + want.Error() + "\n"}) || accept.Load() != 0 {
		t.Fatalf("notify failure %d %v %q", code, accept.Load(), h.err.calls())
	}
	for _, ev := range h.tc.Events() {
		if ev.Name == "service.started" {
			t.Fatal("started despite notify failure")
		}
	}
}

type runGuardRandom struct {
	active  atomic.Int64
	overlap atomic.Bool
	reads   atomic.Int64
}

func (r *runGuardRandom) Read(b []byte) (int, error) {
	if r.active.Add(1) != 1 {
		r.overlap.Store(true)
	}
	defer r.active.Add(-1)
	r.reads.Add(1)
	for i := range b {
		runtime.Gosched()
		b[i] = 0x5a
	}
	return len(b), nil
}

// R-C4FX-SNE2
func TestRunSerializesRandomDuringRequestsAndFire(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	random := &runGuardRandom{}
	h.p.Rand = random
	fired := make(chan struct{}, 1)
	h.p.EventSink = runEventSink(func(c context.Context, e events.Event) error {
		if e.Name == "cron.tick.fired" {
			fired <- struct{}{}
		}
		return h.ec.Deliver(c, e)
	})
	h.start()
	h.call("create", map[string]string{"slug": "tick", "when": "* * * * *"})
	select {
	case <-h.clock.requested:
	case <-time.After(5 * time.Second):
		t.Fatal("no timer")
	}
	start := make(chan struct{})
	var group sync.WaitGroup
	for range 20 {
		group.Go(func() { <-start; h.get("/") })
	}
	close(start)
	h.clock.advance(runTime("2026-10-05T09:33:00Z"))
	group.Wait()
	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("no fire")
	}
	if h.stop("done") != 0 {
		t.Fatal("stop failed")
	}
	if random.overlap.Load() || random.reads.Load() < 24 {
		t.Fatalf("random concurrent=%v reads=%d", random.overlap.Load(), random.reads.Load())
	}
}

type runGateRandom struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *runGateRandom) Read(b []byte) (int, error) {
	if len(b) == 8 {
		r.once.Do(func() { close(r.entered); <-r.release })
	}
	return runBytes(0x5a).Read(b)
}

// R-J6SG-ARKB
func TestRunFinishesCreateDuringDrain(t *testing.T) {
	dir := t.TempDir()
	h := newRunHarness(t, dir)
	gate := &runGateRandom{entered: make(chan struct{}), release: make(chan struct{})}
	h.p.Rand = gate
	h.start()
	result := make(chan mcp.Result, 1)
	callError := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r, e := h.client.CallTool(ctx, identity.Caller{UserID: "owner", Email: "owner@example.com", RequestID: "create-at-stop"}, "create", json.RawMessage(`{"slug":"during_drain","when":"@daily"}`))
		if e != nil {
			callError <- e
			return
		}
		result <- r
	}()
	select {
	case <-gate.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("create did not begin")
	}
	h.cancel(errors.New("SIGTERM"))
	close(gate.release)
	select {
	case r := <-result:
		if r.IsError() {
			b, e := r.MarshalJSON()
			if e != nil {
				t.Fatal(e)
			}
			t.Fatalf("create cut off %s", b)
		}
	case e := <-callError:
		t.Fatal(e)
	case <-time.After(5 * time.Second):
		t.Fatal("create unfinished")
	}
	if h.stop("SIGTERM") != 0 {
		t.Fatal("drain failed")
	}
	bus := h.ec.Events()
	if len(bus) != 1 || bus[0].Name != "cron.during_drain.created" {
		t.Fatalf("drained bus %+v", bus)
	}
	later := newRunHarness(t, dir)
	later.start()
	var list struct{ Triggers []struct{ Slug string } }
	runContent(t, later.call("list", nil), &list)
	if len(list.Triggers) != 1 || list.Triggers[0].Slug != "during_drain" {
		t.Fatalf("drain persistence %+v", list)
	}
	if later.stop("done") != 0 {
		t.Fatal("later stop")
	}
}

// R-C381-EVND R-J989-2B1P R-JHRJ-QP8K R-VI7P-52A2
func TestRunCutsOffRequestsAtRealDeadlineWithoutFiring(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			dir := t.TempDir()
			clock := newRunClock()
			d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "cron.db"), Migrations: cron.Migrations(), Now: clock.Now})
			if e != nil {
				t.Fatal(e)
			}
			st := store.New(d, store.Config{Now: clock.Now, Rand: runBytes(0x5a)})
			v, e := st.Create(context.Background(), store.Draft{Slug: "hourly", When: "@hourly", OwnerID: "owner", OwnerEmail: "owner@example.com"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = st.SetLastFired(context.Background(), v.ID, runTime("2026-10-05T09:00:00Z")); e != nil {
				t.Fatal(e)
			}
			if e = d.Close(); e != nil {
				t.Fatal(e)
			}
			h := newRunHarness(t, dir)
			h.clock.advance(runTime("2026-10-05T09:59:58Z"))
			lookup := h.p.LookupEnv
			h.p.LookupEnv = func(k string) (string, bool) {
				if k == "DRAIN_SECONDS" {
					return "1", true
				}
				return lookup(k)
			}
			entered := make(chan struct{}, count)
			release := make(chan struct{})
			banner := h.p.Banner
			h.p.Banner = func(u page.User) page.Banner { entered <- struct{}{}; <-release; return banner(u) }
			defer close(release)
			h.p.Sink = runTelemetrySink(func(c context.Context, ev telemetry.Event) error {
				if (ev.Name == "service.stopping" || ev.Name == "request.finished") && c.Err() == nil {
					t.Errorf("cutoff event delivered with live context: %+v", ev)
				}
				return h.tc.Deliver(c, ev)
			})
			h.p.Inherit = func(uintptr) (net.Listener, error) {
				return runCloseListener{Listener: h.listener, onClose: func() {
					if h.ctx.Err() != nil && !strings.Contains(h.err.String(), `"event":"service.stopping"`) {
						t.Error("connection closed before stopping line")
					}
				}}, nil
			}
			h.start()
			responses := make(chan error, count)
			for range count {
				go func() {
					req, err := http.NewRequestWithContext(t.Context(), "GET", h.url+"/", nil)
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
			h.clock.advance(runTime("2026-10-05T10:00:02Z"))
			select {
			case code := <-h.done:
				h.stopped = true
				if code != cli.ExitServerFailed {
					t.Fatalf("cutoff exit %d", code)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("drain used frozen injected timer")
			}
			elapsed := time.Since(start)
			if elapsed < time.Second || elapsed >= 2*time.Second {
				t.Fatalf("drain duration %s", elapsed)
			}
			want := "cron: stopped with 1 request unfinished\n"
			if count == 2 {
				want = "cron: stopped with 2 requests unfinished\n"
			}
			lines := h.err.calls()
			if len(lines) < 2 || lines[len(lines)-1] != want {
				t.Fatalf("cutoff diagnostic %q", lines)
			}
			stoppingWritten := false
			for _, line := range lines {
				if strings.Contains(line, `"event":"service.stopping"`) {
					stoppingWritten = true
				}
			}
			if !stoppingWritten {
				t.Fatalf("stopping not written before cutoff %q", lines)
			}
			for range count {
				select {
				case err := <-responses:
					if err == nil {
						t.Fatal("cutoff had no error")
					}
				case <-time.After(time.Second):
					t.Fatal("request connection not closed")
				}
			}
			if h.out.String() != "" {
				t.Fatal(h.out.String())
			}
			for _, ev := range h.ec.Events() {
				if _, ok := ev.Attrs["scheduled"]; ok {
					t.Fatalf("fire after cancel %+v", ev)
				}
			}
			for _, ev := range h.tc.Events() {
				if _, ok := ev.Attrs["scheduled"]; ok {
					t.Fatalf("trail fire after cancel %+v", ev)
				}
			}
			later := newRunHarness(t, dir)
			later.start()
			var shown struct {
				LastFired string `json:"last_fired"`
			}
			runContent(t, later.call("show", map[string]string{"slug": "hourly"}), &shown)
			if shown.LastFired != "2026-10-05T09:00:00Z" {
				t.Fatalf("fired after stop %q", shown.LastFired)
			}
			if later.stop("done") != 0 {
				t.Fatal("later stop")
			}
		})
	}
}

// R-JCVY-7M9S R-JE3U-LE0H R-JFBQ-Z5R6
func TestRunRejectedEventsHaveExactDiagnosticsAndLoss(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	h.p.Sink = runTelemetrySink(func(c context.Context, e telemetry.Event) error { _ = h.tc.Deliver(c, e); return telemetry.ErrRejected })
	h.p.EventSink = runEventSink(func(c context.Context, e events.Event) error { _ = h.ec.Deliver(c, e); return events.ErrRejected })
	h.start()
	h.call("create", map[string]string{"slug": "reject", "when": "@daily"})
	if code, _ := h.get("/"); code != 200 {
		t.Fatal(code)
	}
	if h.stop("done") != 0 {
		t.Fatal("failed stop")
	}
	want := map[string]int{}
	for _, ev := range h.tc.Events() {
		b, e := ev.MarshalJSON()
		if e != nil {
			t.Fatal(e)
		}
		want["cron: undelivered event: "+string(b)+"\n"]++
	}
	for _, ev := range h.ec.Events() {
		b, e := ev.MarshalJSON()
		if e != nil {
			t.Fatal(e)
		}
		want["cron: lost event: "+string(b)+"\n"]++
	}
	got := map[string]int{}
	for _, line := range h.err.calls() {
		got[line]++
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostics got %v want %v", got, want)
	}
	bus := h.ec.Events()
	if len(bus) != 1 {
		t.Fatalf("rejected attempts %+v", bus)
	}
	lost := 0
	for _, ev := range h.tc.Events() {
		if ev.Name == "event.lost" {
			lost++
			if ev.RequestID != bus[0].RequestID || ev.User != bus[0].User || !reflect.DeepEqual(ev.Attrs, telemetry.Attrs{"event": bus[0].ID, "cause": bus[0].Cause}) {
				t.Fatalf("event.lost %+v for %+v", ev, bus[0])
			}
		}
	}
	if lost != 1 {
		t.Fatalf("event.lost count %d", lost)
	}
}

// R-N4KA-F7HR
func TestRunBusDeliveryContinuesDuringDrain(t *testing.T) {
	for _, recover := range []bool{false, true} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			h := newRunHarness(t, t.TempDir())
			lookup := h.p.LookupEnv
			h.p.LookupEnv = func(k string) (string, bool) {
				if k == "DRAIN_SECONDS" {
					return "1", true
				}
				return lookup(k)
			}
			attempted := make(chan struct{}, 1)
			var success atomic.Bool
			var delivered atomic.Bool
			if recover {
				h.p.Sink = runTelemetrySink(func(c context.Context, ev telemetry.Event) error {
					if ev.Name == "service.stopping" && !delivered.Load() {
						t.Error("service.stopping before bus delivery")
					}
					return h.tc.Deliver(c, ev)
				})
			}
			h.p.Sleep = func(context.Context, time.Duration) { runtime.Gosched() }
			h.p.EventSink = runEventSink(func(c context.Context, e events.Event) error {
				select {
				case attempted <- struct{}{}:
				default:
				}
				if success.Load() {
					delivered.Store(true)
					return h.ec.Deliver(c, e)
				}
				return errors.New("temporarily unavailable")
			})
			if !recover {
				h.p.Sink = runTelemetrySink(func(context.Context, telemetry.Event) error { return telemetry.ErrRejected })
			}
			h.start()
			h.call("create", map[string]string{"slug": "drain_bus", "when": "@daily"})
			select {
			case <-attempted:
			case <-time.After(5 * time.Second):
				t.Fatal("bus never attempted")
			}
			start := time.Now()
			h.cancel(errors.New("SIGTERM"))
			if recover {
				success.Store(true)
			}
			if h.stop("SIGTERM") != 0 {
				t.Fatal("bus drain failed")
			}
			if recover {
				if time.Since(start) >= time.Second || len(h.ec.Events()) != 1 || h.err.String() != "" {
					t.Fatalf("recovered drain %s events %+v stderr %q", time.Since(start), h.ec.Events(), h.err.String())
				}
				for _, ev := range h.tc.Events() {
					if ev.Name == "event.lost" {
						t.Fatal("lost successfully delivered event")
					}
				}
			} else {
				lines := h.err.calls()
				if len(lines) < 3 {
					t.Fatalf("drain logs %q", lines)
				}
				last := lines[len(lines)-3:]
				if !strings.Contains(last[0], `"event":"service.stopping"`) || !strings.HasPrefix(last[1], "cron: lost event: ") || !strings.Contains(last[2], `"event":"event.lost"`) {
					t.Fatalf("final drain lines %q", last)
				}
				if elapsed := time.Since(start); elapsed < time.Second || elapsed >= 2*time.Second {
					t.Fatalf("bus deadline %s", elapsed)
				}
			}
		})
	}
}

// R-JIZG-4GZ9
func TestRunAcceptFailureFinalDiagnostic(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	h.p.Inherit = func(uintptr) (net.Listener, error) {
		return runListener{Listener: h.listener, accept: func() error { return errors.New("accept fixture failed") }}, nil
	}
	h.start()
	select {
	case code := <-h.done:
		h.stopped = true
		if code != cli.ExitServerFailed {
			t.Fatal(code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("accept failure kept serving")
	}
	lines := h.err.calls()
	if len(lines) == 0 || lines[len(lines)-1] != "cron: accept fixture failed\n" || h.out.String() != "" {
		t.Fatalf("accept diagnostic %q", lines)
	}
}

type runTemporary struct{}

func (runTemporary) Error() string   { return "temporary fixture error" }
func (runTemporary) Temporary() bool { return true }
func (runTemporary) Timeout() bool   { return false }

// R-JP2Y-1BOQ
func TestRunPanickingBannerAndTemporaryAcceptAreQuiet(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	h := newRunHarness(t, t.TempDir())
	var count atomic.Int64
	h.p.Inherit = func(uintptr) (net.Listener, error) {
		return runListener{Listener: h.listener, accept: func() error {
			if count.Add(1) == 1 {
				return runTemporary{}
			}
			return nil
		}}, nil
	}
	h.p.Banner = func(page.User) page.Banner { panic("banner fixture panic") }
	h.start()
	req, e := http.NewRequestWithContext(t.Context(), "GET", h.url+"/", nil)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("X-User-Id", "owner")
	resp, e := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if e == nil {
		if e = resp.Body.Close(); e != nil {
			t.Fatal(e)
		}
	}
	if h.stop("done") != 0 {
		t.Fatal("panic stop failed")
	}
	if output.Len() != 0 || count.Load() < 2 {
		t.Fatalf("default log %q accepts %d", output.String(), count.Load())
	}
}

type runGuardOutput struct {
	out                     runOutput
	active                  atomic.Int64
	overlap, returned, late atomic.Bool
}

func (w *runGuardOutput) Write(b []byte) (int, error) {
	if w.active.Add(1) != 1 {
		w.overlap.Store(true)
	}
	defer w.active.Add(-1)
	if w.returned.Load() {
		w.late.Store(true)
	}
	runtime.Gosched()
	return w.out.Write(b)
}

// R-JNV1-NJY1
func TestRunSerializesStderrOfConcurrentRequestsAndFire(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	out := &runGuardOutput{}
	h.p.Stderr = out
	h.p.Sink = runTelemetrySink(func(context.Context, telemetry.Event) error { return telemetry.ErrRejected })
	fired := make(chan struct{}, 1)
	h.p.EventSink = runEventSink(func(_ context.Context, e events.Event) error {
		if e.Name == "cron.tick.fired" {
			fired <- struct{}{}
		}
		return events.ErrRejected
	})
	h.start()
	h.call("create", map[string]string{"slug": "tick", "when": "* * * * *"})
	select {
	case <-h.clock.requested:
	case <-time.After(5 * time.Second):
		t.Fatal("no timer")
	}
	var group sync.WaitGroup
	for range 12 {
		group.Go(func() { h.get("/") })
	}
	h.clock.advance(runTime("2026-10-05T09:33:00Z"))
	group.Wait()
	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("no fire")
	}
	if h.stop("done") != 0 {
		t.Fatal("stop failed")
	}
	out.returned.Store(true)
	h.writer.Emit(context.Background(), "request.finished", telemetry.Attrs{"fixture": "late emission"})
	if e := h.writer.Flush(context.Background()); e != nil {
		t.Fatal(e)
	}
	if out.overlap.Load() || out.late.Load() || len(out.out.calls()) < 24 {
		t.Fatalf("output overlap=%v late=%v lines=%d", out.overlap.Load(), out.late.Load(), len(out.out.calls()))
	}
}

// R-C0S8-NC5Z
func TestRunEmptyDirAndNilUnsetenv(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	h := newRunHarness(t, "")
	h.p.Unsetenv = nil
	h.start()
	h.call("create", map[string]string{"slug": "in_cwd", "when": "@daily"})
	if h.stop("done") != 0 {
		t.Fatal("stop failed")
	}
	info, e := os.Stat(filepath.Join(dir, "state", "cron.db"))
	if e != nil || !info.Mode().IsRegular() {
		t.Fatalf("empty Dir database %v %v", info, e)
	}
	if len(h.unsets) != 0 {
		t.Fatalf("nil Unsetenv removed %v", h.unsets)
	}
}

// R-IULG-H25D
func TestRunCancellationDuringNotificationLookup(t *testing.T) {
	for _, notify := range []bool{false, true} {
		t.Run(fmt.Sprint(notify), func(t *testing.T) {
			h := newRunHarness(t, t.TempDir())
			lookup := h.p.LookupEnv
			h.p.LookupEnv = func(k string) (string, bool) {
				if k == "NOTIFY_SOCKET" {
					h.cancel(errors.New("during startup"))
					if !notify {
						return "", false
					}
				}
				return lookup(k)
			}
			var accepted atomic.Int64
			h.p.Inherit = func(uintptr) (net.Listener, error) {
				return runListener{Listener: h.listener, accept: func() error { accepted.Add(1); return nil }}, nil
			}
			if code := cli.Run(h.ctx, h.p); code != 0 {
				t.Fatal(code)
			}
			h.stopped = true
			assertNoRunNotification(h)
			if accepted.Load() != 0 || h.out.String() != "" || h.err.String() != "" || len(h.tc.Events())+len(h.ec.Events()) != 0 {
				t.Fatalf("cancelled startup accepts=%d out=%q err=%q trail=%+v bus=%+v", accepted.Load(), h.out.String(), h.err.String(), h.tc.Events(), h.ec.Events())
			}
		})
	}
}

// R-C0S8-NC5Z
func TestRunNilAfterFiresDueInjectedSlot(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	data := make([]byte, 10000)
	for i := range data {
		data[i] = byte(i / 8)
	}
	h.p.Rand = bytes.NewReader(data)
	h.p.After = nil
	fired := make(chan events.Event, 1)
	h.p.EventSink = runEventSink(func(c context.Context, e events.Event) error {
		if e.Name == "cron.alpha.fired" {
			fired <- e
		}
		return h.ec.Deliver(c, e)
	})
	h.start()
	h.call("create", map[string]string{"slug": "alpha", "when": "*/15 * * * *"})
	h.clock.advance(runTime("2026-10-05T09:45:00Z"))
	h.call("create", map[string]string{"slug": "beta", "when": "@daily"})
	select {
	case ev := <-fired:
		if ev.Attrs["scheduled"] != "2026-10-05T09:45:00Z" {
			t.Fatalf("default timer %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nil After did not fire due slot")
	}
	if h.stop("done") != 0 {
		t.Fatal("stop failed")
	}
}

// R-C0S8-NC5Z
func TestRunNilSinksDeliverToOwnUnixServices(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	short, e := os.MkdirTemp("", "cron-sinks-")
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
	var trailEvents telemetry.Capture
	var busEvents events.Capture
	for _, name := range []string{"telemetry", "events"} {
		name := name
		ln, err := net.Listen("unix", filepath.Join(short, name))
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if name == "telemetry" {
				var ev telemetry.Event
				if err = json.Unmarshal(b, &ev); err != nil {
					t.Error(err)
				}
				var names struct {
					Event     string `json:"event"`
					RequestID string `json:"request_id"`
				}
				if err = json.Unmarshal(b, &names); err != nil {
					t.Error(err)
				}
				ev.Name = names.Event
				ev.RequestID = names.RequestID
				_ = trailEvents.Deliver(r.Context(), ev)
			} else {
				var ev events.Event
				if err = json.Unmarshal(b, &ev); err != nil {
					t.Error(err)
				}
				_ = busEvents.Deliver(r.Context(), ev)
			}
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
	path := filepath.Join(h.p.Dir, "services.toml")
	data := fmt.Sprintf(`{"services":[{"name":"telemetry","url":"http://telemetry.test","description":"fixture","socket":%q,"enabled":true,"mcp":false},{"name":"events","url":"http://events.test","description":"fixture","socket":%q,"enabled":true,"mcp":false}]}`, filepath.Join(short, "telemetry"), filepath.Join(short, "events"))
	if e := os.WriteFile(path, []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv(services.Variable, path)
	h.p.Sink = nil
	h.p.EventSink = nil
	h.start()
	h.call("create", map[string]string{"slug": "socket_sink", "when": "@daily"})
	if h.stop("done") != 0 || h.err.String() != "" {
		t.Fatalf("default sinks %q", h.err.String())
	}
	trail := trailEvents.Events()
	bus := busEvents.Events()
	if len(trail) < 2 || trail[0].Name != "service.started" || trail[len(trail)-1].Name != "service.stopping" || len(bus) != 1 || bus[0].Name != "cron.socket_sink.created" {
		t.Fatalf("default sink events trail=%+v bus=%+v", trail, bus)
	}
}

// R-C0S8-NC5Z
func TestRunNilSleepEndsAtDrainDeadline(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	lookup := h.p.LookupEnv
	h.p.LookupEnv = func(k string) (string, bool) {
		if k == "DRAIN_SECONDS" {
			return "1", true
		}
		return lookup(k)
	}
	h.p.Sleep = nil
	entered := make(chan struct{}, 1)
	h.p.EventSink = runEventSink(func(context.Context, events.Event) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		return errors.New("offline bus")
	})
	h.start()
	h.call("create", map[string]string{"slug": "sleeping", "when": "@daily"})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no attempt")
	}
	started := time.Now()
	if h.stop("done") != 0 {
		t.Fatal("stop failed")
	}
	if elapsed := time.Since(started); elapsed < time.Second || elapsed >= 2*time.Second {
		t.Fatalf("nil Sleep drain %s", elapsed)
	}
	if !strings.Contains(h.err.String(), "cron: lost event: ") {
		t.Fatal("undelivered bus not dropped")
	}
}

// R-J34R-5GC8 R-IZH2-0545 R-J1WU-ROLJ
func TestRunEmptyVersionAndTimestampNormalization(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	h.p.Version = ""
	now := time.Date(2026, 10, 5, 11, 32, 0, 123456789, time.FixedZone("fixture", 2*60*60))
	h.clock.advance(now)
	h.start()
	h.call("create", map[string]string{"slug": "normalized", "when": "@daily"})
	if h.stop("done") != 0 {
		t.Fatal("stop failed")
	}
	expected := now.UTC().Truncate(time.Microsecond)
	for _, ev := range h.tc.Events() {
		if ev.Service != "cron" || ev.Time != expected {
			t.Fatalf("normalized trail %+v", ev)
		}
	}
	for _, ev := range h.ec.Events() {
		if ev.Service != "cron" || ev.Time != expected {
			t.Fatalf("normalized bus %+v", ev)
		}
	}
	first := h.tc.Events()[0]
	if !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"version": ""}) {
		t.Fatalf("empty version %+v", first)
	}
}

type runCloseListener struct {
	net.Listener
	onClose func()
}

func (l runCloseListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return &runCloseConn{Conn: c, onClose: l.onClose}, nil
}

type runCloseConn struct {
	net.Conn
	onClose func()
}

func (c runCloseConn) Close() error { c.onClose(); return c.Conn.Close() }

// R-JCVY-7M9S
func TestRunWriterRecoveryDoesNotRedeliverWrittenEvents(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	var recovered atomic.Bool
	var delivered telemetry.Capture
	h.p.Sink = runTelemetrySink(func(c context.Context, e telemetry.Event) error {
		_ = h.tc.Deliver(c, e)
		if !recovered.Load() {
			return telemetry.ErrRejected
		}
		return delivered.Deliver(c, e)
	})
	h.start()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := h.writer.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	before := h.err.calls()
	if len(before) != 1 || !strings.Contains(before[0], `"event":"service.started"`) {
		t.Fatalf("undelivered started %q", before)
	}
	recovered.Store(true)
	if code, _ := h.get("/"); code != 200 {
		t.Fatal(code)
	}
	if h.stop("done") != 0 {
		t.Fatal("stop failed")
	}
	if !reflect.DeepEqual(h.err.calls(), before) {
		t.Fatalf("recovery diagnostics %q", h.err.calls())
	}
	es := delivered.Events()
	if len(es) != 3 {
		t.Fatalf("delivered %+v", es)
	}
	for _, ev := range es {
		if ev.Name == "service.started" {
			t.Fatal("written event redelivered")
		}
	}
}

// R-J4CN-J82X
func TestRunCapturesServicesPathAndReadsItsFileAfresh(t *testing.T) {
	h := newRunHarness(t, t.TempDir())
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	write := func(path, origin string) {
		data := fmt.Sprintf(`{"services":[{"name":"auth","url":%q,"description":"fixture","socket":"","enabled":true,"mcp":false}]}`, origin)
		if e := os.WriteFile(path, []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(first, "https://auth.initial.test")
	write(second, "https://auth.other.test")
	lookup := h.p.LookupEnv
	var changed atomic.Bool
	h.p.LookupEnv = func(k string) (string, bool) {
		if k == services.Variable {
			if changed.Load() {
				return second, true
			}
			return first, true
		}
		return lookup(k)
	}
	h.start()
	if code, body := h.get("/"); code != 200 || !strings.Contains(body, "https://auth.initial.test") {
		t.Fatalf("initial services %d %s", code, body)
	}
	changed.Store(true)
	write(first, "https://auth.updated.test")
	if code, body := h.get("/"); code != 200 || !strings.Contains(body, "https://auth.updated.test") || strings.Contains(body, "https://auth.other.test") {
		t.Fatalf("captured services %d %s", code, body)
	}
	if h.stop("done") != 0 {
		t.Fatal("stop failed")
	}
}
