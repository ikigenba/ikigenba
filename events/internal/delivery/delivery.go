// Package delivery sends retained events to their subscribers.
package delivery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events/internal/settings"
	"github.com/ikigenba/ikigenba/events/internal/store"
)

// Failed delivery copy is shared with consumers of the failure reason.
const (
	NoAnswerWithinOne  = "no answer from %s within %d second"
	NoAnswerWithin     = "no answer from %s within %d seconds"
	AnsweredWithStatus = "answered with status %d"
	NoAnswer           = "no answer from %s"
)

// Config supplies the log, destinations and delivery scheduling hooks.
type Config struct {
	Store        *store.Store
	Services     string
	Telemetry    *telemetry.Writer
	Settings     settings.Settings
	TimeoutAfter func(d time.Duration) <-chan time.Time
	BackoffAfter func(d time.Duration) <-chan time.Time
}

type subscriberState struct {
	since    time.Time
	status   store.Status
	seq      int64
	failures int64
	active   bool
	wait     <-chan time.Time
	recheck  bool
	cancel   context.CancelFunc
}

// Loop schedules one ordered stream per subscriber.
type Loop struct {
	cfg      Config
	mu       sync.Mutex
	states   map[string]*subscriberState
	inflight int64
	wake     chan struct{}
	changed  chan struct{}
	run      context.Context
}

// New creates a delivery loop; nil scheduling hooks use time.After.
func New(cfg Config) *Loop {
	if cfg.TimeoutAfter == nil {
		cfg.TimeoutAfter = time.After
	}
	if cfg.BackoffAfter == nil {
		cfg.BackoffAfter = time.After
	}
	return &Loop{cfg: cfg, states: make(map[string]*subscriberState), wake: make(chan struct{}, 1), changed: make(chan struct{})}
}

func (l *Loop) signal() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}
func (l *Loop) notify() { close(l.changed); l.changed = make(chan struct{}); l.signal() }

func (l *Loop) wait(st *subscriberState, d time.Duration, recheck bool) {
	if l.run.Err() != nil {
		return
	}
	st.wait = l.cfg.BackoffAfter(d)
	st.recheck = recheck
	timer := st.wait
	ctx := l.run
	go func() {
		select {
		case <-timer:
			l.mu.Lock()
			if st.wait == timer {
				st.wait = nil
				l.signal()
			}
			l.mu.Unlock()
		case <-ctx.Done():
		}
	}()
}

// Run schedules deliveries until canceled, leaving active attempts for Drain.
func (l *Loop) Run(ctx context.Context) {
	l.mu.Lock()
	l.run = ctx
	l.mu.Unlock()
	for ctx.Err() == nil {
		changed := l.cfg.Store.Changed()
		l.scan(ctx)
		select {
		case <-ctx.Done():
			return
		case <-l.wake:
		case <-changed:
			l.mu.Lock()
			for _, st := range l.states {
				if st.recheck {
					st.wait = nil
					st.recheck = false
				}
			}
			l.mu.Unlock()
		}
	}
}

func (l *Loop) target(name string) (string, bool) {
	if name == events.ServiceName {
		return "", false
	}
	list, err := services.Read(l.cfg.Services)
	if err != nil {
		return "", false
	}
	for _, entry := range list {
		if entry.Name == name && entry.Enabled {
			return entry.Socket, true
		}
	}
	return "", false
}

func (l *Loop) scan(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	subs, err := l.cfg.Store.Subscribers(context.Background())
	if err != nil {
		for _, pending := range l.states {
			if pending.recheck && pending.wait != nil {
				return
			}
		}
		st := l.states[""]
		if st == nil {
			st = &subscriberState{}
			l.states[""] = st
		}
		if st.wait == nil {
			l.wait(st, time.Second, true)
		}
		return
	}
	if global := l.states[""]; global != nil && global.wait != nil {
		return
	}
	for _, sub := range subs {
		if ctx.Err() != nil {
			return
		}
		st := l.states[sub.Service]
		if st == nil {
			st = &subscriberState{}
			l.states[sub.Service] = st
		}
		if st.status != sub.Status || !st.since.Equal(sub.Since) {
			st.failures = 0
			st.wait = nil
			st.recheck = false
			st.status = sub.Status
			st.since = sub.Since
		}
		if sub.Status != store.StatusOK || st.active || st.wait != nil {
			continue
		}
		e, ok, err := l.cfg.Store.Next(context.Background(), sub.Service)
		if err != nil {
			l.wait(st, time.Second, true)
			continue
		}
		if !ok {
			continue
		}
		socket, ok := l.target(sub.Service)
		if !ok {
			l.wait(st, time.Second, true)
			continue
		}
		if l.inflight >= l.cfg.Settings.InflightMax {
			continue
		}
		if e.Seq != st.seq {
			st.seq = e.Seq
			st.failures = 0
		}
		if ctx.Err() != nil {
			return
		}
		st.active = true
		l.inflight++
		attemptCtx, cancel := context.WithCancel(context.Background())
		st.cancel = cancel
		deadline := l.cfg.TimeoutAfter(l.cfg.Settings.DeliveryTimeout())
		go l.attempt(attemptCtx, st, sub.Service, socket, e, st.failures+1, deadline)
	}
}

type answer struct {
	result events.Result
	err    error
}

func (l *Loop) attempt(ctx context.Context, st *subscriberState, name, socket string, e events.Event, n int64, deadline <-chan time.Time) {
	transport := telemetry.SocketTransport(socket)
	defer transport.CloseIdleConnections()
	client := telemetry.SiblingClient(l.cfg.Telemetry, name, transport)
	response := make(chan answer, 1)
	go func() {
		r, err := events.Send(ctx, client, name, events.Delivery{Event: e, Attempt: int(n)})
		response <- answer{r, err}
	}()
	var a answer
	timedout, abandoned := false, false
	select {
	case a = <-response:
	case <-deadline:
		timedout = true
		st.cancel()
		a = <-response
	case <-ctx.Done():
		abandoned = true
		a = <-response
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !abandoned {
		l.outcome(st, name, e, n, a, timedout)
	}
	st.cancel()
	st.cancel = nil
	st.active = false
	l.inflight--
	l.notify()
}

func (l *Loop) outcome(st *subscriberState, name string, e events.Event, n int64, a answer, timedout bool) {
	if !timedout && a.err == nil && (a.result.Outcome.Kind() == events.OutcomeOK || a.result.Outcome.Kind() == events.OutcomeSkip) {
		if err := l.cfg.Store.Advance(context.Background(), name, e.Seq); err != nil {
			l.wait(st, time.Second, true)
			return
		}
		kind := "event.delivered"
		if a.result.Outcome.Kind() == events.OutcomeSkip {
			kind = "event.skipped"
		}
		l.cfg.Telemetry.Emit(context.Background(), kind, telemetry.Attrs{"event": e.ID, "service": name})
		st.failures = 0
		return
	}
	st.failures++
	if n >= l.cfg.Settings.DeliveryAttempts {
		subs, err := l.cfg.Store.Subscribers(context.Background())
		if err != nil {
			l.wait(st, time.Second, true)
			return
		}
		wasOK := false
		for _, sub := range subs {
			if sub.Service == name {
				wasOK = sub.Status == store.StatusOK
			}
		}
		message := errorText(name, l.cfg.Settings.DeliveryTimeoutSeconds, a, timedout)
		if err := l.cfg.Store.Pause(context.Background(), name, e.Seq, message); err != nil {
			l.wait(st, time.Second, true)
			return
		}
		if wasOK {
			st.status = store.StatusPaused
			l.cfg.Telemetry.Emit(context.Background(), "subscriber.paused", telemetry.Attrs{"service": name, "event": e.ID, "error": message})
		}
		return
	}
	d := 5 * time.Minute
	if n < 10 {
		d = time.Second * time.Duration(int64(1)<<(n-1))
	}
	if a.result.RetryAfter > d {
		d = a.result.RetryAfter
	}
	l.wait(st, d, false)
}

func errorText(name string, seconds int64, a answer, timedout bool) string {
	if a.err == nil && a.result.Outcome.Kind() == events.OutcomeError && a.result.Outcome.Message() != "" {
		return a.result.Outcome.Message()
	}
	if timedout {
		if seconds == 1 {
			return fmt.Sprintf(NoAnswerWithinOne, name, seconds)
		}
		return fmt.Sprintf(NoAnswerWithin, name, seconds)
	}
	if a.result.Status != 0 {
		return fmt.Sprintf(AnsweredWithStatus, a.result.Status)
	}
	return fmt.Sprintf(NoAnswer, name)
}

// Drain finishes active answers, abandoning connections if its context expires.
func (l *Loop) Drain(ctx context.Context) {
	for {
		l.mu.Lock()
		if l.inflight == 0 {
			l.mu.Unlock()
			return
		}
		changed := l.changed
		if ctx.Err() != nil {
			for _, st := range l.states {
				if st.active {
					st.cancel()
				}
			}
			l.mu.Unlock()
			<-changed
			continue
		}
		l.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
	}
}
