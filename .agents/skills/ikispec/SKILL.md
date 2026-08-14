---
name: ikispec
description: "The project/ spec contracts: authoritative output shapes of product, research, design, and plan; authority boundaries; and hard invariants every writer keeps. Loaded by $open-spec, $grill-me, $seal-spec, and the loop-prompt generator workflows — not spoken by the user directly; use whenever any of those read or write the project/ spec."
---

# Spec Shapes

This skill is the **single source of truth for what the `project/` tree looks
like**. Everything that writes or reasons about the spec or spec-authoring
workflow — an `$open-spec` session's discussion, a `$grill-me` interrogation,
`$seal-spec`, and the loop-prompt generator workflows — takes the shapes below
from here and restates them nowhere.

## The workspace: `project/`

Everything needed to design, plan, and build the application lives under
`project/`, at the root of the codebase it governs. Every artifact has exactly
one writer:

| folder | what's in it | written by |
|---|---|---|
| `product/` | `README.md` — the *why*: problem, users, scope, promises, success criteria | `$seal-spec` (rewritten in place) |
| `research/` | `research.md` — collected external ground truth that design references | `$seal-spec` (rewritten in place; optional) |
| `design/` | `CONVENTIONS.md` (project toolchain) + `INDEX.md` (manifest) + `DNN.md` (one per Decision) | `$seal-spec` (rewritten in place) |
| `plan/` | `STATUS.md` (manifest: `Next phase` counter + `⬜` lines) + `phase-NN.md` (one per **pending** phase) | `$seal-spec` (appends); the build loop deletes completed phases |
| `loops/` | the generated build-loop prompts + the `run` wrapper | a prompt-generator workflow |
| `issues/` | `<slug>.md` — one freeform file per unrelated finding noticed while doing other work | **any session, any time** (the one gate-exempt path); deleted when resolved |
| `README.md` | the ikispec-pointer stub: names the project and declares it abides by ikispec | `$seal-spec` |

The loop prompts and the `run` wrapper are **not** spec artifacts — they are
generated from the finished spec by a generator workflow and describe whichever
loop topology is installed. This skill owns the spec shapes; the generator owns
the loop shapes.

### `project/issues/` — the durable findings tray

`issues/` is the sink for the "finding to report, never a license to edit"
rule: a durable, non-contractual record of anything noticed while doing
*other* work — a defect in the code, a stale or wrong spec statement, a gap —
that the current session or loop turn has no authority or scope to fix.

- **Writable by any session, any time — the one gate-exempt path.** Unlike the
  rest of `project/**`, creating `project/issues/<slug>.md` requires no
  operator-invoked move: an interactive session, an authoring move, and an
  unattended build/verify/audit turn may all file one the moment they notice
  something. The exemption covers *filing only* — it is never a license to fix
  the finding, widen the current work, or write any other `project/` artifact.
- **Scope-bound like everything else.** File in the tree where you are
  working: a subproject finding goes in that subproject's `project/issues/`, a
  suite-level finding in the root umbrella's. A confined loop that notices a
  cross-tree problem files locally, naming the tree the fix belongs to; triage
  moves it, the loop never does.
- **One file, freeform prose.** The floor is: what was observed, where, and
  why it is out of scope for the work at hand. Optional filename prefixes
  (`bug-…`, `request-…`) may classify; there are no status fields, no minted
  ids, no index, and nothing downstream consumes the folder mechanically. Its
  reader is `$open-spec`, which surfaces unresolved issues when a session
  opens.
- **Resolution is deletion.** When an authoring move absorbs an issue (its fix
  enters design and the plan) or judges it stale, that move deletes the file —
  the plan's philosophy: done is deleted, history is git's.

## Authority boundaries

The spec is split across authorities that **never restate each other** — each
fact lives in exactly one place:

- **Product owns *why/promise*** — outcomes only; no mechanism, formats, exit
  codes, or test assertions.
- **Research owns *external evidence*** — non-contractual facts design cites.
- **Design owns *shape + its checkable proof*** — seams, interfaces, types, the
  error surface of every seam, the test strategy, and the minted `R-XXXX-XXXX`
  requirement-id denominator.
- **Plan owns *construction order*** — a work queue of dependency-ordered
  **pending** phases only; completed phases are deleted. Construction history
  lives in git, never in the spec.

Where product and design could overlap (behavior), product states the
*promise*; design states the *exact, checkable proof of that promise*. This
boundary is load-bearing — it is what keeps the three from overlapping.

## The umbrella project and suite contracts

The repo root's `project/` is the **umbrella project**: a spec whose "codebase"
is the suite's **shared contracts**, and which builds no code of its own. Every
rule in this skill applies to it unchanged; only what follows is particular to
it.

- **A contract Decision** states one suite-wide convention (an env contract, a
  protocol, an install layout, a tool-shape rule). Its Verification ids are
  minted normally, but each carries a **proof-location marker** telling the
  coverage check where the tagged test lives:
  - `[proof: <tree>]` — one named tree (e.g. `appkit`, `opsctl`) carries the
    tagged test; the behavior is proven once, in the implementation that owns
    it.
  - `[proof: per-service]` — every service that adopts the contract must carry
    its own tagged test for the id.
  - A contract with no testable behavior of its own (pure layout/prose
    contracts) says so explicitly and carries no ids, exactly like a
    structural Decision.
- **Umbrella coverage** follows the markers: an id marked `[proof: <tree>]`
  must appear as a tag in that tree's test files; an id marked
  `[proof: per-service]` is checked per adopting service (see adoption below),
  not against the umbrella itself. The umbrella never uses the tree-local
  coverage grep, since it governs no code.
- **Cite, never restate.** A subproject spec uses a suite contract **by
  value**: it cites the umbrella Decision by path, written in the exact form
  **`root project/design/DNN.md`** — never the bare path, which names a *local*
  Decision in every subproject and is ambiguous by construction. It owns none
  of the contract and never restates its content normatively. A stale local
  restatement is drift by construction; the citation is the whole mechanism,
  and the fixed `root` prefix makes every contract citation findable with one
  grep (`grep -rn 'root project/design/'`).
- **Citation of an id is adoption.** When a service design cites a
  `[proof: per-service]` contract id, the ordinary coverage invariant applies
  to it: the id must be realized by a tagged test in that service's tree or
  assigned to a pending phase. Contracts proven in one named tree are cited by
  Decision path only, never by id — an id in a design demands a local test.
- **Conformance is the default; deviation is a visible Decision.** A
  subproject conforms to every applicable suite contract unless it has an
  identified, project-specific need. Deviating requires a local Decision that
  names the umbrella Decision/id it departs from and the reason; silence means
  conformance, and authoring moves should push toward it.

## Hard invariants (no writer relaxes these)

- **Spec-first, direction-gated.** The spec is the only path by which the
  codebase changes: behavior is never patched directly into application code —
  it enters design as a Decision and the plan as a phase, and the build loop
  implements it. The gate swings both ways: `project/**` itself is written only
  inside an operator-invoked move (`$seal-spec` for authoring; the build loop's
  completion mutations). In any other session `project/` is read-only
  reference. Noticing the spec is stale, wrong, or incomplete is a finding to
  report, never a license to edit it — record it durably as
  `project/issues/<slug>.md` (the one gate-exempt path; see the findings tray
  above) so it survives the session — and discussion is not direction:
  however settled a conversation feels, reaching a conclusion is not an
  instruction to act on it. Say what should change and wait for the operator
  to invoke the move.
- **Sealed governs writes, not questions.** The direction gate restricts
  *writing* `project/`; it never makes the spec's contents settled. Inside an
  authoring move every existing Decision, Rejected entry, and invariant is a
  proposal under review: input to the discussion, evidence of what was chosen
  and why, never a constraint that closes a question. Do not cite an existing
  Decision as reason a path is unavailable. Re-test it on its merits and say
  so if it still wins. The implementing posture, where the spec is fixed input
  and a flaw is a finding to report, applies only outside an authoring move.
- **Scope boundary.** A `project/` governs **only its own codebase** — the tree
  it sits at the root of, never a sibling service, the repo root, or shared
  tooling. No Decision may name a seam/file outside that tree; no phase may
  build/edit/test outside it. Cross-module work is a signal the work is
  misfiled, not a license to cross. One carve-out: **citing** a suite contract
  from the umbrella project (by Decision path, or by id when adopting a
  per-service contract) is not a scope violation — building, editing, or
  testing outside the tree still is.
- **Authoring write boundary.** Spec authoring is a **docs-only mode**. During
  an `$open-spec` session's discussion, `$grill-me`, `$seal-spec`, and
  loop-prompt generator workflows, the only permitted writes are the
  `project/` artifacts named in
  the workspace table above, by their listed writers — plus
  `project/issues/<slug>.md` files, which any session may create (the
  gate-exempt findings tray). Authors may describe
  future implementation paths such as `cmd/`, `internal/`, `go.mod`, `Makefile`,
  tests, `bin/`, or generated source, but **must not create, edit, format,
  scaffold, test, or commit them**. If an authoring run is about to touch an
  implementation/build file, it must stop before writing and report the boundary
  violation. If it already wrote such a file in the same run, it must revert
  only its own out-of-bound writes, report them, and continue authoring only
  after the checkout is back inside the boundary. Implementation files are
  produced only by an explicit build loop after the operator runs it; they are
  never produced by spec authoring. While in spec-authoring mode, do not present
  direct implementation as an available next-step choice.
- **Real minted ids.** Every design Verification item carries an `R-XXXX-XXXX`
  id minted with `idgen -n <count> -p R` — **never hand-written, never
  invented, never renumbered.** Fresh id per newly added behavior; delete an id
  (and its test) when its behavior goes. One id, one behavior, used in exactly
  one place.
- **Product, research, and design are the single CURRENT statement.** They are
  rewritten in place to match the goal as it stands now: a changed detail is
  *replaced*, not stacked beside its old self; a dropped one is *removed*. None
  of the three may contain a fact that is no longer true — no history, no
  "previously", no superseded paragraphs. Construction history lives only in
  git, never in the spec.
- **The plan is a work QUEUE, never a history.** It holds only pending work:
  one `phase-NN.md` + one `STATUS.md` line per phase not yet built. When a
  phase completes, the build loop **deletes** its `STATUS.md` line and its
  `phase-NN.md` in the completion commit — finished phases never linger to
  contradict a design that has since moved on. There is no `✅` state on disk;
  done is deleted. The record of what was built is git's (the completion
  commits, and the deleted files recoverable there). Phase numbers are
  **never reused**: `STATUS.md` carries a `Next phase: NN` counter line; new
  phases take their numbers from it and bump it, so a number names one phase
  forever even after its files are gone.
- **Deterministic exit conditions.** Every phase carries a
  mechanically-checkable, reproducible, *reachable* "Done when" (green suite,
  exit code, exact match count) — never a prose judgment, never a
  self-referential/unsatisfiable check (classically a `grep` for a phrase the
  phase's own `project/` docs also contain, so it can never return empty).
  Structural/docs-only phases too: a green build plus a `project/`-excluded
  grep or a named smoke, never a prose claim.
- **Total coverage of the denominator.** Every *current* design Verification
  id is either already **realized** — its id appearing verbatim as a tag in a
  test file that runs under the suite — or assigned to **exactly one** pending
  phase: no current id unassigned, none split, none duplicated across pending
  phases. Design (rewritten in place, the current statement) is the
  denominator; realized-ness is read from the **code itself** (the tagged
  tests), never from a ledger or a history. Verify mechanically that no
  current design id is missing — the design-only difference must be empty:

  ```
  comm -23 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/design/D*.md | grep -v 'R-XXXX-XXXX' | sort -u) \
           <(cat <(grep -rhoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' --include='*_test.go' --exclude-dir=project .) \
                 <(grep -hoE 'R-[A-Z0-9]{4}-[A-Z0-9]{4}' project/plan/phase-*.md 2>/dev/null) | sort -u)
  ```

  (substitute the project's real test-file glob from design's CONVENTIONS.md;
  the `--exclude-dir=project` matters — an id quoted in the spec is not a
  test). **Empty output is the pass condition.** No reverse bookkeeping is
  needed: when a behavior leaves design its id and tagged test are deleted
  with it (the minted-ids rule), and a *pending* phase carrying an id design
  no longer mints is stale and must be fixed at authoring time. Ids cited from
  the umbrella's per-service contracts enter the denominator like local ids;
  the umbrella project itself replaces this tree-local check with the
  proof-location markers described above.
- **Every promise is proven.** Each product success criterion maps in design's
  `INDEX.md` criteria trace to at least one minted id whose test exercises the
  assembled artifact end-to-end. A criterion with no mapped id is an unproven
  promise and blocks sealing; a mapped id that design no longer mints is stale
  and fixed at authoring time. The trace is mechanically checkable: every
  criterion line carries ≥1 id, and each such id appears in the design id set.

## `project/product/README.md` — the product shape

Owns **intent** — *why* this exists, *for whom*, what is in and out of scope,
and what we **promise** the user — stated once, in **outcome terms**. It must
NOT state mechanism, exact formats, exit codes, or test assertions; those
belong to design.

Sections, in order:

- **Title** — `# <name> — Product`.
- **Authority header** — a short paragraph beginning `**Authority: intent.**`
  stating what this doc owns and, explicitly, what it does not (mechanism,
  formats, exit codes, test assertions → design), plus the promise-vs-proof
  boundary.
- **## Problem** — the pain in the user's world; no solution yet.
- **## Purpose** — one paragraph: what the thing *is* and the single job it
  does.
- **## Users** — who runs it and what they are trying to get done.
- **## Scope** — what it does and, by exclusion, what it deliberately does not.
  Fold non-goals in as bounded "nothing else" statements; only break out a
  separate `## Non-goals` section when the exclusions genuinely need their own
  emphasis.
- **## Contractual constants** *(only if any exist)* — fixed, promised values
  the design must use verbatim and never re-declare (a baseline constant, a
  starting version, a protocol value). Promises, not implementation detail.
  Omit the section when there are none.
- **## What we promise (user-facing behavior)** — the observable behavior in
  outcome terms: what the user does and what they get back. Short example
  invocations/output where they sharpen the promise. No mechanism, no exit
  codes, no internal formats.
- **## Success criteria (outcomes)** — a bullet list of user-observable
  outcomes, each phrased as a *result* the user could confirm, never as a test
  assertion or mechanism. Every item must be outcome-shaped and checkable
  end-to-end against the real thing: design proves each criterion via the
  criteria trace in its `INDEX.md` (below), so a criterion no test could
  confirm against the assembled artifact is malformed at authoring time.

## `project/research/research.md` — the evidence base

**Optional and non-contractual**: the build loop never reads it, and it feeds
no downstream doc mechanically. It exists for one exact thing: **collected
external ground truth, gathered so design never has to do web research.**
Typical contents: the exact API footprint of a REST service (just the parts the
design will use, documented precisely), a library's actual behavior, a
protocol's real constraints — and sometimes options deliberately evaluated and
*not* chosen, so better/worse can be judged side by side. Design decisions
reference these facts instead of re-deriving them.

Write it only when design depends on external facts not already in hand; skip
it entirely otherwise. It is rewritten in place — a single, coherent statement
of the current research, never a running log.

## `project/design/` — the design shape

Owns **shape and its proof**: *how* the thing is built and *how each behavior is
proven*. It is the single, current statement of the architecture, rewritten in
place; a changed Decision is replaced, a stale one removed, never stacked. It
*uses* product's contractual constants by value but does not own them, and never
re-declares the why.

**Design owns the public surface; the build loop owns everything below it.**
Design fixes what other modules and later phases compile against: the module
graph, exported symbols, signatures, types, and each operation's error contract,
plus the proof (the minted `R-XXXX-XXXX` ids and what each asserts). The loop
owns what no other module sees: function bodies, unexported types and helpers,
algorithms, file layout, and the tests that discharge design's ids until the
suite is green. Rule of thumb: if changing it changes what another module
compiles against, it is design's; otherwise it is the loop's. The ids are the
seam between them, so design states the claim and the loop writes the code and
test that make it true. Hence design carries interfaces, types, and illustrative
signatures, never full implementations, and it shapes the architecture for
testing: seams exist so every behavior can be verified, and design also names the
claims that **cannot** be verified in isolation (those hinging on a real external
contract) so the test plan exercises those for real.

A seam's **error surface is part of its exported shape.** A Decision that
declares a seam declares its failure contract: which failures each operation
returns as named errors the caller can distinguish; which conditions panic
(broken invariants and programming errors, never expected runtime failures); and
what handling each error observably means for the caller (retry, propagate,
surface, degrade), not the code that implements it. A seam declared without its
errors is as incomplete as one declared without its return types.

Split for **addressability**, a build phase reads only the one Decision it
realizes, never the whole architecture:

### `project/design/CONVENTIONS.md` — the project toolchain

The one in-tree design file that is project-specific rather than ikispec
boilerplate. It is a title line (`# <name> — Design Conventions`) plus a single
section of shared facts every Decision leans on:

- **Required** (downstream phases would otherwise have to guess these): the exact
  build/typecheck command, the exact test command, what "the suite is green"
  concretely means, and the test-file glob (e.g. `*_test.go`) where
  requirement-id tags live.
- **Cross-cutting facts, as applicable** (an open-ended list): language/version,
  module path, exit-code taxonomy, formatting rules, a shared time/IO source, and
  anything else every Decision leans on. State the commands, not the coverage
  rule.

Design carries no authority header, no requirement-id explainer, and no layout
section. Those are identical across every project, so ikispec owns them (the
authority boundary above, the requirement-id rules below) and the tree does not
restate them. The `INDEX.md` / `DNN.md` split is described below; `idgen` and the
minting rules are generic and live in ikispec, never in a project's Conventions.

### `project/design/DNN.md` — one self-contained file per Decision

- A header `# Decision N — <title>`.
- **Decision.** — the seams/interfaces/types/naming and each operation's error
  surface (returned errors, panic conditions, and what handling each error
  observably means for the caller), with illustrative signatures and
  struct/interface declarations (never full implementations).
- **Rejected.** — the alternatives considered and why each lost.
- **Verification.** — a bullet list, each line
  `R-XXXX-XXXX — <the behavior a test must assert>`. State each behavior so it
  is *falsifiable*: a wrong implementation must fail it. Pin the discriminating
  property, not a weaker one a degenerate implementation also satisfies — when
  the Decision moves off a specific bad value or state, name the value or
  threshold the behavior excludes (e.g. "≥ 16384", not "non-zero"). Failure
  behaviors are behaviors: an error contract's discriminating cases (this
  condition → this named error; this violated invariant → panic) carry ids and
  falsifiable statements like any other behavior. A pure
  seam/structure decision with no behavior of its own says so explicitly and
  carries no ids (its proof is the behavioral ids of the decisions it enables).
  - **Express the proof as a bounded, writable test — never as a universal or
    negative.** A claim phrased "never / no path / all / any / cannot" cannot be
    tested: a test samples, it can't visit the whole space. Worse, it traps the
    build loop, which grinds forever against a gate that was impossible from the
    start and which it has no authority to rewrite. Reshape it into something a
    real test discharges: a positive assertion at an architectural chokepoint
    (design creates the chokepoint so the negative collapses to one check); a
    bounded enumeration (these N inputs each yield this named error); or a
    mechanism check ("the query is parameterized", not "no injection is
    possible"). A behavior that cannot be reshaped into a bounded test is a
    **design defect to fix now**, never an id handed to the loop.
  - **Verify the claim against a substrate that can falsify it — not a proxy a
    stub also passes.** Ask *what would have to be true for this test to fail,
    and can the chosen substrate make it fail?* A claim whose correctness
    depends on a real external contract is **not** verified by an assertion run
    against a mock or fake — the mock accepts whatever it's handed; such a test
    confirms a field was set, never that the system runs. For every such claim,
    mint a **distinct id whose test exercises the real dependency** — a
    live/integration/smoke check — name that substrate on the id, and name the
    observable outcome that proves it actually ran (a completed call, a
    returned result), not merely that a value was configured.
  - **Wiring is proven live: a module counts only when the assembled program
    runs it.** A package green in its own unit tests but never reached by the
    running program is dead code a green suite hides. So every capability the
    assembled program is meant to run, an external surface (HTTP route, MCP
    endpoint, CLI verb, event subscription) and the internal modules behind it,
    has at least one id whose test drives it **through the composition root**,
    the real assembly production uses against the real dependencies, never a
    component the test constructs directly; proven only in isolation, it is
    **unwired by default**. This id's job is narrow: prove the seam is connected
    and the real substrate works, not re-test behavior the fake-adapter unit
    tests already cover. So one path per module through the real thing (an
    actual SQLite write and read-back, say) is typically all it takes. The id
    names the observable outcome proving the assembled artifact ran it.

### `project/design/INDEX.md` — the manifest

- **Title** — `# <name> — Design Index`.
- A short contract paragraph: each Decision maps to its `DNN.md`; every id maps
  to its Decision/file; id lookup is a grep against this index. Regenerate it
  whenever a Decision is added or its Verification ids change.
- **## Decisions** — one line per Decision in number order: `D<N>` → its file,
  its title, and the ids it owns (or "none — structural").
- **## Verification ids → Decision** — every minted id, **sorted**, each mapped
  to its Decision and file.
- **## Success criteria → ids** — the criteria trace: one line per product
  success criterion (quoted or tightly paraphrased, in product order), each
  mapped to the id(s) whose tests prove it end-to-end against the assembled
  artifact. Every criterion maps to at least one minted id; every mapped id
  exists in the id sections above. Regenerate alongside the rest of the index.

(The construction order that realizes the design lives in the plan — design
carries no `## Status` section.)

### Minting the Verification ids

The `R-XXXX-XXXX` ids are **real, minted ids — never hand-written or made up.**
Mint them with the `idgen` tool (`R` = requirement prefix):

```
idgen -n <count> -p R
```

Mint as many as a Decision's Verification list needs, assign one id per
behavior, and paste each inline. Ids are **stable handles**: when editing the
design in place, do **not** renumber or regenerate existing ids — mint a fresh
id for each newly added behavior; when a behavior is removed, delete its id
with it (its test goes too).

The ids live inline in these Verification lists and nowhere else: there is **no
separate requirements document**. Design's responsibility for ids ends at
minting them — how coverage is measured and when work is "done" are downstream's
concern, owned by the plan and the build loop, not stated here.

## `project/plan/` — the plan shape

Owns **construction order** — a work queue of **pending** phases only. Phases
are appended at the end, numbered from the `STATUS.md` counter, and **deleted
on completion** by the build loop; the plan never holds finished work, so it
can never contradict a design that has moved on. Construction history lives in
git, not here. To extend the project: update product and design in place
first, then **append** a new phase.

**One phase = one package = one build-turn context.** Each phase is a single
coherent unit of work — almost always one package — scoped to that unit's
design Decisions and the *interfaces* (not internals) of the packages it
depends on, and **sized so the build loop can carry it in one fresh build-turn
context** and ideally finish it in a turn or two. The loop does *not* build a
phase in one long accumulating context — size to a single build turn, not an
imagined single sitting. Sizing a phase as large as cleanly fits one turn is
good: fewer cycles, less context churn. If a single Decision is too large for
one context it is split across phases, and each affected phase names the
**slice** of that Decision's Verification ids it carries.

Split for addressability — the loop greps a manifest for the next unit of work
and reads exactly one phase file, never the whole queue:

### `project/plan/STATUS.md` — the manifest (the only home of status markers)

- **Title** — `# <name> — Plan Status`.
- **Contract paragraph** — one line per **pending** phase in build order, the
  only place a phase's marker lives; each phase line is a Markdown bullet
  beginning with `- Phase` carrying `⬜` (pending); the build loop finds its
  next work with `grep -nE '^- Phase .* ⬜' project/plan/STATUS.md | head -1`
  and reads only that phase's body file. On completion the build loop deletes
  the phase's line and body file — there is no done marker; done is gone.
  Note it deliberately carries **no bare status glyph** outside phase lines,
  so the anchored grep matches only phase lines.
- **The counter line** — `Next phase: NN`, a non-bullet line between the
  contract paragraph and the phase lines: the number the next appended phase
  takes. `$seal-spec` bumps it on every append; it is never decremented and a
  number is never reused, so a phase number names one phase forever even
  after its files are deleted.
- **The phase lines** — `- Phase NN ⬜ realizes <Decision ids>` (or
  `realizes —` for a pure structural phase) `— <one cohesive objective>`.
  Aligned and grep-able. A phase body file carries **no** marker of its own.

### `project/plan/phase-NN.md` — one body file per phase

One body file per pending phase, zero-padded; a split sub-phase keeps a letter
suffix (e.g. `phase-07a.md`).

- A header `# Phase N — <one cohesive objective>` — **no status token**.
- A `*Realizes design Decision <n> (<short label>)[ and <m> (...)][. Depends
  on Phase <k>].*` line — exactly which Decisions this phase builds, and which
  earlier **pending** phase(s) it needs, if any (a completed dependency is
  simply the existing codebase and is not named).
- A short body: what gets built (the package/seam and its paths), stated as the
  observable end state, not an implementation recipe.
- **Done when:** the acceptance bar as deterministic exit conditions — its
  Verification ids covered by genuine tests (each id listed with its behavior)
  and the suite green; a structural phase gets a deterministic check instead (a
  clean build, exact named files/targets, a `project/`-excluded grep).

## `project/README.md` — the ikispec-pointer stub

The top-level README exists for one reason: so a reader who lands in the tree
cannot miss that it is governed by ikispec. It is a stub, not a manual, roughly:

> `# <name> — Project`
>
> This tree abides by the **ikispec** skill: ikispec defines its structure,
> document shapes, and authoring rules. Read ikispec before reading or writing
> anything here.

It carries no folder table, no writer list, no loop mechanics, and no
restatement of the shapes above. All of that is ikispec's, one hop away.
`$seal-spec` writes the stub and keeps the project name current.
