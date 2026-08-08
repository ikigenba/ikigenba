---
harness: codex
model: gpt-5.6-sol
---
# build — do one bounded turn of the brief's remaining work

You run in a fresh, isolated context, one turn per invocation, as the middle step
of an unattended `gather → build → verify` loop. `ralph` runs from the service
root (`repos/`), so every path below is service-root-relative.

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
5. **Run the full green suite** (all must pass, from `repos/`):

   ```
   cd repos && go build ./...
   cd repos && go vet ./...
   cd repos && gofmt -l .          # must print nothing
   cd repos && go test ./...
   ```

   Nothing in this gate requires the suite to be up: no test runs against
   `bin/start`, and none may be added.
6. **Commit this turn's increment** — a non-empty commit with a phase-naming
   message and the repo's `Co-Authored-By` trailer. `project/loops/brief.md` is
   gitignored, so `git add -A` will not stage it — good; leave it untouched.
7. Report **`NEXT`**.

## Project conventions (repos)

- **Module / toolchain:** Go 1.26, module path `repos`, a standalone module at
  `repos/` on the `appkit` chassis over `modernc.org/sqlite` (pure-Go, no cgo);
  in-repo libs via committed `replace` (`appkit => ../appkit`,
  `eventplane => ../eventplane`) plus `require registry`; the agent engine via the
  pinned tagged module `github.com/ikigenba/agentkit` at the suite-wide pin.
  GOWORK mode is workspace (the repo-root `go.work`); the production build's
  `GOWORK=off` is `bin/ship repos`' business, not the gate's.
- **"The suite is green"** = the four commands in step 5 all succeed with zero
  failures (`gofmt -l .` prints nothing).
- **Test layers.** Per `root project/design/D23.md` this tree has exactly two
  layers, both in the default gate: **hermetic** (temp-dir filesystems, real
  SQLite through the real migration runner, `httptest`, local subprocesses
  including the real `git` binary against `file://` fixture remotes) and
  **composed** (the install-layout boot smoke in `cmd/repos/main_test.go`, which
  builds and runs the real `cmd/repos` binary). There is **no live layer** and
  **no tree-local manual runbook** — so no `//go:build live` file exists, and
  **`t.Skip`, `t.Skipf`, and `t.SkipNow` may not appear in any `*_test.go` file
  here.** The two environmental preconditions beyond the Go toolchain — the real
  **`git`** binary and the **`go`** binary, each on `PATH` at test time — are hard
  failures when absent, never skips.
- **The assembled stack is out of gate.** Bringing the suite up and checking every
  service's health through nginx is the suite's **manual-layer** item, not this
  tree's. No test here runs `bin/start` or depends on a running suite, and no done
  bar may require one.
- **Real substrates, never mocked.** Git custody is exercised against the **real
  `git` binary** over local bare fixture remotes (`git init --bare` in
  `t.TempDir()`, `file://` URLs) — a mocked git cannot falsify ref/worktree
  behavior. The store runs against real temp-file SQLite through the embedded
  migration set. Suite peers (github, webhooks) are `httptest` stubs that record
  requests. Time enters through a `Clock` seam with a deterministic injected
  clock. No non-loopback network I/O anywhere in the gate.
- **Test placement — co-located, behavior-named, never gathered.** Package-local
  tests live in the **same package as the code they exercise**, in `*_test.go`
  files named for the behavior asserted (`internal/repos/*_test.go`,
  `internal/db/*_test.go`, `internal/mcp/tools_test.go`,
  `internal/runner/runner_test.go`, `internal/tools/tools_test.go`,
  `cmd/repos/*_test.go`). The composed boot smoke is `cmd/repos/main_test.go` and
  the nginx content assertion `cmd/repos/nginx_test.go`. **Never** create a
  per-phase or root-level catch-all test file.
- **Peers by name, addresses from the registry:** name peers in code and ask
  `registry` where they live (`registry.MustPort("repos")`,
  `registry.BaseURL("github")`). No `127.0.0.1:30xx` literal in source.
- **Outbound HTTP is proven at the injected client**, not by re-asserting the
  chassis: tests supply a `*http.Client` whose `Transport` is a recording
  `RoundTripper` and assert the two repos-owned facts — the request reaches the
  wire through that client, and it carries the call's live context. Setting the
  header and emitting the `outbound` record are appkit's behaviors with appkit's
  ids; never re-prove them here.
- **Config:** env only, prefix `REPOS_`, read at the composition root, never
  below it.
- **Migrations** are created with `bin/create-migration repos <name>`
  (timestamped, immutable); never edit or renumber a committed migration. No
  production data may be dropped.
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
  requirement test out of `go test ./...`; never mock `git`; never add a test that
  needs the suite up.
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
