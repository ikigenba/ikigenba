// Package retry provides a reusable retry driver.
package retry

import (
	"context"
	"math/rand/v2"
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
	clock := p.Clock
	if clock == nil {
		clock = realClock{}
	}
	random := p.Rand
	if random == nil {
		random = rand.Float64
	}

	maxAttempts := p.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	for attempt := 1; ; attempt++ {
		value, err := op(ctx)
		if err == nil {
			return value, nil
		}
		if attempt >= maxAttempts || p.Retryable == nil || !p.Retryable(err) {
			var zero T
			return zero, err
		}

		delay := backoff(p, attempt, random)
		if p.RetryAfter != nil {
			if floor := p.RetryAfter(err); floor > delay {
				delay = floor
			}
		}
		if onRetry != nil {
			onRetry(attempt, err, delay)
		}
		if err := clock.Sleep(ctx, delay); err != nil {
			var zero T
			if ctxErr := ctx.Err(); ctxErr != nil {
				return zero, ctxErr
			}
			return zero, err
		}
	}
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func backoff(p Policy, attempt int, random func() float64) time.Duration {
	delay := p.Base
	for retry := 1; retry < attempt && delay < p.Max; retry++ {
		if delay > p.Max/2 {
			delay = p.Max
			break
		}
		delay *= 2
	}
	if delay > p.Max {
		delay = p.Max
	}
	if p.Jitter > 0 && delay > 0 {
		delay -= time.Duration(float64(delay) * p.Jitter * random())
	}
	return delay
}
