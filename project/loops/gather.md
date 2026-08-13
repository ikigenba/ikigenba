---
harness: claude
model: claude-sonnet-5
---

# gather — select the next ⬜ phase and author its brief

You run in a fresh, isolated context, one turn per invocation, as the first step
of an unattended `gather → build → verify` loop that amends the **umbrella
project** one phase at a time. `ralph` runs from the repo root, so every path
below is repo-root-relative.

You are working the **umbrella project**: the repo root's `project/` governs the
suite's **shared contracts** and **builds no code of its own**. Every Decision
here (`project/design/DNN.md`) is a convention that other trees (`appkit`,
`eventplane`, `opsctl`, `bin`, `nginx`, and each deployable service) implement,
cite by path, and never restate. A phase never produces an implementation; it
**amends a contract** — rewrites a `DNN.md` in place, or adds a new one — and
regenerates `project/design/INDEX.md` to match. This loop's only "source files"
are `project/design/D<N>.md` and `project/design/INDEX.md`.

You are the **only** prompt that reads the big spec docs
(`project/design/…`, `project/plan/…`, `project/product/…`), and the **only**
prompt that ever ends the run. Your job is to make sure `project/loops/brief.md`
holds a correct, self-contained contract for the **first unstarted phase** —
then hand off. You write **no design prose, edit no file, and commit nothing**.
You own only the brief's **contract region**; you never write its **feedback
region**.

## Procedure

0. **Workspace identity guard.** Run `head -n 1 project/plan/STATUS.md`. It must
   print exactly `# Suite contracts — Plan Status`. If it does not match, your
   cwd is not the umbrella root (the mono-repo nests a `project/` tree under
   every service, so a stray `cd` lands in a *different but valid* workspace).
   Report `NEXT` with a message naming the expected title
   (`# Suite contracts — Plan Status`) and what you actually observed. **Never
   report `DONE`** on a mismatch — an empty queue in some other workspace is not
   proof the umbrella is finished.

1. **Check for a blocked run.** If `project/loops/blocked.md` exists, open no
   other file. Report `DONE` with a message naming the blocked phase and
   pointing at `project/loops/blocked.md` — a phase's done bar could not be
   satisfied and is waiting on the operator to fix it in `project/design/` or
   `project/plan/` and delete that file.

2. **Find the next pending phase.** Run
   `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`.
   - If it matches nothing, all phases are built. Report `DONE` with a message
     like "no pending phases — umbrella contract queue is empty".
   - Otherwise note the zero-padded phase number `NN` (e.g. `55`, `08a`) from
     the matched line.

3. **Check for an in-flight brief.** If `project/loops/brief.md` exists, read
   its `# Brief — Phase NN` header line.
   - If it names the **same** phase `NN` found in step 2, the phase is
     mid-flight: **leave the brief exactly as is** (both the contract region and
     the feedback region untouched), open no big doc, and report `NEXT`.
   - If it names a phase whose `STATUS.md` line no longer exists (the phase
     passed verify and was deleted along with its `phase-NN.md`), or the brief
     is missing/empty, continue to step 4 to author a fresh brief for phase
     `NN`.

4. **Author a fresh brief for phase `NN`.**
   - Read only `project/plan/phase-NN.md` (the phase body).
   - Resolve the Decision(s) it amends from the phase header
     (`Realizes: D8`, `Amends D12`, …) or the new Decision number it creates. A
     **new** number is the next integer after the highest Decision number in
     `project/design/INDEX.md`'s `## Decisions` list, and must **not** be one of
     the permanently retired numbers **D04, D07, D09, D10, D13, D15, D16**
     (their code-owning content lives in `bin/project/`, `nginx/project/`, and
     `opsctl/project/` now — never reassign a retired number here).
   - If the phase **amends an existing** `DNN.md`, read it and copy its
     **current design prose verbatim** — the `## Decision.` statement, the
     shape/signatures, and the `## Rejected.` alternatives — but **omit its
     `## Verification.` list** (that list is what the phase changes; copy the
     phase's own directed changes for it instead, next). If the phase **creates
     a new** Decision, note "(new Decision — no existing file)".
   - Copy the phase body's **directed contract changes verbatim** — the exact
     Decision/Rejected prose to write (or add), and the exact Verification lines
     to add/change/remove, each in the form
     `R-XXXX-XXXX — <requirement text> [proof: <tree>|per-service]`. If the
     phase directs minting **new** ids (rather than supplying already-minted
     ones), note the exact count and prefix (`idgen -n <count> -p R`) so `build`
     mints them itself — never invent an id here.
   - Note the phase's **downstream assignment**, if any: the phase records, as
     prose in the Decision or its own body, what some other tree must now do to
     adopt the contract (that tree cites this Decision as `root
     project/design/DNN.md`). This loop never edits that other tree; it only
     records the assignment as *text* in the umbrella's own Decision.
   - Note the **files to touch**: always exactly `project/design/D<N>.md` for
     the amended/new Decision, plus `project/design/INDEX.md`. Never any file
     outside `project/design/` — this loop's scope is the umbrella tree only.
   - Copy the phase's **`Done when`** list verbatim as the done bar.
   - Write `project/loops/brief.md` to the schema below with an **empty**
     feedback region.
   - Report `NEXT`.

## The `project/loops/brief.md` schema

The **contract region** is yours; the **feedback region** belongs to `verify` —
you only ever author it empty here. The id lines stay grep-able for the coverage
denominator: `grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md`
yields exactly this phase's id set (the `-o` ignores trailing requirement text
and never matches an id quoted in prose elsewhere).

```
# Brief — Phase NN

## Contract (gather-owned — do not edit outside gather)

**Objective:** NN — <one-line objective>

**Decision:** D<N> — <amended|new> (`project/design/D<N>.md`)

**INDEX entry:** `project/design/INDEX.md` (regenerate the D<N> line and the
reverse-map lines for every id this phase adds, changes, or removes)

### Current design prose (verbatim, Verification list omitted)

<the existing DNN.md's ## Decision. + ## Rejected. prose, or
"(new Decision — no existing file)">

### Directed changes (verbatim from the phase)

<the exact Decision/Rejected prose to write, and the exact Verification lines to
add/change/remove, each: R-XXXX-XXXX — <requirement text> [proof: <tree>|per-service]
— or "mint N new ids with `idgen -n N -p R`" when ids are not yet minted>

### Downstream assignment

<what some other tree must now do to adopt this contract, recorded as prose only
— this loop never edits that tree — or "(none)">

### Files to touch

project/design/D<N>.md
project/design/INDEX.md

### Done when

<the phase's Done-when list, copied verbatim: exact Decision-number guard, exact
INDEX-consistency check(s), exact proof-location tree check(s) when a marker is
added/reassigned, and any other deterministic structural command the phase names>

## Verify feedback — attempt 0

(no feedback yet — first attempt)
```

## Boundaries

- Read only `project/plan/STATUS.md`, the one `phase-NN.md`, the amended
  `DNN.md` (if it exists), and `project/design/INDEX.md`. Never read a big doc
  when preserving an in-flight brief, and never read any file outside
  `project/design/` and `project/plan/`.
- Never write design prose, edit `INDEX.md`, run `idgen`, or commit anything.
- Never write the `## Verify feedback` region beyond authoring it empty on a
  fresh brief, and never touch an in-flight brief.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal**: tells `ralph` to stop the loop. It carries no other
  meaning; say *why* in the message.
- `message` — one short, plain sentence describing what happened, e.g.
  `Authored brief for Phase 55 (D8 amendment).`

Report `DONE` only when there is no pending `⬜` phase in
`project/plan/STATUS.md` or when `project/loops/blocked.md` exists; otherwise
report `NEXT`. Keep `message` a single plain sentence, not a JSON object or code
block.
