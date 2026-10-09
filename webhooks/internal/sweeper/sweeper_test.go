package sweeper_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/webhooks"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
	"github.com/ikigenba/ikigenba/webhooks/internal/sweeper"
)

var base = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Set(t time.Time)     { c.mu.Lock(); c.t = t; c.mu.Unlock() }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// timer hands each wait to the test, which delivers on it.
type timer struct {
	waits chan wait
}

type wait struct {
	d  time.Duration
	ch chan time.Time
}

func (tm *timer) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	tm.waits <- wait{d, ch}
	return ch
}

func (tm *timer) next(t *testing.T) wait {
	t.Helper()
	select {
	case w := <-tm.waits:
		return w
	case <-time.After(10 * time.Second):
		t.Fatal("no wait began")
		return wait{}
	}
}

type setup struct {
	st    *store.Store
	clock *clock
	hook  store.Webhook
}

func newSetup(t *testing.T) *setup {
	t.Helper()
	c := &clock{t: base}
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "state", "webhooks.db"), Migrations: webhooks.Migrations(), Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	st := store.New(d, store.Config{Now: c.Now})
	h, _, err := st.Create(context.Background(), store.Draft{Slug: "tick", Scheme: store.Bearer, OwnerID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	return &setup{st: st, clock: c, hook: h}
}

func (s *setup) receiveAt(t *testing.T, at time.Time) string {
	t.Helper()
	s.clock.Set(at)
	d, err := s.st.Receive(context.Background(), s.hook.ID, store.Arrival{Body: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	return d.ID
}

func (s *setup) present(t *testing.T, id string) bool {
	t.Helper()
	_, _, err := s.st.Delivery(context.Background(), id)
	if errors.Is(err, store.ErrNotFound) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}

func surface(func(context.Context, sweeper.Config) error, func(context.Context, sweeper.Config) *sweeper.Sweeper, func(*sweeper.Sweeper)) {
}

func TestDeclarations(t *testing.T) {
	// R-XZER-NLKZ
	if sweeper.Interval != time.Hour {
		t.Fatal(sweeper.Interval)
	}
	cfg := sweeper.Config{Store: nil, Retention: time.Hour, Now: time.Now, After: time.After}
	_ = cfg
	surface(sweeper.Sweep, sweeper.Start, (*sweeper.Sweeper).Stop)
}

func TestSweep(t *testing.T) {
	s := newSetup(t)
	old := s.receiveAt(t, base)
	edge := s.receiveAt(t, base.Add(time.Hour))
	fresh := s.receiveAt(t, base.Add(2*time.Hour))
	s.clock.Set(base.Add(49 * time.Hour))
	// R-Y0MO-1DBO: earlier than Now less the window goes; at the cut-off stays.
	if err := sweeper.Sweep(context.Background(), sweeper.Config{Store: s.st, Retention: 48 * time.Hour, Now: s.clock.Now}); err != nil {
		t.Fatal(err)
	}
	if s.present(t, old) || !s.present(t, edge) || !s.present(t, fresh) {
		t.Fatal("wrong deliveries swept")
	}
}

func TestStartAndStop(t *testing.T) {
	s := newSetup(t)
	first := s.receiveAt(t, base)
	second := s.receiveAt(t, base.Add(10*time.Hour))
	third := s.receiveAt(t, base.Add(20*time.Hour))
	s.clock.Set(base.Add(30 * time.Hour))
	tm := &timer{waits: make(chan wait)}
	// R-Y0MO-1DBO: one sweep before Start returns.
	sw := sweeper.Start(context.Background(), sweeper.Config{Store: s.st, Retention: 25 * time.Hour, Now: s.clock.Now, After: tm.After})
	if s.present(t, first) || !s.present(t, second) {
		t.Fatal("start did not sweep once")
	}
	w := tm.next(t)
	if w.d != sweeper.Interval {
		t.Fatal(w.d)
	}
	// Moving the clock alone sweeps nothing.
	s.clock.Add(6 * time.Hour)
	if !s.present(t, second) {
		t.Fatal("swept without the timer")
	}
	w.ch <- s.clock.Now()
	w = tm.next(t) // the sweep finished before the next wait began
	if s.present(t, second) || !s.present(t, third) {
		t.Fatal("timer did not sweep")
	}
	done := make(chan struct{})
	go func() { sw.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("stop did not return")
	}
	// After Stop no sweep begins, whatever the timer delivers.
	s.clock.Add(100 * time.Hour)
	w.ch <- s.clock.Now()
	if !s.present(t, third) {
		t.Fatal("swept after stop")
	}
	sw.Stop()
}
