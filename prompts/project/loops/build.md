---
harness: codex
model: gpt-5.6-sol
---
# build — advance the current phase by one bounded increment

You are the **build** step of the prompts build loop, invoked in a fresh,
isolated context. You read **only** `project/loops/brief.md` — never
`project/design/*`, never `project/plan/*`, never `project/product/*`. The brief
is self-contained: it carries the realized Decision's full design prose and the
full requirement text of every id you must cover.

You do a bounded, idempotent turn of the brief's remaining work and commit it.
You do **not** decide completeness — `verify` is the independent gate — and you
never touch `project/plan/STATUS.md`.

All paths below are relative to the **service root** (`prompts/`), which is your
working directory.

## Procedure

0. **Workspace identity guard.** Run `head -n 1 project/plan/STATUS.md`. It
   must print exactly `# prompts — Plan Status`. If it does not, do not proceed
   and do not report `DONE` (you never report `DONE` anyway — see below). Check
   whether `./prompts/project/plan/STATUS.md` passes the same check: if it
   does, `cd prompts` and continue. Otherwise make no changes and return `NEXT`
   with a message naming the expected title and what was actually observed.

1. **Read the whole brief** — the contract region **and** the `## Verify
   feedback` region. If `project/loops/brief.md` is missing or empty, make no
   changes and return `NEXT`.

2. **If the feedback region lists open gaps, they are this turn's priority.**
   They are the exact, command-grounded items the independent gate found
   unsatisfied last cycle, each tied to one `R-id` with the failing command and
   its observed output. Close those first, then continue with the rest of the
   brief.

3. **See what already exists** before writing anything:

   ```
   grep -rn "R-XXXX-XXXX" . --include=*_test.go     # per id from the brief
   go test ./...                                    # read the real failures
   ```

4. **Do as much of the brief as cleanly fits this turn — ideally the whole
   phase**, so `verify` can pass it next cycle. Prefer fewer, fuller turns over
   many thin increments; an incomplete phase is simply re-attacked next cycle.

   - Build the named package(s), consuming dependencies **only** through the
     interface signatures the brief copied in.
   - Write id-tagged tests that **genuinely assert** the behavior: a
     `// R-XXXX-XXXX` comment on a test that would fail against a wrong
     implementation, never a bare literal and never a tag on a test that asserts
     something weaker than the requirement text.
   - **Never write a skip.** `t.Skip`, `t.Skipf`, and `t.SkipNow` are banned
     outside `live`-tagged files, and prompts has **no live layer** — so they are
     banned everywhere in this tree. A tagged test that a build tag, env flag, or
     skip condition holds out of `go test ./...` is unreachable and counts as
     **uncovered** no matter how genuine its assertion reads; a test that converts
     a real failure (non-zero exit, unparseable output) into a skip launders a gap
     into green and also counts as uncovered. A missing tool (including the `git`
     binary the tree's own real-git tests need) is a hard failure, not a skip.
   - Run `gofmt -w` on everything you touched.

5. **Check your own diff for dropped tags before committing:**

   ```
   git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'
   ```

   Any removed line matching an `R-` tag outside `project/` must be restored
   first. A rewrite **extends** a file's tests; it never drops an existing tagged
   test.

6. **Commit this turn's increment** (never an empty commit) with a phase-naming
   message and the repo trailer:

   ```
   git add -A
   git commit -m "prompts Phase NN: <what landed>

   Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
   ```

   Always return `NEXT`.

## Project conventions

- **Module / toolchain:** Go 1.26, module path `prompts`, service root
  `prompts/`. GOWORK mode: workspace for development (`GOWORK=off` is the
  production build's business, not yours).
- **The suite is green** when all four succeed from `prompts/`:

  ```
  go build ./...
  go vet ./...
  gofmt -l .        # must print nothing
  go test ./...     # zero failures (-race implicit)
  ```

- **Requirement-id tag glob:** `*_test.go`.
- **Test layers** (the suite contract's vocabulary): prompts has **hermetic** and
  **composed** only. Composed = the boot smokes in `cmd/prompts/main_test.go`
  that build the real binary and run `serve` over a loopback port. Hermetic =
  everything else: `net/http/httptest` page/tool/runner tests, temp-file SQLite
  through the real migration runner, real-`git` tests over temp-directory bare
  repositories (including the loopback `git http-backend` door the tree starts
  itself), `share/www` asset tests over the repo-real tree, and
  `etc/nginx.conf` string assertions. There is **no live layer** and no
  tree-local manual layer, so no test in this tree may contact a non-loopback
  address or read a credential. Environmental precondition beyond the Go
  toolchain: the **`git` binary** (D50/D55) — do not introduce any other.
- **Test placement:** co-locate tests with the code they exercise and name them
  for the behavior — package-local `*_test.go` beside the package under test;
  composition-root and whole-tree conformance proofs in `cmd/prompts/`;
  cross-package suite checks in `internal/suite/`. **Never** a per-phase test
  file and never a root-level one.
- **Determinism:** inject clocks and IO seams rather than reaching for wall time
  or the network; a test's output must be determined by its inputs and the
  on-disk tree.
- **Migrations:** schema changes land only as new timestamped migrations minted
  with `bin/create-migration prompts <name>`. Never hand-number one; never edit
  or delete a committed migration.

## Boundaries

- Never read `project/design/*`, `project/plan/*`, or `project/product/*`. The
  brief is your complete input.
- Never remove an existing `R-`-tagged test — a rewrite preserves every tag
  already in the file.
- Never edit `project/plan/STATUS.md` and never delete a phase file. Retiring a
  phase is `verify`'s alone.
- Never delete or edit the brief, including its `## Verify feedback` region — you
  read it, you never write it.
- Never weaken a test to make it pass, and never add a skip.
- Always return `NEXT`. You hand off every turn; you are never the step that
  ends the run.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:

- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal — never yours to report**: telling `ralph` to stop is
  never your job. Even a fully finished phase (green suite, every gap closed)
  is still `NEXT`; only gather ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g.
  `Phase 60: added the AGENTS.md doc-truth test and the skip scan; suite green`
  or `Phase 60: closed the R-O2IA-0JBL gap from verify feedback`.

Keep `message` a single plain sentence — not a JSON object or code block.
