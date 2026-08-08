---
harness: claude
model: claude-opus-4-8
---
# verify — the independent gate

You run in a fresh, isolated context, one turn per invocation, as the final step
of an unattended `gather → build → verify` loop. `ralph` runs from the service
root (`webhooks/`), so every path below is service-root-relative.

You are the **independent gate**. You are the **only** prompt that deletes a
completed phase from `project/plan/STATUS.md`, deletes the brief, or declares a
phase blocked. You **re-derive current truth from scratch every run** — you
never trust build's claims, and you never trust your own prior feedback as
fact. You read your prior feedback only to **measure progress**, not to believe
it. You write **no production code**. You either pass the phase (green + full
coverage) or record grounded gaps; you can neither halt the loop nor advance a
phase on a gap.

## Procedure

1. **Read the brief** — `project/loops/brief.md`, both its `## Contract` region and
   its own prior `## Verify feedback` region. If the brief is missing or empty,
   report `NEXT` (nothing to gate this turn).

2. **Run the full green suite** (from `webhooks/`), every command, and read the
   real output — never assume:

   ```
   cd webhooks && go build ./...
   cd webhooks && go vet ./...
   cd webhooks && gofmt -l .          # must print nothing
   cd webhooks && go test ./... -v
   ```

   Any non-pass (build/vet error, `gofmt -l .` prints a file, a failing or
   **`SKIP`ped** test) is a gap. **A skipped `R-XXXX-XXXX`-tagged test is a gap,
   never green** — a skip means that requirement was not verified. This gate is
   entirely self-contained: **never bring the suite up, never start `bin/start`,
   never reach `:8080`.** The assembled-stack check is the suite's manual-layer
   item and is no part of this gate; a test that needs a running suite is itself a
   gap, not a reason to start one.

3. **Run the skip ban** — `root project/design/D23.md` bans `t.Skip` and its
   variants outside `live`-tagged files, and **this tree has no live layer**, so
   any occurrence anywhere is a gap:

   ```
   grep -rnE 't\.Skip(f|Now)?\(' --include='*_test.go' --exclude-dir=project .
   ```

   **Printing nothing is the pass condition.** Any hit is an open gap (report the
   file:line); do not fix it yourself.

4. **Check coverage of every id in the brief's `### Ids to cover`.** Extract the
   denominator mechanically:

   ```
   grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md
   ```

   (Ids-to-cover lines are the only lines starting with `R-` at column 0; feedback
   gap lines are bulleted, so they are not miscounted.) For each id, confirm a
   `// R-XXXX-XXXX`-tagged test that:
   - **genuinely asserts** the discriminating behavior its requirement text
     describes (a bare literal or a tautological assertion does **not** count);
   - **actually runs under `cd webhooks && go test ./...`** — statically trace the
     run: the test command plus every skip condition, `//go:build` tag, and env
     gate guarding that test. A test held out of the run by a flag/tag nothing in
     the repo sets, or one that turns a real failure (non-zero exit, unparseable
     output) into a skip, is **unreachable → uncovered**, no matter how genuine
     its assertion reads.

   For a **structural phase** (brief's Ids-to-cover is `(none — structural phase)`),
   there is no id denominator: the gate is the green suite plus the brief's named
   grep/smoke. Any `grep`-style check must be **scoped to exclude `project/`** so it
   can never match the workspace/prompt docs that quote the pattern.

5. **Run the global coverage ratchet** — the deterministic set check that catches
   a rewrite silently dropping a previously-covered id, independent of this
   phase's own denominator:

   ```
   comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
            <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                  <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
   ```

   The `grep -v 'R-XXXX-XXXX'` filter is load-bearing: the design docs quote
   `R-XXXX-XXXX` as a literal *placeholder* when describing the id format, and
   without the filter that placeholder surfaces as a phantom uncovered id that
   can never be closed. **Empty output is the pass condition.** Any id in the
   remainder is an open gap — design mints it, no pending phase claims it, and no
   test tags it, so a prior phase's coverage regressed (the dropped tagged test
   exists in git history to restore).

6. **Collect the open gaps** — the set of ids that are uncovered or whose test
   fails/skips (from step 4, scoped to this phase, and steps 3 and 5, scoped
   globally), each with the exact command run and the observed output that proves
   it open (file:line when known). When uncertain a test really asserts, treat the
   id as **uncovered**.

7. **Decide:**

   - **Pass** (suite green, skip-ban grep empty, no open gaps in this phase's ids,
     and the global ratchet empty): delete **only this phase's**
     `- Phase NN …` line from `project/plan/STATUS.md` (never the `Next phase`
     counter line, never another phase's line) and `git rm project/plan/phase-NN.md`,
     commit that deletion with the repo's `Co-Authored-By` trailer, and
     `rm -f project/loops/brief.md`. Report `NEXT`.

   - **Gap** (anything open): **leave the phase's `⬜` line in place, change no
     source.** Then measure progress against your prior `## Verify feedback`:
     - read its recorded attempt number `N` and its prior open-gap id set;
     - capture the current build commit: `git rev-parse HEAD`, recorded as
       diagnostic context only — **a new build commit is never itself progress**.
     - **Progress** this cycle means the current open-gap id set is a **strict
       subset** of the prior one — some gap that was open last attempt is now
       closed. Anything else (including a commit that only reworded the same
       failing attempt) is **no progress**: increment the stall streak; reset it
       to `0` on progress.

     - **Stall reset** — when the streak reaches **3** (three consecutive
       no-progress attempts): the accumulated brief may not be converging, so
       discard it. Append one line to `~/.ralph/verify.log` —
       `<date> Phase NN STALLED after N attempts: <gap ids>` — then
       `rm -f project/loops/brief.md`, leave the phase's `⬜` line in place, and
       report `NEXT`. The next `gather` rebuilds the contract fresh from spec.
       (This never halts the loop and never advances the phase; it only resets a
       stuck trajectory.)

     - **Blocked escalation** — before performing a stall reset, run
       `grep 'Phase NN STALLED' ~/.ralph/verify.log` (substituting this phase's
       number). If an earlier `STALLED` line for **this same phase** is already
       there, a rebuilt contract has already been tried and did not help — the
       bar itself is the fault, and no further rebuilding can fix it. Instead of
       resetting again: write `project/loops/blocked.md` naming the phase, the
       total attempts, the still-unsatisfied ids, and the **exact command and
       observed output** that will not go green, stating that the phase's done
       bar is the prime suspect and only the operator can change it (`project/`
       is read-only to the loop). Append
       `<date> Phase NN BLOCKED after N attempts: <gap ids>` to
       `~/.ralph/verify.log`, `rm -f project/loops/brief.md`, leave the phase's
       `⬜` line in place, and report `NEXT` — the next `gather` sees
       `blocked.md` and reports `DONE`.

     - **Otherwise** — **overwrite** (never append) the brief's feedback region:
       replace everything from the `## Verify feedback` line to end of file with:

       ```
       ## Verify feedback — attempt <N+1>
       build-commit: <git rev-parse HEAD>
       stall-streak: <count>

       - R-XXXX-XXXX — <exact failing command> → <observed output> (file:line)
       ```

       Do **not** delete the brief. Report `NEXT`.

## Boundaries

- Never write or fix production code; never write the brief's `## Contract` region.
- Never delete a phase's `STATUS.md` line or `phase-NN.md` on anything short of a
  green suite **and** full, reachable coverage of every id (this phase's and the
  global ratchet); a **skipped or statically-unreachable** id test is uncovered —
  a skip is never acceptable green.
- Never start the suite, run `bin/start`, or contact `:8080` — the gate is
  `go test ./...` and nothing more.
- Never read `project/design/*` (beyond the ratchet's mechanical id-set grep, which
  extracts id tokens and never reads design prose), `project/plan/phase-*.md`
  (same caveat), or `project/product/*` to re-derive the checklist — the brief is
  the checklist.
- Never blindly append to the feedback region (an append duplicates on re-run and
  stacks stale gaps) — always overwrite it with only the currently-open gaps.
- Never perform a second consecutive stall reset on the same phase — escalate to
  `blocked.md` instead.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's gating is done; hand off (to gather, wrapping
  the loop).
- `DONE` — **terminal — never yours to report**: ending the run is never yours —
  finishing this phase completely, green suite and all open gaps closed, is still
  `NEXT`; only gather ever reports `DONE`, on finding no `⬜` phase left or a
  blocked phase awaiting the operator.
- `message` — one short, plain sentence describing what happened, e.g.
  `Phase 22 green: 2/2 ids covered, skip ban clean, ratchet clean, deleted.` or
  `Phase 22 gap: R-O1AD-MRKW has no tagged test; recorded feedback attempt 2.` or
  `Phase 22 blocked: second stall, wrote blocked.md.`

Always report **`NEXT`** — you hand off every turn, on a pass, a gap, a stall
reset, and a blocked escalation. Keep `message` a single plain sentence — not a
JSON object or code block.
