# ledger — build loop (gather → build → verify)

This directory holds the **installed** unattended build loop for the ledger
service. `ralph` re-invokes three prompts in a **fresh context** each turn,
cycling `gather → build → verify → gather → …` and building the project one
phase at a time until the queue in `project/plan/STATUS.md` is empty (every
pending phase line has been deleted) — or until a phase's done bar proves
unfixable and `gather` finds `project/loops/blocked.md`. This README describes
the loop **as it is on disk**; the workspace map (`project/README.md`) only
points here.

## Running it

```
project/loops/run
```

which is exactly:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the **service root** (`ledger/`), so every path the prompts
reference is service-root-relative (`project/…`, `cmd/ledger/…`). The wrapper
is executable.

## The status contract

Each turn ends with a `{status, message}` the harness reads out of band
(`ralph` injects the schema per backend — codex via `--output-schema`, claude
via `--json-schema`). The prompts describe only the contract, never a
transport.

- **`NEXT`** — *terminal*: this turn's work is done; advance to the next
  prompt (wrapping `verify → gather`). `build` and `verify` **always** end on
  `NEXT`.
- **`DONE`** — *terminal*: the whole job is complete; the loop stops. **Only
  `gather` ever reports `DONE`**, and only when `project/loops/blocked.md`
  exists or its `STATUS.md` grep finds no `⬜` phase left.
- **`CONTINUE`** — *non-terminal*: the status a streaming model tags the
  progress messages it emits **before** its terminal message. `ralph` reads
  only the last message, so `CONTINUE` never advances or ends the loop.

## Per-step reads / writes / commits / queue mutations

| step | reads | writes | commits | mutates STATUS.md |
|---|---|---|---|---|
| **gather** | `blocked.md` (existence), `STATUS.md`, one `phase-NN.md`, `INDEX.md`, realized `DNN.md`, (product for intent) | `project/loops/brief.md` **contract region** (only when no brief exists for the active phase) | no | no |
| **build** | `project/loops/brief.md` only | package source + co-located id-tagged tests | yes (the code increment) | no |
| **verify** | `project/loops/brief.md` + runs the suite + the global ratchet | deletes the phase's `STATUS.md` line + `phase-NN.md`, **or** overwrites the brief's `## Verify feedback` region, **or** writes `project/loops/blocked.md` | yes (the deletion, on pass) | **yes**, on pass only |

The next-phase lookup is
`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1` (STATUS.md phase
lines are Markdown bullets: `- Phase NN ⬜ …`). "Green" is
`cd ledger && go build ./...`, `go vet ./...`, `gofmt -l .` (no output), and
`go test ./...`, all with zero failures, with no `R-XXXX-XXXX`-tagged test
reporting `SKIP`. Tests are **co-located** with the code they exercise
(post-D10, landing/route/nginx assertions live in `cmd/ledger`, `package
main`; domain/MCP tests in `internal/ledger`, `internal/mcp`, `internal/db`,
`internal/ids`), never a root-level or `phaseNN_test.go` file.

## The brief lifecycle

`project/loops/brief.md` is the ephemeral seam between the three prompts —
**never committed** (it is `.gitignore`d via the repo-root
`*/project/loops/brief.md`) and **single-phase** (it only ever describes one
phase).

- **gather** authors the brief's contract region **once**, when a phase first
  becomes the active `⬜` phase, and **no-ops** while that same phase stays in
  flight (it leaves the brief — contract *and* feedback — untouched, opening
  no big doc).
- **build** consumes the whole brief (contract + feedback) and, if the
  feedback lists open gaps, closes those first. It reads the brief but never
  writes it.
- **verify** re-derives truth independently, including a global coverage
  ratchet over every design id (not just this phase's). **Pass** → delete the
  phase's line from `STATUS.md` and its `phase-NN.md`, commit that deletion,
  and delete the brief. **Gap** → leave `⬜` and the line untouched, change no
  source, and **overwrite** the feedback region with only the currently-open
  gaps (each tied to its `R-id` and the exact failing command/output). The
  brief **persists across cycles** until the phase passes or a stall reset
  discards it.

## The stall and blocked ladder

`verify` tracks a per-phase stall streak in the brief's feedback region:
*progress* means the open-gap id set strictly shrinks from the prior attempt;
a new build commit alone is never progress. Three consecutive no-progress
attempts trigger a **trajectory reset** — the brief is discarded (logged to
`~/.ralph/verify.log`), `⬜` stays, and the next `gather` rebuilds the
contract fresh from spec. If the **same phase** stalls a **second** time
(a prior `STALLED` line for it already in `~/.ralph/verify.log`), `verify`
escalates instead of resetting again: it writes `project/loops/blocked.md`
naming the phase, the attempts, the unsatisfied ids, and the exact failing
command/output, and logs a `BLOCKED` line. The next `gather` sees
`blocked.md` and reports `DONE`, stopping the run.

**Operator recovery from a blocked run:** read the command and output
recorded in `project/loops/blocked.md` — it names the specific done-bar
condition that would not go green after a rebuilt contract. Fix the phase's
bar in `project/` (design/plan, via the normal spec-authoring flow), delete
`project/loops/blocked.md`, and restart `project/loops/run`.

## Why it converges (human-free)

`verify` can neither halt nor advance a phase on a gap, so an incomplete
phase just stays `⬜` and the loop re-attacks it — now with `verify`'s
grounded feedback in front of `build`, and without `gather` re-reading the
big docs (it no-ops on the in-flight brief). The persisted feedback also
gives `verify` cross-cycle memory: it distinguishes *slow convergence* (the
open-gap id set shrinking) from a *true stall* (no gap closed for ≥3
consecutive attempts), and a second stall on the same phase from a merely
slow one — escalating the latter to the operator instead of spinning
forever. The loop's exits are `gather → DONE` on zero `⬜` markers (every
phase verified green) or on a blocked phase, plus the ralph budget rails.

## The `project/loops/brief.md` schema

A single-phase file with two single-writer regions:

- **Contract region** (gather-owned, written once per phase):
  - `# Brief — Phase NN: <objective>` header, `phase:`, `realizes:`,
    `decision_files:`.
  - `## Design prose` — the full Decision statement, shape/signatures, and
    Rejected alternatives of each realized Decision, copied verbatim from its
    `DNN.md` **with that Decision's Verification list omitted**.
  - `## Ids to cover` — one id per line, `R-XXXX-XXXX — <full requirement
    text copied verbatim>`, listing **only** the phase's own ids (or the
    single line `(none — structural phase; …)`). Grep-able as
    `grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md`.
  - `## Files to touch`, `## Dependency interfaces / required shapes`
    (copied signatures/config snippets), and `## Done bar`.
- **Verify-feedback region** (verify-owned): a `## Verify feedback — attempt
  N` heading carrying the attempt counter, the build commit `verify`
  observed (diagnostic only), the stall-streak counter, and a checklist of
  **only** the open gaps — each line an `R-id` + the exact failing
  command/output. `gather` seeds it empty; `verify` overwrites it each gap
  cycle; `build` reads but never writes it.

`project/loops/brief.md` and `project/loops/blocked.md` are both
`.gitignore`d — neither is a spec artifact, both are loop-run state.
