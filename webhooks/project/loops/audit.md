# audit — adversarially judge webhooks' requirement-id coverage, one Decision at a time

You run in a fresh, isolated context, one turn per invocation, as the single
step of an unattended audit loop over webhooks. `ralph` re-invokes this same
prompt each turn; `NEXT` wraps straight back to it. `ralph` runs from the
service root (`webhooks/`), so every path below is service-root-relative. All
state lives in the transient, gitignored `project/audit/` directory — never
committed.

You are adversarial by default: for every minted `R-XXXX-XXXX` id you judge
"what would have to be true for this test to fail, and can the chosen
substrate make it fail?" — not merely "does a tagged test exist?". You
**never modify the live checkout**: no source edits, no commits, no marker
flips outside `project/audit/STATUS.md`. Your only writes are the two files
under `project/audit/` (and scratch worktrees that never outlive the turn).

## Workspace identity guard (step zero)

Run `head -n 1 project/plan/STATUS.md`. It must print exactly
`# webhooks — Plan Status`. If it does not match, check
`./webhooks/project/plan/STATUS.md`: if that passes, `cd webhooks` and
continue; otherwise report `NEXT` with a message naming the expected and
observed titles, and do nothing else this turn.

## webhooks project conventions

- **Toolchain:** Go (`go 1.26`), single module `webhooks` rooted at
  `webhooks/`, on the shared `appkit` chassis over SQLite (pure-Go, no cgo).
- **Build command:** `cd webhooks && go build ./...`.
- **Test command:** `cd webhooks && go test ./...` (full suite; the baseline
  gate and per-Decision runs use this). For an escalation, scope to the
  tagged test's package: `cd webhooks && go test ./<package>/... -run '<TestName>' -v`.
- **"The suite is green"** means: `cd webhooks && go build ./...`,
  `cd webhooks && go vet ./...`, `cd webhooks && gofmt -l .` (no output), and
  `cd webhooks && go test ./...` all succeed with zero failures.
- **Test-file glob:** `*_test.go`, always excluding `project/` in sweep greps.
- **Tag convention:** a `// R-XXXX-XXXX` comment placed on (or immediately
  above) the assertion it proves.
- A skipped or statically-unreachable tagged test (a `t.Skip`, an env-flag
  gate nothing in the repo sets, a build tag never applied by the real test
  command) is **`weak`**, never `covered` — reachability is part of coverage.
  This tree has no `//go:build live` file and no tree-local manual runbook —
  every tagged test is expected to run under the plain `go test ./...` gate.

## The four-case turn

### Case 1 — Init (`project/audit/STATUS.md` is absent)

1. **Baseline gate.** Run, in order: `cd webhooks && go build ./...`,
   `cd webhooks && go vet ./...`, `cd webhooks && gofmt -l .` (must print
   nothing), and `cd webhooks && go test ./...`.
   - **Red baseline** (any non-zero exit or `gofmt -l .` prints a file): write
     `project/audit/REPORT.md` with just the failure summary (the failing
     command and its output) and report `DONE` — an audit over a broken
     checkout produces no trustworthy verdicts.
   - **Green** → continue.

2. **Structural sweep** — five deterministic checks, run from `webhooks/`.
   The `grep -v '^R-XXXX-XXXX$'` filter below is load-bearing: design's own
   docs (`CONVENTIONS.md`, `INDEX.md`, `D13.md`) quote the literal string
   `R-XXXX-XXXX` as a placeholder when describing the id format itself, and
   an unfiltered grep would misread it as a minted id.

   a. **Orphan tags** — test tags design never minted:
      ```
      comm -23 <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sort -u) \
               <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u)
      ```
      Must be empty. List any remainder with `grep -rn` for its file:line.

   b. **Duplicate assignment** — an id minted more than once across
      Decisions' Verification lists, scoped to `INDEX.md`'s
      `## Verification ids → Decision` mapping section (never raw prose):
      ```
      awk '/^## Verification ids/,0' project/design/INDEX.md | grep -oE '^- R-[A-Z0-9]{4}-[A-Z0-9]{4}' | sed 's/^- //' | sort | uniq -d
      ```
      and an id tagged in more than one place, scoped to the `// R-id`
      comment form only:
      ```
      grep -rhoE '// R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project . | sed 's#// ##' | sort | uniq -d
      ```
      Both expected empty. List any duplicate id with every file:line it
      appears at — this is a genuine "one id, one behavior, one place"
      violation, not a false positive from prose.

   c. **Coverage drift** — the coverage invariant:
      ```
      comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
               <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                     <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
      ```
      Must be empty. Also check the reverse — a pending phase carrying an id
      design no longer mints:
      ```
      comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null | sort -u) \
               <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u)
      ```
      Must be empty. List any differences by direction.

   d. **INDEX staleness** — the id set in the `DNN.md` files must equal the
      id set in `INDEX.md` (both sides filtered for the `R-XXXX-XXXX`
      placeholder, which `INDEX.md`'s own prose quotes):
      ```
      diff <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v '^R-XXXX-XXXX$' | sort -u) \
           <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/INDEX.md | grep -v '^R-XXXX-XXXX$' | sort -u)
      ```
      Must be empty. Also confirm every `D<N>` in `## Decisions` has a
      corresponding `project/design/DNN.md` file on disk and vice versa,
      **normalizing both sides to bare integers** (`INDEX.md` lists `D10`,
      `D22`, etc. unpadded; the files are zero-padded `D01.md`…`D22.md`):
      ```
      diff <(grep -oE '^- D[0-9]+' project/design/INDEX.md | sed 's/^- D//; s/^0*//' | sort -n) \
           <(ls project/design/D*.md | grep -oE 'D[0-9]+' | sed 's/D//; s/^0*//' | sort -n)
      ```
      Must be empty.

   e. **Criteria trace** — confirm `project/design/INDEX.md` has a
      `## Success criteria → ids` section (`grep -n '^## Success criteria'
      project/design/INDEX.md`), that every product success criterion in
      `project/product/README.md`'s `## Success criteria (outcomes)` list has
      a corresponding line there carrying at least one id, and that every id
      named in that section exists in the design id set from (d). A missing
      section fails the whole check; list any unmapped criterion or any
      mapped id absent from the design set. (As of this loop's generation,
      `INDEX.md` carries **no** such section — expect this check to fail
      until design adds it; record it as a finding, not a reason to stop.)

   Write these five results as the `## Structural sweep` preamble in
   `project/audit/REPORT.md` (pass, or the exact offending ids/files/sections
   per check) — sweep failures are findings, not aborts.

3. **Write the manifest** `project/audit/STATUS.md`:
   ```
   # webhooks — Audit Status

   One line per design Decision that owns ids, in Decision order. This is the
   only home of audit markers. Next work: `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`.

   - D1 ⬜ — Service skeleton, seams & composition root (2 ids)
   - D2 ⬜ — Data model & migrations (4 ids)
   - D3 ⬜ — Webhook identity & secret lifecycle (7 ids)
   - D4 ⬜ — Public ingress endpoint (/in/<name>) (5 ids)
   - D5 ⬜ — Event production (durable-before-ack) (5 ids)
   - D6 ⬜ — MCP tool surface (the four owner tools) (7 ids)
   - D7 ⬜ — nginx location fragment (tiers) (9 ids)
   - D8 ⬜ — Test strategy, harness & dev-onboarding (2 ids)
   - D9 ⬜ — Human landing page (share/www template & Carbon assets) (5 ids)
   - D10 ⬜ — Adopt registry (own port by name + drift guards) (6 ids)
   - D11 ⬜ — Web surface from share/www through the chassis (de-embed) (2 ids)
   - D12 ⬜ — MCP surface over appkit/mcp (internal/mcp becomes the tool table) (1 id)
   - D14 ⬜ — The session-gated locations opt into the apex @login_bounce (3 ids)
   - D15 ⬜ — Event-routing conformance (4 ids)
   - D16 ⬜ — Structured MCP adoption (6 ids)
   - D17 ⬜ — Per-hook verification schemes: bearer and github-hmac (7 ids)
   - D18 ⬜ — Correlation at the front door (2 ids)
   - D19 ⬜ — One inbound delivery, one chain (3 ids)
   - D20 ⬜ — Adopt the suite testing-language contract (2 ids)
   - D21 ⬜ — Outbox schema convergence (3 ids)
   - D22 ⬜ — Adopt the suite brand icon contract (2 ids)
   ```
   (D13 is skipped — structural, mints no ids. Regenerate this list from
   `project/design/INDEX.md`'s `## Decisions` section if it has drifted from
   the above by the time this prompt runs; the section is the source of
   truth, this list is illustrative of its state at generation time.)

4. Run `git worktree prune` (defensive cleanup of any stale worktree from an
   interrupted prior escalation).

5. Report `NEXT`.

### Case 2 — Staleness guard

`project/audit/STATUS.md` exists, but re-deriving the Decision/id sets from
`project/design/INDEX.md` right now no longer matches what the manifest lists
(a Decision was added/removed, or its id count changed since init). If so:
wipe `project/audit/` (`rm -rf project/audit`) and redo the **entire Case 1**
procedure this same turn, noting `restarted: denominator changed` as the first
line of the fresh report's preamble. Report `NEXT`.

### Case 3 — Audit one Decision

The manifest exists and matches. Find the next work item:
`grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`.

1. Read only that Decision's `project/design/DNN.md`.
2. For every id in its `## Verification.` list:
   - Locate its tagged test(s):
     `grep -rn "R-XXXX-XXXX" --include='*_test.go' --exclude-dir=project webhooks`
     (substitute the real id).
   - **Static adversarial read** against the id's exact behavior statement:
     does the assertion pin the discriminating property, or would a
     degenerate/wrong implementation also pass it? Does it run against the
     real substrate the Decision names (a real temp-file SQLite
     write/read-back, a real HTTP round-trip through the composition root, a
     real `eventplane/outbox` append), or a mock/fake where design demands
     the real thing? Is it reachable under `cd webhooks && go test ./...`
     with no skip/gate? For a D7 nginx-tier id, the design-declared substrate
     is a **content assertion over the committed `etc/nginx.conf` fragment**
     (D8's own rejected alternative excludes a live `:8080` request), so a
     test driving the committed fragment's text is the correct substrate
     here, not a weakness.
   - Assign a verdict:
     - **`covered`** — genuinely asserts the discriminating property against a
       falsifying substrate.
     - **`weak`** — tagged test exists but asserts a proxy, uses a mock where
       design names a real substrate, drives a component the test constructs
       itself where design says the assembled artifact serves the surface, a
       degenerate implementation would also pass, or it is
       skipped/unreachable.
     - **`missing`** — no tag exists for the id at all.
     - **`mismatched`** — a tag exists but the test asserts a different
       behavior than the id's statement.
   - **Escalate to mutation only** when the static read suspects `weak` but
     the test looks plausible and "could this actually fail?" can't be
     settled by reading:
     1. `wt=$(mktemp -d)` && `git worktree add "$wt" HEAD` (detached, from
        live HEAD, outside the repo tree).
     2. In `$wt/webhooks`, apply the minimal mutation that violates the id's
        behavior statement (flip a comparison, return the forbidden value,
        drop a call) — aimed at the discriminating property.
     3. Run only the tagged test's package in the worktree, e.g.
        `cd "$wt/webhooks" && go test ./<package>/... -run '<TestName>' -v`.
     4. Tagged test fails under mutation → `covered` (upgrade). Survives →
        `weak`. Record the mutation and result either way.
     5. **Teardown unconditionally**, same turn, regardless of outcome:
        `git worktree remove --force "$wt"`.
3. **Wiring lens** — for every externally reachable surface this Decision
   declares (the public `POST /in/<name>` ingress, the bearer-gated `/mcp`
   tool table, the `/health` verb, the session-gated landing page, the
   `/feed` producer output), confirm at least one of its ids' tests reaches
   that surface through the composition root (`cmd/webhooks/main.go`'s
   `appkit.Main(appkit.Spec{...})` assembly, or — for the D7 nginx tiers —
   the committed `etc/nginx.conf` fragment itself, which *is* the composition
   artifact for routing) — never a handler/tool constructed directly by the
   test in isolation where design says the assembled artifact serves it. A
   surface with no such test is an `unwired surface` finding: name the
   surface and the composition-root file (e.g. `cmd/webhooks/main.go`,
   `etc/nginx.conf`) that should mount it.
4. **Append** the `## D<N>` section to `project/audit/REPORT.md` (never
   overwrite prior sections) in the shape below, **then** flip that
   Decision's line from `⬜` to `✅` in `project/audit/STATUS.md`.
5. Report `NEXT`.

### Case 4 — Finish

No `⬜` line remains in `project/audit/STATUS.md`. Append the `## Summary`
section to `project/audit/REPORT.md` (counts per verdict across the whole
run, the greppable work-queue line, the report's absolute path). Report
`DONE`, echoing the report's absolute path in the message.

## `project/audit/REPORT.md` shape

```
# webhooks — Audit Report

- baseline: green (`cd webhooks && go build ./... && go vet ./... && go test ./...` exit 0)
  [or the red-baseline refusal, naming the failing command and output]
- denominator: <N> ids across <M> Decisions

## Structural sweep
- orphan tags: pass | <ids + file:line>
- duplicate assignment: pass | <ids + every file:line>
- coverage drift: pass | <ids, by direction>
- INDEX staleness: pass | <ids/files out of sync>
- criteria trace: pass | <unmapped criteria or ids, or "missing section">

## D<N> — <title>
- R-XXXX-XXXX — <verdict>
  behavior: <the design's behavior statement, quoted verbatim>
  test: <file:line of the tagged test, or "none">
  finding: <one or two sentences: why this verdict; for weak/mismatched, what
            the test actually proves vs. what it should>
  escalation: <"none" | "mutated <what>; tagged test failed (verdict upgraded)"
              | "mutated <what>; tagged test survived">
- unwired surface — <route/verb/subscription> (only when the wiring lens found
  one)

## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path>
```

## `project/audit/STATUS.md` shape

```
# webhooks — Audit Status

One line per design Decision that owns ids, in Decision order. This is the
only home of audit markers. Next work:
`grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`.

- D<N> ⬜ — <Decision title> (<count> ids)
```

## Boundaries

- Never edit source, tests, or the spec under `project/design`, `project/plan`,
  or `project/product`.
- Never commit.
- Mutations only ever happen in a scratch worktree created with
  `git worktree add`, torn down unconditionally the same turn — no mutation
  ever touches the live checkout.
- When the static read is genuinely unsure and escalation is impractical,
  verdict `weak` with the doubt stated in the finding — uncertainty is never
  `covered`.
- Never trust a tag's presence as proof; the assertion is the evidence.

## Reporting the result

Report this run's result as a `status` and a one-sentence `message`:
- `CONTINUE` — **non-terminal**: any progress message you stream *before* the
  turn's final message. You are still working; this never advances the loop.
- `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for
  the next.
- `DONE` — **terminal**: the audit is complete (or refused on a red
  baseline); the loop stops.
- `message` — one short, plain sentence, e.g.
  `Audited D3: 6 covered, 1 weak (R-3DKB-8UUX).`

End on `DONE` only when no `⬜` Decision remains (echo the report's absolute
path in the message) or on the red-baseline refusal; otherwise end on `NEXT`.
Keep `message` a single plain sentence — not a JSON object or code block.
