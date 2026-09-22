---
name: fanout
description: Decompose goals exceeding one agent's capacity into recursively delegated work and independent verification.
---

# Fanout

The goal exceeds one agent's capacity. Both execution and verification must be split into smaller tasks and delegated to sub-agents. To establish that the goal has been met, the smaller tasks must collectively cover everything the goal requires, and each task's result must be independently verified. No single agent rechecks the work as a whole.

## Goal

Every agent receives this document, the overall goal, and its assigned task. The goal specifies the absolute working directory, verifiable completion criteria, authority boundaries, and where to report blockers with evidence. Ask for any missing information. Confirm the directory with `pwd` and stay there. Respect the stated boundaries.

## Decomposition

Every coordinator, including the root, owns this claim:

**If every delegated task succeeds, my assigned task is complete.**

Before delegating, establish:

- Coverage: every completion criterion is addressed.
- Compatibility: dependencies, shared changes, and integration have owners and an execution order where needed.
- Sufficiency: completing the delegated tasks satisfies the coordinator’s assignment, including relationships between tasks.

Explain in each assignment how the delegated tasks together satisfy the goal. Delegate discovery when needed. Revise the task breakdown when evidence shows it is incomplete or incorrect. Successful tasks cannot satisfy a goal they do not fully address. Plan within the twelve-sub-agent limit, leaving room for verifiers and repairs.

Give each sub-agent its scope, completion criteria, dependencies, and exclusive write authority. Coordinators divide that authority among their sub-agents. Transfer authority between subtrees only after the previous subtree relinquishes it. Delegate integration and checks of relationships between tasks as bounded tasks, splitting them further when necessary.

## Roles

Every agent holds exactly one role. Mixing roles costs a coordinator its context and a verifier its independence.

A **coordinator** decomposes a goal into tasks, delegates them, and accepts verified results. It reads only assignments, reports, and evidence summaries — never implementations or diffs. It reports success when the delegated tasks collectively satisfy its assignment and every task’s result has passed independent verification. The delegating coordinator trusts that conclusion; verification composes through the tree. The root uses the same rule.

A **leaf** completes one bounded assignment.

A **verifier** independently challenges one leaf's result against its assigned criteria. It inspects relevant existing material too. It never edits. It returns pass with supporting evidence, or fail with specific findings. Verification evidence identifies the artifact state checked.

A leaf or verifier that cannot finish within its context returns a proposed split early; its coordinator delegates the smaller tasks.

When verification splits, fresh verifiers check bounded subsets; the coordinator accepts only when their combined evidence establishes the original criteria, including relationships between tasks.

Every leaf result requires a fresh verifier. Later changes affecting a passed check require rechecking. A criterion already met needs no work; the claim that it is met needs verification.

## Context

Spawn fresh agents without inherited conversation. Keep assignments, reading, command output, and reports bounded. Reports name outcomes, evidence, artifact locations, and unresolved work. A coordinator spawns at most twelve sub-agents over its whole life, counting every leaf, verifier, sub-coordinator, and replacement. The limit protects the coordinator's context, which every report consumes; it is not a limit on how many run at once. When a task needs more, delegate parts of it to sub-coordinators.

On capacity refusal, wait only for sub-agents that can progress without spawning. Otherwise, return evidence and pending work upward and exit. Exiting must release capacity. Coordinators reduce concurrency or delegation depth before retrying.

## Failure

Delegate repairs for confirmed defects or unfinished work as clearly scoped tasks. Preserve unaffected verified work. Recheck affected criteria and decomposition.

For interactive goals, a pending user decision pauses only work that depends on it. Report the question, affected criteria, and evidence upward; the root is the single contact with the user. Continue independent work, including verification of completed portions. Apply answers to dependent assignments and recheck affected results. Never invent approval or report the whole goal complete while required decisions remain open. If only pending decisions remain, report partial progress and the answers needed to resume. An open decision is not itself a confirmed blocker that stops the tree.

Report genuine blockers with evidence through the goal's designated channel. If a task is too difficult or too large, break it into smaller tasks rather than report it as blocked. Validate blocker claims before propagating them. On a confirmed blocker, report it upward immediately with completed and remaining work. Pause work that depends on resolving the blocker, and let independent tasks finish and verify their results. Resume from preserved evidence.
