// Package observe defines the optional observation seam for completed event-plane hops.
package observe

import (
	"context"
	"time"

	"eventplane/routing"
)

// Hop names the event-plane hop being observed.
type Hop string

const (
	HopPublish Hop = "publish"
	HopConsume Hop = "consume"
)

// Event is the payload-free metadata of one completed event-plane hop.
type Event struct {
	Hop           Hop
	Source        string
	Kind          string
	Subject       string
	EventID       string
	CorrelationID string
	Err           error
	Duration      time.Duration
}

// Key returns the canonical routing key of the observed event.
func (e Event) Key() string { return routing.Key(e.Source, e.Kind, e.Subject) }

// Hook synchronously observes one completed hop. Implementations must not block.
type Hook func(ctx context.Context, ev Event)
