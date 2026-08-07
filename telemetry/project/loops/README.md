# telemetry — the installed build loop

This directory holds the unattended build loop that builds `telemetry` from its
spec, plus this overview of the loop **as installed**. It lives beside the
prompts it describes so it can never describe a different loop than the one on
disk. The spec shapes themselves (`product/`, `research/`, `design/`, `plan/`)
belong to `project/README.md` and the spec docs; loop mechanics live only here.

## Running it

From the service root (`telemetry/`):

```
project/loops/run
```

The wrapper is exactly:

```sh
#!/bin/bash

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from its working directory — the service root — so every path in
the prompts is service-root-relative (`project/…`). It re-invokes each prompt in
a **fresh context**, cycling `gather → build → verify → gather → …` until the
loop ends.

## The status contract

Each turn reports a `status` and a one-sentence `message`. The harness supplies
that schema out of band (codex via `--output-schema`, claude via
`--json-schema`); the prompts describe only the contract, never a transport.

| status | kind | meaning |
|---|---|---|
| `CONTINUE` | non-terminal | a progress message streamed *before* the turn's final message; never advances the loop. A streaming backend coerces every message into the schema, so mid-turn narration needs this value. |
| `NEXT` | terminal | this turn is done; advance to the next prompt (wrapping `verify → gather`). |
| `DONE` | terminal | the whole job is complete; the loop stops. **Only `gather` ever reports it** — on finding no `⬜` phase left, or on finding `project/loops/blocked.md`. |

`ralph` reads only the **last** message of a turn, so the terminal status alone
drives the loop. `build` and `verify` always end on `NEXT` — finishing a phase
completely is still `NEXT`.

## What each step reads, writes, and commits

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| **gather** | `project/loops/blocked.md` (existence check), `plan/STATUS.md`, one `plan/phase-NN.md`, `design/INDEX.md`, the realized `design/DNN.md`, dependency interfaces | the brief's **contract region** (fresh brief only) | nothing | nothing |
| **build** | `project/loops/brief.md` only | source + tests under `telemetry/` | this turn's increment | nothing |
| **verify** | the brief (both regions), the repo, the suite | the brief's **feedback region** (gap cycles); `project/loops/blocked.md` (second stall) | the phase-deletion commit (pass only) | on pass: the phase's `STATUS.md` line + `plan/phase-NN.md` + the brief; on stall reset or blocked escalation: the brief |

Neither `build` nor `verify` ever opens `project/design/`, `project/plan/`, or
`project/product/` — the brief is their whole specification. Nothing in the loop
touches anything outside `telemetry/`.

## The brief lifecycle

`project/loops/brief.md` is the seam between the three steps. It is
**never committed** (covered by the repo-root `.gitignore` rule
`*/project/loops/brief.md`), **single-phase**, and **phase-scoped, not
per-cycle**:

1. `gather` checks for `project/loops/blocked.md` first (→ `DONE` if present),
   then finds the first `⬜` phase with
   `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`. No match →
   `DONE`.
2. If a brief already names that same phase, the phase is mid-flight: `gather`
   leaves it entirely untouched (contract *and* feedback) and reports `NEXT`
   without opening a big doc. Otherwise it authors a fresh brief with an empty
   feedback region.
3. `build` reads the whole brief, closes any open gaps first, does as much of
   the phase as cleanly fits one context, and commits.
4. `verify` re-derives truth independently. **Pass** → delete the phase line,
   `git rm` the phase body, commit, delete the brief. **Gap** → leave `⬜`,
   change no source, overwrite the feedback region with only the currently-open
   gaps.

The brief therefore persists across cycles for as long as its phase stays `⬜`.

## The stall and blocked ladder

`verify` tracks progress across cycles via the brief's feedback region and a
persistent `~/.ralph/verify.log`:

- **Slow convergence** — the open-gap id set shrinks cycle over cycle: `verify`
  just overwrites the feedback region with the smaller set and hands off.
- **Stall reset** — three consecutive cycles close no gap (same gap ids, no new
  build commit): `verify` logs `Phase NN STALLED …` to `~/.ralph/verify.log`,
  deletes the brief, and leaves `⬜`. The next `gather` rebuilds the contract
  fresh from spec — a trajectory reset, not a halt.
- **Blocked escalation** — a *second* stall on the **same** phase (verify finds
  an earlier `Phase NN STALLED` line in `~/.ralph/verify.log`) means a rebuilt
  contract already failed to help: the phase's done bar, not the trajectory, is
  the fault. `verify` writes `project/loops/blocked.md` naming the phase, the
  total attempts, the unsatisfied ids, and the exact command/output that will
  not go green, then deletes the brief and leaves `⬜`. The next `gather` sees
  `blocked.md` and reports `DONE`, stopping the run.

**Operator response to a `blocked.md`:** read the recorded command and output,
fix the phase's done bar in `project/` (the loop cannot — `project/` is
read-only to it), delete `project/loops/blocked.md`, and restart the loop.

## Why it converges

`verify` can neither halt the run nor advance a phase on a gap, so an incomplete
phase simply stays `⬜` and is re-attacked next cycle — now with grounded,
command-level feedback in front of `build`, and without `gather` re-reading the
big docs (it no-ops on an in-flight brief). The persisted feedback also gives
`verify` cross-cycle memory: it distinguishes *slow convergence* (the open-gap
id set shrinking) from a *true stall* (the same gap ids open across three
consecutive attempts with no new build commit). On a true stall it does a
**trajectory reset**; on a second stall of the same phase it escalates to
`blocked.md` instead of resetting again. The only exits are `gather → DONE` —
zero `⬜` markers left, or a blocked phase awaiting the operator — plus ralph's
budget rails.

## The `project/loops/brief.md` schema

Two regions, one writer each — `gather` owns the contract, `verify` owns the
feedback, and neither writes the other's.

```markdown
# Brief — Phase NN
<one-line objective, from the phase header>

## Realized Decisions
- D<N> — <title> (project/design/DNN.md)

## Design — D<N> <title>
<the full Decision. prose (shapes, signatures, code blocks) and the
Rejected. alternatives, copied verbatim from DNN.md — with that
Decision's Verification. list OMITTED. One section per realized Decision.>

## Ids to cover
R-XXXX-XXXX — <the id's full requirement text, verbatim, on the same line>
R-XXXX-XXXX — <…>

## Files to touch
- <path> — <what changes>

## Dependency interfaces
<exported signatures of the packages this phase consumes, or
"(none — no dependencies)">

## Done bar
<the phase's "Done when" conditions verbatim, plus the coverage, substrate,
test-placement, and green-suite bar>

## Verify feedback — attempt N
- build commit: <sha>
- stall streak: <k>
- open gaps:
  - R-XXXX-XXXX — `<exact failing command>` → <observed output> (<file:line>)
```

The `## Ids to cover` shape is load-bearing: one id per line, id at line-start,
` — `, then the full requirement text on the same line. The denominator is read
with `grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md`, which
matches only those line-anchored ids and never one quoted in prose elsewhere. A
structural phase writes the single line `(none — structural phase)`.

`gather` writes the feedback heading empty (`attempt 0`); `verify` **overwrites**
it (never appends) with only the gaps still open.

## The coverage convention this loop enforces

Design mints the `R-XXXX-XXXX` ids; how coverage is measured is the loop's. An
id counts as **covered** only when all of these hold:

- a `// R-XXXX-XXXX` comment sits on or beside a test in a `*_test.go` file that
  **genuinely asserts** that id's stated behavior — never a bare literal;
- that test **actually runs** under `go test ./...`. A test held out by a skip
  condition, build tag, or env flag nothing in the repo satisfies is
  **uncovered**, as is one that turns a real failure signal into a skip. A skip
  is never acceptable green for a requirement;
- it runs on the substrate the requirement names — real temp-file SQLite with
  the real migrations for storage/DDL/ordering/query-plan claims, a real
  `127.0.0.1` listener through the registered route for transport claims, an
  injected `internal/telemetry.Clock` and a test-driven ticker for time;
- it is **co-located** with the code it exercises and named for the behavior
  (`internal/<pkg>/<behavior>_test.go`; `cmd/telemetry/` for composition-root
  and shipped-file guards; `internal/e2e/` for the cross-package end-to-end
  layer) — never a per-phase or root-level test file.

Beyond a single phase's own denominator, `verify` also runs the **global
coverage ratchet** every cycle — the design-wide id set minus the union of the
real test-tag set and the pending-phase id set must be empty — to catch a
rewrite silently dropping a previously-covered id.

A phase is **done** when every id it owns is covered that way, every Done-bar
check in its body passes with its stated criterion, the global ratchet is
empty, and the suite is green: from `telemetry/`, `go build ./...`,
`go vet ./...`, and `go test ./...` all exit 0. A structural phase carries no
ids and is proven by the green build plus the deterministic checks it names.
