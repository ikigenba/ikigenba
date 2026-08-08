# bin — build loop

The installed unattended build loop for this tree: a `gather → build → verify`
cycle that `ralph` re-invokes with a **fresh context** every turn, building the
plan one pending phase at a time. This file describes the loop **as installed**,
beside the prompts it describes. The spec shapes it consumes
(`bin/project/product/`, `bin/project/design/`, `bin/project/plan/`) are
documented in `bin/project/README.md`; loop mechanics live only here.

## Running it

```
cd <repo root>
./bin/project/loops/run
```

`run` is the executable operator wrapper. Its body is:

```sh
exec ralph bin/project/loops/gather.md bin/project/loops/build.md bin/project/loops/verify.md
```

**Run it from the repo root, not from `bin/`.** This tree has no module root of
its own — its Go test package `bin/bintest` rides the repo-root `go.work` — so
its toolchain commands (`go build ./bin/bintest/...`,
`go test ./bin/bintest/...`) and every workspace path in the prompts are
**repo-root-relative**. The wrapper passes the prompt paths in that same form,
so `ralph`'s working directory is the repo root throughout.

**Environmental preconditions:** none beyond the Go toolchain. **GOWORK mode:
workspace** — never `GOWORK=off`, which would break D5 and D6 by construction.

## The status contract

Each turn ends by reporting a `status` and a one-sentence `message`. The harness
supplies the schema out of band and reads back only the turn's **final**
message.

| status | terminal? | who reports it |
|---|---|---|
| `CONTINUE` | no | any prompt, on progress messages streamed *before* the final message. Never advances the loop. |
| `NEXT` | yes | any prompt — advance to the next prompt, wrapping `verify → gather`. |
| `DONE` | yes | **`gather` only** — the loop stops. |

`build` and `verify` **always** end on `NEXT`. Finishing a phase completely,
green gate and all gaps closed, is still `NEXT`; only `gather` ever ends the
run. `CONTINUE` exists because a streaming backend coerces every streamed
message into the schema, so a model narrating progress needs a non-terminal
value to tag those with.

The two `DONE` conditions are the only ends of the loop (plus ralph's own budget
rails): `bin/project/loops/blocked.md` exists, or the `⬜` grep over
`bin/project/plan/STATUS.md` finds no pending phase.

## What each step reads, writes, and commits

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| **gather** | `bin/project/plan/STATUS.md`; the one `bin/project/plan/phase-NN.md`; `bin/project/design/INDEX.md`; only the named `DNN.md`; dependency interfaces | `bin/project/loops/brief.md` (contract region only, and only for a fresh phase) | nothing | nothing |
| **build** | `bin/project/loops/brief.md` only | scripts under `bin/` + tests in `bin/bintest/*_test.go` | this turn's increment | nothing |
| **verify** | `bin/project/loops/brief.md`; the gate; mechanical id-set greps | `bin/project/loops/brief.md` feedback region, or `bin/project/loops/blocked.md` | the phase-retirement deletion | the phase's `STATUS.md` line + `phase-NN.md` + the brief, on a pass |

The next unit of work is found with:

```
grep -nE '^- Phase .* ⬜' bin/project/plan/STATUS.md | head -1
```

Phase lines are Markdown bullets; the `Next phase: NN` counter line is not a
bullet and never matches. There is no done marker — a completed phase's line and
body file are **deleted**.

## The green gate and what "covered" means

From the repo root:

- `go build ./bin/bintest/...` exits 0.
- `go test ./bin/bintest/...` exits 0, no failures, **no `SKIP`**. **This tree is
  green when that command exits 0.** The same tests also run under the repo-wide
  `go test ./...`, so this tree's green is a subset of the suite's.
- `gofmt -l bin/bintest` prints nothing; `bash -n bin/<script>` exits 0 for any
  script touched.

A Verification id counts as **covered** only when it is named in a
`// R-XXXX-XXXX` comment above a test that genuinely asserts the behavior **and
that test actually runs under the real invocation**. Reachability is part of
coverage: a tagged test held out of the run by a build tag, env flag, or skip
condition that nothing in the repo satisfies is **uncovered**, however genuine
its assertion reads — as is a test that converts a real failure signal into a
skip. A skip is never acceptable green for a requirement test. A test whose
claim is about a script must exec the **real script**; a Go reimplementation of
its logic proves nothing about it.

Tests live in `bin/bintest/*_test.go`, **named for the script and behavior they
exercise**. `bin/` itself carries no tests, and `bin/bintest` is the single
designated home for all of them, including the cross-cutting module-graph
checks. There is no per-phase and no root-level test file, and the loop's
prompts forbid creating one.

## Two tiers, and the coverage ratchet

`bin/` is bash orchestration — it builds, copies, launches, and calls remote
APIs, none of which a hermetic test can stand in for faithfully. That tier is
**deliberately untested** and verified once, manually, outside the loop; it is
the **manual** layer and mints **no ids**. The exceptions are the layout readers
(D5) and the repo-wide library-dependency conformance checks (D6), which are
covered automatically in `bin/bintest`. A phase realizing an untested-tier
Decision carries deterministic **structural** exit conditions instead — an exact
named file, a `project/`-excluded grep with an exact match count, a clean
workspace build.

`verify` runs a global ratchet every cycle so a rewrite cannot silently drop a
previously-covered id:

```
design ids (bin/project/design/D*.md, minus the R-XXXX-XXXX placeholder)
  − ( tagged-test ids in bin/bintest/*_test.go ∪ pending-phase ids )
  = must be empty
```

The `grep -v 'R-XXXX-XXXX'` filter on the design side is required: the design
docs write `R-XXXX-XXXX` as the *shape* of an id in prose, and without the
filter that placeholder surfaces as a phantom uncovered id the ratchet can never
clear. There is **no manual-layer id carve-out here** — this tree's manual layer
mints no ids, so every minted id must be covered by a tagged test, and nothing
may be subtracted from the check to make it pass.

Testing layers, per `root project/design/D23.md` (adopted by D7): every
`bin/bintest` test is **hermetic**; the bash orchestration tier is **manual**.
There is no composed and no live layer — no `//go:build live` file, no
`-tags live` invocation — and `t.Skip` and its variants appear nowhere.

## The brief lifecycle

`bin/project/loops/brief.md` is the seam that keeps `build`'s context scoped to
one phase. It is **never committed** (git-ignored), **single-phase**, and
**phase-scoped rather than per-cycle**:

1. `gather` authors it when a phase first becomes the active `⬜` phase, then
   **no-ops while that phase stays in flight** — it re-reads no big doc and
   leaves both regions untouched.
2. `build` consumes the whole brief, prioritizing any open gaps in the feedback
   region, and never writes it.
3. `verify` on a **pass** deletes the phase's line + body file and the brief; on
   a **gap** it overwrites (never appends) the feedback region with only the
   currently-open gaps, and the brief persists into the next cycle.

Region ownership keeps the two writers from clobbering each other: the contract
region is gather's, the feedback region is verify's.

### Brief schema

```markdown
# Brief — Phase NN

## Objective
<one line, from the phase file>

## Realizes
<Decision id(s), or "— (structural phase, no ids)">

## Decision file(s)
<path(s) to the DNN.md read>

## Design prose
<each realized Decision's full Decision statement, shape/signatures, and
rejected alternatives, verbatim, Verification list omitted>

## Ids to cover
R-XXXX-XXXX — <full requirement text, verbatim, on the same line>
<or: "(none — structural phase)">

## Files to touch
<paths, repo-root-relative>

## Dependency interfaces
<copied script flags/env vars, or the module-file shapes the checks read>

## Done bar
<green gate + per-id coverage + the phase's own structural checks with their
stated expected output>

## Verify feedback — attempt N
- build commit observed: <sha>
- stall streak: <k>
- open gaps:
  - R-XXXX-XXXX — <exact failing command> → <observed output> [file:line]
```

Each `## Ids to cover` line starts at column 0 with the bare id, an em-dash,
then the full requirement text on the same line, so the phase's id set is
extractable with
`grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' bin/project/loops/brief.md`.

## Why it converges, and the stall/blocked ladder

`verify` can neither halt the run nor advance a phase on a gap, so an incomplete
phase simply stays `⬜` and the loop re-attacks it next cycle — now with
`verify`'s grounded, command-level feedback in front of `build`, and without
`gather` re-reading the big docs. The persisted feedback also gives `verify`
cross-cycle memory, letting it distinguish slow convergence (the open-gap set
shrinking) from a true stall.

- **Progress** = the current open-gap set is a strict subset of the previous
  one. **A new build commit is never progress** and never resets the streak — a
  builder that cannot satisfy a bar keeps committing plausible rewordings, and a
  detector keyed on commit motion would read that churn as convergence.
- **Stall reset (streak = 3):** three consecutive attempts closing no gap means
  the accumulated brief may not be converging. `verify` logs
  `Phase NN STALLED …` to `~/.ralph/verify.log`, deletes the brief, leaves `⬜`,
  and returns `NEXT`; the next `gather` rebuilds the contract fresh from spec.
- **Blocked escalation (second stall on the same phase):** a rebuilt contract
  has already been tried, so the **done bar** is the fault and no amount of
  rebuilding fixes it. `verify` writes `bin/project/loops/blocked.md` with the
  phase, the attempt count, the unsatisfied ids or checks, and the exact command
  and observed output that will not go green; logs `Phase NN BLOCKED …`; deletes
  the brief; leaves `⬜`; and returns `NEXT`. The next `gather` sees the file and
  reports `DONE`.

Both branches stay inside the invariant that `verify` never halts and never
advances on a gap.

**What the operator does with a `blocked.md`:** read the recorded command and
its observed output, fix the phase's done bar in `bin/project/` (the loop cannot
— `project/` is read-only to it), delete `bin/project/loops/blocked.md`, and
restart the loop.
