// Package scheduler serializes trigger changes and fires on an injected clock.
package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/cron/internal/store"
	"github.com/ikigenba/ikigenba/cron/internal/trail"
	cronparser "github.com/robfig/cron/v3"
)

// Config supplies the store, event queues and execution seams.
type Config struct {
	Store     *store.Store
	Events    *events.Emitter
	Telemetry *telemetry.Writer
	Now       func() time.Time
	After     func(d time.Duration) <-chan time.Time
	Rand      io.Reader
}

// Scheduler keeps only next slots; the store owns all trigger records.
type Scheduler struct {
	mu         sync.Mutex
	cfg        Config
	ctx        context.Context
	next       map[string]time.Time
	timer      <-chan time.Time
	generation uint64
	changed    chan struct{}
	done       chan struct{}
	finished   chan struct{}
	stopped    bool
}

// NextSlot finds the first UTC schedule slot strictly after after.
func NextSlot(when string, after time.Time) (time.Time, bool) {
	if !store.ValidWhen(when) {
		return time.Time{}, false
	}
	schedule, err := cronparser.ParseStandard(when)
	if err != nil {
		return time.Time{}, false
	}
	next := schedule.Next(after.UTC())
	if next.IsZero() {
		return time.Time{}, false
	}
	return next.UTC(), true
}

// Start computes fresh next slots and installs its first timer before returning.
func Start(ctx context.Context, cfg Config) (*Scheduler, error) {
	triggers, err := cfg.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.After == nil {
		cfg.After = time.After
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	s := &Scheduler{cfg: cfg, ctx: ctx, next: make(map[string]time.Time), changed: make(chan struct{}, 1), done: make(chan struct{}), finished: make(chan struct{})}
	now := cfg.Now()
	for _, t := range triggers {
		s.setNext(t, now)
	}
	s.wait(false)
	go s.run()
	return s, nil
}

func (s *Scheduler) setNext(t store.Trigger, now time.Time) {
	delete(s.next, t.ID)
	if t.Status != store.Active {
		return
	}
	if t.LastFired.After(now) {
		now = t.LastFired
	}
	if next, ok := NextSlot(t.When, now); ok {
		s.next[t.ID] = next
	}
}

func (s *Scheduler) wait(failed bool) {
	if s.stopped {
		return
	}
	duration := time.Minute
	if !failed {
		now := s.cfg.Now()
		for _, next := range s.next {
			if d := next.Sub(now); d < duration {
				duration = d
			}
		}
		if duration < 0 {
			duration = 0
		}
	}
	s.timer = s.cfg.After(duration)
	s.generation++
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func (s *Scheduler) run() {
	defer close(s.finished)
	for {
		s.mu.Lock()
		timer, generation := s.timer, s.generation
		s.mu.Unlock()
		select {
		case <-s.done:
			return
		case <-s.ctx.Done():
			return
		case <-s.changed:
			continue
		case <-timer:
			s.mu.Lock()
			if !s.stopped && s.ctx.Err() == nil && generation == s.generation {
				s.wake()
			}
			s.mu.Unlock()
		}
	}
}

func (s *Scheduler) wake() {
	now := s.cfg.Now()
	failed := false
	triggers, err := s.cfg.Store.List(context.Background())
	if err != nil {
		for _, next := range s.next {
			if !next.After(now) {
				failed = true
			}
		}
		s.wait(failed)
		return
	}
	for _, t := range triggers {
		if s.ctx.Err() != nil {
			return
		}
		slot, ok := s.next[t.ID]
		if !ok || t.Status != store.Active || slot.After(now) {
			continue
		}
		for {
			next, exists := NextSlot(t.When, slot)
			if !exists || next.After(now) {
				break
			}
			slot = next
		}
		bytes := make([]byte, 16)
		if _, err := io.ReadFull(s.cfg.Rand, bytes); err != nil {
			failed = true
			continue
		}
		if _, err := s.cfg.Store.SetLastFired(context.Background(), t.ID, slot); err != nil {
			failed = true
			continue
		}
		ctx := identity.NewContext(context.Background(), identity.Caller{UserID: t.OwnerID, Email: t.OwnerEmail, RequestID: hex.EncodeToString(bytes)})
		trail.Fire(ctx, s.cfg.Telemetry, s.cfg.Events, t, slot)
		s.setNext(t, slot)
	}
	s.wait(failed)
}

// Stop waits for an ongoing fire and permanently ends this scheduler's loop.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.stopped {
		s.stopped = true
		close(s.done)
	}
	s.mu.Unlock()
	<-s.finished
}

// Next returns a trigger's in-memory next slot.
func (s *Scheduler) Next(id string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, ok := s.next[id]
	return next, ok
}

// Create stores a trigger and queues its created event.
func (s *Scheduler) Create(ctx context.Context, d store.Draft) (store.Trigger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.cfg.Store.Create(ctx, d)
	if err != nil {
		return store.Trigger{}, err
	}
	s.setNext(t, s.cfg.Now())
	trail.Lifecycle(ctx, s.cfg.Telemetry, s.cfg.Events, trail.Created, t)
	s.wait(false)
	return t, nil
}

func (s *Scheduler) owned(ctx context.Context, owner, slug string) (store.Trigger, error) {
	t, err := s.cfg.Store.Get(ctx, slug)
	if err != nil {
		return store.Trigger{}, err
	}
	if t.OwnerID != owner {
		return store.Trigger{}, store.ErrNotFound
	}
	return t, nil
}

// Update changes a schedule without emitting lifecycle events.
func (s *Scheduler) Update(ctx context.Context, owner, slug, when string) (store.Trigger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.owned(ctx, owner, slug)
	if err != nil {
		return store.Trigger{}, err
	}
	if !store.ValidWhen(when) {
		return store.Trigger{}, store.ErrInvalid
	}
	if t.When != when {
		t, err = s.cfg.Store.SetWhen(ctx, t.ID, when)
		if err != nil {
			return store.Trigger{}, err
		}
		s.setNext(t, s.cfg.Now())
	}
	s.wait(false)
	return t, nil
}

func (s *Scheduler) status(ctx context.Context, owner, slug, status, kind string) (store.Trigger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.owned(ctx, owner, slug)
	if err != nil {
		return store.Trigger{}, err
	}
	if t.Status != status {
		t, err = s.cfg.Store.SetStatus(ctx, t.ID, status)
		if err != nil {
			return store.Trigger{}, err
		}
		s.setNext(t, s.cfg.Now())
		trail.Lifecycle(ctx, s.cfg.Telemetry, s.cfg.Events, kind, t)
	}
	s.wait(false)
	return t, nil
}

// Pause removes an active trigger's next slot.
func (s *Scheduler) Pause(ctx context.Context, owner, slug string) (store.Trigger, error) {
	return s.status(ctx, owner, slug, store.Paused, trail.Paused)
}

// Resume computes a paused trigger's next slot from the current clock.
func (s *Scheduler) Resume(ctx context.Context, owner, slug string) (store.Trigger, error) {
	return s.status(ctx, owner, slug, store.Active, trail.Resumed)
}

// Delete removes a trigger and queues its deleted event.
func (s *Scheduler) Delete(ctx context.Context, owner, slug string) (store.Trigger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.owned(ctx, owner, slug)
	if err != nil {
		return store.Trigger{}, err
	}
	t, err = s.cfg.Store.Delete(ctx, t.ID)
	if err != nil {
		return store.Trigger{}, err
	}
	delete(s.next, t.ID)
	trail.Lifecycle(ctx, s.cfg.Telemetry, s.cfg.Events, trail.Deleted, t)
	s.wait(false)
	return t, nil
}
