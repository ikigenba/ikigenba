# Phase 28 — The run carries its causal chain: `runs.correlation_id` end to end

*Realizes design Decision 29 (`runs.correlation_id`, mint-or-inherit at spawn).
No dependency on another pending phase.*

> **External precondition (operator-sequenced, not built here).** The leaf
> package **`eventplane/correlation`** (`FromContext`, `WithContext`, `New`,
> `Valid`, `Header`) and **appkit**'s read-or-mint request middleware must be
> **built first** — this phase imports the accessors and relies on the
> middleware to put an inbound chain id on the request context.

What gets built:

- **`internal/db/migrations`** — one new timestamped migration, minted with
  `bin/create-migration scripts correlation_id`, adding
  `correlation_id TEXT NOT NULL DEFAULT ''` to `runs` plus the index
  `idx_runs_correlation`. Purely additive; no table is rebuilt and no committed
  migration is edited.
- **`internal/script`** — `Run` gains `CorrelationID` (JSON
  `correlation_id,omitempty`); `newRun` takes a context and applies the one
  rule (inherit the context's chain id; when there is none, the run's own ULID
  *is* the id); `InsertRun`, `scanRun` and every run-reading query carry the
  column; `ListRuns` accepts an optional correlation-id filter.
- **`internal/mcp`** — `run_get`/`run_list`/`run` report `correlation_id` in
  their structured output, and `run_list` advertises and honors an optional
  `correlation_id` input argument (schemas updated per D19's structured-MCP
  contract).

**Done when:** the four ids below are each covered by a clearly-named,
genuinely-asserting test, and the suite is green per design's *Conventions*
(`go build ./...`, `go vet ./...`, `gofmt -l .` silent, `go test ./...` clean):

- **R-4OW5-Q1ND** — the full embedded migration set applied through the appkit
  runner to a fresh real SQLite database yields a `runs` table with a
  `correlation_id` column and index `idx_runs_correlation`, while every frozen
  committed migration stays byte-identical to its committed body.
- **R-4Q42-3TE2** — a run started through `Service.Run` on a context carrying no
  correlation id persists `correlation_id` exactly equal to that run's own id —
  not empty, not a second distinct ULID.
- **R-4RBY-HL4R** — a run started on a context carrying correlation id `X`
  persists `correlation_id == X`, which is not the run id.
- **R-4SJU-VCVG** — through the assembled `appkit/mcp` handler, `run_get`
  returns the run's `correlation_id` and `run_list` given `correlation_id: X`
  returns exactly the runs stored with `X`, omitting a run on another chain and
  a run with an empty id; the advertised input schema carries the argument and
  the output schema the field.
