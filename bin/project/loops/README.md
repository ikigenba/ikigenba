# bin — build loop

The unattended `gather → build → verify` loop that builds `bin/project/plan/`'s
pending phases one at a time. Start it (**from the repo root**, not from
`bin/`) with:

```
bin/project/loops/run
```

which wraps exactly:

```
ralph bin/project/loops/gather.md bin/project/loops/build.md bin/project/loops/verify.md
```

`bin`'s toolchain commands (`go build ./bin/bintest/...`, `go test
./bin/bintest/...`) are repo-root relative — `bin/bintest` has no module
boundary of its own, it rides the root `go.work` — so unlike a normal
subproject loop, this one's working directory is the **repo root**, and every
path the three prompts reference is prefixed `bin/` accordingly.

## Status contract

Every turn ends with one of three statuses. `ralph` reads only the **last**
message of the turn and advances on that.

| status | terminal? | meaning |
|---|---|---|
| `CONTINUE` | no | a progress message streamed before the turn's real end; never advances the loop |
| `NEXT` | yes | this turn's work is done; hand off to the next prompt (wraps `verify → gather`) |
| `DONE` | yes | the whole job is complete; the loop stops — **only `gather` ever reports this** |

## Per-step reads / writes / commits

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| `gather` | `bin/project/plan/STATUS.md`; (fresh brief only) one `phase-NN.md`, `INDEX.md`, the realized `DNN.md`(s) | `bin/project/loops/brief.md` (fresh brief case only; leaves an in-flight brief untouched) | — | — |
| `build` | `bin/project/loops/brief.md` only | `bin/` scripts, `bin/bintest/*_test.go` | this turn's increment | — |
| `verify` | `bin/project/loops/brief.md`; runs the suite | `bin/project/loops/brief.md` feedback region (gap case); `~/.ralph/verify.log` (stall/blocked) | the phase's `STATUS.md` line + `phase-NN.md` deletion (pass case) | pass case: `brief.md`; stall/blocked: `brief.md` |

## The brief lifecycle

`gather` authors `bin/project/loops/brief.md` once, the first time a phase
becomes the active `⬜` phase — a self-contained contract carrying the
realized Decision's design prose (Verification list excluded), the phase's
own slice of ids with their full requirement text, files to touch, and
dependency interfaces. While that phase stays `⬜`, `gather` no-ops on every
later cycle (it never re-reads `bin/project/design/` or
`bin/project/plan/phase-NN.md` for a phase already in flight). `build` reads
the brief and does bounded work against it, prioritizing any open gaps in the
feedback region. `verify` re-derives the truth from scratch every cycle: pass
deletes the phase's `STATUS.md` line, its `phase-NN.md`, and the brief; a gap
leaves `⬜` untouched and overwrites the feedback region with only the
currently open gaps.

## Stall and blocked ladder

`verify` tracks progress by comparing this cycle's open-gap id set against
the prior cycle's, using its own persisted feedback region — not `build`'s
claims. Three consecutive attempts that close no gap trigger a **trajectory
reset**: the brief is discarded (phase stays `⬜`), so the next `gather`
rebuilds the contract fresh from spec. If the *same* phase stalls a second
time after a reset, that is not a stuck trajectory but a defective bar —
`verify` writes `bin/project/loops/blocked.md` naming the phase, the attempts,
the unsatisfied ids, and the exact command/output that will not go green, and
the next `gather` reports `DONE` on sight of that file.

**Operator recovery from a `blocked.md`:** read the recorded command and
output, fix the phase's done bar in `bin/project/plan/` or
`bin/project/design/` (the loop treats `bin/project/` as read-only and cannot
fix it itself), delete `bin/project/loops/blocked.md`, and restart
`bin/project/loops/run`.

## Why this converges

`verify` can neither halt the loop nor advance a phase on a gap — an
incomplete phase just stays `⬜` and gets re-attacked next cycle, now with
`verify`'s grounded feedback in front of `build`. The loop's only two exits
are both `gather`-only: zero `⬜` phases left (everything verified green), or
a blocked phase awaiting the operator — plus `ralph`'s own budget rails.

## `bin/project/loops/brief.md` schema

Two regions, each owned by exactly one writer:

- **Contract region** (`gather`-owned, written once per phase): phase id +
  objective, realized Decision id(s) and file(s), the realized Decision's full
  design prose (Verification list omitted), the ids to cover — one per line as
  `R-XXXX-XXXX — <full requirement text>` (or `(none — structural phase)`),
  files to touch, dependency interface signatures, and the done bar.
- **Verify feedback region** (`verify`-owned): a `## Verify feedback — attempt
  N` heading, the build commit `verify` last observed, the stall streak, and a
  checklist of only the currently open gaps, each tied to an `R-id` and the
  exact failing command/output. Empty on a fresh brief; overwritten (never
  appended) on every gap cycle; read but never written by `build`.

See `bin/project/plan/README.md` and `bin/project/design/README.md` for the
spec shapes this loop consumes — this file documents only the loop mechanics,
never restates those.
