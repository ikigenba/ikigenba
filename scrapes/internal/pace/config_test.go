package pace

import (
	"context"
	"errors"
	"testing"
	"time"
)

// R-3PER-YI90: positional construction proves the complete ordered seam.
func TestConfigOrderAndDefaults(t *testing.T) {
	now := time.Unix(19, 0)
	var pauses []time.Duration
	cfg := Config{100, 10, "", func() time.Time { return now }, func(_ context.Context, d time.Duration) { pauses = append(pauses, d) }}
	p := Start(cfg)
	if err := p.Step(context.Background(), 50, 0); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Second)
	b, elapsed, paused := p.Totals()
	if b != 50 || elapsed != 3*time.Second || paused != 500*time.Millisecond || len(pauses) != 1 {
		t.Fatalf("totals = %d, %v, %v; pauses = %v", b, elapsed, paused, pauses)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p = Start(Config{Budget: 1})
	if err := p.Step(ctx, 1, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("default sleep did not honor cancellation: %v", err)
	}
	_, elapsed, _ = p.Totals()
	if elapsed < 0 {
		t.Fatalf("default clock elapsed = %v", elapsed)
	}
}
