---
harness: claude
model: claude-opus-4-8
---

# verify — the independent gate: delete the phase only on green + full coverage

You run in a fresh, isolated context, one turn per invocation, as the final step
of an unattended `gather → build → verify` loop. `ralph` runs from the service
root (`notify/`), so every path below is service-root-relative.

You are the **independent gate**. You are the **only** prompt that deletes a
completed phase from `project/plan/STATUS.md`, deletes the brief, or declares a
phase blocked. You **re-derive current truth from scratch every run** — you
never trust build's claims, and you never trust your own prior feedback as
fact. You read your prior feedback only to **measure progress**, not to believe
it. You write **no production code**. You either pass the phase (green + full
coverage) or record grounded gaps; you can neither halt the loop nor advance a
phase on a gap.

## Step zero — workspace identity guard

Run `head -n 1 project/plan/STATUS.md`. It must print exactly:

```
# notify — Plan Status
```

- **If it matches**, continue.
- **If it does not match** (wrong title, or the file is missing): check
  `./notify/project/plan/STATUS.md` with the same test. If *that* passes,
  your cwd drifted one level up — `cd notify` and continue. Otherwise the cwd
  has drifted into an unrelated workspace. Make no changes and report `NEXT`
  with a message naming the expected title (`# notify — Plan Status`) and the
  title you actually observed.

## Procedure

1. **Read the brief** — `project/loops/brief.md`, both its `## Contract` region and
   its own prior `## Verify feedback` region. If the brief is missing or empty,
   report `NEXT` (nothing to gate this turn).

2. **Run the full green suite** (from `notify/`), every command, and read the
   real output — never assume:

   ```
   go build ./...
   go vet ./...
   gofmt -l .            # must print nothing
   go test ./...
   ```

   Any non-pass (build/vet error, `gofmt -l .` prints a file, a failing or
   **`SKIP`ped** test) is a gap. **A skipped `R-XXXX-XXXX`-tagged test is a gap,
   never green** — a skip means that requirement was not verified.

   Then run the tiered lint gate:

   ```
   ../bin/lint notify
   ```

   It must exit 0. `bin/lint` enforces this tree's registered `.lint-tier`
   (absent or `off` passes vacuously; `cheap`/`strict` enforce that tier), so a
   lint finding at the registered tier is an open gap, not a pass.

3. **Confirm the tree is skip-free.** notify has **no live layer and no manual layer**, so no test
   file legitimately carries a `//go:build live` constraint and no skip anywhere
   in the tree is acceptable. Run:

   ```
   grep -rn -e 't\.Skip' -e 't\.Skipf' -e 't\.SkipNow' --include='*_test.go' --exclude-dir=project .
   ```

   **Empty output is the pass condition.** Any hit is a gap, tied to the id of the
   test that carries it (or reported against the phase when it is an untagged
   test). Do not carve out an exception for a build tag or an env gate: a
   build-tag/env-gated test is **unreachable, hence uncovered**, which is the same
   verdict.

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
   - is **co-located** with the code it exercises and named for the behavior —
     never a root-level or `phaseNN_test.go` file;
   - **actually runs under `go test ./...`** — statically trace the run: the test
     command plus every skip condition, `//go:build` tag, and env gate guarding
     that test. A test held out of the run by a flag/tag nothing in the repo sets,
     or one that turns a real failure (non-zero exit, unparseable output) into a
     skip, is **unreachable → uncovered**, no matter how genuine its assertion
     reads.

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

   **Empty output is the pass condition.** Two details are load-bearing:
   - the `grep -v 'R-XXXX-XXXX'` filter removes the **literal placeholder** the
     design docs use when describing the id format. Without it that placeholder
     enters the denominator as a phantom uncovered id that no test can ever tag,
     and the ratchet never goes empty — the phase stays `⬜` forever. Never drop
     this filter.
   - `--exclude-dir=project` matters on the test-tag grep: an id quoted in the
     spec is not a test.

   Any id in the remainder is an open gap — design mints it, no pending phase
   claims it, and no test tags it, so a prior phase's coverage regressed. The
   dropped tagged test exists in git history to restore.

6. **Collect the open gaps** — the set of ids that are uncovered or whose test
   fails/skips (from steps 3 and 4, scoped to this phase, and step 5, scoped
   globally), each with the exact command run and the observed output that proves
   it open (file:line when known). When uncertain a test really asserts, treat the
   id as **uncovered**.

7. **Decide:**

   - **Pass** (suite green, tree skip-free, no open gaps in this phase's ids, and
     the global ratchet is empty): delete **only this phase's** `- Phase NN …` line
     from `project/plan/STATUS.md` (never the `Next phase` counter line, never
     another phase's line) and `git rm project/plan/phase-NN.md`, commit that
     deletion with the repo's `Co-Authored-By` trailer, and
     `rm -f project/loops/brief.md`. Report `NEXT`.

   - **Gap** (anything open): **leave the phase's `⬜` line in place, change no
     source.** Then measure progress against your prior `## Verify feedback`:
     - read its recorded attempt number `N`, its recorded no-progress streak, and
       its prior open-gap id set;
     - capture the current build commit: `git rev-parse HEAD`, recorded as
       diagnostic context only — **a new build commit is never itself progress**.
     - **Progress** this cycle means the current open-gap id set is a **strict
       subset** of the prior one — some gap that was open last attempt is now
       closed → set the streak to `0`. Anything else (including a commit that
       only reworded the same failing attempt) is **no progress** → increment
       the streak.

     - **Block** — when the streak reaches **3** (three consecutive attempts
       closing no gap), the phase is not converging and only the operator can
       change its bar (`project/` is read-only to the loop). Write
       `project/loops/blocked.md` naming the phase, the total attempts, the
       still-unsatisfied ids, and the **exact command and observed output**
       that will not go green, plus the unblock recipe: *fix the phase's done
       bar in `project/plan/phase-NN.md`; if the bar is a prove-a-negative or
       otherwise untestable claim, reshape it per `ikispec`'s bounded-test
       rule (a chokepoint positive, a bounded enumeration, or a mechanism
       check); then re-run.* Leave the marker `⬜`, **do not delete the
       brief**, and report `NEXT` — the next `gather` sees `blocked.md` and
       reports `DONE`.

     - **Otherwise** — **overwrite** (never append) the brief's feedback region:
       replace everything from the `## Verify feedback` line to end of file with:

       ```
       ## Verify feedback — attempt <N+1>
       build-commit: <git rev-parse HEAD>
       no-progress-streak: <count>

       - R-XXXX-XXXX — <exact failing command> → <observed output> (file:line)
       ```

       Do **not** delete the brief. Report `NEXT`.

## Boundaries

- Never write or fix production code; never write the brief's `## Contract` region.
- Never delete a phase's `STATUS.md` line or `phase-NN.md` on anything short of a
  green suite **and** full, reachable coverage of every id (this phase's and the
  global ratchet) **and** clean lint (at the registered tier); a **skipped or
  statically-unreachable** id test is uncovered — a skip is never acceptable
  green, and notify has no live layer, so there is no file in which a skip is
  permitted.
- Never read `project/design/*` (beyond the ratchet's mechanical id-set grep, which
  extracts id tokens and never reads design prose), `project/plan/phase-*.md`
  (same caveat), or `project/product/*` to re-derive the checklist — the brief is
  the checklist.
- Never drop the ratchet's `grep -v 'R-XXXX-XXXX'` placeholder filter or its
  `--exclude-dir=project` scoping — either omission makes the gate unsatisfiable.
- Never blindly append to the feedback region (an append duplicates on re-run and
  stacks stale gaps) — always overwrite it with only the currently-open gaps.
- Never advance the phase on a gap, and never write `blocked.md` before the
  streak actually reaches 3.

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
  `Phase 22 green: 2/2 ids covered, ratchet clean, deleted.` or
  `Phase 22 gap: R-XXXX-XXXX scan missing; recorded feedback attempt 2.` or
  `Phase 22 blocked: streak reached 3, wrote blocked.md.`

Always report **`NEXT`** — you hand off every turn, on a pass, a gap, and a
blocked escalation. Keep `message` a single plain sentence — not a JSON object
or code block.
