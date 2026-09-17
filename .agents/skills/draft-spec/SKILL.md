---
name: draft-spec
description: Turn user stories into design and project ground for one sub-project by recursive delegation, with independent verification of story coverage and contract consistency. Produces design, not implementation.
---

# stories into design

The user's stories are the input; a coherent, testable public contract for
one sub-project is the output. Use the same bounded agent tree as
`build-spec`, with authors in place of implementers. This is the
`draft-spec` authoring operation: load the sibling `spec` skill and its
`references/design-format.md` for the design format, permanent requirement ids,
and consumer-usage review. Those rules remain
canonical; this skill supplies delegation and completion criteria.

The goal is to finish all work that can be completed from the available
inputs. A contradiction or unanswered question blocks only the decisions
that depend on it, never the whole run. Carry unresolved issues through
the tree and report them together at the end. Do not inherit build-spec's
stop-on-issue behavior.

## One project, one assignment

Resolve the selected sub-project to an absolute path before delegating.
If the user has not selected one and the context does not identify it,
ask. Read applicable ancestor guidance and the project's own `AGENTS.md`.
Run from the selected directory; every child uses that same directory.
Unlike a build run, authoring may create missing `specs/design/` and
project ground. Never substitute the repository root's `AGENTS.md` for
the sub-project's ground.

The user stories are the sub-project's `specs/stories/` (see the `spec` skill's
`references/story-format.md`); the request names which groups to design, or
all of them, and may reference further artifacts. Read them without
modifying them. A story that needs changing is a `draft-stories` matter,
reported as an unresolved decision, never edited here. Establish the current target from
existing design and relevant code; existing implementation is evidence,
not an obligation to preserve its shape. Do not read sibling internals.
User-provided story sources outside the project may be read as inputs;
they do not broaden the output boundary.

Writes are limited to the selected project's design, the project ground
needed to build it, and supporting review/evidence or issue files under
its `specs/`. No source or test changes. Do not run `build-spec`, invoke
the `check-spec` skill, or commit merely because
drafting finished. These are separate, human-gated skills.

## The coverage ledger

The root keeps `specs/draft-review.md` as a compact, non-normative review
artifact. Record the input sources and versions when available, and give
each story and acceptance criterion a stable source locator. Use existing
story ids or local review labels; these are not requirement ids and are
never minted with `idgen`. For stories without explicit acceptance
criteria, extract their stated outcomes and distinguish those from open
questions. Never silently invent user intent.

Map each criterion to its owning design scope and, when authored, the
requirements that satisfy it. Record unresolved decisions, dependency
evidence, and canonical consumer usage here or in linked review files.
Keep review material outside `specs/design/` so it cannot contaminate the
canonical design-id grep.

Coverage is semantic: a requirement id beside a story does not establish
that its outcome is designed. A criterion is closed only after an
independent verifier confirms the contract satisfies it. Every criterion
must be covered, explicitly excluded by the user, or reported unresolved.
Every new requirement must trace to a story outcome or to a necessary
public declaration, invariant, or project constraint supporting one.

## Three roles

A **coordinator** owns coverage and delegates; it never authors design
requirements. The root is always a coordinator. It holds the story
inventory, scope ownership, shared boundary decisions, and compact child
reports. It may maintain the review ledger. It reads enough existing
design to partition responsibilities; detailed source inspection belongs
to the relevant author. Delegate ground authoring as a separate scope.

Partition by coherent design seam, not arbitrarily by story count: several
stories may use the same public contract. A coordinator holds about six
children; use sub-coordinators for larger assignments. Each file has one
author at a time. Allocate design numbers centrally using the canonical
filename rules, and serialize any folder-wide padding changes. For a
large existing document, delegate analysis of bounded clusters, then give
one author ownership of integrating the document.

An **author** owns one design document or one bounded cluster within it,
roughly a dozen requirements. It reads its assigned stories, applicable
guidance, the canonical authoring rules, its existing document, and the
relevant source/API surface. Targeted lookups resolve adjacent contracts;
it does not load the whole project. If the scope spans multiple seams,
it returns a proposed split or becomes a coordinator before drafting.

Before drafting, an author establishes what is already known from the
existing design and code, and asks, every time, whether the current
package layout still fits once this design is in it, and which package
each new exported name belongs to. The answer is stated as a structural
requirement, so a new or split package is an ordinary re-mint of the
layout requirement and travels through the gap like any other change.

Authors write structural and behavioral requirements, minting new ids
with `idgen` as part of this draft-spec operation. Preserve existing
requirement text byte-for-byte or replace its id according to the spec
rules. Unchanged requirements are not re-minted. Supply complete consumer
tasks using the proposed contract, outside the normative design.

A **verifier** owns one returned scope, reads its source stories, design,
consumer usage, relevant boundary contracts and evidence, and edits
nothing. It checks meaning as well as form. It checks the whole assigned
scope, not just until the first failure, and returns per-criterion
verdicts with the requirement/file and evidence for every finding.
Distinguish correctable drafting defects from unresolved input decisions;
verify completed portions even when other portions remain blocked.
Ground verification checks that toolchain,
test-file set, exact ordered gates, and commit convention are concrete
and consistent with the proposed project.

Delegate to fresh agents with no inherited conversation. Give each its
role, the absolute project and skill paths, assigned source locators,
file ownership, boundary decisions, and expected report. Each reads this
skill, the spec authoring rules, and applicable `AGENTS.md` instructions.
Keep reports compact: criteria covered, files and requirement ids changed,
shared interface changes, evidence locations, and unresolved decisions.

Every author result, including partial work, goes to a fresh verifier.
Correctable failures go to a fresh author with the evidence, then to
another verifier. Unresolved input decisions enter the issue ledger;
re-delegating the same question without new evidence is not progress.
Coordinators never accept authors' own coverage claims as verification.

If spawning has no capacity, wait for running children and retry. With
none running, return the unstarted scope to the parent so it can schedule
it serially. A parent receiving that report uses one child at a time.
At the root, if delegation is unavailable altogether, report that limit;
do not silently collapse authoring and verification into one role. A node
at its depth limit returns its proposed split for its parent to schedule.

## Shared boundaries and decisions

Settle shared vocabulary, ownership, dependency direction, and public
interfaces before dependent authors draft against them. Use a bounded
author/verifier scope for each shared contract, then give downstream
authors its exact references. A proposed interface change returns to its
owner; dependent scopes are revisited and reverified after it lands.

Resolve technical choices from stories, current contracts, and evidence.
Questions that change the user's intended behavior go to the root.
Children report questions instead of independently interrogating the
user. Collect unresolved questions for the final handoff and continue
independent work, including unaffected parts of the same document. If a
user answer arrives during the run, apply it and resume dependent work.
Use the `grill-me` procedure for resolving remaining decisions with the
user after the available work is exhausted, or when the user requests
that interaction — one question at a time, each with a recommendation.
If the design is already fully determined by its inputs, there is
nothing to ask. Never mark an assumption as user-approved.

Gather observations for external dependency claims within the user's
authorized scope and record their provenance. Documentation may support
a draft, but does not replace the observations the spec rules require
before checking. Missing access or unproven claims remain explicit in the
handoff. An external tool's contract — a sibling project's included — is
established through its installed public interface, never its source,
design files, or documentation alone.

## Carry issues to the end

Record each unresolved issue under `specs/issues/<slug>.md` per the `spec`
skill's "Filing an issue", and link it from the coverage ledger. Include
the source criteria, the contradiction
or missing fact with evidence, affected decisions and dependent scopes,
work already completed, and the question or observation needed to resume.
Keep alternatives and provisional proposals in review material; do not
mint an arbitrary resolution into the normative contract.

Authors finish the unaffected portion of their scope before returning
issues. Coordinators collect and deduplicate issues, continue scheduling
all unblocked work, and revisit dependencies when new results resolve an
issue. An issue reported by a child is not an instruction to stop siblings
or halt upward. Correct errors that available evidence resolves instead
of treating them as questions for the user.

Before ending, every input criterion must have a verified result, an
explicit user exclusion, or a specific unresolved issue. Finish all
feasible authoring, correction, and verification, including integration
checks on completed portions. Stop only when no remaining work can
advance without an identified answer, evidence, or unavailable capability.
Do not use issue reporting as a shortcut around difficult work, and do
not repeatedly retry an unchanged blocker.

## What done looks like

Each scope verifier confirms:

- Every assigned outcome and acceptance criterion is represented by
  testable normative requirements, including relevant failure behavior.
- Public names, shapes, ownership, and observable behavior are explicit;
  required decisions are not hidden in prose or left to the build agent.
- Requirements follow the canonical format and id permanence rules.
- Consumer examples use exactly the declared public surface and complete
  the assigned user tasks. Revisions show current and proposed usage.
- The design respects the project boundary and introduces no unsupported
  product behavior or private implementation prescription.

After local verification, delegate integration verification in bounded
scopes: each shared boundary and each story spanning documents must have
an owner. These verifiers check vocabulary, signatures, dependency
direction, and end-to-end story coverage across the relevant contracts.
A passing collection of documents with contradictory interfaces is not a
finished design. Reverify affected scopes after any correction.

The root reconciles the coverage ledger against the complete input
inventory and all verifier reports. With existing tests and declared
ground, compute and report the canonical implementation gap, including
adds and removals from revised requirements. That gap is a handoff to the
build workflow, not something this skill closes. For a new project,
report that implementation is absent; do not invent test results or run
build gates against code that has not been authored.

Finish with consumer usage first, then the design and review file paths,
coverage counts, and a consolidated list of unresolved issues, their
affected criteria, and what would unblock each. Distinguish completion of
all feasible work from completeness of the design. If all
stories are already adequately designed and independently verified,
report that result without rewriting requirements. Label incomplete work
as a partial draft. A complete draft has verified coverage, consistent
contracts, concrete ground, and no unresolved design decisions; any
pending external observation is explicitly identified so `check-spec`
can report it. Never claim the design has been checked or built.
