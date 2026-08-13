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

0. **Workspace identity guard.** Run `head -n 1 project/plan/STATUS.md`. It must
   print exactly `# repos — Plan Status`. If it does not:
   - Check `./repos/project/plan/STATUS.md` with the same command. If **that**
     one prints the expected title, your cwd drifted one directory shallow —
     `cd repos` and continue the procedure below.
   - Otherwise, do not proceed and **never report `DONE`** (that is never your
     status to report regardless): make no changes and report `NEXT` with a
     message naming the expected and observed titles.

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
  `eventplane => ../eventplane`) plus `require registry`. **No other production
  module dependency**: repos drives no agent engine and speaks to no external
  API, so `go.mod` requires neither `github.com/ikigenba/agentkit` nor any
  provider SDK. Two **test-only** dependencies exist —
  `github.com/dop251/goja` (D25's pure-function landing-filter tests) and
  `github.com/chromedp/chromedp` (D26's browser wiring proof) — imported only
  from `*_test.go`; never add either to a non-test source file. GOWORK mode is
  workspace (the repo-root `go.work`); the production build's `GOWORK=off` is
  `bin/ship repos`' business, not the gate's.
- **"The suite is green"** = the four commands in step 5 all succeed with zero
  failures (`gofmt -l .` prints nothing).
- **Test layers.** Per `root project/design/D23.md` (adopted locally as D16)
  this tree has exactly two layers, both in the default gate: **hermetic**
  (temp-dir filesystems, real SQLite through the real migration runner,
  `httptest`, local subprocesses including the real `git` binary against
  `t.TempDir()` bare repos and `file://`/`httptest` remotes, and the single
  headless-Chrome wiring proof) and **composed** (the install-layout boot smoke
  in `cmd/repos/main_test.go`, which builds and runs the real `cmd/repos`
  binary). There is **no live layer** and **no tree-local manual runbook** — so
  no `//go:build live` file exists, and **`t.Skip`, `t.Skipf`, and `t.SkipNow`
  may not appear in any `*_test.go` file here.** The three environmental
  preconditions beyond the Go toolchain — the real **`git`** binary, the real
  **`go`** binary, and the real **`google-chrome`** binary, each on `PATH` at
  test time — are hard failures when absent, never skips.
- **The assembled stack is out of gate.** Bringing the suite up and checking every
  service's health through nginx is the suite's **manual-layer** item, not this
  tree's. No test here runs `bin/start` or depends on a running suite, and no done
  bar may require one.
- **Real substrates, never mocked.** Git custody is exercised against the **real
  `git` binary** — bare repositories under `t.TempDir()`, real `git clone`/
  `fetch`/`push` from a temporary client working copy against the shipped
  smart-HTTP handler mounted on `httptest`, and real plumbing (`hash-object`,
  `read-tree`, `write-tree`, `commit-tree`, `update-ref`, `merge-tree`,
  `merge-base`, `archive`) for the commit paths — a mocked git cannot falsify
  ref/worktree behavior. The store runs against real temp-file SQLite through
  the embedded migration set. Time enters through a `Clock` seam with a
  deterministic injected clock. repos has **no service peers** — it calls no
  other service — so no test stubs a peer HTTP client, and no source path may
  make a request to github.com. No non-loopback network I/O anywhere in the
  gate.
- **Test placement — co-located, behavior-named, never gathered.** Package-local
  tests live in the **same package as the code they exercise**, in `*_test.go`
  files named for the behavior asserted: `internal/repos/*_test.go` (custody,
  events, the git door, merge, reads, ref updates, run tokens, the service,
  the store, writes), `internal/db/db_test.go`, `internal/mcp/mcp_test.go`,
  and `cmd/repos/*_test.go` (the AGENTS.md doc-truth check, brand icons, the
  landing page and its browser/deps/logic slices, the loopback guard, the
  nginx content assertion, and the skip-ban scan). The composed boot smoke is
  `cmd/repos/main_test.go`. **Never** create a per-phase or root-level
  catch-all test file.
- **Peers by name, addresses from the registry:** repos has no service peers,
  but when it needs its own address it asks `registry`
  (`registry.MustPort("repos")`, `registry.BaseURL("repos")` when rendering a
  `content_url`). No `127.0.0.1:30xx` literal in source.
- **Config:** env only, prefix `REPOS_`, read at the composition root, never
  below it. The whole set: `REPOS_STATE_DIR` (dev-only override),
  `REPOS_MAX_COMMIT_BYTES` (default `67108864`), and `REPOS_GIT_BIN` (default
  `git`). No credentials live in repos' environment.
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
  needs the suite up or reaches github.com.
- Never make an empty commit.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to verify.
- `DONE` — **terminal — never yours to report**: telling `ralph` to stop is
  never your job. Even a fully finished phase (green suite, every gap closed)
  is still `NEXT`; only gather ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g.
  `Rewrote AGENTS.md Tests section and added 2 tagged tests; suite green.`

Always report **`NEXT`** — you hand off every turn. Keep `message` a single plain
sentence — not a JSON object or code block.
