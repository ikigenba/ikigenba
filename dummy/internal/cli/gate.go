package cli

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Gate refuses deliveries once the process drain deadline has passed.
type Gate struct {
	mu       sync.Mutex
	next     telemetry.Sink
	deadline time.Time
}

// NewGate wraps the sink receiving deliveries before the drain deadline.
func NewGate(next telemetry.Sink) *Gate { return &Gate{next: next} }

// Deliver forwards the event unless the process drain deadline has passed.
func (g *Gate) Deliver(ctx context.Context, e telemetry.Event) error {
	g.mu.Lock()
	refused := !g.deadline.IsZero() && !time.Now().Before(g.deadline)
	g.mu.Unlock()
	if refused {
		return errors.New("dummy drain deadline elapsed")
	}
	return g.next.Deliver(ctx, e)
}
func (g *Gate) limit(deadline time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.deadline.IsZero() || deadline.Before(g.deadline) {
		g.deadline = deadline
	}
}
func (g *Gate) watch(ctx context.Context, drain time.Duration) func() bool {
	return context.AfterFunc(ctx, func() { g.limit(time.Now().Add(drain)) })
}
