package limits_test

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/settings"
)

type timerClock struct {
	mu        sync.Mutex
	durations []time.Duration
	timers    []chan time.Time
	ready     bool
}

func (c *timerClock) after(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if c.ready {
		ch <- time.Time{}
	}
	c.durations = append(c.durations, d)
	c.timers = append(c.timers, ch)
	return ch
}

func (c *timerClock) fire(i int) {
	c.mu.Lock()
	ch := c.timers[i]
	c.mu.Unlock()
	ch <- time.Time{}
}

func (c *timerClock) calls() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.durations...)
}

func requireDone(ctx context.Context, t *testing.T, cause error) {
	t.Helper()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("operation is still open")
	}
	if !errors.Is(context.Cause(ctx), cause) {
		t.Fatalf("cause = %v, want %v", context.Cause(ctx), cause)
	}
}

func requireOpen(ctx context.Context, t *testing.T) {
	t.Helper()
	select {
	case <-ctx.Done():
		t.Fatalf("operation unexpectedly ended: %v", context.Cause(ctx))
	default:
	}
}

func awaitDone(ctx context.Context, t *testing.T, cause error) {
	t.Helper()
	// This watchdog fails a stuck test; it does not drive the operation's clock.
	watchdog := time.NewTimer(5 * time.Second)
	defer watchdog.Stop()
	select {
	case <-ctx.Done():
		requireDone(ctx, t, cause)
	case <-watchdog.C:
		t.Fatal("operation did not end")
	}
}

func TestDeclarationsAndSettings(t *testing.T) {
	// R-Y63E-T5ZR R-NK0T-3HQO R-NL8P-H9HD R-NMGL-V182
	// R-X6MD-VC6F R-Y8J7-KPH5 R-Y9R3-YH7U
	samples := []settings.Settings{
		settings.Defaults(),
		{DrainSeconds: 1, SiteMaxBytes: 1, OperationSeconds: 1, ReposDir: ""},
		{DrainSeconds: math.MaxInt64, SiteMaxBytes: math.MaxInt64, OperationSeconds: math.MaxInt64, ReposDir: "arbitrary\x00path"},
	}
	for _, s := range samples {
		c := &timerClock{}
		clock := limits.Clock{After: c.after}
		l := limits.New(s, clock)
		if l == nil || l.Settings() != s {
			t.Fatalf("New settings = %v", l)
		}
		if l.Clock().After == nil {
			t.Fatal("nil effective After")
		}
		operation := l.Operation
		drain := l.Drain
		draining := l.Draining
		halt := l.Halt
		op, cancel := operation(context.Background())
		cancel()
		cancel()
		drain()
		drain()
		if !draining() {
			t.Fatal("not draining")
		}
		halt()
		halt()
		requireDone(op, t, context.Canceled)
		if l.Settings() != s {
			t.Fatal("settings changed after methods")
		}
		if len(c.calls()) != 1 {
			t.Fatalf("non-operation method called After: %v", c.calls())
		}
		returned := l.Clock().After(17 * time.Nanosecond)
		c.mu.Lock()
		same := returned == c.timers[1]
		c.mu.Unlock()
		calls := c.calls()
		if !same || len(calls) != 2 || calls[1] != 17*time.Nanosecond {
			t.Fatalf("Clock did not preserve callback: %v", calls)
		}
	}
	if limits.New(settings.Defaults(), limits.Clock{}).Clock().After == nil {
		t.Fatal("nil default After")
	}
}

func TestSentinels(t *testing.T) {
	// R-AQWB-LEGR
	sentinels := []error{limits.ErrTooLarge, limits.ErrTimedOut, limits.ErrDraining, limits.ErrHalted}
	for i, a := range sentinels {
		if a == nil {
			t.Fatal("nil sentinel")
		}
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Fatalf("sentinels %d and %d match", i, j)
			}
		}
	}
}

func TestTimerCallsAndSaturation(t *testing.T) {
	// R-ZWZU-8C1G
	for _, seconds := range []int64{1, 600, math.MaxInt64 / int64(time.Second), math.MaxInt64/int64(time.Second) + 1, math.MaxInt64} {
		c := &timerClock{}
		s := settings.Defaults()
		s.OperationSeconds = seconds
		l := limits.New(s, limits.Clock{After: c.after})
		ctx, stop := context.WithCancel(context.Background())
		stop()
		_, cancel := l.Operation(ctx)
		cancel()
		l.Drain()
		_, cancel = l.Operation(context.Background())
		cancel()
		calls := c.calls()
		expected := time.Duration(math.MaxInt64)
		if seconds <= math.MaxInt64/int64(time.Second) {
			expected = time.Duration(seconds) * time.Second
		}
		if len(calls) != 2 || calls[0] != expected || calls[1] != expected {
			t.Fatalf("seconds %d: After calls %v, want twice %v", seconds, calls, expected)
		}
		l.Halt()
	}
}

func TestOperationCausesAndStability(t *testing.T) {
	// R-TO8G-RL90 R-O2BA-U1V3
	for _, first := range []string{"parent", "timer", "cancel", "ready", "parent-ready"} {
		t.Run(first, func(t *testing.T) {
			c := &timerClock{ready: first == "ready"}
			l := limits.New(settings.Defaults(), limits.Clock{After: c.after})
			parent, parentCancel := context.WithCancelCause(context.Background())
			defer parentCancel(context.Canceled)
			parentCause := errors.New("caller departed")
			if first == "parent-ready" {
				parentCancel(parentCause)
			}
			op, cancel := l.Operation(parent)
			defer cancel()
			cause := context.Canceled
			if first != "ready" && first != "parent-ready" {
				requireOpen(op, t)
			}
			switch first {
			case "parent":
				parentCancel(parentCause)
				cause = parentCause
			case "parent-ready":
				cause = parentCause
			case "timer":
				c.fire(0)
				cause = limits.ErrTimedOut
			case "ready":
				cause = limits.ErrTimedOut
				requireDone(op, t, cause)
			case "cancel":
				cancel()
				requireDone(op, t, cause)
			}
			awaitDone(op, t, cause)
			before := context.Cause(op)
			parentCancel(parentCause)
			if first != "timer" && first != "ready" {
				c.fire(0)
			}
			l.Halt()
			cancel()
			cancel()
			requireDone(op, t, cause)
			if !errors.Is(context.Cause(op), before) {
				t.Fatal("cause changed after later endings")
			}
			if errors.Is(cause, limits.ErrTimedOut) && errors.Is(context.Cause(op), limits.ErrHalted) {
				t.Fatal("timeout also matched halt")
			}
		})
	}
}

func TestDrainAndHalt(t *testing.T) {
	// R-TT42-AO7S R-ZY7Q-M3S5 R-O3J7-7TLS R-O4R3-LLCH
	c := &timerClock{}
	l := limits.New(settings.Defaults(), limits.Clock{After: c.after})
	if l.Draining() {
		t.Fatal("initially draining")
	}
	first, cancelFirst := l.Operation(context.Background())
	defer cancelFirst()
	l.Drain()
	l.Drain()
	if !l.Draining() {
		t.Fatal("Drain did not mark draining")
	}
	requireOpen(first, t)
	second, cancelSecond := l.Operation(context.Background())
	defer cancelSecond()
	requireOpen(second, t)
	c.fire(1)
	awaitDone(second, t, limits.ErrTimedOut)
	l.Halt()
	requireDone(first, t, limits.ErrHalted)
	requireDone(second, t, limits.ErrTimedOut)
	l.Halt()
	if !l.Draining() {
		t.Fatal("Halt changed draining")
	}
	parent, parentCancel := context.WithCancel(context.Background())
	parentCancel()
	third, cancelThird := l.Operation(parent)
	cancelThird()
	requireDone(third, t, limits.ErrHalted)
	if len(c.calls()) != 2 {
		t.Fatal("post-halt operation called After")
	}
	other := limits.New(settings.Defaults(), limits.Clock{After: c.after})
	other.Halt()
	if other.Draining() {
		t.Fatal("Halt marked draining")
	}
}

func TestConcurrentUse(t *testing.T) {
	// R-O5YZ-ZD36
	c := &timerClock{}
	s := settings.Defaults()
	l := limits.New(s, limits.Clock{After: c.after})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Go(func() {
			for j := 0; j < 20; j++ {
				op, cancel := l.Operation(context.Background())
				var cancels sync.WaitGroup
				for k := 0; k < 4; k++ {
					cancels.Go(cancel)
				}
				cancels.Wait()
				awaitDone(op, t, context.Canceled)
				if l.Settings() != s || l.Clock().After == nil {
					t.Error("immutable configuration changed")
				}
				l.Drain()
				_ = l.Draining()
			}
		})
	}
	wg.Wait()
	// Start simultaneous stop, creation, and release calls; post-halt operations
	// can carry ErrHalted rather than context.Canceled.
	for i := 0; i < 32; i++ {
		wg.Go(func() {
			op, cancel := l.Operation(context.Background())
			l.Halt()
			l.Drain()
			cancel()
			cancel()
			select {
			case <-op.Done():
			default:
				t.Error("Halt returned with open operation")
			}
			cause := context.Cause(op)
			if !errors.Is(cause, limits.ErrHalted) && !errors.Is(cause, context.Canceled) {
				t.Errorf("unexpected cause: %v", cause)
			}
		})
	}
	wg.Wait()
}

func TestReleaseWithPendingDeadline(t *testing.T) {
	// R-O2BA-U1V3
	c := &timerClock{}
	l := limits.New(settings.Defaults(), limits.Clock{After: c.after})
	for i := 0; i < 100; i++ {
		op, cancel := l.Operation(context.Background())
		requireOpen(op, t)
		c.fire(i)
		// If the observer has already ended the operation, release preserves its
		// cause. Otherwise release itself cancels immediately, even with a pending
		// buffered deadline. Both contenders may run between these public calls.
		before := context.Cause(op)
		cancel()
		select {
		case <-op.Done():
		default:
			t.Fatal("release left operation open")
		}
		cause := context.Cause(op)
		if before != nil && !errors.Is(cause, before) {
			t.Fatal("release changed a completed operation's cause")
		}
		if !errors.Is(cause, context.Canceled) && !errors.Is(cause, limits.ErrTimedOut) {
			t.Fatalf("unexpected release cause: %v", cause)
		}
		cancel()
		if !errors.Is(context.Cause(op), cause) {
			t.Fatal("repeated release changed cause")
		}
	}
	l.Halt()
}
