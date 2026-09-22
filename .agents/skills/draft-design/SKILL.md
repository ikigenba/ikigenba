---
name: draft-design
description: Turn user stories into design and project ground using fanout, with verified story coverage and contract consistency. Produces design, not implementation.
---

# Stories into design

Invoke this skill directly. Load [fanout](../fanout/SKILL.md) for execution
and verification, and [spec](../spec/SKILL.md) with its
[design format](../spec/references/design-format.md) for authoring rules,
permanent ids, and consumer-usage review. The root is a fanout coordinator.
This skill supplies the goal below; fanout owns the agent roles,
decomposition, ownership, verification, capacity handling, and repairs.

## Goal and authority

Produce a coherent, testable public contract and concrete project ground for
the selected story groups in one sub-project. Finish all work possible from
available inputs while carrying unsettled user decisions to the root.

Resolve the selected sub-project to an absolute directory; ask if context
does not identify it. Read applicable ancestor guidance and its own
`AGENTS.md`; the repository root's guidance is not project ground. Supply
that directory, selected story sources, this skill, and the completion
criteria to the fanout assignments. Missing design and ground may be created.

Stories in `specs/stories/` supply the current intent and may be reconsidered
with the user. Report proposed intent changes for authoring through
`draft-stories`; this operation's output is design and ground. User-supplied
external story sources may be read without broadening output authority. Existing design and
relevant code establish the current target, not a compatibility obligation.
Respect spec's project boundary; do not inspect sibling internals.

Repository writes are limited to `specs/design/` and the project ground
needed to build it. No source, test, story, issue, or review-file changes;
no commits. Do not start `check-spec` or `build-spec`. Evidence, proposals,
consumer examples, and coverage records use ephemeral scratch files outside
the repository under the [handoff convention](../handoff/SKILL.md#scratch-file-convention).
Report blockers and unresolved decisions to the user, not `specs/issues/`.

## Coverage and design work

Delegate an inventory of input stories and acceptance criteria, using stable
source locators rather than minted ids. For stories without explicit criteria,
identify their stated outcomes and distinguish them from open questions.
Maintain a scratch coverage ledger mapping those outcomes to owning scopes,
requirements, verified results, exclusions, and pending decisions. Partition
the inventory and ledger when needed; coordinators retain bounded summaries.
Record input artifact states so later changes invalidate affected evidence.

Partition by public contract or design seam; several stories may share one.
Give ground authoring its own assignment. Assign ownership of design numbering
and folder-wide padding changes. Shared vocabulary, ownership, dependency
direction, and interfaces must be settled and verified before dependent
authors draft against them. Changes return to the owning scope and trigger
review of affected consumers.

Authors establish relevant existing behavior and reconsider whether the
package layout still fits, including the package owning every exported name.
Capture needed layout changes as structural requirements. Author structural
and behavioral requirements under the canonical rules, minting ids with
`idgen`. Preserve unchanged requirement text byte-for-byte; changed text gets
a new id. Every requirement must trace to a story outcome or a necessary
public declaration, invariant, or project constraint supporting one.

Supply complete consumer tasks using exactly the proposed contract in scratch
material. For revisions, show current and proposed usage. Usage review must
expose missing declarations and incoherent interactions, not merely show that
each declaration can be mentioned in isolation.

Gather dependency evidence within the user's authorized scope according to
the design format's proof rules. Record provenance outside the repository.
Use installed public interfaces and published documentation for sibling tools,
never their source or design. Explicitly identify observations still needed
before checking; do not invent evidence or claim drafting performed a check.

## User decisions

Children report unanswered questions and contradictions with source criteria,
evidence, affected decisions, completed work, and what would resolve them.
The root collects and deduplicates these in scratch material. Correct defects
that available evidence resolves; do not send technical choices to the user
merely because they require work.

Pending user decisions pause only dependent work, following fanout. Finish
and verify independent portions, including those in a partly settled document.
Never mint an arbitrary answer into the contract. A genuine blocker uses
fanout's stop-and-report behavior through the user channel above.

Use [grill-me](../grill-me/SKILL.md) at the root to settle remaining intent,
one question at a time with a recommendation, once available work is exhausted
or when the user requests that interaction. Apply answers as they arrive and
resume affected work. Do not repeatedly redelegate a question without new
evidence or treat an assumption as user-approved.

## Completion criteria

Verification must establish:

- Each assigned outcome is covered by testable normative requirements,
  including relevant failures; id permanence and canonical format hold.
- Public names, shapes, ownership, and behavior are explicit, without hidden
  build-time design decisions, unsupported product behavior, or private
  implementation prescriptions.
- Consumer tasks use the declared surface and accomplish their story outcomes.
- Project ground declares concrete toolchain, test files, exact ordered gates,
  and commit conventions consistent with the design.
- Contracts agree across documents. Assign each shared boundary and story
  spanning documents as bounded integration work; local coverage alone is
  insufficient.

Every input criterion needs a verified result, explicit user exclusion, or
specific unresolved decision; the last means the design is incomplete.
Verification covers completed portions even when other decisions remain open.
If existing design already satisfies the inputs, verify it without rewriting.

Delegate final coverage reconciliation and, where tests and ground exist,
canonical gap measurement, including adds and removals from revised ids.
These are bounded report-producing assignments, not a root review of the
whole design. For a new project report absent implementation; do not invent
test results or run build gates against unwritten code.

Report consumer usage first, then design paths, scratch ledger/evidence paths,
coverage counts, the implementation gap, and consolidated unresolved work.
Distinguish a partial draft from a complete design. Completion requires
verified coverage, consistent contracts, concrete ground, and no unresolved
design decisions. Identify any pending external observations needed before
`check-spec`; never claim that the design has been checked or built.
