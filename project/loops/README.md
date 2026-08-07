# The installed build loop

This is the human/author-facing overview of the `gather → build → verify` loop
**as installed** in this directory — kept beside the prompts it describes so it
can never drift from the loop actually on disk. It never restates the
`project/` spec shapes (see `project/README.md` and the `ikispec` skill for
those); it only describes loop mechanics.

This loop drives the **umbrella project**: the repo root's `project/` governs
the suite's shared contracts and **builds no code of its own**. A phase here
never produces an implementation — it amends a contract (rewrites a
`project/design/D<N>.md` in place, or adds one) and regenerates
`project/design/INDEX.md` to match. Every id it mints carries a
**proof-location marker** (`[proof: <tree>]` or `[proof: per-service]`) naming
where the behavior is actually tested; this loop's own gate is structural
(Decision/INDEX consistency, scope, and — where a marker names an already-built
tree — the marker's own coverage grep), never a code test suite.

## Running it

```
./project/loops/run
```

which wraps exactly:

```
ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` re-invokes each prompt with a **fresh context** every turn — no prompt
remembers a prior turn. It runs from the **repo root**, so every path the
prompts reference is repo-root-relative (`project/…`).

## The status contract

Each turn ends with a terminal `status` (`ralph` reads only the *last* message
of a turn and advances on that):

- **`NEXT`** — advance to the next prompt in the cycle, wrapping
  `verify → gather`.
- **`DONE`** — stop the loop. **Only `gather` ever reports this**, either
  because `project/loops/blocked.md` exists or because it finds no `⬜` phase
  left in `project/plan/STATUS.md`. `build` and `verify` always end on `NEXT`.
- **`CONTINUE`** — **non-terminal.** A streaming model (gpt-5.5 under codex,
  which `build` runs on) tags every message it streams with a status;
  `CONTINUE` is what it tags its progress messages with before its real
  terminal message. It never drives the loop by itself.

## Per-step reads / writes / commits / deletions

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| **gather** | `project/plan/STATUS.md`, one `project/plan/phase-NN.md`, the amended `project/design/DNN.md` (if it exists), `project/design/INDEX.md`, an in-flight `brief.md`'s header, `project/loops/blocked.md`'s existence | `project/loops/brief.md` (contract region only, on a fresh brief) | nothing | nothing |
| **build** | `project/loops/brief.md` (both regions) | `project/design/D<N>.md` (amended or new) and `project/design/INDEX.md` — never any other file | one non-empty commit per turn (never touches `STATUS.md`) | nothing |
| **verify** | `project/loops/brief.md` (both regions), the amended `D<N>.md`, `INDEX.md`, and — only where a marker names an already-built tree — that tree's `*_test.go` files | `project/loops/brief.md` feedback region (gap); `project/loops/blocked.md` (second stall) | the phase-retirement commit (pass) | `project/plan/STATUS.md` line + `project/plan/phase-NN.md` (pass); `project/loops/brief.md` (pass, or on a stall reset) |

## The brief lifecycle

`project/loops/brief.md` is the seam that keeps `build`'s context scoped to one
phase — the complete and only input `build` and `verify` consume. It is
**never committed** (`project/loops/brief.md` is git-ignored) and describes
**one phase at a time**:

1. `gather` authors it once, when a phase first becomes the active `⬜` phase —
   the amended Decision's current prose (Verification list omitted), the
   phase's directed changes (the exact Decision/Rejected prose and
   Verification lines to write, or the count of fresh ids to mint), any
   downstream assignment to record, the files to touch (always exactly
   `project/design/D<N>.md` + `project/design/INDEX.md`), and the done bar.
   Its `## Verify feedback` region starts empty.
2. It **persists across cycles** while the phase stays `⬜` — `gather` no-ops on
   an in-flight brief for the same phase (opens no big doc), so the big docs
   are read only once per phase, not once per cycle.
3. `build` consumes it every turn, prioritizing any open gaps in the feedback
   region, and never writes to the brief.
4. `verify` either deletes it (the phase passed, or the phase stalled 3
   consecutive no-progress attempts and its trajectory is being reset) or
   overwrites its feedback region with the currently-open gaps (still failing,
   still converging).

## The stall and blocked ladder

- **Three consecutive attempts closing no gap** → `verify` performs a
  **trajectory reset**: it logs the stall to `~/.ralph/verify.log`, deletes the
  brief, and leaves the phase `⬜`. The next `gather` rebuilds the contract
  fresh from `project/plan/phase-NN.md` — a clean slate, in case the
  accumulated brief itself had drifted.
- **A second stall on the same phase** → the trajectory was already reset once
  and still did not converge, so `verify` treats the phase's own directed
  changes or done bar as the fault: it writes `project/loops/blocked.md`
  naming the phase, the total attempts, the still-unsatisfied ids/checks, and
  the exact command + output that will not go green, logs the escalation, and
  leaves the phase `⬜`.
- **Operator response to `blocked.md`**: read the recorded command and output,
  fix the phase's directed changes or done bar in `project/plan/phase-NN.md`
  (or the Decision it targets, if the contract itself needs rework — this is
  the one docs-only tree where a plan phase legitimately edits
  `project/design/`), delete `project/loops/blocked.md`, and restart the loop.

## Why it converges

`verify` can neither halt the loop nor advance a phase on a gap — an
incomplete phase just stays `⬜` and gets re-attacked next cycle, now with
`verify`'s grounded feedback in front of `build`. The persisted feedback also
gives `verify` cross-cycle memory: it can tell slow convergence (the open-gap
id set shrinking) from a true stall (the same gaps unsatisfied for 3
consecutive attempts with no new commit), and resets the brief on a true
stall so the next `gather` rebuilds the contract fresh. A second stall on the
same phase escalates to `blocked.md` instead of resetting forever, so a
defective bar costs a handful of attempts and yields a written diagnosis
instead of spinning. The only exit is `gather → DONE`, which requires either
zero `⬜` phases left in `STATUS.md` or a `blocked.md` awaiting the operator —
so the run ends only when every phase has been verified and retired, or a
phase needs a human decision (or an external `ralph` budget rail trips).

## `project/loops/brief.md` schema

```
# Brief — Phase NN

## Contract

- **Phase:** NN — <one-line objective>
- **Decision:** D<N> — <amended|new> (`project/design/D<N>.md`)
- **INDEX entry:** `project/design/INDEX.md` (regenerate the D<N> line and the
  reverse-map lines for every id this phase adds, changes, or removes)

### Current design prose (verbatim, Verification list omitted)
<...>

### Directed changes (verbatim from the phase)
<...>

### Downstream assignment
<...>

### Files to touch
project/design/D<N>.md
project/design/INDEX.md

### Done bar
<...>

## Verify feedback

(none yet)
```

— on a gap, `verify` overwrites `## Verify feedback` with:

```
## Verify feedback — attempt N+1

- Build commit observed: <sha>
- Stall streak: <count>

### Open gaps
- R-XXXX-XXXX — <exact failing command> → <observed output> (file:line)
```
