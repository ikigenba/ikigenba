# Phase 150 — The ask response cache seam (`internal/ask` `Cache`)

*Realizes design Decision 83 (ask response cache) — the cache-seam slice: R-02RF-HMOW, R-03ZB-VEFL, R-0578-966A, R-06F4-MXWZ, R-07N1-0PNO, R-08UX-EHED, R-0A2T-S952, R-0BAQ-60VR, R-0CIM-JSMG, R-0JU0-UF2M.*

The `internal/ask` package gains the `Cache` decorator of D83: a `Provider` interface satisfied by `*Asker`, `NewCache(next, cap)` (cap 0 = disabled pass-through, negative cap panics), `Ask` with the same signature as `Asker.Ask`, and `Invalidate(scope)`. The cache keys on `sha256(scope || 0x00 || TrimSpace(question))`, holds at most `cap` answers under LRU with hit-refresh, coalesces concurrent identical asks into one in-flight computation, runs each miss on a values-preserving detached context bounded by `ComputeTimeout`, caches honest-empty answers, and never caches errors. Nothing outside `internal/ask` changes in this phase; the composition root still wires the bare `Asker` until Phase 151.

**Done when:**
- Each of these ids is covered by a clearly-named test tagged verbatim in a `*_test.go` file, asserting the behavior stated in D83:
  - R-02RF-HMOW — a repeat identical ask invokes the underlying `Provider` once and serves the cached `Answer`.
  - R-03ZB-VEFL — trim-only key normalization (surrounding whitespace collides; case change does not).
  - R-0578-966A — scope is in the key; entries never cross scopes.
  - R-06F4-MXWZ — LRU eviction past cap with hit-refreshed recency.
  - R-07N1-0PNO — concurrent identical asks coalesce into exactly one computation, all receiving the same `Answer`.
  - R-08UX-EHED — the detached computation survives leader cancellation: waiters get the answer, the cache fills.
  - R-0A2T-S952 — honest-empty answers are cached.
  - R-0BAQ-60VR — errors propagate to leader and waiters and are never cached; the next ask recomputes.
  - R-0CIM-JSMG — `Invalidate` drops only the named scope's entries.
  - R-0JU0-UF2M — cap 0 disables caching and coalescing entirely.
- The suite is green per design Conventions: `go build ./...`, `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed from `wiki/`.
