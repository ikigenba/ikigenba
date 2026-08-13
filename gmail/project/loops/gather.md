---
harness: claude
model: claude-sonnet-5
---
# gather — select the next phase and author its brief (contract region only)

You are the **gather** step of the gmail build loop, invoked in a **fresh,
isolated context** with no memory of prior turns. All state lives in files under
the gmail service root, which is your working directory. This is **one turn**:
do the procedure once and report. Do not loop internally, and prefer making
progress over asking questions — nobody is watching.

You are the **only** step that reads the big docs (`project/plan/`,
`project/design/`, `project/product/`), the **only** step that owns the brief's
**contract region**, and the **only** step that can ever end the run. You write
no code, run no tests, and commit nothing.

## Step zero — workspace identity guard

Run `head -n 1 project/plan/STATUS.md`. It must print exactly:

```
# gmail — Plan Status
```

- If it matches, continue.
- If it does not match (or the file is missing) but `./gmail/project/plan/STATUS.md`
  passes the same check, your cwd drifted one level up — `cd gmail` and continue.
- Otherwise, do not proceed and do **not** report `DONE`. Report `NEXT` with a
  message naming the expected title (`# gmail — Plan Status`) and what you
  actually observed, so the drift is visible instead of silently misdirecting
  the run.

## Procedure

1. **Blocked check.** If `project/loops/blocked.md` exists, open no other file
   and report **`DONE`** with a message naming the blocked phase and pointing at
   `project/loops/blocked.md`.

2. **Find the next phase.** Run:

   ```
   grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1
   ```

   If this returns nothing, no pending phase remains. Report **`DONE`** with a
   message like "no pending phases".

3. **Check for an in-flight brief.** If `project/loops/brief.md` exists, read its
   `# Brief — Phase NN` header.
   - If it names the **same** phase found in step 2, the phase is mid-flight:
     leave the brief exactly as is (both the contract region and the feedback
     region untouched), open no big doc, and report **`NEXT`**.
   - If it names a phase whose `STATUS.md` line no longer exists (the phase
     completed and was retired), proceed to step 4 to author a fresh brief.
   - If there is no brief at all, proceed to step 4.

4. **Author a fresh brief.** Read only:
   - `project/plan/phase-NN.md` for the phase found in step 2 (the phase body —
     its objective and the ids it lists under `Done when`);
   - the Decision(s) that phase realizes, resolved through
     `project/design/INDEX.md` (grep the id or Decision number, then read only
     those `project/design/DNN.md` files);
   - the dependency packages' public interface signatures the phase needs
     (read only the relevant `.go` files' exported declarations, not whole
     packages).

   Determine the **ids to cover**: only the ids `phase-NN.md`'s `Done when`
   section lists — a slice of a Decision's full Verification list, never all of
   it. If the phase is structural (owns no ids), write `(none — structural
   phase)` instead.

   Write `project/loops/brief.md` with this schema:

   ```
   # Brief — Phase NN

   ## Objective
   <one-line objective copied/paraphrased from phase-NN.md>

   ## Realized Decision(s)
   - D<N> — project/design/D<NN>.md

   ## Design prose (verbatim, Verification list omitted)
   <full copied prose of each realized Decision: Decision statement,
   shape/signatures, rejected alternatives — omit that Decision's
   "Verification" section entirely>

   ## Ids to cover
   R-XXXX-XXXX — <full requirement text copied verbatim from the Decision's
   Verification list, one id per line, id at line-start>
   ...
   (or: (none — structural phase))

   ## Files to touch
   <paths named or implied by phase-NN.md>

   ## Dependency interface signatures
   <copied exported signatures the phase must consume>

   ## Done bar
   <the phase's Done-when condition(s) verbatim, including "suite is green":
   `cd gmail && go build ./...`, `cd gmail && go vet ./...`,
   `cd gmail && gofmt -l .` (no output), `cd gmail && go test ./...` all
   succeed with zero failures, and every listed id is covered by a genuinely
   asserting `// R-XXXX-XXXX`-tagged test co-located with the code it exercises
   (package-local `*_test.go`, named for the behavior — never a per-phase or
   root-level test file) that actually runs under `go test ./...`.>

   ## Verify feedback — attempt 0
   (empty — no prior attempt)
   ```

   Report **`NEXT`**.

## Boundaries

- Read only the one phase file, its realized Decision file(s), and the
  dependency interfaces named above — never the whole `project/design/` or
  `project/plan/` tree.
- Never build, test, or commit anything.
- Never write the brief's feedback region, and never touch an in-flight brief's
  contract region once it exists for the active phase.
- `gmail`'s toolchain: Go 1.26, module `gmail`, workspace `GOWORK` mode (repo
  root `go.work`). Build/vet: `cd gmail && go build ./...` and
  `cd gmail && go vet ./...`. Test: `cd gmail && go test ./...`. Format:
  `cd gmail && gofmt -l .` (must print nothing).

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal**: tells `ralph` to stop the loop. It carries no other
  meaning; say *why* in the message — e.g. "no pending phases" or "blocked on
  Phase 12, see project/loops/blocked.md".
- `message` — one short, plain sentence describing what happened, e.g.
  "Authored brief for Phase 14 (D9 — web surface from share/www)."

Report `DONE` only in the two cases above (no pending phase, or a
`blocked.md` on sight); every other case ends on `NEXT`. Keep `message` a
single plain sentence, not a JSON object or code block.
