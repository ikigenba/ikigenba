# registry — the installed build loop

This directory holds the unattended `gather → build → verify` loop that builds
the `registry` module one **pending phase** at a time from the spec in
`project/design/` and `project/plan/`. This README describes the loop **as
installed here**; it is kept beside the prompts so it can never describe a
different loop than the one on disk. `project/README.md` (the workspace map)
only points here — the spec shapes themselves live in the `$ikispec` skill,
never in this file.

## Running it

```
project/loops/run
```

The wrapper is executable and contains exactly:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the **module root** (`registry/`), so every workspace path the
prompts reference is module-root-relative (`project/…`).

## The status contract

Each turn ends with a `status` and a one-sentence `message`. The harness supplies
that schema out of band and reads it back itself (codex via `--output-schema`,
claude via `--json-schema`), so no prompt hard-codes a transport.

| status | terminal? | meaning |
|---|---|---|
| `CONTINUE` | no | a progress message streamed *before* the turn's final message; never advances the loop |
| `NEXT` | yes | this turn's work is done; advance to the next prompt (wrapping `verify → gather`) |
| `DONE` | yes | the whole job is complete; the loop stops |

`ralph` reads only the **last** message of a turn. `DONE` is **gather's alone** —
`build` and `verify` always end on `NEXT`, even when a phase is finished, green,
and every gap is closed. The run ends only when `gather` reports `DONE`, on one
of exactly two conditions: `project/loops/blocked.md` exists, or the `⬜` grep
finds no pending phase. (ralph's own budget rails are the other stop.)

## What each step reads, writes, and commits

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| **gather** | `project/plan/STATUS.md`, the one `phase-NN.md`, the realized `DNN.md` (via `INDEX.md`) | the brief's **contract region** (fresh phases only) | nothing | nothing |
| **build** | `project/loops/brief.md` only — contract *and* feedback regions | `registry/*.go` + package-local id-tagged tests | this turn's increment | nothing |
| **verify** | the brief; the tree's real state (build, suite, greps) | the brief's **feedback region**, or `blocked.md` | the phase deletion, on a pass | on a pass: the phase's `STATUS.md` line, its `phase-NN.md`, and the brief |

`gather` is the only step that opens the big docs. `build` never does. `verify`
re-derives current truth from scratch and never trusts `build`'s claims — its
mechanical id-set greps over `project/design/D*.md` and
`project/plan/phase-*.md` extract id tokens only, which is not "reading" design
prose.

## The brief's lifecycle

`project/loops/brief.md` is the seam that keeps `build`'s context scoped to one
phase. It is **never committed** (git-ignored) and describes exactly one phase at
a time.

1. `gather` authors it **once**, when a phase first becomes the active `⬜`
   phase, with an empty feedback region.
2. While that phase stays `⬜`, `gather` **no-ops** on it — it reads only the
   `# Brief — Phase NN` header, sees the same phase, opens no big doc, and
   returns `NEXT`. The contract and any accumulated feedback survive.
3. `build` reads the whole brief each turn and closes the feedback region's open
   gaps first. It never writes the brief.
4. `verify` either **passes** the phase (deletes its `STATUS.md` line and body
   file, commits, and deletes the brief) or records **only the currently-open
   gaps** by overwriting the feedback region — never appending, which would
   duplicate on a re-run and stack stale gaps.

## The brief schema

Two single-writer regions, split by a hard marker line.

```
# Brief — Phase NN: <one-line objective>

## Contract
- Phase: NN
- Realizes: D<N> (<short label>)
- Decision files: project/design/DNN.md
- Design prose (verbatim, Verification lists omitted):
  <Decision statement + shape/signatures + Rejected, copied from each DNN.md>
- Ids to cover:
R-XXXX-XXXX — <full requirement text copied verbatim, Substrate: clause included>
  (or the literal line: (none — structural phase))
- Files to touch:
  - registry/<file>.go
  - registry/registry_test.go
- Done bar:
  - `GOWORK=off go build ./...` exits 0
  - `GOWORK=off go test ./...` exits 0 (no failures, no SKIP)
  - every id is covered by a genuinely-asserting, actually-running tagged test
    in package-local `registry/*_test.go`
  - (structural phase) the phase's Done-when smoke commands pass

<!-- VERIFY FEEDBACK BELOW — verify owns everything past this line; gather writes this marker once, leaves the stub, and never touches this region again. -->

## Verify feedback
(none yet)
```

The id lines stay grep-able as the phase's denominator:
`grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md` extracts just the
id per line and never miscounts an id quoted in prose elsewhere in the file.

`verify`'s region, when it writes one:

```
## Verify feedback — attempt N
- build commit observed: <sha>
- stall streak: <k>
- open gaps:
  - R-XXXX-XXXX — <exact failing command> → <observed output> [file:line]
```

## The green gate and the coverage bars

From `registry/`, the suite is green when `GOWORK=off go build ./...` exits 0 and
`GOWORK=off go test ./...` exits 0 with no failures and no `SKIP`. `GOWORK=off`
is forced deliberately: it matches the deterministic production build and proves
the module resolves standalone with zero third-party dependencies.

An id counts as **covered** only when it is named in a `// R-XXXX-XXXX` comment
on a test that genuinely asserts the behavior, in a package-local
`registry/*_test.go` file named for the behavior (there is no separate
integration-test home and no per-phase or root-level test files), **and that test
actually runs under `GOWORK=off go test ./...`**.

Per the suite testing contract (`root project/design/D23.md`), **every test in
this tree is hermetic**: the package is pure compile-time data and total
functions, so registry builds no binary and has no composed layer, no live layer,
no manual runbook, and no environmental precondition beyond the Go toolchain.
There is therefore no build-tag carve-out here — a test held out of the default
gate by a build tag, an env flag, or a skip condition is unreachable and scores
**uncovered**, and `t.Skip`/`t.Skipf`/`t.SkipNow` may not appear in any test file
in this tree at all. `verify` enforces both with a source scan and the no-`SKIP`
gate.

`verify` also runs a **global coverage ratchet** each cycle — design ids minus
(tagged-test ids ∪ pending-phase ids) must be empty — so a rewrite that silently
drops a previously-covered id's tag is caught immediately. The ratchet filters
the literal `R-XXXX-XXXX` placeholder out of every id set: the docs use it to
describe the id *shape*, it matches the id regex, and left in it would be a
phantom id no test can ever carry, making the ratchet permanently red.

## The stall and blocked ladder

`verify` can neither halt the run nor advance a phase with an open gap, so an
incomplete phase simply stays `⬜` and is re-attacked next cycle — now with
grounded feedback in front of `build`. Its persisted feedback also gives it
cross-cycle memory, so it can tell slow convergence from a true stall.

- **Progress** = the current open-gap id set is a **strict subset** of the prior
  one. A new build commit is explicitly *not* progress and never resets the
  streak — a builder that cannot satisfy a bar keeps committing plausible
  rewordings, and a detector keyed on commit motion would read that churn as
  convergence and never trip.
- **Stall reset (streak reaches 3).** The accumulated brief may not be
  converging: `verify` logs `Phase NN STALLED …` to `~/.ralph/verify.log`,
  deletes the brief, leaves `⬜`, and returns `NEXT`. The next `gather` rebuilds
  the contract fresh from spec.
- **Blocked escalation (a second stall on the same phase).** A rebuilt contract
  was already tried and did not help, so the **done bar itself** is the prime
  suspect and no further rebuilding can fix it. `verify` writes
  `project/loops/blocked.md` with the phase, the attempt count, the
  still-unsatisfied ids, and the exact command and observed output that will not
  go green; logs `Phase NN BLOCKED …`; deletes the brief; leaves `⬜`; returns
  `NEXT`. The next `gather` sees the file and reports `DONE`.

**When you find a `blocked.md`:** read the recorded command and its observed
output, fix the phase's done bar (or the Decision behind it) in `project/` —
which is read-only to the loop, so only you can — delete
`project/loops/blocked.md`, and restart the loop.

## Why it converges

`verify` is the only step that can retire a phase, and it retires one only on a
green build and suite plus full, reachable, genuinely-asserting coverage plus a
clean ratchet. Every other outcome leaves the phase `⬜` with a localized,
mechanically-satisfiable target written down for the next `build`. `gather` stops
re-reading the big docs once a phase is in flight, so the cycle cost stays flat.
Every done bar in the plan is a deterministic, reachable predicate (checked
against the real tree when this loop was generated), so a phase that cannot go
green is a defective bar rather than an infinite loop — and the ladder above
turns that into a written diagnosis after a handful of attempts.

Both `project/loops/brief.md` and `project/loops/blocked.md` are listed in
`registry/.gitignore`.
