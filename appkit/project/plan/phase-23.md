# Phase 23 — Chain-root helpers and the chassis's recorder reach

*Realizes design Decision 20 (root helpers + Router accessors). Depends on
Phase 19 and Phase 22.*

**End state.** `appkit/telemetry` gains the two chain-origin helpers as methods
on `*Recorder` — `StartRoot(ctx, op, detail)`, which always mints a fresh id and
ignores any ambient one, and `StartChain(ctx, op, detail)`, which adopts an
ambient id when present and mints otherwise — both installing the id on the
returned context, emitting exactly one `root` record with the caller's op, and
both nil-safe on a nil `*Recorder` (id still minted and installed, nothing
recorded). `appkit/server.Router` gains `Recorder() *telemetry.Recorder`,
returning the recorder `runServe` wired (nil when telemetry is disabled), and
`HTTPClient(timeout time.Duration) *http.Client`, returning a D19 instrumented
client already wired to that recorder. `Spec.Config` is deliberately left
without a recorder.

**Done when:**
- These Verification ids are covered by clearly-named tests tagged with the id
  verbatim, with records observed at a **live in-process HTTP sink** and the
  outbound claim driven against a **live** `httptest` peer:
  - R-XP15-H34E — `StartRoot` on an already-correlated context returns a
    different valid id and emits one `root` record carrying it and the op.
  - R-XQ91-UUV3 — `StartChain` adopts an existing id, mints when there is none,
    and emits exactly one `root` record in both cases.
  - R-XRGY-8MLS — both helpers on a nil `*Recorder` return a context carrying a
    valid id, do not panic, and record nothing.
  - R-XSOU-MECH — a record emitted through `rt.Recorder()` inside a `Handlers`
    hook arrives at the sink the serve-wired recorder posts to.
  - R-XTWR-0636 — `rt.HTTPClient(3*time.Second)` has `Timeout` 3s and its real
    request to a live peer produces an `outbound` record at the sink.
- The suite is green per design's *Conventions* (`go build ./...`, `go vet
  ./...`, `gofmt -l .` empty, `go test ./...`, all from `appkit/`).
