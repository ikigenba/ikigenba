---
harness: codex
model: gpt-5.6-sol
---

# build — advance the current phase by one bounded increment

You run in a fresh, isolated context, one turn per invocation, as the middle step
of an unattended `gather → build → verify` loop. `ralph` runs from the service
root (`sites/`), so every path below is service-root-relative.

You read **only** `project/loops/brief.md` — never the plan, design, or product
docs. The brief is the complete and only contract for the one phase in flight: it
carries the realized Decision's full design prose, the exact ids to cover with
their requirement text, the files to touch, the dependency interface signatures,
and the done bar. Do a bounded, idempotent turn of the phase's remaining work and
commit it. You do **not** decide completeness and you do **not** delete a
phase's `STATUS.md` line or body file — that is verify's job.

## Procedure

0. **Workspace identity guard.** Run `head -n 1 project/plan/STATUS.md`. It must
   print exactly `# sites — Plan Status`. If it does not, check
   `./sites/project/plan/STATUS.md` for the same title: if that one matches,
   `cd sites` and continue. Otherwise report `NEXT` with a message naming the
   expected and observed titles. Never report `DONE` — that status is never
   yours to report.

1. **Read the whole brief.** Read `project/loops/brief.md` in full: the
   contract region and the `## Verify feedback` region both. If it is missing
   or empty, make no changes and report `NEXT` with a message saying so.

2. **Close open gaps first.** If the feedback region lists open gaps (each an
   `R-id` tied to an exact failing command/output from the last verify run),
   fix those first — they are the gate's own findings from last cycle.

3. **Do as much of the brief as cleanly fits this turn.** Prefer one full turn
   over many thin increments — an incomplete phase is simply re-attacked next
   cycle. Before writing:
   - see what already exists: `grep -rn "R-" --include='*_test.go' .` scoped to
     the packages the brief names, and run
     `cd sites && go test ./...` to read current failures;
   - build the named package(s)/files from the brief's `Files to touch`,
     consuming any dependency **only** through the interface signatures copied
     into the brief — never by reading that package's internals;
   - write id-tagged, genuinely asserting tests for every id in the brief's
     "Ids to cover" list, each as a `// R-XXXX-XXXX` comment directly above the
     assertion, **co-located with the code it exercises and named for the
     behavior** — package-local (e.g. `internal/sites/store_test.go` beside
     `internal/sites/store.go`), never gathered into a per-phase or
     root-level test file. The one exception is a cross-package/boot-level
     assertion (a composed-layer check driving the real built binary or the
     assembled MCP surface): that goes in `cmd/sites/main_test.go`, sites'
     single home for the composed layer — never a new file for it;
   - if the brief's Decision touches the local D23 headless-browser wiring
     test, treat `google-chrome` on `PATH` as a hard precondition, never
     something to skip around;
   - format: `cd sites && gofmt -w .`;
   - run the full gate: `cd sites && go build ./...`,
     `cd sites && go vet ./...`, `cd sites && gofmt -l .` (must print
     nothing), `cd sites && go test ./...`.

4. **Protect existing coverage.** Before committing, check this turn's own diff
   for dropped tags: `git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'`
   restricted to paths outside `project/`. Any such removed line must be
   restored — a rewrite may extend a file's tests, it never drops a tagged
   one.

5. **Commit.** If the tree has a real, non-empty diff, commit this turn's
   increment with a phase-naming message (e.g. `sites: Phase 7 — internal/sites
   store tests`) and the repo's trailer convention. Never commit an empty diff.

6. Report `NEXT`.

## Project conventions

- Toolchain: Go 1.26, module `sites`, pure-Go SQLite (`modernc.org/sqlite`, no
  cgo).
- Build/vet: `cd sites && go build ./...`, `cd sites && go vet ./...`.
- Test: `cd sites && go test ./...`. Requirement-id tags live in `*_test.go`
  files as a `// R-XXXX-XXXX` comment directly above the assertion.
- **"The suite is green"** means all four succeed with zero failures:
  `go build ./...`, `go vet ./...`, `gofmt -l .` (no output), `go test ./...`.
  Green **hard-requires** `google-chrome` on `PATH` for the D23 browser-wiring
  test — its absence is a red suite, never a skip.
- Test layers: **hermetic** (the bulk — httptest, temp-dir SQLite through the
  real appkit migration runner, goja-evaluated landing JS, recording
  RoundTrippers, the local headless-Chrome wiring test) and **composed** (the
  boot smoke in `cmd/sites/main_test.go`, which builds and runs the real
  `cmd/sites` binary). No live layer, no manual runbook in this tree.
- Formatting: `gofmt`-clean; `gofmt -l .` must print nothing.
- Migrations: `sites/internal/db/migrations/`, forward-only, additive only.
  Never hand-name or edit a migration; use `bin/create-migration sites <name>`.
- GOWORK: tests/build/vet run in workspace mode through the repo-root
  `go.work`; production build forces `GOWORK=off` and is out of scope for the
  gate.
- Test placement: unit tests are package-local, named for the behavior
  (`<file>_test.go` beside `<file>.go`); the single home for cross-package /
  boot-level integration checks is `cmd/sites/main_test.go`. Never create a
  per-phase or root-level test file.

## Boundaries

- Never read `project/product/`, `project/design/`, or `project/plan/` — the
  brief is the complete contract.
- Never remove an existing `R-`-tagged test.
- Never edit `project/plan/STATUS.md` or delete a phase file.
- Never write `project/loops/brief.md` (contract or feedback region).
- Always report `NEXT` — `DONE` is never yours to report.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal — never yours to report**: telling `ralph` to stop is
  never your job. Even a fully finished phase (green suite, every gap closed)
  is still `NEXT`; only gather ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g.
  `implemented internal/sites store writes for Phase 7, suite green`.

Keep `message` a single plain sentence, not a JSON object or code block.
