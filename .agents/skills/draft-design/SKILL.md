---
name: draft-design
description: Turn user stories into design using fanout, with verified story coverage and contract consistency. Produces design, not implementation.
---

# Stories into design

Invoke this skill directly. Load [fanout](../fanout/SKILL.md) for execution
and verification, and [spec](../spec/SKILL.md) with its
[design format](../spec/references/design-format.md) for authoring rules,
permanent ids, and consumer-usage review. The root is a fanout coordinator.
This skill supplies the goal below; fanout owns the agent roles,
decomposition, ownership, verification, capacity handling, and repairs.

## Goal and authority

Produce a coherent, testable public contract for the selected story groups
in one sub-project. For a library, the input is instead the public interface
agreed with the user in this conversation: present it at a high level and
get agreement before authoring; this operation locks it in. Finish all work
possible from available inputs while carrying unsettled user decisions to
the root.

Resolve the selected sub-project to an absolute directory; ask if context does
not identify it. Read applicable ancestor guidance and its own `AGENTS.md`;
never use the repository root's `AGENTS.md` in its place. Supply that
directory, selected story sources, this skill, and the completion criteria to
the fanout assignments. Missing design may be created.

Stories in `specs/stories/` supply the current intent and may be reconsidered
with the user. Report proposed intent changes for authoring through
`draft-stories`; this operation's output is design. User-supplied external
story sources may be read without broadening output authority. Existing design and relevant code establish the
current target, not a compatibility obligation. Respect spec's sub-project
boundary; do not inspect sibling internals.

Repository writes are limited to `specs/design/`. No source, test, story,
issue, or review-file changes. Do not start `build-spec`.
The root commits the design once verification passes (see "Committing").
Report blockers and unresolved decisions to the user, not `specs/issues/`.

## Coverage and design work

Delegate an inventory of input stories and acceptance criteria (for a
library, the agreed interface and its consumer tasks), using stable
source locators rather than minted ids. For stories without explicit criteria,
identify their stated outcomes and distinguish them from open questions.
Track, for each of those outcomes, its owning scope, requirements, verified
result, exclusion, or pending decision. Partition the inventory when needed;
coordinators retain bounded summaries.

Partition by public contract or design seam; several stories may share one.
Assign ownership of design numbering and folder-wide padding changes. Shared
vocabulary, ownership, dependency direction, and interfaces must be settled
and verified before dependent authors draft against them. Changes return to
the owning scope and trigger review of affected consumers.

Authors establish relevant existing behavior and reconsider whether the
package owning each exported name still fits. Capture a changed owner as a
structural requirement, proved by importing the name from its new package.
Author structural and behavioral requirements under the canonical rules,
minting ids with `idgen`. Preserve unchanged requirement text byte-for-byte;
changed text gets a new id, and so does every requirement citing a re-minted
id. Tests are not a reason to re-mint: the build rewrites tests the revision
makes stale. Every requirement must trace to a story outcome
(for a library, an agreed consumer task) or a public declaration supporting
one, and must be provable by use.

Supply complete consumer tasks using exactly the proposed contract. For
revisions, show current and proposed usage. Usage review must
expose missing declarations and incoherent interactions, not merely show that
each declaration can be mentioned in isolation.

Prove each external fact a new or changed requirement states, within the
user's authorized scope and according to the design format's proof rules,
before writing that requirement. A requirement already in the design is not
proven again.
Use installed public interfaces and published documentation for sibling tools,
never their source or design. Do not invent evidence or claim drafting
performed a check.

## User decisions

Children report unanswered questions and contradictions with source criteria,
evidence, affected decisions, completed work, and what would resolve them.
The root collects and deduplicates these. Correct defects
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
- Each new or changed requirement is provable. A verifier sketches a test the
  sub-project's `AGENTS.md` permits, then tries to construct a plausible
  defect that passes it: a wrong value, a missed case, an overflow, never an
  implementation that special-cases the test's inputs. If one exists, the
  requirement is reworded,
  or the test rule it needs goes to the user as a decision.
- Behavioral requirements state observable outcomes at public seams, never how
  the code produces them.
- The contract passes the declared gates. In a throwaway worktree, stub the
  declared public surface, write the consumer tasks as code against it, and
  run the sub-project's gates, then remove the worktree. A shape the gates
  reject is redesigned; it is never left for the build.
- Contracts agree across documents. Assign each shared boundary and story
  spanning documents as bounded integration work; local coverage alone is
  insufficient.

Every input criterion needs a verified result, explicit user exclusion, or
specific unresolved decision; the last means the design is incomplete.
Verification covers completed portions even when other decisions remain open.
If existing design already satisfies the inputs, verify it without rewriting.

Delegate final coverage reconciliation and, where tests and the
sub-project's `AGENTS.md` exist, canonical gap measurement, including adds and
removals from revised ids. These are bounded report-producing assignments, not
a root review of the whole design. For a new sub-project report absent
implementation; do not invent test results or run build gates against
unwritten code.

## Committing

The operation is not complete until its design changes are committed. Once
the completion criteria hold, the root commits exactly the `specs/design/`
files this operation wrote, and nothing else, on the current branch. The
message follows the sub-project's `AGENTS.md` commit conventions. Its
`Requirements:` trailer lists the ids this operation added, and the message
ends with the repository's attribution trailer. A partial draft is not
committed: report it as partial, with its files left uncommitted.

Report consumer usage first, then design paths, the commit,
coverage counts, the implementation gap, and consolidated unresolved work.
Distinguish a partial draft from a complete design. Completion requires
verified coverage, consistent contracts, and no unresolved design decisions. Never claim that the design has been
built.
