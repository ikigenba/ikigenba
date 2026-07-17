# sites — build loop (installed)

This directory holds the **three-prompt `gather → build → verify` build loop**
that an unattended harness (`ralph`) re-invokes with a **fresh context** every
turn to build sites one phase at a time from the sealed spec in `project/`. This
README describes the loop **as installed on disk** — it lives beside the prompts
so it can never drift from them. It carries no spec shapes (those live in
`project/design/` and `project/plan/`); `project/README.md` only points here.

## Running it

```
project/loops/run
```

The wrapper is exactly:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs the three prompts in a cycle, each in a fresh isolated context,
wrapping `verify → gather`. Workspace paths in the prompts are service-root
(`sites/`) relative; the Go toolchain commands run as `cd sites && …`.

## The status contract

Each turn's **final** message reports a `status` the harness reads back out of
band (the `{status, message}` schema is injected per backend — codex via
`--output-schema`, claude via a synthetic `StructuredOutput` tool — so the
prompts describe the contract, never a transport):

| status | kind | meaning |
|---|---|---|
| `CONTINUE` | non-terminal | a progress message streamed *before* the turn's final message; never advances the loop. A streaming backend (e.g. gpt-5.6 under codex) tags its mid-turn narration with this. |
| `NEXT` | terminal | this turn is done; advance to the next prompt. **build and verify always end here.** |
| `DONE` | terminal | the whole job is complete; the loop stops. **Only `gather` ever reports this** — when no `⬜` phase remains. |

`ralph` reads only the **last** message of a turn and advances on its terminal
`NEXT`/`DONE`.

## What each step reads, writes, and may mutate

| step | reads | writes | commits | deletes phase | deletes brief |
|---|---|---|---|---|---|
| **gather** | the big docs (`project/plan/*`, `project/design/*`), for one phase only | the brief's **contract region** (fresh phases only) | no | no | no |
| **build** | **only** `project/loops/brief.md` | source + co-located `*_test.go` | yes (code) | no | no |
| **verify** | the brief + the running suite | the brief's **feedback region** (on a gap) | yes (deletion / stall log) | **yes** (`STATUS.md` line + `phase-NN.md`, pass only) | **yes** (pass or stall reset) |

- **gather** greps `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
  for the first `⬜` phase. None left → the queue is empty, report `DONE`.
  Otherwise, if a brief for **that same** phase already exists it is mid-flight
  and left untouched (no big doc opened); only when no brief exists (or it names
  a phase with no `STATUS.md` line left — completed, hence deleted) does gather
  read that one `phase-NN.md`, resolve its Decision(s) via `INDEX.md`, read only
  those `DNN.md`, and author a fresh brief. Returns `NEXT`.
- **build** reads the whole brief (contract + any feedback), closes listed gaps
  first, does as much of the phase as cleanly fits one context (ideally the whole
  phase), commits the code, leaves the phase's `⬜` line and body file in place,
  never touches the brief. Returns `NEXT`.
- **verify** re-derives truth independently: runs the suite, checks that every
  brief id has a genuinely-asserting, actually-running `// R-XXXX-XXXX` test.
  Pass → delete the phase's `STATUS.md` line and its `phase-NN.md`, commit,
  delete the brief. Gap → leave the `⬜` line and body file as is, change no
  source, overwrite the feedback region with only the open gaps. Returns `NEXT`.

## Brief lifecycle

`project/loops/brief.md` is the **ephemeral seam** — the complete and only input
build and verify consume, so neither reopens design or plan. It is **never
committed** (already ignored via the repo-root `.gitignore` rule
`*/project/loops/brief.md`), **single-phase**, and **phase-scoped**: gather
authors the contract **once** when a phase first becomes the active `⬜` phase and
no-ops while it stays in flight; build consumes it every cycle; verify persists it
across cycles (overwriting the feedback region on each gap) until the phase passes
(delete) or a stall reset (delete). A fresh `gather` then rebuilds it from spec.

## Why it converges (human-free)

`verify` can neither halt the loop nor advance a phase on a gap, so an incomplete
phase just stays `⬜` and is re-attacked next cycle — now with verify's
command-grounded feedback in front of build, and without gather re-reading the big
docs (it no-ops on the in-flight brief). The persisted feedback gives verify
cross-cycle memory: it distinguishes *slow convergence* (the open-gap id set
shrinking/changing) from a *true stall* (the **same** gap ids unsatisfied for **3**
consecutive attempts with **no new build commit**). On a true stall verify does a
**trajectory reset** — discards the brief, logs the stall to `~/.ralph/verify.log`,
leaves the `⬜` line and body file in place — so the next gather rebuilds the
contract fresh. The only exit is `gather → DONE`, which requires **zero** `⬜`
phase lines left in `STATUS.md` — so the run ends only when every phase has been
verified green and deleted (or a `ralph` budget rail trips).

## The green bar

"The suite is green" (from design's *Conventions*) means **all** of:

```
cd sites && go build ./...
cd sites && go vet ./...
cd sites && gofmt -l .      # prints nothing
cd sites && go test ./...
```

succeed with zero failures. Green **includes** the D23 headless-Chrome wiring
test and therefore hard-requires a `google-chrome` binary on `PATH`; no Chrome
makes the suite **red**, never skipped (one browser-*launch* retry is allowed;
scenario assertions are never retried). Coverage of a requirement id means a
genuinely-asserting `// R-XXXX-XXXX`-tagged test that actually runs under
`go test ./...` (no SKIP, no unreachable gate), **co-located** with the code it
exercises in a package-local `*_test.go` (or, for the cross-package render / store
/ browser tests, in `sites/cmd/sites/*_test.go`) — never a per-phase or
root-level test file.

## `project/loops/brief.md` schema

Two region-owned parts, one writer each:

**Contract region (gather-owned, written once per phase):**

```
# Brief — Phase NN

## Objective
<one-line objective>

## Realized Decision(s)
- D<n> — <title> — project/design/D<NN>.md

## Design prose (verbatim; Verification lists omitted)
### D<n> — <title>
<Decision statement + shape/signatures + Rejected, verbatim from D<NN>.md,
 WITHOUT that Decision's Verification list>

## Ids to cover
R-XXXX-XXXX — <full requirement text, verbatim from the Decision's Verification list>
<one id per line; id at line-start, em-dash, full text on the same line;
 or `(none — structural phase)`>

## Files to touch
- sites/<path>

## Dependency interface signatures
<public signatures/types of the packages this phase depends on>

## Done bar
<deterministic exit conditions: green suite + each id covered by a co-located,
 genuinely-asserting, actually-running tagged test>
```

The id lines are grep-able as the phase's denominator:
`grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md`.

**Feedback region (verify-owned; gather seeds it empty, build reads it, verify
overwrites it):**

```
## Verify feedback — attempt N
build commit: <sha>
stall streak: <n>
- R-XXXX-XXXX — <exact failing command> → <observed output> [file:line]
```

gather writes this region **empty** (`## Verify feedback — attempt 0` /
`(none yet)`); verify **overwrites** it (never appends) with only the currently
open gaps each gap cycle; build reads but never writes it.
