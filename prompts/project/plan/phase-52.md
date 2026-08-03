# Phase 52 — The correlation id on the record: runs and calls

*Realizes design Decision 39 (correlation id storage + mint-or-inherit at spawn).*

⛔ **External precondition (operator-sequenced, not built here).** appkit's
correlation package must be built and available through the committed
`replace appkit => ../appkit`: the `X-Correlation-Id` read-or-mint middleware
and its context accessors (`correlation.FromContext`, `correlation.WithContext`).
This phase cannot build before it lands.

`internal/prompt` and `internal/calls` learn the causal chain. One new
timestamped migration (minted with `bin/create-migration prompts
correlation_id`, never hand-numbered) adds `correlation_id` to `runs` and to
`calls` with their indexes; no committed migration is edited. `Service.startRun`
— the shared start path for the manual and event-fired runs alike — reads the
chain id from its context and falls back to the run's own id (durable-root
reuse), stamping it on the run row before insert. `prompt.Run` gains
`CorrelationID` (JSON `correlation_id`), `FinishRunInput` threads the run's
value into the session `calls` row (D33's same-tx write), `calls.Filter` gains
the `correlation_id` dimension, and the MCP surface exposes it: `run_get`
returns the field and `run_list` accepts an optional `correlation_id` argument
that narrows to exactly the runs on that chain.

Nothing here propagates or records anything — the header on peer calls is
phase 50, the `root` record and event-plane continuation are phase 49. This
phase makes the value exist, durably and queryably.

**Done when:** `go build ./...` and `go test ./...` from `prompts/` are green
(design *Conventions*), with these ids covered by clearly-named tests:

- R-HIAG-2MGL — the full embedded migration set applied to a fresh real SQLite
  database yields `correlation_id` on both `runs` and `calls` with the
  `runs_correlation` / `calls_correlation` indexes, and every frozen committed
  migration is byte-identical to its committed body.
- R-HJIC-GE7A — a run started on a context with no chain id persists
  `correlation_id` exactly equal to its own run id (not empty, not a second
  ULID).
- R-HKQ8-U5XZ — a run started on a context carrying chain `X` persists exactly
  `X`, not the run id.
- R-HLY5-7XOO — a `calls` row round-trips its `correlation_id` through `Get`,
  and `List` filtered by a correlation id returns exactly the seeded rows on
  that chain.
- R-HN61-LPFD — a completed run's session `calls` row carries the run row's
  `correlation_id` (the inherited value when the run inherited one).
- R-HODX-ZH62 — through the assembled MCP handler, `run_get` returns
  `correlation_id` and `run_list` given `correlation_id: X` returns exactly the
  runs on chain `X`, with the argument advertised in the schema.
