# prompts — the installed build loop

This folder holds the `gather → build → verify` build loop as installed for
`prompts/`, plus this overview. It lives beside the prompts it describes so it
can never document a different loop than the one on disk.

`project/README.md` (the workspace map) points here; the loop's mechanics live
here and nowhere else. The spec shapes themselves — product, research, design,
plan — belong to the `$ikispec` contracts and are not restated here.

## Running it

```
project/loops/run
```

The wrapper is executable and contains exactly:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the **service root** (`prompts/`), so every workspace path the
prompts reference is service-root-relative.

## The status contract

Each turn ends with a `status` and a one-sentence `message`. The harness supplies
the `{status, message}` schema out of band and reads it back itself, so no prompt
hard-codes a transport.

| status | terminal? | meaning |
|---|---|---|
| `CONTINUE` | no | a progress message streamed *before* the turn's final message; never advances the loop |
| `NEXT` | yes | this turn's work is done; advance to the next prompt (wrapping `verify → gather`) |
| `DONE` | yes | the whole job is complete; the loop stops |

`ralph` reads only the **last** message of a turn. `CONTINUE` exists because a
streaming backend coerces every streamed message into the schema, so a model that
narrates progress needs a non-terminal value for those messages.

**Only `gather` ever reports `DONE`** — on finding no `⬜` phase left, or on
finding `project/loops/blocked.md`. `build` and `verify` always end on `NEXT`,
including the turn that finishes a phase completely.

## What each step reads, writes, and commits

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| **gather** | `project/plan/STATUS.md`; one `project/plan/phase-NN.md`; `project/design/INDEX.md`; only the realized `DNN.md`; dependency exported signatures | the brief's **contract region** (fresh briefs only) | nothing | nothing |
| **build** | `project/loops/brief.md` only — contract *and* feedback regions | source and id-tagged tests | this turn's increment | nothing |
| **verify** | the brief; the real suite; mechanical id-set greps | the brief's **feedback region**; `project/loops/blocked.md` on escalation | the phase-retirement deletion | on pass: the `STATUS.md` phase line, `project/plan/phase-NN.md`, and the brief |

`build` never opens the big docs. `verify` never opens them to re-derive its
checklist — the brief is the checklist; the ratchet's id-set greps extract
tokens, not prose.

## The brief lifecycle

`project/loops/brief.md` is the seam that keeps `build`'s context scoped to one
phase. It is **never committed** (the repo-root `.gitignore` covers
`*/project/loops/brief.md`), describes **one phase at a time**, and is
**phase-scoped, not per-cycle**:

- `gather` authors it once, when a phase first becomes the active `⬜` phase.
- While that phase stays `⬜`, `gather` **no-ops on it** — it reads the
  `# Brief — Phase NN` header, sees the same phase, and leaves both regions
  untouched without opening a big doc.
- `build` consumes it every cycle and writes to neither region.
- `verify` deletes it on a pass, or overwrites its feedback region with the
  currently-open gaps.

It is **region-owned by a single writer each**, so the two writers never clobber
each other: `gather` owns the contract region, `verify` owns the feedback region.

## The brief schema

```markdown
# Brief — Phase NN

**Objective:** <one-line objective>
**Realizes:** D<n>[, D<m>]
**Decision files:** project/design/DNN.md[, …]

## Design prose — D<n>

<full design prose of that Decision copied verbatim from its DNN.md — Decision
statement, shape/signatures, Rejected alternatives — with that Decision's
Verification list OMITTED>

## Ids to cover

R-XXXX-XXXX — <full requirement text copied verbatim, on this same line>

## Files to touch

- <path> — <what lands there>

## Dependency interfaces

<exported signatures copied in, so build never opens a design file>

## Done bar

<the phase's deterministic exit conditions, including the test-placement rule>

## Verify feedback

_(none yet)_
```

The **contract region** is everything above `## Verify feedback`. Ids are one per
line, `R-XXXX-XXXX — <text>`, id at line-start, so the denominator stays
grep-able:

```
grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md
```

A phase owning no ids carries the single line `(none — structural phase)`.

The **feedback region** is a single `## Verify feedback — attempt N` heading
carrying the attempt counter, the build commit `verify` observed, the stall
streak, and a checklist of **only** the currently-open gaps — each line tied to
one `R-id` and grounded in the exact failing command and its observed output.
`verify` **overwrites** it (an append would duplicate on a re-run and stack stale
gaps); `build` reads it but never writes it.

## The stall and blocked ladder

`verify` measures progress across cycles from the feedback region it persisted
last time. *Progress* means the current open-gap id set is a **strict subset** of
the prior one. A new build commit is deliberately **not** progress — a builder
that cannot satisfy a bar keeps committing plausible rewordings, and a detector
keyed on commit motion would read that churn as convergence and never trip.

1. **Three consecutive attempts closing no gap → trajectory reset.** The
   accumulated brief may not be converging, so `verify` logs
   `<date> Phase NN STALLED after N attempts: <gap ids>` to `~/.ralph/verify.log`,
   deletes the brief, leaves the marker `⬜`, and returns `NEXT`. The next
   `gather` rebuilds the contract fresh from spec.
2. **A second stall on the same phase → blocked.** A rebuilt contract was already
   tried, so the *bar itself* is the fault and no further rebuilding fixes it.
   `verify` writes `project/loops/blocked.md` with the phase, the total attempts,
   the still-unsatisfied ids, and the exact command and observed output that will
   not go green; logs `… BLOCKED after N attempts: <gap ids>`; deletes the brief;
   leaves `⬜`; and returns `NEXT`. The next `gather` sees the file and reports
   `DONE`.

Both stay inside the invariant that `verify` never halts and never advances a
phase on a gap.

**What the operator does with a `blocked.md`:** read the recorded command and its
observed output, fix the phase's done bar in `project/` (the loop cannot — the
spec is read-only to it), delete `project/loops/blocked.md`, and restart the
loop. Like the brief, it is never committed.

## Why the loop converges

`verify` can neither halt nor advance a phase on a gap, so an incomplete phase
just stays `⬜` and the loop re-attacks it next cycle — now with `verify`'s
grounded, command-level feedback in front of `build`, and without `gather`
re-reading the big docs (it no-ops on the in-flight brief). The persisted
feedback also gives `verify` cross-cycle memory, so it can tell slow convergence
(a shrinking gap set) from a true stall, and the ladder above turns a defective
bar into a written diagnosis after a handful of attempts instead of an endless
spin.

The only exits are `gather → DONE`: zero `⬜` markers left, or a blocked phase
awaiting the operator — plus ralph's own budget rails.

## The green gate this loop enforces

From `prompts/`, all four must succeed:

```
go build ./...
go vet ./...
gofmt -l .        # must print nothing
go test ./...     # zero failures
```

Requirement-id tags live in `*_test.go`. An id counts as **covered** only when a
`// R-XXXX-XXXX` comment sits on a test that genuinely asserts the behavior
**and** that test actually runs under `go test ./...`. prompts has **no live
layer**, so any build tag, env gate, or skip condition standing between the suite
and a tagged test makes it unreachable and therefore uncovered — there is no
carve-out, and a skip is never acceptable green.
