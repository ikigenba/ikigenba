// Package retention owns telemetry's only scheduled record-deletion path.
package retention

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"appkit"
	"telemetry/internal/db"
	telemetrytime "telemetry/internal/telemetry"
)

const retentionDaysEnv = "TELEMETRY_RETENTION_DAYS"

// DefaultDays is the product's contractual default retention window.
const DefaultDays = 90

// Interval is how often the pruner runs while the process lives.
const Interval = time.Hour

// Days resolves the configured retention window, falling back safely for bad values.
func Days(getenv func(string) string, log *slog.Logger) int {
	value := getenv(retentionDaysEnv)
	days, err := strconv.Atoi(value)
	if err != nil || days <= 0 {
		log.Warn("invalid telemetry retention window; using default", "value", value, "days", DefaultDays)
		return DefaultDays
	}
	return days
}

// Pruner removes records older than its configured retention window.
type Pruner struct {
	store *db.Store
	clock telemetrytime.Clock
	days  int
	log   *slog.Logger
}

// New constructs a retention pruner.
func New(store *db.Store, clock telemetrytime.Clock, days int, log *slog.Logger) *Pruner {
	return &Pruner{store: store, clock: clock, days: days, log: log}
}

// PruneOnce removes records strictly older than the configured cutoff.
func (p *Pruner) PruneOnce(ctx context.Context) (int, error) {
	cutoff := p.clock.Now().Add(-time.Duration(p.days) * 24 * time.Hour)
	return p.store.PruneBefore(ctx, cutoff)
}

// Run prunes immediately and after every delivered tick until cancellation.
func (p *Pruner) Run(ctx context.Context, ticks <-chan time.Time) {
	p.pruneAndLog(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}
			p.pruneAndLog(ctx)
		}
	}
}

func (p *Pruner) pruneAndLog(ctx context.Context) {
	if _, err := p.PruneOnce(ctx); err != nil {
		p.log.Error("telemetry retention prune failed", "error", err)
	}
}

// Start resolves retention once and launches the hourly process-lifetime pruner.
func Start(rt *appkit.Router, store *db.Store, clock telemetrytime.Clock) {
	pruner := New(store, clock, Days(os.Getenv, rt.Logger()), rt.Logger())
	ticker := time.NewTicker(Interval)
	go func() {
		defer ticker.Stop()
		pruner.Run(context.Background(), ticker.C)
	}()
}
