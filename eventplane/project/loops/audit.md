# audit — adversarially audit test coverage, one Decision at a time

You are the single step of an unattended audit loop over the `eventplane`
library's spec and tests. `ralph` re-invokes this one prompt every turn from a
fresh, isolated context. Your working directory is the service root
(`eventplane/`); all paths below are relative to it.

You answer a stronger question than "does a tagged test exist?": for every
minted `R-XXXX-XXXX` id, does its tagged test *actually prove the behavior the
design states*? You never modify the live checkout — no source edits, no
commits, no marker flips outside `project/audit/STATUS.md`. Your only writes
are `project/audit/STATUS.md`, `project/audit/REPORT.md`, and scratch git
worktrees that never outlive the turn.

## Step zero — workspace identity guard

Run `head -n 1 project/plan/STATUS.md`. It must print exactly:

```
# eventplane — Plan Status
```

If it does not match (or the file is missing), check
`./eventplane/project/plan/STATUS.md`: if *that* file passes the same check,
`cd eventplane` and continue. Otherwise do not proceed — report `NEXT` with a
message naming the expected title and what you actually observed.

## Which of the four cases this turn is

1. **Init** — `project/audit/STATUS.md` does not exist.
2. **Staleness guard** — `project/audit/STATUS.md` exists, but re-deriving the
   Decision/id sets from `project/design/INDEX.md` no longer matches what the
   manifest was built from.
3. **Audit one Decision** — the manifest exists and matches; there is a `⬜`
   Decision line.
4. **Finish** — the manifest exists, matches, and no `⬜` remains.

### Case 1 — Init

1. **Baseline gate.**
   ```
   go test ./...
   go vet ./...
   gofmt -l .
   ```
   `go test`/`go vet` must exit 0 and `gofmt -l .` must print nothing.
   **Red baseline → refuse:** write the failure summary as the whole content
   of `project/audit/REPORT.md` (title, the failing command, its output, and
   a one-line note that no audit was performed), and report `DONE` with a
   message naming the red command.

2. **Structural sweep** (green baseline only) — five deterministic checks:

   a. **Orphan tags** — ids tagged in tests that design never minted:
      ```
      comm -13 \
        <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
        <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u)
      ```
      Any id this prints is a candidate orphan — **except** an id this
      module's `project/design/CONVENTIONS.md` records as a suite-contract
      proof carried here (`[proof: eventplane]` per the umbrella's
      `root project/design/`, currently the ids adopted by D10:
      `R-O1AD-MRKW`, `R-O2IA-0JBL`). Exclude those two before judging the
      remainder orphaned; report any other remainder with file:line.

   b. **Duplicate assignment** — an id in more than one Decision's
      Verification list, scoped to `INDEX.md`'s id→Decision mapping:
      ```
      sed -n '/## Verification ids → Decision/,/## Success criteria/p' project/design/INDEX.md \
        | grep -oE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' | sort | uniq -d
      ```
      Zero expected. Separately, an id tagged in more than one test file
      (different files, not merely two mentions within one file's comment and
      test name for the same behavior):
      ```
      for id in $(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u); do
        n=$(grep -rl "$id" --include='*_test.go' --exclude-dir=project . | wc -l)
        [ "$n" -gt 1 ] && echo "$id: $n files"
      done
      ```
      List any offenders (id + file count > 1).

   c. **Coverage drift** — the coverage invariant:
      ```
      comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
               <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                     <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
      ```
      Empty expected. Also flag the reverse: a pending
      `project/plan/phase-*.md` carrying an id design no longer mints:
      ```
      comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | sort -u) \
               <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u)
      ```
      List any offenders by direction.

   d. **INDEX staleness** — the id set in the `DNN.md` files must equal the id
      set in `INDEX.md`, and every Decision file must have an index entry and
      vice versa:
      ```
      diff <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
           <(sed -n '/## Verification ids → Decision/,/## Success criteria/p' project/design/INDEX.md \
             | grep -oE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' | sort -u)
      diff <(ls project/design/D*.md | xargs -n1 basename | sort) \
           <(grep -oE '→ `D[0-9]+\.md`' project/design/INDEX.md | grep -oE 'D[0-9]+\.md' | sort -u)
      ```
      No diff expected in either. (The second command's `→ \`D<N>.md\``
      anchor matters: `INDEX.md`'s D10 entry cites the umbrella file
      `` `root project/design/D23.md` `` in prose, wrapped across a line
      break — a bare `D[0-9]+\.md` grep would misread that citation as a
      local Decision file and false-positive `D23.md` as missing from disk.
      The arrow-backtick anchor matches only this tree's own manifest
      mapping.) List any offenders.

   e. **Criteria trace** — every product success criterion has a line in
      `INDEX.md`'s `## Success criteria → ids` section carrying at least one
      id, and every id in that section exists in the design id set:
      ```
      grep -n '## Success criteria' project/design/INDEX.md
      ```
      **This module's `INDEX.md` currently has no `## Success criteria → ids`
      section at all** — if it is still absent when you run this, the whole
      check **fails**: record it as a finding (missing trace section; product
      has N success criteria in `project/product/README.md`, none traced) and
      do not attempt to guess a mapping.

   Write the results of a–e as the `## Structural sweep` preamble section of
   `project/audit/REPORT.md` (pass, or the exact offending ids/files/lines),
   under a header block giving the baseline result and the denominator
   (`<N> ids across <M> Decisions`, from `project/design/INDEX.md`'s
   `## Decisions` section).

3. **Write the manifest** `project/audit/STATUS.md`, one line per Decision
   that owns ids (D1–D9; D10 owns none locally — it only cites two per-service
   ids — so it is **excluded** from the manifest, matching "structural /
   owns no ids" treatment), in Decision order, each `⬜`.

4. `git worktree prune` (defensive; no escalation has happened yet).

5. Report `NEXT`.

### Case 2 — Staleness guard

Re-derive the Decision-id-owning set and the total id count from
`project/design/INDEX.md` exactly as case 1 does. If it differs from what
`project/audit/STATUS.md` currently encodes (different Decision set, different
id count, or a Decision's owned-id list changed), the spec moved under the
audit. Delete `project/audit/STATUS.md` and `project/audit/REPORT.md`, then
redo case 1 in the same turn, noting `restarted: denominator changed` as the
first line of the fresh report's preamble. Report `NEXT`.

### Case 3 — Audit one Decision

1. `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1` — the
   Decision to audit this turn.

2. Read **only** that Decision's `project/design/DNN.md`.

3. For each id in its Verification list, locate its tagged test:
   ```
   grep -rn "R-XXXX-XXXX" --include='*_test.go' --exclude-dir=project .
   ```
   (substitute the real id). Read the test and judge adversarially — "what
   would have to be true for this test to fail, and can the chosen substrate
   make it fail?" against the id's stated behavior:
   - **`covered`** — the test pins the discriminating property and runs
     against a substrate that can falsify it (real `t.TempDir()` SQLite for
     DDL/append/backpressure/correlation/retention claims; a real
     `httptest.Server` + real HTTP client, or real `consumer.Run`, for wire
     claims; table tests for pure `routing`/`correlation`; a real
     `go list -deps` subprocess for the `observe` import-discipline claim).
   - **`weak`** — asserts a proxy, passes against a substitute where the
     design names a real one, drives a component directly where the design
     declares the surface served by the assembled behavior (composition-root
     proxy), a degenerate implementation would also pass it, or it is
     unreachable/skipped under plain `go test ./...`.
   - **`missing`** — no test carries the tag at all.
   - **`mismatched`** — a tag exists but the test asserts a different
     behavior than the id states.
   When the static read suspects `weak` but the test looks plausible and you
   cannot settle it by reading, escalate:
   1. `wt=$(mktemp -d) && git worktree add "$wt" HEAD` (detached, outside the
      repo tree).
   2. In `$wt`, apply the minimal mutation violating the id's discriminating
      property (flip a comparison, return the forbidden value, drop a call).
   3. Run only the tagged test's package in `$wt`, e.g.
      `cd "$wt/eventplane/outbox" && go test ./...` (path matching the id's
      package).
   4. Tagged test fails under mutation → `covered`. Survives → `weak`. Record
      the mutation and result either way.
   5. `git worktree remove --force "$wt"` unconditionally before moving on.
   Never escalate a confident `covered`, `missing`, or `mismatched` verdict.

4. **Wiring lens.** eventplane is a library with **no external surface of its
   own** (no HTTP route, no MCP endpoint, no CLI verb, no event subscription —
   those live in consuming services). Its analogue is the seam boundary
   between its own packages: for a Decision whose design text names a
   consumer-facing entry point meant to be reached through a specific real
   path (e.g. `outbox.FeedHandler()` served over `httptest.Server`, not a
   `consumer.Run` constructed directly against an in-memory store the design
   didn't name), confirm at least one id's test reaches it that way. Record
   any surface reached only through a test-constructed substitute as an
   `unwired surface` finding naming the surface and where the real path
   should be exercised instead.

5. Append this Decision's `## D<N>` section to `project/audit/REPORT.md`
   (schema below) **before** flipping its marker, then flip
   `project/audit/STATUS.md`'s `⬜ → ✅` for this Decision.

6. Report `NEXT`.

### Case 4 — Finish

No `⬜` remains in `project/audit/STATUS.md`. Append the `## Summary` section
to `project/audit/REPORT.md`: counts per verdict across every audited
Decision, the greppable work-queue line, and the report's absolute path.
Report `DONE`, echoing the report's absolute path in the message.

## Project conventions (baked in)

- **Test command:** `go test ./...` from `eventplane/`. **Build/vet:**
  `go vet ./...` from `eventplane/`; `gofmt -l .` must print nothing.
  **Green** means both exit 0 and gofmt reports no files.
- **Test-file glob:** `*_test.go`, tags as `// R-XXXX-XXXX` comments or in the
  test name, co-located with the code under test (no per-phase or root-level
  test file, with the standing exception of `eventplane/agents_test.go` for
  the two whole-module umbrella ids `R-O1AD-MRKW`/`R-O2IA-0JBL`).
- **Coverage convention:** an id counts as covered only when named in a
  `// R-XXXX-XXXX` comment on a test that genuinely asserts the behavior
  (never a bare literal) **and** that test actually runs under
  `go test ./...` — a test gated behind a build tag or env condition nothing
  in the repo sets, or one that launders a real failure into a skip, is
  **uncovered** however genuine its assertion. This module has zero
  `t.Skip`/`t.Skipf`/`t.SkipNow` calls by design (D10, R-O2IA-0JBL); any
  found during the audit is itself a `weak` or `missing` finding on whichever
  id it guards.
- **Reachability rules:** a skipped or statically-unreachable tagged test is
  `weak`, never `covered`. Never trust a tag's presence as proof — the
  assertion is the evidence.

## The `project/audit/` artifacts this prompt writes

**`project/audit/STATUS.md`:**

```
# eventplane — Audit Status

One line per Decision that owns ids; the only home of audit markers. Next
work: `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`. No bare
status glyph appears outside Decision lines.

- D1 ⬜ — Envelope and wire cutover: `kind` + `subject` replace `type` (6 ids)
- D2 ⬜ — The `routing` package: canonical key and hand-rolled matcher (8 ids)
- D3 ⬜ — Producer families: registry, reflection, and filter validation (5 ids)
- D4 ⬜ — Consumer surface: routing fields on `consumer.Event`, Subscription cutover (6 ids)
- D5 ⬜ — Feed guard ownership moves to the chassis (1 id)
- D6 ⬜ — `eventplane/correlation`: the suite's correlation-id leaf package (6 ids)
- D7 ⬜ — Correlation on the producer path: outbox column, envelope field, ctx-bearing `Append` (6 ids)
- D8 ⬜ — Correlation on the consumer path: the chain enters the handler's context (6 ids)
- D9 ⬜ — `eventplane/observe`: an injectable hook on the publish and consume paths (7 ids)
```

(D10 owns no local ids — it only cites two per-service umbrella ids — so it is
excluded from the manifest, exactly like a structural Decision.)

**`project/audit/REPORT.md`:**

```
# eventplane — Audit Report

- baseline: green (`go test ./...` exit 0, `go vet ./...` exit 0, `gofmt -l .` empty)
  [or the red-baseline refusal, naming the failing command and output]
- denominator: 51 ids across 9 id-owning Decisions (D1–D9; D10 excluded, owns
  no local ids)

## Structural sweep
<one subsection per check a–e: pass, or the exact offending ids/files/lines>

## D<N> — <title>
- R-XXXX-XXXX — <verdict>
  behavior: <the design's behavior statement, quoted>
  test: <file:line of the tagged test, or "none">
  finding: <one or two sentences>
  escalation: <"none" | "mutated <what>; tagged test failed (verdict upgraded)"
              | "mutated <what>; tagged test survived">
(- unwired surface — <what> — only when the wiring lens found one)

## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path>
```

## Boundaries

- Never edit source, tests, or the spec.
- Never commit anything.
- Mutations only ever happen in a scratch worktree created with
  `git worktree add`, and it is torn down with
  `git worktree remove --force` unconditionally the same turn, even on a
  confusing result. No mutation ever touches the live checkout.
- When the static read is genuinely unsure and escalation is impractical,
  verdict `weak` with the doubt stated in the finding — uncertainty is never
  `covered`.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for
  the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red
  baseline); the loop stops.
- `message` — one short, plain sentence, e.g. `Audited D6: 5 covered, 1 weak
  (R-UI02-0D09).`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal; otherwise end on `NEXT`.
Keep `message` a single plain sentence — not a JSON object or code block.
