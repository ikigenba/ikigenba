---
harness: claude
model: claude-sonnet-5
---
# gather — author the phase brief

You run in a **fresh, isolated context** from the service root `appkit/` (the
directory `ralph` launched from; all `project/…` paths below are relative to it).
You are the **only** prompt that reads the big design/plan docs, and you own the
**contract region** of `project/loops/brief.md` for exactly one phase. You write
no code, run no tests, and commit nothing. Do one iteration, then report.

## What you produce

A self-contained `project/loops/brief.md` that is the **complete and only** input
`build` and `verify` consume — so neither of them ever opens a design or plan
file. You either author it fresh for a newly-active phase or leave an in-flight
one untouched.

## Procedure

1. **Find the next unit of work.** Run:

   ```
   grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1
   ```

   - **No match** → the queue is empty. The whole job is done: report **`DONE`**.
     This is the *only* place the loop ends. Do not write or touch the brief.
   - **A match** → note its phase number `NN` (the two-digit number after the
     literal words `- Phase`). Continue.

2. **Preserve an in-flight brief.** If `project/loops/brief.md` exists, read only
   its first line, the header `# Brief — Phase NN`.
   - If that header names the **same** phase `NN` you just found, the phase is
     **mid-flight** — its contract and any `verify` feedback must be preserved.
     Leave the file **exactly as is** (touch neither region), open **no** big doc,
     and report **`NEXT`**. You are done this turn.
   - If it names a phase with no `STATUS.md` line left (completed, hence
     deleted), or the file does not exist, fall through to step 3 and author a
     fresh brief.

3. **Resolve the phase.** Read **only**:
   - `project/plan/phase-NN.md` — the phase body (what gets built, the ids it
     owns or its slice of a Decision's ids, its *Done when* bar).
   - The realized Decision file(s). Resolve each `D<k>` the phase names via
     `project/design/INDEX.md` (`- D<k> → project/design/D0k.md …`); read only
     those `project/design/D0k.md`. To resolve a bare id, `grep -n R-XXXX-XXXX
     project/design/INDEX.md`.
   - The **dependency interface signatures** the phase leans on: the public
     Go signatures of any package the phase's code calls (e.g. from `appkit/mcp`,
     `appkit/server`, or the `eventplane` sibling's `outbox`/`consumer` packages)
     — read just enough to copy the exact signatures. If the phase has no code
     dependency beyond its own package, this is `(none)`.

   Determine **the ids to cover**: *only* the `R-XXXX-XXXX` ids the phase's body /
   *Done when* section lists as this phase's new coverage — this is usually a
   **slice** of a Decision's full Verification list, never all of it. Never
   include an id the phase does not own, even if it lives in the same Decision.

4. **Write `project/loops/brief.md`** to the schema below. Copy the **full design
   prose** of each realized Decision verbatim from its `D0k.md` — the Decision
   statement, the shape/signatures, and the Rejected alternatives — but **omit
   that Decision's `Verification` list** (build must not see ids the phase does
   not own). Copy **each covered id's full requirement text** verbatim from the
   Decision's Verification list. Leave the feedback region **empty** (the stub
   shown). Then report **`NEXT`**.

## `project/loops/brief.md` schema

Write exactly this structure. The `<!-- VERIFY FEEDBACK BELOW … -->` marker is the
hard boundary between the two single-writer regions: you own everything **above
and including** the marker; `verify` owns everything **below** it. Never write
below the marker beyond the empty stub, and when preserving an in-flight brief
never touch either region.

```
# Brief — Phase NN

## Objective
Phase NN — <one-line objective copied/condensed from phase-NN.md's title>

## Realized Decision(s)
- D<k> — project/design/D0k.md

## Design prose (verbatim from D0k.md; Verification list omitted)
<the Decision statement, shape/signatures code block, and the Rejected
 alternatives, copied verbatim from D0k.md — everything except its Verification
 list>

## Ids to cover
R-XXXX-XXXX — <full requirement text copied verbatim from the Decision's Verification list>
R-YYYY-YYYY — <full requirement text copied verbatim>
```
(one id per line, id at line-start, an em-dash, then that id's complete
requirement prose on the **same** line. Never a bare id, never the text on its
own line. If the phase owns no ids, write the single line
`(none — structural phase)`.)
```
## Files to touch
- <path>
- <path>

## Dependency interface signatures
<public signatures the phase's code calls, copied verbatim, or "(none)">

## Done bar
<the deterministic pass predicate(s) for this phase, as exact commands — copied
 from phase-NN.md's Done when section and design's Conventions. Use the
 project's fixed check catalog:
 - appkit Go ids → the appkit suite is green from `appkit/`: `go build ./...`,
   `go vet ./...`, `gofmt -l .` (no output), `go test ./...` all pass, plus the
   isolated-module mirror `GOWORK=off go build ./...`; and every owned id has a
   genuinely-asserting `// R-XXXX-XXXX`-tagged test co-located in the exercised
   package's own `*_test.go` (e.g. `mcp/*_test.go` for transport/tool ids,
   `server/*_test.go` for route ids) that actually runs (no SKIP).
 - shell-collaborator ids (only when the phase names one; design's cross-module
   collaborators) → the `bin/registry` behavior is `../bin/registry.test.sh`
   exiting 0 with a `# R-…`-tagged asserting case; the `bin/start` behavior is
   the named live `/services` smoke.
 - any extra deterministic check the phase's Done when section states — copy it
   verbatim (e.g. Phase 12's `grep -rn "JSONResult" --include="*.go" .` from
   `appkit/` printing nothing; the `--include` scope keeps it off `project/`
   docs).
 State the concrete co-located test path(s) so build and verify enforce
 placement: unit tests live beside the code they exercise, named for the
 behavior — never a per-phase or root-level test file.>

<!-- VERIFY FEEDBACK BELOW — verify owns everything past this line; gather writes this marker once, leaves the stub, and never touches this region again. -->

## Verify feedback
(none yet — first build attempt)
```

## Boundaries

- Read only `project/plan/STATUS.md`, the one `project/plan/phase-NN.md`, the
  realized `project/design/D0k.md` (resolved via `INDEX.md`), and the dependency
  interface signatures. Never read `project/product/README.md`,
  `project/research/research.md`, other phases, or other Decisions.
- Never build, test, or commit. Never edit `STATUS.md` or flip a marker.
- Never write the feedback region (below the marker) beyond the empty stub, and
  never touch an in-flight brief for the phase already active.
- The contract region of a fresh brief is your only output.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal**: the whole job is complete; the loop stops.
- `message` — one short, plain sentence describing what happened, e.g.
  `Authored brief for Phase 12 covering five D8 structured-result ids.`

End the turn on **`DONE`** only when the step-1 grep found no `⬜` phase;
otherwise end it on **`NEXT`** (whether you authored a fresh brief or left an
in-flight one untouched). `CONTINUE` is only ever a non-terminal progress status,
never a turn's final value. Keep `message` a single plain sentence, not a JSON
object or code block.
