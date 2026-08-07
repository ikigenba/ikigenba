# nginx — build loop

This is the installed `gather → build → verify` loop for the `nginx/` tree,
driven by `ralph` and re-invoked with a **fresh context** every turn. It lives
beside the prompts it describes so it can never drift from what is actually
on disk. It does not restate the spec — see `project/product/README.md`,
`project/design/README.md`, and `project/plan/README.md` for the why/how/order
— only how the loop that builds against that spec behaves.

## Running it

```
./project/loops/run
```

which wraps, exactly:

```
ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the `nginx/` service root, so every path the three prompts
reference is `project/…`-relative to that root.

## Status contract

Each turn ends with exactly one **terminal** status, plus an optional
non-terminal one used mid-turn:

- `CONTINUE` — non-terminal. Any progress message a prompt streams before its
  final message (some backends, e.g. codex, tag every streamed message with a
  status; `ralph` only acts on the last one).
- `NEXT` — terminal. Advance to the next prompt in the cycle
  (`gather → build → verify → gather → …`).
- `DONE` — terminal, and **only `gather` ever reports it**: either no `⬜`
  phase remains in `project/plan/STATUS.md` (every phase verified green) or
  `project/loops/blocked.md` exists (a phase awaiting the operator). `build`
  and `verify` always report `NEXT`, even when a phase is fully done — ending
  the run is never theirs to call.

## What each step reads / writes / commits

| Step | Reads | Writes | Commits | Deletes |
|---|---|---|---|---|
| `gather` | `STATUS.md`, `blocked.md` (existence), the one pending `phase-NN.md`, its Decision `DNN.md` via `INDEX.md` | `brief.md` (contract region only, on a fresh brief) | never | never |
| `build` | `brief.md` (contract + feedback) | config/script files the brief names | this turn's increment | never |
| `verify` | `brief.md`, re-derives all checks itself | `brief.md` (feedback region, on a gap) | the phase-retirement commit, on a pass | `brief.md` (on pass or stall reset), the phase's `STATUS.md` line + `phase-NN.md` (on pass) |

## The brief's lifecycle

`project/loops/brief.md` is the ephemeral seam between the three steps — never
committed (`.gitignore`d), single-phase, and phase-scoped rather than
per-cycle:

- `gather` authors it once, when a phase first becomes the active `⬜` phase.
- While that phase stays `⬜`, later `gather` turns find the same phase named
  in the brief's header and leave it untouched (no re-read of design/plan).
- `build` reads it every turn (contract + any feedback) and never writes it.
- `verify` either deletes it (the phase passed, or a stall reset gave up on
  the accumulated brief) or overwrites its feedback region with the current
  open gaps (the phase still has a gap).

Schema:

```markdown
# Brief — Phase NN
## Objective
## Decision(s) realized       <- verbatim Decision prose, Verification list excluded
## Ids to cover                <- always "(none — structural phase)" in this tree today
## Files to touch
## Dependency interfaces
## Done bar
## Verify feedback — attempt N <- verify-owned; empty on a fresh brief
```

The contract region (everything above `## Verify feedback`) is gather-owned;
`verify` never writes there. The feedback region is verify-owned; `gather`
never writes there and leaves it alone on a no-op cycle.

## Stall and blocked ladder

`verify` tracks, per phase, whether each gap cycle actually closes a gap:

1. **Progress** (the open-gap set shrank since the prior attempt) resets the
   stall streak to 0.
2. **No progress** for **3 consecutive attempts** is a stall: `verify` logs it
   to `~/.ralph/verify.log`, deletes the brief, leaves `⬜`, and returns
   `NEXT`. The next `gather` rebuilds the contract fresh from spec — a
   trajectory reset, not a giveup.
3. If **this same phase** stalls a **second** time (found via a prior
   `STALLED` line in `~/.ralph/verify.log`), a rebuilt brief already failed to
   help, so `verify` treats the done bar itself as defective: it writes
   `project/loops/blocked.md` naming the phase, the attempts, the
   still-unsatisfied checks, and the exact failing command/output, then logs
   `BLOCKED` and deletes the brief. The next `gather` sees `blocked.md` and
   reports `DONE`, stopping the run.

**Operator recovery from a `blocked.md`:** read the recorded command and
output, fix the phase's done bar in `project/plan/` or `project/design/`
(never patch it from inside the loop — `project/` is read-only to `gather`,
`build`, and `verify`), delete `project/loops/blocked.md`, and restart
`./project/loops/run`.

## Why this converges

`verify` can neither halt the run nor advance a phase on a gap — an
incomplete phase just stays `⬜` and gets re-attacked next cycle, now with
`verify`'s grounded feedback in front of `build`, and without `gather`
re-reading the big docs each cycle. The stall/blocked ladder turns a
genuinely un-satisfiable bar into a bounded number of attempts and a written
diagnosis instead of an infinite spin. The only two ways out are `gather`
finding zero `⬜` phases (everything verified green) or a `blocked.md`
awaiting the operator — plus `ralph`'s own budget rails.

## This tree's specifics

`nginx/` mints no Verification ids today (every Decision's Verification list
says "ids: none" — see `project/design/README.md` "Requirement ids"), so every
phase here is **structural**: its done bar is an exact committed file, an
exact diff/grep, or a real `nginx -t`, never a tagged-test count. The two
checks every phase's done bar builds on:

- `bash -n nginx/run` exits 0.
- `mkdir -p nginx/tmp && nginx -p nginx -c nginx.conf -t` exits 0.

`verify.md` still runs a coverage-ratchet grep over `project/design/D*.md`
so the loop keeps working unmodified the moment some future Decision does
mint an id — see its step 3.
