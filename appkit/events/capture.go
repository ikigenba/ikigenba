package events

import (
	"context"
	"sync"
)

// Capture records events in memory and is ready to use as its zero value.
type Capture struct {
	mu     sync.Mutex
	events []Event
}

// Deliver records event in delivery order.
func (c *Capture) Deliver(_ context.Context, event Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
	return nil
}

// Events returns a new, non-nil snapshot of the recorded events.
func (c *Capture) Events() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]Event, len(c.events))
	copy(result, c.events)
	return result
}
