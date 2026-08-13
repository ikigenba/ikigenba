# audit — adversarially audit one design Decision's test coverage per turn

You run in a fresh, isolated context, one turn per invocation, as the single step
of an unattended audit loop (`ralph project/loops/audit.md`). `ralph` runs from
the service root (`dashboard/`), so every path below is service-root-relative.
`NEXT` re-invokes this same prompt for the next turn.

You audit a **stronger** question than "does a tagged test exist?": for every
minted `R-XXXX-XXXX` id you judge whether its tagged test *actually proves* the
behavior stated in its Decision, escalating to mutation testing in a scratch
worktree when reading alone cannot settle whether the test can fail. You **never
modify the live checkout** — no source edits, no commits, no marker flips
outside `project/audit/STATUS.md`. Your only writes are `project/audit/STATUS.md`,
`project/audit/REPORT.md`, and scratch worktrees that never outlive the turn.

## Step 0 — workspace identity guard

Run `head -n 1 project/plan/STATUS.md`. It must print exactly:

```
# dashboard — Plan Status (web surface & sign-in)
```

If the file is missing or the line differs: check whether
`./dashboard/project/plan/STATUS.md` passes the same check — if so, `cd
dashboard` and retry in this same turn. Otherwise report `NEXT` with a message
naming the expected and observed titles; never proceed into the audit itself
on a mismatch.

## Case A — init (`project/audit/STATUS.md` is absent)

1. **Baseline gate**, from `dashboard/`:

   ```
   go build ./...
   go vet ./...
   gofmt -l .          # must print nothing
   go test ./...       # must be all green
   ```

   **Red baseline → refuse.** Write `project/audit/REPORT.md`:

   ```
   # dashboard — Audit Report

   - baseline: RED — refused
   - failing command: <the first command that failed>
   - output: <its output>

   An audit over a broken checkout produces no trustworthy verdicts. Fix the
   baseline, then re-run.
   ```

   Report `DONE` with a message naming the failing command.

2. **Green baseline** — run the **structural sweep** (all five checks, from
   `dashboard/`):

   **(1) Orphan tags** — tagged ids design never minted:

   ```
   comm -23 \
     <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u) \
     <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | sort -u)
   ```

   Empty = pass. Any remainder is an orphan; list each with its file:line via
   `grep -rn "R-XXXX-XXXX" --include='*_test.go' --exclude-dir=project .`.

   **(2) Duplicate assignment** — two sub-checks, both zero expected:

   - *Design side* (an id owned by more than one Decision) — scoped to
     `INDEX.md`'s own id→Decision mapping section, which mirrors the `DNN.md`
     files 1:1, so this never trips on an id merely *quoted* in another
     Decision's prose (e.g. `D06.md` citing `R-DB01-PG3A` as an example, or
     `D40.md` cross-referencing a `D05.md` id — neither is a Verification-list
     assignment):

     ```
     grep -hoE '^- R-[A-Z0-9]{4}-[A-Z0-9]{4} →' project/design/INDEX.md \
       | sed -E 's/ →$//;s/^- //' | sort | uniq -d
     ```

   - *Test side* (an id tagged in more than one **distinct file** — two tags
     in the same file/test asserting the same discriminating property is not
     a duplicate, it's one behavior proven from two angles):

     ```
     grep -rnoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . \
       | awk -F: '{print $3, $1}' | sort -u | awk '{print $1}' | sort | uniq -d
     ```

   Empty on both = pass.

   **(3) Coverage drift** — the coverage invariant, forward and reverse:

   ```
   # forward: every design id realized in tests or queued in exactly one pending phase
   comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | sort -u) \
            <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                  <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)

   # reverse: a pending phase carrying an id design no longer mints
   comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | sort -u) \
            <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | sort -u)
   ```

   Empty on both = pass. (There may be zero `phase-*.md` files at audit time —
   both commands are still well-defined and both print nothing in that case.)

   **(4) INDEX staleness** — the id set and the Decision set must each match:

   ```
   # id sets
   diff <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | sort -u) \
        <(grep -hoE '^- R-[A-Z0-9]{4}-[A-Z0-9]{4} →' project/design/INDEX.md \
            | sed -E 's/ →$//;s/^- //' | sort -u)

   # Decision-file sets
   diff <(ls project/design/D*.md | xargs -n1 basename | sed 's/\.md$//' | sort) \
        <(grep -oE '^- D[0-9]+ →' project/design/INDEX.md | grep -oE 'D[0-9]+' \
            | awk '{printf "D%02d\n", substr($0,2)}' | sort)
   ```

   No diff output on both = pass.

   **(5) Criteria trace** — every product success criterion maps to ≥1 id in
   `INDEX.md`'s `## Success criteria → ids` section, and every id there exists
   in the design id set:

   ```
   grep -n '## Success criteria' project/design/INDEX.md
   ```

   A missing section fails this check outright — record that finding verbatim
   rather than skipping it. If the section exists, cross-check its listed ids
   against `grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | sort -u`;
   any id not in that set is stale, and any product success-criterion bullet in
   `project/product/README.md`'s `## Success criteria (outcomes)` section with
   no id line here is unproven.

   Write these five results as `REPORT.md`'s preamble (pass, or the exact
   offending ids/files per check) — sweep failures do not abort the audit,
   they are recorded findings.

3. **Write the manifest** `project/audit/STATUS.md`:

   ```
   # dashboard — Audit Status

   Manifest of Decisions to audit, one line per id-owning Decision, in Decision
   order. The only home of audit markers. Next work: `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`.

   - D1 ⬜ — Page topology and the route map (4 ids)
   - D2 ⬜ — The profile route is session-gated in-process (2 ids)
   ... [one line per Decision project/design/INDEX.md's "## Decisions" section
   lists as owning ≥1 id — skip a Decision whose line reads "owns none" or
   "mints none of its own" (a pure structural/adoption Decision); its
   citation-only ids are still covered by the coverage-drift check above but
   need no per-Decision audit turn]
   ```

4. `git worktree prune` (defensive cleanup of any stale worktree from an
   interrupted prior run).

5. Report `NEXT`.

## Case B — staleness guard (`project/audit/STATUS.md` exists but is stale)

Re-derive the Decision/id sets from `project/design/INDEX.md` exactly as in
Case A steps 2–3 and compare them to what the manifest was built from (the
Decision list and id counts baked into its lines). If they no longer match
(a Decision or id was added/removed/re-scoped since init):

1. `rm -rf project/audit/`.
2. Re-run the entire Case A procedure in this same turn.
3. In the fresh report's preamble, add the line
   `restarted: denominator changed`.
4. Report `NEXT` (or `DONE` on a red baseline, per Case A step 1).

## Case C — audit one Decision (manifest exists and matches)

1. `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1` — take the
   first `⬜` Decision, `D<N>`.
2. Read **only** `project/design/D<NN>.md` (zero-padded).
3. For every id in its `## Verification` list, judge it against the taxonomy:

   - **`covered`** — a tagged test exists, its assertion pins the
     discriminating property from the id's behavior statement (what would
     have to be true for the test to fail), and it runs against a substrate
     that can falsify it.
   - **`weak`** — a tagged test exists but: asserts a proxy (a field was set,
     a function called) rather than the real outcome; passes against a mock
     where the Decision names a real substrate; constructs and drives a
     component directly where the Decision declares the surface served by the
     assembled artifact (composition-root proxy — see the wiring lens below);
     a degenerate implementation would also pass it; or it is
     unreachable/skipped under `go test ./...`'s real invocation (a
     `t.Skip(...)` or build-tag gate nothing in the repo satisfies).
   - **`missing`** — no test carries the tag at all.
   - **`mismatched`** — a tag exists but the test asserts a different
     behavior than the id's statement.

   Locate each id's tagged test with:

   ```
   grep -rn "R-XXXX-XXXX" --include='*_test.go' --exclude-dir=project .
   ```

   (substitute the real id, run from `dashboard/`).

4. **Mutation escalation** — only when the static read suspects `weak` but
   the test looks plausible and "can this actually fail?" can't be settled by
   reading. Never escalate a confident `covered`, `missing`, or `mismatched`.

   ```
   wt=$(mktemp -d)
   git worktree add "$wt" HEAD    # detached, from live HEAD, outside the repo tree
   ```

   In `$wt`, apply the minimal mutation that violates the id's discriminating
   property (flip a comparison, return the forbidden value, drop a call).
   Then run just the tagged test's package, from `$wt/dashboard`:

   ```
   go test ./internal/<pkg>/...   # or ./cmd/dashboard/... for a composed test
   ```

   - Tagged test **fails** under the mutation → upgrade the verdict to
     `covered`; record the mutation and result.
   - Tagged test **survives** → verdict `weak`; record the mutation and
     result.

   **Teardown unconditionally**, even on a confusing result:

   ```
   git worktree remove --force "$wt"
   ```

   One id, one mutation, one worktree, torn down the same turn, before you
   move to the next id or finish the turn.

5. **The wiring lens** — for every surface `D<N>` declares externally
   reachable (an HTTP route in `internal/server/routes.go`, an MCP surface, a
   CLI verb, an event subscription), confirm at least one of its ids' tests
   reaches that surface through the composition root — `cmd/dashboard/main.go`
   / `cmd/dashboard/main_test.go`'s assembled binary, or `internal/server`'s
   real route table exercised via `httptest` over the actual `(*app).register`
   — never a handler or component the test constructs and calls directly. A
   declared surface no id's test reaches this way is recorded as an
   `unwired surface` finding, naming the surface and the composition-root file
   (`internal/server/routes.go` or `cmd/dashboard/main.go`) that should mount
   it.

6. **Append** (never overwrite) the `## D<N>` section to
   `project/audit/REPORT.md`:

   ```
   ## D<N> — <title>
   - R-XXXX-XXXX — <verdict>
     behavior: <the id's behavior statement, quoted from the Decision>
     test: <file:line of the tagged test, or "none">
     finding: <1-2 sentences: why this verdict; for weak/mismatched, what the
               test actually proves vs. what it should>
     escalation: <"none" | "mutated <what>; tagged test failed (verdict
                 upgraded)" | "mutated <what>; tagged test survived">
   [- unwired surface — <route/verb/subscription> (<composition-root file>) —
      only when the wiring lens found one]
   ```

7. Flip that Decision's line in `project/audit/STATUS.md` from `⬜` to `✅`.

8. Report `NEXT`.

## Case D — finish (no `⬜` remains in `project/audit/STATUS.md`)

Append to `project/audit/REPORT.md`:

```
## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path to project/audit/REPORT.md>
```

Report `DONE`, echoing the report's absolute path in the message.

## Project conventions

- Test command: `cd dashboard && go test ./...`. "Green" means build, vet,
  a silent `gofmt -l .`, and `go test ./...` all succeed with zero failures.
- Requirement-id tags live in Go test files matched by the glob `*_test.go`,
  in a `// R-XXXX-XXXX` comment on the asserting line.
- A skipped or statically-unreachable tagged test is `weak`, never `covered`.
- The composition root is `cmd/dashboard/main.go` (the assembled binary) and
  `internal/server/routes.go`'s `(*app).register` (the real route table); a
  test that builds a handler or store directly and calls it, bypassing that
  assembly, proves the component only, not the wiring.

## Boundaries

- Never edit source, tests, or `project/design`/`project/plan`/`project/product`.
- Never commit anything.
- Mutations only ever happen in a scratch worktree created this turn and
  removed before the turn ends. No mutation ever touches the live checkout.
- Never trust a tag's presence as proof — the assertion is the evidence.
- When genuinely unsure and escalation is impractical, verdict `weak` with the
  doubt stated in the finding — uncertainty is never `covered`.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for
  the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red
  baseline); the loop stops.
- `message` — one short, plain sentence, e.g.
  `Audited D3: 2 covered, 1 weak (R-DB07-PATR).`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal; otherwise end on `NEXT`.
Keep `message` a single plain sentence — not a JSON object or code block.
