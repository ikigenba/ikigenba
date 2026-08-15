---
name: create-gather-build-verify-prompts
description: Explicit workflow for creating or regenerating the gather-build-verify build-loop prompts under project/loops. Use only when the user explicitly invokes this skill or clearly asks to create or regenerate the gather/build/verify loop prompts.
---
First use the project-local `$ikispec` skill (the `project/` artifact contracts), then proceed.

Create the **three-prompt build loop** an unattended harness (`ralph`) re-invokes with a **fresh context** every turn to build the project one phase at a time: `project/loops/gather.md`, `project/loops/build.md`, and `project/loops/verify.md`. The three prompts share the ephemeral `project/loops/brief.md` seam. This workflow also writes the executable operator wrapper `project/loops/run`. It writes only those four files: it does not build anything and never edits the spec.

Assume the `project/` structure already exists in the shapes `ikispec` defines: the product doc, optional research, the addressable design (`CONVENTIONS.md` + the `INDEX.md` manifest + one `DNN.md` per Decision, each ending in minted `R-XXXX-XXXX` Verification ids), and the addressable plan (the `STATUS.md` manifest, which holds the `Next phase` counter and is the only home of the `⬜` markers, plus one `phase-NN.md` body per pending phase). The plan is a work queue, never a history: completing a phase deletes its line and its body file.

## Roles: gather, build, verify

Three prompts, each read in a fresh isolated context, each with one job:

- **gather orients.** The only prompt that reads the big design/plan docs, and the only one that ever stops the loop. It finds the active phase and writes the one-phase **brief** that build and verify consume, so neither of them opens a big doc. Writes no code, commits nothing.
- **build implements.** Reads only the brief. Does a bounded, idempotent turn of the phase's work (code, id-tagged tests, run the suite, format) and commits it. Never judges completeness, never touches `STATUS.md`.
- **verify judges.** The independent gate, re-deriving truth from scratch every run (it never trusts build's claims). It retires a passed phase, or records the still-open gaps for the next build, or, when a phase stops making progress, blocks. Writes no production code.

The loop runs `gather → build → verify → gather …`, driven by `project/loops/run`.

## The run wrapper and the status contract

`project/loops/run` is the operator's entry point. Its contents:

```sh
#!/bin/bash

if [ -f project/loops/blocked.md ]; then
  echo "⚠  BLOCKED — the previous run stopped on a phase it could not satisfy:"
  echo
  cat project/loops/blocked.md
  echo
  echo "Fix the phase's done bar in project/, then re-run to continue."
  rm -f project/loops/brief.md
fi
rm -f project/loops/blocked.md

exec ralph project/loops/gather.md project/loops/build.md project/loops/verify.md
```

Make it executable. On startup it **announces** a `blocked.md` left by a prior run (the one moment the operator is at the terminal) instead of hiding it, then clears both the block and the now-stale brief so the next `gather` rebuilds the phase from the corrected spec. `ralph` runs from the **service root** (its working directory), so every workspace path the prompts reference is service-root-relative (`project/…`).

`ralph` reads only the **final** message of each turn and cycles on a two-value terminal contract:

- **`NEXT`** — terminal: this turn is done, advance to the next prompt (wrapping `verify → gather`).
- **`DONE`** — terminal: **stop the loop.** DONE is the wire signal to `ralph` and nothing more; it carries **no** meaning about *why* the run stopped. The reason (all phases built, or a phase blocked) lives only in the human-readable `message`.
- **`CONTINUE`** — **non-terminal**: the status a streaming model tags the progress messages it emits *before* its terminal message. It never advances or ends the loop. It exists because codex coerces *every* streamed message into the schema, so a model narrating mid-turn needs a non-terminal value for those and a terminal `NEXT`/`DONE` for the last; `ralph` acts on the last message alone.

Only **gather** ever reports `DONE`. build and verify always end on `NEXT`.

## How the loop converges without a human

verify can neither stop the loop nor advance a phase on a gap, so an incomplete phase just stays `⬜` and the loop re-attacks it next cycle, now with verify's grounded feedback in front of build and without gather re-reading the big docs (gather no-ops on the in-flight brief). The brief's feedback region gives verify **cross-cycle memory in one project-local place**: it counts consecutive cycles that close no gap, which distinguishes slow convergence (the open-gap set shrinking) from a true stall. After **three** cycles with no gap closed, verify writes `project/loops/blocked.md` (the phase, the unsatisfied ids, the exact failing command and output, and how to unblock) and the next gather stops the loop on sight of it.

The loop's only stops are therefore `gather → DONE`: either no `⬜` phase remains, or a phase is blocked awaiting the operator (plus `ralph`'s own budget rails). There is no separate stall/reset tier and no cross-run state file: the stall count lives in the brief, so it is per-project by construction and cannot collide across services.

## How to work

1. **Read the spec and extract the concrete facts the loop bakes in.** Read `project/design/CONVENTIONS.md` (the single source of truth for the toolchain — make no assumptions), `project/design/INDEX.md` (pull individual `DNN.md` as needed), `project/plan/STATUS.md` (the plan rules live in `ikispec`), and `project/product/README.md` for intent (a phase body or two for a concrete example). Extract:
   - the **build/typecheck command**, the **test command**, and what "the suite is green" concretely means (all from CONVENTIONS.md). **"Green" also requires the tiered lint gate** — `../bin/lint <name>` (invoked from the module root; `bin/lint` derives the repo root from its own location, so no `cd`) must exit 0. `bin/lint` enforces the tree's own registered `.lint-tier` marker (absent or `off` passes vacuously; `cheap`/`strict` enforce that tier), so this one command is correct for every tree regardless of tier and needs no per-tier branching in the prompt. A tree with no lint module target (a non-Go tree, or the umbrella that builds no code) omits this line;
   - the **source/test layout** and any architectural seams the design mandates (pure-core/impure-shell, determinism seams), including the **test-placement rule**: where unit tests live relative to the code they exercise (package-local, named for the behavior) and the single home for cross-package integration tests, so the prompts forbid per-phase or root-level test files;
   - the **pending marker** (`⬜` in `STATUS.md`; there is no done marker, a completed phase's line and body file are deleted) and the **next-phase lookup** `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1` (phase lines are `- `-prefixed bullets; the `Next phase: NN` counter is not a bullet and never matches);
   - the **id resolution path**: a Decision → its `DNN.md` via `INDEX.md`; a single id via `grep -n R-XXXX-XXXX project/design/INDEX.md`.
2. **Define the coverage convention and the done bar yourself** — design mints the `R-` ids but deliberately does not own how coverage is measured. Use a greppable default: an id counts as covered only when named in a `// R-XXXX-XXXX` comment on a test that genuinely asserts the behavior (never a bare literal) **and that test actually runs under the suite's real invocation**. **Reachability is part of covered:** a tagged test held out of the run by an env flag, build tag, or skip condition nothing in the repo satisfies is **uncovered**, however genuine its assertion; likewise a test that converts a real failure (non-zero exit, unparseable output) into a skip. A skip is never acceptable green for a requirement test. A phase is done when every id it owns is covered **and** the suite is green; a structural phase carries no ids and is proven by the green build plus any integration smoke it names. **Carry design's test-placement rule into the convention:** tests are co-located with the code they exercise and named for the behavior, never gathered into a per-phase or root-level test file. State the concrete placement (the package-local test path, and the single home for cross-package integration tests) extracted from design, and require both gather (in the brief's done bar) and build (in its procedure) to enforce it.
3. **Every exit condition must be deterministic** — a mechanically-evaluable pass/fail predicate, reproducible on identical repo state, whose passing state is actually **reachable** (a green suite, an exit code, an exact match count). A bar resting on a **subjective judgment** ("documents adequately", "reads clearly") or an **unsatisfiable/self-referential check** (classically a `grep` for a phrase the phase's own `project/` docs also contain, so it can never return empty) will make the loop spin forever. So will a bar whose **command semantics** put its expected output out of reach regardless of implementation (the known trap: `go list -deps X` always prints `X`, so "prints exactly `Y`" can never pass; prefer **absence** and **count** forms over "prints exactly"). **Execute every structural done-when command against the current tree before emitting the loop** and compare real output to what the bar expects; an error because the code does not exist yet (`no such package`) is fine, but output that structurally cannot match is a defect. If the design does not state the toolchain clearly enough to write concrete commands, **or any phase's exit conditions cannot be made deterministic, terminate**: do not emit the loop, and name the specific phase + condition to fix upstream. Only when every phase's bar is deterministic do you write the files.
4. Write `project/loops/gather.md`, `build.md`, and `verify.md` in the shapes below (substitute the real commands/conventions from design, no placeholders), write `project/loops/run` exactly as shown and make it executable, ensure `project/loops/brief.md` and `project/loops/blocked.md` are in `.gitignore`, and report the paths.

## The `project/loops/brief.md` contract (gather emits it)

The brief is the seam that scopes build and verify to one phase: the complete and only input they consume, so neither opens design or plan. It is self-contained (it carries the realized Decision's full design prose and the full requirement text of the phase's ids), **never committed** (gitignored), **single-phase**, and **phase-scoped, not per-cycle**: gather creates it when a phase first becomes the active `⬜` phase, it **persists across cycles while that phase stays `⬜`**, and verify deletes it only when the phase passes. It is **region-owned by one writer each**, so the two never clobber each other:

- a **gather-owned contract region** — the phase identity, the full design prose of each realized Decision (its Verification list excluded), the ids to cover with full requirement text, files to touch, dependency interface signatures, and the done bar. Written once when the phase becomes active; verify never writes here.
- a **verify-owned feedback region** — the current attempt counter, the no-progress streak, the build commit last observed, and the currently-open gaps. gather never writes here; on its no-op for an in-flight phase it leaves this region untouched.

Give it a strict, grep-able schema. The contract region carries at minimum: the phase id + one-line objective; the realized Decision id(s) and their file paths; the **full design prose of each realized Decision** (Decision statement, shape/signatures, rejected alternatives) copied verbatim from the `DNN.md` **with that Decision's Verification list omitted** (build must not see ids the phase does not own); the **ids to cover** — *only* the ids the phase's body/`Done when` lists, **one per line, each in the exact form `R-XXXX-XXXX — <full requirement text copied verbatim from the Verification list>`** (id at line-start, then its complete requirement prose on the same line, never a bare id and never the text on a separate line), so `grep -oE '^R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/loops/brief.md` yields exactly this phase's id set; an explicit `(none — structural phase)` line when the phase owns no ids; the files to touch; the **dependency interface signatures** copied in; and the **done bar**. The feedback region is a single `## Verify feedback — attempt N` heading carrying the attempt counter, the **no-progress streak**, the build commit verify observed, and a checklist of **only the open gaps**, each tied to one `R-id` and grounded in the exact failing command/output (never free prose). gather writes this region **empty** on a fresh brief; verify **overwrites** it (never appends) each gap cycle; build reads it but never writes it.

## Writing the prompts

Each prompt is **self-contained** (read in a fresh, isolated context; it cannot rely on the others being read) and **autonomous** (one iteration per invocation, no internal loop, all state in the workspace, default to progress over questions). Each ends with a short **"Reporting the result"** section that binds the turn to the loop's `status`/`message` contract at the **semantic layer** (*which* status and *what* the message says) and **never prescribes a transport**.

**Frontmatter is entirely an operator option — never add any.** This workflow writes no frontmatter block on any prompt; which keys a prompt carries (`harness:`/`model:` routing, `max-retries:`, anything else `ralph` accepts) is the operator's choice alone. The only frontmatter duty is **preservation**: if a prompt being replaced carries a frontmatter block, copy it into the regenerated prompt verbatim.

**Do not tell the model to emit a literal JSON object, call a named tool, or set "structured-output fields."** The harness supplies the `{status, message}` schema out of band and reads it back itself (`ralph` injects it per backend: codex via `--output-schema`, claude via `--json-schema` surfaced as a synthetic `StructuredOutput` tool), so a prompt hard-coding one transport is wrong on the other. Describe only the contract, generically, and have **every** prompt present **all three** status values. Write each prompt's closing section in this shape:

> ## Reporting the result
>
> Report this run's result as a `status` and a one-sentence `message`:
> - `CONTINUE` — **non-terminal**: any progress message you stream *before* the turn's final message. You are still working; this never advances the loop.
> - `NEXT` — **terminal**: this turn's work is done; hand off to the next prompt.
> - `DONE` — **terminal**: tells `ralph` to stop the loop. It carries no other meaning; say *why* in the message. *(this line is for `gather.md` only — see the build/verify form below)*
> - `message` — one short, plain sentence describing what happened, e.g. `<example>`.
>
> *<the prompt's own terminal rule — see below>.* Keep `message` a single plain sentence, not a JSON object or code block.

In `build.md` and `verify.md`, replace the `DONE` line above with this verbatim:

> - `DONE` — **terminal — never yours to report**: telling `ralph` to stop is never your job. Even a fully finished phase (green suite, every gap closed) is still `NEXT`; only gather ever reports `DONE`.

This wording is load-bearing: the observed failure mode is a build model that closes the last gap with a green suite and reaches for `DONE` because the work "feels finished." The annotation preempts exactly that inference. `CONTINUE` is available to every prompt as the non-terminal progress status but is never a terminal value.

**Unrelated findings are filed, never fixed — every prompt carries this rule.**
A defect, stale doc, or gap a turn notices that is outside its brief (or, for
gather, outside its phase lookup) is recorded as a short prose note in
`project/issues/<slug>.md` — what was observed, where, and why it is out of
scope — and the turn then continues its own work unchanged. `project/issues/`
is the gate-exempt findings tray, the one `project/` path the loop may write
beyond its named mutations (per `ikispec`); filing never widens the turn,
never fixes the finding, and never changes the turn's status. build stages any
new issue files with its increment commit and verify with its retirement
commit; a file written by gather waits, untracked, for the next committing
turn.

**The workspace identity guard — every prompt's step zero.** The suite is a mono-repo of **nested** spec workspaces, so a step whose shell cwd drifts (a harness cwd reset, a stray `cd` to the repo root) lands in a *different but valid* `project/` tree, classically the umbrella workspace at the repo root, whose plan legitimately holds zero `⬜` lines. That turns cwd drift into a false `DONE`. Each prompt must therefore open with an identity assertion baked to the concrete service: `head -n 1 project/plan/STATUS.md` must print exactly `# <name> — Plan Status` (the real name substituted at generation time). On mismatch or a missing file the step must **not** proceed and must **never** report `DONE`: if `./<name>/project/plan/STATUS.md` passes the same check, the cwd drifted one level up — `cd <name>` and continue; otherwise return `NEXT` with a message naming the expected and observed titles, so the drift is visible instead of silently ending or misdirecting the run.

### `project/loops/gather.md`
- **Framing** — the only prompt that reads the big docs; owns the brief's **contract region** for exactly one phase; writes no code, runs no tests, commits nothing; preserves an in-flight brief rather than regenerating it every cycle.
- **Procedure** — **step zero is the workspace identity guard.** Then, **if `project/loops/blocked.md` exists, open no other file and return `DONE`** with a message naming the blocked phase and pointing at that file. Otherwise grep `STATUS.md` for the first `⬜` phase → if none, return **`DONE`** (a message like "no pending phases"). Otherwise check for an existing `project/loops/brief.md` and read its `# Brief — Phase NN` header: **if it names this same phase, the phase is mid-flight — leave the brief exactly as is (both regions untouched), open no big doc, and return `NEXT`.** Only when there is no brief, or the brief names a phase with no `STATUS.md` line left (completed, hence deleted), author a fresh brief: read only that `phase-NN.md`, resolve its Decision(s) via `INDEX.md` and read only those `DNN.md`, determine the ids to cover (**only** the ids the phase lists, a slice of a Decision's Verification ids, never all of them), copy each realized Decision's **full design prose** verbatim **minus its Verification list**, copy each covered id's **full requirement text** verbatim (no out-of-scope ids), extract the dependency packages' **public interface signatures**, and write `brief.md` to the schema with an **empty feedback region**. Return `NEXT`.
- **Boundaries** — read only the one phase file + its realized Decision file(s) + dependency interfaces; never build/test/commit; never write the feedback region or touch an in-flight brief.

### `project/loops/build.md`
- **Framing** — reads **only** `project/loops/brief.md`, never the big docs; does a bounded, idempotent turn of the remaining work and commits it; never decides completeness, never touches `STATUS.md`.
- **Procedure** — read the **whole** brief, contract and feedback regions both (if it is missing/empty, make no changes and return `NEXT`); **if the feedback region lists open gaps, close those first** — they are the exact, command-grounded items the gate found unsatisfied last cycle. **Do as much of the brief as cleanly fits this turn, ideally the whole phase, preferring fewer fuller turns over many thin increments** (an incomplete phase is just re-attacked next cycle). See what exists (`grep -rn "R-XXXX-XXXX" <test glob>`; run the suite to read failures); build the named package(s), consuming dependencies only through the brief's copied interface signatures; write id-tagged asserting tests **co-located with the code they exercise and named for the behavior, never in a per-phase or root-level test file**; format; **before committing, check this turn's own diff for dropped tags** — any removed line matching `R-[A-Z0-9]{4}-[A-Z0-9]{4}` outside `project/` (`git diff HEAD | grep -E '^-.*R-[A-Z0-9]{4}-[A-Z0-9]{4}'`) must be restored first (a rewrite extends a file's tests, it never drops a tagged one); commit this turn's increment (no empty commit) with a phase-naming message and the repo trailer. Always return `NEXT`.
- **Project conventions** — inline the real toolchain, the "suite is green" definition, test styles, determinism seams, and the **test-placement rule** from design.
- **Boundaries** — never read design/plan/product; never remove an existing `R-`-tagged test; never edit `STATUS.md` or delete a phase file; never write the brief (feedback region included); always return `NEXT`.

### `project/loops/verify.md`
- **Framing** — the independent gate; the only prompt that retires a phase (deletes its `STATUS.md` line + body file and the brief) or blocks it (writes `project/loops/blocked.md`, which the next gather turns into `DONE`); it ends every turn on `NEXT` and never advances a phase on a gap; writes no production code. It **re-derives current truth from scratch every run** — it never trusts build's claims or its own prior feedback as input (its prior feedback is read only to measure progress, not believed).
- **Procedure** — read the brief, contract region and its own prior feedback region both (if missing/empty, return `NEXT`). **Every coverage check is a deterministic command with a defined pass criterion**, and any `grep`-style check is **scoped to exclude `project/`** so it can never match the workspace docs that quote the pattern. Run the full suite (all green) **and confirm no `R-XXXX-XXXX`-tagged test reported `SKIP`** (a skipped requirement test is a gap). **Run the tiered lint gate** (`../bin/lint <name>`, which must exit 0); it enforces the tree's registered `.lint-tier` (absent/`off` vacuous, `cheap`/`strict` enforced), so a lint finding at the registered tier is an open gap, not a pass — carry this into the "suite is green" definition and the boundaries' retire bar (`green build + green test + clean lint + full coverage + clean ratchet`). For every id in the brief confirm a genuinely-asserting `// R-XXXX-XXXX` tagged test **that actually runs under the suite's real invocation** — statically trace the run (the test command plus every skip/build-tag/env gate guarding that test) and treat a test gated behind a flag nothing in the repo sets, or one that turns a real failure into a skip, as **uncovered** (structural phase → green + named smoke instead). Then run the **global coverage ratchet**: the design id set (`grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | sort -u`) minus the union of the test-tag set (the same pattern over the project's real test-file glob, excluding `project/`) and the pending-phase id set (the same pattern over `project/plan/phase-*.md`) must be **empty** — because the plan is a work queue, any minted id not owned by a pending phase was already retired and must stay covered; each id in the remainder is a **coverage regression** (its dropped tagged test is recoverable from git history). Collect the set of **open gaps** (each an uncovered/failing id with the exact command + observed output proving it open).
  - **Pass** (no open gaps) → delete **only this phase's** `- Phase NN …` line from `STATUS.md` (never the `Next phase` counter line, never another phase's line), `git rm project/plan/phase-NN.md`, commit the deletion with the trailer, and `rm -f project/loops/brief.md`. Return `NEXT`.
  - **Gap** → leave `⬜`, change no source, and **measure progress against the prior feedback region**: read its attempt counter `N` and its prior open-gap id set. *Progress* means the current open-gap id set is a **strict subset** of the prior (some gap that was open is now closed) → set the streak to 0. Anything else is *no progress* → increment the streak. **A new build commit is never progress and never resets the streak** (a builder that cannot satisfy a bar will keep committing plausible rewordings; a detector keyed on commit motion reads that churn as convergence and never trips). Then:
    - **Block** — when the streak reaches **3** (three consecutive attempts closing no gap), the phase is not converging and only the operator can change its bar (`project/` is read-only to the loop). Write `project/loops/blocked.md` naming the phase, the total attempts, the still-unsatisfied ids, and the **exact command and observed output** that will not go green, plus the unblock recipe: *fix the phase's done bar in `project/plan/phase-NN.md`; if the bar is a prove-a-negative or otherwise untestable claim, reshape it per `ikispec`'s bounded-test rule (a chokepoint positive, a bounded enumeration, or a mechanism check); then re-run.* Leave the marker `⬜`, **do not delete the brief**, and return `NEXT` — the next gather sees `blocked.md` and reports `DONE`.
    - **Otherwise** — **overwrite** (never append) the `## Verify feedback — attempt N` region with attempt `N+1`, the streak, the observed build commit, and a checklist of **only** the current open gaps (each `R-id` + the exact failing command + observed output, + file:line when known). Do **not** delete the brief. Return `NEXT`.
- **Boundaries** — never write/fix production code; never write the contract region; never retire a phase on anything short of green + full coverage; the ratchet's id-set greps over `project/design/D*.md` and `project/plan/phase-*.md` extract id tokens and are not "reading the big docs" in the forbidden sense; when uncertain a test really asserts, treat the id as uncovered; **treat a skipped or statically-unreachable id test as uncovered**; always return `NEXT`.
