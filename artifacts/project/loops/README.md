# artifacts build loop — gather → build → verify

The unattended build loop installed in this tree. It builds `artifacts` one
pending phase at a time from the `project/` spec, with no human in the turn.
This document describes the loop **as installed** and lives beside the prompts
it describes; `project/README.md` only points here.

## Running it

```sh
./project/loops/run
```

which is exactly:

```sh
exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

`ralph` runs from the **service root** (`artifacts/`), so every path the
prompts reference is service-root-relative (`project/…`), and every toolchain
command runs bare inside the tree (design writes them as `cd artifacts && …`
because design is read from the repo root).

## The status contract

`ralph` re-invokes each prompt in a **fresh context** and reads only the
**final** message of the turn:

- **`NEXT`** — terminal: advance to the next prompt, wrapping `verify →
  gather`.
- **`DONE`** — terminal: stop. **Only `gather` ever reports `DONE`**, and only
  on one of two conditions: `project/loops/blocked.md` exists, or the `⬜`
  grep finds no pending phase.
- **`CONTINUE`** — **non-terminal**. A streaming backend under codex coerces
  *every* streamed message into the schema, so mid-turn progress messages need
  a status that does not advance the loop. `CONTINUE` never terminates a turn.

`build` and `verify` **always** end on `NEXT` — finishing a phase completely,
green suite and all, is still `NEXT`.

## Per-step reads, writes, commits, deletions

| step | reads | writes | commits | deletes |
|---|---|---|---|---|
| **gather** | `blocked.md`, `STATUS.md`, one `phase-NN.md`, `INDEX.md`, the realized `DNN.md`, dependency signatures | `brief.md` contract region (fresh briefs only) | nothing | nothing |
| **build** | `brief.md` only (contract + feedback) | source and tests under `artifacts/` | this turn's increment | nothing |
| **verify** | `brief.md`, the tree's tests/source, mechanical id greps | `brief.md` feedback region; `blocked.md` on escalation | the phase retirement | on pass: the phase's `STATUS.md` line, its `phase-NN.md`, and `brief.md`; on stall: `brief.md` |

`gather` is the only step that reads the big docs. `build` never opens
`project/design/`, `project/plan/`, or `project/product/`. `verify` never
reads them for its checklist — the brief *is* the checklist; its mechanical
id-set greps extract tokens, not prose.

## The brief lifecycle

`project/loops/brief.md` is the seam that keeps `build`'s context scoped to
one phase. It is **never committed** (the suite `.gitignore` covers it and
`blocked.md`), describes exactly **one** phase, and is **region-owned**:

- `gather` authors the **contract region** once, when a phase first becomes
  the active `⬜` phase, and **no-ops** on every later cycle while that phase
  stays `⬜` — it leaves the file untouched and opens no big doc.
- `build` reads the whole brief and writes none of it.
- `verify` owns the **feedback region**: on a pass it deletes the brief; on a
  gap it **overwrites** the feedback region with only the currently-open gaps
  and leaves the brief in place, so the next `build` sees them.

## The brief schema

```markdown
# Brief — Phase NN

## Objective
<the phase's one-line objective>

## Realizes
D<n> — <short label>

## Decision files
project/design/D<n>.md

## Design prose
<each realized Decision's statement, shape/signatures, and rejected
alternatives, verbatim — with that Decision's Verification list OMITTED>

## Ids to cover
R-XXXX-XXXX — <full requirement text, verbatim, on the same line>
(or: (none — structural phase))

## Files to touch
<paths>

## Dependency interfaces
<public signatures, copied in>

## Done when
<the phase's "Done when" bar, verbatim>

## Verify feedback — attempt N
Build commit observed: <sha>   (diagnostic only, not progress)
Stall streak: <k>

- [ ] R-XXXX-XXXX — <exact failing command>
      observed: <exact observed output>
```

The `## Ids to cover` line format is load-bearing: the id at line start, an
em-dash, then the full requirement text on the same line, so the phase's id
set stays grep-able with
`grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md`.

## The green gate

```sh
go build ./...      # must exit 0
go vet ./...        # must exit 0
gofmt -l .          # must print NOTHING
go test ./...       # must exit 0, zero failures
```

Green means all four succeed with zero failures and `gofmt -l .` prints
nothing. No `R-`-tagged test may report `SKIP`.

## Coverage, reachability, and the ratchet

An id counts as **covered** only when a `// R-XXXX-XXXX` comment names a test
that genuinely asserts the behavior **and** that test actually runs under the
suite's real invocation. Reachability is traced **statically** — the test
command plus every build constraint, env condition, and skip guarding the test
body.

**artifacts has no live layer** (design D10: hermetic and composed only, both
in the default gate), so `verify` applies the strict rule with no carve-outs:
a build-tag-gated tagged test, an env-gated test nothing in the repo
satisfies, a `//go:build live` file, or a test that launders a real failure
into a skip all count as **uncovered**. The declared environmental
preconditions (Go toolchain, POSIX `bash` with `grep`, headless Chrome for the
landing-page proof) are hard failures when absent, never skips.

Every cycle `verify` also runs the **global coverage ratchet**, which catches
a rewrite silently dropping a previously-covered id:

```sh
comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
         <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
               <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
```

**Empty output is the pass condition.** Two parts are load-bearing:
`grep -v 'R-XXXX-XXXX'` filters the documentation placeholder the design docs
quote when explaining the id format (it matches the pattern, no test will ever
carry it, and without the filter the ratchet can never report clean), and
`--exclude-dir=project` keeps ids quoted in spec and loop documents out of the
test-tag set.

## The stall and blocked ladder

`verify` can neither halt the run nor advance a phase on a gap, so an
incomplete phase simply stays `⬜` and the loop re-attacks it next cycle — now
with `verify`'s grounded feedback in front of `build`, and without `gather`
re-reading the big docs. The persisted feedback gives `verify` cross-cycle
memory:

1. **Progress** — the open-gap id set is a strict subset of last attempt's.
   The stall streak resets to 0. (A new build commit is **never** progress: a
   builder stuck on a bar keeps committing plausible rewordings, and a
   commit-keyed detector reads that churn as convergence and never trips.)
2. **Stall reset at 3** — three consecutive attempts closing no gap. The
   accumulated brief may not be converging, so `verify` logs
   `Phase NN STALLED …` to `~/.ralph/verify.log`, deletes the brief, leaves
   `⬜`, and reports `NEXT`. The next `gather` rebuilds the contract fresh
   from spec.
3. **Blocked on a second stall** — if `~/.ralph/verify.log` already carries a
   `STALLED` line for this same phase, a rebuilt contract has been tried and
   did not help, so the **bar itself** is the fault. `verify` writes
   `project/loops/blocked.md` with the phase, the attempt count, the
   unsatisfied ids, and the exact command and observed output that will not go
   green; logs `Phase NN BLOCKED …`; deletes the brief; leaves `⬜`; reports
   `NEXT`. The next `gather` sees the file and reports `DONE`.

**What the operator does with a `blocked.md`:** read the recorded command and
its observed output, decide whether the phase's *done bar* is defective (it
usually is — a non-deterministic, self-referential, or structurally
unsatisfiable check), fix it in `project/plan/phase-NN.md` (or the design
behind it) through a spec authoring move, delete `project/loops/blocked.md`,
and restart the loop.

## Why it converges

`verify` never ends the run and never advances a phase on a gap, so the only
exits are `gather`'s two: zero `⬜` markers (every phase verified green) or a
blocked phase awaiting the operator — plus ralph's own budget rails. Every bar
the loop checks is a deterministic, reachable, mechanically-evaluable
predicate, so a phase either goes green or produces a written diagnosis; it
never spins silently.
