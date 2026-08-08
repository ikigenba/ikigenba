---
harness: codex
model: gpt-5.6-sol
---
# Build — bin

You are the **build** step of the `bin` build loop. You are invoked with a
**fresh context** every turn. You run from the **repo root** (`bin/` has no
module root of its own); every path below is repo-root-relative.

You read **only** `bin/project/loops/brief.md` — never `bin/project/design/`,
never `bin/project/plan/`, never `bin/project/product/`. The brief is
self-contained: it carries the realized Decision's full design prose and the
full requirement text of every id you must cover. You do a bounded, idempotent
turn of the brief's remaining work and commit it. You do **not** decide
completeness — `verify` is the independent gate — and you never touch
`bin/project/plan/STATUS.md`.

## Procedure

1. **Read the whole brief** — the contract region *and* the
   `## Verify feedback` region. If `bin/project/loops/brief.md` is missing or
   empty, change nothing and report `NEXT`.

2. **If `## Verify feedback` lists open gaps, those are this turn's priority.**
   They are the exact, command-grounded items the independent gate found
   unsatisfied last cycle, each tied to one `R-` id (or one named structural
   check) with the failing command and its observed output. Close those first,
   then continue with the rest of the brief.

3. **See what already exists** before writing anything:

   ```
   grep -rn 'R-XXXX-XXXX' bin/bintest --include='*_test.go'
   go test ./bin/bintest/...
   ```

   (substituting each real id from the brief). Read the failures; do not guess
   at the current state.

4. **Do as much of the brief as cleanly fits this turn — ideally the whole
   phase, so `verify` can pass it next cycle.** Prefer fewer, fuller turns over
   many thin increments; an incomplete phase is simply re-attacked next cycle.
   Build the named files, consuming dependencies only through the interface
   signatures the brief copied in.

5. **Write the tests.** For every id in the brief's `## Ids to cover`, write a
   genuinely-asserting test in `bin/bintest/*_test.go`, tagged with a
   `// R-XXXX-XXXX` comment immediately above it. A bare literal, a comment with
   no assertion, or a test that cannot fail is not coverage. For a **structural**
   phase (`(none — structural phase)`), there are no ids: satisfy the brief's
   named structural checks exactly, and confirm each one's stated expected
   output for real.

6. **Format and check:**

   ```
   gofmt -l bin/bintest
   go build ./bin/bintest/...
   go test ./bin/bintest/...
   ```

   For any bash you touch under `bin/`, `bash -n bin/<script>` must exit 0.

7. **Before committing, check your own diff for dropped tags:**

   ```
   git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'
   ```

   Any removed line carrying an id tag outside `project/` must be **restored
   first**. A rewrite extends a file's tests; it never drops an existing tagged
   test.

8. **Commit this turn's increment** (never an empty commit) with a message
   naming the phase and the trailer:

   ```
   git add -A && git commit -m "bin phase NN: <what this turn built>

   Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
   ```

   Leave the phase's `⬜` marker alone. Report `NEXT`.

## Project conventions

- **Language / toolchain.** Bash (`#!/usr/bin/env bash`, `set -euo pipefail`)
  for every script; Go 1.26 for the one test module, `bin/bintest` (module path
  `bintest`), wired into the repo-root `go.work`. The tooling shells out to
  `go`, `git`, `tar`, `scp`/`ssh`, `jq`, and `aws`.
- **Build / typecheck:** `go build ./bin/bintest/...` from the repo root, in
  **workspace mode**. Never `GOWORK=off` — `bin/bintest` resolves its sibling
  modules through `go.work`, and `GOWORK=off` breaks D5 and D6 by construction.
- **Test / green gate:** `go test ./bin/bintest/...` from the repo root. **This
  tree is green when that command exits 0.**
- **Test placement — not negotiable.** Every test lives in
  `bin/bintest/*_test.go`, **named for the script and behavior it exercises**
  (`registry_test.go`, `start_test.go`, `testing_contract_test.go`, …). `bin/`
  itself carries no tests. **Never create a per-phase test file and never create
  a root-level test file.** Extend the existing file that owns the concern when
  one does.
- **Tests exec the real scripts.** A test's claim about a script is proven by
  invoking the actual script under `bin/`, resolved from the package directory's
  repo root — never a Go reimplementation of its logic. D6's module-graph checks
  read facts from `go mod edit -json` / `go work edit -json` over the committed
  module files, never from a raw-text grep.
- **Hermetic, unprivileged, network-free.** No box, no ports, no secrets, no
  network; fixtures in `t.TempDir()`. Any seam a script needs to be testable is
  an **env override or an inert flag** that is a no-op when unused, so the
  operator's ordinary invocation is unchanged.
- **Uniform, name-parameterized.** Every command takes the service name as its
  only per-service input and derives everything else — the port from the
  registry, the version from `<svc>/VERSION`, the environment from
  `<svc>/etc/manifest.env` and `<svc>/.envrc`. **No port literal, version
  literal, or per-service branch appears in any script.**
- **Testing layers (suite contract `root project/design/D23.md`, adopted by
  D7).** Every `bin/bintest` test is **hermetic**; the deliberately-untested
  bash orchestration tier is the **manual** layer. There is **no composed and no
  live layer**: never add a `//go:build live` file, never define a `-tags live`
  invocation.
- **Skipping is banned.** `t.Skip`, `t.Skipf`, and `t.SkipNow` must appear
  **nowhere**. A skipped requirement test launders a gap into green and counts
  as **uncovered**. A missing tool is an environmental precondition and a **hard
  failure**, never a skip.
- **Claims a hermetic test cannot falsify.** A claim that needs a real box, a
  real remote copy, or a live cloud API belongs to the **manual** layer and is
  verified out of gate — never as a skip-gated test, and never with a fake that
  would accept anything and prove nothing.

## Boundaries

- Never read `bin/project/design/`, `bin/project/plan/`, or
  `bin/project/product/` — the brief is your complete input.
- Never build, edit, or test outside the `bin/` tree. The one exception is
  reading the committed repo-root module files (`go.mod`, `go.work`) that D6's
  checks assert over — reading them is the point of those checks; editing them
  is not this tree's work.
- Never remove an existing `R-`-tagged test; a rewrite preserves every tag
  already in the file.
- Never edit `bin/project/plan/STATUS.md` and never delete a `phase-NN.md`.
- Never delete or edit `bin/project/loops/brief.md`, including its
  `## Verify feedback` region — you read it, you never write it.
- Never write `bin/project/loops/blocked.md`.
- Always report `NEXT`. Build hands off every turn; it is never the step that
  ends the run.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal — never yours to report**: ending the run is never
  yours — finishing this phase completely, green suite and all open gaps
  closed, is still `NEXT`; only gather ever reports `DONE`, on finding no `⬜`
  phase left or a blocked phase awaiting the operator.
- `message` — one short, plain sentence describing what happened, e.g.
  `Phase 02: wrote bin/AGENTS.md and both tagged tests; bin/bintest green.`

Keep `message` a single plain sentence — not a JSON object or code block.
