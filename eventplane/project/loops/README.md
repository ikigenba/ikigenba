# eventplane — Build loop (as installed)

The unattended gather → build → verify loop that builds `eventplane` one phase
at a time from `project/design/` + `project/plan/`. Start it from the service
root (`eventplane/`):

```
project/loops/run
```

which is exactly:

```
exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the service root, re-invokes each prompt with a **fresh
context**, and advances on the **final** message's status alone.

## Status contract

- `NEXT` — terminal: advance to the next prompt (wrapping verify → gather).
- `DONE` — terminal: stop the loop. **Only gather ever reports it**, and only
  when `project/loops/blocked.md` exists or `STATUS.md` has no `⬜` phase
  left. Build and verify always end on `NEXT`.
- `CONTINUE` — non-terminal: the status a streaming model tags its mid-turn
  progress messages with. It never terminates a turn and never drives the
  loop; ralph reads only the last message.

## Who reads / writes / commits / deletes what

| step | reads | writes | commits | deletes completed phase |
|---|---|---|---|---|
| gather | `blocked.md` (existence), `STATUS.md`, one `phase-NN.md`, `INDEX.md`, realized `DNN.md`(s), dependency signatures | brief contract region (fresh briefs only) | never | never |
| build | the brief only (both regions) | source + co-located id-tagged tests | yes — the increment | never |
| verify | the brief + the repo (re-derives truth) | brief feedback region; or deletes the brief; or writes `blocked.md` | yes — the phase deletion on pass | pass only: removes the `STATUS.md` line + `phase-NN.md` |

## Brief lifecycle

`project/loops/brief.md` is the seam between the steps — the complete and
only input build and verify consume. It is **gitignored, never committed**,
single-phase, and **phase-scoped, not per-cycle**:

1. gather authors it once when a phase first becomes the active `⬜` phase;
   on every later cycle of the same phase, gather sees the matching
   `# Brief — Phase NN` header and no-ops (no big doc reads, feedback
   preserved).
2. build consumes it — feedback gaps first, then the remaining contract work.
3. verify either **passes** (delete the phase's `STATUS.md` line and
   `phase-NN.md`, commit, delete the brief) or finds **gaps** (overwrite the
   feedback region with only the currently-open gaps; brief persists into the
   next cycle).

## The stall and blocked ladder

- **Slow convergence** — each gap cycle, verify overwrites the feedback region
  with only the ids still open. A strictly shrinking gap set — some open gap
  from last attempt now closed — is progress; the stall streak resets to 0. A
  new build commit alone is never progress and never resets the streak.
- **Stall (streak reaches 3)** — the same (or a non-shrinking) gap set across
  three consecutive attempts. Verify logs the stall to
  `~/.ralph/verify.log`, deletes the brief, and leaves `⬜` — a **trajectory
  reset**: the next gather rebuilds the contract fresh from spec, in case a
  stale accumulated brief (not the bar itself) was the problem.
- **Blocked (a second stall on the same phase)** — verify greps
  `~/.ralph/verify.log` for an earlier `Phase NN STALLED` line before
  resetting again. If one exists, a rebuilt contract already failed to
  converge, so the phase's done bar itself is the prime suspect and no amount
  of rebuilding fixes that. Verify writes `project/loops/blocked.md` (the
  phase, total attempts, unsatisfied ids, and the exact command/output that
  will not go green), logs the escalation, deletes the brief, and leaves `⬜`.
  The next gather sees `blocked.md` on sight and reports `DONE` without
  reading anything else.
- **Operator recovery** — read `project/loops/blocked.md` for the recorded
  command and output, fix the phase's done bar in `project/plan/` or
  `project/design/` (an authoring move, not a code edit), delete
  `project/loops/blocked.md`, and restart the loop with `project/loops/run`.

Both rungs of the ladder stay inside the "verify never halts / never advances
a phase on a gap" invariant — a defective bar costs a handful of attempts and
a written diagnosis, never an unbounded spin.

## Why it converges

Verify can neither halt nor advance a phase on a gap, so an incomplete phase
stays `⬜` and is re-attacked next cycle — with verify's command-grounded
feedback in front of build, and without gather re-reading the big docs (it
no-ops on the in-flight brief). The persisted feedback gives verify
cross-cycle memory: a strictly shrinking gap set is slow convergence; a
non-shrinking set (a new build commit alone never counts) is a stall,
answered by the reset-then-block ladder above. The only exits are
gather → `DONE`: zero `⬜` markers (every
phase verified green) or a blocked phase awaiting the operator — plus
ralph's budget rails.

## The brief schema

Two regions, one writer each — the writers never clobber each other.

**Contract region** (gather-owned; written once per phase):

```markdown
# Brief — Phase NN
<one-line objective>

## Realized Decisions
- D<N> — <title> (project/design/DNN.md)

## Design — D<N> <title>
<the Decision's full design prose verbatim — Decision statement,
shapes/signatures, Rejected alternatives — with its Verification list OMITTED>

## Ids to cover
R-XXXX-XXXX — <full requirement text verbatim, same line>
(one id per line, id at line-start; or "(none — structural phase)")

## Files to touch
- <path> — <what changes>

## Dependency interfaces
<copied-in exported signatures, or "(none — no dependencies)">

## Done bar
<the phase's Done-when conditions verbatim: id-tagged co-located tests on the
required substrate, no skipped/gated requirement test, go test ./... + go vet
./... exit 0 from eventplane/, gofmt -l . empty, plus the phase's own
grep/list/diff checks with their exact pass criteria>
```

**Feedback region** (verify-owned; overwritten each gap cycle, written empty
by gather):

```markdown
## Verify feedback — attempt N
- build commit: <sha>
- stall streak: <k>
- open gaps:
  - R-XXXX-XXXX — `<exact failing command>` → <observed output> (<file:line>)
```

The denominator is grep-able: `grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}'
project/loops/brief.md` yields exactly the phase's id set.

`project/loops/brief.md` and `project/loops/blocked.md` are both gitignored —
they are loop-runtime state, never committed.
