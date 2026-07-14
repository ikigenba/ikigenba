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
     into a per-phase or root-level test file.
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
- **appkit tests:** `go test ./...`.
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
- **Test placement (design's rule — enforce it):** unit tests live **beside the
  code they exercise**, named for the behavior — appkit unit tests in the
  exercised package's own `*_test.go` (e.g. `mcp/*_test.go` for the transport and
  standard tools, `server/*_test.go` for routes, `config/*_test.go` for env
  resolution); the shell-collaborator behaviors in their named script/smoke.
  **Never** create a per-phase or root-level test file.
- **Id tagging:** each covered id is named in a comment on the test that asserts
  it — `// R-XXXX-XXXX` in Go, `# R-XXXX-XXXX` in shell — on a test that *genuinely
  asserts* the behavior (never a bare literal, never a test held out of the run by
  a skip/build-tag/env gate nothing satisfies, never one that turns a real failure
  into a skip).
- **Determinism seams (design's testing strategy):** exercise behavior through
  the **real seams, not stand-ins** — MCP transport and standard-tool claims go
  through the real `ServeHTTP` JSON-RPC seam via `net/http/httptest`; route and
  loopback-class claims drive the real handler and the real `server.New` mux
  (recording inner handlers for not-invoked claims); on-disk claims use real
  `t.TempDir()` trees; config claims use injected `getenv` maps. Result-shape
  assertions compare `structuredContent` against the parsed text block — never
  against a string fixture.

## Boundaries

- Never read design / plan / product docs. The brief is your only input.
- Never edit `project/plan/STATUS.md` or flip a marker.
- Never delete or edit `project/loops/brief.md`, including its feedback region —
  you read it but never write it.
- Always report `NEXT`: build hands off every turn; it is never the step that ends
  the run.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal — never yours to report**: ending the run is never yours —
  finishing this phase completely, green suite and all open gaps closed, is still
  `NEXT`; only gather, finding no `⬜` phase left, ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g.
  `Implemented Phase 12 StructuredResult + ErrorCode with five tagged tests; appkit suite green.`

Always end the turn on **`NEXT`**. `CONTINUE` is only ever a non-terminal
progress status. Keep `message` a single plain sentence, not a JSON object or
code block.
