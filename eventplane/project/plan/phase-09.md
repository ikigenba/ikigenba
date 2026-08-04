# Phase 9 — The `observe` hook on both plane paths

*Realizes design Decision 9 (observation seam). Depends on Phase 7 and
Phase 8.*

A new package `eventplane/observe` at `eventplane/observe/` exports `Hop`
(with `HopPublish`/`HopConsume`), the skeleton `Event` with its `Key()` method,
and the `Hook` func type — importing only stdlib plus `eventplane/routing`.
`outbox.Options` gains `Observe observe.Hook` and `consumer.Config` gains the
same field; both default to nil, so no existing caller changes.

`Append` times itself and invokes the hook once on every call, success or
failure, with `Err` set and `EventID` empty when the event never got as far as
an id. The consumer engine times the handler invocation and invokes the hook
once per delivery with the correlation id the handler's context carried and
`Err` set to the handler's return — never for the engine's unparseable-envelope
skip and never for control frames. Every invocation on both sides is wrapped so
a panicking hook is recovered and logged and delivery is unaffected.

**Done when:**

- `go test ./...` and `go vet ./...` from `eventplane/` both exit 0, and
  `gofmt -l .` prints nothing.
- These behaviors are covered by clearly-named tests, each citing its id, with
  every consume-path claim exercised end-to-end (real `outbox.FeedHandler()` in
  an `httptest.Server`, `consumer.Run`, real `modernc.org/sqlite`):
  - R-UXUQ-ZDNA — one publish observation per successful `Append`, with hop,
    address, `Key()`, `EventID` matching the stored row, the context's
    correlation id, and `Err == nil`.
  - R-V0AJ-QX4O — an invalid-kind rejection and a registry rejection each fire
    one observation with non-nil `Err` and insert no row.
  - R-V1IG-4OVD — one consume observation per delivery carrying the address,
    the event id, `Err == nil`, and the handler context's correlation id —
    including the minted-root case, where the wire value was `""` and the
    observation reports the non-empty minted id.
  - R-V2QC-IGM2 — a wrapped `ErrSkip` yields an observation whose `Err`
    satisfies `errors.Is(err, consumer.ErrSkip)` with the cursor advancing; a
    non-skip error yields an observation carrying it, the engine still stalls,
    and the re-delivery fires a second observation for the same event id.
  - R-V3Y8-W8CR — a handler sleeping 50ms yields `Duration >= 50ms` while an
    immediate handler yields under 50ms.
  - R-V565-A03G — a hook panicking on every call, wired into both producer and
    consumer, leaves `Append` returning nil with the row present and a
    three-event feed delivered in order with the cursor committed past all
    three (a reconnect receives nothing) — matching the nil-hook run.
  - R-V6E1-NRU5 — no observation for an empty-`kind` envelope (cursor still
    advances) and none for `caught-up`/`status`/keepalive frames; the
    observation count equals the number of events whose handler ran.
- `observe` imports only stdlib and `eventplane/routing`:
  `go list -f '{{join .Deps "\n"}}' eventplane/observe | grep '^eventplane/'`
  prints exactly `eventplane/routing` (`.Deps` excludes the queried package
  itself, which `go list -deps` would always include).
- `eventplane/go.mod` gains no `require` line:
  `git diff -- go.mod | grep -c '^+.*require'` is `0`.
