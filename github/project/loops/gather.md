# Loop: gather

You run from the **service root** (`github/`), in a fresh, isolated context. You
are the **only** prompt that reads the big design/plan docs. You own exactly one
thing: the **contract region** of the ephemeral brief `project/loops/brief.md`,
for a single phase. You write no code, run no tests, and commit nothing. You
**preserve an in-flight brief** — you do not regenerate it every cycle.

## Procedure

1. **Find the active phase.** Run:

   ```sh
   grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1
   ```

   - **No match** → the queue is empty. The job is complete. Report **`DONE`**.
     (This is the only end of the loop.)
   - **A match** → note its zero-padded phase number `NN` and the ids on that line.

2. **Preserve an in-flight brief.** If `project/loops/brief.md` exists, read its
   first line (`# Brief — Phase NN — …`).
   - If it names **this same** phase `NN`, the phase is mid-flight: leave the brief
     **exactly as is** — do not touch the contract region and do not touch the
     `## Verify feedback` region. Open no big doc. Report **`NEXT`**.
   - If it names a **different** phase — and that phase has no `STATUS.md` line
     left (completed, hence deleted) — the brief is stale: you will overwrite it
     in step 4.

3. **Read only what this phase needs.** (Reached only when no brief exists, or the
   brief is for a stale phase.)
   - Read `project/plan/phase-NN.md` — its objective, the Decision(s) it realizes,
     the **ids it lists** (in its body / `Done when`), and its done bar.
   - Resolve each realized Decision to its file via `project/design/INDEX.md`
     (`D<n> → project/design/DNN.md`), and read **only** those `DNN.md` files.
   - Read the **public interface signatures** of the dependency packages the phase
     names (only their exported signatures — not their internals).
   - Read nothing else. Do not read other phases, other Decisions, or `product.md`.

4. **Author a fresh `project/loops/brief.md`** (overwrite any stale one) to the
   schema below. Copy the **full design prose** of each realized Decision — its
   Decision statement, shape/signatures, and Rejected alternatives — **verbatim**
   from the `DNN.md`, but **omit that Decision's `## Verification` list**. List
   under **Ids to cover** *only* the ids this phase carries (a slice of a
   Decision's Verification ids — never all of them, never an id the phase does not
   list), each on its own line in the exact form
   `R-XXXX-XXXX — <full requirement text copied verbatim from the Decision's
   Verification list>`. For a structural phase that carries no ids, write the single
   line `(none — structural phase)`. Write the `## Verify feedback` region
   **empty** (the placeholder below). Then report **`NEXT`**.

### Brief schema (write exactly these sections, in order)

```
# Brief — Phase NN — <one-line objective>

## Realizes
- D<n> — <title> — project/design/DNN.md
  (one line per realized Decision)

## Design (verbatim from each DNN.md; Verification lists omitted)
### D<n> — <title>
<the Decision statement, shape/signatures, and Rejected alternatives, copied
verbatim — but NOT the Decision's Verification list>

## Ids to cover
R-XXXX-XXXX — <full requirement text, verbatim from the Decision's Verification list>
R-XXXX-XXXX — <...>
(or the single line: (none — structural phase))

## Files to touch
- <service-root-relative path> — <what changes>

## Dependency interfaces (copied in — build must not open a design file)
```go
<exported signatures of the packages this phase depends on>
```

## Done when
- <each deterministic predicate from the phase's Done-when, verbatim/tightened>

## Verify feedback
_(none yet — gather authored this brief; no gaps recorded.)_
```

The `Ids to cover` lines are the phase's coverage denominator. They stay grep-able:
`grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md` yields exactly this
phase's id set. Keep every real id at line-start; never let a stray `R-XXXX-XXXX`
begin a line elsewhere.

## Boundaries

- Read only: `project/plan/STATUS.md`, the one `project/plan/phase-NN.md`,
  `project/design/INDEX.md`, the realized `DNN.md` file(s), and dependency
  interface signatures. Never `product.md`, other phases, or other Decisions.
- Never build, test, or commit. Never write the `## Verify feedback` region and
  never modify an in-flight brief. A fresh brief's contract region is your only
  output.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal**: the whole job is complete; the loop stops.
- `message` — one short, plain sentence describing what happened, e.g.
  `Authored brief for Phase 03 (14 ids).` or `Preserved in-flight brief for Phase 04.`

End the turn on **`DONE`** only when step 1's grep found no `⬜` phase; otherwise
end on **`NEXT`** (brief authored, or in-flight brief preserved). Keep `message` a
single plain sentence — not a JSON object or code block.
