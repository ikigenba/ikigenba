# crm — build loop (gather → build → verify)

This directory holds the **installed** three-prompt build loop for crm and the
executable wrapper that runs it. It is generated from the finished `project/`
spec; it describes the loop **as on disk** and is kept beside the prompts so it
can never drift from them. The workspace map (`project/README.md`) only points
here — the loop mechanics live only in this file.

## Running it

```
project/loops/run
```

`run` is the operator wrapper; its entire body is:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the **service root** (`crm/`, its working directory) and
cycles the three prompts in fresh, isolated contexts — `gather → build →
verify → gather → …` — until `gather` finds no `⬜` phase left, or finds a
`project/loops/blocked.md` left by a prior `verify`. Every workspace path the
prompts reference is service-root-relative (`project/…`).

## The status contract

Each turn ends with a `{status, message}` the harness supplies out of band
(`ralph` injects the schema per backend and reads back only the turn's
**final** message). Three status values:

- **`NEXT`** — *terminal*: this turn is done, advance to the next prompt
  (wrapping `verify → gather`). `build` and `verify` **always** end on `NEXT`.
- **`DONE`** — *terminal*: the whole job is complete, stop the loop. **Only
  `gather` ever reports `DONE`** — either its `STATUS.md` grep finds no `⬜`
  phase (the queue is empty), or it finds `project/loops/blocked.md` (a phase
  awaiting the operator).
- **`CONTINUE`** — *non-terminal*: the status a streaming model tags the
  progress messages it emits **before** its final message. It never advances
  the loop; `ralph` reads only the terminal message.

## Per-step reads / writes / commits / STATUS.md mutations

| step | reads | writes | commits | mutates STATUS.md |
|---|---|---|---|---|
| **gather** | `project/loops/blocked.md` (existence), `project/plan/STATUS.md`, one `phase-NN.md`, `design/INDEX.md`, the realized `DNN.md`, (opt.) `product/README.md` | the brief's **contract region** (only when no brief for the phase exists) | no | no |
| **build** | **only** `project/loops/brief.md` (contract + feedback) | production code + id-tagged tests | yes (the code increment) | no |
| **verify** | `project/loops/brief.md` + runs the suite | pass → deletes the brief; gap → the brief's **feedback region**; second stall → `project/loops/blocked.md` | yes (only the phase's line + body-file deletion, on pass) | yes (on pass, deletes exactly one phase's line) |

- **gather** is the only step that reads the big docs. It first checks for
  `project/loops/blocked.md`; otherwise it greps `STATUS.md` for the first
  `⬜` phase (`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`); if
  neither yields work, it reports `DONE`.
- **build** never opens design/plan/product — the brief is its whole world.
  It prioritises verify's open-gap feedback, does as much of the phase as
  cleanly fits one context, commits, and leaves `STATUS.md` untouched.
- **verify** is the independent gate — the only step that mutates
  `STATUS.md`, deletes the brief, or writes `blocked.md`. It re-derives truth
  from scratch and never trusts build.

## The brief lifecycle

`project/loops/brief.md` is the ephemeral, single-phase seam between the
prompts. It is **git-ignored** (the repo-root `.gitignore` matches
`*/project/loops/brief.md`) and **never committed**.

- **gather** authors the brief's contract region **once**, when a phase first
  becomes the active `⬜` phase, with an empty feedback region. While that
  phase stays `⬜`, gather **no-ops on the in-flight brief** — it leaves both
  regions untouched and does not re-read the big docs.
- **build** consumes the whole brief (contract + feedback) and writes neither
  region.
- **verify** either **passes** the phase (delete its `STATUS.md` line and its
  `phase-NN.md` body file, commit the deletion, delete the brief — there is
  no done marker; done is gone) or records a **gap** (overwrite the feedback
  region with the currently open gaps, leave the brief in place). The brief
  therefore persists across cycles until the phase passes or a stall reset
  discards it.

## The stall and blocked ladder

`verify` measures progress against its own prior feedback: the current
open-gap id set strictly shrinking from the prior attempt is progress; a new
build commit alone is not.

- **Three consecutive no-progress attempts** on the same phase → a
  **trajectory reset**: `verify` logs the stall to `~/.ralph/verify.log`,
  deletes the brief, and leaves the phase `⬜`. The next `gather` rebuilds the
  contract fresh from spec — this never halts the loop or advances the phase.
- **A second stall on the same phase** (found via a prior `STALLED` entry for
  it in `~/.ralph/verify.log`) means a rebuilt contract already failed to
  help, so the *bar* is the fault, not the trajectory: `verify` writes
  `project/loops/blocked.md` naming the phase, the total attempts, the
  still-open ids, and the exact command/output that will not go green, then
  deletes the brief and leaves the phase `⬜`. The next `gather` sees the file
  and reports `DONE`, stopping the run.
- **Operator recovery:** read the diagnosis in `project/loops/blocked.md`, fix
  the phase's done bar in `project/` (its design/plan is read-only to the
  loop), delete `project/loops/blocked.md`, and restart with
  `project/loops/run`.

## Why it converges (human-free)

`verify` can neither halt the loop nor advance a phase on a gap — an
incomplete phase just stays `⬜` and is re-attacked next cycle, now with
verify's command-grounded feedback in front of `build` and without `gather`
re-reading the big docs. The persisted feedback also gives verify cross-cycle
memory: it can tell *slow convergence* (the open-gap id set shrinking) from a
*true stall* (no gap closed for 3 consecutive attempts), and a *stuck
trajectory* from a *defective bar* (a second stall on the same phase). The
only exits are `gather → DONE` — either zero `⬜` lines in `STATUS.md` (every
phase verified green and deleted) or `project/loops/blocked.md` present
(awaiting the operator) — plus the ralph budget rails.

## The `project/loops/brief.md` schema

Region-owned by a single writer each, so the two writers never clobber:

**Contract region** (gather-owned; written once per phase):

```
# Brief — Phase NN: <one-line objective>

phase: NN
realizes: D<n>[, D<m>]
decision_files:
  - project/design/D0n.md

## Decision prose (copied verbatim from the DNN.md — Verification lists omitted)
### D<n> — <title>
<Decision statement + shape/signatures + Rejected alternatives, verbatim, minus
that Decision's Verification list>

## Ids to cover
R-XXXX-XXXX — <full requirement text, verbatim from the Decision's Verification list>
# ...one id per line, each `R-XXXX-XXXX — <text>`, OR: (none — structural phase)

## Files to touch
- crm/<path>

## Dependency interfaces / required shapes (copied from design)
<public signatures / required config snippets, verbatim>

## Done bar
- every id covered by a genuinely-asserting `// R-XXXX-XXXX`-tagged test
  (co-located, never phase-named), the suite green, plus any named check
```

**Feedback region** (verify-owned; gather writes it empty, build reads but
never writes it, verify overwrites it):

```
## Verify feedback — attempt N
<build commit observed, stall-streak counter, and a checklist of ONLY the
currently-open gaps — each an R-id + the exact failing command + observed output>
```

`grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md` extracts
exactly the phase's covered-id set from the contract region.

## Toolchain baked into the prompts (from design's Conventions)

- **Build / typecheck:** `cd crm && go build ./...` and
  `cd crm && go vet ./...`
- **Test:** `cd crm && go test ./...`
- **"The suite is green":** `go build ./...`, `go vet ./...`, `gofmt -l .`
  (prints nothing), and `go test ./...` all succeed with zero failures
  (`cd crm` first).
- **Test placement:** co-located `*_test.go` in the package it exercises,
  named for the behavior — never a root-level or `phaseNN_test.go` file. The
  composition-root surfaces (landing route, `share/www` assets,
  `crm/etc/nginx.conf` content-assertion) are tested in
  `cmd/crm/main_test.go`; the MCP surface in `crm/internal/mcp/tools_test.go`.
