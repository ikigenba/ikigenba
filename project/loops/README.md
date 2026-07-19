# The installed build loop

This is the human/author-facing overview of the `gather → build → verify` loop
**as installed** in this directory — kept beside the prompts it describes so it
can never drift from the loop actually on disk. It never restates the
`project/` spec shapes (see `project/README.md` and the `ikispec` skill for
those); it only describes loop mechanics.

## Running it

```
./project/loops/run
```

which wraps exactly:

```
ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` re-invokes each prompt with a **fresh context** every turn — no prompt
remembers a prior turn. It runs from the **service root** (this repo's root),
so every path the prompts reference is repo-root-relative (`project/…`).

## The status contract

Each turn ends with a terminal `status` (`ralph` reads only the *last* message
of a turn and advances on that):

- **`NEXT`** — advance to the next prompt in the cycle, wrapping
  `verify → gather`.
- **`DONE`** — stop the loop. **Only `gather` ever reports this**, and only when
  it finds no `⬜` phase left in `project/plan/STATUS.md`. `build` and `verify`
  always end on `NEXT`.
- **`CONTINUE`** — **non-terminal.** A streaming model (gpt-5.5 under codex,
  which `build` runs on) tags every message it streams with a status; `CONTINUE`
  is what it tags its progress messages with before its real terminal message.
  It never drives the loop by itself.

## Per-step reads / writes / commits / deletions

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| **gather** | `project/plan/STATUS.md`, one `project/plan/phase-NN.md`, its Decision's `project/design/DNN.md` (via `INDEX.md`), an in-flight `brief.md`'s header | `project/loops/brief.md` (contract region only, on a fresh brief) | nothing | nothing |
| **build** | `project/loops/brief.md` (both regions) | source + tests named in the brief | one non-empty commit per turn (never touches `STATUS.md`) | nothing |
| **verify** | `project/loops/brief.md` (both regions), runs `go test ./...` and the brief's structural checks | `project/loops/brief.md` feedback region (gap) | the phase-retirement commit (pass) | `project/plan/STATUS.md` line + `project/plan/phase-NN.md` (pass); `project/loops/brief.md` (pass, or on a 3-attempt stall reset) |

## The brief lifecycle

`project/loops/brief.md` is the seam that keeps `build`'s context scoped to one
phase — the complete and only input `build` and `verify` consume. It is
**never committed** (`project/loops/brief.md` is git-ignored) and describes
**one phase at a time**:

1. `gather` authors it once, when a phase first becomes the active `⬜` phase —
   the full design prose of its realized Decision(s) (Verification list
   omitted), the exact slice of `R-XXXX-XXXX` ids that phase owns (or
   `(none — structural phase)`), the files to touch, dependency interface
   signatures, and the done bar. Its `## Verify feedback` region starts empty.
2. It **persists across cycles** while the phase stays `⬜` — `gather` no-ops on
   an in-flight brief for the same phase (opens no big doc), so the big docs are
   read only once per phase, not once per cycle.
3. `build` consumes it every turn, prioritizing any open gaps in the feedback
   region, and never writes to the brief.
4. `verify` either deletes it (the phase passed, or the phase stalled 3
   consecutive no-progress attempts and its trajectory is being reset) or
   overwrites its feedback region with the currently-open gaps (still failing,
   still converging).

## Why it converges

`verify` can neither halt the loop nor advance a phase on a gap — an incomplete
phase just stays `⬜` and gets re-attacked next cycle, now with `verify`'s
grounded feedback in front of `build`. The persisted feedback also gives
`verify` cross-cycle memory: it can tell slow convergence (the open-gap id set
shrinking or changing) from a true stall (the same gap ids unsatisfied for 3
consecutive attempts with no new build commit), and resets the brief on a true
stall so the next `gather` rebuilds the contract fresh from spec. The only exit
is `gather → DONE`, which requires zero `⬜` phases left in `STATUS.md` — so the
run ends only when every phase has been verified and retired (or an external
`ralph` budget rail trips).

## `project/loops/brief.md` schema

```
# Brief — Phase NN

## Contract

- **Phase:** NN — <one-line objective>
- **Realizes:** <Decision id(s), or "—" for a structural phase>
- **Decision files:** <project/design/DNN.md paths>

### Design prose (verbatim, Verification lists omitted)
<...>

### Ids to cover
R-XXXX-XXXX — <full requirement text, verbatim>
<... or "(none — structural phase)">

### Files to touch
<...>

### Dependency interface signatures
<...>

### Done bar
<deterministic exit conditions>

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
