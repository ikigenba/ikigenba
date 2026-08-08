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
and the done bar. Do a bounded, idempotent turn of the phase's remaining work and
commit it. You do **not** decide completeness and you do **not** delete a
phase's `STATUS.md` line or body file — that is verify's job.

## Procedure

1. **Read the whole brief** — `project/loops/brief.md`, **both** its `## Contract`
   region and its `## Verify feedback` region. If the brief is missing or empty,
   make no changes and report `NEXT`.
2. **If the `## Verify feedback` region lists open gaps, those are this turn's
   priority.** They are the exact, command-grounded items the independent gate
   found unsatisfied last cycle (each tied to an `R-id` with the failing command
   and observed output). Close **those** first.
3. **See what already exists** so this turn is idempotent (never rebuild what is
   already there):
   - `grep -rn "R-XXXX-XXXX" --include=*_test.go .` — which ids already have tagged
     tests (substitute each real id from the brief's **Ids to cover**);
   - run the suite (below) and read the actual failures.
4. **Do as much of the brief as cleanly fits this one fresh context — ideally the
   whole phase**, so verify can pass it next cycle. Prefer fewer, fuller turns over
   many thin increments; an incomplete phase is simply re-attacked next cycle.
   - Build the named package(s) / edit the named files, consuming dependencies
     **only** through the brief's copied interface signatures.
   - For every id in the brief's **Ids to cover**, write a genuinely-asserting test
     tagged with a `// R-XXXX-XXXX` comment, exercising the behavior its
     requirement text describes. **A tagged test that does not truly assert the
     discriminating behavior is worse than none** — verify will treat it as
     uncovered.
   - **Never write a skip.** `t.Skip`, `t.Skipf`, and `t.SkipNow` are banned
     outright in this tree: dashboard has **no live layer**, so no test file
     carries a `//go:build live` constraint and there is no file in which a skip
     is legitimate. A tool a test needs (`git`, the `go` toolchain, `python3`) is
     an environmental precondition — declare it in `AGENTS.md` and let its absence
     be a hard failure. Likewise never gate a tagged test behind a build tag or an
     env variable nothing in the repo sets: verify treats an unreachable test as
     **uncovered**, however genuine its assertion reads, and a test that converts a
     real failure signal into a skip launders a gap into green.
   - **Never drop an existing tagged test.** Before committing, check this turn's
     own diff for dropped tags: any removed line matching `R-[A-Z0-9]{4}-[A-Z0-9]{4}`
     outside `project/` (`git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'`)
     must be restored first. A rewrite extends a file's tests; it never drops one
     already there.
5. **Run the full green suite** (all must pass, from `dashboard/`):

   ```
   gofmt -w .
   go build ./...
   go vet ./...
   gofmt -l .            # must print nothing
   go test ./...
   ```

   Plus any phase-specific check the brief's **Done bar** names.

6. **Commit this turn's increment** — a non-empty commit with a phase-naming
   message and the repo's `Co-Authored-By` trailer. `project/loops/brief.md` is
   gitignored, so `git add -A` will not stage it — good; leave it untouched.
7. Report **`NEXT`**.

## Project conventions (dashboard)

- **Module / toolchain:** Go 1.26, single `module dashboard` rooted at
  `dashboard/`; pure-Go SQLite `modernc.org/sqlite` (no cgo); `appkit` and
  `eventplane` are in-repo replace-siblings.
- **"The suite is green"** = `go build ./...`, `go vet ./...`, `gofmt -l .`
  (prints nothing), and `go test ./...` all succeed with zero failures, run from
  `dashboard/`.
- **Test-file glob:** `*_test.go` — requirement-id tags live only in files matching
  it.
- **Test placement — co-located, behavior-named, never gathered.** Unit and
  HTTP-level tests live in the **same package as the code they exercise**, in
  `*_test.go` files named for the behavior asserted:
  - `internal/server/*_test.go` (`package server`) — HTTP-level tests drive the
    real route table via `(*app).routes()` with `httptest`, asserting status
    codes, `Location` headers, and rendered HTML (see the existing
    `index_test.go`, `grants_test.go`, `login_test.go`, `landing_composition_test.go`);
  - `internal/telemetry/*_test.go`, `internal/identity/*_test.go`,
    `internal/googleidp/*_test.go`, `internal/githubidp/*_test.go`,
    `internal/metrics/*_test.go` — package-local unit tests;
  - `cmd/dashboard/docs_test.go` — the read-from-disk assertions over committed
    docs; `cmd/dashboard/main_test.go` — the composed boot smoke.
  **Never** create a per-phase or root-level test file, and never gather multiple
  packages' tests into one file. A phase is one package; its tests live with it.
- **Test layers.** dashboard has **hermetic**, **composed**, and **manual**
  layers, and **no live layer**. Hermetic covers the package suites and the
  shipped-file guards (`etc/nginx.conf`, `etc/manifest.env`); composed is the boot
  smoke in `cmd/dashboard/main_test.go`, which builds and runs the real binary;
  manual is the interactive Google/GitHub sign-in and live apex routing, exercised
  by the operator at deploy time and never by the gate. No test outside the (absent)
  live layer may contact a non-loopback address, read a credential, or change
  behavior based on ambient secrets.
- **Real substrate where a claim needs it.** Session/identity/store tests run
  against a **real temp `modernc.org/sqlite`** migrated by the appkit runner (as
  the existing server tests do); metric readers run against temp trees / fixtures
  at injected roots (free-disk reads a real `statfs`); Google and GitHub are
  driven through their injectable test seams (crafted id_token / `httptest`
  fakes), never a live network.
- **Migrations** are created with `bin/create-migration dashboard <name>`
  (timestamped, immutable); never edit or renumber a committed migration.
- **Doc truth is a Go test, not a grep in a runbook.** Claims about `AGENTS.md`
  are proven by an ordinary hermetic test in `cmd/dashboard/docs_test.go` that
  reads the committed file **from disk** and asserts over its content, so the
  claim is re-checked on every `go test ./...`. When a phase changes such a
  claim, edit the doc **and** keep its test true.
- **`AGENTS.md` / `CLAUDE.md` are one file** (`dashboard/CLAUDE.md` is a symlink
  to `dashboard/AGENTS.md`). Edit **`AGENTS.md`**; a refusal to write through the
  symlink is expected.
- **Nginx-fragment work** (`dashboard/etc/nginx.conf`) is proven by a Go test
  that reads the file from disk and asserts its content — nginx itself is
  never run by the suite.

## Boundaries

- Never read `project/design/*`, `project/plan/*`, or `project/product/*` — the
  brief is your only input. If it seems insufficient, do what it does support and
  report `NEXT`; gather will re-author it if the phase resets.
- Never edit `project/plan/STATUS.md` or delete a phase's line/body file — that is
  verify's sole right.
- Never delete or edit `project/loops/brief.md`, including its `## Verify feedback`
  region — you **read** the feedback but never write it.
- Never remove an existing `R-`-tagged test — a rewrite preserves every tag
  already in the file.
- Never write `t.Skip`, `t.Skipf`, or `t.SkipNow` anywhere in this tree.
- Never make an empty commit.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to verify.
- `DONE` — **terminal — never yours to report**: ending the run is never yours —
  finishing this phase completely, green suite and all open gaps closed, is still
  `NEXT`; only gather ever reports `DONE`, on finding no `⬜` phase left or a
  blocked phase awaiting the operator.
- `message` — one short, plain sentence describing what happened, e.g.
  `Rewrote the AGENTS.md Tests section and added 2 tagged tests in cmd/dashboard/docs_test.go; suite green.`

Always report **`NEXT`** — you hand off every turn. Keep `message` a single plain
sentence — not a JSON object or code block.
