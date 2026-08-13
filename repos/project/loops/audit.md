# audit — adversarially judge one Decision's proof, one turn at a time

You run in a fresh, isolated context, one turn per invocation, as the single
step of an unattended audit loop (`ralph project/loops/audit.md`) that
adversarially judges whether repos' tagged tests actually prove what
`project/design/` claims — one Decision at a time. `ralph` runs from the
service root (`repos/`), so every path below is service-root-relative.

You ask a stronger question than "does a tagged test exist?": for every minted
`R-XXXX-XXXX` id, does the tagged test actually prove the behavior the design
states? You **never modify the live checkout** — no source edits, no test
edits, no spec edits, no commits. Your only writes are the two files under
`project/audit/`, plus scratch git worktrees that never outlive the turn they
were created in.

## Procedure

0. **Workspace identity guard.** Run `head -n 1 project/plan/STATUS.md`. It must
   print exactly `# repos — Plan Status`. If it does not:
   - Check `./repos/project/plan/STATUS.md` with the same command. If **that**
     one prints the expected title, your cwd drifted one directory shallow —
     `cd repos` and continue the procedure below.
   - Otherwise, write nothing, make no changes, and report `NEXT` with a
     message naming the expected and observed titles — never report `DONE` on
     an identity mismatch.

1. **Init** — if `project/audit/STATUS.md` does not exist:
   - **Baseline gate.** Run, from `repos/`:

     ```
     cd repos && go build ./...
     cd repos && go vet ./...
     cd repos && gofmt -l .          # must print nothing
     cd repos && go test ./...
     ```

     **Red baseline → refuse.** If any command fails, create `project/audit/`,
     write `project/audit/REPORT.md` with a preamble stating the failing
     command and its output verbatim, and report `DONE` — an audit over a
     broken checkout produces no trustworthy verdicts.

   - **Green → run the structural sweep** (five deterministic set checks; bake
     in these exact commands):

     1. **Orphan tags** — ids tagged in tests that design never minted:

        ```
        comm -13 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
                 <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u)
        ```

        Empty is pass. Any remainder is an orphan id — list each with its
        file:line (`grep -rn '<id>' --include='*_test.go' .`).

     2. **Duplicate assignment** — an id owned by more than one Decision, or
        tagged in more than one test:

        ```
        # design side: scoped to the actual Verification BULLETS only (lines
        # anchored `- R-XXXX-XXXX` at column 0), never whole-file prose — every
        # Decision's Verification intro sentence and Rejected section may
        # legitimately *mention* an id minted elsewhere (e.g. D26's own
        # parenthetical "R-SPNZ-LS3V through R-SY7A-A6AQ are one scenario",
        # or D14 discussing D1's R-EISY-2LYZ) without that being a second
        # assignment
        grep -hoE '^- R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | sed 's/^- //' | sort | uniq -d

        # test side: the `// R-XXXX-XXXX` tag-comment form only, never a bare
        # literal appearing in a string or table
        grep -rhoE '//\s*R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . \
          | grep -oE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' | sort | uniq -d
        ```

        Zero lines from each is pass. Any id listed is a duplicate assignment
        — record which Decisions or which test files/lines share it. (The test
        side legitimately does find real duplicates on this tree at time of
        writing — e.g. an id tagged on two separate tests — which is exactly
        the kind of finding this check exists to surface, not a false
        positive to filter away.)

     3. **Coverage drift** — the coverage invariant:

        ```
        comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
                 <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                       <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
        ```

        Empty is pass — every current id is realized in tests or queued in a
        pending phase. Also check the reverse (a pending phase carrying an id
        design no longer mints):

        ```
        comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | sort -u) \
                 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u)
        ```

        Empty is pass. List any remainder from either direction, by direction.

     4. **INDEX staleness** — the id set in the `DNN.md` files must equal the
        id set in `INDEX.md`, and every Decision file must have an index
        entry:

        ```
        comm -3 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
                <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/INDEX.md | grep -v '^R-XXXX-XXXX$' | sort -u)

        # Decision-number cross-check, normalized to bare integers on both
        # sides (DNN.md is zero-padded; INDEX's `## Decisions` lines are not)
        ls project/design/D*.md | sed -E 's#.*/D0*([0-9]+)\.md#\1#' | sort -n > /tmp/repos_audit_dfiles.txt
        grep -oE '^- D[0-9]+ →' project/design/INDEX.md | sed -E 's/^- D([0-9]+) →/\1/' | sort -n > /tmp/repos_audit_dindex.txt
        diff /tmp/repos_audit_dfiles.txt /tmp/repos_audit_dindex.txt
        ```

        Empty output from both is pass.

     5. **Criteria trace** — every product success criterion has a line in
        `INDEX.md`'s `## Success criteria → ids` section carrying at least one
        id, and every id in that section exists in the design id set:

        ```
        grep -n '## Success criteria' project/design/INDEX.md
        ```

        A missing section header **fails the whole check** (record it as a
        finding: "no criteria trace section in INDEX.md — every product
        success criterion is an unproven promise until one is added"). If the
        section exists, check each of its id references against the design id
        set from check 3 and flag any that don't resolve, and cross-reference
        `project/product/README.md`'s `## Success criteria (outcomes)` bullets
        against the section's line count to flag any criterion with no line at
        all.

     Sweep failures are findings, not aborts — record them in the report
     preamble and continue.

   - **Write the manifest** `project/audit/STATUS.md`, one line per Decision
     that owns at least one id, in Decision order (D1, D2, D10, D13–D27 per
     `project/design/INDEX.md`'s `## Decisions` section — skip a Decision only
     if its line says "none — structural" or "mints none"), to the schema
     below.
   - Run `git worktree prune` defensively (clears any worktree orphaned by a
     prior crashed run).
   - Report `NEXT`.

2. **Staleness guard** — `project/audit/STATUS.md` exists; re-derive the
   Decision/id sets from `project/design/INDEX.md` right now and diff them
   against what the manifest lists. If they differ (a Decision or id was
   added, removed, or renumbered since init), the spec moved under the audit:
   `rm -rf project/audit`, redo the **init** steps above in this same turn, and
   note `restarted: denominator changed since <prior manifest Decision count>
   Decisions` in the fresh report's preamble. Report `NEXT`.

3. **Audit one Decision** — the manifest exists and matches; find the next
   unit of work:

   ```
   grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1
   ```

   Read **only** that Decision's `project/design/DNN.md`. For every id in its
   `**Verification.**` list:

   - **Locate its tagged test(s)**: `grep -rn '<id>' --include='*_test.go' --exclude-dir=project .`
   - **Static adversarial read.** Ask: *what would have to be true for this
     test to fail, and can the substrate it runs against actually make it
     fail?* Read the test body, not just its name. Judge against the
     taxonomy below.
   - **Escalate to mutation only when genuinely unsure** whether a plausible-
     looking test can fail — never for a confident `covered`, `missing`, or
     `mismatched` read. Recipe:
     1. `wt=$(mktemp -d)` && `git worktree add "$wt" HEAD` (detached, from the
        live checkout's `HEAD`, outside the repo tree).
     2. In `$wt`, apply the minimal mutation that violates the id's
        discriminating property (flip a comparison, return the forbidden
        value, drop a call, invert a check).
     3. Run only the tagged test's **package** in `$wt` — e.g.
        `cd "$wt/repos" && go test ./internal/repos/ -run <TestName> -v` (adjust
        the package path to the file the tagged test lives in).
     4. Tagged test **fails** under the mutation → upgrade to `covered`.
        Tagged test **survives** → `weak`. Record what was mutated and the
        observed result either way.
     5. **Teardown unconditionally**, even on a confusing result:
        `git worktree remove --force "$wt"` before the turn ends. No mutation
        ever touches the live checkout.

   Then apply the **wiring lens**: for every surface this Decision declares
   externally reachable (an MCP tool, an HTTP/loopback route, the git
   smart-HTTP door, an event publish) at least one of its ids' tests must
   reach that surface through the composition root — `cmd/repos/main.go`'s
   real assembly, not a handler or store constructed directly by the test. A
   surface with no such id is recorded as an `unwired surface` finding naming
   the surface and `cmd/repos/main.go` (or the specific mounting file) as
   where it should be wired.

   Append the `## D<N>` section to `project/audit/REPORT.md` **before**
   flipping the marker (so a crash mid-Decision leaves the manifest `⬜` and
   nothing half-recorded), then flip that Decision's `⬜ → ✅` in
   `project/audit/STATUS.md`. Report `NEXT`.

4. **Finish** — no `⬜` remains in `project/audit/STATUS.md`. Append the
   `## Summary` section to `project/audit/REPORT.md` (verdict counts, the
   greppable work-queue line, the report's absolute path). Report `DONE`,
   echoing the report's absolute path in the message.

The only exits are the red-baseline refusal and the finish turn; every other
case ends on `NEXT`, so an interrupted run resumes at the first `⬜` with every
prior finding intact.

## The verdict taxonomy (one verdict per id)

- **`covered`** — a tagged test exists, its assertion pins the discriminating
  property the id's behavior statement names (not a weaker proxy a degenerate
  implementation would also pass), and it runs against a substrate that can
  actually falsify it (the real `git` binary, real temp-file SQLite, the real
  assembled binary for a composed claim — never a stub standing in for one of
  these). A mutation escalation whose tagged test **failed** upgrades an
  unsure read to `covered`.
- **`weak`** — a tagged test exists but fails the adversarial read: it asserts
  a proxy (a field got set, a function got called) rather than the
  discriminating behavior; it constructs and drives a handler/store directly
  where the design declares the surface served by the assembled artifact (the
  **composition-root proxy** — the component is proven, the wiring is not); a
  degenerate implementation would still pass it; or it is unreachable/skipped
  under `go test ./...`'s real invocation. A tagged test that **survived** its
  mutation is automatically `weak`, with the mutation described.
- **`missing`** — no test anywhere carries the tag.
- **`mismatched`** — a tag exists but the test next to it asserts a different
  behavior than the id's statement describes (tag pasted on the wrong test, or
  design and test have drifted apart since either was last touched).

## Project conventions (repos)

- **Toolchain.** Go 1.26, module path `repos`, standalone module at `repos/`.
  Build: `cd repos && go build ./...`. Vet: `cd repos && go vet ./...`. Format:
  `cd repos && gofmt -l .` (must print nothing). Test: `cd repos && go test ./...`.
  **"Green" means all four exit 0 with no failures and no output from
  `gofmt -l .`.**
- **Tag convention.** A requirement id is asserted only via a `// R-XXXX-XXXX`
  comment on the test that discharges it. Requirement-id tags live in the
  `*_test.go` glob.
- **Coverage convention (defined here, not by design).** An id counts as
  covered only when named in a `// R-XXXX-XXXX` comment on a test that
  genuinely asserts the behavior (never a bare literal) **and** that test
  actually runs under `go test ./...`'s real invocation. A test gated behind a
  flag or build tag nothing in the repo sets, or one that launders a real
  failure into a skip, is uncovered however genuine its assertion reads. This
  tree has **no live layer**, so no `//go:build live` gate exists to trace —
  every test file in `*_test.go` is reachable by the plain `go test ./...`
  invocation unless it carries some other, non-standard gate; if you find one,
  treat the ids it guards as uncovered and say so.
- **Test layers, per `root project/design/D23.md` (adopted locally as D16).**
  Two, both in the default gate: **hermetic** (temp-dir filesystems, real
  temp-file SQLite through the embedded migration set, the real `git` binary
  against `t.TempDir()` bare repos and `httptest`/`file://` remotes, local
  subprocesses including the headless-Chrome wiring proof) and **composed**
  (the install-layout boot smoke in `cmd/repos/main_test.go`, which builds and
  runs the real `cmd/repos` binary). No live layer, no tree-local manual
  runbook. `t.Skip`/`t.Skipf`/`t.SkipNow` are banned outright in every
  `*_test.go` file here — any occurrence found while auditing is itself a
  `weak` (or `missing`, if it fully replaces the assertion) finding on the id
  it guards, not just a structural-sweep note.
- **Real substrates only.** Git custody claims are proven only against the
  real `git` binary; a test that drives repos' git logic through a hand-rolled
  fake is `weak` regardless of how thorough its assertions look. repos has no
  service peers, so no id should ever be proven against a stubbed peer client
  — if one is found, it is `weak` (or `mismatched`, if the design demanded a
  real substrate this test never touches).
- **Composition root.** `cmd/repos/main.go` (the appkit chassis boot) is the
  one real assembly path. The MCP tool surface, the loopback filesystem
  routes, the git smart-HTTP door, the landing page, and the nginx fragment
  are all surfaces the wiring lens checks against it.

## Boundaries

- Never edit source, tests, `AGENTS.md`, or anything under `project/design`,
  `project/plan`, or `project/product` — read-only, always.
- Never commit anything, in the live checkout or a scratch worktree.
- Mutations happen only in a scratch worktree created with `git worktree add`,
  and that worktree is removed with `git worktree remove --force` before the
  turn ends, unconditionally — even when the result is confusing or the turn
  is otherwise cut short.
- Never trust a tag's mere presence as proof — the assertion itself, read
  adversarially, is the evidence. When a static read is genuinely unsure and
  escalation is impractical (e.g. the mutation isn't a clean minimal edit),
  verdict `weak` and state the doubt plainly. Uncertainty is never `covered`.

## The `project/audit/` artifacts

**`project/audit/STATUS.md`**:

```
# repos — Audit Status

This is the manifest: one line per id-owning Decision, and the only place an
audit marker lives. Each line is a Markdown bullet beginning with `- D`,
carrying `⬜` (pending) or `✅` (audited). The next unit of work is
`grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`. This file
carries no bare status glyph outside Decision lines.

- D1 ⬜ — Composition root & chassis boot (2 ids)
- D2 ⬜ — Data model & migrations (5 ids)
- D10 ⬜ — nginx fragment & the web locations (5 ids)
- D13 ⬜ — Correlation id capture/strip on the nginx fragment (2 ids)
- D14 ⬜ — Suite-contract conformance: install layout & env contract (3 ids)
- D15 ⬜ — Env-channel conformance (5 ids)
- D16 ⬜ — Adopt the suite testing-language contract (2 ids)
- D17 ⬜ — Custody: bare-repo store, Git seam, ref choke point (7 ids)
- D18 ⬜ — The loopback filesystem + commit API (12 ids)
- D19 ⬜ — The git smart-HTTP door (8 ids)
- D20 ⬜ — Run tokens (6 ids)
- D21 ⬜ — Statuses and the merge verb (10 ids)
- D22 ⬜ — The MCP tool surface (10 ids)
- D23 ⬜ — Events: push and archived families (4 ids)
- D24 ⬜ — The landing page lists live repositories (14 ids)
- D25 ⬜ — Client-side filter, sort, pagination (14 ids)
- D26 ⬜ — Browser wiring proof (9 ids)
- D27 ⬜ — Adopt the suite brand icon contract (2 ids)
```

(Regenerate this list from `project/design/INDEX.md`'s `## Decisions` section
at init time rather than hand-copying it — the counts above are illustrative
of the shape, not a fixed set to trust blindly if the design has moved.)

**`project/audit/REPORT.md`**:

```
# repos — Audit Report

- baseline: green (`cd repos && go build ./... && go vet ./... && gofmt -l . && go test ./...` exit 0)
  [or, on refusal: "RED — refused: <command> failed:\n<output>"]
- denominator: <N> ids across <M> Decisions

## Structural sweep
- orphan tags: pass | <ids + file:line>
- duplicate assignment: pass | <ids + where>
- coverage drift: pass | <ids, by direction>
- INDEX staleness: pass | <mismatched ids / Decision numbers>
- criteria trace: pass | <missing section, or unmapped criteria/ids>

## D<N> — <title>
- R-XXXX-XXXX — <verdict>
  behavior: <the design's behavior statement, quoted verbatim>
  test: <file:line of the tagged test, or "none">
  finding: <why this verdict; for weak/mismatched, what the test actually
            proves vs. what it should>
  escalation: <"none" | "mutated <what>; tagged test failed (upgraded to
              covered)" | "mutated <what>; tagged test survived">
- unwired surface — <route/verb/subscription> (only when the wiring lens found
  one)

## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path to project/audit/REPORT.md>
```

Verdict-first on the id line keeps the gap list greppable — the work-queue
grep is the audit's product.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for
  the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red baseline);
  the loop stops.
- `message` — one short, plain sentence, e.g.
  `Audited D17: 6 covered, 1 weak (R-J8LY-MKEF).`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal; otherwise end on `NEXT`.
Keep `message` a single plain sentence — not a JSON object or code block.
