package cli

import (
	"context"
	"io"
	"sync"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// deliveryGate prevents delivery after the shared drain window closes.
type deliveryGate struct {
	mu      sync.Mutex
	drain   context.Context
	aborted bool
}

func (g *deliveryGate) stop(ctx context.Context) { g.mu.Lock(); g.drain = ctx; g.mu.Unlock() }
func (g *deliveryGate) abort()                   { g.mu.Lock(); g.aborted = true; g.mu.Unlock() }
func (g *deliveryGate) allowed(name string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return (!g.aborted || name != "service.stopping") && (g.drain == nil || g.drain.Err() == nil)
}

type gatedTelemetry struct {
	gate *deliveryGate
	sink telemetry.Sink
}

func (s gatedTelemetry) Deliver(ctx context.Context, e telemetry.Event) error {
	if !s.gate.allowed(e.Name) {
		if e.Name == "service.stopping" {
			s.gate.mu.Lock()
			aborted := s.gate.aborted
			s.gate.mu.Unlock()
			if aborted {
				return nil
			}
		}
		return telemetry.ErrRejected
	}
	return s.sink.Deliver(ctx, e)
}

type gatedEvents struct {
	gate *deliveryGate
	sink events.Sink
}

func (s gatedEvents) Deliver(ctx context.Context, e events.Event) error {
	if !s.gate.allowed(e.Name) {
		return events.ErrRejected
	}
	return s.sink.Deliver(ctx, e)
}

type lockedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}

// requests tracks handlers so the stopping event follows their final events.
type requests struct {
	mu      sync.Mutex
	active  int
	changed chan struct{}
}

func (r *requests) begin() { r.mu.Lock(); r.active++; r.mu.Unlock() }
func (r *requests) end() {
	r.mu.Lock()
	r.active--
	close(r.changed)
	r.changed = make(chan struct{})
	r.mu.Unlock()
}
func (r *requests) wait(ctx context.Context) {
	for {
		r.mu.Lock()
		n, ch := r.active, r.changed
		r.mu.Unlock()
		if n == 0 {
			return
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return
		}
	}
}
