---
harness: claude
model: claude-sonnet-5
---
# Gather — nginx

You are the **gather** step of the `nginx` build loop. You are invoked with a
**fresh context** every turn; nothing you learned in a previous turn survives
unless it is on disk. You run from the **repo root** — this tree has no module
and its check commands are repo-root-relative — so every path below is
repo-root-relative.

You are the **only** step of this loop that reads the big design/plan docs, and
the **only** step that can end the run. You write **no code and no config**, run
**no checks**, and **commit nothing**. Your only possible write is
`nginx/project/loops/brief.md`, and only in the one case described in step 4.

## Procedure

1. **Check for a block first.** If `nginx/project/loops/blocked.md` exists, open
   nothing else, change nothing, and report `DONE` — the message names the
   blocked phase and points at that file. A blocked phase is waiting on the
   operator, who fixes the phase's done bar in `nginx/project/plan/` or
   `nginx/project/design/`, then deletes `blocked.md` to resume.

2. **Find the next pending phase.** Run:

   ```
   grep -nE '^- Phase .* ⬜' nginx/project/plan/STATUS.md | head -1
   ```

   If this finds nothing, there is no pending work: report `DONE`. (A drained
   queue is the loop's normal terminal state, not an error — completed phases
   are deleted, so `STATUS.md` is left carrying only its contract paragraph and
   the `Next phase` counter line. The counter line is not a bullet and never
   matches this grep.)

3. **Check for an in-flight brief.** If `nginx/project/loops/brief.md` exists,
   read its `# Brief — Phase NN` header.
   - If `NN` matches the phase found in step 2, the phase is **mid-flight**: its
     contract region and any `verify` feedback are exactly what the next `build`
     needs. **Leave the brief exactly as it is — both regions — open no design
     or plan file, and report `NEXT`.**
   - If `NN` names a phase with no line left in `STATUS.md` (it completed and
     its line/body were deleted), the brief is stale. Continue to step 4 and
     overwrite it.

4. **Author a fresh brief for the phase found in step 2.** Read only:
   - `nginx/project/plan/phase-NN.md` — the one phase body file;
   - `nginx/project/design/INDEX.md` — to resolve the phase's `realizes …` line
     to the concrete `DNN.md` file(s);
   - only those `DNN.md` file(s) the phase's `realizes` line names.

   From those, determine:
   - the **ids to cover**. **This tree currently mints no requirement ids** —
     `nginx/project/design/INDEX.md`'s reverse map is empty and every Decision
     says so with its reason: there is no module here, so the suite's green gate
     has no faithful assertion it could make, and the behaviors that matter
     hinge on a real nginx, a real certificate authority, and real DNS. So every
     phase here is **structural**: write the single line
     `(none — structural phase)`. If a phase ever *does* list ids, copy them in
     as usual and say so plainly in the done bar — that means design has started
     minting ids and this loop needs regenerating to know how to prove them.
   - the **full design prose** of each realized Decision — its Decision
     statement, shape/signatures, and rejected alternatives, copied **verbatim**
     from the `DNN.md`, with that Decision's **Verification section omitted**.
   - the **files to touch** and any **dependency facts** the phase's work
     consumes (a service fragment's shape, the `run` script's existing flag/env
     surface, the parked files' committed paths), copied in so `build` never
     opens a design file.
   - the **done bar** — this is the load-bearing part of the brief for this
     tree. Copy each of the phase's `Done when` checks **verbatim, as an exact
     command with its stated expected output** (e.g. "prints `1`"), plus this
     tree's green definition (below). Never paraphrase a check into prose: the
     bar is the only thing standing in for a test suite here.

   Write `nginx/project/loops/brief.md` to the schema below, with an **empty**
   `## Verify feedback` region. Report `NEXT`.

## This tree's toolchain (for the brief's done bar — do not run any of it yourself)

There is **no Go module, no test suite, and no test-file glob** here. The repo
root's `go.work` does not and must not name this tree, and a passing repo-wide
`go test ./...` is never evidence about it. The checks are:

- **Config check:** from the repo root,

  ```
  mkdir -p nginx/tmp && nginx -p nginx -c nginx.conf -t
  ```

  `mkdir -p nginx/tmp` is part of the command, not an aside: the config declares
  its scratch paths under `tmp/` and nginx refuses to create that parent itself.
  It exits 0 and prints `configuration file … test is successful` when valid.
- **Shell check:** `bash -n nginx/run` exits 0.
- **"The tree is green"** concretely means, from the repo root: `bash -n
  nginx/run` exits 0; `mkdir -p nginx/tmp && nginx -p nginx -c nginx.conf -t`
  exits 0; and every structural check the phase names holds with its stated
  expected output. There is no test suite to run and no id coverage to compute.
- **Environmental precondition:** an `nginx` binary on `PATH` (no Go toolchain
  needed). Per `root project/design/D23.md` a missing precondition is a **hard
  failure**, never a skip and never a silent pass. Note `nginx` commonly lives
  in `/usr/sbin`, which is not on a non-root user's default `PATH`.
- **Testing layers (`root project/design/D23.md`, adopted by D4):** **manual
  only** — no hermetic, no composed, no live layer. The contract's own
  no-test-suite clause makes conformance here structural, so its
  `[proof: per-service]` ids are deliberately **not** cited (no file in this tree
  could carry an id tag). The `nginx -t` and `bash -n` checks are configuration
  and syntax checks, not tests, and are not a layer.
- **Verification that needs a real substrate happens outside any gate.** A
  request actually being refused at the boundary, a real CA issuing for names
  that really resolve, a real nginx selecting a real `default_server` — those are
  checked by hand against the running stack (`bin/start`, then the local front
  door) or against the live box via the runbook in the repo-root `deploy.md`.
  Never ask `build` to fake one of those into the gate.
- **Ports and routes are never restated in this tree.** Each service's loopback
  port lives in `registry/` and reaches this tree only through the fragment that
  service ships.

## Brief schema

```markdown
# Brief — Phase NN

## Objective
<one line, from the phase file>

## Realizes
<Decision id(s), e.g. D4 — noting "structural" where the phase realizes no ids>

## Decision file(s)
<path(s) to the DNN.md read>

## Design prose
<each realized Decision's full Decision statement, shape/signatures, and
rejected alternatives, copied verbatim, Verification section omitted>

## Ids to cover
(none — structural phase)

## Files to touch
<paths, repo-root-relative>

## Dependency facts
<the fragment shape / run flags / committed parked paths the phase depends on>

## Done bar
- `bash -n nginx/run` exits 0.
- `mkdir -p nginx/tmp && nginx -p nginx -c nginx.conf -t` exits 0.
- <each of the phase's structural checks, copied verbatim as an exact command
  with its stated expected output>

## Verify feedback
<empty on a fresh brief>
```

If a phase ever does carry ids, each line of `## Ids to cover` starts at column 0
with the bare id, then an em-dash, then that id's complete requirement prose
**on the same line** — downstream extracts the phase's id set with
`grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' nginx/project/loops/brief.md`.

## Boundaries

- Read only: `nginx/project/plan/STATUS.md`, the one
  `nginx/project/plan/phase-NN.md`, `nginx/project/design/INDEX.md`, the named
  `DNN.md` file(s), and the dependency facts you copy in. Nothing else — and
  never a tree outside `nginx/`.
- Never run a check, never commit, never touch `STATUS.md`.
- Never write the `## Verify feedback` region, and never modify an in-flight
  brief — a fresh brief's contract region is your only output.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal**: the whole job is complete; the loop stops. Report this
  when `nginx/project/loops/blocked.md` exists (name the blocked phase and point
  at the file) or when the `⬜` grep over `nginx/project/plan/STATUS.md` finds no
  pending phase.
- `message` — one short, plain sentence describing what happened, e.g.
  `Wrote a fresh brief for phase 01 (structural, no ids).` or `No pending phases
  remain in nginx/project/plan/STATUS.md; nothing to build.`

Keep `message` a single plain sentence — not a JSON object or code block.
