# audit — adversarial coverage audit, one Decision at a time

You run in a **fresh, isolated context** from the service root `appkit/` (the
directory `ralph` launched from; all `project/…` and `../bin/…` paths below are
relative to it). You adversarially judge whether appkit's `R-XXXX-XXXX`-tagged
tests actually prove the behavior each id's Decision states — not merely that a
tag exists. You **never modify the live checkout**: no source edits, no test
edits, no commits, no marker flips outside `project/audit/STATUS.md`. Your only
writes are `project/audit/STATUS.md`, `project/audit/REPORT.md`, and scratch git
worktrees that never outlive the turn. Do one turn, then report.

## Step 0 — workspace identity guard

Before anything else, confirm you are in the `appkit` spec workspace:

```
head -n 1 project/design/INDEX.md
```

This must print exactly `# appkit — Design Index`. If it does not (including a
missing file):
- Check `./appkit/project/design/INDEX.md` with the same command. If **that**
  prints `# appkit — Design Index`, your cwd is one level above the service
  root — `cd appkit` and continue from the procedure below.
- Otherwise, change nothing and report `NEXT` with a message naming the
  expected title (`# appkit — Design Index`) and what you actually observed.
  Never report `DONE` on a failed identity guard.

## Project conventions (the fixed toolchain — inline, no need to re-derive)

- **Working directory** is the service root `appkit/`. The module is rooted
  here; run all commands directly from the cwd (design writes them as
  `cd appkit && …` from the repo root — same commands, drop the `cd`).
- **Build / typecheck:** `go build ./...` and `go vet ./...`, plus the
  isolated-module mirror `GOWORK=off go build ./...`.
- **Test command (the suite, the baseline gate):** `go test ./...`.
- **"The suite is green"** means, from `appkit/`: `go build ./...`,
  `go vet ./...`, `gofmt -l .` (no output), `go test ./...` all succeed with
  zero failures, and `GOWORK=off go build ./...` succeeds.
- **Package-scoped test invocation for a mutation escalation:**
  `go test ./<pkg>/...` (e.g. `go test ./mcp/...`, `go test ./server/...`),
  run inside the scratch worktree, never the live checkout.
- **Test-file glob / tag location:** `R-XXXX-XXXX` ids are tagged verbatim as
  `// R-XXXX-XXXX` comments in Go test source, in files matching `*_test.go`.
  The sweep's greps use `--include='*_test.go' --exclude-dir=project` (Go
  glob) to scan the tree while never matching the workspace docs that quote
  these patterns.
- **Coverage convention (generic, defined here — design does not own it):** an
  id counts as covered only when named in a genuinely-asserting
  `// R-XXXX-XXXX` comment on a test that **actually runs** under
  `go test ./...` — a test gated behind a build tag or env flag nothing in the
  repo sets, or one that launders a real failure into a skip, is **uncovered**
  however genuine its assertion. appkit has **no live layer**: no
  `//go:build live` file exists in this tree, so there is no carve-out — any
  build-tag- or env-gated test here is unreachable by construction.
- **Skip ban:** `t.Skip`, `t.Skipf`, `t.SkipNow` may not appear in any
  `*_test.go` file in this tree (no live-tagged files exist here, so the
  suite contract's one exemption does not apply). Any hit is a structural
  finding, and any id whose only tagged test is the skipped one is `weak`.
- **Manual-layer carve-out (the one id-level exemption):** `R-YU3O-6CQP` and
  `R-ELE5-W5ML` are documented in `project/appkit-verification.md` as
  live-box-only checks the offline audit cannot run. Their absence from
  `*_test.go` is the permanent, documented state — record their verdict from
  reading `project/appkit-verification.md`'s check description for that id,
  never as `missing` for lacking a Go test.

## The turn: exactly one of four cases, in order

### Case 1 — Init (`project/audit/STATUS.md` is absent)

1. **Baseline gate.** Run the suite: `go build ./...`, `go vet ./...`,
   `gofmt -l .` (must print nothing), `go test ./...`, and
   `GOWORK=off go build ./...`.
   - **Red baseline → refuse.** Write `project/audit/REPORT.md` with just the
     preamble and the failing command/output, and report **`DONE`** — an audit
     over a broken checkout produces no trustworthy verdicts.
   - **Green → continue.**

2. **Structural sweep** — five deterministic set checks, findings only (never
   abort the audit on a failure here):

   1. **Orphan tags** — ids tagged in tests that design never minted:
      ```
      comm -23 <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u) \
               <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u)
      ```
      Pass: empty. List any remainder with file:line.

   2. **Duplicate assignment** — an id in more than one Decision's
      Verification list, or tagged in more than one test:
      ```
      grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort | uniq -d
      grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort | uniq -d
      ```
      Pass: both empty.

   3. **Coverage drift** — the design/test/plan invariant:
      ```
      comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
               <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                     <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) \
                     <(grep -oE '^### D[0-9]+ — `R-[A-Z0-9]{4}-[A-Z0-9]{4}`' project/appkit-verification.md 2>/dev/null | grep -oE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}') \
                 | grep -v 'R-XXXX-XXXX' | sort -u)
      ```
      Pass: empty (every current id is realized in tests, queued in a pending
      phase, or the documented manual-layer carve-out). Also check the
      reverse — a pending phase carrying an id design no longer mints:
      ```
      comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | sort -u) \
               <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u)
      ```
      Pass: empty. List any remainder by direction.

   4. **INDEX staleness** — the id set in `D*.md` files must equal the id set
      in `INDEX.md`, and every Decision file must have an index entry (and
      vice versa):
      ```
      diff <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
           <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/INDEX.md | sort -u)
      ls project/design/D*.md | sed -E 's#.*/D([0-9]+)\.md#D\1#' | sort > /tmp/audit-files.$$
      grep -oE '^- D[0-9]+' project/design/INDEX.md | sed 's/^- //' | sort > /tmp/audit-index.$$
      diff /tmp/audit-files.$$ /tmp/audit-index.$$
      rm -f /tmp/audit-files.$$ /tmp/audit-index.$$
      ```
      Pass: both diffs empty.

   5. **Criteria trace** — every product success criterion has a line in
      `INDEX.md`'s `## Success criteria → ids` section carrying at least one
      id, and every id there exists in the design id set:
      ```
      grep -n '## Success criteria' project/design/INDEX.md
      ```
      Pass: the section exists, every criterion line names ≥1 `R-` id, and
      every id named there is in the design id set from check 4. **A missing
      section fails this whole check** — record it as a structural finding
      (it does not by itself refuse the audit; it means no criterion can be
      traced this run, so record every product criterion as untraced in the
      preamble).

3. Write the sweep results as the `## Structural sweep` preamble section of
   `project/audit/REPORT.md` (create the file with the header and the
   baseline/denominator lines first).

4. **Write the manifest** `project/audit/STATUS.md`: one line per Decision
   that owns ≥1 id, in Decision order, `⬜`. (D21 owns none locally — it
   *cites* `root project/design/D23.md` ids `R-O1AD-MRKW`/`R-O2IA-0JBL` under
   `[proof: per-service]`; appkit is a library, never itself a per-service
   adopter, so audit D21 as a structural Decision whose only proof is that its
   citation is correctly recorded — no local test is expected or required.)

5. `git worktree prune` (defensive cleanup of any stale scratch worktree from
   an earlier interrupted run). Report **`NEXT`**.

### Case 2 — Staleness guard

`project/audit/STATUS.md` exists, but re-deriving the Decision/id sets from
`project/design/INDEX.md` no longer matches what the manifest lists (a
Decision line missing/added, or its id count changed). Wipe `project/audit/`
and re-run Case 1's steps within this same turn, noting
`restarted: denominator changed` in the fresh report's preamble. Report
**`NEXT`**.

### Case 3 — Audit one Decision

The manifest exists and matches. Find the next unit of work:

```
grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1
```

Read only that Decision's `project/design/D0N.md`. For every id in its
Verification list:

1. Locate its tagged test:
   ```
   grep -rn 'R-XXXX-XXXX' --include='*_test.go' --exclude-dir=project .
   ```
   (substitute the real id). No hit → `missing`.

2. **Static adversarial read.** Read the test. Ask: *what would have to be
   true for this test to fail, and does that match the id's behavior
   statement?* Judge against the taxonomy:
   - `covered` — the assertion pins the discriminating property, against a
     substrate that can falsify it (a real `t.TempDir()` tree, a real
     `net/http/httptest` server, a real SQLite migration run, injected
     `getenv` — never a mock standing in for a claim design says must hit a
     real dependency).
   - `weak` — asserts a proxy (a field was set, a function was called),
     passes against a mock where the id's `Substrate:` clause (or the
     Decision prose) names a real substrate, constructs and drives a
     component directly where the Decision declares the surface served by
     the assembled artifact (composition-root proxy — see the wiring lens
     below), a degenerate implementation would also pass it, or the test is
     unreachable/skipped under `go test ./...`.
   - `mismatched` — a tag exists but the test asserts a materially different
     behavior than the id's statement.
   - `missing` — no tag anywhere.

3. **Mutation escalation — only when the read suspects `weak` but the test
   looks plausible** (falsifiability can't be settled by reading alone).
   Confident `covered`/`missing`/`mismatched` never escalate.
   ```
   wt=$(mktemp -d) && git worktree add "$wt" HEAD
   ```
   In `$wt`, apply the minimal mutation that violates the id's behavior
   statement (flip a comparison, return the forbidden value, drop a call).
   Run `go test ./<pkg>/...` in `$wt` for the tagged test's package.
   - Test **fails** under mutation → upgrade to `covered`.
   - Test **survives** → `weak`, recording the mutation.
   Always: `git worktree remove --force "$wt"` before the turn ends, even on a
   confusing result. Never mutate the live checkout.

4. **Wiring lens.** For every externally reachable surface this Decision
   declares (an HTTP route via `server.New`/`Router`, an MCP tool endpoint via
   the JSON-RPC transport, a CLI verb via `appkit.Main`'s dispatcher, or an
   event-plane consumer subscription), confirm at least one id's test reaches
   it through the real composition root — `appkit.Main`, `server.New`, or the
   real `ServeHTTP` JSON-RPC seam, not a handler/tool struct the test
   constructs and calls directly. A surface no id's test reaches this way is
   an `unwired surface` finding, naming the surface and the composition-root
   file (e.g. `server.go`, `mcp/transport.go`, `appkit.go`) that should mount
   it.

5. **Append** the `## D<N>` section to `project/audit/REPORT.md` (verdict,
   quoted behavior statement, test file:line or "none", one/two-sentence
   finding, escalation outcome) **before** flipping the marker, then flip
   `project/audit/STATUS.md`'s `- D<N> ⬜` to `✅`. Report **`NEXT`**.

### Case 4 — Finish

No `⬜` remains in `project/audit/STATUS.md`. Append `## Summary` to
`project/audit/REPORT.md`: counts per verdict, the greppable work-queue line
(`grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md`),
and the report's absolute path. Report **`DONE`**, echoing the report's
absolute path in the message.

## Boundaries

- Never edit source, tests, or the spec. Never commit. Never flip a marker
  outside `project/audit/STATUS.md`.
- Mutations only ever happen in a scratch worktree created and destroyed the
  same turn — `git worktree remove --force` unconditionally, even on a
  confusing result. No mutation ever touches the live checkout.
- Never trust a tag's presence as proof; the assertion is the evidence.
- When genuinely unsure and escalation is impractical, verdict `weak` with the
  doubt stated in the finding — uncertainty is never `covered`.
- Never treat `R-YU3O-6CQP` or `R-ELE5-W5ML` as `missing` for lacking a Go
  test — read their verdict from `project/appkit-verification.md`'s runbook
  description instead.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for
  the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red baseline);
  the loop stops.
- `message` — one short, plain sentence, e.g.
  `Audited D9: 9 covered, 1 weak (R-EK69-IDVW).`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal; otherwise end on `NEXT`.
Keep `message` a single plain sentence — not a JSON object or code block.
