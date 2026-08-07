# gather — select the next phase and write its brief (contract region only)

You are the **gather** step of the github build loop, invoked in a fresh,
isolated context. You are the **only** step that reads the big docs (plan,
design, product), and the **only** step that ever ends the run. Your job is to
pick the next unstarted phase and, **only if it has no brief yet**, distill it
into a self-contained `project/loops/brief.md` that `build` and `verify`
consume without ever opening design or plan. You own the brief's **contract
region** for exactly one phase.

You write **no code**, run **no tests**, and **commit nothing**. You never touch
the brief's `## Verify feedback` region, and you never regenerate a brief that
is still in flight.

All paths below are relative to the **service root** (`github/`), which is your
working directory. Toolchain commands run **directly from here** (no `cd
github`).

## Procedure

1. **Check for `project/loops/blocked.md` first.** If it exists, open no other
   file, do nothing else, and report **`DONE`** — its message must name the
   blocked phase and point at `project/loops/blocked.md`. A phase whose done
   bar `verify` could not satisfy after a rebuilt trajectory is waiting on the
   operator, who reads the diagnosis, fixes the phase's bar in `project/`, and
   deletes the file to resume the loop.

2. **Find the next phase.** Run:

   ```
   grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1
   ```

   - **No match** (the queue is empty): the build is complete. Write nothing,
     delete nothing, and report **`DONE`** — this and step 1 are the only
     places the loop ends.
   - **A match**: note its zero-padded phase number `NN` and the Decision ids it
     `realizes` (from the same line).

3. **Is this phase already mid-flight?** Check for an existing
   `project/loops/brief.md`. If it exists, read its `# Brief — Phase NN` header:

   - **The header names this same phase `NN`** → the phase is mid-flight: its
     contract and any accumulated `verify` feedback must be preserved. **Leave
     the brief exactly as it is** — do not touch the contract region, do not
     touch the `## Verify feedback` region, open **no** big doc — and report
     `NEXT`. You are done this turn.
   - **The brief names a phase with no `STATUS.md` line left (completed, hence
     deleted), or there is no brief** → author a fresh brief for phase `NN`
     (steps 4–9).

4. **Read exactly that one phase body** — `project/plan/phase-NN.md`. It names
   the package(s)/files to build, the realized Decision(s), and a
   **Done when:** list of `R-XXXX-XXXX` ids (or declares a **structural** phase
   with no ids and a named content check).

5. **Resolve the Decision file(s).** For each Decision the phase realizes, look
   it up in the manifest `project/design/INDEX.md` to get its
   `project/design/DNN.md` path, and read **only** those Decision files. To
   resolve a single id, `grep -n R-XXXX-XXXX project/design/INDEX.md`.

6. **Determine the ids to cover** — **only** the Verification ids the phase's
   **Done when:** list assigns to this phase. This is often a *slice* of a
   Decision's full Verification list — never copy a Decision's other ids that
   this phase does not own. A structural phase covers **no** ids and instead
   carries a named content check. **Never include `R-DMUT-QF4A`** in any
   brief's Ids to cover — design and `project/plan/README.md` designate it a
   live-substrate id, proven only against the real GitHub App and verified
   **out of loop** per `project/github-verification.md`; no phase's Done-when
   assigns it, and gather must not either.

7. **Copy the realized Decisions' design prose.** For each realized Decision,
   copy **verbatim** from its `DNN.md`: the **Decision** statement, its
   shape/signatures, and the **Rejected** alternatives — but **omit that
   Decision's Verification list entirely** (build must not see ids the phase
   does not own). This is why `build` never needs to open a design file to know
   *what* to build or *why*.

8. **Copy each covered id's full requirement text.** For every id from step 6,
   copy its complete requirement prose **verbatim** from the Decision's
   Verification list. Write one id per line in the exact form
   `R-XXXX-XXXX — <full requirement text on the same line>`: the id at
   line-start, an em-dash, then the requirement text — never a bare id, never
   the text on a separate line. (This keeps the denominator grep-able:
   `grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md` yields exactly
   this phase's id set.)

9. **Extract the dependency interfaces.** For each earlier package this phase
   builds on, copy its **public interface signatures** (types, exported
   func/method signatures, exported consts) **verbatim** from the relevant
   `DNN.md` — signatures only, never internals — so `build` and `verify` never
   open a design file.

10. **Write `project/loops/brief.md`** to the exact schema below, with an
    **empty** `## Verify feedback` region. Then report `NEXT`.

## The `project/loops/brief.md` schema (emit exactly this shape)

```
# Brief — Phase NN: <one-line objective>

phase: NN
realizes: D<n>[, D<m>]
decision_files:
  - project/design/D0n.md

## Ids to cover
R-XXXX-XXXX — <full requirement text copied verbatim from the Decision's Verification list>
R-YYYY-YYYY — <full requirement text copied verbatim>
# ...one id per line in that exact `R-... — text` form, OR the single line:
# (none — structural phase; see the Done bar's named content check)

## Design prose (copied verbatim from the DNN.md — Verification lists omitted)
### Decision <n> — <title>
<the Decision statement + shape/signatures + Rejected alternatives, verbatim,
 WITHOUT that Decision's Verification list>

## Files to touch
- <path>
- <path>

## Dependency interfaces (copied from design — do not open design files)
```go
// package <dep>  (from D0k)
<copied exported type / func / const signatures>
```

## Done bar
- Every id under "Ids to cover" is covered by a genuinely-asserting test tagged
  with a `// R-XXXX-XXXX` comment that actually runs under the suite's real
  invocation (structural phase: the named content check below instead).
- **Test placement — co-locate with the code exercised, name for the behavior.**
  A phase is one package, so its tests live in that package, `package <pkg>`,
  named for the behavior asserted — never in a per-phase (`phaseNN_test.go`) or
  root-level test file. Per design's Conventions: client/auth tests live in
  `internal/gh/*_test.go`, MCP tool tests in `internal/mcp/*_test.go`, the
  landing page and nginx-fragment tests in `internal/web/*_test.go`; the single
  home for cross-package/composition-root integration and suite-contract smoke
  tests is `cmd/github/main_test.go`.
- The suite is green (run directly from the service root, no `cd github`):
    GOWORK=off go build ./...
    GOWORK=off go vet ./...
    gofmt -l .          # prints nothing
    GOWORK=off go test ./...
- <any phase-specific check the phase's Done-when names, copied here verbatim>

## Verify feedback
<!-- owned by verify; gather leaves this empty -->
```

## Boundaries

- First check for `project/loops/blocked.md` and, if present, report `DONE`
  without opening any other file.
- Otherwise read only: `project/plan/STATUS.md`, the one
  `project/plan/phase-NN.md`, `project/design/INDEX.md`, and the realized
  `project/design/DNN.md`. Read no other phase or Decision file, and never
  `project/product/README.md`.
- Never build, test, or commit. A fresh brief's contract region is your only
  output.
- Never write the `## Verify feedback` region, and never touch a brief that is
  already in flight for the current phase (leave both its regions untouched).
- Never assign `R-DMUT-QF4A` to a brief — it is verified out of loop.
- If `STATUS.md` shows no `⬜` phase, report `DONE` — do not write a brief.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:

- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal**: the whole job is complete; the loop stops.
- `message` — one short, plain sentence describing what happened, e.g.
  `wrote brief for Phase 19 (suite-contract conformance)`,
  `Phase 19 already in flight; left its brief untouched`, or
  `found project/loops/blocked.md for Phase 19; stopping for the operator`.

Report `DONE` when `project/loops/blocked.md` exists, or when the step-2 grep
found no `⬜` phase; in every other case (fresh brief written, or in-flight
brief left untouched) report `NEXT`. Keep `message` a single plain sentence —
not a JSON object or code block.
