# Phase 81 — The executor holds its lease, keeps its pool, and never loses a result quietly

*Realizes design Decision 29 (the completion queue) — the executor slice: renewal, graceful release, pool resilience, and durable terminal writes. Depends on Phase 80.*

`internal/completion/executor.go` gains the behavior that makes Phase 80's lease real.

`NewExecutor` takes four new inputs beside the runtime bound it already accepts: the **lease duration**, the **renewal interval**, an injected **clock/ticker seam**, and a `*slog.Logger`. Without the clock seam the renewal behaviors can only be approximated with sleeps; without the logger the loss paths cannot be asserted at all.

While an executor holds an item it renews the lease on the renewal interval, under its own short context so that a starved connection makes it abandon *before* expiry and take the graceful, non-counting path. Losing the lease cancels the item's execution context, aborting the in-flight provider stream rather than merely suppressing its write, and ownership is re-checked before each corrective round trip. On shutdown the executor releases every held lease on a context that outlives cancellation, and the process waits for those releases within a bounded wait before the database handle closes.

The terminal writes stop being discarded: a store failure is retried a bounded number of times and then logged at error level naming the item. Each item executes under a recover boundary, so a panicking provider path fails its own item instead of the process. A worker that meets an error logs it, backs off, and keeps claiming; only context cancellation ends a worker, and `Run` returns non-nil only on a context error. The initial reclaim pass at startup stays fatal.

**Done when:** every id below is covered by a clearly-named test in `internal/completion/*_test.go`, with store failures staged by closing the real `*sql.DB` and log assertions made against a `slog` handler over a buffer — no fake queue and no `time.Sleep`; `go test ./...` from `prompts/` passes; `gofmt -l .` emits no output; and the design-only coverage difference is empty.

- R-05IS-4Q3G — after another owner reclaims, the original executor's renewal affects zero rows, its provider call is cancelled, and it writes no terminal result
- R-ZOG6-RXPQ — an executor whose renewal fails at the store abandons the item with no terminal result written, leaves it claimable, and logs the abandonment
- R-ZQVZ-JH74 — a failing terminal write is retried the bounded number of times, then logged at error level naming the item id, and the executor keeps claiming
- R-ZTBS-B0OI — a worker that meets a claim error neither exits nor wedges the pool: an item inserted after the error still reaches a terminal state, and `Run` returns non-nil only when its context is done
- R-ZUJO-OSF7 — a panicking execution fails only its own item while an item queued alongside it still reaches `done` and the process survives
