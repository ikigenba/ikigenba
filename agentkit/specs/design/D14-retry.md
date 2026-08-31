# D14-retry

`agentkit/retry` is a **public leaf**: its own package under the module, importing
nothing but the standard library, depended on by the agentkit root and never
depending back on it. The root wires its own error knowledge into the package
through function-typed hooks, so the retry driver can decide *whether* to retry
and *how long* to wait without ever importing agentkit's `*Error`, `Category`, or
`Retryable` (D4) — which would close a dependency cycle. The package is exported
so a sibling (D0) can reuse the same backoff driver, and so its behavior is
testable in isolation.

**Time enters through an injected `Clock`, exactly as idgen's mint loop does
(D3-precedent).** Production wiring is `time.Now` and a context-aware sleep; a
test injects a fake whose `Sleep` advances a virtual now, so the whole retry
suite runs deterministically and consumes no real wall time.

```go
package retry

// Clock is the package's only time dependency, injected so tests run without
// real waiting. Sleep blocks for d or until ctx is done, whichever comes first,
// returning ctx.Err() when the context ended first so a cancelled retry stops
// promptly.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}
```

**`Policy` is pure backoff configuration plus the two hooks the root supplies.**
It holds no agentkit types; `Retryable` and `RetryAfter` are functions the caller
passes in — the root binds `agentkit.Retryable` and a reader of `*Error.RetryAfter`
respectively — so `Policy` stays stdlib-only while still honoring a category-driven
retry decision and a server-mandated delay floor.

```go
// Policy configures bounded exponential backoff with jitter. It knows nothing
// about agentkit's errors; Retryable and RetryAfter are injected so the package
// remains a leaf.
type Policy struct {
	MaxAttempts int           // total attempts including the first; <=0 means 1
	Base        time.Duration // backoff for the first retry
	Max         time.Duration // ceiling applied to every computed backoff delay
	Jitter      float64       // 0..1 fraction of each delay randomized away
	Clock       Clock         // injected time; nil means the real clock
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
```

**`Do` is the driver.** It runs the operation, and while the policy says the last
error is retryable and attempts remain, it waits `max(backoff(attempt),
RetryAfter(err))` on the injected clock and tries again. Backoff is exponential
from `Base`, doubled per attempt, capped at `Max`, then reduced by up to `Jitter`.
Before each wait it calls `onRetry` (may be nil) so the orchestrator can emit a
`retry` event-log record (D15) carrying the attempt number, the error, and the
chosen delay. `Do` returns the first success or, when attempts are exhausted or
the error is non-retryable, the last error verbatim — it never wraps, so
`errors.Is`/`errors.As` at the call site still see the original `*Error`.

```go
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
) (T, error)
```

The package neither logs nor prices anything itself; `onRetry` is the one seam
back to the orchestrator, and the orchestrator owns turning a retry into a log
record and folding a retried request's usage into the turn (D12, D15).

## REQUIREMENTS

- R-5628-QV0F: The `agentkit/retry` package MUST import only the standard library and MUST NOT import the agentkit root package, so the dependency arrow points root → retry only.
- R-57A5-4MR4: `Do` MUST retry only while `Policy.Retryable(err)` is true and fewer than `MaxAttempts` attempts have run; a nil `Retryable` MUST make the first error terminal, and `MaxAttempts <= 0` MUST permit exactly one attempt.
- R-58I1-IEHT: The delay before an attempt MUST be the larger of the computed exponential-backoff-with-jitter delay and `Policy.RetryAfter(err)`, and every computed backoff delay MUST be capped at `Policy.Max`.
- R-59PX-W68I: All waiting MUST occur through the injected `Clock.Sleep`; with a fake clock whose `Sleep` advances a virtual now, a full retry sequence MUST complete consuming no real wall time.
- R-5AXU-9XZ7: A context cancellation during a wait MUST end `Do` promptly and return the context's error, not a provider error.
- R-5C5Q-NPPW: `Do` MUST return op's final error unwrapped (never re-wrapped), so `errors.Is`/`errors.As` at the call site still match the original `*Error`.
- R-5DDN-1HGL: When `onRetry` is non-nil it MUST be called exactly once before each wait, with the failed attempt's 1-based number, its error, and the delay to be waited.
