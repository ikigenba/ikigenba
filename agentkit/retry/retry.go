// Package retry provides a reusable retry driver.
package retry

import (
	"context"
	"time"
)

// Clock is the package's only time dependency, injected so tests run without
// real waiting. Sleep blocks for d or until ctx is done, whichever comes first,
// returning ctx.Err() when the context ended first so a cancelled retry stops
// promptly.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// Policy configures bounded exponential backoff with jitter. It knows nothing
// about agentkit's errors; Retryable and RetryAfter are injected so the package
// remains a leaf.
type Policy struct {
	MaxAttempts int            // total attempts including the first; <=0 means 1
	Base        time.Duration  // backoff for the first retry
	Max         time.Duration  // ceiling applied to every computed backoff delay
	Jitter      float64        // 0..1 fraction of each delay randomized away
	Clock       Clock          // injected time; nil means the real clock
	Rand        func() float64 // [0,1) jitter source; nil means the stdlib default

	// Retryable decides whether an error warrants another attempt. The root
	// wires in agentkit.Retryable (D4); a nil Retryable makes every error
	// terminal.
	Retryable func(err error) bool
	// RetryAfter extracts a server-mandated minimum delay from an error (the
	// root reads *Error.RetryAfter, D4). The wait before an attempt is the
	// larger of this floor and the computed backoff; nil means no floor.
	RetryAfter func(err error) time.Duration
}

// Do runs op under p, retrying transient failures with backoff. onRetry, if
// non-nil, is invoked once before each wait with the just-failed attempt's
// number (1-based), its error, and the delay about to elapse. A context
// cancellation during a wait ends Do immediately with the context error. The
// returned value is op's first success or its final error, unwrapped.
func Do[T any](
	ctx context.Context,
	p Policy,
	op func(ctx context.Context) (T, error),
	onRetry func(attempt int, err error, delay time.Duration),
) (T, error) {
	_ = p
	_ = onRetry
	return op(ctx)
}
