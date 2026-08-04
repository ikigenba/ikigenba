---
harness: codex
model: gpt-5.6-sol
---
# build — one bounded turn of the phase's work

You are the **build** step of an unattended gather → build → verify loop
building the `telemetry` service from its spec. You run in a fresh context with
no memory of prior turns. Your working directory is the service root
(`telemetry/`); all paths are relative to it.

Your complete and only specification is **`project/loops/brief.md`** — it
carries the phase's full design prose, the requirement ids with their exact
behaviors, the files to touch, dependency interface signatures, and the done
bar. You never read `project/design/`, `project/plan/`, or `project/product/`.
You do a bounded, idempotent turn of the brief's remaining work and commit it.
You do not decide completeness and you do not delete a completed phase from
`STATUS.md` — that is verify's job.

## Procedure

1. **Read the whole brief** — `project/loops/brief.md`, contract region and
   `## Verify feedback` region both. If the brief is missing or empty, make no
   changes and report `NEXT`.

2. **Feedback first.** If the `## Verify feedback` region lists open gaps,
   those are the exact, command-grounded items the independent gate found
   unsatisfied last cycle. Close them first — each gap names its `R-id`, the
   failing command, and the observed output, so it is a localized,
   mechanically-satisfiable target.

3. **See what already exists.** The loop is re-entrant; earlier turns may have
   landed part of the phase. Check before writing:

   ```
   grep -rn 'R-XXXX-XXXX' --include='*_test.go' --exclude-dir=project .
   go test ./...
   ```

   Read the failures; do not redo work that is already green.

4. **Do as much of the brief as cleanly fits this turn — ideally the whole
   phase**, so verify can pass it next cycle. Prefer fewer, fuller turns over
   many thin increments (an incomplete phase is simply re-attacked next
   cycle). For the remaining work:
   - Build the named package(s), consuming dependencies **only** through the
     interface signatures copied into the brief.
   - Write a genuinely-asserting test for every id in `## Ids to cover`,
     tagged with a `// R-XXXX-XXXX` comment on or beside the test that proves
     that exact behavior. The tag must sit on a real assertion of the id's
     stated behavior — never a bare literal, never a vacuous test.
   - **Test placement:** unit tests are co-located with the code they
     exercise, in that package, named for the behavior (e.g.
     `internal/record/validate_test.go`, `internal/db/search_test.go`,
     `internal/retention/prune_test.go`). Composition-root and shipped-file
     guards (the Spec, the manifest, `etc/nginx.conf`) live in
     `cmd/telemetry/`, the sibling-service idiom. Cross-package end-to-end
     tests live in `internal/e2e/`, driving the real composed service over
     HTTP only. Never create a per-phase or root-level test file.
   - Run the suite and iterate until your increment is green (or you run out
     of clean room this turn).

5. **Format, vet, commit.**
   - `gofmt -w` everything you touched; `gofmt -l .` should print nothing.
   - `go build ./...` and `go vet ./...` must exit 0.
   - Commit this turn's increment (never an empty commit) with a message
     naming the phase, ending with the repo trailer naming the model you are
     running as:

     ```
     telemetry: phase NN — <what this increment did>

     Co-Authored-By: Claude <model> <noreply@anthropic.com>
     ```

     Never commit `project/loops/brief.md` (it is gitignored) and never
     `git add -A` from outside the files you touched.

6. Report `NEXT`.

## Project conventions

- **Language/module:** Go (repo targets `go 1.26`), module `telemetry`, on the
  shared `appkit` chassis over SQLite (`modernc.org/sqlite`, pure Go, no cgo).
  In-repo libraries come in through committed `replace` directives
  (`appkit => ../appkit`, `registry => ../registry`). Local dev runs in
  workspace mode via the repo-root `go.work` — do **not** set `GOWORK=off`
  unless the brief's Done bar explicitly names a `GOWORK=off go build ./...`
  check.
- **No event-plane role:** telemetry neither produces nor consumes events. No
  `eventplane` import, no `eventplane` requirement in `go.mod`, no `Feed`, no
  `Consumes`, no `Events` on the Spec.
- **Package layout:** `cmd/telemetry` (composition root: the `appkit.Spec` and
  route wiring — no domain logic), `internal/record` (record type, JSON codec,
  validation), `internal/db` (the `Store` plus embedded migrations),
  `internal/ingest` (the loopback ingest handler), `internal/retention` (the
  pruner), `internal/mcp` (tool table + embedded guide),
  `internal/telemetry` (the `Clock` seam), `internal/e2e` (the end-to-end
  layer). No package imports `cmd/`.
- **Suite is green means:** from `telemetry/`, `go build ./...`,
  `go vet ./...`, and `go test ./...` each exit 0 with no failures.
- **Formatting:** code is `gofmt`-clean — `gofmt -l .` prints nothing.
- **Port:** read at composition time with `registry.MustPort("telemetry")`.
  The port number appears as a literal in **no** Go source; the single literal
  lives in `etc/nginx.conf`, pinned to the registry by a guard test.
- **Time:** time enters the domain through the `Clock` interface
  (`Now() time.Time`); tests inject a deterministic clock and drive tickers as
  parameters. No test sleeps and no test depends on wall-clock ordering.
  Wall-clock is used for exactly two things — stamping `received_at` and
  computing the retention cutoff; a record's own `time` always comes from the
  reporter.
- **Timestamp normalization:** every timestamp is normalized on the way in to
  the fixed-width UTC form `2006-01-02T15:04:05.000000000Z` (Go layout
  `"2006-01-02T15:04:05.000000000Z07:00"` in UTC) before it is stored,
  compared, indexed, or put in a cursor.
- **Migrations:** schema lives in `internal/db/migrations/`, applied
  forward-only by the appkit runner. Committed migrations are **never** edited
  or deleted; a later change is a new file minted with
  `bin/create-migration telemetry <name>`, never hand-numbered.
- **Append-only by construction:** the `Store` has exactly one record write
  path (the idempotent insert) and exactly one delete path (the retention
  prune by cutoff). No update method, no delete-by-id, no raw-SQL entry point,
  and nothing on the MCP surface reaches either write path.
- **Errors:** tool errors use `appkit/mcp`'s closed codes (`validation`,
  `not_found`, `conflict`, `too_large`, `source_unavailable`, `internal`); the
  ingest surface answers with HTTP status codes only.
- **Test substrate rule:** a claim that depends on a real substrate is proven
  on that substrate. Storage, DDL, ordering, and query-plan claims open a real
  temp-file SQLite database the same way `serve` opens the real one and apply
  the real migration set through the appkit runner — never an in-memory fake
  store, never a substitutable store interface; `EXPLAIN QUERY PLAN` is the
  assertion for "this is index-backed". Transport claims bind a real
  `127.0.0.1` listener on an ephemeral port and speak real HTTP through the
  **registered route** with a real `http.Client` — never a hand-called
  `ServeHTTP`. Never satisfy such an id with a mock.
- **Test naming/tagging:** requirement-id tags live in `*_test.go` files; each
  id is covered by a clearly-named test citing the id in an adjacent
  `// R-XXXX-XXXX` comment, so grepping for the id finds the proof. Never gate
  a requirement test behind a skip condition, env flag, or build tag that the
  plain `go test ./...` run does not satisfy, and never convert a real failure
  signal into a skip — a test that doesn't run proves nothing.

## Boundaries

- Never read `project/design/`, `project/plan/`, or `project/product/` — the
  brief is your entire specification.
- Never edit `project/plan/STATUS.md` or delete a completed phase.
- Never delete or edit `project/loops/brief.md` — including its
  `## Verify feedback` region, which you read but never write.
- Never create, edit, build, or test anything outside `telemetry/`. If a phase
  needs a seam that does not exist yet in `appkit` or `registry`, that is a
  blocked phase to report in your message — never a licence to reach into a
  sibling workspace.
- Always end the turn on `NEXT` — you hand off every turn; you are never the
  step that ends the run.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:

- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next
  prompt.
- `DONE` — **terminal — never yours to report**: ending the run is never
  yours — finishing this phase completely, green suite and all open gaps
  closed, is still `NEXT`; only gather, finding no `⬜` phase left, ever
  reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g.
  `Built internal/db Store and covered 5 of 7 ids; suite green; committed.`

Keep `message` a single plain sentence — not a JSON object or code block.
