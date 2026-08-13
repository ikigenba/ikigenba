# audit — adversarially judge one Decision's test coverage per turn

You are the **audit** step for the artifacts service, invoked in a **fresh,
isolated context** with no memory of prior turns. All state lives in files
under the artifacts service root, which is your working directory
(`project/audit/STATUS.md` and `project/audit/REPORT.md`), plus scratch git
worktrees that never outlive the turn. This is **one turn**: do exactly one
of the four cases below, once, and report.

You answer a stronger question than "does a tagged test exist?" — for every
minted `R-XXXX-XXXX` id in the Decision you audit this turn, you judge
whether the tagged test *actually proves the behavior the design states*,
escalating to mutation testing in a scratch worktree only when reading alone
cannot settle it. You **never modify the live checkout**: no source edits,
no commits, no marker flips outside `project/audit/STATUS.md`. Your only
writes are `project/audit/STATUS.md`, `project/audit/REPORT.md`, and scratch
worktrees you remove before the turn ends.

## Step 0 — workspace identity guard

Run:

```
head -n 1 project/plan/STATUS.md
```

It must print exactly `# artifacts — Plan Status`. This repo nests several
valid `project/` trees, so a drifted working directory would audit the wrong
workspace. On a mismatch or a missing file, do **not** proceed:

- If `head -n 1 artifacts/project/plan/STATUS.md` prints
  `# artifacts — Plan Status`, the cwd drifted one level up: `cd artifacts`
  and continue normally from step 1.
- Otherwise change nothing and return `NEXT` with a message naming the
  expected title (`# artifacts — Plan Status`) and what was actually
  observed.

## Which of the four cases applies

1. **Init** — `project/audit/STATUS.md` does not exist.
2. **Staleness guard** — `project/audit/STATUS.md` exists, but re-deriving
   the Decision/id sets from `project/design/INDEX.md` no longer matches
   what the manifest was built from.
3. **Audit one Decision** — the manifest exists and matches; at least one
   line is `⬜`.
4. **Finish** — the manifest exists, matches, and no line is `⬜`.

### Case 1 — Init

**Baseline gate.** From the service root:

```
cd artifacts && go build ./... && go vet ./... && gofmt -l . && go test ./...
```

All four must succeed (`gofmt -l .` prints nothing; the rest exit 0).

- **Red baseline → refuse.** Write `project/audit/REPORT.md` with only a
  preamble stating which command failed and its exact output, under a
  `# artifacts — Audit Report` title and a
  `- baseline: RED (\`<command>\` exit <n>)` line. Report **`DONE`** with a
  message saying the audit refused on a red baseline and naming the failing
  command.

**Green baseline → structural sweep.** Run all five checks below and record
each result (pass, or the exact offending ids/files) as report subsections.

1. **Orphan tags** — ids tagged in tests that design never minted:

   ```
   comm -23 <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u) \
            <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u)
   ```

   Empty = pass. Any remainder is listed per id with its `file:line`
   (`grep -rn '<id>' --include='*_test.go' --exclude-dir=project .`).

2. **Duplicate assignment** — an id appearing in more than one Decision's
   Verification list, or tagged in more than one test. Scope strictly to
   **bullet-leading** ids inside each file's `**Verification.**` section (a
   mid-prose cross-reference to another id inside a different bullet's text
   is not a second assignment):

   ```
   for f in project/design/D*.md; do
     awk '/^\*\*Verification\.\*\*/{flag=1} flag' "$f" | grep -oE "^- \`?R-[A-Z0-9]{4}-[A-Z0-9]{4}"
   done | grep -oE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' | sort | uniq -d
   ```

   for cross-Decision duplicates, and

   ```
   grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort | uniq -d
   ```

   for an id tagged in more than one test file. Zero expected from both; a
   hit in the second command is not automatically wrong (the same id may
   legitimately anchor a hermetic test and its composed/browser counterpart)
   — record each hit with its files and note in the finding whether the two
   tests look like one behavior split in two places (a real duplicate) or a
   layered pair sharing one id (note it, do not silently pass it — that
   distinction is judged in the per-Decision turn that owns the id, not
   here).

3. **Coverage drift** — the coverage invariant:

   ```
   comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
            <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                  <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
   ```

   Empty = pass — every current id is realized in tests or queued in exactly
   one pending phase. Also check the reverse (a pending phase carrying an id
   design no longer mints):

   ```
   comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | sort -u) \
            <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u)
   ```

   Empty = pass. List differences by direction.

4. **INDEX staleness** — the id set in the `DNN.md` files must equal the id
   set in `INDEX.md`:

   ```
   diff <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
        <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/INDEX.md | grep -v 'R-XXXX-XXXX' | sort -u)
   ```

   Empty = pass. Also confirm every Decision file has an `INDEX.md` entry
   and vice versa: the `## Decisions` section lists `D1`..`D<N>` for exactly
   the `D*.md` files present (`ls project/design/D*.md | wc -l` equals the
   count of `- D` lines under `## Decisions`).

5. **Criteria trace** — every product success criterion has a line in
   `INDEX.md`'s `## Success criteria → ids` section carrying at least one
   id, and every id there exists in the design id set:

   ```
   awk '/## Success criteria/,0' project/product/README.md | grep -c '^- '
   grep -c '^[0-9]\+\.' project/design/INDEX.md
   comm -23 <(grep -A1 '^[0-9]\+\.' project/design/INDEX.md | grep -oE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' | sort -u) \
            <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u)
   ```

   Pass when the two counts are equal (every criterion has a numbered trace
   line) **and** the `comm` output is empty (no stale id in the trace). A
   missing `## Success criteria → ids` section fails the whole check.

Sweep failures are **findings, not aborts** — record them in the report
preamble and continue.

**Write the manifest.** `project/audit/STATUS.md`:

```markdown
# artifacts — Audit Status

This is the manifest: one line per design Decision that owns ids, the only
home of audit markers. Next work: `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`.

- D1 ⬜ — Service skeleton, chassis adoption & composition root (3 ids)
- D2 ⬜ — Data model, tokens & blob store (7 ids)
- D3 ⬜ — Signed upload links: mint + the public upload ingress (11 ids)
- D4 ⬜ — The download surface: public and private tiers (6 ids)
- D5 ⬜ — Content-plane citizenship: holder endpoint + `import` acceptor (6 ids)
- D6 ⬜ — MCP tool surface (11 ids)
- D7 ⬜ — Event production: `created` / `updated` / `deleted` (4 ids)
- D8 ⬜ — nginx location fragment (tiers) (8 ids)
- D9 ⬜ — The landing page: a sortable, filterable inventory (6 ids)
- D10 ⬜ — Test strategy: adopt the suite testing-language contract (2 ids)
- D11 ⬜ — Adopt the suite brand icon contract: the shipped icon set and its link markup (3 ids)
```

(Regenerate this list from `project/design/INDEX.md`'s `## Decisions`
section rather than trusting the count above verbatim — INDEX.md is the
live source; the counts shown here are what INDEX.md holds as of this
prompt's authoring and may drift.)

**Write the report preamble.** `project/audit/REPORT.md`:

```markdown
# artifacts — Audit Report

- baseline: green (`go build ./... && go vet ./... && gofmt -l . && go test ./...` exit 0)
- denominator: <N> ids across <M> Decisions

## Structural sweep
### 1. Orphan tags
<pass, or the offending ids + file:line>
### 2. Duplicate assignment
<pass, or the offending ids + files, noting layered-pair vs. true duplicate>
### 3. Coverage drift
<pass, or the offending ids by direction>
### 4. INDEX staleness
<pass, or the offending ids/files>
### 5. Criteria trace
<pass, or the unmapped criterion / stale id>
```

Run `git worktree prune`, then return `NEXT` with a message summarizing the
baseline and sweep result counts.

### Case 2 — Staleness guard

Re-derive the Decision list and id set from `project/design/INDEX.md`'s
`## Decisions` section and compare to `project/audit/STATUS.md`'s lines (by
Decision id and id count per Decision). On any mismatch — a Decision added
or removed, or an id count changed — the denominator moved under the audit:
`rm -rf project/audit` and redo Case 1's init in this same turn, adding one
line to the fresh report's preamble: `restarted: denominator changed`.
Return `NEXT`.

### Case 3 — Audit one Decision

Find the first pending line:

```
grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1
```

Read **only** that `project/design/DNN.md`. For every id in its
`**Verification.**` list, judge it against the taxonomy:

- **`covered`** — a tagged test exists (`grep -rn '<id>' --include='*_test.go' --exclude-dir=project .`),
  its assertion pins the **discriminating property** from the id's behavior
  statement — "what would have to be true for this test to fail" — and runs
  against a substrate that can falsify it (real temp-file SQLite through the
  real appkit migration runner, real temp-dir filesystems for the
  `BlobStore` seam, the real eventplane outbox, `httptest`, or headless
  Chrome for a browser-proof id — never a mock standing in for one of
  these). A mutation escalation whose tagged test *failed* under mutation
  upgrades an unsure read to `covered`.
- **`weak`** — a tagged test exists but fails the adversarial read: it
  asserts a proxy (a field was set, a function was called, a component was
  constructed directly rather than driven through `cmd/artifacts`'s real
  assembled router where the design declares the surface served by the
  composition root), a degenerate implementation would also pass it, or it
  is unreachable/skipped under `go test ./...` (a build tag the gate does
  not set, an env flag nothing in the repo sets, or a real failure — missing
  Chrome, non-zero exit — converted into a skip). A tagged test that
  **survived** its mutation is automatically `weak`, with the mutation
  described.
- **`missing`** — no test carries the tag at all.
- **`mismatched`** — a tag exists but the test asserts a *different*
  behavior than the id's statement (tag pasted on the wrong test, or design
  and test have drifted).

**Wiring lens.** For every surface this Decision declares externally
reachable (an HTTP route under `cmd/artifacts`, an MCP tool, a CLI verb, an
event subscription), confirm at least one of its ids' tests reaches that
surface through `cmd/artifacts`'s composition root — the real assembled
router/binary — not a component the test constructs itself (`httptest`
against a hand-built handler, or a package-level call that bypasses
`cmd/artifacts` entirely). A declared surface no id's test reaches this way
is an `unwired surface` finding, naming the surface and the
`cmd/artifacts/main.go` (or the relevant composition-root file) that should
mount it.

**Mutation escalation** — only when the static read suspects `weak` but the
test looks plausible and "could this fail?" cannot be settled by reading.
Confident `covered`, `missing`, `mismatched` verdicts never escalate:

```
wt=$(mktemp -d)
git worktree add "$wt" HEAD
```

In `$wt`, apply the minimal mutation that violates the id's discriminating
property (flip a comparison, return the forbidden value, drop a call). Run
only the tagged test's package, not the full suite, e.g.:

```
(cd "$wt/artifacts" && go test ./internal/artifacts/... -run TestNameHere)
```

(substitute the actual package path and test name). Tagged test **fails**
under the mutation → `covered`; **survives** → `weak`. Record the mutation
and the observed result either way, then tear down unconditionally before
the turn ends:

```
git worktree remove --force "$wt"
```

**Append the report section** (before flipping the marker):

```markdown
## D<N> — <title>
- R-XXXX-XXXX — <verdict>
  behavior: <the design's behavior statement, quoted>
  test: <file:line of the tagged test, or "none">
  finding: <one or two sentences: why the verdict; for weak/mismatched, what
            the test actually proves vs. what it should>
  escalation: <"none" | "mutated <what>; tagged test failed (verdict upgraded)"
              | "mutated <what>; tagged test survived">
```

repeated for every id, then, only if the wiring lens found one:

```markdown
- unwired surface — <route/verb/subscription> (no id's test reaches this
  declared surface through the composition root; should be mounted in
  <file>)
```

Flip that `STATUS.md` line `⬜ → ✅`. Return `NEXT` with a message naming
the Decision and its verdict counts.

### Case 4 — Finish

No `⬜` remains. Append to `REPORT.md`:

```markdown
## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path to project/audit/REPORT.md>
```

Report **`DONE`**, echoing the report's absolute path in the message.

## Project conventions (inlined so this prompt never opens `CONVENTIONS.md`)

- **Toolchain:** Go (`go 1.26`), module path `artifacts`, on the shared
  `appkit` chassis over SQLite (`modernc.org/sqlite`, pure Go, no cgo).
- **Build/typecheck:** `go build ./...`, `go vet ./...`, `gofmt -l .`
  (prints nothing).
- **Test command:** `go test ./...`; green means all four commands above
  succeed.
- **Test-file glob:** `*_test.go`; every `// R-XXXX-XXXX` tag lives in a Go
  test file matching this glob, excluding anything under `project/`.
- **Test placement:** unit/hermetic tests are co-located with the code they
  exercise (package-local `*_test.go`, e.g. `internal/web/landing_test.go`);
  cross-package/composed tests live only in `cmd/artifacts/*_test.go`.
- **Reachability:** a skipped or statically-unreachable tagged test is
  `weak`, never `covered` — a tag's presence is never itself proof; the
  assertion, run for real, is the evidence.

## Boundaries

- Never edit source, tests, or `project/design|plan|product/`.
- Never commit anything.
- Mutations only ever happen in a scratch worktree created and destroyed
  within this same turn; never touch the live checkout.
- When the static read is genuinely unsure and escalation is impractical,
  verdict `weak` with the doubt stated in the finding — uncertainty is never
  `covered`.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:

- `CONTINUE` — **non-terminal**: any progress message you stream *before*
  the turn's final message. You are still working; this never advances the
  loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for
  the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red
  baseline); the loop stops.
- `message` — one short, plain sentence, e.g.
  `Audited D9: 5 covered, 1 weak (R-55UH-BF0N).`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal; otherwise end on
`NEXT`. Keep `message` a single plain sentence — not a JSON object or code
block.
