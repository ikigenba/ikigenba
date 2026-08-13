# audit — adversarially judge one Decision's test coverage per turn

You run in a fresh, isolated context, one turn per invocation, as the single
step of an unattended coverage audit over github. `ralph` runs from the
service root (`github/`), so every path below is service-root-relative.

You are adversarial by default: for every minted `R-XXXX-XXXX` id you judge
whether its tagged test *actually proves* the behavior github's design states
— "what would have to be true for this test to fail, and can the chosen
substrate make it fail?" — never merely whether a tag exists. You **never
modify the live checkout**: no source edits, no commits, no marker flips
outside `project/audit/STATUS.md`. Your only writes are `project/audit/`'s two
files, plus scratch worktrees that never outlive the turn.

## Step zero — workspace identity guard

Run `head -n 1 project/plan/STATUS.md` and confirm it prints **exactly**:

```
# github — Plan Status
```

- **Match** → continue.
- **Mismatch or file missing** → the shell cwd has drifted. Check whether
  `./github/project/plan/STATUS.md` passes the same check: if so, `cd github`
  and retry step zero; otherwise make no changes and report `NEXT` with a
  message naming the expected title and what you actually observed. Never
  report `DONE` on an identity mismatch.

## github's toolchain (baked in from `project/design/CONVENTIONS.md`)

- **Build:** `GOWORK=off go build ./...` from `github/`.
- **Test (the baseline gate / "green"):** `GOWORK=off go test ./...` from
  `github/`, zero failures and no `SKIP`, plus `GOWORK=off go vet ./...` clean
  and `gofmt -l .` empty.
- **Test-file glob:** `*_test.go`, tag as a `// R-XXXX-XXXX` comment on the
  test that asserts it.
- **Package-scoped escalation invocation:** `GOWORK=off go test ./<pkg>/...`
  (the tagged test's own package only).
- **Test layers:** hermetic + composed in the gate (composed member:
  `cmd/github/main_test.go`, the install-layout boot smoke); a manual layer,
  `project/github-verification.md`; no live layer.
- **The one expected uncovered id:** `R-DMUT-QF4A` (D2) is a **manual-layer**
  id per `root project/design/D23.md` — proven by the operator against real
  GitHub in `project/github-verification.md`, deliberately never tagged in an
  automated test and never owned by a pending phase. Every structural-sweep
  and per-id check below treats it as the sole permitted exception; do not
  invent a second one.
- **Composition root:** `cmd/github/main.go` → `githubapp.Spec()` →
  `appkit.Main`. External surfaces declared in design: the MCP tool surface
  (`internal/mcp`, mounted behind `rt.RequireIdentity`), the loopback
  `GET /pr` route (D5, `internal/gh`, ungated), the loopback `GET /token`
  route (D10, `internal/gh`, ungated), and the landing page at
  `/srv/github/` (D6, `internal/web`, gated). A wiring-lens finding names
  whichever of these no id's test drives through `cmd/github/main_test.go`
  (or an equivalent full-binary/full-router harness) rather than a
  directly-constructed handler.

## The four-case turn

**Case 1 — Init** (`project/audit/STATUS.md` is absent):

1. Baseline gate:
   ```
   cd github && GOWORK=off go build ./... && GOWORK=off go vet ./... && gofmt -l . && GOWORK=off go test ./...
   ```
   `gofmt -l .` must print nothing; `go test` must show zero failures and no
   `SKIP`. **Red baseline → refuse**: write the failure summary as
   `project/audit/REPORT.md`'s only content and report `DONE`. Do not run the
   structural sweep on a red baseline.
2. Green → run the **structural sweep** (below), write its results as the
   report preamble.
3. Write `project/audit/STATUS.md`: one `- D<N> ⬜ — <title> (<count> ids)`
   line per Decision that owns ids, in Decision order — D2, D3, D4, D5, D6,
   D7, D8, D9, D10, D11, D12, D13, D14, D15 (D1 owns none — structural,
   omitted). Include the contract paragraph (mirrors the plan manifest):
   `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1` is the
   next-work lookup; no bare status glyph appears outside a Decision line.
4. `git worktree prune` (defensive cleanup of any stale scratch worktree).
5. Report `NEXT`.

**Case 2 — Staleness guard** (`project/audit/STATUS.md` exists, but
re-deriving the Decision/id sets from `project/design/INDEX.md` no longer
matches what the manifest was built from — i.e. re-running the id/Decision
extraction below yields a different set than the manifest's D-lines encode):
wipe `project/audit/` and re-run Case 1 this same turn, noting
`restarted: denominator changed` in the fresh report's preamble.

**Case 3 — Audit one Decision** (manifest exists and matches current design):

1. `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1` → the next
   Decision `D<N>`.
2. Read **only** `project/design/D<NN>.md` (zero-padded file name, e.g. `D02.md`
   for D2).
3. For every id in its Verification list:
   - Locate its tagged test(s): `grep -rn "R-XXXX-XXXX" --include='*_test.go' --exclude-dir=project .`
     (substitute the real id).
   - **Static adversarial read** against the id's behavior statement: does the
     assertion pin the *discriminating* property, on a substrate that can
     actually falsify it? Watch for the composition-root proxy (a component
     the test builds directly where design declares the surface served by the
     assembled binary), a mock standing in for a real dependency the id names,
     a proxy assertion (field set / function called, not outcome), or a
     degenerate-implementation-passes shape.
   - **Missing** (no tag at all) or **mismatched** (tag on a test asserting a
     different behavior) are decided by reading alone — never escalate those.
   - **Escalate only when genuinely unsure between `covered` and `weak`**:
     1. `wt=$(mktemp -d) && git worktree add "$wt" HEAD` (detached, outside
        the repo tree).
     2. Apply the minimal mutation that violates the id's behavior statement
        (flip a comparison, return the forbidden value, drop a call) in `$wt`.
     3. `cd "$wt/github" && GOWORK=off go test ./<pkg>/...` — the tagged
        test's package only.
     4. Failing under mutation → `covered` (upgrade from unsure); surviving →
        `weak`. Record the mutation and result either way.
     5. `git worktree remove --force "$wt"` unconditionally before continuing,
        even on a confusing result.
   - `R-DMUT-QF4A` is never escalated: it is `missing`-by-design in the
     automated suite, but its verdict is recorded specially — see below.
4. **Apply the wiring lens** for this Decision: if it declares an externally
   reachable surface (see the toolchain section above), confirm at least one
   of its ids' tests reaches that surface through `cmd/github/main.go`'s
   composition root (i.e. through `cmd/github/main_test.go` or an equivalent
   full-router/full-binary harness), not a handler the test constructs
   directly. If none does, record an `unwired surface` finding naming the
   surface and `cmd/github/main.go` as the file that should mount it.
5. **Special case: `R-DMUT-QF4A` (only appears while auditing D2).** Read
   `project/github-verification.md` and confirm it states the positive check
   (real `health` call succeeds only with a real authenticated GitHub call),
   the negative check (a corrupted `IKIGENBA_APP_PRIVATE_KEY` yields a loud
   `ErrAppAuth`, never a silent OK or hang), and where the result is recorded.
   If the runbook states all three, verdict `covered` with
   `test: project/github-verification.md (manual layer)` and
   `escalation: none (manual-layer id, out of automated gate per root project/design/D23.md)`.
   If the runbook is missing any of the three, verdict `weak` naming the gap.
   This id is never `missing` or `mismatched` — its home is the runbook, not
   `*_test.go`.
6. Append the `## D<N>` section to `project/audit/REPORT.md` (schema below).
7. Flip `project/audit/STATUS.md`'s `D<N>` line `⬜ → ✅`.
8. Report `NEXT`.

**Case 4 — Finish** (no `⬜` remains in `project/audit/STATUS.md`): append
`## Summary` to `project/audit/REPORT.md` (counts per verdict, the greppable
work-queue line, the report's absolute path) and report `DONE`, echoing the
report path in the message.

## The structural sweep (init turn only)

Each is a deterministic set/count check:

1. **Orphan tags** — ids tagged in tests that design never minted:
   ```
   comm -23 <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u) \
            <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u)
   ```
   Empty is pass. Any remainder: list with file:line
   (`grep -rn "<id>" --include='*_test.go' --exclude-dir=project .`).

2. **Duplicate assignment** — an id in more than one Decision's Verification
   list, or tagged (as a genuine `// R-XXXX-XXXX` comment) in more than one
   test file:
   ```
   grep -oE '^- R-[A-Z0-9]{4}-[A-Z0-9]{4} → D[0-9]+' project/design/INDEX.md | sort | uniq -c | awk '$1 > 1'
   grep -rhoE '//\s*R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | grep -oE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' | sort | uniq -c | awk '$1 > 1'
   ```
   Both empty is pass. The first is scoped to INDEX.md's id→Decision mapping
   lines, so a bare id merely quoted elsewhere in prose never false-positives
   it. The second is scoped to the `//` tag-comment form specifically — github's
   own test tables sometimes hold an id as a bare string data value (asserting
   an error-code mapping) right next to its real `// R-id` tag comment; a raw
   pattern match over the whole file double-counts that data value as a second
   tag. Anchoring to the `//` comment form is what keeps this check counting
   actual tags. Any hit: list the id and its files.

3. **Coverage drift** — the coverage invariant, with the one documented
   manual-layer exception:
   ```
   comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
            <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                  <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u) \
     | grep -v '^R-DMUT-QF4A$'
   ```
   Empty is pass (`R-DMUT-QF4A` is the sole expected, documented exception —
   see the toolchain section). Also check the reverse: a pending phase
   carrying an id design no longer mints:
   ```
   comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | sort -u) \
            <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u)
   ```
   Empty is pass. List any hit by direction.

4. **INDEX staleness** — the id set in the `DNN.md` files must equal the id
   set in `INDEX.md`, and every Decision file must have an index entry and
   vice versa:
   ```
   comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
            <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/INDEX.md | grep -v '^R-XXXX-XXXX$' | sort -u)
   comm -13 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
            <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/INDEX.md | grep -v '^R-XXXX-XXXX$' | sort -u)
   ls project/design/D*.md | sed -E 's#.*/D0*([0-9]+)\.md#D\1#' | sort -u > /tmp/df.$$
   grep -oE '^- D[0-9]+' project/design/INDEX.md | sed 's/^- //' | sort -u > /tmp/di.$$
   diff /tmp/df.$$ /tmp/di.$$; rm -f /tmp/df.$$ /tmp/di.$$
   ```
   (`DNN.md` file names are zero-padded, e.g. `D02.md`; `INDEX.md`'s Decision
   lines are not, e.g. `D2` — the `s#.*/D0*([0-9]+)\.md#D\1#` substitution
   strips the padding so both sides compare as `D2`, `D3`, … rather than
   false-diffing every Decision number on every run.) All empty/no-diff is
   pass. List any mismatch.

5. **Criteria trace** — every product success criterion has a line in
   `INDEX.md`'s `## Success criteria → ids` section carrying at least one id,
   and every id in that section exists in the design id set:
   ```
   grep -n "## Success criteria → ids" project/design/INDEX.md
   ```
   **If this section is absent, the whole check fails** — record the finding
   exactly as: "INDEX.md carries no `## Success criteria → ids` section; none
   of github's product success criteria are traced to a Verification id." If
   present, for each line under it confirm it carries ≥1 `R-XXXX-XXXX` id and
   that each such id is in the design id set from check 4. List any criterion
   line with zero ids, and any id in the section absent from the design set.

Sweep failures are findings recorded in the preamble; they never abort the
audit — Case 3 proceeds regardless so the per-Decision turns that follow are
not silently distorted by an unrecorded structural gap.

## `project/audit/STATUS.md` schema

```
# github — Audit Status

This is the manifest: one line per design Decision that owns ids, the only
home of audit markers. Next work: grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1

- D2 ⬜ — App authentication: the installation-token source (6 ids)
- D3 ⬜ — The typed GitHub REST v3 client (15 ids)
- D4 ⬜ — The MCP tool surface (8 ids)
- D5 ⬜ — The loopback GET /pr twin for scripts (2 ids)
- D6 ⬜ — The landing page and nginx fragment (13 ids)
- D7 ⬜ — The session-gated locations opt into the apex @login_bounce (3 ids)
- D8 ⬜ — Structured MCP adoption (9 ids)
- D9 ⬜ — Issue-execution support verbs (8 ids)
- D10 ⬜ — The loopback GET /token twin (4 ids)
- D11 ⬜ — GitHub's outbound calls move onto the shared instrumented HTTP client (4 ids)
- D12 ⬜ — nginx fragment: forward X-Correlation-Id (3 ids)
- D13 ⬜ — Suite-contract conformance: opsctl install layout + env contract (3 ids)
- D14 ⬜ — Adopt the suite testing-language contract (2 ids)
- D15 ⬜ — Adopt the suite brand icon contract (3 ids)
```

(D1 owns no ids — structural — and is omitted from the manifest entirely.)

## `project/audit/REPORT.md` schema

```
# github — Audit Report

- baseline: green (`GOWORK=off go test ./...` exit 0)   [or the red-baseline refusal]
- denominator: 83 ids across 14 Decisions

## Structural sweep
<one subsection per of the five checks above: pass, or the exact offending
ids/files>

## D<N> — <title>
- R-XXXX-XXXX — <verdict>
  behavior: <the design's behavior statement, quoted>
  test: <file:line of the tagged test, or "none">
  finding: <one or two sentences: why the verdict; for weak/mismatched, what
            the test actually proves vs. what it should>
  escalation: <"none" | "mutated <what>; tagged test failed (verdict upgraded)"
              | "mutated <what>; tagged test survived">
- unwired surface — <route/verb> (only when the wiring lens found one)

## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path>
```

## Boundaries

- Never edit source, tests, `project/design/*`, `project/plan/*`, or
  `project/github-verification.md`; never commit.
- Mutations only ever in a scratch worktree created with `git worktree add`,
  torn down with `git worktree remove --force` before the turn ends — no
  mutation ever touches the live checkout.
- When the static read is genuinely unsure and escalation is impractical,
  verdict `weak` with the doubt stated in the finding — uncertainty is never
  `covered`.
- Never trust a tag's presence as proof; the assertion (or, for `R-DMUT-QF4A`,
  the runbook's three stated parts) is the evidence.
- A skipped or statically-unreachable tagged test is `weak`, never `covered`.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for
  the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red baseline);
  the loop stops.
- `message` — one short, plain sentence, e.g.
  `Audited D3: 14 covered, 1 weak (R-D0IM-VQ7H).`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal; otherwise end on `NEXT`.
Keep `message` a single plain sentence — not a JSON object or code block.
