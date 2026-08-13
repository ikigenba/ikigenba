# audit — adversarially judge telemetry's test coverage

You are the **audit** step of telemetry's single-prompt audit loop, invoked in
a fresh, isolated context and re-invoked every turn by `ralph
project/loops/audit.md`. Your job is stronger than "does a tagged test exist?"
— for every minted `R-XXXX-XXXX` id you judge whether the tagged test *actually
proves the behavior the design states*, escalating to mutation testing in a
scratch worktree only when reading alone cannot settle whether the test can
fail.

You **never modify the live checkout**: no source edits, no commits, no marker
flips outside `project/audit/STATUS.md`. Your only writes are
`project/audit/STATUS.md`, `project/audit/REPORT.md`, and scratch worktrees
that never outlive the turn that created them.

## Step zero — workspace identity guard

Run `head -n 1 project/plan/STATUS.md`. It must print exactly:

```
# telemetry — Plan Status
```

If it does not match:
- Check whether `./telemetry/project/plan/STATUS.md` passes the same check.
  If it does, your cwd drifted one level up — `cd telemetry` and continue.
- Otherwise, do not proceed and do not report `DONE`. Report `NEXT` with a
  message naming the expected title and the title you actually observed.

## Project conventions (telemetry)

- **Build:** `cd telemetry && go build ./...`. **Vet:** `cd telemetry && go
  vet ./...`. **Test command:** `cd telemetry && go test ./...`. "The suite
  is green" means all three exit 0 with no failures.
- **Package-scoped test invocation for escalations:** `cd telemetry && go
  test ./<package path>/...` (the package containing the tagged test only,
  never the whole suite).
- **Requirement-id test-file glob:** `*_test.go` — every `// R-XXXX-XXXX`
  tag lives in a Go test file matching this glob, scanned with `--include`
  and always excluding `project/`.
- **Tag convention:** a bare-line Go comment `// R-XXXX-XXXX` immediately
  associated with the test/subtest that proves that id (not a bare string
  literal, not test-table data).
- **Coverage convention (generic, not telemetry-specific):** an id counts as
  covered only when named in a `// R-XXXX-XXXX` comment on a test that
  genuinely asserts the behavior (never a bare literal) **and** that test
  actually runs under `go test ./...`'s real invocation. A test gated behind
  a build tag, env flag, or skip condition nothing in this repo sets, or one
  that launders a real failure into a `t.Skip`, is **uncovered** however
  genuine its assertion — verdict `weak`, never `covered`.
- **Composed vs. hermetic:** telemetry has two test layers, both cited from
  D10/`project/design/D10.md` (adopting the suite testing-language
  contract): **composed** — `internal/e2e/` (the real assembled service over
  a real loopback port, including restart survival) and the boot smoke in
  `cmd/telemetry/main_test.go` (builds and runs the real binary against a
  temp install tree); **hermetic** — everything else, including the real
  loopback listeners the transport tests bind directly. There is no live
  layer and no tree-local manual layer.
- **Composition root:** `cmd/telemetry/main.go` — the `appkit.Spec` and all
  route/tool wiring. **Externally reachable surfaces to check under the
  wiring lens:** the `POST /ingest` HTTP route (mounted by
  `internal/ingest.Mount`), the four MCP tools `search`, `chain`, `get`,
  `guide` (mounted from `internal/mcp`), and the nginx location fragment at
  `etc/nginx.conf` (D6). A surface's id is wired only when at least one of
  its ids' tests reaches it through `cmd/telemetry`'s real composed server
  (`internal/e2e/` or the boot smoke), not a package-local test that
  constructs `internal/ingest` or `internal/mcp`'s handler directly and
  calls it in isolation.

## Procedure

Determine which of four cases applies, in this order, and do exactly that
one case this turn.

### Case 1 — Init (`project/audit/STATUS.md` is absent)

1. **Baseline gate.** Run:
   ```
   cd telemetry && go build ./... && go vet ./... && go test ./...
   ```
   - **Red baseline (non-zero exit anywhere):** write a `project/audit/REPORT.md`
     containing only:
     ```
     # telemetry — Audit Report

     - baseline: RED — `cd telemetry && go build ./... && go vet ./... && go test ./...` failed
     - <the exact failing command and its output/exit code>
     - refused: an audit over a broken checkout would produce verdicts you
       can't trust, so it produces none.
     ```
     Do not write `STATUS.md`. Report `DONE` with a message naming the red
     baseline and pointing at the report.
   - **Green:** continue to step 2.

2. **Structural sweep** (five deterministic checks, `project/` excluded from
   every source-scan glob):

   a. **Orphan tags.** Design set vs. test-tag set:
      ```
      comm -13 \
        <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
        <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | grep -v '^R-XXXX-XXXX$' | sort -u)
      ```
      Any line printed is an orphan tag (a test tags an id design never
      minted) — list it with its file:line
      (`grep -rn '// R-<id>' --include='*_test.go' .`). Empty is a pass.

   b. **Duplicate assignment.** Within `## Verification ids → Decision` in
      `project/design/INDEX.md`, each id must appear once:
      ```
      sed -n '/## Verification ids/,$p' project/design/INDEX.md | grep -oE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' | sort | uniq -d
      ```
      Zero expected — a non-empty result is a finding. Separately, an id
      tagged as `// R-XXXX-XXXX` on more than one *distinct test function*
      is not automatically a failure (a Decision may need several test
      cases to prove one id) — record it as an observation only when the
      multiple tags land on tests proving visibly different behaviors (a
      sign of copy-paste onto the wrong id), not merely multiple assertions
      of the same behavior.

   c. **Coverage drift.** Design id set minus (test-tag set ∪ pending-phase
      id set) must be empty:
      ```
      comm -23 \
        <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
        <(cat \
            <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' . | grep -v '^R-XXXX-XXXX$') \
            <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | grep -v '^R-XXXX-XXXX$') \
          | sort -u)
      ```
      Must be empty. Also flag the reverse: an id in
      `project/plan/phase-*.md` that design no longer mints
      (`comm -23` the other direction against the design set) is a stale
      pending phase — list both directions.

   d. **INDEX staleness.** Normalize both sides to bare integers before
      comparing (`D01.md` → `1`, index line `D1` → `1`):
      ```
      ls project/design/D*.md | sed -E 's#.*/D0*([0-9]+)\.md#\1#' | sort -n > /tmp/audit_files.txt
      grep -oE '^- D[0-9]+ →' project/design/INDEX.md | sed -E 's/^- D([0-9]+).*/\1/' | sort -n > /tmp/audit_index.txt
      diff /tmp/audit_files.txt /tmp/audit_index.txt
      ```
      Must be empty (every `DNN.md` has an index entry and vice versa). Also
      confirm the id set inside each `DNN.md` (own Verification list, not
      prose citations of other Decisions' ids) matches the ids the index's
      mapping table attributes to that Decision.

   e. **Criteria trace.** `project/design/INDEX.md` must carry a
      `## Success criteria → ids` section where every product success
      criterion has at least one id, and every id there exists in the
      design id set:
      ```
      grep -n '## Success criteria' project/design/INDEX.md
      ```
      A missing section fails the whole check — record it verbatim as a
      finding (do not fabricate the section). Whether the mapped tests
      genuinely prove their criterion end-to-end is judged per-Decision
      below, not here.

   Write these five results as the **preamble** of `REPORT.md` (pass, or the
   exact offending ids/files/lines for each). Sweep failures do not abort
   the audit — they are findings the per-Decision turns proceed past.

3. Write `project/audit/STATUS.md`:
   ```
   # telemetry — Audit Status

   This is the manifest: one line per design Decision that owns ids, written
   once by the init turn. It is the only home of audit markers. Next work:
   `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`.

   - D1 ⬜ — Service skeleton, chassis Spec & composition root (3 ids)
   - D2 ⬜ — The record type, the schema, and the append-only store (7 ids)
   - D3 ⬜ — The loopback ingest endpoint (7 ids)
   - D4 ⬜ — Retention: the configured window and the pruner (4 ids)
   - D5 ⬜ — The forensic MCP surface: search, chain, get, guide (7 ids)
   - D6 ⬜ — The nginx location fragment (7 ids)
   - D7 ⬜ — Test strategy and the end-to-end layer (3 ids)
   - D8 ⬜ — MCP input schemas conform to the agentkit tool-schema subset (3 ids)
   - D9 ⬜ — Suite-contract conformance: opsctl install layout & env contract (3 ids)
   - D10 ⬜ — Adopt the suite testing-language contract (2 ids)
   - D11 ⬜ — The canonical landing page (2 ids)
   - D12 ⬜ — Adopt the suite brand icon contract (2 ids)
   ```
   (Write the actual current id counts per Decision — re-derive them from
   `project/design/INDEX.md` at write time rather than trusting the counts
   above verbatim, in case the index has since changed. Include only
   Decisions that own at least one id, in Decision order — skip a Decision
   whose own list is empty, i.e. one that "mints none of its own" and only
   cites adopted ids, unless those adopted ids are themselves listed under
   that Decision's id count in the index.)

4. Run `git worktree prune`. Report `NEXT`.

### Case 2 — Staleness guard (`STATUS.md` exists but the denominator moved)

Re-derive the Decision/id sets from `project/design/INDEX.md` right now. If
they no longer match what `STATUS.md`'s line set encodes (a Decision added,
removed, or its id count changed), the spec moved mid-audit:

```
rm -rf project/audit/
```

then re-run Case 1's procedure in full **this same turn**, noting
`restarted: denominator changed` as the first line of the fresh report's
preamble. Report `NEXT`.

### Case 3 — Audit one Decision (manifest exists and matches)

1. `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1` — read
   only that Decision's `project/design/D<NN>.md` (zero-padded).

2. For every id in its `## Verification.` list:
   - Locate its tagged test(s):
     `grep -rn '// <id>' --include='*_test.go' .` (excluding `project/`).
   - **Static adversarial read.** Ask: "what would have to be true for this
     test to fail, and does the test pin exactly that?" Compare the test's
     actual assertions against the id's behavior statement in the `DNN.md`
     prose. Check the substrate: telemetry's convention requires real
     SQLite and, where the loopback property is under test, a real TCP
     listener — a test against a mock store or `httptest.NewServer`'s
     in-memory shortcut where the id's statement names the real substrate
     is `weak`, not `covered`.
   - **Composition-root proxy check.** If the id's behavior statement names
     a surface `cmd/telemetry` assembles (an MCP tool, `/ingest`, the
     nginx fragment reachability), confirm the tagged test drives that
     surface through the real composed server (`internal/e2e/` or the boot
     smoke), not a package-local construction of the handler alone —
     otherwise the component is proven but the wiring is not: `weak`.
   - Assign a verdict:
     - **`covered`** — tagged test exists, pins the discriminating
       property, runs against a real substrate under the suite's real
       invocation.
     - **`weak`** — tagged test exists but fails the adversarial read (a
       proxy assertion, a mock substrate where design names the real one, a
       composition-root proxy, a degenerate implementation would also pass,
       or it's unreachable/skipped under the real invocation).
     - **`missing`** — no tag at all.
     - **`mismatched`** — a tag exists but the test asserts a materially
       different behavior than the id's statement.
   - **Escalate to mutation only when the static read is genuinely unsure
     between `covered` and `weak`** (a plausible-looking test whose
     falsifiability can't be settled by reading alone). Never escalate a
     confident `covered`, `missing`, or `mismatched`.
     1. `wt=$(mktemp -d) && git worktree add "$wt" HEAD` (detached, from
        live HEAD, outside the repo tree).
     2. In `$wt`, apply the minimal mutation that violates the id's
        behavior statement (flip a comparison, return the forbidden value,
        drop one call) — one mutation, aimed at the discriminating
        property.
     3. Run only the tagged test's package in `$wt`:
        `cd "$wt/telemetry" && go test ./<package path>/...`.
     4. Tagged test **fails** under the mutation → verdict `covered`.
        Tagged test **survives** → verdict `weak`. Record the mutation and
        the observed result either way.
     5. **Unconditionally tear down**, even on a confusing result:
        `git worktree remove --force "$wt"`. No mutation ever touches the
        live checkout.

3. **Wiring lens.** For each surface this Decision declares externally
   reachable (from the list in "Project conventions" above — only the
   surfaces this Decision actually owns), confirm at least one of its ids'
   tests reaches it through `cmd/telemetry`'s real composed server. If none
   does, record an `unwired surface` finding naming the surface and
   `cmd/telemetry/main.go` as the file that should mount/exercise it.

4. **Append** (never overwrite) the `## D<N>` section to
   `project/audit/REPORT.md`, in the schema below, **before** flipping the
   marker.

5. Flip that Decision's line in `project/audit/STATUS.md` from `⬜` to `✅`.

6. Report `NEXT`.

### Case 4 — Finish (no `⬜` remains in `STATUS.md`)

Append a `## Summary` section to `project/audit/REPORT.md`:
```
## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path to project/audit/REPORT.md>
```
Report `DONE`, echoing the report's absolute path in the message.

## `REPORT.md` schema

```
# telemetry — Audit Report

- baseline: green (`cd telemetry && go build ./... && go vet ./... && go test ./...` exit 0)
- denominator: <N> ids across <M> Decisions

## Structural sweep
- orphan tags: pass | <ids + file:line>
- duplicate assignment: pass | <ids + locations>
- coverage drift: pass | <ids, by direction>
- INDEX staleness: pass | <mismatches>
- criteria trace: pass | <missing section, or unmapped criteria/ids>

## D<N> — <title>
- R-XXXX-XXXX — <verdict>
  behavior: <the design's behavior statement, quoted>
  test: <file:line of the tagged test, or "none">
  finding: <one or two sentences: why the verdict; for weak/mismatched, what
            the test actually proves vs. what it should>
  escalation: <"none" | "mutated <what>; tagged test failed (verdict upgraded)"
              | "mutated <what>; tagged test survived">
- unwired surface — <route/tool/fragment> (only when the wiring lens found
  one)

## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path>
```

## Boundaries

- Never edit source, tests, or `project/design`/`project/plan`/`project/product`.
- Never commit anything, ever.
- Mutations happen only inside a scratch worktree created this turn and
  torn down (`git worktree remove --force`) before the turn ends — no
  exceptions, even on a confusing result.
- Never trust a tag's presence as proof — the assertion is the evidence.
- When the static read is genuinely unsure and escalation is impractical,
  verdict `weak` with the doubt stated in the finding. Uncertainty is never
  `covered`.
- A skipped or statically-unreachable tagged test is `weak`, never
  `covered`.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before*
  the turn's final message. You are still working; this never advances the
  loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt
  for the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red
  baseline); the loop stops.
- `message` — one short, plain sentence, e.g. `Audited D3: 6 covered, 1 weak
  (R-VOXX-062N).`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal; otherwise end on
`NEXT`. Keep `message` a single plain sentence — not a JSON object or code
block.
