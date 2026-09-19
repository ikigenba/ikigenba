---
name: fanout
description: Decompose goals exceeding one agent's capacity into recursively delegated work and independent verification.
---

# Fanout

The goal exceeds one agent's capacity. Execution and verification must both
divide. Correctness rests on sufficient decomposition and verified parts.
No agent rechecks the whole.

## Goal

Every agent receives this document, the goal, and its assignment. The goal
supplies the absolute working directory, verifiable completion criteria,
authority boundaries, and where to report blockers with evidence. A calling
skill may supply this goal; the user need not invoke fanout separately.
Ask if missing.
Confirm the directory with `pwd`; stay there. Respect the boundaries.

## Decomposition

Every coordinator (including the root) owns this claim:

**If every assigned part succeeds, my assigned goal is satisfied.**

Before delegating, establish:

- Coverage: every completion criterion is accounted for.
- Compatibility: dependencies, shared changes, and integration have owners
  and an execution order where needed.
- Sufficiency: the parts' completion criteria together establish the parent
  goal, including relationships between parts.

Explain why the parts suffice in delegation messages. Delegate discovery when
needed. Revise the decomposition when evidence breaks it. A collection of passing
parts cannot establish a goal the decomposition omitted.

Give each child its scope, completion criteria, dependencies, and exclusive
write authority for its subtree. Coordinators subdivide that authority among
children. Reassign authority between subtrees only after the previous subtree
relinquishes it. Delegate integration and checks of relationships as bounded
parts; split those recursively when necessary.

## Roles

Every agent holds exactly one role. Mixing roles costs a coordinator its
context and a verifier its independence.

A **coordinator** decomposes a goal into parts, delegates them, and accepts
verified results. It reads only assignments, reports, and evidence summaries —
never implementations or diffs. It reports success when its decomposition
remains sufficient and all parts pass. Parents trust that conclusion;
verification composes through the tree. The root uses the same rule.

A **leaf** completes one bounded assignment.

A **verifier** independently challenges one leaf's result against its assigned
criteria. It inspects relevant existing material too. It never edits. It
returns pass with supporting evidence, or fail with specific findings.
Verification evidence identifies the artifact state checked.

A leaf or verifier that cannot finish within its context returns a proposed
split early; its coordinator delegates the parts.

When verification splits, fresh verifiers check bounded subsets; the coordinator
accepts only when their combined evidence establishes the original criteria,
including relationships between parts.

Every leaf result requires a fresh verifier. Later changes affecting a passed
check require rechecking. A criterion already met needs no work; the claim
that it is met needs verification.

## Context

Spawn fresh agents without inherited conversation. Keep assignments, reading,
command output, and reports bounded. Reports name outcomes, evidence, artifact
locations, and unresolved work. Keep at most six direct children; use
sub-coordinators for more.

On capacity refusal, wait only for children that can progress without spawning.
Otherwise, return evidence and pending work upward and exit. Exiting must release
capacity. Parents reduce concurrency or delegation depth before retrying.

## Failure

Delegate evidenced defects or unfinished work as bounded repairs. Preserve
unaffected verified work. Recheck affected criteria and decomposition.

For interactive goals, a pending user decision pauses only work that depends
on it. Report the question, affected criteria, and evidence upward; the root
is the single contact with the user. Continue independent work, including
verification of completed portions. Apply answers to dependent assignments
and recheck affected results. Never invent approval or report the whole goal
complete while required decisions remain open. If only pending decisions
remain, report partial progress and the answers needed to resume. An open
decision is not itself a confirmed blocker that stops the tree.

Report genuine blockers with evidence through the goal's designated channel.
Difficulty or size requires decomposition,
not a blocker. Validate blocker claims before propagating them. On a confirmed
blocker, stop delegation, stop active descendants, and report completed work,
remaining work, and the blocker. Resume from preserved evidence.
