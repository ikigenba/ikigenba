# scripts — build loop (gather → build → verify)

This directory holds the installed **three-prompt build loop** an unattended
harness (`ralph`) re-invokes with a **fresh context** every turn to build the
scripts service one phase at a time, straight from its `project/` spec. This
README describes the loop **as installed** — it lives beside the prompts so it can
never drift from what is on disk. It carries no spec shapes (those live in the
`project/` docs and the `ikispec` skill); it documents only the loop mechanics.

## Running it

From the service root (`scripts/`):

```
./project/loops/run
```

which is exactly:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the service root (its working directory), cycles the three
prompts in fresh contexts (`gather → build → verify → gather → …`), and reads only
the **final** message of each turn. The loop stops when `gather` finds either no
`⬜` phase left in `project/plan/STATUS.md`, or a `project/loops/blocked.md` file
(a phase awaiting the operator) — or a ralph budget rail trips.

## Status contract

Each turn's **final** message reports a `status` and a one-sentence `message`.
`ralph` injects the `{status, message}` schema per backend (codex via
`--output-schema`, claude via `--json-schema`) and reads it back itself — the
prompts describe the contract only, never a transport.

| status | meaning |
|---|---|
| `CONTINUE` | **non-terminal** — a progress message streamed *before* the turn's final message; never advances the loop. (codex coerces every streamed message into the schema, so a narrating model tags its mid-turn messages with this.) |
| `NEXT` | **terminal** — this turn is done; advance to the next prompt (`verify → gather` wraps). |
| `DONE` | **terminal** — the whole job is complete; the loop stops. **Only `gather` ever reports this**, and only on an empty `⬜` queue or a `blocked.md` file. |

`build` and `verify` **always** end on `NEXT` — finishing a phase completely,
green suite and all, is still `NEXT`; ending the run is never theirs.

## Per-step responsibilities

| step | reads | writes / commits | mutates queue? | terminal status |
|---|---|---|---|---|
| **gather** | `blocked.md`; the big docs — `STATUS.md`, one `phase-NN.md`, `INDEX.md`, the realized `DNN.md` | authors `brief.md`'s **contract region** once per phase (no-ops on an in-flight brief); commits nothing | no | `DONE` if `blocked.md` exists or no `⬜` phase, else `NEXT` |
| **build** | **only** `brief.md` (contract + feedback) | builds the package/artifact + id-tagged tests; commits the code increment; never writes the brief | no | always `NEXT` |
| **verify** | `brief.md` + re-runs the suite/coverage/ratchet from scratch | on pass: deletes the phase's `STATUS.md` line + `phase-NN.md`, commits the deletion, deletes the brief; on gap: overwrites the **feedback region**, or (after a repeat stall) writes `blocked.md` | **yes** (the only step that does) | always `NEXT` |

The next unit of work is found with
`grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1` (pending phase lines
are Markdown bullets: `- Phase NN ⬜ …`). "The suite is green" means all of
`cd scripts && go build ./...`, `cd scripts && go vet ./...`,
`cd scripts && gofmt -l .` (no output), and `cd scripts && go test ./...` succeed
with zero failures (design's *Conventions*).

## The `brief.md` seam and its lifecycle

`project/loops/brief.md` is the single, phase-scoped seam the three prompts share.
It is **never committed** (the repo-root `.gitignore` carries
`*/project/loops/brief.md`), and it describes exactly **one** phase at a time.
Its lifecycle:

- **gather** authors the **contract region** when a phase first becomes the active
  `⬜` phase, then **leaves it untouched** every cycle the phase stays `⬜` (it
  reads the header and no-ops, never re-opening the big docs).
- **build** consumes the whole brief; if the feedback region lists open gaps it
  closes those first, then commits its increment. It never writes the brief.
- **verify** owns the **feedback region**. On a pass it deletes the phase's
  `STATUS.md` line and its `phase-NN.md` body file, commits that deletion, and
  **deletes** the brief. On a gap it leaves `⬜`, changes no source, and
  **overwrites** the feedback region with the currently-open gaps — the brief
  **persists** across cycles until the phase passes or a stall reset discards it.

The two writers never clobber each other: gather owns the contract region, verify
owns the feedback region, build writes neither.

## The stall and blocked ladder

`verify` tracks progress cycle over cycle by comparing the current open-gap id
set to the prior `## Verify feedback` region's set. A new build commit alone
never counts as progress — only a shrinking gap set does.

1. **Slow convergence** — the gap set shrinks (or changes) each cycle: `verify`
   just overwrites the feedback region with the new open gaps and hands back to
   `build`. The stall streak resets to 0.
2. **Stall (3 consecutive no-progress attempts)** — the same gaps stay
   unsatisfied for three cycles running: `verify` treats the accumulated brief as
   not converging, logs `Phase NN STALLED …` to `~/.ralph/verify.log`, deletes
   `brief.md`, and leaves the phase `⬜`. The next `gather` rebuilds the contract
   fresh from the current spec — a **trajectory reset**, not a halt.
3. **Blocked (a second stall on the same phase)** — if `~/.ralph/verify.log`
   already has an earlier `STALLED` line for this phase, a rebuilt contract has
   already been tried and did not help: the fault is most likely the phase's own
   done bar, which no amount of rebuilding fixes. `verify` writes
   `project/loops/blocked.md` naming the phase, the attempts, the unsatisfied
   ids, and the exact command + output that will not go green, logs a `BLOCKED`
   line, deletes the brief, and leaves the phase `⬜`. The next `gather` sees
   `blocked.md` and reports `DONE` — the run stops for the operator.

**Operator recovery from a `blocked.md`:** read the recorded failing command and
output, fix the phase's done bar or its Decision in `project/` (the loop never
edits `project/` itself), delete `project/loops/blocked.md`, and restart
`./project/loops/run`.

### Why it converges

`verify` can neither halt nor advance a phase on a gap, so an incomplete phase
just stays `⬜` and is re-attacked next cycle — now with verify's grounded,
command-tied feedback in front of `build`, and without `gather` re-reading the big
docs (it no-ops on the in-flight brief). The stall/blocked ladder turns a
non-converging phase into, at worst, a handful of wasted attempts and one written
diagnosis, instead of a loop that spins forever. The only exits are `gather`
reporting `DONE` — either the queue is empty (every phase verified green and
removed) or a blocked phase is waiting on the operator.

## `brief.md` schema

```
# Brief — Phase NN: <one-line objective>

phase: NN
realizes: D<n>[, D<m>]
decision_files:
  - project/design/D0n.md

## Design prose — D<n> (<short label>)
<the realized Decision's full Decision statement + shape/signatures + Rejected.,
 copied verbatim from D0n.md, with that Decision's Verification list OMITTED>

## Ids to cover
R-XXXX-XXXX — <full requirement text copied verbatim from the Decision>
# ...one id per line in that exact form, OR: (none — structural phase; see Done bar)

## Files to touch
- scripts/<path>

## Dependency interfaces / required shapes (copied from design — do not open design files)
<public signatures / the exact required config snippet, copied verbatim>

## Done bar
- every id covered by a genuinely-asserting `// R-XXXX-XXXX` test that actually
  runs; test placement (co-located `internal/<pkg>/<pkg>_test.go`; the
  nginx/landing content-assertion tests in `cmd/scripts/main_test.go`; never a
  root-level or `phaseNN_test.go` file); the green-suite commands; any
  phase-specific named check.

## Verify feedback — attempt N
<gather writes this empty; verify overwrites it with only the currently-open
 gaps — each an R-id + the exact failing command + observed output — plus the
 attempt counter, the observed build commit, and the stall streak>
```

The contract region (everything above `## Verify feedback`) is written once by
gather; the feedback region is owned by verify. `project/README.md` (the
workspace map) points here for these mechanics and does not restate them.
