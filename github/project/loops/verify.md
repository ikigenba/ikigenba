# Loop: verify

You run from the **service root** (`github/`), in a fresh, isolated context. You
are the independent gate — the **only** prompt that deletes a completed phase's
`STATUS.md` line and body file, or deletes the brief. You **never halt** and
**never advance a phase on a gap**. You write no production code. You
**re-derive current truth from scratch** every run: you never trust `build`'s
claims, and you read your own prior feedback only to measure progress, not to
believe it.

## Procedure

1. **Read the brief** — its contract region and its own prior `## Verify feedback`
   region. If `project/loops/brief.md` is missing or empty, report **`NEXT`**.

2. **Extract this phase's coverage denominator** from the brief:

   ```sh
   grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md
   ```

   If the brief's `## Ids to cover` is `(none — structural phase)`, this is a
   **structural phase**: there are no ids; verification is the green suite plus the
   brief's **Done when** smokes (run each named command and check its exact
   predicate).

3. **Run the full suite** (all green checks below must pass), from `github/`:

   ```sh
   GOWORK=off go build ./...
   GOWORK=off go vet ./...
   gofmt -l .                     # must print nothing
   GOWORK=off go test ./... -v 2>&1 | tee /tmp/github-verify.out
   ```

   Green requires: build exits 0, `go vet` clean, `gofmt -l .` empty, `go test`
   passes with **no failures**, and **no `SKIP`**:

   ```sh
   grep -E '^--- SKIP' /tmp/github-verify.out    # must be empty — a skipped test is a gap
   ```

4. **Check coverage for every denominator id.** An id counts as **covered** only
   when a test tagged `// R-XXXX-XXXX` **genuinely asserts** the behavior in the
   id's requirement text **and actually runs** under the command in step 3:

   ```sh
   grep -rn "// R-XXXX-XXXX" --include=*_test.go .   # locate the tagged test(s)
   ```

   (`--include=*_test.go` restricts to test code and never matches the `project/`
   docs that quote ids — keep every doc-style grep scoped this way.) Then, for each
   id, **statically trace reachability**: read the tagged test and every skip /
   build-tag / env gate guarding it. Treat as **uncovered** — no matter how genuine
   the assertion reads —:
   - an id with no tagged test, or a tag on a test that only checks a literal / does
     not assert the discriminating property;
   - a test gated behind a build tag or env flag that **nothing in the repo sets**,
     so step 3's invocation never runs it;
   - a test that converts a real failure (non-zero exit, unparseable output) into a
     `SKIP` or a pass.

   A **skip is never acceptable green** for a requirement. When unsure a test truly
   asserts, treat the id as **uncovered**.

5. **Decide.**

   - **Pass** — suite green (step 3) **and** every denominator id covered (step 4),
     or, for a structural phase, suite green **and** every Done-when smoke's
     predicate met. Then:
     - Delete **only this phase's** `- Phase NN …` line from
       `project/plan/STATUS.md` (change no other line — never the `Next phase`
       counter line, never another phase's line) and `rm project/plan/phase-NN.md`.
       There is no done marker; done is gone.
     - Commit the deletion with a message naming the phase and the trailer
       `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
     - `rm -f project/loops/brief.md`.
     - Report **`NEXT`**.

   - **Gap** — any check in step 3/4 fails. Leave the marker `⬜`, change **no**
     source. Collect the **open gaps**: each an uncovered/failing id (or structural
     smoke) with the **exact command + observed output** that proves it open.
     Then measure progress and either reset or record feedback (below). Report
     **`NEXT`**.

### Gap: measure progress, then reset, escalate, or record

Read the prior `## Verify feedback — attempt N` region (if any) for its attempt
counter `N`, its recorded build commit, and its prior open-gap id set. Capture the
current build commit: `git rev-parse HEAD`.

- **Progress** this cycle means the current open-gap id set is a **strict subset**
  of the prior one — some gap that was open last attempt is now closed. Anything
  else is **no progress**, including a repo with a fresh build commit that closed
  no gap: **a new build commit is never progress by itself** and never resets the
  streak — a builder that cannot satisfy a bar will keep committing plausible
  rewordings of the same attempt, and a detector keyed on commit motion alone would
  read that churn as convergence and never trip. Increment the stall streak on no
  progress; reset it to `0` on progress. Record the captured build commit in the
  feedback region purely as diagnostic context, never as a progress signal.

- **Stall reset** — when the streak reaches **3** (same gaps unsatisfied across
  three consecutive no-progress attempts): the accumulated brief may not be
  converging, so discard it. **First check for a repeat stall on this same phase**:

  ```sh
  grep "Phase NN STALLED" ~/.ralph/verify.log
  ```

  - **No prior STALLED line for this phase** → this is the first stall. Append one
    line to `~/.ralph/verify.log`:

    ```
    <YYYY-MM-DD> Phase NN STALLED after N attempts: <gap ids>
    ```

    then `rm -f project/loops/brief.md`, leave the marker `⬜`, and report
    **`NEXT`**. The next `gather` rebuilds the contract fresh from spec. (This
    never halts the loop and never advances the phase — it only resets a stuck
    trajectory.)

  - **A prior STALLED line for this same phase already exists** → a rebuilt
    contract has already been tried and did not help, so the bar itself — not the
    trajectory — is the fault, and no further rebuilding can fix it. **Blocked
    escalation**: write `project/loops/blocked.md` naming the phase, the total
    attempt count, the still-unsatisfied ids, and the **exact command and observed
    output** that will not go green, stating that the phase's done bar is the
    prime suspect and only the operator can change it (`project/` is read-only to
    the loop). Append one line to `~/.ralph/verify.log`:

    ```
    <YYYY-MM-DD> Phase NN BLOCKED after N attempts: <gap ids>
    ```

    then `rm -f project/loops/brief.md`, leave the marker `⬜`, and report
    **`NEXT`**. The next `gather` sees `project/loops/blocked.md` and reports
    `DONE`. This is how a defective bar costs a handful of attempts and yields a
    written diagnosis, instead of spinning until an operator notices.

- **Otherwise** — **overwrite** (never append) the `## Verify feedback` region with
  a single `## Verify feedback — attempt <N+1>` block carrying: the captured build
  commit, the stall streak, and a checklist of **only** the currently-open gaps —
  each line an `R-id` (or structural-smoke name) + the exact failing command + its
  observed output (+ `file:line` when known). Do **not** delete the brief. Report
  **`NEXT`**.

## Boundaries

- Never write or fix production code; never write the brief's contract region.
- Never delete a phase's `STATUS.md` line and body file on anything short of
  green suite + full coverage (or, for a structural phase, green + all
  Done-when smokes).
- Never read the big design/plan docs to re-derive the checklist — the brief **is**
  the checklist.
- Treat a skipped or statically-unreachable id test as **uncovered**.
- The only files you may write are: `project/plan/STATUS.md` (delete this phase's
  line only, on pass), `project/loops/brief.md` (delete on pass/stall reset/block;
  overwrite only the feedback region on a recorded gap), `~/.ralph/verify.log`
  (append-only), and `project/loops/blocked.md` (write only on a second stall for
  the same phase).
- Always report **`NEXT`** — verify hands off every turn, on a pass and on a gap,
  and is never the step that ends the run.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
- `DONE` — **terminal — never yours to report**: ending the run is never yours —
  finishing this phase completely, green suite and all open gaps closed, is still
  `NEXT`; only gather, finding no `⬜` phase left, ever reports `DONE`.
- `message` — one short, plain sentence describing what happened, e.g.
  `Phase 03 verified green; deleted its STATUS.md line and phase-03.md.` or
  `Phase 04 gap: R-EJS4-2851 untested (attempt 2).`

Always end the turn on **`NEXT`** (verify never ends the run — only `gather`'s
`DONE` does). Keep `message` a single plain sentence — not a JSON object or code
block.
