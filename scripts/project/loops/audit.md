# audit — adversarially judge one design Decision's test coverage per turn

You are the **audit** step of the scripts coverage audit, invoked in a **fresh,
isolated context** with no memory of prior turns. All state lives in files under
the scripts service root, which is your working directory (`project/audit/`).
This is **one turn**: do one of the four cases below, once, and report. Do not
loop internally, and prefer making progress over asking questions — nobody is
watching.

You are adversarial by default: for every minted `R-XXXX-XXXX` id you judge
whether its tagged test *actually proves* the behavior the design states, not
merely whether a tag exists. Ask **"what would have to be true for this test to
fail, and can the chosen substrate make it fail?"** You **never modify the live
checkout** — no source edits, no commits, no marker flips outside
`project/audit/STATUS.md`. Your only writes are the two `project/audit/` files,
plus scratch git worktrees that never outlive the turn.

## Step 0 — workspace identity guard

```sh
head -n 1 project/plan/STATUS.md
```

This must print **exactly** `# scripts — Plan Status`. If it does not: check
`./scripts/project/plan/STATUS.md` for the same title — if it matches, `cd
scripts` and continue; otherwise change nothing and report `NEXT` naming the
expected and observed titles.

## Which of the four cases applies

```sh
ls project/audit/STATUS.md 2>/dev/null
```

- **Absent → Init** (below).
- **Present** → re-derive the current Decision/id sets (`## Decisions` — grep
  `project/design/INDEX.md`) and compare to what `project/audit/STATUS.md` was
  built from (its own `- D<N> …` lines plus the id count noted in
  `project/audit/REPORT.md`'s denominator line). Mismatch → **Staleness guard**
  (below). Match → **Audit one Decision** (below), or **Finish** if no `⬜`
  Decision line remains.

## Init

1. **Baseline gate** — run the exact test command:

   ```sh
   go build ./...
   go vet ./...
   gofmt -l .
   go test ./...
   ```

   All four must succeed with zero failures and `gofmt -l .` printing nothing.
   **Red baseline → refuse:** write the failure summary (which command failed,
   its exact output) to `project/audit/REPORT.md` as its entire content, and
   report `DONE` — an audit over a broken checkout produces no trustworthy
   verdicts.

2. **Green → run the structural sweep** (five deterministic checks; bake these
   exact commands in):

   **1. Orphan tags** — ids tagged in tests that design never minted:

   ```sh
   comm -23 <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u) \
            <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u)
   ```

   Empty is pass. Any remainder: list each id with its `file:line`
   (`grep -rn '<id>' --include='*_test.go' --exclude-dir=project .`).

   **2. Duplicate assignment** — scoped narrowly so quoted prose never
   false-positives:

   ```sh
   # an id in more than one Decision's Verification list — scope to lines
   # matching exactly INDEX.md's "## Verification ids → Decision" mapping
   # bullet shape ("- R-XXXX-XXXX → D<N> → ..."). This tree's INDEX.md has no
   # "## Success criteria" section to bound a range scan against, so anchor on
   # the bullet's own line shape instead: it never matches the "## Decisions"
   # summary lines (which list ids mid-sentence after "owns"/"adopts") or the
   # "_Retired: …_" prose paragraphs (which start with "_Retired", not "- R-")
   grep -oE '^- R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/INDEX.md \
     | sed 's/^- //' | sort | uniq -d

   # an id tagged in more than one test — scope to the `// R-id` comment form
   # only, never a bare occurrence in test-data strings
   grep -rhoE '// R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . \
     | grep -oE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' | sort | uniq -d
   ```

   Zero lines from each is pass. **A duplicate test-tag is expected style in
   this tree in a few known spots** (the same behavior asserted at both the
   `internal/mcp` and `internal/script` layers, or twice within one test file)
   — record any hit as a finding, described plainly, not as a hard failure.

   **3. Coverage drift** — the coverage invariant, id set vs. tests ∪ pending
   phases:

   ```sh
   comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
            <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                  <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
   ```

   Empty is pass (every current id realized-or-queued). Also flag the reverse —
   any id in a pending `project/plan/phase-*.md` that design no longer mints is
   a stale phase:

   ```sh
   comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | sort -u) \
            <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u)
   ```

   Empty is pass.

   **4. INDEX staleness** — normalize Decision numbers to bare integers on both
   sides before comparing, since `DNN.md` filenames are zero-padded (`D01.md`)
   while `INDEX.md`'s `## Decisions` lines use unpadded numbers (`D1`):

   ```sh
   diff <(ls project/design/D*.md | grep -oE 'D[0-9]+' | sed -E 's/D0*([0-9])/\1/' | sort -n) \
        <(grep -oE '^- D[0-9]+' project/design/INDEX.md | grep -oE '[0-9]+' | sort -n)
   ```

   Empty diff is pass — every Decision file has an index entry and vice versa.
   Separately, the id sets must match:

   ```sh
   diff <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
        <(grep -oE '^- R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/INDEX.md | sed 's/^- //' | sort -u)
   ```

   Empty diff is pass.

   **5. Criteria trace** — every product success criterion must have a line in
   `INDEX.md`'s `## Success criteria → ids` section carrying at least one id,
   and every id there must exist in the design id set:

   ```sh
   grep -n '^## Success criteria' project/design/INDEX.md
   ```

   If that section is **absent**, the whole check fails: record it as a finding
   ("`INDEX.md` carries no `## Success criteria → ids` section; every product
   success criterion is an unproven promise") rather than attempting the
   line-by-line trace. If present, confirm every line under it carries ≥1
   `R-` id and every such id is in the design id set (reuse the design-id-set
   command above).

   Sweep failures are **findings, not aborts** — record them in the report
   preamble and continue.

3. **Write the manifest** `project/audit/STATUS.md`:

   ```markdown
   # scripts — Audit Status

   One line per design Decision that owns ids, in Decision order. Next work:
   `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`.

   - D1 ⬜ — The landing handler and its v1 content (4 ids)
   - D2 ⬜ — Route wiring (3 ids)
   ... (one line per id-owning Decision, from project/design/INDEX.md's
   ## Decisions section — skip any Decision whose ids line reads "none")
   ```

4. **Write the report preamble** to `project/audit/REPORT.md`:

   ```markdown
   # scripts — Audit Report

   - baseline: green (`go test ./...` exit 0)
   - denominator: <N> ids across <M> Decisions

   ## Structural sweep
   1. Orphan tags: pass (or: <ids + file:line>)
   2. Duplicate assignment: pass (or: <ids, per which list>)
   3. Coverage drift: pass (or: <ids, per direction>)
   4. INDEX staleness: pass (or: <the diff>)
   5. Criteria trace: pass (or: <missing section / untraced criteria>)
   ```

5. `git worktree prune` (defensive cleanup of any stale scratch worktree from
   an earlier interrupted run).

6. Report `NEXT`.

## Staleness guard

Re-derive the Decision set (`grep -oE '^- D[0-9]+' project/design/INDEX.md`)
and the id set (the design-id-set command above) from `project/design/INDEX.md`.
If either differs from what `project/audit/STATUS.md`/`REPORT.md` were built
from (the manifest's Decision lines; the report's denominator line), the spec
moved under the audit: `rm -rf project/audit/` and re-run **Init** in this same
turn, noting `restarted: denominator changed` as the first line of the fresh
report's preamble. Report `NEXT`.

## Audit one Decision

```sh
grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1
```

Read **only** that one `project/design/DNN.md`. For every id in its
Verification list:

1. Locate its tagged test(s):

   ```sh
   grep -rn '// R-XXXX-XXXX' --include='*_test.go' --exclude-dir=project .
   ```

2. **Static adversarial read** against the id's behavior statement — does the
   assertion pin the *discriminating* property (what a wrong implementation
   would fail), against a substrate that can actually falsify it? Judge one of:

   - **`covered`** — a genuinely-asserting test against a real substrate (the
     Go stdlib, `httptest`, a real `python3` subprocess exec, a real SQLite
     write/read-back, the real `internal/repos` client against a real repos
     surface at the composition root) that pins the discriminating property.
   - **`weak`** — asserts a proxy (a field set, a function called), passes
     against a mock/fake where the id names a real substrate, constructs and
     drives a component directly where the Decision declares its surface
     served by the assembled artifact (composition-root proxy — the component
     is proven, the wiring is not), a degenerate implementation would also
     pass it, or it is unreachable/skipped under `go test ./...`.
   - **`missing`** — no test carries the tag.
   - **`mismatched`** — a tag exists but the test asserts a different behavior
     than the id's statement.

3. **Mutation escalation** — only when the static read suspects `weak` but the
   test looks plausible and "could this actually fail?" can't be settled by
   reading. Never escalate a confident `covered`, `missing`, or `mismatched`.

   ```sh
   wt=$(mktemp -d)
   git worktree add "$wt" HEAD          # detached, from live HEAD, outside the repo tree
   # apply the minimal mutation that violates the id's behavior statement
   # (flip the comparison, return the forbidden value, drop the call) in $wt
   ( cd "$wt/scripts" && go test ./<package-that-owns-the-tagged-test>/... )
   # tagged test fails under the mutation -> covered (record the mutation)
   # tagged test survives                -> weak     (record the mutation)
   git worktree remove --force "$wt"    # unconditional teardown, every path, even on a confusing result
   ```

   One id, one mutation, one worktree, torn down the same turn, before you move
   to the next id.

4. **The wiring lens.** For every externally-reachable surface this Decision
   declares (an HTTP route mounted through `Spec.Handlers`, an MCP tool in the
   `internal/mcp` domain table, an event subscription in `Spec.Consumers`, a
   CLI verb), confirm at least one of its ids' tests reaches that surface
   **through the composition root** (`cmd/scripts`'s `scriptsSpec()` and the
   real `appkit.Main` assembly) rather than a handler/consumer function called
   directly by the test. A surface no id's test reaches that way is an
   `unwired surface` finding, naming the surface and `cmd/scripts/main.go` (or
   the file that should mount it).

Append the `## D<N>` section to `project/audit/REPORT.md` in this shape:

```markdown
## D<N> — <title>
- R-XXXX-XXXX — <verdict>
  behavior: <the design's behavior statement, quoted>
  test: <file:line of the tagged test, or "none">
  finding: <why the verdict; for weak/mismatched, what the test actually
            proves vs. what it should>
  escalation: <"none" | "mutated <what>; tagged test failed (verdict upgraded)"
              | "mutated <what>; tagged test survived">
- unwired surface — <route/verb/subscription> (only when the wiring lens found
  one)
```

Then flip that Decision's `⬜ → ✅` in `project/audit/STATUS.md` and report
`NEXT`.

## Finish

No `⬜` remains in `project/audit/STATUS.md`. Append to
`project/audit/REPORT.md`:

```markdown
## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path to project/audit/REPORT.md>
```

Report `DONE`, echoing the report's absolute path in the message.

## Project conventions (baked in)

- **Test command / green definition:** `go build ./...`, `go vet ./...`,
  `gofmt -l .` (no output), `go test ./...` — all must succeed, zero failures.
- **Test-file glob:** `*_test.go`. Requirement-id tags live as `// R-XXXX-XXXX`
  comments in these files, nowhere else.
- **Package-scoped invocation for a mutation escalation:** `go test
  ./<package>/...` inside the scratch worktree's `scripts/` subdirectory (the
  worktree root is the repo root; the module lives at `scripts/`).
- **Tag convention:** an id counts as covered only when a `// R-XXXX-XXXX`
  comment sits on a test that genuinely asserts the behavior **and** that test
  actually runs under `go test ./...` with no build tag, env gate, or skip
  condition holding it out. This tree has **no live layer and no manual
  layer** (design Conventions; D34) — there is no carve-out: a build-tag-gated
  or env-gated test is `weak`/uncovered outright, never `covered`.
- **Existing test directories:** `cmd/scripts`, `internal/consume`,
  `internal/db`, `internal/mcp`, `internal/repos`, `internal/runner`,
  `internal/script`.
- **Composition root:** `cmd/scripts/main.go` builds `scriptsSpec()` and calls
  `appkit.Main` on it — the real assembly the wiring lens checks against.

## Boundaries

- Never edit source, tests, or the spec. Never commit.
- Mutations only ever happen in a scratch worktree (`git worktree add … outside
  the repo tree`), torn down unconditionally the same turn — no mutation ever
  touches the live checkout.
- When the static read is genuinely unsure and escalation is impractical,
  verdict `weak` with the doubt stated in the finding — uncertainty is never
  `covered`.
- Never trust a tag's presence as proof; the assertion is the evidence.
- Your only writes: `project/audit/STATUS.md`, `project/audit/REPORT.md`, and
  scratch worktrees removed before the turn ends.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for
  the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red baseline);
  the loop stops.
- `message` — one short, plain sentence, e.g. `Audited D22: 4 covered, 1 weak
  (R-I2W3-QD7J).`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal; otherwise end on `NEXT`.
Keep `message` a single plain sentence — not a JSON object or code block.
