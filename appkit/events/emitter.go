package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Sink receives events from the emitter's single sender.
type Sink interface {
	Deliver(ctx context.Context, e Event) error
}

// ErrRejected marks a permanent rejection of an event.
var ErrRejected = errors.New("event rejected")

// Emission declares one emitted event and its attribute keys.
type Emission struct {
	Event string
	Attrs []string
}

// Config provides delivery settings and runtime hooks.
type Config struct {
	Service       string
	Sink          Sink
	Stderr        io.Writer
	Now           func() time.Time
	Sleep         func(ctx context.Context, d time.Duration)
	Rand          io.Reader
	Telemetry     *telemetry.Writer
	QueueCapacity int
	RetryWindow   time.Duration
	Emits         []Emission
}

// Delivery defaults and limits.
const (
	DefaultQueueCapacity               = 1024
	DefaultRetryWindow   time.Duration = 5 * time.Minute
	RetryBackoff         time.Duration = 100 * time.Millisecond
	MaxRetryBackoff      time.Duration = 30 * time.Second
	AttemptTimeout       time.Duration = 5 * time.Second
)

type pendingEvent struct {
	event    Event
	sequence uint64
	started  bool
}

// Emitter stamps, queues, and delivers events in order.
type Emitter struct {
	cfg               Config
	mu                sync.Mutex
	stderrMu          sync.Mutex
	randMu            sync.Mutex
	fallbackIDs       map[string]struct{}
	queue             []pendingEvent
	inflight          *pendingEvent
	next, completed   uint64
	stopping, stopped bool
	changed, done     chan struct{}
	wake              chan struct{}
	ctx               context.Context
	shutdownCtx       context.Context
	cancel            context.CancelFunc
}

// New validates declarations and starts the sender.
func New(cfg Config) *Emitter {
	if cfg.Service == "" {
		panic("service name is empty")
	}
	seen := make(map[string]bool)
	for _, emission := range cfg.Emits {
		if !validEventName(emission.Event) || seen[emission.Event] {
			panic("invalid or duplicate emission name")
		}
		seen[emission.Event] = true
		keys := make(map[string]bool)
		for _, key := range emission.Attrs {
			if !validAttributeKey(key) || keys[key] {
				panic("invalid or duplicate emission attribute")
			}
			keys[key] = true
		}
	}
	cfg.Emits = copyEmissions(cfg.Emits)
	if cfg.Sink == nil {
		cfg.Sink = NewSocketSink()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = pause
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = DefaultQueueCapacity
	}
	if cfg.RetryWindow <= 0 {
		cfg.RetryWindow = DefaultRetryWindow
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &Emitter{cfg: cfg, ctx: ctx, cancel: cancel, changed: make(chan struct{}), done: make(chan struct{}), wake: make(chan struct{}, 1), fallbackIDs: make(map[string]struct{})}
	go e.send()
	return e
}

func copyEmissions(src []Emission) []Emission {
	dst := make([]Emission, len(src))
	for i, emission := range src {
		dst[i] = Emission{Event: emission.Event, Attrs: make([]string, len(emission.Attrs))}
		copy(dst[i].Attrs, emission.Attrs)
	}
	return dst
}

// Emits returns independent copies of the fixed declarations.
func (e *Emitter) Emits() []Emission { return copyEmissions(e.cfg.Emits) }

func pause(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

func (e *Emitter) mintID() string {
	e.randMu.Lock()
	defer e.randMu.Unlock()
	var b [8]byte
	if _, err := io.ReadFull(e.cfg.Rand, b[:]); err == nil {
		return "evt_" + hex.EncodeToString(b[:])
	}
	for {
		_, _ = rand.Read(b[:])
		id := "evt_" + hex.EncodeToString(b[:])
		if _, used := e.fallbackIDs[id]; !used {
			e.fallbackIDs[id] = struct{}{}
			return id
		}
	}
}

func (e *Emitter) form(ctx context.Context, name string, attrs Attrs) (Event, bool) {
	event := Event{ID: e.mintID(), Time: e.cfg.Now().UTC().Truncate(time.Microsecond), Service: e.cfg.Service, Name: name, Attrs: make(Attrs, len(attrs))}
	if ctx != nil {
		if caller, ok := identity.FromContext(ctx); ok {
			event.RequestID = caller.RequestID
			event.User = caller.UserID
		}
		if cause, ok := FromContext(ctx); ok {
			event.Cause = cause.ID
			event.Depth = cause.Depth + 1
		}
	}
	valid := true
	for key, value := range attrs {
		basic, err := basicAttribute(value)
		if err != nil {
			valid = false
			basic = nil
		}
		event.Attrs[key] = basic
	}
	valid = valid && validEmitted(event)
	declared := false
	for _, emission := range e.cfg.Emits {
		if emission.Event != name {
			continue
		}
		declared = len(emission.Attrs) == len(attrs)
		for _, key := range emission.Attrs {
			if _, ok := attrs[key]; !ok {
				declared = false
			}
		}
		break
	}
	return event, valid && declared
}

// Emit copies the caller's attributes and returns without awaiting delivery.
func (e *Emitter) Emit(ctx context.Context, name string, attrs Attrs) {
	event, valid := e.form(ctx, name, attrs)
	if !valid {
		e.writeEvent("malformed", event)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping || e.queuedCount() >= e.cfg.QueueCapacity {
		e.lost(event)
		return
	}
	e.next++
	e.queue = append(e.queue, pendingEvent{event: event, sequence: e.next})
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Emitter) writeEvent(kind string, event Event) {
	body, err := marshalEnvelope(event)
	if err != nil {
		return
	}
	line := append([]byte(e.cfg.Service+": "+kind+" event: "), body...)
	line = append(line, '\n')
	e.stderrMu.Lock()
	defer e.stderrMu.Unlock()
	if e.cfg.Stderr != nil {
		_, _ = e.cfg.Stderr.Write(line)
	}
}

func (e *Emitter) lost(event Event) {
	e.writeEvent("lost", event)
	if e.cfg.Telemetry != nil {
		ctx := identity.NewContext(context.Background(), identity.Caller{UserID: event.User, RequestID: event.RequestID})
		e.cfg.Telemetry.Emit(ctx, "event.lost", telemetry.Attrs{"event": event.ID, "cause": event.Cause})
	}
}

// Ready has no bus lifecycle event to emit.
func (*Emitter) Ready() {}

func (e *Emitter) queuedCount() int {
	n := len(e.queue)
	if e.inflight != nil && !e.inflight.started {
		n++
	}
	return n
}

func (e *Emitter) notify() { close(e.changed); e.changed = make(chan struct{}) }

// Flush waits for every event already queued to finish.
func (e *Emitter) Flush(ctx context.Context) error {
	e.mu.Lock()
	target := e.next
	for e.completed < target {
		changed := e.changed
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
		e.mu.Lock()
	}
	e.mu.Unlock()
	return nil
}

// Shutdown drains queued events, reporting any remaining ones lost when ctx ends.
func (e *Emitter) Shutdown(ctx context.Context) {
	e.mu.Lock()
	if e.stopping {
		e.mu.Unlock()
		select {
		case <-e.done:
		case <-ctx.Done():
		}
		return
	}
	e.stopping = true
	e.shutdownCtx = ctx
	stop := context.AfterFunc(ctx, e.cancel)
	if ctx.Err() != nil {
		e.cancel()
	}
	e.mu.Unlock()
	defer stop()
	_ = e.Flush(ctx)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopped = true
	e.cancel()
	if e.inflight != nil {
		e.lost(e.inflight.event)
		e.inflight = nil
	}
	for _, pending := range e.queue {
		e.lost(pending.event)
	}
	e.queue = nil
	e.completed = e.next
	e.notify()
	close(e.done)
}

func (e *Emitter) send() {
	for {
		e.mu.Lock()
		if e.stopped || e.ctx.Err() != nil {
			e.mu.Unlock()
			return
		}
		if len(e.queue) == 0 {
			e.mu.Unlock()
			select {
			case <-e.wake:
			case <-e.ctx.Done():
				return
			}
			continue
		}
		pending := e.queue[0]
		e.queue = e.queue[1:]
		e.inflight = &pending
		e.mu.Unlock()
		err := e.deliver(pending.event)
		e.mu.Lock()
		if e.stopped || e.ctx.Err() != nil {
			e.mu.Unlock()
			return
		}
		if err != nil {
			e.lost(pending.event)
		}
		e.inflight = nil
		e.completed = pending.sequence
		e.notify()
		e.mu.Unlock()
	}
}

func (e *Emitter) deliver(event Event) error {
	started := e.cfg.Now()
	backoff := RetryBackoff
	for {
		if e.ctx.Err() != nil {
			return e.ctx.Err()
		}
		e.mu.Lock()
		if e.inflight != nil {
			e.inflight.started = true
		}
		e.mu.Unlock()
		ctx, cancel := context.WithTimeout(e.ctx, AttemptTimeout)
		err := e.cfg.Sink.Deliver(emitterContext{ctx, e}, event)
		cancel()
		if err == nil || errors.Is(err, ErrRejected) {
			return err
		}
		ended := e.cfg.Now()
		if ended.Sub(started) >= e.cfg.RetryWindow {
			return err
		}
		if e.ctx.Err() != nil {
			return e.ctx.Err()
		}
		e.cfg.Sleep(emitterContext{e.ctx, e}, backoff)
		if backoff < MaxRetryBackoff {
			backoff *= 2
			if backoff > MaxRetryBackoff {
				backoff = MaxRetryBackoff
			}
		}
	}
}

// Observe the shutdown context synchronously, including before its cancellation callback runs.
type emitterContext struct {
	context.Context
	emitter *Emitter
}

func (c emitterContext) checkShutdown() {
	c.emitter.mu.Lock()
	shutdown := c.emitter.shutdownCtx
	c.emitter.mu.Unlock()
	if shutdown != nil && shutdown.Err() != nil {
		c.emitter.cancel()
	}
}
func (c emitterContext) Err() error            { c.checkShutdown(); return c.Context.Err() }
func (c emitterContext) Done() <-chan struct{} { c.checkShutdown(); return c.Context.Done() }
