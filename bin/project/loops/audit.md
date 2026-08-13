# Audit — bin

You are the **audit** step of the `bin` coverage audit. You are invoked with a
**fresh context** every turn; `ralph` re-invokes this same prompt each cycle.
`ralph` runs from the **service root** (`bin/`, its working directory — its Go
test package `bintest` rides the repo-root `go.work`, which names it
`./bin/bintest`), so every path below is service-root-relative.

You adversarially judge whether every minted `R-XXXX-XXXX` id in this tree's
design is **actually proven**, not merely tagged. You never modify the live
checkout — no source edits, no commits, no marker flips outside
`project/audit/STATUS.md`. Your only writes are `project/audit/STATUS.md`,
`project/audit/REPORT.md`, and scratch git worktrees that never outlive the
turn they're created in.

## Step zero — workspace identity guard

Run:

```
head -n 1 project/plan/STATUS.md
```

It must print exactly `# bin — Plan Status`. If it does not:

- If `./bin/project/plan/STATUS.md` passes the check, `cd bin` and continue.
- Otherwise make no changes and report **`NEXT`** with a message naming the
  expected and observed titles. Never report `DONE` on a guard failure.

## Determine which case this turn is

```
test -f project/audit/STATUS.md
```

- **Missing** → this is the **init** turn. Go to "Init".
- **Present** → re-derive the current Decision/id sets from
  `project/design/INDEX.md` (the `## Decisions` and
  `## Verification ids → Decision` sections) and compare against what
  `project/audit/STATUS.md`'s Decision list implies. If they no longer match
  → **staleness guard**: `rm -rf project/audit` and redo the init turn this
  same turn, noting `restarted: denominator changed` in the fresh report's
  preamble. If they match → go to "Audit one Decision".

## Init

1. **Baseline gate** — from `bin/`:
   ```
   go build ./bintest/...
   go test ./bintest/...
   gofmt -l bintest
   ```
   All three must be clean (build exits 0; test exits 0 with no failures and
   no `SKIP`; gofmt prints nothing). **Red baseline → refuse:** write
   `project/audit/REPORT.md` with just a title and a
   `- baseline: RED — <exact command> → <observed output>` line, and report
   `DONE` with a message stating the baseline is red. Do not write
   `project/audit/STATUS.md`.

2. **Green → run the structural sweep** (four deterministic set checks, all
   from `bin/` unless noted; use the repo-root-relative forms when that reads
   more naturally):

   **(a) Orphan tags** — ids tagged in tests that design never minted:
   ```
   comm -13 \
     <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
     <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' bintest | sort -u)
   ```
   Must be empty. Any id listed is tagged in a test but never minted in
   design — list it with its file:line.

   **(b) Duplicate assignment** — an id appearing more than once across
   `project/design/D*.md`'s Verification lists, or tagged in more than one
   test:
   ```
   grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort | uniq -d
   grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' bintest | sort | uniq -d
   ```
   Both must be empty.

   **(c) Coverage drift** — the coverage invariant:
   ```
   comm -23 \
     <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
     <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' bintest) \
           <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) \
           | sort -u)
   ```
   Must be empty. Also check the reverse — a pending phase carrying an id
   design no longer mints:
   ```
   comm -13 \
     <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
     <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | sort -u)
   ```
   Must also be empty. List any offending ids by direction.

   **(d) INDEX staleness** — the id set in `project/design/D*.md` must equal
   the id set in `project/design/INDEX.md`, and every `DNN.md` must have an
   index entry (and vice versa):
   ```
   comm -3 \
     <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
     <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/INDEX.md | sort -u)
   ```
   Must be empty. Also confirm every `D<N>` in `project/design/D*.md`'s
   filenames appears under `## Decisions` in `INDEX.md`, and vice versa
   (manual comparison of the two lists is fine — it's a handful of entries).

   **(e) Criteria trace** — `project/design/INDEX.md` must carry a
   `## Success criteria → ids` section, every product success criterion in
   `project/product/README.md`'s `## Success criteria (outcomes)` must have a
   line there carrying at least one id, and every id in that section must
   exist in the design id set from (d). **Record this check's actual result
   plainly** — if the section is absent, that is a finding (missing trace
   section fails this check), not a silent skip.

   Sweep failures do not abort the audit — record them as findings in the
   report preamble.

3. **Write the manifest** `project/audit/STATUS.md`:
   ```markdown
   # bin — Audit Status

   One line per design Decision that owns ids (directly or via a
   `[proof: bin]` umbrella marker), in Decision order. The only home of audit
   markers. Next work: `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`.

   - D5 ⬜ — The manifest readers are proven under the green gate (3 ids)
   - D6 ⬜ — bin/bintest proves the library-dependency contract (4 ids, root D22)
   - D7 ⬜ — The testing-language contract (2 ids, root D23)
   - D8 ⬜ — bin/bump enforces the changelog contract (4 ids, root D28)
   ```
   (Adjust the exact list/counts to whatever `project/design/INDEX.md`
   actually shows at run time — the above reflects the id-owning Decisions as
   of this prompt's writing. D1–D4 own no ids and are **not** listed: nothing
   to audit against a Decision with zero Verification ids.)

4. **Write the report preamble** to `project/audit/REPORT.md`:
   ```markdown
   # bin — Audit Report

   - baseline: green (`go test ./bintest/...` exit 0)
   - denominator: 13 ids across 4 Decisions (D5, D6, D7, D8)

   ## Structural sweep
   - orphan tags: pass (empty)
   - duplicate assignment: pass (empty)
   - coverage drift: pass (empty)
   - INDEX staleness: pass (empty)
   - criteria trace: <pass, or the exact finding>
   ```
   (Fill in each line with the real result of that turn's checks — "pass
   (empty)" only when the corresponding command genuinely produced no output;
   otherwise list the exact offending ids/files.)

5. `git worktree prune` (defensive cleanup of any stale worktree entry from a
   prior interrupted run).

6. Report **`NEXT`**.

## Audit one Decision

1. Grep `project/audit/STATUS.md` for the first `⬜`:
   ```
   grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1
   ```
   If none remains, go to "Finish" instead.

2. Read **only** that Decision's `project/design/DNN.md`.

3. For every id in its `## Verification.` list:
   - Locate its tagged test: `grep -n "R-XXXX-XXXX" bintest/*_test.go`
     (substitute the real id).
   - **Adversarial static read**, against the standard "what would have to be
     true for this test to fail, and can the chosen substrate make it
     fail?": does the assertion pin the *discriminating property* the id's
     behavior statement names (not a weaker property a degenerate
     implementation would also satisfy)? Does it exec the **real script**
     under `bin/` when the claim is about a script (never a Go
     reimplementation)? Is it reachable under `go test ./bintest/...` with no
     build tag, env gate, or skip holding it out?
   - Assign a verdict:
     - **`covered`** — tagged test exists, pins the discriminating property,
       runs against a substrate that can falsify it.
     - **`weak`** — tagged test exists but only proves a proxy (a Go
       reimplementation instead of the real script; a mock where a real
       external call is claimed; a degenerate-implementation-passes
       assertion; unreachable/skip-gated).
     - **`missing`** — no test carries the tag.
     - **`mismatched`** — a tag exists but the test asserts a different
       behavior than the id states.
   - **Escalate to mutation only** when the static read suspects `weak` but
     the test looks plausible and you cannot settle "could this fail?" by
     reading alone. Confident `covered`, `missing`, `mismatched` never
     escalate. Recipe:
     ```
     wt=$(mktemp -d)
     git worktree add "$wt" HEAD
     ```
     In `$wt`, apply the minimal mutation violating the id's discriminating
     property (flip a comparison, return the forbidden value, drop a call) in
     the corresponding file under `$wt/bin/`. Then run just that test's
     package:
     ```
     cd "$wt/bin" && go test ./bintest/... -run '<TestName>'
     ```
     Tagged test **fails** under mutation → upgrade to `covered`. **Survives**
     → `weak`. Either way, record the mutation and the observed result, then
     unconditionally:
     ```
     cd - && git worktree remove --force "$wt"
     ```
     before this turn ends — no mutation ever touches the live checkout.

4. **Wiring lens** — this tree has no HTTP/MCP/CLI-verb/event surfaces beyond
   the Go test package itself (D5's readers and D6/D8's conformance checks are
   invoked directly by `go test`, with no separate composition root to
   bypass). Record `unwired surface — none applicable (bintest has no
   composition root distinct from the test binary)` for this Decision unless
   a specific `DNN.md` declares an externally-reachable surface (none do as
   of this prompt's writing) — in that case apply the lens for real: does at
   least one id's test reach that surface through the real assembly path,
   not a directly-constructed component?

5. Append the `## D<N>` section to `project/audit/REPORT.md` (append, never
   overwrite prior sections):
   ```markdown
   ## D<N> — <title>
   - R-XXXX-XXXX — <verdict>
     behavior: <the id's behavior statement, quoted from the DNN.md>
     test: <file:line, or "none">
     finding: <why the verdict; for weak/mismatched, what the test actually
               proves vs. what it should>
     escalation: <"none" | "mutated <what>; tagged test failed (upgraded to
                 covered)" | "mutated <what>; tagged test survived">
   ...
   - unwired surface — none applicable (bintest has no composition root
     distinct from the test binary)
   ```

6. Flip that line in `project/audit/STATUS.md` from `⬜` to `✅`.

7. Report **`NEXT`**.

## Finish

No `⬜` remains in `project/audit/STATUS.md`. Append to
`project/audit/REPORT.md`:

```markdown
## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path to project/audit/REPORT.md>
```

Report **`DONE`**, echoing the report's absolute path in the message.

## Boundaries

- Never edit source, tests, scripts, or `project/design/`, `project/plan/`,
  `project/product/`.
- Never commit anything.
- Mutations happen only in a scratch worktree created with
  `git worktree add`, torn down with `git worktree remove --force` before the
  turn ends — no exceptions, even on a confusing result.
- When the static read is genuinely unsure and escalation is impractical
  (e.g. the mutation isn't clean to express), verdict `weak` and state the
  doubt — uncertainty is never `covered`.
- Never trust a tag's presence as proof; the assertion is the evidence.
- The only exits are the red-baseline refusal (init) and Finish; every other
  path ends on `NEXT`.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for
  the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red
  baseline); the loop stops.
- `message` — one short, plain sentence, e.g. `Audited D5: 3 covered.` or
  `Refused — baseline red: go test ./bintest/... failed.`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal; otherwise end on `NEXT`.
Keep `message` a single plain sentence — not a JSON object or code block.
