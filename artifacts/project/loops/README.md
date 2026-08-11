# artifacts build loop — gather → build → verify

The unattended build loop installed in this tree. It builds `artifacts` one
pending phase at a time from the `project/` spec, with no human in the turn.
This document describes the loop **as installed** and lives beside the
prompts it describes, so it can never describe a different loop than the one
on disk. The spec shapes themselves (product/design/plan) are owned
elsewhere; `project/README.md` points here for loop mechanics and nothing is
restated in either direction.

## Invocation

```
./project/loops/run
```

which is exactly:

```
exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the service root and re-invokes each prompt in a **fresh
context** every turn, cycling `gather → build → verify → gather → …`. Every
prompt opens with a **workspace identity guard**: `head -n 1
project/plan/STATUS.md` must print `# artifacts — Plan Status`, so a drifted
cwd (this repo nests several valid `project/` trees) can never produce a
false `DONE` or misdirected work.

## The status contract

Each turn's **final** message carries a status; `ralph` reads only that last
message:

- `NEXT` — terminal: advance to the next prompt (wrapping `verify → gather`).
- `DONE` — terminal: stop the run. **Only `gather` ever reports it**, on
  either of exactly two conditions: zero `⬜` lines left in
  `project/plan/STATUS.md` (every phase verified green and deleted), or
  `project/loops/blocked.md` exists (a phase awaits the operator). `build`
  and `verify` always end on `NEXT`.
- `CONTINUE` — non-terminal: the status a streaming model tags its mid-turn
  progress messages with. It never terminates a turn and never drives the
  loop.

## Per-step responsibilities

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| **gather** | `STATUS.md`, one `phase-NN.md`, `INDEX.md`, realized `DNN.md`(s) | `brief.md` contract region (fresh phase only) | nothing | nothing |
| **build** | `brief.md` only (both regions) | source + co-located id-tagged tests | one increment commit per turn | nothing |
| **verify** | `brief.md`, the codebase, the gate's own output | `brief.md` feedback region; on escalation `blocked.md` + `~/.ralph/verify.log` | the phase-retirement commit (pass only) | on pass: the phase's `STATUS.md` line, its `phase-NN.md`, and `brief.md`; on stall reset: `brief.md` |

## The brief lifecycle

`project/loops/brief.md` is the seam between the steps — the complete and
only input `build` and `verify` consume, so neither ever opens design or
plan. It is **never committed** (ignored via the repo-root `.gitignore`),
**single-phase**, and **phase-scoped, not per-cycle**:

- `gather` authors the contract once when a phase first becomes the active
  `⬜` phase, then **no-ops** while that phase stays in flight (brief left
  byte-for-byte untouched, no big doc opened).
- `build` consumes it, closing `verify`'s open gaps first, and never writes
  it.
- `verify` on a **pass** retires the phase (deletes its `STATUS.md` line and
  `phase-NN.md`, commits) and deletes the brief; on a **gap** it overwrites
  the feedback region with the currently-open gaps and leaves the brief in
  place for the next `build`.

### Brief schema

```markdown
# Brief — Phase NN
<one-line objective>

## Contract                       ← gather-owned; verify never writes here

### Realizes
- D<N> — project/design/DNN.md

### Design — D<N>: <title>
<the Decision's full prose verbatim (Decision + Rejected), Verification
list omitted>

### Ids to cover
R-XXXX-XXXX — <full requirement text verbatim>   (one per line, id at
line-start; or "(none — structural phase)")

### Files to touch
### Dependency interfaces
### Done bar

## Verify feedback — attempt N    ← verify-owned; gather writes it empty

- Build commit observed: <sha> (diagnostic only)
- Stall streak: <k>

Open gaps:
- [ ] R-XXXX-XXXX — <exact failing command> → <observed output>
```

Each region has exactly one writer, so the two writers never clobber each
other. `grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md` yields
exactly the active phase's id set.

## The stall and blocked ladder

`verify` measures progress each gap cycle: progress means the open-gap id
set strictly shrank. A new build commit is **never** progress — churn is not
convergence.

1. **Three consecutive attempts closing no gap** → *trajectory reset*:
   verify logs `Phase NN STALLED` to `~/.ralph/verify.log`, deletes the
   brief, leaves `⬜`; the next `gather` rebuilds the contract fresh from
   spec.
2. **A second stall on the same phase** → *blocked escalation*: a rebuilt
   contract did not help, so the phase's done bar itself is the prime
   suspect. verify writes `project/loops/blocked.md` (phase, attempt count,
   unsatisfied ids, the exact command + observed output that will not go
   green), logs `Phase NN BLOCKED`, deletes the brief, leaves `⬜`; the next
   `gather` reports `DONE` on sight of that file.

**Operator playbook for a `blocked.md`:** read the recorded command and
output, fix the phase's done bar (or the underlying Decision) in `project/`
via the normal spec-authoring moves, delete `project/loops/blocked.md`, and
restart `./project/loops/run`.

Both rungs stay inside the core invariant — verify never halts the loop and
never advances a phase on a gap.

## Why the loop converges

An incomplete phase just stays `⬜` and is re-attacked next cycle — now with
verify's command-grounded feedback in front of build, and without gather
re-reading the big docs (it no-ops on the in-flight brief). The persisted
feedback gives verify cross-cycle memory to tell slow convergence (the gap
set shrinking) from a true stall, and the stall/blocked ladder guarantees a
defective bar costs a bounded number of attempts and ends in a written
diagnosis instead of an infinite spin. Every check verify runs is a
deterministic command with a defined pass criterion, scoped to exclude
`project/` so it can never match the docs that quote the pattern. The only
exits are gather's two `DONE` conditions, plus ralph's own budget rails.

## Ignored files

`project/loops/brief.md` and `project/loops/blocked.md` are listed in the
repo-root `.gitignore` (via `*/project/loops/…` patterns) — both are
ephemeral loop state, never committed.
