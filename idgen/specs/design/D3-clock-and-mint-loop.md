# D3-clock-and-mint-loop

Time enters the CLI through a small `Clock` interface, defined in
`internal/cli` (its only consumer) and injected into `Run` (D1). Production
wiring is `time.Now`/`time.Sleep`; tests inject a fake.

```go
package cli

type Clock interface {
    Now() time.Time
    Sleep(d time.Duration)
}
```

Minting `N` ids (the `-n` flag, D4) must yield `N` distinct ids. Since an id
encodes a millisecond, distinctness means each id occupies its own,
strictly-later millisecond: the mint loop reads `clock.Now()`, and if that
instant's ms-since-epoch is not strictly greater than the previously minted
one, it sleeps (via `clock.Sleep`) and re-reads until it is. Each id is minted
from an instant the clock has already reported — never a fabricated or future
instant.

Observable consequences of the contract:

- A single mint (the default, `-n 1`) never waits.
- `N` ids cost at least ~`N−1` ms of (possibly virtual) clock advance.
- **Backward-clock policy: tolerate.** If the wall clock steps backward
  mid-sequence, the loop simply waits until the clock climbs past the last
  minted millisecond; the minted sequence within one invocation never repeats
  or goes backward. There is no cross-invocation protection — that is inherent
  to wall-clock ids and is documented, not defended against.

Each minted id is written to stdout on its own line, in mint order.

## REQUIREMENTS

- R-SU6S-QJ1E: With a fake `Clock` whose `Sleep(d)` advances a virtual now by `d`, `-n N` MUST print `N` pairwise-distinct ids and terminate without consuming real wall time.
- R-SVEP-4AS3: Under that same fake clock, minting `N` ids MUST advance virtual time by at least `N−1` milliseconds.
- R-SWML-I2IS: With a stalled fake clock whose `Now` advances only when `Sleep` is called, `-n N` MUST terminate with `N` pairwise-distinct ids whose last id decodes to a millisecond at least `N−1` beyond the first.
- R-SZ2E-9M06: The default single mint MUST call `Sleep` zero times.
- R-T0AA-NDQV: A minted id MUST decode (via `TimeOf`) to the millisecond of an instant the injected clock actually reported, never a future instant.
- R-T1I7-15HK: With a fake clock that steps backward mid-sequence and recovers only as `Sleep` advances it, the minted millisecond sequence MUST be strictly increasing and MUST advance past the pre-step value (the loop waits out the excursion rather than emitting duplicates).
