---
harness: codex
model: gpt-5.6-sol
---
# build — implement the phase from the brief

You run in a **fresh, isolated context** from the service root `appkit/` (the
directory `ralph` launched from; all `project/…` and `../bin/…` paths below are
relative to it). You read **only** `project/loops/brief.md` — never the design or
plan docs. You do a bounded, idempotent turn of the phase's remaining work and
commit it. You do **not** decide completeness and you do **not** flip any marker.
Do one iteration, then report.

## Step 0 — workspace identity guard

Before anything else, confirm you are in the `appkit` spec workspace:

```
head -n 1 project/plan/STATUS.md
```

This must print exactly `# appkit — Plan Status`. If it does not (including a
missing file):
- Check `./appkit/project/plan/STATUS.md` with the same command. If **that**
  prints `# appkit — Plan Status`, your cwd is one level above the service
  root — `cd appkit` and continue from step 1 below.
- Otherwise, make no changes and report `NEXT` with a message naming the
  expected title (`# appkit — Plan Status`) and what you actually observed.
  Never report `DONE` — ending the run is never build's job (see *Reporting
  the result*).

## Procedure

1. **Read the whole brief** — both the contract region and the
   `## Verify feedback` region. If `project/loops/brief.md` is missing or empty,
   make no changes and report `NEXT`.

2. **Prioritize verify's feedback.** If the `## Verify feedback` region lists open
   gaps, those are the exact, command-grounded items the independent gate found
   unsatisfied last cycle. **Close those first**, using the precise failing
   command/output each gap records to reproduce and fix it.

3. **Do as much of the brief as cleanly fits this one context — ideally the whole
   phase**, so `verify` can pass it next cycle. Prefer fewer, fuller turns over
   many thin increments (an incomplete phase is simply re-attacked next cycle, so
   there is no benefit to stopping short).

   - See what already exists: for each id in the brief,
     `grep -rn 'R-XXXX-XXXX' --include='*.go' .` from `appkit/`, and run the
     suite to read current failures.
   - Build the named package(s) / edit the named files (see *Files to touch*),
     consuming dependencies **only** through the interface signatures the brief
     copied in — do not open a design file to look them up.
   - Write **id-tagged, genuinely-asserting** tests **co-located with the code they
     exercise and named for the behavior** (see *Conventions*). Never gather tests
     into a per-phase or root-level test file. Before rewriting a test file,
     check the turn's own diff for dropped tags — any removed line matching
     `R-[A-Z0-9]{4}-[A-Z0-9]{4}` outside `project/`
     (`git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'`) must be restored
     first: a rewrite extends a file's tests, it never drops an existing tagged
     test.
   - Run the suite; make it green (see *Conventions*).
   - `gofmt -w` any Go files you touched.

4. **Commit this turn's increment** (never an empty commit) with a phase-naming
   message and the repo trailer, e.g.:

   ```
   git commit -m "appkit: phase NN — <what changed> (build)

   Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
   ```

   Then report `NEXT`.

## Project conventions (the fixed toolchain — inline, do not open design)

**Working directory is the service root `appkit/`.** The appkit Go module is
rooted here, so run its commands directly from the cwd. (Design writes them as
`cd appkit && …` from the repo root; same commands, drop the `cd`.) The repo-root
collaborator scripts are at `../bin/…` and resolve their own root, so they work
regardless of cwd.

- **appkit build / typecheck:** `go build ./...` and `go vet ./...`, plus the
  isolated-module mirror `GOWORK=off go build ./...`.
- **appkit tests (the default gate):** `go test ./...`, in workspace mode via the
  repo-root `go.work`.
- **"The appkit suite is green"** means, from `appkit/`: `go build ./...`,
  `go vet ./...`, `gofmt -l .` (no output), and `go test ./...` all succeed with
  zero failures, and `GOWORK=off go build ./...` succeeds.
- **Cross-module collaborators (not appkit Go, not verified by the Go suite —
  only when the brief names one):**
  - `../bin/registry` — verified by `../bin/registry.test.sh` (passes = exit 0).
  - `../bin/start` — verified by the live `/services` smoke: bring the suite up,
    assert the staged `tmp/opt/<svc>/etc/current/manifest.env` layout, and
    `curl -s http://127.0.0.1:3000/services` lists `crm`. Tear down with
    `../bin/stop` after. ⚠️ Only start/stop the suite this loop started from
    **this** worktree; if a shared port (`:3000`–`:3006`, `:8080`) is held by a
    stack from another worktree, stop and surface it — do not kill it.
- **Test layers (the suite contract, `root project/design/D23.md`).** appkit's
  suite spans **hermetic** (in-process code plus real *local* substrates —
  `t.TempDir()` trees, real SQLite through the real migration runner,
  `net/http/httptest`, loopback listeners, local subprocesses) and **composed**
  (everything hermetic may, plus building and running a real chassis-based
  service binary — the boot smoke in `appkit_test.go`). Both run in the default
  gate. appkit has **no live layer**: never add a `//go:build live` file, never
  reach a non-loopback address, and never read a credential from a test. The
  **manual** layer is the operator's live-box runbook
  `project/appkit-verification.md` — never loop work, never yours to run.
- **Skipping is banned.** `t.Skip`, `t.Skipf`, and `t.SkipNow` may not appear in
  any test file in this tree (there are no live-tagged files here, so the
  contract's one exemption does not apply). A tool a test needs — the `go`
  binary on `PATH` at test time, a populated module cache — is an
  **environmental precondition** declared in `AGENTS.md` and a hard failure when
  absent. A skipped requirement test launders a gap into green and `verify`
  scores it **uncovered**.
- **Test placement (design's rule — enforce it):** unit tests live **beside the
  code they exercise**, named for the behavior — appkit unit tests in the
  exercised package's own `*_test.go` (e.g. `mcp/*_test.go` for the transport and
  standard tools, `server/*_test.go` for routes, `config/*_test.go` for env
  resolution); the shell-collaborator behaviors in their named script/smoke.
  **Never** create a per-phase or root-level test file.
- **Id tagging:** each covered id is named in a comment on the test that asserts
  it — `// R-XXXX-XXXX` in Go, `# R-XXXX-XXXX` in shell — on a test that *genuinely
  asserts* the behavior (never a bare literal, never a test held out of the run by
  a skip/build-tag/env gate, never one that turns a real failure into a skip).
- **Determinism seams (design's testing strategy):** exercise behavior through
  the **real seams, not stand-ins** — MCP transport and standard-tool claims go
  through the real `ServeHTTP` JSON-RPC seam via `net/http/httptest`; route and
  loopback-class claims drive the real handler and the real `server.New` mux
  (recording inner handlers for not-invoked claims); on-disk claims use real
  `t.TempDir()` trees; config claims use injected `getenv` maps; consumer-loop
  claims run over a real SSE feed (`httptest`) and a real `t.TempDir()` SQLite
  database (`modernc.org/sqlite`); telemetry delivery/drop-not-block/correlation
  claims use a live `httptest.NewServer` ingest sink or peer, and a genuinely
  closed TCP port for the refused-connection claim. Result-shape assertions
  compare `structuredContent` against the parsed text block — never against a
  string fixture. When the brief's id line carries a `Substrate:` clause, that
  clause names the substrate the test must actually run against.

## Boundaries

- Never read design / plan / product docs. The brief is your only input.
- Never edit `project/plan/STATUS.md` or flip a marker.
- Never delete or edit `project/loops/brief.md`, including its feedback region —
  you read it but never write it.
- Never remove an existing `R-`-tagged test — a rewrite preserves every tag
  already in the file.
- Never introduce a `t.Skip` variant, a `//go:build live` file, or any other
  gate that holds a test out of `go test ./...`.
- Always report `NEXT`: build hands off every turn; it is never the step that ends
  the run.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal — never yours to report**: telling `ralph` to stop is never
  your job. Even a fully finished phase (green suite, every gap closed) is still
  `NEXT`; only gather ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g.
  `Implemented Phase 30's AGENTS.md declaration and skip-ban tests; appkit suite green.`

Always end the turn on **`NEXT`**. `CONTINUE` is only ever a non-terminal
progress status. Keep `message` a single plain sentence, not a JSON object or
code block.
