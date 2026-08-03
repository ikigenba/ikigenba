# Phase 8 — Correlation on the consumer path (`consumer`)

*Realizes design Decision 8 (consumer correlation). Depends on Phase 7.*

`consumer.Event` gains `CorrelationID`, read from the envelope by `parseEvent`
and never synthesized. Before invoking the handler, `handleFrame` derives the
handler's context: the envelope's id when `correlation.Valid`, otherwise a
fresh `correlation.New()` root attached for that delivery only. A non-empty
malformed id is treated exactly as absent. Delivery semantics are untouched —
every event still reaches the handler, the handler's return still gates the
cursor, and the engine's unparseable-envelope skip still runs no handler and
still advances.

**Done when:**

- `go test ./...` and `go vet ./...` from `eventplane/` both exit 0, and
  `gofmt -l .` prints nothing.
- These behaviors are covered by clearly-named tests in
  `eventplane/consumer/`, each citing its id; every end-to-end claim runs the
  real `outbox.FeedHandler()` in an `httptest.Server` with `consumer.Run` on
  the other end over a real `modernc.org/sqlite` database:
  - R-UQJC-OR74 — `ev.CorrelationID` equals the appended chain id, and is
    `""` for an event appended under a bare context.
  - R-URR9-2IXT — `correlation.FromContext(hctx)` inside the handler equals
    the appended chain id exactly.
  - R-USZ5-GAOI — two uncorrelated events yield handler contexts carrying two
    different `Valid` non-empty ids.
  - R-UU71-U2F7 — an envelope carrying `correlation_id:
    "not-a-correlation-id"` yields a handler context with a `Valid` id that is
    not that string, while `ev.CorrelationID` still reports the wire value.
  - R-UVEY-7U5W — two producers, two real feeds: A's event appended under `X`
    is consumed by a handler that appends to B using the context it was
    handed; a consumer on B's feed receives `CorrelationID == X`, with nothing
    but the context carrying it across.
  - R-UWMU-LLWL — delivery is unchanged: an empty-`kind` envelope is still
    engine-skipped with the cursor advancing and the next event delivered, and
    a non-skip handler error still stalls and re-delivers that event before any
    later one.
- `eventplane/go.mod` gains no `require` line:
  `git diff -- go.mod | grep -c '^+.*require'` is `0`.
