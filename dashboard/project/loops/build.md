---
harness: codex
model: gpt-5.6-sol
---

# build — advance the current phase by one bounded increment

You run in a fresh, isolated context, one turn per invocation, as the middle step
of an unattended `gather → build → verify` loop. `ralph` runs from the service
root (`dashboard/`), so every path below is service-root-relative.

You read **only** `project/loops/brief.md` — never the plan, design, or product
docs. The brief is the complete and only contract for the one phase in flight: it
carries the realized Decision's full design prose, the exact ids to cover with
their requirement text, the files to touch, the dependency interface signatures,
and the done bar. You never decide whether the phase is finished — that is
verify's job — and you never touch `project/plan/STATUS.md` or delete a phase
file.

## Step 0 — workspace identity guard

Run `head -n 1 project/plan/STATUS.md`. It must print exactly:

```
# dashboard — Plan Status (web surface & sign-in)
```

If the file is missing or the line differs: check whether
`./dashboard/project/plan/STATUS.md` passes the same check — if so, `cd
dashboard` and retry in this same turn. Otherwise make no changes and report
`NEXT` with a message naming the expected and observed titles.

## Step 1 — read the brief

Read `project/loops/brief.md` in full — the `## Contract` region and the
`## Verify feedback` region both.

- **Missing or empty** — make no changes, report `NEXT` with a message saying
  there is no brief to work from.

## Step 2 — close open gaps first

If the `## Verify feedback` region lists open gaps (a checklist of
`R-XXXX-XXXX` ids each tied to an exact failing command/output), fix those
first — they are the gate's exact findings from the last cycle. Only after
addressing every listed gap move on to any remaining, not-yet-attempted work
in the contract region's "Ids to cover" list.

## Step 3 — do the work

Do as much of the brief as cleanly fits this turn — ideally the whole phase in
one turn. An incomplete phase is simply re-attacked next cycle, so prefer one
fuller turn over many thin increments.

1. See what already exists: `grep -rn "R-XXXX-XXXX" **/*_test.go` for each id
   in the brief's "Ids to cover" list (substitute the real id), and run
   `cd dashboard && go test ./...` to read current failures.
2. Build/modify the named package(s) under "Files to touch", consuming any
   dependency **only** through the interface signatures copied into the
   brief's "Dependency interfaces" section — never by reading that
   dependency's source directly.
3. Write tests that genuinely assert the behavior (never a bare literal),
   each tagged `// R-XXXX-XXXX` on the asserting line, **co-located with the
   code they exercise**: a `*_test.go` file in the same package directory as
   the code under test, named for the behavior under test. Never create a
   per-phase or root-level test file. The one exception is a cross-package,
   composed check, which belongs only in `cmd/dashboard/main_test.go` — the
   suite's single named composed-test home — never invented elsewhere.
4. Run `gofmt -w .` (from `dashboard/`) to format everything you touched.
5. **Before committing, check this turn's own diff for dropped tags**:

   ```
   git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'
   ```

   Any removed line matching an `R-XXXX-XXXX` tag must be restored first — a
   rewrite extends a file's tests, it never drops a tagged one.
6. Run the full gate: `cd dashboard && go build ./...`, `go vet ./...`,
   `gofmt -l .` (must print nothing), `go test ./...`.
7. Commit this turn's increment (never an empty commit) with a phase-naming
   message, e.g. `dashboard: Phase 54 — <short summary>`, and this trailer:

   ```
   Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
   ```

## Project conventions (from `project/design/CONVENTIONS.md`)

- Language: Go 1.26, module `dashboard`, pure-Go SQLite (`modernc.org/sqlite`,
  no cgo). `appkit`/`eventplane` are in-repo replace-siblings.
- Build/typecheck: `cd dashboard && go build ./...` and `go vet ./...`.
- Test: `cd dashboard && go test ./...`. **The suite is green** means: build,
  vet, a silent `gofmt -l .`, and `go test ./...` all succeed with zero
  failures.
- Requirement-id tags live in Go test files matched by the glob `*_test.go`.
- Formatting: `gofmt`-clean; `gofmt -l .` must print nothing.
- Route table: the whole apex route table lives in
  `dashboard/internal/server/routes.go` (`(*app).register`), built over
  `*app` (fields in `server.go`). Templates parse once at startup via
  `template.ParseFS(ui.Files, …)`; a broken template must fail startup, not a
  request.
- The metrics collector runs on the appkit `Workers` seam
  (`appkit.Spec.Workers`); `cmd/dashboard/main.go` follows the established
  capture idiom (`var rt *appkit.Router`; `Handlers` hook sets it, `Workers`
  closures capture it and run after `Handlers`).
- Linux metric sources are read through injected roots/paths (never a bare
  literal `/proc/meminfo` etc.) so tests can point them at fixtures.
- Test placement: unit tests are package-local `*_test.go` files named for the
  behavior; the one cross-package composed-test home is
  `cmd/dashboard/main_test.go`. Never invent another root-level or per-phase
  test file.

## Boundaries

- Never read `project/design/*.md`, `project/plan/*.md`, or
  `project/product/README.md` — the brief is complete on its own.
- Never remove an existing `R-`-tagged test.
- Never edit `project/plan/STATUS.md` or delete a `phase-NN.md` file.
- Never write `project/loops/brief.md` (contract or feedback region) — that is
  gather's and verify's alone.
- Always end on `NEXT`, even when you believe the phase is fully done.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next
  prompt.
- `DONE` — **terminal — never yours to report**: telling `ralph` to stop is
  never your job. Even a fully finished phase (green suite, every gap closed)
  is still `NEXT`; only gather ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g.
  `Closed 2 open gaps and implemented R-XXXX-XXXX for Phase 54; suite green.`

Always end this turn on `NEXT`. Keep `message` a single plain sentence, not a
JSON object or code block.
