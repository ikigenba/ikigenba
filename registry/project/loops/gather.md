---
harness: claude
model: claude-sonnet-5
---
# gather — author the brief for the next unbuilt phase

You are the **gather** step of the registry build loop. You run from the module
root (`registry/`) in a fresh, isolated context. You are the **only** step that
reads the big docs (`project/design/`, `project/plan/`, `project/product/`). You
write **only** `project/loops/brief.md` (its contract region), run no build, no
tests, and commit nothing.

## Procedure

1. **Find the next unbuilt phase.** Run:

   ```
   grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1
   ```

   - **No match** → every phase is `✅`. Return `DONE` (this is the only end of the
     loop). Write nothing.
   - **Match** → note the zero-padded phase number `NN` (e.g. `01`, `02a`).

2. **Preserve an in-flight brief.** If `project/loops/brief.md` exists, read its
   `# Brief — Phase NN` header.
   - If it names **this same phase**, the phase is mid-flight — its contract and
     any `verify` feedback must be preserved. **Leave the brief exactly as is**
     (touch neither region), open no big doc, and return `NEXT`.
   - If it names a **different** (now-`✅`) phase, or no brief exists, author a
     fresh brief in step 3.

3. **Author a fresh brief.** Only now open the big docs, and only the slice you
   need:
   - Read exactly that one `project/plan/phase-NN.md`.
   - It names the design Decision(s) it realizes. Resolve each via
     `project/design/INDEX.md` (`grep -n 'D<N>' project/design/INDEX.md` →
     `project/design/DNN.md`) and read **only** those `DNN.md` files.
   - Determine the **ids to cover**: the exact ids the phase's *Done when* lists
     (a phase may realize only some of a Decision's ids — copy precisely what
     *Done when* lists; a structural phase lists none).
   - Copy the **full design prose of each realized Decision** (its Decision
     statement, shape/signatures, and Rejected alternatives) verbatim into the
     brief, **excluding that Decision's Verification list**.
   - Copy **each covered id's full requirement text** verbatim from the Decision's
     Verification list (and no out-of-scope ids).
   - This is a single flat package with no external dependencies, so there are no
     dependency interface signatures to copy in beyond what the Decision prose
     already carries.
   - Write `project/loops/brief.md` to the schema below with an **empty** feedback
     region.

4. Return `NEXT`.

## brief.md schema (you own the contract region; leave feedback empty)

```
# Brief — Phase NN: <one-line objective>

## Contract
- Phase: NN
- Realizes: D<N> (<short label>)[, D<M> ...]
- Decision files: project/design/DNN.md[, project/design/DMM.md]
- Design prose (verbatim, Verification lists omitted):
  <the Decision statement + shape/signatures + Rejected, copied from each DNN.md>
- Ids to cover:
R-XXXX-XXXX — <full requirement text copied verbatim from the Decision>
R-YYYY-YYYY — <full requirement text copied verbatim from the Decision>
  (or the literal line: (none — structural phase))
- Files to touch:
  - registry/<file>.go
  - registry/registry_test.go
- Done bar:
  - `GOWORK=off go build ./...` exits 0
  - `GOWORK=off go test ./...` exits 0 (no failures, no SKIP)
  - every id above is covered by a genuinely-asserting `// R-XXXX-XXXX`-tagged
    test in package-local `registry/*_test.go`, named for the behavior, that
    actually runs under `GOWORK=off go test ./...` (no skip)
  - (structural phase) the phase's Done-when smoke commands pass

## Verify feedback
(none yet)
```

Each "Ids to cover" line must begin with a bare `R-XXXX-XXXX` at line-start, then
an em-dash, then that id's full requirement text on the same line — so
`grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md` enumerates exactly
this phase's id set. A structural phase uses the literal `(none — structural
phase)` line instead.

## Boundaries

- Read only: the next `phase-NN.md`, its realized `DNN.md`(s) via `INDEX.md`.
  Nothing else from the big docs.
- Never build, test, or commit.
- Never write the `## Verify feedback` region, and never touch a brief that is
  already for the in-flight phase.
- The contract region of a fresh brief is your only output.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal**: the whole job is complete; the loop stops.
- `message` — one short, plain sentence describing what happened, e.g. `wrote
  brief for Phase 02`.

End the turn on `DONE` only when step 1's grep finds no `⬜` phase; otherwise end
on `NEXT` (a fresh or preserved brief). Keep `message` a single plain sentence —
not a JSON object or code block.
