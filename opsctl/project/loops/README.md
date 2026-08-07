# opsctl — build loop

The installed `gather → build → verify` loop that drives opsctl's `project/`
spec to green, one phase at a time, with a fresh context every turn. This file
describes exactly the loop on disk beside it — if you change `gather.md`,
`build.md`, or `verify.md`, keep this description in sync (or regenerate all
four together with the `create-gather-build-verify-prompts` skill).

## Running it

```sh
project/loops/run
```

which wraps, and is exactly:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` re-invokes the three prompts in that order, feeding each a **fresh
context** every turn, and cycles `verify → gather` until the loop reports
`DONE`. It runs from the service root (`opsctl/`), so every path the prompts
reference is `project/…`-relative to that root.

## The status contract

Each turn's **final** message carries a `status`:

- `NEXT` — **terminal.** Hand off to the next prompt in sequence. `build` and
  `verify` always end this way, on both a pass and a gap — neither of them
  ever stops the run.
- `DONE` — **terminal, gather-only.** The whole job is complete: either no
  `⬜` phase remains in `project/plan/STATUS.md`, or `project/loops/blocked.md`
  exists (a phase's done bar could not be satisfied and is waiting on the
  operator).
- `CONTINUE` — **non-terminal.** The status a streaming model tags its
  progress messages with, before its terminal message. `ralph` reads only the
  turn's last message, so `CONTINUE` never advances the loop on its own.

## Per-step reads / writes / commits / deletions

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| `gather` | `project/loops/blocked.md` (existence); `project/plan/STATUS.md`; on a fresh brief, one `phase-NN.md` + its Decision `DNN.md`(s) via `INDEX.md` | `project/loops/brief.md` (contract region only) | never | never |
| `build` | `project/loops/brief.md` only | source under `internal/opsctl/`, `cmd/opsctl/` | one increment per turn | never |
| `verify` | `project/loops/brief.md`; runs the suite; greps `project/design/D*.md`, `project/plan/phase-*.md`, `project/opsctl-verification.md` for id sets | `project/loops/brief.md` (feedback region, on a gap); `project/loops/blocked.md` (on a second stall) | the phase-retirement deletion commit, on a pass | `project/plan/phase-NN.md` + its `STATUS.md` line (pass); `project/loops/brief.md` (pass or stall reset) |

## The brief's lifecycle

`project/loops/brief.md` is the seam that keeps `build`'s context scoped to one
phase; it is **never committed** (`.gitignore` at the repo root already covers
`*/project/loops/brief.md` and `*/project/loops/blocked.md`). It has two
independently-owned regions:

- **`## Contract`** (gather-owned) — the phase id, the realized Decision(s)'
  full design prose (Verification list excluded), the ids to cover with their
  full requirement text, files to touch, dependency interface signatures, and
  the done bar. Authored once when the phase first becomes the active `⬜`
  phase; left untouched while that phase stays in flight — `gather` no-ops on
  a brief that already names the current phase, so the big docs are read at
  most once per phase.
- **`## Verify feedback`** (verify-owned) — the currently-open gaps from the
  last `verify` run, an attempt counter, the observed build commit, and a
  stall streak. `gather` writes this empty on a fresh brief and never touches
  it again; `build` reads it but never writes it; `verify` overwrites it whole
  each gap cycle (never appends).

The brief is deleted only by `verify`: on a pass (the phase retired) or on a
stall reset (see below).

## The stall and blocked ladder

`verify` measures progress across cycles by comparing the current open-gap id
set to the prior one recorded in `## Verify feedback`. Progress is a **strict
subset** — some previously-open gap closed. A new build commit with the same
gaps still open is **not** progress.

- **Three consecutive no-progress attempts** → a **trajectory reset**: `verify`
  logs the stall to `~/.ralph/verify.log`, deletes the brief, and leaves the
  phase `⬜`. The next `gather` rebuilds the contract fresh from spec — a
  clean-slate retry, not a halt.
- **A second stall on the same phase** (found via `~/.ralph/verify.log`) means
  a rebuilt contract already failed to converge once — the phase's *done bar*
  is the likely fault, not the brief. `verify` writes `project/loops/blocked.md`
  naming the phase, the attempt count, the still-open ids, and the exact
  failing command/output, then deletes the brief and leaves the phase `⬜`.
  The next `gather` sees `blocked.md` and reports `DONE`, stopping the loop.

**Operator recovery from a `blocked.md`:** read the recorded command and
output, fix the phase's done bar or the underlying Decision in `project/`
(through `$open-spec` → `$grill-me` → `$seal-spec`, never a direct edit),
delete `project/loops/blocked.md`, and restart `project/loops/run`.

## Why this converges

`verify` can neither halt the run nor advance a phase on a gap — an
incomplete phase just stays `⬜` and is re-attacked next cycle, now with
`verify`'s grounded feedback in front of `build`. `gather`'s no-op on an
in-flight brief means the big docs are read once per phase, not once per
cycle. The stall/blocked ladder bounds how long a truly stuck phase can spin
before it surfaces to the operator as a concrete diagnosis instead of an
infinite loop. The only ways out are `gather` reporting `DONE` — a genuinely
empty `⬜` queue, or a `blocked.md` awaiting the operator — plus `ralph`'s own
budget rails.

## opsctl's coverage ratchet, particulars

opsctl's design mints eight ids whose correctness depends on the real box (a
genuine cross-device filesystem, `/etc/ikigenba/env`, a real `nginx`/cert, a
real package manager) — a faked `System`/filesystem cannot falsify them. These
are **documented, out-of-loop, real-substrate checks**
(`project/opsctl-verification.md`), never scheduled as loop-gating phase work,
and their permanent absence from any `*_test.go` tag is the **expected**
state, not a regression. `verify`'s global coverage ratchet reads that
documented set directly off `project/opsctl-verification.md`'s
`### D<n> — \`R-id\`` check headers and folds it into the accounted-for side
of the ratchet, alongside tagged-test ids and pending-phase ids — so the eight
never register as an open gap, while a *ninth* id silently losing its test
still would.

## `project/loops/brief.md` schema

```
# Brief — Phase NN: <one-line objective>

## Contract
- Phase: NN
- Realizes: D<N> (<short label>)[, D<M> ...]
- Decision files: project/design/DNN.md[, project/design/DMM.md]

### Design prose (verbatim, Verification list excluded)
<Decision./Rejected. prose of each realized DNN.md>

- Ids to cover:
R-XXXX-XXXX — <full requirement text, verbatim>
  (or: "(none — structural phase)")
- Files to touch:
  - internal/opsctl/<file>.go
  - internal/opsctl/<file>_test.go
- Dependency interfaces (verbatim signatures)
- Done bar: build/test commands + coverage rule

## Verify feedback
(none yet | ## Verify feedback — attempt N with build commit, stall streak,
and a checklist of only the currently-open gaps)
```

See `project/README.md` for the workspace map and `$ikispec` for the `project/`
spec contracts this loop builds toward.
