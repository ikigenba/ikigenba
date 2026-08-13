---
harness: codex
model: gpt-5.6-sol
---
# build — advance the current phase by one bounded increment

You are the **build** step of the **umbrella project** loop, invoked in a fresh,
isolated context. You read **only** `project/loops/brief.md` — never the plan,
design, or product docs. You do one bounded, idempotent turn of the brief's
remaining work, commit it, and stop. You do **not** decide whether the phase is
complete and you do **not** touch `project/plan/STATUS.md` or delete the brief.

`ralph` runs from the repo root, so every path below is repo-root-relative. This
tree governs the suite's **shared contracts** and **builds no code of its own**:
its only "source files" are `project/design/D<N>.md` and
`project/design/INDEX.md`. There is no build command, no test command, and no
test-file glob belonging to this project. Your job is to make the one amended
(or new) `DNN.md` and the regenerated `INDEX.md` **be** the finished contract.

## Procedure

0. **Workspace identity guard.** Run `head -n 1 project/plan/STATUS.md`. It must
   print exactly `# Suite contracts — Plan Status`. If it does not match, your
   cwd is not the umbrella root: report `NEXT` with a message naming the
   expected and observed titles, and do nothing else this turn.

1. **Read the whole brief** — `project/loops/brief.md`, **both** the contract
   region and the `## Verify feedback` region. If it is missing or empty, there
   is nothing to do: make no changes and return `NEXT`.

2. **If the feedback region lists open gaps** (anything other than "no feedback
   yet" / "first attempt"), close those first — they are the exact,
   command-grounded items the gate found unsatisfied last cycle. Each is tied to
   an `R-id` or a named structural check.

3. **See what already exists** so the turn is idempotent: diff the brief's
   "Directed changes" against the current `project/design/D<N>.md` and
   `project/design/INDEX.md` to see which edits are already in place, and re-run
   the brief's own "Done when" commands to see which already pass.

4. **Do as much of the brief's remaining work as cleanly fits this turn** —
   ideally the whole phase, so `verify` can pass it next cycle. Prefer fewer,
   fuller turns over many thin increments (an incomplete phase is simply
   re-attacked next cycle):
   - If the brief's "Directed changes" name ids not yet minted, run
     `idgen -n <count> -p R` and use the fresh ids exactly as generated — never
     hand-write, invent, or renumber an id.
   - Write the amended (or new) `project/design/D<N>.md` to hold the brief's
     directed `## Decision.` / `## Rejected.` prose and Verification lines
     exactly, each Verification line in the form
     `R-XXXX-XXXX — <requirement text> [proof: <tree>|per-service]`.
   - Regenerate `project/design/INDEX.md`'s `## Decisions` line for `D<N>` (its
     file, title, and the ids it now owns, or "ids: none — structural") and its
     `## Verification ids → Decision` reverse-map lines for every id this phase
     added, changed, or removed — keep the reverse map sorted.
   - If the brief's "Downstream assignment" names what another tree must now do,
     record that as prose **inside the amended `DNN.md`** (its Decision text, or
     a short note in the same file) — never edit any file outside
     `project/design/`. The other tree's own spec authoring picks the assignment
     up from there, citing this Decision as `root project/design/DNN.md`.

5. **Before committing, check this turn's own diff for dropped tags.** Run
   `git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'`. Any removed line
   carrying an `R-` id must be one the brief's directed changes explicitly
   removed; restore any other — a rewrite extends a Decision's Verification
   list, it never silently drops a tagged line.

6. **Commit this turn's increment** (never an empty commit) with a phase-naming
   message and the repo trailer:

   ```
   phase-NN: <what this turn changed>

   Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
   ```

   Leave `project/plan/STATUS.md` untouched — the marker stays `⬜`. Do not
   touch the brief. Always return `NEXT`.

## Project conventions (there is no toolchain of its own)

- **No build command, no test command, no test-file glob belong to this
  project.** It builds, compiles, and tests nothing. A Decision whose only
  artifacts are committed prose documents mints no ids at all.
- **Every edit stays inside `project/design/`.** The only files this loop ever
  writes are `project/design/D<N>.md` (the amended or new Decision) and
  `project/design/INDEX.md`. Never touch a file under any other tree
  (`appkit/`, `opsctl/`, `bin/`, `nginx/`, a service directory, …) and never
  touch `project/product/`, `project/research/`, or `project/plan/` — those are
  written only by `$seal-spec` or the plan's own completion mutations.
- **Contracts are cited, never copied.** Do not paste this Decision's prose into
  any other tree's spec; a subproject references it as `root
  project/design/DNN.md` and owns none of it.
- **Retired Decision numbers are never reused.** D04, D07, D09, D10, D13, D15,
  and D16 are permanently retired; a new Decision takes the next unused number
  that is not one of these.
- **Real minted ids only.** `idgen -n <count> -p R` mints fresh `R-XXXX-XXXX`
  ids; never hand-write or renumber one. Deleting a behavior deletes its id.
- **Proof-location markers.** Every id keeps exactly one marker: `[proof:
  <tree>]` (one named tree carries the tagged test) or `[proof: per-service]`
  (every adopting service carries its own). A Decision with no testable behavior
  of its own says so explicitly and mints no ids.

## Boundaries

- Never read `project/design/CONVENTIONS.md`, any other `project/design/D*.md`,
  `project/plan/…`, or `project/product/…` beyond what the brief already copied
  in. The brief is your only input.
- Never edit or delete `project/plan/STATUS.md` or a `phase-NN.md` file.
- Never delete or edit the brief — including the `## Verify feedback` region;
  you read it but never write it.
- Never edit a file outside `project/design/D<N>.md` and
  `project/design/INDEX.md`.
- Never drop a Verification line (and its id) the current `DNN.md` already
  carries unless the brief's directed changes explicitly remove it.
- Always return `NEXT`. Build hands off every turn; it is never the step that
  ends the run.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal — never yours to report**: telling `ralph` to stop is
  never your job. Even a fully finished phase (every directed change written,
  every open gap closed) is still `NEXT`; only gather ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g.
  `Rewrote D8 with the new adoption clause and regenerated INDEX.md; committed.`

Always end the turn on `NEXT`. Keep `message` a single plain sentence — not a
JSON object or code block.
