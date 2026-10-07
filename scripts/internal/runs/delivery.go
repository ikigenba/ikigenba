package runs

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// Deliver makes each subscriber's run and acknowledges only settled deliveries.
func (c *Core) Deliver(ctx context.Context, d events.Delivery) events.Outcome {
	c.mu.Lock()
	stopping := c.draining
	c.mu.Unlock()
	if stopping {
		return events.Fail("scripts is stopping; try again later")
	}
	if d.Event.ID == "" {
		return events.Fail("event has no id")
	}
	ctx = context.WithoutCancel(ctx)
	subs, err := c.cfg.Store.Subscribers(ctx, d.Event.Name)
	if err != nil {
		return events.Fail(store.Unreachable)
	}
	if len(subs) == 0 {
		return events.Skip()
	}
	// Snapshot all admission decisions before creating any subscriber's folder.
	needed := make([]store.Script, 0, len(subs))
	for _, sc := range subs {
		done, e := c.cfg.Store.Delivered(ctx, sc.ID, d.Event.ID)
		if e != nil {
			return events.Fail(store.Unreachable)
		}
		if !done {
			needed = append(needed, sc)
		}
	}
	if len(needed) == 0 {
		return events.OK()
	}
	c.mu.Lock()
	unavailable := c.cfg.Unavailable != ""
	freeActive := max(int64(0), c.cfg.MaxActive-int64(len(c.active))-c.starting)
	freeQueue := max(int64(0), c.cfg.MaxQueued-int64(len(c.queue)))
	full := int64(len(needed)) > freeActive+freeQueue
	c.mu.Unlock()
	if unavailable {
		return events.Fail(c.unavailable().Error())
	}
	if full {
		return events.Fail(c.queueFull().Error())
	}
	subs = needed
	input, err := d.Event.MarshalJSON()
	if err != nil {
		return events.Fail(store.Unreachable)
	}
	caller, _ := identity.FromContext(ctx)
	results := make(chan error, len(subs))
	for _, sc := range subs {
		go func() {
			_, err := c.run(ctx, sc, Request{Input: input, Caller: identity.Caller{UserID: sc.Owner, RequestID: caller.RequestID}, Cause: events.Cause{ID: d.Event.ID, Depth: d.Event.Depth}}, true)
			results <- err
		}()
	}
	var halted, starting, failed bool
	for range subs {
		err := <-results
		switch {
		case err == nil, errors.Is(err, store.ErrDelivered), errors.Is(err, store.ErrNotFound):
		case errors.Is(err, ErrDraining), errors.Is(err, limits.ErrHalted):
			halted = true
		case errors.Is(err, ErrStarting):
			starting = true
		default:
			failed = true
		}
	}
	if halted {
		return events.Fail("scripts is stopping; try again later")
	}
	if starting {
		return events.Fail("a run for this event is starting; try again later")
	}
	if failed {
		return events.Fail(store.Unreachable)
	}
	return events.OK()
}
