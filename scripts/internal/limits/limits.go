// Package limits bounds each git operation and halts outstanding operations.
package limits

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/scripts/internal/settings"
)

// Clock supplies the timer used by each git operation. Nil means time.After.
type Clock struct {
	After func(d time.Duration) <-chan time.Time
}

// ErrTooLarge reports a tree exceeding the size limit.
var ErrTooLarge = errors.New("tree too large")

// ErrTimedOut reports a git operation exceeding its deadline.
var ErrTimedOut = errors.New("operation timed out")

// ErrHalted reports an operation stopped at the drain deadline.
var ErrHalted = errors.New("operations halted")

// Limits holds immutable settings and the operations of one process.
type Limits struct {
	s      settings.Settings
	after  func(time.Duration) <-chan time.Time
	mu     sync.Mutex
	halted bool
	open   map[*operation]struct{}
}

type operation struct {
	parent context.Context
	ctx    context.Context
	cancel context.CancelCauseFunc
	timer  <-chan time.Time
}

// New creates process limits with the given settings and timer.
func New(s settings.Settings, c Clock) *Limits {
	after := c.After
	if after == nil {
		after = time.After
	}
	return &Limits{s: s, after: after, open: make(map[*operation]struct{})}
}

// Settings returns the settings supplied at construction.
func (l *Limits) Settings() settings.Settings { return l.s }

// Operation starts one operation's deadline and returns its release function.
func (l *Limits) Operation(ctx context.Context) (context.Context, context.CancelFunc) {
	l.mu.Lock()
	if l.halted {
		opctx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
		cancel(ErrHalted)
		l.mu.Unlock()
		return opctx, func() { cancel(context.Canceled) }
	}
	opctx, cancel := context.WithCancelCause(ctx)
	op := &operation{parent: ctx, ctx: opctx, cancel: cancel}
	release := func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		op.cancel(context.Canceled)
		delete(l.open, op)
	}
	l.open[op] = struct{}{}
	l.mu.Unlock()
	d := time.Duration(math.MaxInt64)
	if l.s.OperationSeconds <= math.MaxInt64/int64(time.Second) {
		d = time.Duration(l.s.OperationSeconds) * time.Second
	}
	timer := l.after(d)
	l.mu.Lock()
	op.timer = timer
	l.settle(op, nil)
	l.mu.Unlock()
	if opctx.Err() == nil {
		go func() {
			select {
			case <-ctx.Done():
				l.finish(op, context.Cause(ctx))
			case <-timer:
				l.finish(op, ErrTimedOut)
			case <-opctx.Done():
				l.finish(op, context.Cause(opctx))
			}
		}()
	}
	return opctx, release
}

func (l *Limits) finish(op *operation, cause error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.settle(op, cause)
}

// settle runs under mu and gives an already-ended caller priority over a timer.
func (l *Limits) settle(op *operation, cause error) {
	if op.ctx.Err() != nil {
		delete(l.open, op)
		return
	}
	if op.parent.Err() != nil {
		cause = context.Cause(op.parent)
	} else {
		select {
		case <-op.timer:
			cause = ErrTimedOut
		default:
		}
	}
	if cause != nil {
		op.cancel(cause)
		delete(l.open, op)
	}
}

// Halt synchronously ends every open operation and refuses future operations.
func (l *Limits) Halt() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.halted = true
	for op := range l.open {
		l.settle(op, ErrHalted)
	}
}
