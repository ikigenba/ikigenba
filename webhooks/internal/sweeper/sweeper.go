// Package sweeper deletes deliveries older than the retention window.
package sweeper

import (
	"context"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/webhooks/internal/store"
)

// Interval is the wait between two sweeps.
const Interval = time.Hour

// Config supplies the records, the window and the clock and timer.
type Config struct {
	Store     *store.Store
	Retention time.Duration
	Now       func() time.Time
	After     func(time.Duration) <-chan time.Time
}

// Sweeper runs the sweep on its timer until stopped.
type Sweeper struct {
	stop, done chan struct{}
	once       sync.Once
}

// Sweep deletes, once, every delivery received before Now less the window.
func Sweep(ctx context.Context, cfg Config) error {
	now := time.Now
	if cfg.Now != nil {
		now = cfg.Now
	}
	_, err := cfg.Store.Sweep(ctx, now().Add(-cfg.Retention))
	return err
}

// Start sweeps once, then sweeps again each time a wait of Interval on After ends.
func Start(ctx context.Context, cfg Config) *Sweeper {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
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

// Stop ends the loop and waits for a sweep under way to finish.
func (s *Sweeper) Stop() {
	s.once.Do(func() { close(s.stop) })
	<-s.done
}
