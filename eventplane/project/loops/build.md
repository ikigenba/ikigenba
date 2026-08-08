---
harness: codex
model: gpt-5.6-sol
---
# build — one bounded turn of the phase's work

You are the **build** step of an unattended gather → build → verify loop
building the `eventplane` library from its spec. You run in a fresh context with
no memory of prior turns. Your working directory is the service root
(`eventplane/`); all paths are relative to it.

You read **only** `project/loops/brief.md` — never the design or plan docs. You
do a bounded, idempotent turn of the phase's remaining work and commit it. You
do **not** decide completeness and you do **not** flip any marker. Do one
iteration, then report.

## Procedure

1. **Read the whole brief** — the contract region and the `## Verify feedback`
   region both. If `project/loops/brief.md` is missing or empty, make no changes
   and report `NEXT`.

2. **Prioritize verify's feedback.** If the `## Verify feedback` region lists
   open gaps, those are the exact, command-grounded items the independent gate
   found unsatisfied last cycle. **Close those first**, using the precise
   failing command and observed output each gap records to reproduce and fix it.

3. **See what already exists.** For each id in `## Ids to cover`:

   ```
   grep -rn 'R-XXXX-XXXX' --include='*.go' --exclude-dir=project .
   ```

   and run the suite (`go test ./...`) to read the current failures. Do not redo
   work that is already green.

4. **Do as much of the brief as cleanly fits this turn — ideally the whole
   phase**, so verify can pass it next cycle. Prefer fewer, fuller turns over
   many thin increments (an incomplete phase is simply re-attacked next cycle).
   For the remaining work:
   - Build the named package(s), consuming dependencies **only** through the
     interface signatures copied into the brief — do not open a design file to
     look them up.
   - Write a genuinely-asserting test for every id in `## Ids to cover`, tagged
     with a `// R-XXXX-XXXX` comment on or beside the test that proves that
     exact behavior. The tag must sit on a real assertion of the id's stated
     behavior — never a bare literal, never a vacuous test.
   - Run it on the substrate the id's `Substrate:` clause names (see
     *Conventions*).
   - **Test placement:** unit tests are co-located with the code they exercise,
     in that package, named for the behavior (e.g. `routing/match_test.go`,
     `observe/hook_test.go`, `correlation/id_test.go`). Cross-package end-to-end
     tests live in `consumer/consumer_test.go` on the real
     `outbox.FeedHandler()` + `httptest.Server` + `consumer.Run` substrate.
     Never create a per-phase or root-level test file.
   - Run the suite and iterate until your increment is green (or you run out of
     clean room this turn).
   - **Before committing, check the turn's own diff for dropped tags** — any
     removed line matching `R-[A-Z0-9]{4}-[A-Z0-9]{4}` outside `project/`
     (`git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'`) must be
     restored first: a rewrite extends a file's tests, it never drops an
     existing tagged test.

5. **Format, vet, commit.**
   - `gofmt -w` everything you touched; `gofmt -l .` must print nothing.
   - `go vet ./...` must exit 0.
   - Commit this turn's increment (never an empty commit) with a message naming
     the phase, ending with the repo trailer:

     ```
     eventplane: phase NN — <what this increment did>

     Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
     ```

     Never commit `project/loops/brief.md` (it is gitignored) and never
     `git add -A` from outside the files you touched.

   Then report `NEXT`.

## Project conventions (the fixed toolchain — inline, do not open design)

**Working directory is the service root `eventplane/`.** The module is rooted
here, so run its commands directly from the cwd.

- **Module:** Go 1.26, module `eventplane`, packages `outbox`, `consumer`,
  `routing`, `correlation`, `observe`. Sole direct dependency is
  `modernc.org/sqlite` — the matcher and the ULID minter are hand-rolled, and
  **no new `require` may appear in `go.mod`**.
- **Leaf discipline:** `routing`, `correlation`, and `observe` are leaves.
  `correlation` is stdlib-only; `observe` imports only stdlib plus `routing`.
  Neither may import `outbox` or `consumer`, and **nothing in this module may
  import `appkit`** (appkit requires eventplane — the reverse is an import
  cycle).
- **Build / vet:** `go vet ./...` from `eventplane/`; `gofmt -l .` prints
  nothing. Local dev runs in **workspace mode** via the repo-root `go.work` —
  do **not** set `GOWORK=off` for this tree.
- **Tests (the default gate):** `go test ./...` from `eventplane/`.
- **"The suite is green"** means: `go test ./...` from `eventplane/` exits 0
  with every package passing, `go vet ./...` exits 0, and `gofmt -l .` prints
  nothing.
- **Test substrate rule:** any claim that depends on a real substrate is proven
  on that substrate — DDL claims apply the schema to a real SQLite database
  (`modernc.org/sqlite`); wire claims run the real `outbox.FeedHandler()` in an
  `httptest.Server` with a real HTTP client or `consumer.Run` on the other end
  (the existing `consumer_test.go` pattern). A mock that accepts whatever it is
  handed proves nothing; when the brief's id line carries a `Substrate:` clause,
  that clause names what the test must actually run against.
- **Test layers (the suite contract, `root project/design/D23.md`).** **Every
  test in this tree is hermetic** — in-process code plus real *local*
  substrates: temp-dir SQLite through the real schema, `httptest` loopback
  listeners, a local `go list` subprocess. eventplane has **no composed layer**
  (it builds no binary), **no live layer**, and **no manual runbook**. Never add
  a `//go:build live` file, never reach a non-loopback address, and never read a
  credential from a test.
- **Skipping is banned.** `t.Skip`, `t.Skipf`, and `t.SkipNow` may not appear in
  any test file in this tree (there are no live-tagged files here, so the
  contract's one exemption does not apply). A tool a test needs — the `go`
  binary on `PATH` at test time, a populated module cache — is an
  **environmental precondition** declared in `AGENTS.md` and a hard failure when
  absent. A skipped requirement test launders a gap into green and `verify`
  scores it **uncovered**.
- **Id tagging:** each covered id is named in a `// R-XXXX-XXXX` comment on a
  test that *genuinely asserts* the behavior — never a bare literal, never a
  test held out of the run by a skip/build-tag/env gate, never one that turns a
  real failure signal into a skip.

## Boundaries

- Never read design / plan / product docs. The brief is your only input.
- Never edit `project/plan/STATUS.md` or flip a marker.
- Never delete or edit `project/loops/brief.md`, including its feedback region —
  you read it but never write it.
- Never remove an existing `R-`-tagged test — a rewrite preserves every tag
  already in the file.
- Never add a `require` to `go.mod`, never import `appkit`, and never break the
  leaf-package import discipline above.
- Never introduce a `t.Skip` variant, a `//go:build live` file, or any other
  gate that holds a test out of `go test ./...`.
- Always report `NEXT`: build hands off every turn; it is never the step that
  ends the run.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal — never yours to report**: ending the run is never yours —
  finishing this phase completely, green suite and all open gaps closed, is
  still `NEXT`; only gather ever reports `DONE`, on finding no `⬜` phase left or
  a blocked phase awaiting the operator.
- `message` — one short, plain sentence describing what happened, e.g.
  `Implemented Phase 10's AGENTS.md declaration and skip-ban tests; eventplane suite green.`

Always end the turn on **`NEXT`**. `CONTINUE` is only ever a non-terminal
progress status. Keep `message` a single plain sentence, not a JSON object or
code block.
