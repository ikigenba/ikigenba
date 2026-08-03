# Phase 53 — Rebuild to adopt: event-plane chain continuation, the spawn `root` record, the recorded boundary

*Realizes design Decision 43 (rebuild-to-adopt: consumer/producer chain
continuation, `root` at spawn, recorded boundary). Depends on Phase 52.*

⛔ **External precondition (operator-sequenced, not built here).** Both chassis
revisions must be built and available through the committed `replace`
directives: appkit (correlation middleware and `StartChain`, telemetry
recorder, MCP-dispatch and plain-HTTP request instrumentation, lifecycle
records) **and** eventplane
(the `correlation_id` outbox column and wire envelope field, the ctx-populated
`Append`, and the id surfaced into consumer handler contexts). The `Append`
signature change is compile-caught, so this phase cannot build before they
land.

prompts rebuilds against the new chassis and closes the three gaps the rebuild
does not close by itself. `internal/consume` re-seeds its detached fan-out
context with the delivered event's chain id (`FireFunc`'s signature is
unchanged — the value rides the context), so an event-fired run continues its
upstream chain and an unchained event makes the run its own root.
`internal/prompt`'s `FinishRun` wraps its context with the run row's
`correlation_id` before the same-transaction `outbox.Append`, so the emitted
`run.succeeded`/`run.failed` event carries the run's chain; one new timestamped
migration (`bin/create-migration prompts outbox_correlation`) drops and
recreates the outbox per eventplane's revised `outbox.SchemaSQL` verbatim, and
the DDL drift guard re-points at that newest outbox migration while the frozen
ones stay untouched. `prompt.Service` gains the injected `RootStarter` seam
(nil = ctx unchanged) and calls it at spawn **only** when the run roots its own
chain, passing the run id and op `run:<run-id>`; the composition root binds it
to the seed-then-`StartChain` composition (`correlation.WithContext(ctx,
runID)` then `correlation.StartChain(ctx, op)`, which adopts the seeded id and
records the `root`) and wires the chassis telemetry/correlation middleware.
prompts mints no chain id and builds no record itself.

**Done when:** `go build ./...` and `go test ./...` from `prompts/` are green
(design *Conventions*), with these ids covered by clearly-named tests:

- R-HZD1-FEUB — an event delivered on a handler context carrying chain `X`
  fires a run whose stored `correlation_id` is exactly `X` (read back by SQL) —
  the id survives the detached fan-out goroutine.
- R-I0KX-T6L0 — an event delivered with no chain id fires a run rooted at its
  own run id, and the same event delivered twice yields two runs on two
  distinct chains.
- R-I1SU-6YBP — after `FinishRun` lands a succeeded run on chain `X`, the real
  SQLite outbox holds exactly one row for it with `correlation_id` exactly `X`,
  appended on the terminal-state transaction; a cancelled run still emits none.
- R-I30Q-KQ2E — the injected `RootStarter` is called exactly once for a run
  that roots its own chain (`rootID` equal to the run id, `op` exactly
  `run:<run-id>`) and zero times for a run that inherited a chain.
- R-I48M-YHT3 — for a run driven through at least one builtin sandbox tool use,
  the `RootStarter` is still called exactly once (the spawn) and nothing in the
  run path emits anything naming that tool, while the run's `output.jsonl` does
  contain the `tool_use` record.
