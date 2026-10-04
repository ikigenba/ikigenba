// Package limits bounds git operations and tracks a run's stop state.
package limits

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/sites/internal/settings"
)

// Clock supplies the timer for each git operation.
type Clock struct {
	After func(d time.Duration) <-chan time.Time
}

// Limits holds the immutable configuration and current stop state of a run.
type Limits struct {
	settings settings.Settings
	clock    Clock
	mu       sync.Mutex
	draining bool
	halted   bool
	active   map[*operation]struct{}
	gitRuns  map[chan struct{}]struct{}
}

type operation struct {
	ctx    context.Context
	parent context.Context
	cancel context.CancelCauseFunc
	timer  <-chan time.Time
}

// Errors classify limit and stop refusals without prescribing their wording.
var (
	ErrTooLarge = errors.New("site too large")
	ErrTimedOut = errors.New("git operation timed out")
	ErrDraining = errors.New("run is draining")
	ErrHalted   = errors.New("run is halted")
)

// New constructs limits for one run.
func New(s settings.Settings, c Clock) *Limits {
	if c.After == nil {
		c.After = time.After
	}
	return &Limits{settings: s, clock: c, active: make(map[*operation]struct{}), gitRuns: make(map[chan struct{}]struct{})}
}

// Settings returns the configuration supplied to New.
func (l *Limits) Settings() settings.Settings { return l.settings }

// Clock returns the effective clock supplied to New.
func (l *Limits) Clock() Clock { return l.clock }

// Operation creates the context bounding one git process.
func (l *Limits) Operation(ctx context.Context) (context.Context, context.CancelFunc) {
	opctx, cancel := context.WithCancelCause(ctx)
	op := &operation{ctx: opctx, parent: ctx, cancel: cancel}
	l.mu.Lock()
	if l.halted {
		// A new operation after Halt always reports the halt, including when its
		// caller is already cancelled.
		halted, haltCancel := context.WithCancelCause(context.WithoutCancel(ctx))
		haltCancel(ErrHalted)
		l.mu.Unlock()
		cancel(context.Canceled)
		return halted, func() { haltCancel(context.Canceled) }
	}
	l.active[op] = struct{}{}
	l.mu.Unlock()
	seconds := l.settings.OperationSeconds
	duration := time.Duration(math.MaxInt64)
	if seconds <= math.MaxInt64/int64(time.Second) {
		duration = time.Duration(seconds) * time.Second
	}
	timer := l.clock.After(duration)
	l.mu.Lock()
	op.timer = timer
	l.finishReady(op)
	l.mu.Unlock()
	go func() {
		select {
		case <-opctx.Done():
		case _, ok := <-timer:
			if ok {
				op.finishTimer()
			} else {
				<-opctx.Done()
			}
		}
		l.mu.Lock()
		delete(l.active, op)
		l.mu.Unlock()
	}()
	// An internal git consumer marks an actual start attempt separately from
	// releasing its cancellation context. Halt joins only those real attempts.
	tracked := context.WithValue(opctx, [1]string{"sites.git.lifetime"}, func() func() {
		l.mu.Lock()
		if l.halted || context.Cause(opctx) != nil {
			l.mu.Unlock()
			return func() {}
		}
		done := make(chan struct{})
		l.gitRuns[done] = struct{}{}
		l.mu.Unlock()
		var once sync.Once
		return func() {
			once.Do(func() {
				l.mu.Lock()
				delete(l.gitRuns, done)
				close(done)
				l.mu.Unlock()
			})
		}
	})
	return tracked, func() {
		l.mu.Lock()
		cancel(context.Canceled)
		delete(l.active, op)
		l.mu.Unlock()
	}
}

// finishReady honors a deadline already delivered before a later stop.
// The caller holds mu.
func (l *Limits) finishReady(op *operation) {
	select {
	case _, ok := <-op.timer:
		if ok {
			op.finishTimer()
		}
	default:
	}
}

// finishTimer preserves a parent cause even when the parent's propagation
// callback has not yet run and the timer is ready at the same time.
func (op *operation) finishTimer() {
	select {
	case <-op.parent.Done():
		op.cancel(context.Cause(op.parent))
	default:
		op.cancel(ErrTimedOut)
	}
}

// Drain marks the run as draining without ending existing operations.
func (l *Limits) Drain() { l.mu.Lock(); l.draining = true; l.mu.Unlock() }

// Draining reports whether Drain has been called.
func (l *Limits) Draining() bool { l.mu.Lock(); defer l.mu.Unlock(); return l.draining }

// Halt ends every open operation before returning and forbids new operations.
func (l *Limits) Halt() {
	l.mu.Lock()
	l.halted = true
	for op := range l.active {
		l.finishReady(op)
		op.cancel(ErrHalted)
		delete(l.active, op)
	}
	pending := make([]chan struct{}, 0, len(l.gitRuns))
	for done := range l.gitRuns {
		pending = append(pending, done)
	}
	l.mu.Unlock()
	for _, done := range pending {
		<-done
	}
}
