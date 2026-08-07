# gmail — build loop (gather → build → verify)

This directory holds the **installed** unattended build loop for the gmail
service: the three prompts `ralph` cycles to build the project one phase at a
time, the executable `run` wrapper, and this overview. It is kept beside the
prompts it describes so it can never drift from the loop on disk. It is **not** a
spec artifact — the spec shapes live under `project/product`, `project/design`,
and `project/plan`; `project/README.md` (the workspace map) only points here.

## Running it

```
project/loops/run
```

which is exactly:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the **service root** (`gmail/`, its working directory), so every
workspace path the prompts reference is service-root-relative (`project/…`). It
re-invokes each prompt in a **fresh, isolated context** and cycles
`gather → build → verify → gather → …` until the loop ends.

## Status contract

Each turn ends by reporting a `status` (and a one-sentence `message`). The
harness supplies the `{status, message}` schema out of band and reads back the
**final** message of the turn:

- `NEXT` — **terminal**: advance to the next prompt (wrapping `verify → gather`).
- `DONE` — **terminal**: stop the loop. **Only `gather` ever reports it**, when it
  finds no `⬜` phase left, or finds `project/loops/blocked.md`. `build` and
  `verify` always report `NEXT` — finishing a phase completely is still `NEXT`.
- `CONTINUE` — **non-terminal**: the status a streaming model tags the progress
  messages it emits *before* its terminal message. `ralph` reads only the last
  message, so `CONTINUE` never advances or ends the loop.

## Per-step reads / writes / commits / queue mutation

| step | reads | writes | commits | removes phase from queue |
|---|---|---|---|---|
| **gather** | `blocked.md` (existence), `STATUS.md`, one `phase-NN.md`, `INDEX.md`, the realized `DNN.md`, `product/README.md` (intent) | the brief's **contract region** (fresh phase only) | no | no |
| **build**  | **only** `project/loops/brief.md` (both regions) | source + co-located id-tagged tests | yes (the increment) | no |
| **verify** | `project/loops/brief.md` + runs the suite + the design/plan id-set greps for the ratchet | the brief's **feedback region** (on a gap); `project/loops/blocked.md` (on second stall) | yes (the `STATUS.md` line + `phase-NN.md` deletion, on pass) | yes (on pass) |

The toolchain the prompts bake in (from design's *Conventions*): "green" means
`cd gmail && go build ./...`, `cd gmail && go vet ./...`,
`cd gmail && gofmt -l .` (no output), and `cd gmail && go test ./...` all succeed
with zero failures. The next-phase lookup is
`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`. Tests are co-located
with the code they exercise as `*_test.go` files named for the behavior (e.g.
`cmd/gmail/nginx_test.go`, `cmd/gmail/landing_test.go`), never in a per-phase or
root-level file.

## Brief lifecycle

`project/loops/brief.md` is the ephemeral, single-phase seam between the prompts.
It is **never committed** (`project/loops/brief.md` matches the repo-root
`.gitignore`'s `*/project/loops/brief.md` pattern) and is **phase-scoped, not
per-cycle**:

- `gather` **authors the contract region once** when a phase first becomes the
  active `⬜` phase, and **no-ops** (leaves the brief untouched, opens no big doc)
  while that same phase is still in flight.
- `build` consumes the whole brief — contract **and** verify-feedback regions —
  and closes any open gaps first; it never writes the brief.
- `verify` re-derives truth independently and either **passes** the phase (delete
  its `STATUS.md` line and `phase-NN.md`, commit, delete the brief), records a
  **gap** (overwrite the feedback region with only the currently-open gaps, keep
  the brief), or — on a stuck trajectory — discards the brief (stall reset) or
  escalates to `project/loops/blocked.md` (second stall on the same phase). The
  brief therefore **persists across cycles** until the phase passes or a stall
  reset discards it.

## The stall and blocked ladder

`verify` tracks a per-phase **stall streak** in the brief's feedback region:
*progress* means the current open-gap id set is a strict subset of the prior
one (some gap closed); anything else — including a new build commit with the
same gaps — is *no progress* and increments the streak.

1. **Three consecutive no-progress attempts** on a phase that has **not**
   stalled before: a **trajectory reset** — `verify` logs the stall to
   `~/.ralph/verify.log`, deletes the brief, and leaves the phase `⬜`. The next
   `gather` rebuilds the contract fresh from spec — a stuck brief, not
   necessarily a broken bar.
2. **A second stall on the same phase** (found by grepping
   `~/.ralph/verify.log` for a prior `Phase NN STALLED` line): the rebuilt
   contract didn't help either, so the phase's **done bar itself** is the
   suspect, not the brief. `verify` writes `project/loops/blocked.md` naming
   the phase, the attempts, the unsatisfied ids, and the exact command/output
   that won't go green, then deletes the brief and leaves the phase `⬜`. The
   next `gather` sees `blocked.md` and reports `DONE`, stopping the run.

**Operator recovery from a blocked run:** read the diagnosis in
`project/loops/blocked.md` (the exact failing command and output), fix the
phase's done bar upstream in `project/plan/phase-NN.md` or the design Decision
it realizes, delete `project/loops/blocked.md`, and restart `project/loops/run`.

## Why it converges (human-free)

`verify` can neither halt the loop nor advance a phase on a gap, so an incomplete
phase just stays `⬜` and is re-attacked next cycle — now with `verify`'s
grounded, command-tied feedback in front of `build`, and without `gather`
re-reading the big docs (it no-ops on the in-flight brief). The persisted feedback
gives `verify` cross-cycle memory: it distinguishes *slow convergence* (the
open-gap id set shrinking) from a *true stall* (the **same** gap ids unsatisfied
for **3** consecutive attempts, commit motion notwithstanding). A true stall
triggers a **trajectory reset**; a second stall on the same phase escalates to
`blocked.md` instead of resetting forever, so a defective bar costs a bounded
number of attempts and yields a written diagnosis rather than spinning. The only
exits are `gather → DONE` on an empty queue (every phase verified green) or on
finding `blocked.md` (awaiting the operator) — plus the ralph budget rails.

## The `project/loops/brief.md` schema

A single-phase file with a **gather-owned contract region** and a
**verify-owned feedback region** (each written by exactly one step, so they never
clobber each other):

```
# Brief — Phase NN: <one-line objective>

phase: NN
realizes: D<n>[, D<m>]
decision_files:
  - project/design/D<nn>.md

## Design prose (copied verbatim from the DNN.md — Verification lists omitted)
<the realized Decision's full Decision. statement + shape/signatures + Rejected.
alternatives, verbatim, minus that Decision's Verification list>

## Ids to cover
R-XXXX-XXXX — <full requirement text copied verbatim from the Decision's Verification list>
# ...one id per line in that exact form, OR: (none — structural phase; see Done bar's named check)

## Files to touch
- gmail/<path>

## Dependency interfaces / required shapes (copied from design — do not open design files)
<the dependency packages' public signatures / required config shapes, copied verbatim>

## Done bar
- every id under "Ids to cover" covered by a genuinely-asserting, reachable
  `// R-XXXX-XXXX`-tagged test co-located with the code it exercises;
- the suite green (build, vet, gofmt -l, go test — all clean);
- any phase-specific named check.

## Verify feedback — attempt N
<empty when gather authors it; verify overwrites it with the current open gaps —
each an R-id + the exact failing command + observed output, plus the observed
build commit and stall streak>
```

The denominator for a phase is `grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}'
project/loops/brief.md` — the matched id substring per line, ignoring the trailing
requirement text — so it yields exactly the phase's id set. The global ratchet
`verify` runs each cycle is a separate, mechanical id-set comparison across
`project/design/D*.md` and `project/plan/phase-*.md` against the codebase's real
tagged tests — see `verify.md` for the exact command.
