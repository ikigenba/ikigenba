package runs

import (
	"context"
	"errors"
	"fmt"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

// Deliver records each subscriber's event run before acknowledging the event.
func (c *Core) Deliver(ctx context.Context, d events.Delivery) events.Outcome {
	ctx = context.WithoutCancel(ctx)
	c.mu.Lock()
	stopping := c.draining
	c.mu.Unlock()
	if stopping {
		return events.Fail(Stopping)
	}
	if d.Event.ID == "" {
		return events.Fail(NoEventID)
	}
	subs, err := c.cfg.Store.Subscribers(ctx, d.Event.Name)
	if err != nil {
		return events.Fail(store.Unreachable)
	}
	if len(subs) == 0 {
		return events.Skip()
	}
	needed := make([]store.Prompt, 0, len(subs))
	for _, p := range subs {
		delivered, err := c.cfg.Store.Delivered(ctx, p.ID, d.Event.ID)
		if err != nil {
			return events.Fail(store.Unreachable)
		}
		if !delivered {
			needed = append(needed, p)
		}
	}
	if len(needed) == 0 {
		return events.OK()
	}
	if c.cfg.Unavailable != "" {
		return events.Fail(fmt.Sprintf(NoRuns, c.cfg.Unavailable))
	}
	c.mu.Lock()
	slots := max(int64(0), c.cfg.MaxActive-int64(len(c.active)))
	queue := max(int64(0), c.cfg.MaxQueued-int64(len(c.queue)))
	full := int64(len(needed)) > slots && int64(len(needed))-slots > queue
	c.mu.Unlock()
	if full {
		return events.Fail(fmt.Sprintf(QueueFull, c.cfg.MaxQueued))
	}
	input, err := d.Event.MarshalJSON()
	if err != nil {
		return events.Fail(store.Unreachable)
	}
	caller, _ := identity.FromContext(ctx)
	outcome := events.OK()
	for _, p := range needed {
		_, err := c.run(ctx, p, Request{Input: input, Caller: identity.Caller{UserID: p.Owner, Email: p.OwnerEmail, RequestID: caller.RequestID}, Cause: events.Cause{ID: d.Event.ID, Depth: d.Event.Depth}}, true)
		switch {
		case err == nil, errors.Is(err, store.ErrDelivered), errors.Is(err, store.ErrNotFound):
		case errors.Is(err, ErrDraining), errors.Is(err, ErrCutOff):
			return events.Fail(Stopping)
		case errors.Is(err, ErrStarting):
			outcome = events.Fail(Starting)
		default:
			if outcome != events.Fail(Starting) {
				outcome = events.Fail(store.Unreachable)
			}
		}
	}
	return outcome
}
