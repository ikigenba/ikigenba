# prompts — build loop (installed)

This is the human/author-facing overview of the `ralph` build loop as it is
actually installed in this directory. It can never describe a different loop
than the one on disk — if you change `gather.md`, `build.md`, or `verify.md`,
update this file to match.

## Running it

```
project/loops/run
```

which wraps, exactly:

```
ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the service root (`prompts/`); every path the prompts
reference is service-root-relative (`project/…`).

## The status contract

Every turn ends with a terminal `status`:

- **`NEXT`** — advance to the next prompt in the cycle (`gather → build →
  verify → gather → …`).
- **`DONE`** — stop the whole run. Only `gather` ever reports this — on
  finding no `⬜` phase left in `project/plan/STATUS.md`, or on finding
  `project/loops/blocked.md`. `build` and `verify` always report `NEXT`, even
  when a phase is fully done — the temptation to report `DONE` because "the
  work feels finished" belongs to `gather` alone.
- **`CONTINUE`** — non-terminal. A streaming model (e.g. one running under
  codex) tags every message it emits mid-turn with a status; `ralph` reads
  only the turn's final message, so `CONTINUE` is what covers the progress
  narration before that final `NEXT`/`DONE`.

## Per-step reads / writes / commits / deletions

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| `gather` | `project/plan/STATUS.md`, one `phase-NN.md`, `project/design/INDEX.md`, the realized `DNN.md`(s), dependency interfaces | `project/loops/brief.md` (contract region only, on a fresh phase) | never | never |
| `build` | `project/loops/brief.md` (whole) | source + id-tagged tests | this turn's increment | never |
| `verify` | `project/loops/brief.md` (whole), the live suite | `project/loops/brief.md` (feedback region only, on a gap); `project/loops/blocked.md` (on a second stall) | the phase-retirement deletion (on a pass) | `project/plan/phase-NN.md` + its `STATUS.md` line (on a pass); `project/loops/brief.md` (on a pass, or on a stall reset) |

## The brief's lifecycle

`project/loops/brief.md` is the seam that keeps `build`'s context scoped to
one phase. `gather` authors its **contract region** once, the first cycle a
phase becomes the active `⬜` line, and leaves it untouched on every
subsequent cycle while that phase stays `⬜` (it no-ops rather than
re-reading the big docs). `build` reads the whole brief, including any
feedback, but never writes to it. `verify` owns the **feedback region**
exclusively: a pass deletes the brief outright; a gap overwrites the feedback
region with the currently-open gaps (never appends — an append would stack
stale gaps across cycles). The brief is never committed
(`project/loops/brief.md` is gitignored) and only ever describes one phase at
a time.

## The stall and blocked ladder

`verify` tracks, cycle to cycle, whether the open-gap id set is shrinking:

1. **Progress** (the gap set strictly shrank since the last attempt) resets
   the stall streak to 0.
2. **No progress** for **3** consecutive attempts triggers a **trajectory
   reset**: the accumulated brief is discarded (deleted, `⬜` left in place,
   the stall logged to `~/.ralph/verify.log`) so the next `gather` rebuilds
   the contract fresh from the current spec.
3. A **second** stall on the **same phase** (found via the same log) means a
   rebuilt contract already failed to converge — the fault is the phase's
   done bar itself, not the trajectory. `verify` writes
   `project/loops/blocked.md` naming the phase, the attempts, the
   still-open ids, and the exact failing command/output, then leaves `⬜`.

**Operator response to a `blocked.md`:** read the recorded command and
output, fix the phase's done bar (or the design/plan it derives from) in
`project/`, delete `project/loops/blocked.md`, and restart the loop. The next
`gather` sees no `blocked.md`, finds the same `⬜` phase, and (since the brief
is gone) authors a fresh brief against the corrected spec.

## Why it converges

`verify` can neither halt the run nor advance a phase on a gap — an
incomplete phase simply stays `⬜` and gets re-attacked next cycle, now with
`verify`'s grounded feedback in front of `build`, and without `gather`
re-reading the big docs (it no-ops on the in-flight brief). The stall ladder
gives that retry loop an exit even when a phase's own bar is unreachable: two
failed rebuild attempts turn into a written diagnosis for the operator
instead of an infinite spin. The loop's only two ends remain `gather`
reporting `DONE` — zero `⬜` phases left, or a `blocked.md` awaiting the
operator — plus the `ralph` budget rails.

## The `project/loops/brief.md` schema

```markdown
# Brief — Phase NN

## Objective
<one-line objective>

## Realizes
<Decision id(s) and file path(s)>

## Design prose (verbatim, Verification list excluded)
<Decision statement, shape/signatures, Rejected alternatives>

## Ids to cover
R-XXXX-XXXX — <full requirement text, verbatim>
<...>

## Files to touch
<paths>

## Dependency interfaces
<public signatures only>

## Done bar
<deterministic Done-when conditions>

## Verify feedback — attempt N
<empty when fresh; verify's currently-open gaps otherwise>
```

`project/loops/brief.md` and `project/loops/blocked.md` are both listed in
`prompts/.gitignore` — neither is ever committed.

For the spec shapes this loop reads from (`project/product/`,
`project/design/`, `project/plan/`), see `project/README.md` and the
`$ikispec` skill — this file describes only the loop mechanics, never the
spec contracts.
