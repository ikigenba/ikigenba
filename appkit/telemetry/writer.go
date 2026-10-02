package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
)

// Sink receives one event at a time from a Writer.
type Sink interface {
	Deliver(context.Context, Event) error
}

// ErrRejected marks an event that cannot succeed on retry.
var ErrRejected = errors.New("event rejected")

// Delivery limits bound buffering and retries.
const (
	QueueCapacity                = 1024
	Attempts                     = 3
	RetryBackoff   time.Duration = 50 * time.Millisecond
	AttemptTimeout time.Duration = time.Second
)

// Config supplies a service's event delivery and deterministic runtime hooks.
type Config struct {
	Service, Version string
	Sink             Sink
	Stderr           io.Writer
	Now              func() time.Time
	Sleep            func(context.Context, time.Duration)
	Rand             io.Reader
}

type queuedEvent struct {
	event    Event
	sequence uint64
}

// Writer queues events and delivers them through one sender.
type Writer struct {
	done                     chan struct{}
	senderDone               chan struct{}
	cfg                      Config
	mu                       sync.Mutex
	stderrMu                 sync.Mutex
	randMu                   sync.Mutex
	queue                    []queuedEvent
	inflight                 *queuedEvent
	next, completed          uint64
	ready, stopping, stopped bool
	changed                  chan struct{}
	wake                     chan struct{}
	ctx                      context.Context
	cancel                   context.CancelFunc
}

// New starts a sender for cfg.Service.
func New(cfg Config) *Writer {
	if cfg.Service == "" {
		panic("service name is empty")
	}
	if cfg.Sink == nil {
		cfg.Sink = NewSocketSink()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = realPause
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Writer{cfg: cfg, changed: make(chan struct{}), wake: make(chan struct{}, 1), done: make(chan struct{}), senderDone: make(chan struct{}), ctx: ctx, cancel: cancel}
	go w.send()
	return w
}

func realPause(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// Now reads the clock once at UTC microsecond resolution.
func (w *Writer) Now() time.Time { return w.cfg.Now().UTC().Truncate(time.Microsecond) }

func (w *Writer) mintRequestID() (string, error) {
	var b [16]byte
	w.randMu.Lock()
	_, err := io.ReadFull(w.cfg.Rand, b[:])
	w.randMu.Unlock()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (w *Writer) form(ctx context.Context, name string, attrs Attrs) (Event, bool) {
	e := Event{Time: w.Now(), Service: w.cfg.Service, Name: name, Attrs: make(Attrs, len(attrs))}
	if ctx != nil {
		if c, ok := identity.FromContext(ctx); ok {
			e.RequestID = c.RequestID
			e.User = c.UserID
		}
	}
	valid := validEventName(name) && e.Time.Year() >= 0 && e.Time.Year() <= 9999
	for key, value := range attrs {
		basic, err := basicAttribute(value)
		if err != nil {
			valid = false
			basic = nil
		}
		if !validAttributeKey(key) {
			valid = false
		}
		e.Attrs[key] = basic
	}
	return e, valid
}

// Emit copies and queues an event without waiting for delivery.
func (w *Writer) Emit(ctx context.Context, name string, attrs Attrs) {
	e, valid := w.form(ctx, name, attrs)
	if !valid {
		w.writeEvent("malformed", e)
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopping || len(w.queue) >= QueueCapacity {
		w.writeEvent("undelivered", e)
		return
	}
	w.enqueue(e)
}

func (w *Writer) enqueue(e Event) {
	w.next++
	w.queue = append(w.queue, queuedEvent{e, w.next})
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Writer) notify() { close(w.changed); w.changed = make(chan struct{}) }

func (w *Writer) writeEvent(kind string, e Event) {
	body, err := marshalEnvelope(e)
	if err != nil {
		return
	}
	line := append([]byte(w.cfg.Service+": "+kind+" event: "), body...)
	line = append(line, '\n')
	w.stderrMu.Lock()
	defer w.stderrMu.Unlock()
	if w.cfg.Stderr != nil {
		_, _ = w.cfg.Stderr.Write(line)
	}
}

// Ready records the service's first ready transition.
func (w *Writer) Ready() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ready || w.stopping {
		return
	}
	w.ready = true
	e, valid := w.form(context.Background(), "service.started", Attrs{"version": w.cfg.Version})
	if !valid {
		w.writeEvent("malformed", e)
		return
	}
	if len(w.queue) >= QueueCapacity {
		w.writeEvent("undelivered", e)
		return
	}
	w.enqueue(e)
}

// Flush waits for the events queued before this call.
func (w *Writer) Flush(ctx context.Context) error {
	w.mu.Lock()
	target := w.next
	for w.completed < target {
		changed := w.changed
		w.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
		w.mu.Lock()
	}
	w.mu.Unlock()
	return nil
}

// Shutdown queues the last event and drains until ctx ends.
func (w *Writer) Shutdown(ctx context.Context, reason string) {
	w.mu.Lock()
	if w.stopping {
		w.mu.Unlock()
		<-w.done
		return
	}
	w.stopping = true
	e, valid := w.form(ctx, "service.stopping", Attrs{"reason": reason})
	if valid {
		w.enqueue(e)
	} else {
		w.writeEvent("malformed", e)
	}
	w.mu.Unlock()
	stop := context.AfterFunc(ctx, w.cancel)
	defer stop()
	_ = w.Flush(ctx)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	w.cancel()
	if w.inflight != nil {
		w.writeEvent("undelivered", w.inflight.event)
		w.inflight = nil
	}
	for _, pending := range w.queue {
		w.writeEvent("undelivered", pending.event)
	}
	w.queue = nil
	w.completed = w.next
	w.notify()
	close(w.done)
}

func (w *Writer) send() {
	defer close(w.senderDone)
	for {
		w.mu.Lock()
		if w.stopped {
			w.mu.Unlock()
			return
		}
		if len(w.queue) == 0 {
			w.mu.Unlock()
			select {
			case <-w.wake:
			case <-w.ctx.Done():
				return
			}
			continue
		}
		pending := w.queue[0]
		w.queue = w.queue[1:]
		w.inflight = &pending
		w.mu.Unlock()
		err := w.deliver(pending.event)
		w.mu.Lock()
		if w.stopped || w.ctx.Err() != nil {
			w.mu.Unlock()
			return
		}
		if err != nil {
			w.writeEvent("undelivered", pending.event)
		}
		w.inflight = nil
		w.completed = pending.sequence
		w.notify()
		w.mu.Unlock()
	}
}

func (w *Writer) deliver(e Event) error {
	var err error
	for attempt := 0; attempt < Attempts; attempt++ {
		if w.ctx.Err() != nil {
			return w.ctx.Err()
		}
		ctx, cancel := context.WithTimeout(w.ctx, AttemptTimeout)
		err = w.cfg.Sink.Deliver(ctx, e)
		cancel()
		if err == nil || errors.Is(err, ErrRejected) {
			return err
		}
		if attempt+1 < Attempts && w.ctx.Err() == nil {
			w.cfg.Sleep(w.ctx, RetryBackoff<<attempt)
		}
	}
	return err
}
