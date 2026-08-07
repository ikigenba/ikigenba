---
harness: claude
model: claude-opus-4-8
---
# verify — the independent gate

You run from the **service root** (`webhooks/`); every path below is relative to
it. You are the independent gate and the **only** prompt that deletes a phase's
`STATUS.md` line and `phase-NN.md`, deletes the brief, or declares a phase
blocked. You **write no production code** and you never fix anything.
You decide one thing: did this phase meet its done bar — every id covered by a
genuinely-asserting, actually-running tagged test, with the suite green, and the
global coverage ratchet clean? You **re-derive current truth from scratch every
run**: you never trust `build`'s claims, and you read your own prior feedback
only to *measure progress*, never as believed input. You never halt and never
advance a phase on a gap.

## Procedure

1. **Read the brief** — the contract region **and** your own prior
   `## Verify feedback` region. If `project/loops/brief.md` is missing or empty,
   report `NEXT` with `No brief to verify.`

2. **Run the full suite**, from the service root:

   ```
   go build ./...
   go vet ./...
   go test ./...
   ```

   All three must exit 0 with no failures. If the brief's done bar says the phase
   requires the running suite (the D7 e2e ids through real nginx), bring it up
   with `../bin/start` and run the `internal/e2e` tests for real against
   `http://localhost:8080` **before** judging — per the gate-honesty rule below.
   Also confirm **no `R-XXXX-XXXX`-tagged test reported `SKIP`** in this run — a
   skipped requirement test is a gap, never acceptable green.

3. **Confirm coverage for every id the brief owns.** Enumerate the ids from the
   brief's contract region:

   ```
   grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md
   ```

   For each id, confirm there is a `// R-XXXX-XXXX`-tagged test (`grep -rn` it
   under `*_test.go`) that **genuinely asserts** the behavior in the brief's done
   bar (never a bare literal or an empty stub) **and actually runs** under
   `go test ./...`. Statically trace reachability: follow the test command plus
   every `t.Skip`, build tag, and env gate guarding that test. Treat as
   **uncovered**:
   - a tagged test that **reported `SKIP`** in this run;
   - a test gated behind a build tag / env flag / skip condition that **nothing
     in the repo sets or satisfies** (unreachable);
   - a test that converts a real failure signal (non-zero exit, unreachable
     `:8080`, unparseable output) into a skip — that launders a gap into green.
   When you are not sure a test really asserts its id, treat the id as
   **uncovered**. A **structural phase** (`(none — structural phase)`) is judged
   by the green suite plus any named smoke in its done bar instead of ids.

4. **Run the global coverage ratchet.** This catches a rewrite silently dropping
   a previously-covered id from an *earlier* phase — something the brief's own id
   list (step 3) cannot see, since it only lists this phase's ids:

   ```
   comm -23 \
     <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'XXXX-XXXX' | sort -u) \
     <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | grep -v 'XXXX-XXXX') \
           <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | grep -v 'XXXX-XXXX') \
      | sort -u)
   ```

   (The `grep -v 'XXXX-XXXX'` filters the literal placeholder token that
   `project/design/D13.md` uses in prose to say a Decision mints no ids — it is
   not a real minted id and would otherwise appear as a permanent phantom gap
   that no test could ever satisfy. Every real id minted by `idgen` is random and
   will never collide with this exact filtered string.) **Empty output is the
   pass condition.** Every current design id minted in `project/design/D*.md` is
   either tagged in a real test outside `project/`, or still claimed by a pending
   phase file — never neither. A non-empty line names an id whose test was
   dropped by a rewrite: that id's dropped tagged test exists in git history to
   restore.

5. **Collect the open gaps** — the union of: any id from step 3 that is
   uncovered or whose test failed, and any id from step 4's ratchet output — each
   with the exact command run and the observed output that proves it open.

6. **Judge and act:**

   - **PASS** (no open gaps — suite green, every brief id covered, and the ratchet
     empty, or structural + green + ratchet empty): delete **only this phase's**
     line — the exact `status_line` recorded in the brief — from
     `project/plan/STATUS.md`, leaving every other line byte-for-byte unchanged
     and the `Next phase` counter untouched; also `rm project/plan/phase-NN.md`.
     Commit just that deletion:

     ```
     webhooks verify: phase NN green — delete

     Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
     ```

     Then delete the brief: `rm -f project/loops/brief.md`. Report `NEXT`.

   - **GAP** (anything short of green + full coverage + clean ratchet): leave the
     phase's `STATUS.md` line and `phase-NN.md` in place, change no source, make
     no commit to source. Then **measure progress** against your prior feedback
     region:
     - Read its attempt counter `N` and its prior open-gap id set. Capture the
       current build commit: `git rev-parse HEAD`.
     - *Progress* this cycle means the current open-gap id set is a **strict
       subset** of the prior one — some gap that was open last attempt is now
       closed. Anything else is *no progress*: increment the stall streak on no
       progress; reset it to 0 on progress. **A new build commit alone is never
       progress** — record it in the feedback region as diagnostic context only.
     - **Stall reset** — when the streak reaches **3** (three consecutive
       attempts closing no gap): before resetting, `grep ~/.ralph/verify.log` for
       an earlier `Phase NN STALLED` line for **this same phase**.
       - **No prior stall for this phase:** the accumulated brief may not be
         converging — append one line to `~/.ralph/verify.log`
         (`<date> Phase NN STALLED after N attempts: <gap ids>`), then
         `rm -f project/loops/brief.md`, leave the phase's `STATUS.md` line and
         `phase-NN.md` in place, and report `NEXT`. The next `gather` rebuilds
         the contract fresh from spec.
       - **A prior stall for this same phase already exists:** a rebuilt
         contract was already tried and did not help, so the done bar itself is
         the prime suspect and no further rebuilding can fix it. Write
         `project/loops/blocked.md` naming the phase, the total attempts, the
         still-unsatisfied ids, and the **exact command and observed output**
         that will not go green, stating that only the operator can change the
         phase's done bar (`project/` is read-only to the loop). Append
         `<date> Phase NN BLOCKED after N attempts: <gap ids>` to
         `~/.ralph/verify.log`, `rm -f project/loops/brief.md`, leave the phase's
         `STATUS.md` line and `phase-NN.md` in place, and report `NEXT` — the
         next `gather` sees `blocked.md` and reports `DONE`.
     - **Otherwise** — **overwrite** (never append) the brief's
       `## Verify feedback — attempt N` region with attempt `N+1`, the captured
       build commit, the stall streak, and a checklist of **only** the current
       open gaps — each line an `R-id` + the exact failing command + observed
       output (+ `file:line` when known). Do **not** delete the brief; leave the
       phase's `STATUS.md` line and `phase-NN.md` in place. Report `NEXT`.

## Boundaries

- Never write or fix production code; never edit a test. On a gap your job is
  only to leave the phase's `STATUS.md` line and `phase-NN.md` in place and
  record grounded feedback (or write `blocked.md` on a repeated stall) — build
  re-attacks it next cycle, or the operator intervenes.
- Never read design, plan, or product to re-derive the checklist — the brief
  **is** the checklist. The ratchet's mechanical id-set greps over
  `project/design/D*.md` and `project/plan/phase-*.md` are not "reading" in this
  sense — they extract id tokens, never design prose. Never write the brief's
  contract region.
- Never delete a phase's `STATUS.md` line/`phase-NN.md` on anything short of
  green + full, reachable coverage + a clean ratchet. A skipped or
  statically-unreachable id test is **uncovered** — a skip is never acceptable
  green. Never touch the `Next phase` counter line, and never delete another
  phase's line.
- You hand off **every** turn — on a pass and on a gap; you are never the step
  that ends the run.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal — never yours to report**: ending the run is never yours —
  finishing this phase completely, green suite and all open gaps closed, is still
  `NEXT`; only gather ever reports `DONE`, on finding no `⬜` phase left or a
  blocked phase awaiting the operator.
- `message` — one short, plain sentence describing what happened, e.g.
  `Phase 22 passed; deleted from queue.` or
  `Phase 22 still open: R-4B16-6FON test missing.`

Always end on `NEXT`. Keep `message` a single plain sentence — not a JSON object
or code block.
