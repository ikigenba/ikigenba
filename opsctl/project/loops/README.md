# opsctl — build loop

The installed unattended build loop for this tree: a `gather → build → verify`
cycle that `ralph` re-invokes with a **fresh context** every turn, building the
plan one pending phase at a time. This file describes the loop **as installed**,
beside the prompts it describes. The spec shapes it consumes
(`project/product/`, `project/design/`, `project/plan/`) are documented in
`project/README.md`; loop mechanics live only here.

## Running it

```
cd opsctl
./project/loops/run
```

`run` is the executable operator wrapper. Its whole body is:

```sh
exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the working directory it is launched in — the **service root**
(`opsctl/`) — so every workspace path in the prompts is service-root-relative
(`project/…`). The spec sometimes writes paths with an `opsctl/` prefix
(`opsctl/AGENTS.md`, `opsctl/project/opsctl-verification.md`); that is the
repo-root naming convention used for readability. `gather` translates such paths
into their service-root-relative form when it writes the brief, and every
done-bar command in the loop is written to be correct from `opsctl/`.

**Environmental precondition:** a real `tar` binary on `PATH`, on top of the Go
toolchain. Per `root project/design/D23.md` its absence is a hard failure, never
a skip — the loop will report a gap rather than pass.

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
green suite and all gaps closed, is still `NEXT`; only `gather` ever ends the
run. `CONTINUE` exists because a streaming backend coerces every streamed
message into the schema, so a model narrating progress needs a non-terminal
value to tag those with.

The two `DONE` conditions are the only ends of the loop (plus ralph's own budget
rails): `project/loops/blocked.md` exists, or the `⬜` grep over
`project/plan/STATUS.md` finds no pending phase.

## What each step reads, writes, and commits

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| **gather** | `project/plan/STATUS.md`; the one `project/plan/phase-NN.md`; `project/design/INDEX.md`; only the named `DNN.md`; dependency interfaces | `project/loops/brief.md` (contract region only, and only for a fresh phase) | nothing | nothing |
| **build** | `project/loops/brief.md` only | source + package-local `*_test.go` | this turn's increment | nothing |
| **verify** | `project/loops/brief.md`; the suite; mechanical id-set greps | `project/loops/brief.md` feedback region, or `project/loops/blocked.md` | the phase-retirement deletion | the phase's `STATUS.md` line + `phase-NN.md` + the brief, on a pass |

The next unit of work is found with:

```
grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1
```

Phase lines are Markdown bullets; the `Next phase: NN` counter line is not a
bullet and never matches. There is no done marker — a completed phase's line and
body file are **deleted**.

## The green gate and what "covered" means

From the service root:

- `GOWORK=off go build ./...` exits 0.
- `GOWORK=off go test ./...` exits 0, no failures, **no `SKIP`**.
- `gofmt -l .` prints nothing.

A Verification id counts as **covered** only when it is named in a
`// R-XXXX-XXXX` comment above a test that genuinely asserts the behavior **and
that test actually runs under the real invocation**. Reachability is part of
coverage: a tagged test held out of the run by a build tag, env flag, or skip
condition that nothing in the repo satisfies is **uncovered**, however genuine
its assertion reads — as is a test that converts a real failure signal into a
skip. A skip is never acceptable green for a requirement test.

Tests are **co-located with the code they exercise and named for the behavior**
(`internal/opsctl/*_test.go`). There is no per-phase and no root-level test
file, and the loop's prompts forbid creating one.

## The manual layer and the coverage ratchet

`verify` runs a global ratchet every cycle so a rewrite cannot silently drop a
previously-covered id:

```
design ids (project/design/D*.md, minus the R-XXXX-XXXX placeholder)
  − ( tagged-test ids ∪ pending-phase ids ∪ the documented manual-layer ids )
  = must be empty
```

Two details are load-bearing:

- The design docs write `R-XXXX-XXXX` as the *shape* of an id, so the design
  side pipes through `grep -v 'R-XXXX-XXXX'`; without that filter the
  placeholder surfaces as a phantom uncovered id and the ratchet can never go
  green.
- opsctl's **manual layer** is subtracted explicitly. Per D17 and
  `root project/design/D23.md`, eight ids are live-box checks automation cannot
  reach even with credentials, proven by the committed runbook
  `project/opsctl-verification.md` rather than by a tagged test:
  `R-WRJF-H7J9`, `R-66UP-LI59`, `R-6FE0-9WC4`, `R-MYS7-2H2R`, `R-AXY7-K8GA`,
  `R-B0E0-BRXO`, `R-JRO8-5Q0R`, `R-MMF1-HFMO`. Their absence from `*_test.go` is
  the permanent expected state, not a regression. The set is stated literally in
  `verify.md` so the check stays deterministic; a ninth real-substrate id is
  caught instead by `R-2B4O-Z98N`'s doc-truth test, and widening the set to
  silence a gap is forbidden.

This tree has exactly two testing layers — **hermetic** and **manual**. There is
no composed and no live layer: no `//go:build live` file, no `-tags live`
invocation, and `t.Skip` and its variants appear nowhere.

## The brief lifecycle

`project/loops/brief.md` is the seam that keeps `build`'s context scoped to one
phase. It is **never committed** (git-ignored), **single-phase**, and
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
<paths, service-root-relative>

## Dependency interfaces
<copied seam/type signatures>

## Done bar
<green gate + per-id coverage + the phase's own structural checks>

## Verify feedback — attempt N
- build commit observed: <sha>
- stall streak: <k>
- open gaps:
  - R-XXXX-XXXX — <exact failing command> → <observed output> [file:line]
```

Each `## Ids to cover` line starts at column 0 with the bare id, an em-dash,
then the full requirement text on the same line, so the phase's id set is
extractable with
`grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md`.

## Why it converges, and the stall/blocked ladder

`verify` can neither halt the run nor advance a phase on a gap, so an incomplete
phase simply stays `⬜` and the loop re-attacks it next cycle — now with
`verify`'s grounded, command-level feedback in front of `build`, and without
`gather` re-reading the big docs. The persisted feedback also gives `verify`
cross-cycle memory, letting it distinguish slow convergence (the open-gap id set
shrinking) from a true stall.

- **Progress** = the current open-gap id set is a strict subset of the previous
  one. **A new build commit is never progress** and never resets the streak — a
  builder that cannot satisfy a bar keeps committing plausible rewordings, and a
  detector keyed on commit motion would read that churn as convergence.
- **Stall reset (streak = 3):** three consecutive attempts closing no gap means
  the accumulated brief may not be converging. `verify` logs
  `Phase NN STALLED …` to `~/.ralph/verify.log`, deletes the brief, leaves `⬜`,
  and returns `NEXT`; the next `gather` rebuilds the contract fresh from spec.
- **Blocked escalation (second stall on the same phase):** a rebuilt contract
  has already been tried, so the **done bar** is the fault and no amount of
  rebuilding fixes it. `verify` writes `project/loops/blocked.md` with the
  phase, the attempt count, the unsatisfied ids, and the exact command and
  observed output that will not go green; logs `Phase NN BLOCKED …`; deletes the
  brief; leaves `⬜`; and returns `NEXT`. The next `gather` sees the file and
  reports `DONE`.

Both branches stay inside the invariant that `verify` never halts and never
advances on a gap.

**What the operator does with a `blocked.md`:** read the recorded command and
its observed output, fix the phase's done bar in `project/` (the loop cannot —
`project/` is read-only to it), delete `project/loops/blocked.md`, and restart
the loop.
