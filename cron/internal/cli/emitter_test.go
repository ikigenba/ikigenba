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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/cron/internal/cli"
	"github.com/ikigenba/ikigenba/cron/internal/tools"
)

type ecClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []chan time.Time
	firing bool
}

func (c *ecClock) read() time.Time         { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *ecClock) advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }
func (c *ecClock) after(time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, ch)
	if c.firing {
		ch <- c.now
	}
	return ch
}
func (c *ecClock) wake(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at
	c.firing = true
	for _, ch := range c.timers {
		select {
		case ch <- at:
		default:
		}
	}
}

type ecRandom struct{}

func (ecRandom) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0x31
	}
	return len(p), nil
}

type ecOutput struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *ecOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *ecOutput) text() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

type ecBusSink func(context.Context, events.Event) error

func (s ecBusSink) Deliver(ctx context.Context, e events.Event) error { return s(ctx, e) }

type ecTrailSink func(context.Context, telemetry.Event) error

func (s ecTrailSink) Deliver(ctx context.Context, e telemetry.Event) error { return s(ctx, e) }

type ecNginxTransport struct{}

func (ecNginxTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header = req.Header.Clone()
	req.Host = "cron.example.test"
	req.Header.Set("X-Forwarded-Proto", "https")
	return http.DefaultTransport.RoundTrip(req)
}

type ecRun struct {
	t        *testing.T
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan int
	url      string
	clock    *ecClock
	stderr   *ecOutput
	client   *mcp.Client
	caller   identity.Caller
	stopOnce sync.Once
}

func ecStart(t *testing.T, dir string, clock *ecClock, sink telemetry.Sink, bus events.Sink, sleep func(context.Context, time.Duration), notifyPaths ...string) *ecRun {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	if dir == "" {
		dir = t.TempDir()
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	short, err := os.MkdirTemp("", "cron-ready-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(short); err != nil {
			t.Error(err)
		}
	})
	notifyPath := filepath.Join(short, "n")
	if len(notifyPaths) > 0 {
		notifyPath = notifyPaths[0]
	}
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = notify.Close() })
	if clock == nil {
		clock = &ecClock{now: time.Date(2026, 10, 5, 9, 32, 0, 0, time.UTC)}
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &ecRun{t: t, ctx: ctx, cancel: cancel, done: make(chan int, 1), url: "http://" + ln.Addr().String(), clock: clock, stderr: &ecOutput{}, caller: identity.Caller{UserID: "user", Email: "owner@example.test"}}
	env := map[string]string{"LISTEN_PID": "123", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notify.LocalAddr().String(), "DRAIN_SECONDS": "2"}
	p := cli.Process{Dir: dir, Pid: 123, Version: "test", Stdout: io.Discard, Stderr: r.stderr, LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }, Unsetenv: func(string) error { return nil }, Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Now: clock.read, After: clock.after, Rand: ecRandom{}, Sink: sink, EventSink: bus, Sleep: sleep, Banner: func(page.User) page.Banner { return page.Banner{} }, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: "cron", Version: "test", Telemetry: w})
	}}
	go func() { r.done <- cli.Run(ctx, p) }()
	r.client = mcp.NewClient(mcp.ClientConfig{Endpoint: r.url + "/mcp", HTTPClient: &http.Client{Timeout: 5 * time.Second, Transport: ecNginxTransport{}}})
	t.Cleanup(r.stop)
	if err := notify.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 32)
	n, _, err := notify.ReadFromUnix(b)
	if err != nil || string(b[:n]) != "READY=1" {
		t.Fatalf("readiness %q %v stderr %s", b[:n], err, r.stderr.text())
	}
	_ = notify.Close()
	if err := os.Remove(notifyPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return r
}
func (r *ecRun) stop() {
	r.t.Helper()
	r.stopOnce.Do(func() {
		r.cancel()
		select {
		case code := <-r.done:
			if code != cli.ExitSuccess {
				r.t.Errorf("Run exit %d: %s", code, r.stderr.text())
			}
		case <-time.After(5 * time.Second):
			r.t.Error("Run failed to stop")
		}
	})
}
func (r *ecRun) call(name string, args any) mcp.Result {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := json.Marshal(args)
	if err != nil {
		r.t.Fatal(err)
	}
	result, err := r.client.CallTool(ctx, r.caller, name, raw)
	if err != nil {
		r.t.Fatal(err)
	}
	return result
}
func (r *ecRun) ok(name string, args any) mcp.Result {
	r.t.Helper()
	result := r.call(name, args)
	if result.IsError() {
		raw, _ := result.MarshalJSON()
		r.t.Fatalf("%s refused: %s", name, raw)
	}
	return result
}
func ecAwait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not finish")
		var zero T
		return zero
	}
}

// R-4DJY-5IRW R-UTVO-U646
func TestRunEmitterOrderedDelivery(t *testing.T) {
	var mu sync.Mutex
	var names []string
	active := 0
	overlap := false
	sleeps := 0
	entered := make(chan events.Event, 8)
	release := make(chan error, 8)
	sleepEntered := make(chan struct{}, 8)
	sleepRelease := make(chan struct{}, 8)
	bus := ecBusSink(func(ctx context.Context, e events.Event) error {
		mu.Lock()
		active++
		overlap = overlap || active != 1
		names = append(names, e.Name)
		mu.Unlock()
		entered <- e
		var err error
		select {
		case err = <-release:
		case <-ctx.Done():
			err = ctx.Err()
		}
		mu.Lock()
		active--
		mu.Unlock()
		return err
	})
	r := ecStart(t, "", nil, &telemetry.Capture{}, bus, func(ctx context.Context, _ time.Duration) {
		mu.Lock()
		sleeps++
		mu.Unlock()
		sleepEntered <- struct{}{}
		select {
		case <-sleepRelease:
		case <-ctx.Done():
		}
	})
	r.ok("create", tools.CreateArgs{Slug: "alpha", When: "@daily"})
	first := ecAwait(t, entered)
	r.ok("pause", tools.PauseArgs{Slug: "alpha"})
	r.ok("resume", tools.ResumeArgs{Slug: "alpha"})
	release <- errors.New("temporary")
	ecAwait(t, sleepEntered)
	select {
	case e := <-entered:
		t.Fatalf("overtook first event: %s", e.Name)
	default:
	}
	sleepRelease <- struct{}{}
	again := ecAwait(t, entered)
	if again.ID != first.ID || again.Name != first.Name {
		t.Fatal("did not retry first event")
	}
	release <- nil
	second := ecAwait(t, entered)
	release <- nil
	third := ecAwait(t, entered)
	release <- nil
	r.stop()
	mu.Lock()
	defer mu.Unlock()
	if overlap || sleeps != 1 || !strings.HasSuffix(second.Name, ".paused") || !strings.HasSuffix(third.Name, ".resumed") || len(names) != 4 {
		t.Fatalf("order=%v overlap=%v sleeps=%d", names, overlap, sleeps)
	}
}

// R-4FZQ-X29A R-UTVO-U646
func TestRunEmitterRetryWindow(t *testing.T) {
	for _, jump := range []bool{false, true} {
		t.Run(fmt.Sprint(jump), func(t *testing.T) {
			clock := &ecClock{now: time.Date(2026, 10, 5, 9, 32, 0, 0, time.UTC)}
			var mu sync.Mutex
			starts := map[string][]time.Time{}
			var sequence []string
			var actions []string
			gate := make(chan struct{})
			firstAttempt := make(chan struct{}, 1)
			var firstOnce sync.Once
			sleeps := 0
			dropped := make(chan struct{}, 4)
			trail := ecTrailSink(func(_ context.Context, e telemetry.Event) error {
				if e.Name == "event.lost" {
					dropped <- struct{}{}
				}
				return nil
			})
			bus := ecBusSink(func(_ context.Context, e events.Event) error {
				firstOnce.Do(func() { firstAttempt <- struct{}{}; <-gate })
				mu.Lock()
				actions = append(actions, "deliver:"+e.Name)
				starts[e.Name] = append(starts[e.Name], clock.read())
				sequence = append(sequence, e.Name)
				mu.Unlock()
				if jump {
					clock.advance(events.DefaultRetryWindow)
				}
				return errors.New("temporary")
			})
			r := ecStart(t, "", clock, trail, bus, func(_ context.Context, d time.Duration) {
				mu.Lock()
				sleeps++
				actions = append(actions, "sleep")
				mu.Unlock()
				clock.advance(d)
			})
			r.ok("create", tools.CreateArgs{Slug: "alpha", When: "@daily"})
			ecAwait(t, firstAttempt)
			r.ok("pause", tools.PauseArgs{Slug: "alpha"})
			close(gate)
			for i := 0; i < 2; i++ {
				select {
				case <-dropped:
				case <-time.After(time.Second):
					t.Fatal("injected retry window took a second of real time")
				}
			}
			r.stop()
			mu.Lock()
			defer mu.Unlock()
			attempts := 0
			for _, name := range []string{"cron.alpha.created", "cron.alpha.paused"} {
				times := starts[name]
				for i := 0; i < len(times)-1; i++ {
					if times[i].Sub(times[0]) >= events.DefaultRetryWindow {
						t.Fatal("retried after expired window", name, times)
					}
				}
				attempts += len(times)
				if len(times) == 0 {
					t.Fatal("missing attempt", name)
				}
				if jump {
					if len(times) != 1 {
						t.Fatal("retried event whose first attempt consumed window")
					}
				} else if len(times) < 2 || times[len(times)-1].Sub(times[0]) < events.DefaultRetryWindow {
					t.Fatalf("window for %s: %v", name, times)
				}
			}
			if sleeps != attempts-2 {
				t.Fatalf("%d sleeps for %d attempts", sleeps, attempts)
			}
			expectedActions := []string{}
			for _, name := range []string{"cron.alpha.created", "cron.alpha.paused"} {
				for i := range starts[name] {
					if i > 0 {
						expectedActions = append(expectedActions, "sleep")
					}
					expectedActions = append(expectedActions, "deliver:"+name)
				}
			}
			if fmt.Sprint(actions) != fmt.Sprint(expectedActions) {
				t.Fatalf("retry actions %v want %v", actions, expectedActions)
			}
			paused := false
			for _, name := range sequence {
				if name == "cron.alpha.paused" {
					paused = true
				} else if paused {
					t.Fatal("creation retried after pause began")
				}
			}
			if strings.Count(r.stderr.text(), "cron: lost event: ") != 2 {
				t.Fatal("missing drop diagnostics", r.stderr.text())
			}
		})
	}
}

// R-4H7N-ATZZ
func TestRunEmitterPermanentRejection(t *testing.T) {
	var mu sync.Mutex
	var attempts []events.Event
	sleeps := 0
	r := ecStart(t, "", nil, &telemetry.Capture{}, ecBusSink(func(_ context.Context, e events.Event) error {
		mu.Lock()
		attempts = append(attempts, e)
		mu.Unlock()
		return fmt.Errorf("refused: %w", events.ErrRejected)
	}), func(context.Context, time.Duration) { mu.Lock(); sleeps++; mu.Unlock() })
	r.ok("create", tools.CreateArgs{Slug: "alpha", When: "@daily"})
	r.stop()
	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != 1 || sleeps != 0 {
		t.Fatalf("attempts=%d sleeps=%d", len(attempts), sleeps)
	}
	data, err := attempts[0].MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if r.stderr.text() != "cron: lost event: "+string(data)+"\n" {
		t.Fatal("rejection diagnostic", r.stderr.text())
	}
}

// R-4IFJ-OLQO R-4DJY-5IRW
func TestRunEmitterQueueCapacity(t *testing.T) {
	completed := make(chan struct{}, 1)
	first := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var delivered []events.Event
	bus := ecBusSink(func(_ context.Context, e events.Event) error {
		once.Do(func() { first <- struct{}{}; <-release })
		mu.Lock()
		delivered = append(delivered, e)
		if len(delivered) == events.DefaultQueueCapacity+1 {
			completed <- struct{}{}
		}
		mu.Unlock()
		return nil
	})
	r := ecStart(t, "", nil, &telemetry.Capture{}, bus, func(context.Context, time.Duration) { t.Error("successful delivery slept") })
	defer close(release)
	r.caller.RequestID = fmt.Sprintf("%032x", 1)
	r.ok("create", tools.CreateArgs{Slug: "alpha", When: "@daily"})
	ecAwait(t, first)
	for i := 0; i < events.DefaultQueueCapacity+1; i++ {
		r.caller.RequestID = fmt.Sprintf("%032x", i+2)
		if i%2 == 0 {
			r.ok("pause", tools.PauseArgs{Slug: "alpha"})
		} else {
			r.ok("resume", tools.ResumeArgs{Slug: "alpha"})
		}
	}
	if strings.Count(r.stderr.text(), "cron: lost event: ") != 1 {
		t.Fatal("capacity drop count", r.stderr.text())
	}
	var lost struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(r.stderr.text(), "cron: lost event: "))), &lost); err != nil {
		t.Fatal(err)
	}
	if lost.RequestID != fmt.Sprintf("%032x", events.DefaultQueueCapacity+2) {
		t.Fatal("dropped a queued event instead of the last", lost)
	}
	release <- struct{}{}
	ecAwait(t, completed)
	r.stop()
	mu.Lock()
	defer mu.Unlock()
	if len(delivered) != events.DefaultQueueCapacity+1 {
		t.Fatalf("delivered %d", len(delivered))
	}
	for i, e := range delivered {
		if e.RequestID != fmt.Sprintf("%032x", i+1) {
			t.Fatalf("delivery %d request id %s", i, e.RequestID)
		}
		want := "cron.alpha.created"
		if i > 0 {
			want = "cron.alpha.paused"
			if i%2 == 0 {
				want = "cron.alpha.resumed"
			}
		}
		if e.Name != want {
			t.Fatalf("delivery %d %s want %s", i, e.Name, want)
		}
	}
}
