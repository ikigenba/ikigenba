// Package sweeper deletes expired artifacts and records their deletions.
package sweeper

import (
	"context"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/trail"
)

// Interval is the wait between sweeps.
const Interval = time.Hour

// Config supplies the store, clock, timer and event destinations.
type Config struct {
	Store     *store.Store
	Now       func() time.Time
	After     func(time.Duration) <-chan time.Time
	Telemetry *telemetry.Writer
	Events    *events.Emitter
}

// Sweeper runs expiry sweeps until stopped.
type Sweeper struct {
	stop, done chan struct{}
	once       sync.Once
}

// Sweep deletes expired artifacts, recording each under its owner without a cause.
func Sweep(ctx context.Context, cfg Config) error {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	gone, err := cfg.Store.Sweep(ctx, now())
	if err != nil {
		return err
	}
	for _, a := range gone {
		owner := identity.NewContext(context.Background(), identity.Caller{UserID: a.OwnerID, Email: a.OwnerEmail})
		trail.Record(owner, cfg.Telemetry, cfg.Events, trail.Deleted, a)
	}
	return nil
}

// Start completes an initial sweep before running the hourly loop.
func Start(ctx context.Context, cfg Config) *Sweeper {
	if cfg.After == nil {
		cfg.After = time.After
	}
	_ = Sweep(ctx, cfg)
	s := &Sweeper{stop: make(chan struct{}), done: make(chan struct{})}
	loop := context.WithoutCancel(ctx)
	go func() {
		defer close(s.done)
		for {
			select {
			case <-s.stop:
				return
			case <-cfg.After(Interval):
				select {
				case <-s.stop:
					return
				default:
				}
				_ = Sweep(loop, cfg)
			}
		}
	}()
	return s
}

// Stop waits for any current sweep and prevents every subsequent sweep.
func (s *Sweeper) Stop() { s.once.Do(func() { close(s.stop) }); <-s.done }
