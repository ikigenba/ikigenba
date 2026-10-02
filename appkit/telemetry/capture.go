package telemetry

import (
	"context"
	"sync"
)

// Capture is an in-memory sink, ready to use as its zero value.
type Capture struct {
	mu     sync.Mutex
	events []Event
}

// Deliver records e in delivery order.
func (c *Capture) Deliver(_ context.Context, e Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	return nil
}

// Events returns a fresh snapshot of the recorded events.
func (c *Capture) Events() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]Event, len(c.events))
	copy(result, c.events)
	return result
}
