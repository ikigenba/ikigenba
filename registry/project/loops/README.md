# registry — build loop (gather → build → verify)

This directory holds the **installed** three-prompt build loop for `registry`
and the executable wrapper that runs it. It is generated from the finished
`project/` spec; it describes the loop **as on disk** and is kept beside the
prompts so it can never drift from them. The workspace map (`project/README.md`)
only points here — the loop mechanics live only in this file.

## Running it

```
project/loops/run
```

`run` is the operator wrapper; its entire body is:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the **module root** (`registry/`, its working directory) and
cycles the three prompts in fresh, isolated contexts — `gather → build → verify
→ gather → …` — until `gather` finds no `⬜` phase left, or finds
`project/loops/blocked.md`. Every workspace path the prompts reference is
module-root-relative (`project/…`).

## The status contract

Each turn ends with a `{status, message}` the harness supplies out of band
(`ralph` injects the schema per backend and reads back only the turn's **final**
message). Three status values:

- **`NEXT`** — *terminal*: this turn is done, advance to the next prompt
  (wrapping `verify → gather`). `build` and `verify` **always** end on `NEXT` —
  neither ever reports `DONE`, even on a fully green phase.
- **`DONE`** — *terminal*: the whole job is complete, stop the loop. **Only
  `gather` ever reports `DONE`**, and only when `project/loops/blocked.md`
  exists or its `STATUS.md` grep finds no `⬜` phase (the queue is empty).
- **`CONTINUE`** — *non-terminal*: the status a streaming model tags the progress
  messages it emits **before** its final message. It never advances the loop;
  `ralph` reads only the terminal message.

## Per-step reads / writes / commits / STATUS.md mutations

| step | reads | writes | commits | mutates STATUS.md |
|---|---|---|---|---|
| **gather** | `project/loops/blocked.md`, `project/plan/STATUS.md`, one `phase-NN.md`, `design/INDEX.md`, the realized `DNN.md` | the brief's **contract region** (only when no brief for the phase exists) | no | no |
| **build** | **only** `project/loops/brief.md` (contract + feedback) | production code + id-tagged tests | yes (the code increment) | no |
| **verify** | `project/loops/brief.md` + runs the suite + the ratchet | pass → deletes the brief; gap → the brief's **feedback region**, or `project/loops/blocked.md` on a second stall | yes (only the phase's line + body-file deletion, on pass) | yes (on pass, deletes exactly one phase's line) |

- **gather** is the only step that reads the big docs. It first checks for
  `project/loops/blocked.md` (reports `DONE` on sight of it), then greps
  `STATUS.md` for the first `⬜` phase
  (`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`); if none, the
  queue is empty and it reports `DONE`.
- **build** never opens design/plan/product — the brief is its whole world. It
  prioritises verify's open-gap feedback, does as much of the phase as cleanly
  fits one context, commits, and leaves `STATUS.md` untouched.
- **verify** is the independent gate — the only step that mutates `STATUS.md`,
  deletes the brief, or writes `blocked.md`. It re-derives truth from scratch
  and never trusts build.

## The brief lifecycle

`project/loops/brief.md` is the ephemeral, single-phase seam between the prompts.
It is **git-ignored** (both the repo-root `.gitignore` pattern
`*/project/loops/brief.md` and `registry/.gitignore` match it) and **never
committed**.

- **gather** authors the brief's contract region **once**, when a phase first
  becomes the active `⬜` phase, with an empty feedback region. While that phase
  stays `⬜`, gather **no-ops on the in-flight brief** — it leaves both regions
  untouched and does not re-read the big docs.
- **build** consumes the whole brief (contract + feedback) and writes neither
  region.
- **verify** either **passes** the phase (delete its `STATUS.md` line and its
  `phase-NN.md` body file, commit the deletion, delete the brief — there is no
  done marker; done is gone) or records a **gap** (overwrite the feedback region
  with the currently open gaps, leave the brief in place). The brief therefore
  persists across cycles until the phase passes or a stall reset discards it.

## The stall and blocked ladder

`verify` measures progress across cycles using its own persisted feedback: the
prior attempt's open-gap id set versus the current one. **Progress** is the
current set being a strict subset of the prior one (some gap closed); a new
build commit with the same gaps still open is **not** progress.

- **Three consecutive no-progress attempts** on the same phase → a **stall
  reset**: `verify` logs `Phase NN STALLED …` to `~/.ralph/verify.log`, deletes
  the brief, and leaves the phase `⬜`. The next `gather` rebuilds the contract
  fresh from spec — this discards a trajectory that may just be stuck, without
  halting the loop or advancing the phase.
- **A second stall on the same phase** (found in `~/.ralph/verify.log`) means a
  rebuilt contract already failed to help — the phase's **done bar** is the
  likely fault, not the trajectory. `verify` writes `project/loops/blocked.md`
  naming the phase, the attempts, the unsatisfied ids, and the exact
  command/output that will not go green, logs `Phase NN BLOCKED …`, deletes the
  brief, and leaves the phase `⬜`. The next `gather` sees `blocked.md` and
  reports `DONE`, stopping the run.
- **Operator recovery from `blocked.md`:** read the recorded command and
  output, fix the phase's done bar (or the underlying Decision/plan) in
  `project/`, delete `project/loops/blocked.md`, and restart the loop
  (`project/loops/run`).

## The `project/loops/brief.md` schema

Region-owned by a single writer each, so the two writers never clobber:

**Contract region** (gather-owned; written once per phase):

```
# Brief — Phase NN: <one-line objective>

## Contract
- Phase: NN
- Realizes: D<N> (<short label>)[, D<M> ...]
- Decision files: project/design/DNN.md[, project/design/DMM.md]
- Design prose (verbatim, Verification lists omitted):
  <the Decision statement + shape/signatures + Rejected, copied from each DNN.md>
- Ids to cover:
R-XXXX-XXXX — <full requirement text copied verbatim from the Decision>
  (or the literal line: (none — structural phase))
- Files to touch:
  - registry/<file>.go
  - registry/registry_test.go
- Done bar:
  - `GOWORK=off go build ./...` exits 0
  - `GOWORK=off go test ./...` exits 0 (no failures, no SKIP)
  - every id above is covered by a genuinely-asserting `// R-XXXX-XXXX`-tagged
    test in package-local `registry/*_test.go`, named for the behavior, that
    actually runs under `GOWORK=off go test ./...` (no skip)
  - (structural phase) the phase's Done-when smoke commands pass

## Verify feedback
(none yet)
```

`grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md` extracts exactly
the phase's covered-id set from the contract region.

## Why it converges (human-free)

`verify` can neither halt the loop nor advance a phase on a gap — an incomplete
phase just stays `⬜` and is re-attacked next cycle, now with verify's
command-grounded feedback in front of `build` and without `gather` re-reading the
big docs. A stuck trajectory gets one reset (fresh contract, same phase); a
phase whose bar is itself unsatisfiable gets diagnosed once more and then parked
in `blocked.md` for the operator, rather than spinning forever. The exits are
`gather → DONE` on an empty queue or on `blocked.md`, plus the ralph budget
rails.

## Toolchain baked into the prompts (from design's Conventions)

- **Build / typecheck:** `GOWORK=off go build ./...`
- **Test:** `GOWORK=off go test ./...`
- **"The suite is green":** `GOWORK=off go build ./...` and
  `GOWORK=off go test ./...` both exit 0 with no failures and no SKIP.
- **Test placement:** package-local `registry/*_test.go`, named for the
  behavior — never a root-level or `phaseNN_test.go` file.
- **Global coverage ratchet** (verify only): `comm -23` between the current
  design id set (`project/design/D*.md`) and the union of tagged test ids
  (`--include='*_test.go' --exclude-dir=project`) and pending-phase ids
  (`project/plan/phase-*.md`) must be empty — catches a rewrite silently
  dropping a previously-covered id.
