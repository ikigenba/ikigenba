package limits_test

import (
	"context"
	"errors"
	"math"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scripts/internal/limits"
	"github.com/ikigenba/ikigenba/scripts/internal/settings"
)

func done(ctx context.Context, t *testing.T, want error) {
	t.Helper()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("context not already done")
	}
	if !errors.Is(context.Cause(ctx), want) {
		t.Fatalf("cause = %v; want %v", context.Cause(ctx), want)
	}
}

func await(ctx context.Context, t *testing.T, want error) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("context did not end")
	}
	done(ctx, t, want)
}

func live(ctx context.Context, t *testing.T) {
	t.Helper()
	select {
	case <-ctx.Done():
		t.Fatalf("context ended early: %v", context.Cause(ctx))
	default:
	}
}

// R-LPAA-56WN
func TestSentinels(t *testing.T) {
	errs := []error{limits.ErrTooLarge, limits.ErrTimedOut, limits.ErrHalted, context.Canceled, context.DeadlineExceeded}
	for i, a := range errs[:3] {
		if a == nil {
			t.Fatal("nil sentinel")
		}
		for j, b := range errs {
			if i != j && errors.Is(a, b) {
				t.Fatalf("%v matches %v", a, b)
			}
		}
	}
}

// R-LKEO-M3XV R-LLMK-ZVOK R-LQI6-IYNC R-LRQ2-WQE1 R-LU5V-O9VF
func TestSettingsAndTimerContract(t *testing.T) {
	for _, seconds := range []int64{1, 600, math.MaxInt64 / int64(time.Second), math.MaxInt64/int64(time.Second) + 1, math.MaxInt64} {
		s := settings.Settings{DrainSeconds: seconds, TreeMaxBytes: seconds, OutputMaxBytes: seconds, OperationSeconds: seconds, ScriptSeconds: seconds, RunKeepDays: seconds, RunKeepCount: seconds, ReposDir: "arbitrary\x00"}
		calls := 0
		after := func(d time.Duration) <-chan time.Time {
			calls++
			want := time.Duration(math.MaxInt64)
			if seconds <= math.MaxInt64/int64(time.Second) {
				want = time.Duration(seconds) * time.Second
			}
			if d != want {
				t.Fatalf("duration %v; want %v", d, want)
			}
			return make(chan time.Time)
		}
		newLimits := limits.New
		l := newLimits(s, limits.Clock{After: after})
		if l == nil || l.Settings() != s || calls != 0 {
			t.Fatal("New changed settings or requested timer")
		}
		for _, canceled := range []bool{false, true} {
			ctx, end := context.WithCancel(context.Background())
			if canceled {
				end()
			}
			op, release := l.Operation(ctx)
			if canceled {
				done(op, t, context.Canceled)
			} else {
				live(op, t)
			}
			release()
			release()
			end()
			if l.Settings() != s {
				t.Fatal("operation changed settings")
			}
		}
		if calls != 2 {
			t.Fatalf("Operation calls = %d", calls)
		}
		l.Halt()
		l.Halt()
		if calls != 2 || l.Settings() != s {
			t.Fatal("Halt changed settings or requested timer")
		}
	}
	// Nil Clock is valid; no operation needs a real timer to prove construction.
	l := limits.New(settings.Defaults(), limits.Clock{})
	if l == nil || l.Settings() != settings.Defaults() {
		t.Fatal("nil clock construction")
	}
}

// R-LMUH-DNF9 R-LVDS-21M4 R-LZ1H-7CU7
func TestTimerDeadlineAndStableCause(t *testing.T) {
	for _, ready := range []bool{false, true} {
		timer := make(chan time.Time, 1)
		if ready {
			timer <- time.Time{}
		}
		l := limits.New(settings.Defaults(), limits.Clock{After: func(time.Duration) <-chan time.Time { return timer }})
		ctx, end := context.WithCancelCause(context.Background())
		op, release := l.Operation(ctx)
		if ready {
			done(op, t, limits.ErrTimedOut)
		} else {
			live(op, t)
			timer <- time.Time{}
			await(op, t, limits.ErrTimedOut)
		}
		if errors.Is(context.Cause(op), limits.ErrHalted) {
			t.Fatal("deadline reported halted")
		}
		end(errors.New("later caller"))
		l.Halt()
		release()
		release()
		done(op, t, limits.ErrTimedOut)
	}
}

// R-LWLO-FTCT R-IBAY-YAJZ R-LVDS-21M4
func TestCallerCauseHasPriority(t *testing.T) {
	for _, when := range []string{"before", "during", "after"} {
		for _, timerReady := range []bool{false, true} {
			ctx, end := context.WithCancelCause(context.Background())
			cause := errors.New("caller left")
			timer := make(chan time.Time, 1)
			if when == "before" {
				end(cause)
			}
			l := limits.New(settings.Defaults(), limits.Clock{After: func(time.Duration) <-chan time.Time {
				if when == "during" {
					end(cause)
				}
				if timerReady && when != "after" {
					timer <- time.Time{}
				}
				return timer
			}})
			op, release := l.Operation(ctx)
			if when == "after" {
				live(op, t)
				end(cause)
				if timerReady {
					timer <- time.Time{}
				}
				await(op, t, cause)
			} else {
				done(op, t, cause)
			}
			if !errors.Is(context.Cause(op), cause) {
				t.Fatal("caller cause not preserved exactly")
			}
			l.Halt()
			release()
			end(errors.New("another cause"))
			done(op, t, cause)
		}
	}
}

// R-LO2D-RF5Y R-M09D-L4KW R-M1H9-YWBL R-LU5V-O9VF R-LVDS-21M4
func TestHaltEndsAllAndRefusesTimers(t *testing.T) {
	calls := 0
	l := limits.New(settings.Defaults(), limits.Clock{After: func(time.Duration) <-chan time.Time { calls++; return make(chan time.Time) }})
	var contexts []context.Context
	var releases []context.CancelFunc
	for range 20 {
		op, release := l.Operation(context.Background())
		live(op, t)
		contexts = append(contexts, op)
		releases = append(releases, release)
	}
	l.Halt()
	for _, op := range contexts {
		done(op, t, limits.ErrHalted)
	}
	for _, release := range releases {
		release()
		release()
	}
	l.Halt()
	for _, canceled := range []bool{false, true} {
		ctx, end := context.WithCancel(context.Background())
		if canceled {
			end()
		}
		op, release := l.Operation(ctx)
		done(op, t, limits.ErrHalted)
		release()
		end()
		done(op, t, limits.ErrHalted)
	}
	if calls != 20 {
		t.Fatalf("clock called %d times", calls)
	}
}

// R-M2P6-CO2A R-LVDS-21M4 R-LU5V-O9VF
func TestReleaseCauseAndIdempotence(t *testing.T) {
	calls := 0
	timer := make(chan time.Time, 1)
	l := limits.New(settings.Defaults(), limits.Clock{After: func(time.Duration) <-chan time.Time { calls++; return timer }})
	ctx, end := context.WithCancelCause(context.Background())
	op, release := l.Operation(ctx)
	live(op, t)
	release()
	done(op, t, context.Canceled)
	release()
	timer <- time.Time{}
	end(errors.New("later"))
	l.Halt()
	release()
	done(op, t, context.Canceled)
	if calls != 1 {
		t.Fatalf("clock calls = %d", calls)
	}
}

// R-M2P6-CO2A
func TestReleaseBeforeReadyTimerIsProcessed(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	timer := make(chan time.Time, 1)
	l := limits.New(settings.Defaults(), limits.Clock{After: func(time.Duration) <-chan time.Time { return timer }})
	op, release := l.Operation(context.Background())
	defer release()
	timer <- time.Time{}
	live(op, t)
	release()
	done(op, t, context.Canceled)
	l.Halt()
	done(op, t, context.Canceled)
}

// R-M3X2-QFSZ
func TestConcurrentMethodsAndReleases(t *testing.T) {
	l := limits.New(settings.Defaults(), limits.Clock{After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }})
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 50 {
		wg.Go(func() {
			<-start
			ctx, end := context.WithCancel(context.Background())
			op, release := l.Operation(ctx)
			var releaseWG sync.WaitGroup
			for range 5 {
				releaseWG.Go(release)
			}
			if i%3 == 0 {
				l.Halt()
			}
			if l.Settings() != settings.Defaults() {
				t.Error("settings changed")
			}
			end()
			releaseWG.Wait()
			done(op, t, context.Cause(op))
		})
	}
	close(start)
	wg.Wait()
	l.Halt()
}
