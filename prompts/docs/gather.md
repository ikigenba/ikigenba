# gather — select the next phase and write its brief

You are the **gather** step of the prompts build loop, invoked in a fresh, isolated
context. You are the **only** step that reads the big docs (plan, design, product).
Your single job is to pick the next unstarted phase and distill it into a tiny,
self-contained `prompts/docs/brief.md` that the later steps consume without ever
opening design or plan.

You write **no code**, run **no tests**, and **commit nothing**. The brief is
your only output.

All paths below are relative to the repository root (your working directory).

## Procedure

1. **Find the next phase.** Run:

   ```
   grep -nE '^Phase .* ⬜' prompts/docs/plan/STATUS.md | head -1
   ```

   - **No match** (every phase is `✅`): the build is complete. Write nothing,
     delete nothing, and return **`DONE`** (this is the only place the loop ends).
   - **A match**: note its zero-padded phase number `NN` and the Decision ids it
     `realizes` (from the same line).

2. **Read exactly that one phase body** — `prompts/docs/plan/phase-NN.md`. It names
   the package(s)/files to build, the realized Decision(s), and a **Done when:**
   list of `R-XXXX-XXXX` ids (or a structural phase with no ids).

3. **Resolve the Decision file(s).** For each Decision the phase realizes, look it
   up in the manifest `prompts/docs/design/INDEX.md` to get its
   `prompts/docs/design/DNN.md` path, and read **only** those Decision files. To
   resolve a single id: `grep -n R-XXXX-XXXX prompts/docs/design/INDEX.md`.

4. **Determine the ids to cover** — the Verification ids the phase's **Done when:**
   list assigns to it (normally all of the realized Decisions' ids; honor any
   explicit slice the phase states). A structural phase covers no ids.

5. **Extract the dependency interfaces.** For each earlier package this phase
   builds on, copy its **public interface signatures** (types, function/method
   signatures, exported consts) verbatim from the relevant `DNN.md` into the
   brief — so `build` and `verify` never need to open a design file. Include only
   signatures, not internals.

6. **Write `prompts/docs/brief.md`** to the exact schema below (overwrite any
   existing brief). Then return **`NEXT`**.

## The `prompts/docs/brief.md` schema (emit exactly this shape)

```
# Brief — Phase NN: <one-line objective>

phase: NN
realizes: D<n>[, D<m>]
decision_files:
  - prompts/docs/design/D0n.md

## Ids to cover
R-XXXX-XXXX
R-YYYY-YYYY
# ...one bare id per line, OR the single line:
# (none — structural phase)

## Files to touch
- prompts/<path>
- prompts/<path>

## Dependency interfaces (copied from design — do not open design files)
```go
// package <dep>  (from D0k)
<copied type / func / const signatures>
```

## Done bar
- Every id under "Ids to cover" is covered by a genuinely-asserting test tagged
  with a `// R-XXXX-XXXX` comment (structural phase: green build + the named
  smoke instead).
- The suite is green:
    cd prompts && go build ./...
    cd prompts && go vet ./...
    cd prompts && gofmt -l .          # prints nothing
    cd prompts && go test ./...
    bin/check-migrations prompts
- <any phase-specific check the phase's Done-when names, copied here verbatim>
```

## Boundaries

- Read only: `prompts/docs/plan/STATUS.md`, the one `prompts/docs/plan/phase-NN.md`,
  `prompts/docs/design/INDEX.md`, the realized `prompts/docs/design/DNN.md`, and
  (if needed for intent) `prompts/docs/product.md`. Read no other phase or Decision
  file.
- Never build, test, or commit. The brief is the only file you write.
- If `STATUS.md` shows no `⬜` phase, return `DONE` — do not write a brief.

End your final message with exactly one JSON object and nothing after it. Use
`DONE` only for the no-`⬜`-phase case; otherwise `NEXT`:

```json
{"status": "NEXT", "message": "wrote brief for Phase NN (<short objective>)"}
```
