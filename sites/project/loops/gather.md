---
harness: claude
model: claude-sonnet-5
---

# gather — select the next ⬜ phase and author its brief

You run in a fresh, isolated context, one turn per invocation, as the first step
of an unattended `gather → build → verify` loop that builds sites one phase at a
time. `ralph` runs from the service root (`sites/`), so every path below is
service-root-relative.

You are the **only** prompt that reads the big spec docs, and the **only**
prompt that ever ends the run. Your job is to make sure `project/loops/brief.md`
holds a correct, self-contained contract for the **first unstarted phase** —
then hand off. You write **no code, run no tests, and commit nothing**. You own
only the brief's **contract region**; you never write its **feedback region**.

## Procedure

0. **Workspace identity guard.** Run `head -n 1 project/plan/STATUS.md`. It must
   print exactly `# sites — Plan Status`. If it does not (missing file, wrong
   title), check `./sites/project/plan/STATUS.md` for the same title: if that
   one matches, your cwd drifted one level up — `cd sites` and continue from
   step 1. Otherwise report `NEXT` with a message naming the expected title
   (`# sites — Plan Status`) and what you actually saw. Never report `DONE` on
   an identity mismatch.

1. **Blocked check.** If `project/loops/blocked.md` exists, open no other file
   and report `DONE` with a message naming the blocked phase and pointing at
   that file.

2. **Find the next pending phase.** Run
   `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`. If it prints
   nothing, every phase is built — report `DONE` with a message like "no
   pending phases; sites is fully built".

3. **Check for an in-flight brief.** If `project/loops/brief.md` exists, read
   its `# Brief — Phase NN` header.
   - If it names the **same** phase number found in step 2, the phase is
     mid-flight: leave the brief exactly as is (both regions untouched), open
     no design/plan doc, and report `NEXT`.
   - If it names a phase whose `STATUS.md` line is gone (the phase completed
     and was deleted), or the brief is absent, proceed to step 4 and author a
     fresh brief.

4. **Author the brief.** Read only:
   - `project/plan/phase-NN.md` for the phase found in step 2 (its body, its
     `Realizes design Decision …` line, and its `Done when` bar);
   - the realized Decision file(s) it names, resolved by number
     (`project/design/D<NN>.md`, zero-padded filename for the unpadded `D<n>`
     the phase names) — read the whole Decision's **Decision.** and
     **Rejected.** sections, never its Verification list wholesale;
   - `project/design/INDEX.md` only to resolve a Decision id to its file path
     if the phase doesn't already spell it out;
   - the public interface signatures of any package the phase's body names as
     a dependency (read just enough of that package's exported
     types/functions to copy the signatures — never its internals).

   Determine the **ids to cover**: only the ids the phase body / `Done when`
   bar lists (a slice of a Decision's Verification ids, possibly an adopted
   umbrella id like `R-O1AD-MRKW` cited `[proof: per-service]` — treat those
   exactly like local ids, they need a local tagged test too). Never copy a
   Decision's full Verification list if the phase owns only part of it.

   Write `project/loops/brief.md`:

   ```
   # Brief — Phase NN

   ## Contract (gather-owned — verify never writes here)

   Phase: NN — <one-line objective, copied from the phase file's header>
   Realizes: D<n> — <Decision title> (project/design/D<NN>.md)[, D<m> — ... ]

   ### Design prose (verbatim, Verification list omitted)

   <the realized Decision's full "Decision." and "Rejected." sections, copied
   verbatim, for each realized Decision>

   ### Ids to cover

   R-XXXX-XXXX — <full requirement text copied verbatim from the Verification list>
   R-XXXX-XXXX — <...>
   (or, for a structural phase: `(none — structural phase)`)

   ### Files to touch

   <the paths named in the phase body>

   ### Dependency interface signatures

   <exported types/functions of packages the phase depends on, copied verbatim
   — or "(none)">

   ### Done bar

   <the phase's "Done when" bar, copied verbatim: every id above covered by a
   genuine `// R-XXXX-XXXX`-tagged test co-located with the code it exercises
   (package-local, named for the behavior; a cross-package/boot-level check
   goes in `cmd/sites/main_test.go`, sites' single home for the composed
   layer — never a per-phase or root-level test file), plus:
   `cd sites && go build ./...`, `cd sites && go vet ./...`,
   `cd sites && gofmt -l .` (no output), and `cd sites && go test ./...` all
   green. If the phase's Decision touches the local D23 headless-browser
   wiring test, a `google-chrome` binary must be on `PATH` — its absence is a
   hard failure of the gate, never a skip. A structural phase (no ids) is done
   on the green build/vet/fmt/test bar alone plus any named smoke.>

   ## Verify feedback — attempt 0

   (no prior attempts)
   ```

5. Report `NEXT`.

## Boundaries

- Read only: this one phase's `phase-NN.md`, its realized Decision file(s),
  `INDEX.md` (id/Decision lookup only), and the exported-signature surface of
  named dependency packages. Never open another phase file, `product/README.md`,
  or an unrelated Decision.
- Never build, run tests, or commit.
- Never write the brief's `## Verify feedback` region; never touch an in-flight
  brief that already names the active phase.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal**: tells `ralph` to stop the loop. It carries no other
  meaning; say *why* in the message, e.g. `no pending phases; sites is fully
  built` or `blocked on Phase 12 — see project/loops/blocked.md`.
- `message` — one short, plain sentence describing what happened, e.g.
  `wrote brief for Phase 7 (realizes D9)`.

Report `DONE` only when step 2 finds no pending phase, or step 1 finds
`blocked.md`. Every other path (including the identity-mismatch case) ends on
`NEXT`. Keep `message` a single plain sentence, not a JSON object or code
block.
