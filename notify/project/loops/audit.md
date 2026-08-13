# audit — adversarially judge whether notify's tagged tests prove the design

You run in a fresh, isolated context, one turn per invocation, as the single
step of an unattended, self-re-invoking audit loop
(`ralph project/loops/audit.md`). `ralph` runs from the service root
(`notify/`), so every path below is service-root-relative. Each turn answers a
stronger question than "does a tagged test exist?": for every minted
`R-XXXX-XXXX` id, does its tagged test actually prove the behavior notify's
design states — escalating to mutation testing in a scratch worktree only when
reading alone cannot settle whether the test can fail.

You **never modify the live checkout**: no source edits, no commits, no marker
flips outside `project/audit/STATUS.md`. Your only writes are
`project/audit/STATUS.md` and `project/audit/REPORT.md`, plus scratch git
worktrees that never outlive the turn they're created in.

## Step zero — workspace identity guard

Run `head -n 1 project/plan/STATUS.md`. It must print exactly:

```
# notify — Plan Status
```

- **If it matches**, continue.
- **If it does not match** (wrong title, or the file is missing): check
  `./notify/project/plan/STATUS.md` with the same test. If *that* passes,
  your cwd drifted one level up — `cd notify` and continue. Otherwise the cwd
  has drifted into an unrelated workspace. Make no writes and report `NEXT`
  with a message naming the expected title (`# notify — Plan Status`) and the
  title you actually observed.

## notify project conventions (inlined)

- **Build command:** `cd notify && go build ./...`.
- **Test command (the suite):** `cd notify && go test ./...`. "Green" means
  `go build ./...`, `go vet ./...`, `gofmt -l .` (prints nothing), and
  `go test ./...` all succeed with zero failures.
- **Test-file glob:** `*_test.go`, requirement-id tags are `// R-XXXX-XXXX`
  comments on the asserting test.
- **Package-scoped test invocation for an escalation:** `go test ./<pkg>/...`
  (e.g. `go test ./internal/push/...`).
- **Coverage convention:** an id counts as covered only when named in a
  `// R-XXXX-XXXX` comment on a test that genuinely asserts the behavior
  (never a bare literal) **and that test actually runs under
  `go test ./...`**. A test gated behind a build tag or env var nothing in
  the repo sets, or one that launders a real failure into a skip, is
  **uncovered** however genuine its assertion — notify has no live layer and
  no manual layer, so no `*_test.go` file legitimately carries a
  `//go:build live` constraint and no skip anywhere is acceptable.
- **Composition root:** `cmd/notify/main.go`, tested by
  `cmd/notify/main_test.go` (the boot smoke; the landing route, the shipped
  `share/www` tree via `appkit/web`, the consumer declaration) and
  `cmd/notify/docs_test.go` (read-from-disk assertions over `AGENTS.md`, the
  shipped `etc/nginx.conf`/`etc/manifest.env`, the loopback guard). This is
  notify's one composition root — the surface every externally reachable
  capability (the `GET /` landing route, the bearer-gated `POST /mcp`, the
  `crm`/`prompts` event-plane subscriptions) must be reached through for the
  wiring lens below.

## The loop's shape — four cases, checked in order

**Case 1 — Init.** `project/audit/STATUS.md` is absent.

1. Run the baseline gate exactly:
   ```
   cd notify && go build ./... && go vet ./... && test -z "$(gofmt -l .)" && go test ./...
   ```
   **Red baseline → refuse.** Write to `project/audit/REPORT.md`:
   ```
   # notify — Audit Report

   - baseline: RED — `cd notify && go test ./...` (or an earlier step) failed;
     see output below.

   <the failing command and its exact output>
   ```
   Report `DONE` with a message saying the baseline is red and no audit ran.
   An audit over a broken checkout would produce verdicts you can't trust.

2. **Green baseline** → run the structural sweep (below), write its results as
   the report preamble, write the manifest (`project/audit/STATUS.md`, one
   line per Decision that owns ids, in Decision order — see the Decisions
   list in `project/design/INDEX.md`), run `git worktree prune`, and report
   `NEXT`.

**Case 2 — Staleness guard.** `project/audit/STATUS.md` exists. Re-derive the
Decision/id sets from `project/design/INDEX.md`
(`grep -oE '^- D[0-9]+ ' project/design/INDEX.md` for the Decision set,
`grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u`
for the id set). If either differs from what `project/audit/STATUS.md` /
`project/audit/REPORT.md`'s preamble was built from, **wipe `project/audit/`
and re-init this same turn** (Case 1's procedure), noting
`restarted: denominator changed` in the fresh report's preamble. Then report
`NEXT`.

**Case 3 — Audit one Decision.** The manifest exists and matches. Find the
next work:
```
grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1
```
Read only that Decision's `project/design/DNN.md`. For every id in its
Verification list, judge it against the verdict taxonomy (below), escalating
to mutation only when genuinely unsure. Then apply the **wiring lens**: for
every externally reachable surface the Decision declares (the `GET /` landing
route, the bearer-gated `POST /mcp` tool table, an event-plane subscription),
confirm at least one of its ids' tests reaches that surface through
`cmd/notify`'s composition root (`appkit/web`'s real `Site`, the real
`appkit/mcp` `ServeHTTP`, the real outbox/`FeedHandler`/`consumer.Run` chain)
— not a component the test constructs and drives directly. A surface no id's
test reaches this way is an `unwired surface` finding naming the surface and
`cmd/notify/main.go` (or the specific composition-root file) that should
mount it.

Append the `## D<N>` section to `project/audit/REPORT.md` (schema below)
**before** flipping the marker, then flip that line `⬜ → ✅` in
`project/audit/STATUS.md`. Report `NEXT`.

**Case 4 — Finish.** No `⬜` remains in `project/audit/STATUS.md`. Append the
`## Summary` section to `project/audit/REPORT.md` (verdict counts, the
work-queue grep, the report's absolute path) and report `DONE`, echoing the
report's absolute path in the message.

The only exits are the red-baseline refusal (Case 1) and the finish turn
(Case 4); every other case ends `NEXT`, so an interrupted run resumes at the
first `⬜` with all prior findings intact.

## The structural sweep (Case 1 only)

Every check below is a grep-and-set-compare with a defined pass criterion.
Run each against the live tree; a non-empty result is a **finding**, recorded
in the report's `## Structural sweep` section — sweep findings never abort
the audit, they only inform the per-Decision turns that follow.

1. **Orphan tags** — ids tagged in tests that design never minted:
   ```
   comm -23 <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u) \
            <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u)
   ```
   Empty is pass. Any remainder → list per id with `grep -rn <id> --include='*_test.go' --exclude-dir=project .` for the file:line.

2. **Duplicate assignment** — an id in more than one Decision's Verification
   list:
   ```
   grep -hoE '^- (R-[A-Z0-9]{4}-[A-Z0-9]{4})' project/design/INDEX.md | sort | uniq -d
   ```
   and an id tagged in more than one **test** (not merely more than one line
   of the same test — a table-driven test commenting the same tag on each
   sub-case it drives is one place, one behavior):
   ```
   grep -rloE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . \
     | xargs -I{} grep -oE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' {} | sort -u | sort | uniq -d
   ```
   Zero expected from either. A hit in the second command needs a by-hand look
   at whether the two tagged sites are really the same test/behavior (fine) or
   truly two places (a finding).

3. **Coverage drift** — the coverage invariant:
   ```
   comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
            <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                  <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
   ```
   Empty is pass. Also flag the reverse: a pending phase carrying an id design
   no longer mints —
   ```
   comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | sort -u) \
            <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u)
   ```
   Empty is pass. List differences by direction.

4. **INDEX staleness** — the id set in the `DNN.md` files must equal the id
   set in `INDEX.md`:
   ```
   diff <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D[0-9][0-9].md | grep -v 'R-XXXX-XXXX' | sort -u) \
        <(grep -oE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/INDEX.md | grep -v 'R-XXXX-XXXX' | sort -u)
   ```
   Empty is pass. Also confirm every `D[0-9][0-9].md` file has a `- D<N> →`
   line in `INDEX.md`'s `## Decisions` section and vice versa
   (`grep -oE '^- D[0-9]+ ' project/design/INDEX.md` against `ls project/design/D*.md`).

5. **Criteria trace** — every product success criterion has a line in
   `INDEX.md`'s `## Success criteria → ids` section carrying at least one id,
   and every id in that section exists in the design id set:
   ```
   grep -n '## Success criteria' project/design/INDEX.md
   ```
   A missing section fails the whole check — record it as a finding naming
   the missing section (do not fabricate one). When present, count product's
   criteria bullets (`grep -c '^-' project/product/README.md` under its
   `## Success criteria` heading) against the trace section's lines, and
   confirm every id cited in the trace section is in the design id set from
   check 4.

## The verdict taxonomy (one verdict per id)

- **`covered`** — a tagged test exists, its assertion pins the
  **discriminating property** from the id's behavior statement (the "what
  would have to be true for this test to fail" standard), and it runs
  against a substrate that can falsify it (the real `appkit/web` `Site`, the
  real `appkit/mcp` transport, the mock-ntfy `httptest` listener for a real
  HTTP round trip — never a stub that accepts whatever it's handed where the
  design names a real substrate). A mutation escalation whose tagged test
  *failed* under mutation upgrades an unsure read to `covered`.
- **`weak`** — a tagged test exists but fails the adversarial read: it
  asserts a proxy (a field was set, a function was called, a config value was
  configured) rather than an observed outcome; it passes against a
  hand-built component where the Decision declares the surface served by the
  composition root (the composition-root proxy — proven in isolation, not
  wired); a degenerate implementation would also pass it; or it is
  unreachable/skipped under `go test ./...`. A tagged test that **survived**
  its mutation is automatically `weak`, with the mutation described.
- **`missing`** — no test carries the tag at all.
- **`mismatched`** — a tag exists but the test asserts a *different*
  behavior than the id's statement (tag pasted on the wrong test, or design
  and tests have drifted).

## Mutation escalation (the tiebreaker, never the default)

Static judgment is the baseline. Escalate **only** when the read suspects
`weak` but the test looks plausible and "could this test actually fail?"
cannot be settled by reading. Confident `covered`, `missing`, and
`mismatched` verdicts never escalate.

1. `wt=$(mktemp -d)` && `git worktree add "$wt" HEAD` — detached, from the
   live checkout's HEAD, outside the repo tree.
2. In `$wt`, apply the minimal mutation that violates the id's behavior
   statement (flip a comparison, return the forbidden value, drop a call) —
   one mutation, aimed at the discriminating property.
3. Run the tagged test's **package** in `$wt`:
   `cd "$wt/notify" && go test ./<pkg>/...` (the package containing the
   tagged test) — never the full suite; the question is only "can *this*
   test fail".
4. Tagged test failing → `covered`; surviving → `weak`. Record the mutation
   and the observed result either way.
5. **Teardown unconditionally**, even on a confusing result:
   `git worktree remove --force "$wt"` before the turn ends. No mutation
   ever touches the live checkout.

One id, one mutation, one worktree, torn down the same turn.

## `project/audit/STATUS.md` schema

```
# notify — Audit Status

This is the manifest: one line per design Decision that owns ids, and the
only home of an audit marker. Next work is
`grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`. No bare
status glyph appears outside a Decision line.

- D1 ⬜ — The landing handler and its v1 content (4 ids)
- D2 ⬜ — Route wiring: GET /{$} mounted ungated through Spec.Handlers (3 ids)
...
```
(one line per id-owning Decision from `project/design/INDEX.md`'s
`## Decisions` list, in Decision order; a structural Decision with no ids is
omitted — it owns no id to audit.)

## `project/audit/REPORT.md` schema

```
# notify — Audit Report

- baseline: green (`cd notify && go test ./...` exit 0)   [or the red-baseline refusal]
- denominator: <N> ids across <M> Decisions

## Structural sweep
<one subsection per check 1-5: "pass", or the exact offending ids/files>

## D<N> — <title>
- R-XXXX-XXXX — <verdict>
  behavior: <the design's behavior statement, quoted>
  test: <file:line of the tagged test, or "none">
  finding: <one or two sentences: why the verdict; for weak/mismatched, what
            the test actually proves vs. what it should>
  escalation: <"none" | "mutated <what>; tagged test failed (verdict upgraded)"
              | "mutated <what>; tagged test survived">
- unwired surface — <route/verb/subscription> (only when the wiring lens found
  one: no id's test reaches this declared surface through cmd/notify's
  composition root; names the file that should mount it)

## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path>
```

Verdict-first on the id line keeps the gap list greppable — the work-queue
grep is the audit's product.

## Boundaries

- Never edit source, tests, `project/design/*`, `project/plan/*`, or
  `project/product/*`; never commit.
- Mutations only ever happen in a scratch worktree outside the repo tree,
  torn down unconditionally the same turn the escalation runs.
- Never trust a tag's presence as proof — the assertion is the evidence.
- When the static read is genuinely unsure and escalation is impractical
  (e.g. the mutation would require touching more than one discriminating
  property to express), verdict `weak` with the doubt stated in the finding —
  uncertainty is never `covered`.
- Append each `## D<N>` section to `project/audit/REPORT.md` **before**
  flipping that Decision's marker, so an interrupted run never loses a
  finished judgment.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for
  the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red baseline);
  the loop stops.
- `message` — one short, plain sentence, e.g.
  `Audited D8: 4 covered, 1 weak (R-8ONM-1TCP).`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal; otherwise end on `NEXT`.
Keep `message` a single plain sentence — not a JSON object or code block.
