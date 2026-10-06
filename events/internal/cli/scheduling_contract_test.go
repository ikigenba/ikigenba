package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	appEvents "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/store"
)

type scheduled struct {
	duration time.Duration
	channel  chan time.Time
}

func controlledTimer(calls chan scheduled) func(time.Duration) <-chan time.Time {
	return func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		calls <- scheduled{d, ch}
		return ch
	}
}
func nextSchedule(t *testing.T, calls <-chan scheduled, want time.Duration) scheduled {
	t.Helper()
	select {
	case call := <-calls:
		if call.duration != want {
			t.Fatal(call.duration, want)
		}
		return call
	case <-time.After(3 * time.Second):
		t.Fatal("timer was not armed")
		return scheduled{}
	}
}
func socketServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	socket := filepath.Join(shortDir(t), "service")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: handler}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })
	return socket
}
func entry(name, socket string) map[string]any {
	return map[string]any{"name": name, "description": name, "url": "https://" + name + ".test", "socket": socket, "enabled": true, "mcp": false}
}

// R-CBMM-PF9D R-ZXHP-4DO7 R-ZTTZ-Z2G4
func TestDrivenRetentionAndRefresh(t *testing.T) {
	fixed := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	now := fixed.Add(-48 * time.Hour)
	dir := t.TempDir()
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "events.db"), Migrations: events.Migrations(), Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(d, store.Config{DepthMax: 8, Now: func() time.Time { return now }})
	if err := st.Declare(context.Background(), "repos", store.Declaration{Emits: []appEvents.Emission{{Event: "repo.pushed", Attrs: []string{"private_attr"}}}}); err != nil {
		t.Fatal(err)
	}
	older := emitted(&runFixture{fixed: fixed}, 1, 0)
	recent := emitted(&runFixture{fixed: fixed}, 2, 0)
	if err := st.Deliver(context.Background(), older); err != nil {
		t.Fatal(err)
	}
	now = fixed.Add(-12 * time.Hour)
	if err := st.Deliver(context.Background(), recent); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	asks := make(chan struct{}, 8)
	socket := socketServer(t, func(w http.ResponseWriter, _ *http.Request) {
		asks <- struct{}{}
		_, _ = io.WriteString(w, `{"emits":[{"event":"repo.pushed","attrs":["private_attr"]}],"accepts":[]}`)
	})
	path := filepath.Join(t.TempDir(), "services")
	writeServices(t, path, "", []map[string]any{entry("repos", socket)})
	sweeps, refreshes := make(chan scheduled, 8), make(chan scheduled, 8)
	var clockMu sync.Mutex
	clock := fixed
	f := startRun(t, dir, map[string]string{"IKIGENBA_SERVICES": path, "EVENTS_RETENTION_DAYS": "1", "EVENTS_DECLARATIONS_SECONDS": "13"}, func(f *runFixture) {
		f.p.Now = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return clock }
		f.p.SweepAfter = controlledTimer(sweeps)
		f.p.RefreshAfter = controlledTimer(refreshes)
	})
	search := func() []byte {
		result := call(t, f, "search", map[string]any{}, "retention-search")
		data, _ := json.Marshal(result)
		return data
	}
	data := search()
	if bytes.Contains(data, []byte(older.ID)) || !bytes.Contains(data, []byte(recent.ID)) {
		t.Fatal(string(data))
	}
	<-asks
	refresh := nextSchedule(t, refreshes, 13*time.Second)
	refresh.channel <- fixed
	_ = nextSchedule(t, refreshes, 13*time.Second)
	select {
	case <-asks:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not ask sibling")
	}
	sweep := nextSchedule(t, sweeps, time.Hour)
	clockMu.Lock()
	clock = fixed.Add(48 * time.Hour)
	clockMu.Unlock()
	sweep.channel <- clock
	_ = nextSchedule(t, sweeps, time.Hour)
	if bytes.Contains(search(), []byte(recent.ID)) {
		t.Fatal("sweep did not use injected clock and retention")
	}
	f.stop(t)
}

// R-CBMM-PF9D R-9F23-T37H R-9CMB-1JQ3 R-ZTTZ-Z2G4
func TestStartupAskDeadlineAndUndeclaredAsk(t *testing.T) {
	started := make(chan struct{}, 1)
	var count atomic.Int32
	socket := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if count.Add(1) == 1 {
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, `{"emits":[{"event":"repo.pushed","attrs":["private_attr"]}],"accepts":["repo.pushed"]}`)
	})
	path := filepath.Join(t.TempDir(), "services")
	writeServices(t, path, "", []map[string]any{entry("repos", socket)})
	asks := make(chan scheduled, 8)
	fired := make(chan struct{})
	go func() { call := <-asks; <-started; close(fired); call.channel <- time.Time{} }()
	f := startRun(t, t.TempDir(), map[string]string{"IKIGENBA_SERVICES": path}, func(f *runFixture) { f.p.AskAfter = controlledTimer(asks) })
	select {
	case <-fired:
	default:
		t.Fatal("READY preceded injected startup deadline")
	}
	if count.Load() != 1 {
		t.Fatal(count.Load())
	}
	// Startup learned no declaration. The first emit must ask the producer anew.
	e := emitted(f, 1, 0)
	emit(t, f, e, 204)
	_ = nextSchedule(t, asks, 2*time.Second)
	if count.Load() < 2 {
		t.Fatal("undeclared emit did not ask")
	}
	result := call(t, f, "subscribers", map[string]any{}, "since")
	raw, _ := json.Marshal(result)
	if !bytes.Contains(raw, []byte(f.fixed.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"))) {
		t.Fatal(string(raw))
	}
	f.stop(t)
}

// R-CBMM-PF9D R-9GA0-6UY6
func TestDrivenDeliveryTimeoutAndRetry(t *testing.T) {
	attempts := make(chan int, 8)
	var count atomic.Int32
	socket := socketServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/declarations" {
			_, _ = io.WriteString(w, `{"emits":[{"event":"repo.pushed","attrs":["private_attr"]}],"accepts":["repo.pushed"]}`)
			return
		}
		n := int(count.Add(1))
		attempts <- n
		if n == 1 {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `{"outcome":"error","error":"rejected"}`)
	})
	path := filepath.Join(t.TempDir(), "services")
	writeServices(t, path, "", []map[string]any{entry("repos", socket)})
	timeouts, backoffs := make(chan scheduled, 8), make(chan scheduled, 8)
	f := startRun(t, t.TempDir(), map[string]string{"IKIGENBA_SERVICES": path, "EVENTS_DELIVERY_TIMEOUT_SECONDS": "7", "EVENTS_DELIVERY_ATTEMPTS": "2"}, func(f *runFixture) {
		f.p.TimeoutAfter = controlledTimer(timeouts)
		f.p.BackoffAfter = controlledTimer(backoffs)
	})
	emit(t, f, emitted(f, 1, 0), 204)
	first := nextSchedule(t, timeouts, 7*time.Second)
	select {
	case n := <-attempts:
		if n != 1 {
			t.Fatal(n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first attempt")
	}
	first.channel <- f.fixed
	backoff := nextSchedule(t, backoffs, time.Second)
	select {
	case <-attempts:
		t.Fatal("retry before driven backoff")
	default:
	}
	backoff.channel <- f.fixed
	_ = nextSchedule(t, timeouts, 7*time.Second)
	select {
	case n := <-attempts:
		if n != 2 {
			t.Fatal(n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("retry missing")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		result := call(t, f, "subscribers", map[string]any{}, "retry-subscribers")
		raw, _ := json.Marshal(result)
		if bytes.Contains(raw, []byte("paused")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(string(raw))
		}
	}
	select {
	case <-backoffs:
		t.Fatal("backoff after configured attempt limit")
	default:
	}
	f.stop(t)
}

// R-9GA0-6UY6
func TestConfiguredInflightLimit(t *testing.T) {
	inFlight := make(chan string, 8)
	release := make(chan struct{})
	var live, maximum atomic.Int32
	socketFor := func(name string) string {
		return socketServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/declarations" {
				_, _ = io.WriteString(w, `{"emits":[{"event":"repo.pushed","attrs":["private_attr"]}],"accepts":["repo.pushed"]}`)
				return
			}
			n := live.Add(1)
			defer live.Add(-1)
			for {
				old := maximum.Load()
				if n <= old || maximum.CompareAndSwap(old, n) {
					break
				}
			}
			inFlight <- name
			select {
			case <-release:
				_, _ = io.WriteString(w, `{"outcome":"ok"}`)
			case <-r.Context().Done():
			}
		})
	}
	path := filepath.Join(t.TempDir(), "services")
	writeServices(t, path, "", []map[string]any{entry("repos", socketFor("repos")), entry("scripts", socketFor("scripts"))})
	f := startRun(t, t.TempDir(), map[string]string{"IKIGENBA_SERVICES": path, "EVENTS_INFLIGHT_MAX": "1"})
	emit(t, f, emitted(f, 1, 0), 204)
	select {
	case <-inFlight:
	case <-time.After(3 * time.Second):
		t.Fatal("first subscriber")
	}
	select {
	case <-inFlight:
		t.Fatal("two concurrent deliveries")
	default:
	}
	release <- struct{}{}
	select {
	case <-inFlight:
	case <-time.After(3 * time.Second):
		t.Fatal("second subscriber")
	}
	release <- struct{}{}
	f.stop(t)
	if maximum.Load() != 1 {
		t.Fatal(maximum.Load())
	}
}
