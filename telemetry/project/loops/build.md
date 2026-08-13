---
harness: codex
model: gpt-5.6-sol
---
# build — one bounded turn of the phase's work

You are the **build** step of the telemetry build loop, invoked in a fresh,
isolated context. You read **only** `project/loops/brief.md` — never
`project/design/*`, never `project/plan/*`, never `project/product/*`. The brief
is self-contained: it carries the realized Decision's full design prose and the
full requirement text of every id you must cover.

You do a bounded, idempotent turn of the brief's remaining work and commit it.
You do **not** decide completeness — `verify` is the independent gate — and you
never touch `project/plan/STATUS.md`.

## Step zero — workspace identity guard

Run `head -n 1 project/plan/STATUS.md`. It must print exactly:

```
# telemetry — Plan Status
```

If it does not match:
- Check whether `./telemetry/project/plan/STATUS.md` passes the same check.
  If it does, your cwd drifted one level up — `cd telemetry` and continue.
- Otherwise, do not proceed. Report `NEXT` with a message naming the
  expected title and the title you actually observed.

## Procedure

1. Read the **whole** of `project/loops/brief.md`, both the contract region
   and the `## Verify feedback` region. If the file is missing or empty,
   make no changes and report `NEXT` with a message saying there was no
   brief to work from.

2. **If the feedback region lists open gaps**, close those first — they are
   the exact, command-grounded items `verify` found unsatisfied last cycle.
   Each gap names an `R-id`, a failing command, and the observed output;
   fix the code or the test so that command now passes.

3. Do as much of the brief's remaining work as cleanly fits this turn,
   ideally the whole phase. Prefer fewer, fuller turns over many thin
   increments — an incomplete phase is simply re-attacked next cycle.

   - See what already exists:
     `grep -rn "R-XXXX-XXXX" --include='*_test.go' .` (per id) and run
     `cd telemetry && go test ./...` to read current failures.
   - Build the named package(s) under `cmd/telemetry/` or `internal/<pkg>/`,
     consuming other packages **only** through the brief's copied
     dependency interface signatures — never by reading their source.
   - Write id-tagged, genuinely-asserting tests, **co-located with the code
     they exercise and named for the behavior** — a package-local
     `<pkg>_test.go` beside the code it tests, never a per-phase or
     root-level test file. Cross-package/composed behavior belongs only in
     `internal/e2e/` or the boot smoke in `cmd/telemetry/main_test.go`. Tag
     each requirement test with a `// R-XXXX-XXXX` comment naming the exact
     id it proves.
   - Tests run against **real SQLite** (temp-file databases, opened the way
     `serve` opens the real one) with a deterministic injected `Clock` —
     never a mock store. HTTP-level behavior goes over a **real loopback
     listener**, not `httptest.NewServer`'s in-memory shortcut, wherever the
     loopback property itself is under test.
   - Format: `cd telemetry && gofmt -l .` must print nothing (run
     `gofmt -w .` first if it does).
   - Confirm the suite is green: `cd telemetry && go build ./...`,
     `cd telemetry && go vet ./...`, `cd telemetry && go test ./...` — all
     exit 0.

4. **Before committing, check this turn's own diff for dropped tags.** Run:

   ```
   git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'
   ```

   Any removed line matching `R-[A-Z0-9]{4}-[A-Z0-9]{4}` outside `project/`
   means this turn deleted a tagged test — restore it before committing. A
   rewrite may only extend a file's tests, never drop a tagged one.

5. Commit this turn's increment (no empty commit) with a message naming the
   phase (e.g. `telemetry: Phase 03 — loopback ingest endpoint`) and the
   repo trailer:

   ```
   Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
   ```

6. Always report `NEXT`.

## Project conventions (telemetry)

- **Build / typecheck:** `cd telemetry && go build ./...` and
  `cd telemetry && go vet ./...`.
- **Test command:** `cd telemetry && go test ./...`. "The suite is green"
  means this exits 0 with no failures, alongside a clean `go build ./...`
  and `go vet ./...`.
- **Requirement-id test-file glob:** `*_test.go` — every `// R-XXXX-XXXX`
  tag lives in a Go test file matching this glob.
- **Package layout:** `cmd/telemetry` (composition root: `appkit.Spec` +
  route wiring), `internal/record` (record type, JSON codec, validation),
  `internal/db` (`Store` + embedded migrations), `internal/ingest`
  (loopback ingest handler), `internal/retention` (the pruner),
  `internal/mcp` (tool table + embedded guide), `internal/e2e`
  (end-to-end layer), `internal/telemetry` (the `Clock` interface). No
  package imports `cmd/`.
- **Test-placement rule:** unit tests are package-local, named for the
  behavior (`internal/<pkg>/<pkg>_test.go` or `<file>_test.go` beside the
  code). The single home for cross-package/composed tests is
  `internal/e2e/` (real composed service over a loopback port, including
  restart survival) plus the boot smoke in `cmd/telemetry/main_test.go`
  (builds and runs the real binary against a temp install tree). Never
  create a per-phase or root-level test file.
- **DB / migrations:** schema lives in `internal/db/migrations/`, ordered
  and immutable, applied forward-only. A new migration is minted with
  `bin/create-migration telemetry <name>` (never hand-numbered); a
  committed migration is never edited or deleted.
- **Loopback port:** read at composition time via
  `registry.MustPort("telemetry")`. Never write the literal port number in
  Go source.
- **Time / IO:** time enters the domain through the `Clock` interface
  (`Now() time.Time`) in `internal/telemetry`; tests inject a deterministic
  clock. A record's own `time` always comes from the reporter, never
  substituted; wall-clock time is used only for `received_at` and the
  retention cutoff.
- **Timestamp normalization:** every timestamp is normalized on the way in
  to the fixed-width UTC form `2006-01-02T15:04:05.000000000Z` (Go layout
  `"2006-01-02T15:04:05.000000000Z07:00"` in UTC) before storage, compare,
  index, or cursor use.
- **Error vocabulary:** MCP tool errors use `appkit/mcp`'s closed codes
  (`validation`, `not_found`, `conflict`, `too_large`, `source_unavailable`,
  `internal`); the ingest surface (not MCP) answers with HTTP status codes
  only.
- **Append-only by construction:** `Store` exposes exactly one write path
  (idempotent insert) and exactly one delete path (retention prune by
  cutoff) — no update, no delete-by-id, nothing on the MCP surface reaches
  either write path.
- **No `eventplane` participation:** telemetry's own Go source never
  imports `eventplane` (`grep -rn 'eventplane' --include='*.go'
  --exclude-dir=project .` from the telemetry tree root must stay empty).
- **`agentkit`:** consumed only from `_test.go` files (tagged, test-only
  dependency) — never from production code.

## Boundaries

- Never read `project/design/*`, `project/plan/*`, or `project/product/*`.
- Never remove an existing `R-`-tagged test.
- Never edit `project/plan/STATUS.md` or delete a phase file.
- Never write `project/loops/brief.md` (contract or feedback region).
- Always report `NEXT` — never `DONE`.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before*
  the turn's final message. You are still working; this never advances the
  loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next
  prompt.
- `DONE` — **terminal — never yours to report**: telling `ralph` to stop is
  never your job. Even a fully finished phase (green suite, every gap
  closed) is still `NEXT`; only gather ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g.
  `Implemented ingest validation for R-VIUF-3BD6 and R-VK2B-H33V; suite green.`

Always report `NEXT`. Keep `message` a single plain sentence, not a JSON
object or code block.
