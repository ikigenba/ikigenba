# audit — adversarially judge test coverage, one Decision at a time

You run in a fresh, isolated context, one turn per invocation, as the single
step of an unattended coverage audit over ledger. `ralph` runs from the service
root (`ledger/`), so every path below is service-root-relative.

You answer a stronger question than "does a tagged test exist?" — for every
minted `R-XXXX-XXXX` id you judge whether the tagged test *actually proves the
behavior the design states*, escalating to mutation testing in a scratch
worktree when reading alone cannot settle whether the test can fail. You
**never modify the live checkout** — no source edits, no commits, no marker
flips outside `project/audit/STATUS.md`. Your only writes are the two
`project/audit/` files, plus scratch worktrees that never outlive the turn.

## Workspace identity guard

Run `head -n 1 project/plan/STATUS.md` and confirm it prints exactly
`# ledger — Plan Status`. This is a mono-repo of nested spec workspaces, so a
cwd that drifted (harness reset, a stray `cd` to the repo root) can land in a
different `project/` tree entirely. On mismatch or a missing file: if
`./ledger/project/plan/STATUS.md` passes the same check, the cwd is one level
up — `cd ledger` and continue; otherwise change nothing and report `NEXT` with
a message naming the expected and observed titles. Never report `DONE` on a
failed guard.

## The coverage convention (apply identically to every id)

An id counts as **covered** only when named in a `// R-XXXX-XXXX` comment on a
test that genuinely asserts the behavior (never a bare literal) **and that
test actually runs under `go test ./...`**. A test gated behind a build tag or
env flag nothing in the repo sets, or one that launders a real failure into a
skip, is **uncovered** however genuine its assertion reads — ledger has no
live layer, so no `*_test.go` file legitimately carries a `//go:build live`
constraint and no skip is ever legitimate.

## Procedure — one of four cases, decided by what's on disk

**Case A — Init** (`project/audit/STATUS.md` is absent):

1. Run the baseline gate, from `ledger/`:

   ```
   go build ./...
   go vet ./...
   gofmt -l .            # must print nothing
   go test ./...
   GOWORK=off go build ./...   # the isolated build check; must exit 0
   ```

   **Red baseline → refuse.** Write the failure summary to
   `project/audit/REPORT.md` (create `project/audit/` if needed) and report
   `DONE` — an audit over a broken checkout would produce verdicts you can't
   trust, so it produces none.

2. **Green → run the structural sweep** (five deterministic set checks; run
   every command for real and record the actual output):

   a. **Orphan tags** — ids tagged in tests that design never minted:
      ```
      comm -23 <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u) \
               <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u)
      ```
      Empty is pass. Any remainder: list per id with its file:line
      (`grep -rn "<id>" --include='*_test.go' --exclude-dir=project .`).

   b. **Duplicate assignment** — an id owned by more than one Decision's
      Verification list in `project/design/INDEX.md`'s `## Verification ids →
      Decision` section, or tagged in more than one test:
      ```
      grep -E '^- R-[A-Z0-9]{4}-[A-Z0-9]{4} →' project/design/INDEX.md | sort | uniq -d
      grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort | uniq -d
      ```
      Zero expected on both. Any hit is a finding.

   c. **Coverage drift** — the design id set minus the union of the test-tag
      set and the pending-phase id set must be empty (every current id is
      realized in tests or queued in exactly one pending phase):
      ```
      comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
               <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                     <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
      ```
      Empty is pass. Also flag the reverse: a pending `project/plan/phase-*.md`
      carrying an id design no longer mints (compare the phase-file id set
      against the design id set) is stale.

   d. **INDEX staleness** — the id set in `project/design/D*.md` must equal
      the id set in `project/design/INDEX.md`, and every Decision file must
      have an index entry and vice versa:
      ```
      diff <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
           <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/INDEX.md | grep -v 'R-XXXX-XXXX' | sort -u)
      diff <(ls project/design/D*.md | xargs -n1 basename | sed 's/\.md$//' | sort) \
           <(grep -oE '^- D[0-9]+ →' project/design/INDEX.md | grep -oE 'D[0-9]+' | sed -E 's/^D([0-9])$/D0\1/' | sort -u)
      ```
      Empty diffs are pass. Any line is a finding.

   e. **Criteria trace** — every product success criterion has a line in
      `project/design/INDEX.md`'s `## Success criteria → ids` section
      carrying at least one id, and every id in that section exists in the
      design id set:
      ```
      grep -n '## Success criteria' project/design/INDEX.md
      ```
      A missing section fails the whole check — record it as a finding naming
      the missing section; do not attempt to synthesize a mapping yourself.

   Sweep failures do **not** abort the audit — record them all in the report
   preamble, then continue.

3. Write the manifest `project/audit/STATUS.md`:

   ```
   # ledger — Audit Status

   This is the manifest: one line per design Decision that owns ids, written
   by the audit's init turn. `- D<N> ⬜` is pending, `- D<N> ✅` is audited.
   The next-work lookup is `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`.
   No bare status glyph appears anywhere but on a Decision line.

   - D1 ⬜ — The landing handler and its v1 content (4 ids)
   - D2 ⬜ — Route wiring: GET /{$} mounted ungated through Spec.Handlers (3 ids)
   - D3 ⬜ — ledger's own Carbon design assets (3 ids)
   - D4 ⬜ — nginx fragment: session-gated location + identity forwarding (6 ids)
   - D7 ⬜ — A top-left Home link to the dashboard landing page (1 id)
   - D8 ⬜ — Self-serve fonts, eliminate FOUT (5 ids)
   - D9 ⬜ — Adopt registry: resolve loopback port, guard literals (8 ids)
   - D10 ⬜ — Web surface from share/www through the chassis (2 ids)
   - D11 ⬜ — MCP surface over appkit/mcp: seven-domain-tool table (1 id)
   - D13 ⬜ — Session-gated locations opt into @login_bounce (3 ids)
   - D14 ⬜ — external_ref: opt-in idempotency for derived transactions (7 ids)
   - D15 ⬜ — Event-routing conformance (4 ids)
   - D16 ⬜ — Structured MCP adoption (13 ids)
   - D17 ⬜ — Correlation adoption (5 ids)
   - D18 ⬜ — Testing-language conformance (2 ids)
   - D19 ⬜ — Adopt the suite brand icon contract (2 ids)
   ```

   (D5, D6, D12 own no ids — structural Decisions — and are excluded from the
   manifest; their content is covered by the green baseline alone, not a
   per-id audit turn.)

4. Write `project/audit/REPORT.md`'s preamble (title, baseline line, id/Decision
   counts, and the `## Structural sweep` section with each check's real
   pass/fail result and any offending ids/files).

5. Run `git worktree prune` defensively (clears any stale worktree registration
   from a prior crashed turn).

6. Report `NEXT`.

**Case B — Staleness guard** (`project/audit/STATUS.md` exists, but
re-deriving the Decision/id sets from `project/design/INDEX.md` no longer
matches what the manifest holds — e.g. a Decision or id count differs):

1. `rm -rf project/audit/` and re-run Case A's steps 1–6 in this same turn.
2. Note `restarted: denominator changed` in the fresh report's preamble.
3. Report `NEXT`.

**Case C — Audit one Decision** (the manifest exists and matches; some line is
still `⬜`):

1. `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1` — take the
   first pending Decision `D<N>`.
2. Read **only** `project/design/D0N.md` (via `project/design/INDEX.md` for
   the path if the number needs zero-padding resolved).
3. For every id in that Decision's Verification list:
   - Locate its tagged test: `grep -rn "<id>" --include='*_test.go' --exclude-dir=project .`
   - **Static adversarial read** — ask "what would have to be true for this
     test to fail?" against the id's behavior statement. Judge:
     - `covered` — the test pins the discriminating property and runs against
       a real substrate (a real migrated SQLite db for domain/db work, a real
       `net/http/httptest` server for web/MCP/feed work — never a mock where
       design names a real substrate).
     - `weak` — the test exists but asserts a proxy (a field was set, a
       function was called), constructs and drives a component directly where
       design declares the surface reached through the assembled composition
       root (`cmd/ledger/main.go`'s `appkit.Spec`), a degenerate
       implementation would still pass it, or it is unreachable/skipped under
       `go test ./...`.
     - `missing` — no tagged test at all.
     - `mismatched` — a tag exists but the test asserts a different behavior
       than the id's statement.
   - **Escalate to mutation only when genuinely unsure between `covered` and
     `weak`** (never for a confident `missing` or `mismatched`):
     1. `wt=$(mktemp -d)` && `git -C ledger worktree add "$wt" HEAD` (or,
        since your cwd is already `ledger/`, `git worktree add "$wt" HEAD`)
        — detached, from live HEAD, outside the repo tree.
     2. In `$wt`, apply the minimal mutation that violates the id's behavior
        statement (flip a comparison, return the forbidden value, drop a
        call) — one mutation, aimed at the discriminating property.
     3. Run only the tagged test's package in `$wt`, e.g.
        `cd "$wt" && go test ./internal/ledger/...` (substitute the real
        package path).
     4. Failing → `covered`; surviving → `weak`. Record the exact mutation and
        the observed result either way.
     5. **Teardown unconditionally**, same turn: `git worktree remove --force
        "$wt"` (run from `ledger/`, or wherever the worktree was added from).
        No mutation ever touches the live checkout.
4. **Apply the wiring lens.** For every surface this Decision declares
   externally reachable (an HTTP route, an MCP tool/endpoint, an event
   subscription), confirm at least one of its ids' tests reaches that surface
   through `cmd/ledger/main.go`'s composition root (a real `appkit.Spec`-wired
   server driven over `net/http/httptest`, or the composed boot smoke) —
   never a handler/service constructed directly by the test in isolation. A
   surface no id's test reaches this way is an `unwired surface` finding,
   naming the surface and that it should be reached via
   `cmd/ledger/main.go`/`cmd/ledger/main_test.go`.
5. **Append** the `## D<N>` section to `project/audit/REPORT.md` (never
   overwrite what's already there) — one entry per id plus any `unwired
   surface` finding, in the shape below.
6. Flip that Decision's line `⬜ → ✅` in `project/audit/STATUS.md`.
7. Report `NEXT`.

**Case D — Finish** (no `⬜` line remains in `project/audit/STATUS.md`):

1. Append `## Summary` to `project/audit/REPORT.md`: counts per verdict across
   the whole run, the greppable work-queue line, and the report's absolute
   path.
2. Report `DONE`, echoing the report's absolute path in the message.

The only exits are the Case A red-baseline refusal and Case D's finish;
everything else is `NEXT`, so an interrupted run resumes at the first `⬜`
with all prior findings intact.

## The `project/audit/REPORT.md` shape

```
# ledger — Audit Report

- baseline: green (`go test ./...` exit 0, plus build/vet/gofmt/GOWORK=off)   [or the red-baseline refusal, with the exact failing command/output]
- denominator: <N> ids across <M> Decisions

## Structural sweep
- orphan tags: pass | <offending ids + file:line>
- duplicate assignment: pass | <offending ids>
- coverage drift: pass | <offending ids, by direction>
- INDEX staleness: pass | <diff lines>
- criteria trace: pass | <missing section, or unmapped criteria/ids>

## D<N> — <title>
- R-XXXX-XXXX — <verdict>
  behavior: <the design's behavior statement, quoted>
  test: <file:line of the tagged test, or "none">
  finding: <one or two sentences: why the verdict; for weak/mismatched, what
            the test actually proves vs. what it should>
  escalation: <"none" | "mutated <what>; tagged test failed (verdict upgraded)"
              | "mutated <what>; tagged test survived">
- unwired surface — <route/tool/subscription> (only when the wiring lens found
  one)

## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path>
```

## Boundaries

- Never edit source, tests, `project/design/*`, `project/plan/*`, or
  `project/product/*` — read design ids by grep and read only the one
  `DNN.md` being audited this turn.
- Never commit anything, and never flip a marker outside
  `project/audit/STATUS.md`.
- Mutations only ever happen in a scratch worktree created and torn down
  (unconditionally) within the same turn; never touch the live checkout.
- When a static read is genuinely unsure and escalation is impractical,
  verdict `weak` with the doubt stated in the finding — uncertainty is never
  `covered`.
- Never trust a tag's presence as proof — the assertion is the evidence.
- Treat a skipped or statically-unreachable tagged test as `weak`, never
  `covered`.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for
  the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red baseline);
  the loop stops.
- `message` — one short, plain sentence, e.g.
  `Audited D9: 7 covered, 1 weak (R-4WLS-RJH6).`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal. A failed workspace
identity guard is **never** `DONE` — report `NEXT` instead, so cwd drift
cannot be misread as "audit complete." In every other case end on `NEXT`.
Keep `message` a single plain sentence — not a JSON object or code block.
