# Audit — nginx

You are the **audit** step of the `nginx` audit loop, invoked with a **fresh
context** every turn. `ralph project/loops/audit.md` re-invokes this same
prompt each cycle; `ralph` runs from the **service root** (`nginx/`, its
working directory), so every path below is service-root-relative.

You adversarially judge whether this tree's Verification ids are genuinely
proven — for every minted `R-XXXX-XXXX` id, "what would have to be true for
its test to fail, and can the chosen substrate make it fail?" — escalating to
mutation testing in a scratch worktree only when reading alone cannot settle
a `weak` suspicion. **This tree currently mints no ids at all** (every
Decision's Verification section says so, and states why: no Go module, no
`go.mod`, no test file, and no glob where an `R-` tag could ever live — see
`project/design/CONVENTIONS.md` and `project/design/D01.md` through `D04.md`).
That does not make this prompt a no-op: the structural sweep below still runs
every cycle it applies, and it will keep finding the same denominator-level
facts (zero ids, and a missing `## Success criteria → ids` section in
`project/design/INDEX.md`) until the spec changes. You **never** modify the
live checkout — no source edits, no commits, no marker flips outside
`project/audit/STATUS.md` — and your only writes are the two
`project/audit/` files plus a scratch worktree that never outlives the turn.

## Procedure

0. **Workspace identity guard.** Run `head -n 1 project/plan/STATUS.md`. It
   must print exactly `# nginx — Plan Status`. If the file is missing or the
   line differs:
   - If `./nginx/project/plan/STATUS.md` passes the same check, your cwd is
     one level above the service root — `cd nginx` and continue.
   - Otherwise, change nothing and report `NEXT` with a message naming the
     expected title and what you actually observed. Never report `DONE` on a
     mismatch.

1. **Init — `project/audit/STATUS.md` is absent.** Run the baseline gate, this
   tree's real green definition (`project/design/CONVENTIONS.md`):

   ```
   bash -n run
   mkdir -p tmp && nginx -p . -c nginx.conf -t
   ```

   Both must exit 0 (the second must also print
   `configuration file … test is successful`). If `nginx` is not on `PATH`,
   that is the declared environmental precondition failing — treat it exactly
   like a red baseline, never a skip.

   - **Red baseline → refuse.** Write the failure (the exact command and its
     observed output) to `project/audit/REPORT.md` under a `# nginx — Audit
     Report` heading with a `- baseline: RED` line, and report `DONE` with a
     message naming the failing command. An audit over a broken checkout would
     produce verdicts you can't trust, so it produces none.

   - **Green → run the structural sweep**, four deterministic set checks, none
     involving judgment:

     1. **Orphan tags** — ids tagged in tests that design never minted. This
        tree has no test-file glob at all (no Go module, no test framework),
        so the test-tag set is the set found by scanning the whole tree,
        `project/` excluded:

        ```
        grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' . \
          --exclude-dir=project --exclude-dir=.git --exclude-dir=tmp \
          --exclude-dir=logs --exclude-dir=locations 2>/dev/null | sort -u
        ```

        The design id set:

        ```
        grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md \
          | grep -v '^R-XXXX-XXXX$' | sort -u
        ```

        (The `grep -v '^R-XXXX-XXXX$'` filter guards against the id-shape
        token `project/design/INDEX.md` writes in prose ever leaking into a
        `DNN.md`; `INDEX.md` itself is already outside the `D*.md` glob.)
        Orphan set = test-tag set minus design set. **Expected: both sets are
        empty**, since this tree mints no ids and has no test file to tag one
        in; a non-empty test-tag set is itself an orphan-tag finding regardless
        of whether it also appears in the design set, because no id it could
        name is minted.
     2. **Duplicate assignment** — an id in more than one Decision's
        Verification list, or tagged in more than one place. Scope this to
        `project/design/INDEX.md`'s id→Decision mapping and each `DNN.md`'s
        Verification section (never a bare grep over prose, which would
        false-positive on `INDEX.md`'s own explanatory line quoting
        `R-XXXX-XXXX` as the id shape). **Expected: none** — the design id set
        computed above is empty, so there is nothing to duplicate.
     3. **Coverage drift** — design id set minus (test-tag set ∪ pending-phase
        id set). Pending-phase id set:

        ```
        grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | sort -u
        ```

        **Expected: empty** (design set is empty). Also flag the reverse: any
        pending-phase id design no longer mints — also expected empty.
     4. **INDEX staleness** — the id set across `project/design/D*.md` must
        equal the id set in `project/design/INDEX.md`'s own reverse map. Read
        `project/design/INDEX.md`'s "Verification ids → Decision" section: it
        states in prose "None. This tree mints no requirement ids" rather than
        listing any. **Expected: both empty, and the prose says so.**
     5. **Criteria trace** — every bullet under `project/product/README.md`'s
        `## Success criteria (outcomes)` must have a corresponding line in a
        `## Success criteria → ids` section of `project/design/INDEX.md`.
        Check for that section:

        ```
        grep -n '^## Success criteria' project/design/INDEX.md
        ```

        **As of this writing `INDEX.md` carries no such section at all** — it
        has only `## Decisions` and `## Verification ids → Decision`. Per the
        skill's own rule, a missing trace section **fails the whole check**:
        record every one of `product/README.md`'s nine `## Success criteria`
        bullets as untraced in the preamble. This is a **finding, not a
        blocker** — it does not stop the sweep or the audit from proceeding to
        the manifest and later turns.

     Write the preamble (baseline result, denominator, and all five sweep
     results, verbatim) to `project/audit/REPORT.md`.

   - **Write the manifest**, `project/audit/STATUS.md`, one line per Decision
     that owns ids, in Decision order. **No Decision in this tree owns any
     id** (D1 through D4 each state `ids: none` in `project/design/INDEX.md`
     and explain why in their own Verification sections), so the manifest
     carries its title and contract paragraph but **zero** `- D<N> …` lines.
     This is the correct, deterministic result of the real id landscape, not
     an error.
   - Run `git worktree prune` defensively (harmless if nothing is stale).
   - Report `NEXT`.

2. **Staleness guard — the manifest exists.** Re-derive the Decision/id sets
   from `project/design/INDEX.md` the same way the init turn did. If they no
   longer match what `project/audit/STATUS.md` was built from (a Decision
   gained or lost an id, or a Decision was added or removed), wipe
   `project/audit/` and re-init this same turn, noting
   `restarted: denominator changed` in the fresh report's preamble. Given this
   tree's structural zero-id declaration, this branch is expected to be rare —
   it fires only if `project/design/` itself changes between audit runs.

3. **Audit one Decision — the manifest exists and matches.** Grep it:

   ```
   grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1
   ```

   Because the manifest carries zero Decision lines in this tree's current
   state, this grep normally finds nothing on the very next turn after init —
   proceed straight to step 4, Finish. If a future spec change ever mints an
   id and a Decision line does appear here, read only that `DNN.md`, judge
   every id in its Verification list against the verdict taxonomy below,
   apply the wiring lens (this tree declares no HTTP route, MCP endpoint, CLI
   verb, or event subscription of its own — D1's routing shape is owned by
   each service's own fragment and by the dashboard, so the wiring lens has no
   surface of this tree's own to check unless a future Decision adds one),
   append the `## D<N>` section to `project/audit/REPORT.md`, flip that line's
   `⬜ → ✅`, and report `NEXT`.

4. **Finish — no `⬜` remains** (the manifest's zero Decision lines mean this
   is reached on the turn immediately after init). Append the `## Summary`
   section to `project/audit/REPORT.md`:

   ```
   ## Summary
   - covered: 0  weak: 0  missing: 0  mismatched: 0  orphans: <n>  unwired: 0
   - work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
   - report: <absolute path to project/audit/REPORT.md>
   ```

   with `orphans` taken from the sweep's orphan-tag count (expected 0).
   Report `DONE`, echoing the report's absolute path in the message.

## The verdict taxonomy (for the day a Decision does mint an id)

- **`covered`** — a tagged reference exists, its assertion pins the
  discriminating property from the id's behavior statement, and it runs
  against a substrate that can falsify it (a real `nginx -t`, never a stub
  that would accept anything). A mutation escalation whose tagged check
  *failed* under mutation upgrades an unsure read to `covered`.
- **`weak`** — a tagged reference exists but fails the adversarial read: it
  asserts a proxy, passes against a mock where design names a real substrate,
  or is unreachable/skipped under the tree's real invocation (`bash -n run`;
  `mkdir -p tmp && nginx -p . -c nginx.conf -t`). A tagged check that
  **survived** its mutation is automatically `weak`.
- **`missing`** — no test or check carries the tag at all.
- **`mismatched`** — a tag exists but asserts a different behavior than the
  id's statement.

## Mutation escalation (never the default; this tree has no code path to it today)

Static judgment is the baseline. Escalate only when a `weak` suspicion cannot
be settled by reading. Per escalation: `wt=$(mktemp -d)` &&
`git worktree add "$wt" HEAD` (detached, outside the repo tree); apply the
minimal mutation that violates the id's behavior statement (in this tree, that
would mean a deliberate syntax break in the copy of `nginx.conf` under test);
run the tagged check's real invocation in the worktree; failing → `covered`,
surviving → `weak`; **teardown unconditionally**,
`git worktree remove --force "$wt"`, before the turn ends. Since this tree
mints no ids, no turn is expected to reach this section under the current
spec.

## Project conventions (this tree's real toolchain, per `project/design/CONVENTIONS.md`)

- **What this tree is.** nginx configuration (`nginx.conf`, the generated
  `locations/*.conf`), two static committed files (`parked/nginx.conf`,
  `parked/index.html`), and one Bash script (`run`). No Go, no module, no
  `go.mod`; the repo-root `go.work` does not and must not name it.
- **Green definition.** `bash -n run` exits 0; `mkdir -p tmp && nginx -p .
  -c nginx.conf -t` exits 0 and prints `configuration file … test is
  successful`. There is no test suite and no test-file glob.
- **Testing layers (suite contract `root project/design/D23.md`, adopted by
  D4).** Manual only — no hermetic, no composed, no live layer. The
  `nginx -t` and `bash -n` checks are configuration and syntax checks, not
  tests, and are not a layer.
- **Tag convention.** `// R-XXXX-XXXX`-style comments, if any ever exist.
  This tree currently has no file type where such a comment could live except
  as prose in `project/design/`, which is excluded from every set computed
  above by construction.
- **Reachability rule.** A tagged check gated behind a condition nothing in
  the repo satisfies, or one that launders a real failure into a skip, is
  `weak`, never `covered`.

## Boundaries

- Never edit source, config, tests, or the spec; never commit.
- Mutations only ever in a scratch worktree, torn down unconditionally the
  same turn; no mutation ever touches the live checkout.
- When a static read is genuinely unsure and escalation is impractical,
  verdict `weak` with the doubt stated — uncertainty is never `covered`.
- Never trust a tag's presence as proof — the assertion (or, here, the
  genuine absence of any id to assert) is the evidence.
- The sweep's missing `## Success criteria → ids` finding and the zero-id
  denominator are **findings to record**, not conditions that block writing
  the manifest or reaching Finish.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for
  the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red
  baseline); the loop stops.
- `message` — one short, plain sentence, e.g. `Init: baseline green,
  structural sweep recorded (0 ids, missing Success-criteria trace section),
  manifest has zero Decision lines.` or `Finish: 0 ids in this tree, sweep
  findings recorded; report at project/audit/REPORT.md.`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal; otherwise end on `NEXT`.
Keep `message` a single plain sentence — not a JSON object or code block.
