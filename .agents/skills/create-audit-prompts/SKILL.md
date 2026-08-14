---
name: create-audit-prompts
description: Explicit workflow for creating or regenerating the single-prompt audit loop under project/loops. Use only when the user explicitly invokes this skill or clearly asks to create or regenerate the coverage audit prompt.
---
First use the project-local `$ikispec` skill (the `project/` artifact contracts), then proceed.

Create the **single-prompt audit loop** an unattended harness (`ralph`) re-invokes with a **fresh context** every turn to adversarially audit the project's test coverage one design Decision at a time: `project/loops/audit.md`. The audit answers a stronger question than "does a tagged test exist?" — for every minted `R-XXXX-XXXX` id it judges whether the tagged test *actually proves the behavior the design states*, escalating to mutation testing in a scratch worktree when reading alone cannot settle whether the test can fail.

Assume the `project/` structure already exists in the shapes `ikispec` defines. This workflow writes only `project/loops/audit.md`; it does not audit anything, never edits the spec, and never touches the build loop's prompts.

## The loop's shape (what you are emitting)

A one-prompt cycle driven by `ralph project/loops/audit.md` — `ralph` runs from the **service root** and re-invokes the single prompt each turn; `NEXT` wraps straight back to the same prompt. State lives only in two **transient, gitignored** files under `project/audit/`:

- **`project/audit/STATUS.md`** — the manifest: one `- D<N> ⬜`/`✅` line per design Decision that owns ids, written by the init turn; each audit turn greps it for the first `⬜`, exactly like the build loop's phase lookup.
- **`project/audit/REPORT.md`** — the findings, **append-only within a run**: a preamble from the structural sweep, then one `## D<N>` section per audited Decision, then a final `## Summary`. It survives any crash because each turn appends its section before flipping its marker.

Both are point-in-time snapshots, never committed — a fresh audit starts from a fresh denominator. The implicit contract: **the spec does not move while an audit runs** (the staleness guard below enforces it by restarting).

Each turn is one of four cases:

- **Init** — `project/audit/STATUS.md` is absent. Run the baseline gate: the suite, with the exact test command from design's Conventions. **Red baseline → refuse**: write the failure summary to `REPORT.md` and report `DONE` (an audit over a broken checkout would produce verdicts you can't trust, so it produces none). Green → run the **structural sweep** (four deterministic set checks, below), write its results as the report preamble, write the manifest (one line per Decision that owns ids, in Decision order), run `git worktree prune` defensively, and report `NEXT`.
- **Staleness guard** — the manifest exists, but re-deriving the Decision/id sets from `project/design/INDEX.md` no longer matches what the manifest was built from. Wipe `project/audit/` and re-init this same turn, noting `restarted: denominator changed` in the fresh report's preamble.
- **Audit one Decision** — the manifest exists and matches; grep it for the first `⬜` Decision, read only that `DNN.md`, judge every id in its Verification list (verdicts and escalation below). Then apply the **wiring lens**: for every surface the Decision declares externally reachable (an HTTP route, an MCP endpoint, a CLI verb, an event subscription), at least one of its ids' tests must reach that surface through the composition root — the assembly path production uses — not a component the test constructs itself; a surface where no id's test does so is recorded as an `unwired surface` finding in the `## D<N>` section, naming the surface and the composition-root file that should mount it. Append the `## D<N>` section to `REPORT.md`, flip that line's `⬜ → ✅`, report `NEXT`.
- **Finish** — no `⬜` remains. Append the `## Summary` section (counts per verdict, the greppable work-queue line, the report's absolute path) and report `DONE`, echoing the report path in the message.

The only exits are the red-baseline refusal and the finish turn; everything else is `NEXT`, so an interrupted run resumes at the first `⬜` with all prior findings intact.

### The structural sweep (init turn; four deterministic checks)

Each is a grep-and-set-compare with a defined pass criterion — no judgment involved:

1. **Orphan tags** — ids tagged in tests that design never minted: the sorted test-tag set minus the sorted design set must be empty. Any remainder is listed per id with its file:line.
2. **Duplicate assignment** — an id appearing in more than one Decision's Verification list, or tagged in more than one test (one id, one behavior, one place). Zero expected.
3. **Coverage drift** — the coverage invariant: the sorted design id set minus the union of the test-tag set and the sorted set over the pending `project/plan/phase-*.md` (if any) must be empty — every current id is realized in tests or queued in exactly one pending phase. Also flag the reverse on the plan side: a pending phase carrying an id design no longer mints is stale. Differences listed by direction.
4. **INDEX staleness** — the id set in the `DNN.md` files must equal the id set in `INDEX.md`, and every Decision file must have an index entry (and vice versa).
5. **Criteria trace** — every product success criterion has a line in `INDEX.md`'s `## Success criteria → ids` section carrying at least one id, and every id in that section exists in the design id set. A criterion with no mapped id is an unproven promise; a missing trace section fails the whole check. (Whether the mapped tests genuinely prove their criterion end-to-end is judged per Decision, not here.)

Bake the concrete commands into the prompt (the design set via `grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | sort -u`, the test set via the same pattern over the project's real test-file glob **excluding `project/`**, etc.). Sweep failures do not abort the audit — they are findings, recorded in the preamble so the per-Decision turns that follow aren't silently distorted by them.

### The verdict taxonomy (one verdict per id)

- **`covered`** — a tagged test exists, its assertion pins the **discriminating property** from the id's behavior statement (the "what would have to be true for this test to fail" standard), and it runs against a substrate that can falsify it. A mutation escalation whose tagged test *failed* under mutation upgrades an unsure read to `covered`.
- **`weak`** — a tagged test exists but fails the adversarial read: it asserts a proxy (a field was set, a function was called), passes against a mock where the design names a real substrate, constructs and drives a component directly where the design declares its surface served by the assembled artifact (the **composition-root proxy** — the component is proven, the wiring is not), a degenerate implementation would also pass it, or it is unreachable/skipped under the suite's real invocation. A tagged test that **survived** its mutation is automatically `weak`, with the mutation described.
- **`missing`** — no test carries the tag at all.
- **`mismatched`** — a tag exists but the test asserts a *different* behavior than the id's statement (tag pasted on the wrong test, or design and tests have drifted).

`weak` and `mismatched` stay separate because the fix differs: a weak test gets strengthened; a mismatched one signals design/test drift, which may be a spec problem.

### Mutation escalation (the tiebreaker, never the default)

Static judgment is the baseline for every id. Escalate **only** when the read suspects `weak` but the test looks plausible — when "could this test actually fail?" cannot be settled by reading. Confident `covered`, `missing`, and `mismatched` verdicts never escalate (missing has nothing to mutate; mismatched is decided by reading). Per escalation:

1. `wt=$(mktemp -d)` && `git worktree add "$wt" HEAD` — detached, from the live checkout's HEAD, **outside the repo tree**.
2. In the worktree, apply the **minimal mutation that violates the id's behavior statement** — flip the comparison, return the forbidden value, drop the call. One mutation, aimed at the discriminating property.
3. Run the tagged test's **package** (not the full suite) in the worktree — the question is only "can *this* test fail".
4. The tagged test failing → `covered`; surviving → `weak`. Record the mutation and the observed result either way.
5. **Teardown unconditionally** — `git worktree remove --force "$wt"` before the turn ends, even on a confusing result. No mutation ever touches the live checkout.

One id, one mutation, one worktree, torn down the same turn.

## How to work

1. Read `project/design/CONVENTIONS.md` and `project/design/INDEX.md` (pull a `DNN.md` or two as concrete examples). **`CONVENTIONS.md` is the single source of truth for the toolchain** — take from it the exact **test command**, the exact **build command**, what "the suite is green" concretely means, and the **test layout** (where tagged tests live, the test-file glob for the sweep's greps). **Define the coverage convention yourself, identically to the build generator** (design mints the ids but deliberately does not own how coverage is measured): an id counts as covered only when named in a `// R-XXXX-XXXX` comment on a test that genuinely asserts the behavior (never a bare literal) **and that test actually runs under the suite's real invocation** — a test gated behind a flag nothing in the repo sets, or one that launders a real failure into a skip, is uncovered however genuine its assertion. This is generic, so it is the same across every project; do not read it from another file.
2. **Every check the prompt runs must be deterministic where it can be, and adversarial where it must be.** The sweep and the baseline gate are pure commands with pass criteria; the per-id verdict is a judgment, but one anchored to the falsifiability standard and, when unsure, to a mutation whose outcome is a command result. If design's Conventions do not state the toolchain concretely enough to bake real commands in, **terminate**: do not emit the prompt; surface what the design must state first.
3. Write `project/loops/audit.md` in the shape below (substitute the real commands, globs, and paths — no placeholders), ensure `project/audit/` is listed in `.gitignore`, and report the path.

## The `project/audit/` artifacts (the prompt writes them)

**`project/audit/STATUS.md`** — mirror the plan manifest's grep discipline: a title, a one-line contract paragraph (one line per id-owning Decision, the only home of audit markers, next-work lookup is `grep -nE '^- D[0-9]+ .* ⬜' project/audit/STATUS.md | head -1`, no bare glyph outside Decision lines), then the lines:

```
- D<N> ⬜ — <Decision title> (<count> ids)
```

**`project/audit/REPORT.md`** — the deliverable:

```
# <name> — Audit Report

- baseline: green (`<test command>` exit 0)   [or the red-baseline refusal]
- denominator: <N> ids across <M> Decisions

## Structural sweep
<one subsection per check: pass, or the exact offending ids/files>

## D<N> — <title>
- R-XXXX-XXXX — <verdict>
  behavior: <the design's behavior statement, quoted>
  test: <file:line of the tagged test, or "none">
  finding: <one or two sentences: why the verdict; for weak/mismatched, what
            the test actually proves vs. what it should>
  escalation: <"none" | "mutated <what>; tagged test failed (verdict upgraded)"
              | "mutated <what>; tagged test survived">
- unwired surface — <route/verb/subscription> (only when the wiring lens found
  one: no id's test reaches this declared surface through the composition
  root; names the file that should mount it)

## Summary
- covered: <n>  weak: <n>  missing: <n>  mismatched: <n>  orphans: <n>  unwired: <n>
- work queue: grep -E 'R-.* (weak|missing|mismatched)|unwired surface' project/audit/REPORT.md
- report: <absolute path>
```

Verdict-first on the id line keeps the gap list greppable — the work-queue grep is the audit's product.

## Output shape of the prompt file

The prompt is **self-contained** (read in a fresh, isolated context) and **autonomous** (one turn per invocation, no internal loop, all state in `project/audit/`, default to progress over questions). It ends with a **"Reporting the result"** section binding the turn to the loop's `status`/`message` contract at the **semantic layer** — never prescribing a transport.

**Frontmatter is entirely an operator option — never add any.** This workflow never writes a frontmatter block of its own, for any reason; which keys the prompt carries (`harness:`/`model:` backend routing, `max-retries:`, anything else ralph accepts) is the operator's choice alone. The workflow's only frontmatter duty is preservation: if a prompt being replaced carries a frontmatter block, copy it over verbatim into the regenerated prompt — regeneration must never strip or alter the operator's keys.

Do **not** tell the model to emit a literal JSON object, call a named tool, or set "structured-output fields" — the harness supplies the `{status, message}` schema out of band and reads only the **final** message of a turn. Describe the contract generically and present **all three** status values (codex-style backends coerce every streamed message into the schema, so mid-turn narration needs the non-terminal value):

> ## Reporting the result
>
> Report this run's result as a `status` and a one-sentence `message`:
> - `CONTINUE` — **non-terminal**: any progress message you stream *before* the turn's final message. You are still working; this never advances the loop.
> - `NEXT` — **terminal**: this turn's work is done; re-invoke this prompt for the next.
> - `DONE` — **terminal**: the audit is complete (or refused on a red baseline); the loop stops.
> - `message` — one short, plain sentence, e.g. `Audited D3: 4 covered, 1 weak (R-XXXX-XXXX).`
>
> End on `DONE` only when no `⬜` Decision remains (echo the report's absolute path in the message) or on the red-baseline refusal; otherwise end on `NEXT`. Keep `message` a single plain sentence — not a JSON object or code block.

This prompt is the only one in its loop, so — unlike the build loop's build/verify — it legitimately owns `DONE`, in exactly those two cases.

### `project/loops/audit.md`

- **Framing** — the audit step of a single-prompt loop; adversarial by default ("what would have to be true for this test to fail, and can the chosen substrate make it fail?"); reads the design and the tests, **never modifies the live checkout** — no source edits, no commits, no marker flips outside `project/audit/STATUS.md`; its only writes are the two `project/audit/` files, `project/issues/<slug>.md` findings notes (the carve-out in Boundaries), and scratch worktrees that never outlive the turn.
- **Procedure** — the four-case turn above, in order: init (baseline gate → refuse-on-red → structural sweep → manifest → `git worktree prune`), staleness guard (re-derive from `INDEX.md`, wipe-and-restart on mismatch), audit the first `⬜` Decision (locate each id's tagged test with the baked-in grep excluding `project/`; static adversarial read against the id's behavior statement; mutation escalation per the recipe only when unsure; append the `## D<N>` report section **before** flipping the marker), finish (append `## Summary`, `DONE`). Bake in the real commands: the test command, the package-scoped test invocation for escalations, the sweep greps with the project's real test-file glob.
- **Project conventions** — inline the real toolchain, the "green" definition, the tag convention (`// R-XXXX-XXXX` comments), and the reachability rules (a skipped or statically-unreachable tagged test is `weak`, never `covered`).
- **Boundaries** — never edit source, tests, or the spec; never commit; mutations only ever in a scratch worktree, torn down unconditionally the same turn; when the static read is genuinely unsure and escalation is impractical, verdict `weak` with the doubt stated (uncertainty is never `covered`); never trust a tag's presence as proof — the assertion is the evidence. One carve-out: a finding *outside* the audit's own verdict taxonomy (a bug in the code itself, a stale doc, a broken tool) is filed as a short prose note in `project/issues/<slug>.md` (`ikispec`'s gate-exempt findings tray), left uncommitted for the operator; filing never widens the audit, and verdicts themselves never move to the tray — they belong in `REPORT.md`.
