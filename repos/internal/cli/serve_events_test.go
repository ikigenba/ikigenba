package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	"github.com/ikigenba/ikigenba/repos/internal/web"
)

type uniformBusRandom byte

func (r uniformBusRandom) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}

type busSink func(context.Context, events.Event) error

func (s busSink) Deliver(ctx context.Context, e events.Event) error { return s(ctx, e) }
func pushServeEvent(t *testing.T, f *serveFixture) {
	t.Helper()
	f.tool(t, "create", `{"name":"alpha"}`)
	sha, pack := servePack(t, f)
	response := finishServePush(t, startServePush(t, f, "bus-push", servePushBody(sha, pack), false))
	if response.err != nil || response.status != 200 || !bytes.Contains(response.body, []byte("ok refs/heads/main")) {
		t.Fatalf("push %+v", response)
	}
}
func waitBus(t *testing.T, ch <-chan events.Event) events.Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("bus delivery missing")
		return events.Event{}
	}
}
func assertBusLoss(t *testing.T, f *serveFixture, bus events.Event, lost bool) {
	t.Helper()
	f.flush(t)
	pushed, loss := -1, -1
	for i, e := range serveRecordedEvents(t, f) {
		if e.Name == "repo.pushed" && e.RequestID == bus.RequestID {
			pushed = i
		}
		if e.Name == "event.lost" {
			if loss >= 0 {
				t.Fatal("duplicate loss")
			}
			loss = i
			if !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"event": bus.ID, "cause": bus.Cause}) || e.User != bus.User || e.RequestID != bus.RequestID {
				t.Fatalf("loss %+v", e)
			}
		}
	}
	canonical, err := bus.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range f.stderr.lines() {
		if bytes.HasPrefix([]byte(line), []byte("repos: lost event: ")) {
			lines = append(lines, line)
		}
	}
	if lost {
		if pushed < 0 || loss <= pushed {
			t.Fatalf("push/loss %d/%d", pushed, loss)
		}
		if len(lines) != 1 || lines[0] != "repos: lost event: "+string(canonical)+"\n" {
			t.Fatalf("lines %q", lines)
		}
	} else if loss >= 0 || len(lines) != 0 {
		t.Fatalf("unexpected loss %d %q", loss, lines)
	}
}

// R-9UIL-8JM0 R-9VQH-MBCP R-KV0B-8G82
func TestServeBusCaptureEnvelope(t *testing.T) {
	f := newServeFixture(t)
	capture := &events.Capture{}
	f.p.Rand = uniformBusRandom(0xab)
	f.p.EventSink = capture
	f.start(t)
	pushServeEvent(t, f)
	f.stop(t, "done", cli.ExitSuccess)
	es := capture.Events()
	if len(es) != 1 {
		t.Fatalf("bus %+v", es)
	}
	e := es[0]
	if e.Name != "repo.pushed" || e.Service != web.ServiceName || !e.Time.Equal(f.p.Now().UTC().Truncate(time.Microsecond)) {
		t.Fatalf("envelope %+v", e)
	}
	if e.ID != "evt_abababababababab" {
		t.Fatalf("id %q", e.ID)
	}
	if f.stdout.text() != "" || f.stderr.text() != "" {
		t.Fatalf("streams %q %q", f.stdout.text(), f.stderr.text())
	}
}

// R-DKKS-371O
func TestServeDeclarationsWithoutIdentity(t *testing.T) {
	f := newServeFixture(t)
	f.start(t)
	for _, identity := range []bool{false, true} {
		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://"+f.listener.Addr().String()+"/declarations", nil)
		if err != nil {
			t.Fatal(err)
		}
		if identity {
			req.Header.Set("X-User-Id", "owner")
		}
		resp, err := f.client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var got, want map[string]any
		err = json.NewDecoder(resp.Body).Decode(&got)
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("status %d %v", resp.StatusCode, err)
		}
		if err = json.Unmarshal([]byte(`{"emits":[{"event":"repo.pushed","attrs":["repo","ref","old","new"]}],"accepts":[]}`), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("declarations %v", got)
		}
	}
	f.stop(t, "done", cli.ExitSuccess)
}

// R-3MCM-O5P3 R-3NKJ-1XFS R-3UVX-CJVY R-3OSF-FP6H R-3Q0B-TGX6
func TestServeBusRetryAndLoss(t *testing.T) {
	for _, mode := range []string{"recover", "reject", "retry-window", "exact-window", "before-window", "first-call-window"} {
		t.Run(mode, func(t *testing.T) {
			f := newServeFixture(t)
			var mu sync.Mutex
			now := f.p.Now()
			base := now
			count := 0
			var first events.Event
			seen := make(chan events.Event, 1)
			f.p.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
			f.p.Sleep = func(_ context.Context, d time.Duration) {
				mu.Lock()
				defer mu.Unlock()
				switch mode {
				case "exact-window":
					now = base.Add(events.DefaultRetryWindow)
				case "before-window":
					if count == 1 {
						now = base.Add(events.DefaultRetryWindow - time.Microsecond)
					} else {
						now = base.Add(events.DefaultRetryWindow)
					}
				default:
					now = now.Add(d)
				}
			}
			f.p.EventSink = busSink(func(_ context.Context, e events.Event) error {
				mu.Lock()
				defer mu.Unlock()
				count++
				if count == 1 {
					first = e
					base = now
				} else if e.ID != first.ID {
					t.Error("retry changed id")
				}
				switch mode {
				case "recover":
					if count < 3 {
						return errors.New("temporary")
					}
					seen <- e
					return nil
				case "reject":
					seen <- e
					return fmt.Errorf("wrapped: %w", events.ErrRejected)
				case "first-call-window":
					now = now.Add(events.DefaultRetryWindow)
				}
				if now.Sub(base) >= events.DefaultRetryWindow {
					seen <- e
				}
				return errors.New("temporary")
			})
			f.start(t)
			pushServeEvent(t, f)
			bus := waitBus(t, seen)
			if mode != "recover" {
				waitServeLoss(t, f)
				assertBusLoss(t, f, bus, true)
			}
			f.stop(t, "done", cli.ExitSuccess)
			assertBusLoss(t, f, bus, mode != "recover")
			mu.Lock()
			calls := count
			mu.Unlock()
			switch mode {
			case "recover":
				if calls != 3 {
					t.Fatalf("calls %d", calls)
				}
			case "reject", "first-call-window":
				if calls != 1 {
					t.Fatalf("calls %d", calls)
				}
			case "exact-window":
				if calls != 2 {
					t.Fatalf("calls %d", calls)
				}
			case "before-window":
				if calls <= 2 {
					t.Fatalf("calls %d", calls)
				}
			default:
				if calls < 2 {
					t.Fatalf("calls %d", calls)
				}
			}
		})
	}
}

// R-3R88-78NV R-3SG4-L0EK R-3TO0-YS59
func TestServeBusDrainRecoveryAndDeadline(t *testing.T) {
	for _, recover := range []bool{true, false} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			f := newServeFixture(t)
			entered := make(chan events.Event, 1)
			resume := make(chan struct{})
			var once sync.Once
			var mu sync.Mutex
			calls := 0
			f.p.Sleep = func(ctx context.Context, _ time.Duration) {
				if recover {
					<-resume
				} else {
					<-ctx.Done()
				}
			}
			f.p.EventSink = busSink(func(ctx context.Context, e events.Event) error {
				mu.Lock()
				calls++
				n := calls
				mu.Unlock()
				once.Do(func() { entered <- e })
				if !recover {
					<-ctx.Done()
					return nil
				}
				if n == 1 {
					return errors.New("temporary")
				}
				return nil
			})
			f.start(t)
			pushServeEvent(t, f)
			bus := waitBus(t, entered)
			begin := time.Now()
			f.cancel(errors.New("drain"))
			if recover {
				close(resume)
			} else {
				f.flush(t)
				if f.stderr.text() != "" {
					t.Fatalf("early loss %q", f.stderr.text())
				}
			}
			f.stop(t, "drain", cli.ExitSuccess)
			elapsed := time.Since(begin)
			assertBusLoss(t, f, bus, !recover)
			if recover && elapsed >= time.Second {
				t.Fatalf("recovery waited %s", elapsed)
			}
			if !recover {
				if elapsed < time.Second || elapsed >= 2*time.Second {
					t.Fatalf("deadline %s", elapsed)
				}
				es := serveRecordedEvents(t, f)
				loss, stop := -1, -1
				for i, e := range es {
					if e.Name == "event.lost" {
						loss = i
					}
					if e.Name == "service.stopping" {
						stop = i
					}
				}
				if loss < 0 || stop <= loss {
					t.Fatalf("order %+v", es)
				}
			}
		})
	}
}

func serveRecordedEvents(t *testing.T, f *serveFixture) []telemetry.Event {
	t.Helper()
	es := f.capture.Events()
	for _, line := range f.stderr.lines() {
		prefix := []byte("repos: undelivered event: ")
		if bytes.HasPrefix([]byte(line), prefix) {
			var wire struct {
				Name      string          `json:"event"`
				RequestID string          `json:"request_id"`
				User      string          `json:"user"`
				Attrs     telemetry.Attrs `json:"attrs"`
			}
			if err := json.Unmarshal(bytes.TrimPrefix([]byte(line), prefix), &wire); err != nil {
				t.Fatal(err)
			}
			es = append(es, telemetry.Event{Name: wire.Name, RequestID: wire.RequestID, User: wire.User, Attrs: wire.Attrs})
		}
	}
	return es
}

func waitServeLoss(t *testing.T, f *serveFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		found := false
		for _, e := range f.capture.Events() {
			if e.Name == "event.lost" {
				found = true
			}
		}
		if found && bytes.Contains([]byte(f.stderr.text()), []byte("repos: lost event: ")) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("loss not written while running")
		default:
			runtime.Gosched()
		}
	}
}

// R-3NKJ-1XFS: retry age begins at first delivery, not emission while queued.
func TestServeQueuedBusRetryWindow(t *testing.T) {
	f := newServeFixture(t)
	var mu sync.Mutex
	now := f.p.Now()
	base := now
	calls := 0
	entered := make(chan events.Event, 1)
	released := make(chan struct{})
	lost := make(chan events.Event, 1)
	f.p.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	f.p.Sleep = func(_ context.Context, d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	f.p.EventSink = busSink(func(_ context.Context, e events.Event) error {
		if e.Attrs["ref"] == "refs/heads/main" {
			entered <- e
			<-released
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			base = now
		}
		if now.Sub(base) >= events.DefaultRetryWindow {
			lost <- e
		}
		return errors.New("offline")
	})
	f.start(t)
	f.tool(t, "create", `{"name":"alpha"}`)
	sha, pack := servePack(t, f)
	body := servePushBody(sha, pack)
	if response := finishServePush(t, startServePush(t, f, "first", body, false)); response.err != nil || response.status != 200 {
		t.Fatalf("first %+v", response)
	}
	waitBus(t, entered)
	body = bytes.Replace(body, []byte("refs/heads/main"), []byte("refs/heads/side"), 1)
	if response := finishServePush(t, startServePush(t, f, "second", body, false)); response.err != nil || response.status != 200 {
		t.Fatalf("second %+v", response)
	}
	mu.Lock()
	now = now.Add(events.DefaultRetryWindow + time.Hour)
	mu.Unlock()
	close(released)
	bus := waitBus(t, lost)
	waitServeLoss(t, f)
	assertBusLoss(t, f, bus, true)
	f.stop(t, "done", cli.ExitSuccess)
	mu.Lock()
	n := calls
	mu.Unlock()
	if n <= 1 {
		t.Fatalf("queued event attempted %d times", n)
	}
}
