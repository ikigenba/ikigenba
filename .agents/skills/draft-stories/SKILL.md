---
name: draft-stories
description: Turn user intent into new or updated stories using fanout, asking the user one question at a time for unsettled behavior. Produces stories, not design.
---

# Intent into stories

Invoke this skill directly. Load [fanout](../fanout/SKILL.md) for execution
and verification, and [spec](../spec/SKILL.md) with its
[story format](../spec/references/story-format.md) for the artifacts.
The root is a fanout coordinator and the single conversational contact with
the user. This skill supplies the goal below; fanout owns the agent roles,
decomposition, ownership, verification, capacity handling, and repairs.

## Goal and authority

Make the selected project's `specs/stories/` express the user's current
intent, with every settled interaction in the canonical format and no
contradictory stories. Drafting includes updating existing stories.

Resolve the selected sub-project to an absolute directory holding `specs/`
and its own `AGENTS.md`; ask if the project is not identified by context.
Read applicable ancestor guidance and project ground. Supply that directory,
the user's request and settled decisions, this skill, and the completion
criteria to the fanout assignments.

Repository writes are limited to stories. No design, source, test, ground,
or issue-file changes; no requirement ids or commits. Do not start
`draft-design`. Working inventories, proposals, and evidence belong in
ephemeral scratch files outside the repository using the
[handoff scratch convention](../handoff/SKILL.md#scratch-file-convention).
Report blockers and unresolved decisions to the user, not `specs/issues/`.

## Domain work

Delegate discovery of the existing story groups and relevant design and code.
Account for every existing group, dividing discovery when needed. Establish
the current command grammar, names, output, and behavior through bounded
reports. Existing behavior is evidence, not an obligation to preserve it.

Partition authoring by coherent interaction or group. An existing group owns
an extension of its command or interaction; a new seam takes a new group.
Assign ownership of filename numbering and folder-wide padding changes under
the canonical format. Shared messages, names, and exit codes need consistency
work across every story that uses them.

List proposed story headings before drafting: each distinct interaction and
each distinct failure the actor can cause or meet. Surface missing cases such
as help, unknown options, and missing arguments as proposals; format needs
do not authorize inventing product behavior.

Authors write the settled portions and report decisions they cannot settle.
Do not silently delete existing stories. Rewrite a changed interaction to
express the new intent; if the interaction is gone, ask the user how to
retire it. Identify existing designs affected by story changes for the handoff.

## User decisions

The root uses [grill-me](../grill-me/SKILL.md) for unsettled intent: one
question at a time, with a recommendation and reasoning. Children return
questions with evidence and affected stories; they do not question the user
independently. Available inputs may already settle a point; report that basis
instead of asking again. Neither sibling agents nor another project's stories
are authority for the user's intent.

Settle all substance the format requires: grammar, option behavior, literal
or variable output, return/exit behavior and streams, preconditions, and
postconditions. Do not invent any of these or treat a proposal as approved.
Record settled decisions and their source so fresh agents can use them.

Pending decisions pause only dependent work, following fanout. Continue
authoring and verifying independent portions. Keep unresolved alternatives in
scratch material rather than writing guesses into stories. A genuine blocker
uses fanout's stop-and-report behavior through the user channel above.

## Completion criteria

Verification must establish:

- Every requested interaction and settled decision is represented faithfully,
  with no unsupported product behavior.
- Each completed story follows the canonical format and describes observable
  behavior rather than implementation.
- Existing and changed groups agree on shared behavior. Assign cross-group
  consistency and coverage as bounded work, including unaffected groups that
  share a changed contract.
- Every requested outcome has verified coverage, an explicit user exclusion,
  or a specific pending decision. Only the first two permit completion.

Report added and changed stories by heading and file, consistency changes,
scratch evidence locations, and unresolved questions with affected work.
Identify designs that now diverge and name `draft-design` as the next step
without starting it. Label unresolved work as a partial draft. If existing
stories already satisfy the request, report that verified result without
rewriting them.
