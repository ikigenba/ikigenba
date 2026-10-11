package sweeper_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/artifacts"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/sweeper"
	"github.com/ikigenba/ikigenba/artifacts/internal/trail"
)

var base = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) Now() time.Time   { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *clock) Set(at time.Time) { c.mu.Lock(); c.at = at; c.mu.Unlock() }

type setup struct {
	d  *db.DB
	st *store.Store
	c  *clock
	w  *telemetry.Writer
	em *events.Emitter
	ts *telemetry.Capture
	es *events.Capture
}

func newSetup(t *testing.T) *setup {
	t.Helper()
	c := &clock{at: base}
	dir := t.TempDir()
	blobs := filepath.Join(dir, "blobs")
	if err := os.Mkdir(blobs, 0700); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "artifacts.db"), Migrations: artifacts.Migrations(), Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ts, es := &telemetry.Capture{}, &events.Capture{}
	sleep := func(context.Context, time.Duration) {}
	w := telemetry.New(telemetry.Config{Service: "artifacts", Sink: ts, Now: c.Now, Sleep: sleep, Rand: bytes.NewReader(make([]byte, 65536)), Stderr: io.Discard})
	em := events.New(events.Config{Service: "artifacts", Sink: es, Now: c.Now, Sleep: sleep, Rand: bytes.NewReader(make([]byte, 65536)), Stderr: io.Discard, Emits: trail.Emits()})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		em.Shutdown(ctx)
		w.Shutdown(ctx, "test")
	})
	rnd := make([]byte, 65536)
	for i := range rnd {
		rnd[i] = byte(i / 8)
	}
	return &setup{d: d, st: store.New(d, blobs, store.Config{Now: c.Now, Rand: bytes.NewReader(rnd)}), c: c, w: w, em: em, ts: ts, es: es}
}
func (s *setup) cfg() sweeper.Config {
	return sweeper.Config{Store: s.st, Now: s.c.Now, Telemetry: s.w, Events: s.em}
}
func (s *setup) put(t *testing.T, path string, expiry time.Time) store.Artifact {
	t.Helper()
	a, _, err := s.st.Put(context.Background(), store.Draft{Path: path, OwnerID: "owner-" + path, OwnerEmail: path + "@example.test", ContentType: "application/custom", Expires: expiry, Max: 100}, bytes.NewBufferString("payload-"+path))
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func (s *setup) present(t *testing.T, path string) bool {
	t.Helper()
	_, err := s.st.Get(context.Background(), path)
	if errors.Is(err, store.ErrNotFound) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}
func (s *setup) checkEvents(t *testing.T, want []store.Artifact) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.em.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	ts, es := s.ts.Events(), s.es.Events()
	if len(ts) != len(want) || len(es) != len(want) {
		t.Fatalf("trail=%v bus=%v want=%v", ts, es, want)
	}
	for i, a := range want {
		attrs := map[string]any{"artifact": a.ID, "path": a.Path, "bytes": a.Bytes, "content_type": a.ContentType}
		if ts[i].Name != trail.Deleted || es[i].Name != trail.Deleted || ts[i].User != a.OwnerID || es[i].User != a.OwnerID || ts[i].RequestID != "" || es[i].RequestID != "" || es[i].Cause != "" || es[i].Depth != 0 || !reflect.DeepEqual(map[string]any(ts[i].Attrs), attrs) || !reflect.DeepEqual(map[string]any(es[i].Attrs), attrs) {
			t.Fatalf("trail=%+v bus=%+v", ts[i], es[i])
		}
	}
}
func surface(func(context.Context, sweeper.Config) error, func(context.Context, sweeper.Config) *sweeper.Sweeper, func(*sweeper.Sweeper)) {
}

// R-DANI-WKF1
func TestSurface(t *testing.T) {
	surface(sweeper.Sweep, sweeper.Start, (*sweeper.Sweeper).Stop)
	if sweeper.Interval != time.Hour {
		t.Fatal(sweeper.Interval)
	}
	s := newSetup(t)
	cfg := s.cfg()
	cfg.After = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	if err := sweeper.Sweep(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
}

// R-DBVF-AC5Q
func TestSweep(t *testing.T) {
	s := newSetup(t)
	old := s.put(t, "a-old", base.Add(time.Second))
	edge := s.put(t, "b-edge", base.Add(2*time.Second+900*time.Millisecond))
	future := s.put(t, "c-future", base.Add(3*time.Second))
	never := s.put(t, "d-never", time.Time{})
	s.c.Set(base.Add(2*time.Second + 100*time.Millisecond))
	ctx := events.NewContext(identity.NewContext(context.Background(), identity.Caller{UserID: "wrong", RequestID: "wrong"}), events.Cause{ID: "evt_0123456789abcdef", Depth: 7})
	if err := sweeper.Sweep(ctx, s.cfg()); err != nil {
		t.Fatal(err)
	}
	if s.present(t, old.Path) || s.present(t, edge.Path) || !s.present(t, future.Path) || !s.present(t, never.Path) {
		t.Fatal("wrong artifacts swept")
	}
	s.checkEvents(t, []store.Artifact{old, edge})
}

// R-DD3B-O3WF
func TestFailedSweep(t *testing.T) {
	s := newSetup(t)
	a := s.put(t, "expired", base.Add(time.Second))
	s.c.Set(base.Add(2 * time.Second))
	s.d.SetFailing(true)
	if err := sweeper.Sweep(context.Background(), s.cfg()); err == nil {
		t.Fatal("failing database accepted")
	}
	s.d.SetFailing(false)
	if !s.present(t, a.Path) {
		t.Fatal("failed sweep deleted artifact")
	}
	s.checkEvents(t, nil)
}

type wait struct {
	duration time.Duration
	ch       chan time.Time
}
type timer struct{ waits chan wait }

func (tm *timer) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	tm.waits <- wait{d, ch}
	return ch
}
func (tm *timer) next(t *testing.T) wait {
	t.Helper()
	select {
	case w := <-tm.waits:
		if w.duration != sweeper.Interval {
			t.Fatal(w.duration)
		}
		return w
	case <-time.After(5 * time.Second):
		t.Fatal("no timer wait")
		return wait{}
	}
}

// R-DEB8-1VN4
func TestStartTimerFailureAndStop(t *testing.T) {
	s := newSetup(t)
	first := s.put(t, "a-first", base.Add(time.Second))
	second := s.put(t, "b-second", base.Add(3*time.Second))
	third := s.put(t, "c-third", base.Add(5*time.Second))
	s.c.Set(base.Add(2 * time.Second))
	tm := &timer{waits: make(chan wait, 16)}
	cfg := s.cfg()
	cfg.After = tm.After
	sw := sweeper.Start(context.Background(), cfg)
	if s.present(t, first.Path) || !s.present(t, second.Path) {
		t.Fatal("initial sweep incomplete")
	}
	w := tm.next(t)
	s.c.Set(base.Add(4 * time.Second))
	if !s.present(t, second.Path) {
		t.Fatal("clock alone swept")
	}
	s.d.SetFailing(true)
	w.ch <- s.c.Now()
	w = tm.next(t)
	s.d.SetFailing(false)
	if !s.present(t, second.Path) {
		t.Fatal("failed timer sweep deleted")
	}
	w.ch <- s.c.Now()
	w = tm.next(t)
	if s.present(t, second.Path) || !s.present(t, third.Path) {
		t.Fatal("retry sweep failed")
	}
	sw.Stop()
	s.c.Set(base.Add(6 * time.Second))
	w.ch <- s.c.Now()
	if !s.present(t, third.Path) {
		t.Fatal("sweep after stop")
	}
	sw.Stop()
	s.checkEvents(t, []store.Artifact{first, second})
}

// R-DEB8-1VN4
func TestStartContinuesAfterInitialFailure(t *testing.T) {
	s := newSetup(t)
	a := s.put(t, "expired", base.Add(time.Second))
	s.c.Set(base.Add(2 * time.Second))
	s.d.SetFailing(true)
	tm := &timer{waits: make(chan wait, 16)}
	cfg := s.cfg()
	cfg.After = tm.After
	sw := sweeper.Start(context.Background(), cfg)
	t.Cleanup(sw.Stop)
	w := tm.next(t)
	s.d.SetFailing(false)
	if !s.present(t, a.Path) {
		t.Fatal("failed initial sweep deleted")
	}
	s.checkEvents(t, nil)
	w.ch <- s.c.Now()
	_ = tm.next(t)
	if s.present(t, a.Path) {
		t.Fatal("no retry after initial failure")
	}
	s.checkEvents(t, []store.Artifact{a})
}

// R-DEB8-1VN4
func TestStopJoinsSweepInProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetup(t)
		a := s.put(t, "expired", base.Add(time.Second))
		tm := &timer{waits: make(chan wait, 16)}
		cfg := s.cfg()
		cfg.After = tm.After
		entered, release := make(chan struct{}), make(chan struct{})
		var calls int
		cfg.Now = func() time.Time {
			calls++
			if calls == 2 {
				close(entered)
				<-release
			}
			return s.c.Now()
		}
		sw := sweeper.Start(context.Background(), cfg)
		w := tm.next(t)
		s.c.Set(base.Add(2 * time.Second))
		w.ch <- s.c.Now()
		<-entered
		stopped := make(chan struct{})
		go func() { sw.Stop(); close(stopped) }()
		// Wait settles both goroutines: the sweep is blocked on release, and
		// Stop must be blocked joining it. A nonjoining Stop has already returned.
		synctest.Wait()
		select {
		case <-stopped:
			close(release)
			synctest.Wait()
			t.Fatal("Stop returned while sweep was blocked")
		default:
		}
		close(release)
		synctest.Wait()
		select {
		case <-stopped:
		default:
			t.Fatal("Stop did not return after sweep completed")
		}
		if s.present(t, a.Path) {
			t.Fatal("Stop returned before deletion completed")
		}
		s.checkEvents(t, []store.Artifact{a})
	})
}
