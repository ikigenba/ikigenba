# Phase 30 — Rebuild to adopt: the chain across the fan-out, out through `Append`, and the origin at spawn

*Realizes design Decision 32 (rebuild to adopt). Depends on Phase 28 (the run
row must carry `correlation_id`).*

> **External precondition (operator-sequenced, not built here).** The revised
> **eventplane** (`eventplane/correlation`, the `correlation_id` outbox column
> and envelope field, the ctx-bearing `Append(ctx, tx, ev)`, the chain installed
> on the consumer handler's context, and `eventplane/observe`) and the revised
> **appkit** (correlation middleware, telemetry recorder and its root-start
> helper, request/lifecycle instrumentation, and the injected publish/consume
> observation hooks) must both be **built first**. Until they land this phase
> cannot compile: `Append` gains a leading context parameter.

What gets built:

- **`internal/consume`** — the detached fan-out goroutine re-seeds its
  `context.Background()` with the chain id the handler's context carries, so a
  fired run continues the delivered event's chain. `FireFunc`'s signature, the
  five upstream handlers, the fire-and-forget contract and the `ErrSkip` poison
  rule are unchanged.
- **`internal/script`** — `FinishRunInput` gains `CorrelationID`, filled by the
  runner from the run it holds; `FinishRun` wraps its own context with that id
  before `Append(ctx, tx, ev)`, keeping the append on the same transaction as
  the terminal-state write. A `RootStarter` seam
  (`func(ctx context.Context, rootID, op string) context.Context`, nil-safe) is
  called at spawn **only** when the spawn context carries no chain, with the
  run's own id as `rootID` and op `run:<run-id>`.
- **`cmd/scripts`** — binds `RootStarter` to the chassis root-start helper from
  the wired recorder; no other composition-root change.
- **`internal/db/migrations`** — one new timestamped migration minted with
  `bin/create-migration scripts outbox_correlation`, dropping and recreating the
  outbox table per eventplane's revised `outbox.SchemaSQL` verbatim; the frozen
  `004_outbox.sql` and the `outbox_routing` migration are untouched and the DDL
  drift guard re-points at the newest outbox migration.

**Done when:** the four ids below are each covered by a clearly-named,
genuinely-asserting test over the real substrates D32 names (real
`consume.Handler` over the real service and a temp-file SQLite database built by
the full embedded migration set; real SQLite outbox rows read back by SQL; the
injected `RootStarter` observed directly; the real `python3` probe harness), and
the suite is green per design's *Conventions* (`go build ./...`, `go vet ./...`,
`gofmt -l .` silent, `go test ./...` clean):

- **R-4XFG-EFU8** — an event delivered on a handler context carrying chain `X`
  fires runs whose stored `correlation_id` is exactly `X`, including when one
  event matches three scripts (all three rows carry `X`); and a delivery whose
  envelope carried no chain (so `ev.CorrelationID == ""` while the handler
  context's id is valid) fires a run storing that **context** id — catching a
  fan-out re-seeded from the event field instead of the context.
- **R-4YNC-S7KX** — after `FinishRun` lands a succeeded run whose row carries
  chain `X`, the real SQLite outbox holds exactly one row for that run with
  `correlation_id == X`, appended on the same transaction as the terminal write;
  a cancelled run still emits no row.
- **R-4ZV9-5ZBM** — the injected `RootStarter` is called exactly once for a run
  spawned on a context with no chain, with `rootID` equal to that run's id and
  `op` exactly `run:<run-id>`, and exactly zero times for a run spawned on a
  context already carrying chain `X` (which still stores `X`).
- **R-5135-JR2B** — a run whose probe script makes a real HTTP call to a
  non-suite `httptest` server and writes a file into its run dir produces no
  telemetry observation naming that call, while the run's captured `stdout` and
  persisted run dir do contain the evidence of both.
