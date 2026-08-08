---
harness: codex
model: gpt-5.6-sol
---
# build — do one bounded turn of the brief's remaining work

You run in a fresh, isolated context, one turn per invocation, as the middle step
of an unattended `gather → build → verify` loop. `ralph` runs from the service
root (`webhooks/`), so every path below is service-root-relative.

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
   - **Never skip.** `t.Skip`, `t.Skipf`, and `t.SkipNow` are banned in this tree
     (see conventions); a missing tool is a hard failure, never a skip.
   - **Never drop an existing tagged test.** Before committing, check this turn's
     own diff for dropped tags: any removed line matching `R-[A-Z0-9]{4}-[A-Z0-9]{4}`
     outside `project/` (`git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'`)
     must be restored first. A rewrite extends a file's tests; it never drops one
     already there.
5. **Run the full green suite** (all must pass, from `webhooks/`):

   ```
   cd webhooks && go build ./...
   cd webhooks && go vet ./...
   cd webhooks && gofmt -l .          # must print nothing
   cd webhooks && go test ./...
   ```

   Nothing in this gate requires the suite to be up: **no test drives the `:8080`
   dev front door**, and none may be added.
6. **Commit this turn's increment** — a non-empty commit with a phase-naming
   message and the repo's `Co-Authored-By` trailer. `project/loops/brief.md` is
   gitignored, so `git add -A` will not stage it — good; leave it untouched.
7. Report **`NEXT`**.

## Project conventions (webhooks)

- **Toolchain:** Go (`go 1.26`), module path `webhooks`, built on the `appkit`
  chassis over `modernc.org/sqlite` (pure-Go, no cgo); in-repo libs via committed
  `replace` (`appkit => ../appkit`, `eventplane => ../eventplane`,
  `registry => ../registry`). Loopback port comes from
  `registry.MustPort("webhooks")` (`3006`), never a hard-coded `127.0.0.1:30xx`
  literal. GOWORK mode is workspace (the repo-root `go.work`); the production
  build's `GOWORK=off` is `bin/ship webhooks`' business, not the gate's.
- **"The suite is green"** = the four commands in step 5 all succeed with zero
  failures (`gofmt -l .` prints nothing).
- **Test layers.** Per `root project/design/D23.md` this tree has exactly two
  layers, both in the default gate: **hermetic** (temp-dir filesystems, real
  SQLite through the real migration runner, `httptest`, committed-file reads,
  local subprocesses) and **composed** (the boot smokes that build the real
  `cmd/webhooks` binary and run its `serve` verb — the install-layout smoke in
  `cmd/webhooks/main_test.go` and the launch/health smoke in
  `internal/e2e/e2e_test.go`, each reaching the process over loopback). There is
  **no live layer** and **no tree-local manual runbook** — so no `//go:build live`
  file exists, and **`t.Skip`, `t.Skipf`, and `t.SkipNow` may not appear in any
  `*_test.go` file here.** A tool the tests need (`go`, `bash`, `grep`) is an
  environmental precondition: a hard failure when absent, never a skip. The
  `internal/e2e` package name is an **informal alias, not a layer**.
- **The assembled stack is out of gate.** Bringing the suite up and checking
  every service's health through nginx on `:8080` is the suite's **manual-layer**
  item, not this tree's. No test here drives `:8080`, spawns `bin/start`, or
  depends on a running suite, and no done bar may require one.
- **Real substrate, no mocks for DB/outbox.** Tests run against **real temp-file
  SQLite** (`db.Open`, `t.TempDir()` — never `:memory:`) with a **deterministic
  injected `Clock`**; events run against the real `eventplane/outbox`. A mocked
  store/outbox cannot falsify a PK/UNIQUE constraint, durability across reopen,
  owner scoping, or Append-time registry validation, so it is forbidden. Handlers
  are exercised with `httptest`; the nginx fragment is proven by reading
  `etc/nginx.conf` from disk and asserting over its content (nginx is not run by
  the suite).
- **Test placement — co-located, behavior-named, never gathered.** Package-local
  tests live in the **same package as the code they exercise**, in `*_test.go`
  files named for the behavior asserted (`internal/webhooks/*_test.go`,
  `internal/db/*_test.go`, `internal/mcp/*_test.go`, `cmd/webhooks/*_test.go`).
  The cross-package tests live in the single dedicated `internal/e2e/` package,
  and the nginx content assertion in `cmd/webhooks/nginx_test.go`. **Never**
  create a per-phase or root-level catch-all test file.
- **DB / migrations:** ordered, immutable SQL in `internal/db/migrations/`,
  applied forward-only by the appkit runner. New migrations only via
  `bin/create-migration webhooks <name>` (timestamped); numbers are never
  hand-picked and a committed migration is never edited. No production data may
  be dropped.
- **Determinism seam:** time enters the domain through a `Clock`; tests inject a
  deterministic clock. The DB handle is the appkit-owned single-writer `*sql.DB`
  (`rt.DB()`), shared with the producer outbox.
- **Doc-truth work** (a brief whose done bar asserts the content of `AGENTS.md`
  or another committed doc) is satisfied by editing the real doc **and** by the
  tagged test that reads that committed file from disk — never by a fixture copy.

## Boundaries

- Never read `project/design/*`, `project/plan/*`, or `project/product/*` — the
  brief is your only input. If it seems insufficient, do what it does support and
  report `NEXT`; gather will re-author it if the phase resets.
- Never edit `project/plan/STATUS.md` or delete a phase's line/body file — that is
  verify's sole right.
- Never delete or edit `project/loops/brief.md`, including its `## Verify feedback`
  region — you **read** the feedback but never write it.
- Never introduce a `t.Skip` variant, an env gate, or a build tag that holds a
  requirement test out of `go test ./...`; never add a test that needs the suite
  up or reaches `:8080`.
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
  `Rewrote AGENTS.md Tests section and added 2 tagged tests; suite green.`

Always report **`NEXT`** — you hand off every turn. Keep `message` a single plain
sentence — not a JSON object or code block.
