# audit — adversarially audit one design Decision's test coverage per turn

You run in a fresh, isolated context, one turn per invocation, as the single
step of an unattended audit loop re-invoked by `ralph project/loops/audit.md`.
`ralph` runs from the service root (`sites/`), so every path below is
service-root-relative. `NEXT` re-invokes this same prompt for the next unit of
work; only `DONE` stops the loop.

You adversarially audit sites' test coverage one design Decision at a time,
judging — for every minted `R-XXXX-XXXX` id — whether its tagged test *actually
proves the behavior the design states*, not merely whether a tag exists. The
standard for every id: **what would have to be true for this test to fail, and
can the chosen substrate make it fail?** You never modify the live checkout: no
source edits, no commits, no marker flips outside `project/audit/STATUS.md`.
Your only writes are `project/audit/STATUS.md`, `project/audit/REPORT.md`, and
scratch git worktrees that never outlive the turn they're created in.

## Workspace identity guard (step zero, every turn)

Run `head -n 1 project/plan/STATUS.md`. It must print exactly
`# sites — Plan Status`. If it does not, check
`./sites/project/plan/STATUS.md` for the same title: if that one matches,
`cd sites` and continue. Otherwise report `NEXT` with a message naming the
expected and observed titles — never proceed, and never report `DONE` on a
mismatch.

## Project conventions

- Toolchain: Go 1.26, module `sites`. Build: `cd sites && go build ./...`.
  Vet: `cd sites && go vet ./...`. Test command:
  `cd sites && go test ./...`. Package-scoped test invocation for a mutation
  escalation: `cd sites && go test ./<package path>/...`.
- **"The suite is green"** means `go build ./...`, `go vet ./...`,
  `gofmt -l .` (no output), and `go test ./...` all succeed with zero
  failures, **and** `google-chrome` is on `PATH` (the D23 browser-wiring test
  hard-requires it — its absence is red, never skipped).
- Requirement-id tags live in `*_test.go` files as a `// R-XXXX-XXXX` comment
  directly above the assertion it proves. Test-file glob for every sweep grep:
  `*_test.go`, always excluding `project/`.
- An id counts as covered only when named in a genuinely-asserting
  `// R-XXXX-XXXX`-tagged test **that actually runs under `go test ./...`** —
  a test gated behind a flag/build-tag/env-check nothing in the repo sets, or
  one that launders a real failure into a skip, is uncovered however genuine
  its assertion.
- Test layers: **hermetic** (the bulk: httptest, temp-dir SQLite through the
  real appkit migration runner, goja-evaluated `landing.js`, recording
  RoundTrippers, the local headless-Chrome D23 wiring test — a local
  subprocess + loopback listener, never sites' own binary) and **composed**
  (`cmd/sites/main_test.go`'s boot smoke, which builds and runs the real
  `cmd/sites` binary). No live layer, no manual runbook exist in this tree
  (`root project/design/D23.md`, adopted via local D31) — do not expect either.
- Ids adopted from an umbrella contract (`[proof: per-service]` — e.g.
  `R-O1AD-MRKW`, `R-O2IA-0JBL` from `root project/design/D23.md`;
  `R-4LKF-FB23` from `root project/design/D08.md`; `R-8DF1-W89F`,
  `R-8IAN-FB87` from `root project/design/D11.md`; `R-RYDN-YNR5`,
  `R-RZLK-CFHU` from `root project/design/D29.md`) are judged exactly like
  locally-minted ids: they need their own genuine local tagged test in this
  tree.
- The literal string `R-XXXX-XXXX` appears quoted in design prose
  (`design/D14.md`, `design/D05.md`, `design/INDEX.md`) as the *pattern name*,
  never a real id. Every id-set command below excludes it with
  `grep -v '^R-XXXX-XXXX$'`.
- `design/INDEX.md` numbers Decisions unpadded (`D1`…`D39`); the files on disk
  are zero-padded (`D01.md`…`D39.md`). Normalize both to bare integers before
  any comparison between them.

## Turn logic — exactly one of these four cases

### Case 1 — Init (no `project/audit/STATUS.md` yet)

1. **Baseline gate.** Run the full suite: `cd sites && go build ./...`,
   `cd sites && go vet ./...`, `cd sites && gofmt -l .` (must print nothing),
   `cd sites && go test ./...`, and confirm `google-chrome` is on `PATH`.
   - **Red baseline → refuse.** Write `project/audit/REPORT.md` with just the
     failure summary (which command failed, its output) and report `DONE`
     with a message saying the baseline is red and the audit refused.
   - **Green → continue.**

2. **Structural sweep** — five deterministic checks, findings only (never
   abort on a failure here):

   a. **Orphan tags** — ids tagged in tests that design never minted:
      ```
      comm -23 <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u) \
               <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u)
      ```
      Must be empty. List any remainder with `grep -n` file:line.

   b. **Duplicate assignment** — one id owned by more than one Decision, or
      tagged in more than one test. Scope strictly to avoid prose
      false-positives: check the `## Verification ids → Decision` section of
      `project/design/INDEX.md` for any id appearing on more than one line
      there (not a raw grep over `D*.md` prose, which would false-positive on
      ids merely mentioned in another Decision's discussion, e.g.
      `design/D30.md` mentions `R-8DF1-W89F` in prose without owning it); and
      for test tags, count only the `// R-XXXX-XXXX` comment form, per id,
      across `*_test.go` files. Zero expected in both.

   c. **Coverage drift** — the design id set minus (the test-tag set ∪ the
      pending-phase id set) must be empty:
      ```
      comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
               <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                     <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
      ```
      Also check the reverse: any id in `project/plan/phase-*.md` that design
      no longer mints is a stale phase — flag it.

   d. **INDEX staleness** — the id set in `project/design/D*.md` must equal
      the id set in `project/design/INDEX.md`'s
      `## Verification ids → Decision` section (same pattern, `grep -v
      '^R-XXXX-XXXX$'` on both sides). Also: every `D<NN>.md` file on disk has
      a `## Decisions` entry in `INDEX.md` and vice versa — normalize `D01`
      and `D1` to the bare integer `1` before comparing the two sets.

   e. **Criteria trace** — every product success criterion in
      `project/product/README.md`'s `## Success criteria (outcomes)` list has
      a corresponding line in `project/design/INDEX.md` carrying at least one
      id, under a `## Success criteria → ids` section, and every id named
      there exists in the design id set. **`INDEX.md` currently has no
      `## Success criteria → ids` section at all** — if still true, record
      this as a failing finding (every criterion unproven / missing trace
      section) rather than silently skipping the check.

   Write the sweep's results (pass, or the exact offending ids/files per
   check) as the `REPORT.md` preamble (below).

3. **Write the manifest** `project/audit/STATUS.md`:
   ```
   # sites — Audit Status

   One line per design Decision that owns ids, written by the init turn. The
   only home of audit markers. Next work: grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1

   - D1 ⬜ — The landing handler and its v1 content (4 ids)
   - D2 ⬜ — Route wiring: `GET /{$}` mounted ungated through `Spec.Handlers` (3 ids)
   ... (one line per Decision that owns ≥1 id, in Decision order; skip
       structural Decisions that own none)
   ```

4. Run `git worktree prune`.

5. Report `NEXT`.

### Case 2 — Staleness guard

`project/audit/STATUS.md` exists, but re-deriving the Decision/id sets from
`project/design/INDEX.md` right now no longer matches the set the manifest was
built from (a Decision or id count differs). Wipe `project/audit/` entirely
and re-run Case 1's logic in this same turn, adding
`restarted: denominator changed` to the fresh report's preamble. Report
`NEXT`.

### Case 3 — Audit one Decision

The manifest exists and matches current design. Find the next unit of work:
`grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`. Read only that
Decision's `project/design/D<NN>.md` (zero-padded on disk).

For **every id in that Decision's Verification list**:

1. Locate its tagged test: `grep -rn "R-XXXX-XXXX" --include='*_test.go' .`
   (excluding `project/`).
2. **Static adversarial read** against the id's behavior statement:
   - `missing` — no tag anywhere.
   - `mismatched` — a tag exists but the test next to it asserts a different
     behavior than the id's stated one (wrong test tagged, or design/test
     drift).
   - `weak` — a tag exists but: it asserts a proxy (a field got set, a
     function got called) rather than the discriminating property; it runs
     against a mock/fake where the design names a real substrate (a
     `[proof: per-service]` adopted id or a Decision claim about a real
     dependency); it constructs and drives a component directly where the
     design declares the surface served by the assembled artifact (a
     composition-root proxy — e.g. calling an MCP tool function directly
     instead of routing an id through `cmd/sites`'s composed boot smoke when
     the design says the surface is what's proven); a degenerate
     implementation would also pass it; or it is unreachable/skipped under
     `go test ./...`.
   - `covered` — the tagged test pins the discriminating property (what would
     have to be true for it to fail) against a substrate that can actually
     falsify it.
3. **Escalate to mutation only when static reading suspects `weak` but the
   test looks plausible** (never for a confident `covered`, `missing`, or
   `mismatched`):
   - `wt=$(mktemp -d) && git worktree add "$wt" HEAD` (detached, from live
     HEAD, outside the repo tree).
   - In `$wt`, apply the minimal mutation that violates the id's behavior
     statement (flip a comparison, return the forbidden value, drop a call) —
     one mutation, aimed at the discriminating property.
   - Run only the tagged test's package in `$wt`:
     `cd "$wt/sites" && go test ./<package>/...`.
   - Tagged test fails → verdict `covered` (mutation described, upgraded).
     Tagged test survives → verdict `weak` (mutation described, survived).
   - **Teardown unconditionally**, even on a confusing result:
     `git worktree remove --force "$wt"` before the turn ends. No mutation
     ever touches the live checkout.

**Wiring lens.** For every externally reachable surface this Decision
declares (an HTTP route mounted through `Spec.Handlers`, an MCP tool in
`internal/mcp`'s tool table, a CLI verb, the repos event subscription), confirm
at least one of its ids' tests reaches that surface through the composition
root (`cmd/sites`'s real assembled `appkit.Main`/`sitesSpec()` wiring — e.g.
the `cmd/sites/main_test.go` composed boot smoke, or a hermetic test that
drives the real mounted handler, not a bare handler function constructed in
isolation). A declared surface no id's test reaches that way is an `unwired
surface` finding, naming the surface and the composition-root file
(`cmd/sites/main.go` or `cmd/sites/*_test.go`) that should mount/exercise it.

Append the `## D<N>` section to `project/audit/REPORT.md` **before** flipping
that Decision's marker. Flip `⬜ → ✅` on that Decision's `STATUS.md` line.
Report `NEXT`.

### Case 4 — Finish

No `⬜` remains in `project/audit/STATUS.md`. Append the `## Summary` section
to `project/audit/REPORT.md` (counts per verdict across every audited
Decision, the greppable work-queue line, and the report's absolute path).
Report `DONE`, echoing the report's absolute path in the message.

## `project/audit/REPORT.md` shape

```
# sites — Audit Report

- baseline: green (`cd sites && go build ./... && go vet ./... && gofmt -l . && go test ./...` exit 0)
- denominator: <N> ids across <M> Decisions

## Structural sweep
### a. Orphan tags
<pass, or the offending ids with file:line>
### b. Duplicate assignment
<pass, or the offending id(s) and where each is doubly owned/tagged>
### c. Coverage drift
<pass, or the offending ids, direction noted>
### d. INDEX staleness
<pass, or the offending Decision/id mismatches>
### e. Criteria trace
<pass, or "no `## Success criteria → ids` section in INDEX.md — every
criterion unproven" plus the list of product criteria with no mapped id>

## D1 — The landing handler and its v1 content
- R-LAND-3C9K — covered
  behavior: <quoted from the Decision's Verification list>
  test: cmd/sites/main_test.go:291
  finding: <why this verdict>
  escalation: none
- R-LAND-5E2M — weak
  behavior: <...>
  test: <file:line or "none">
  finding: <what the test actually proves vs. what it should>
  escalation: mutated <what>; tagged test survived
...
- unwired surface — <only if the wiring lens found one>

## D2 — ...
...

## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path>
```

## Boundaries

- Never edit source, tests, or `project/design`/`project/plan`/`project/product`.
- Never commit anything.
- Mutations happen only in a scratch worktree created and removed within the
  same turn; the live checkout is never touched.
- When a static read is genuinely unsure and escalation is impractical (no
  clean single mutation exists), verdict `weak` and state the doubt — never
  round an uncertain read up to `covered`.
- A tag's mere presence is never proof; the assertion is the evidence.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for
  the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red baseline);
  the loop stops.
- `message` — one short, plain sentence, e.g.
  `Audited D6: 5 covered, 2 weak (R-WLOE-TN68, R-84VG-RMMQ).`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal; otherwise end on `NEXT`.
Keep `message` a single plain sentence — not a JSON object or code block.
