---
name: build-spec
description: Close the mechanical id gap between design and tests using fanout, with verified changes and passing gates. Human-gated; never self-invoked.
---

# Close the gap

Only the user starts this operation; an agent never invokes it. Never start `audit-spec` from this operation.

Load two skills before starting:

- [fanout](../fanout/SKILL.md) governs how the run executes; the root is a `fanout` coordinator.
- [spec](../spec/SKILL.md) defines the `specs/` system: its files, their formats, and the rules that connect them.

This skill supplies the goal, authority, blocker channel, and completion criteria; `fanout` supplies the method.

## Goal and authority

Identify the sub-project before anything else:

1. If the user named a sub-project, use it.
2. Otherwise, if the working directory is a sub-project, use it.
3. Otherwise, ask the user which sub-project to build.

Confirm the sub-project's directory holds both `specs/` and the sub-project's `AGENTS.md`; if not, stop and report. Read the root `AGENTS.md` for project-wide guidance, but never use it in place of the sub-project's.

Once the sub-project is identified, every prompt a coordinator gives a sub-agent includes the sub-project's absolute directory, this skill, the sub-agent's assigned gap ids, and the completion criteria.

Close the mechanical gap between design ids and tagged test ids. Verify the work performed to close it and pass the gates declared in the sub-project's `AGENTS.md`. The work is the gap ids found at the start of the run, and each sub-agent works only on the gap ids assigned to it. Ids already present in both design and tests are not part of the work: their tests must keep passing, but are never judged, strengthened, or rewritten.

Design and the sub-project's `AGENTS.md` are read-only. Source and test changes must close specific gap ids; issue files are the escalation channel defined by `spec`. Never mint ids or run `idgen`. Never alter a contract to make work pass. Do not add aliases, shims, or forwarding layers preserving superseded shapes.

## Build work

Missing design, an incomplete sub-project `AGENTS.md`, or any open issue under `spec`'s rules halts the run. Have a sub-agent compute the gap as `spec` defines it: collect the requirement ids in `specs/design/` and the ids tagged in the test files the sub-project's `AGENTS.md` declares, then compare the two lists. Record the ids to add, the ids to remove, the ids already matched, and the commit the lists were taken from. These lists fix the work for the rest of the run.

An independently verified empty gap ends the run with id counts: no source review, test adequacy inspection, gate execution, edits, or commits. Verification of this result checks only the declared sets and mechanical gap calculation.

For a nonempty gap, partition work by design seam and overlapping source/test changes. Paired additions and removals from a replacement land together so the superseded shape is retired. Shared files, integration, commits, and gate execution need explicit owners and ordering; a passing gate must describe an identifiable integrated artifact state, not concurrent unfinished edits.

Each implementer reads its assigned requirements and the code and tests they touch. When its work depends on a name or behavior another requirement defines, it looks up that requirement. Use the toolchain, test-file set, exact ordered gates, and commit convention declared in the sub-project's `AGENTS.md`. Commit only when every declared gate passes, naming the gap ids and following project attribution rules. When committing, stage only the files your assignment owns.

## Blockers and handoff

A blocker is a contradictory or unsatisfiable requirement, a false dependency fact, unavailable required tooling, or a gate that cannot pass within the contract. Difficulty and size are not blockers; they call for smaller tasks. A sub-agent that hits a blocker reports it to its coordinator with evidence: ids, commands and output, or quoted contradictions. The coordinator has a fresh sub-agent validate the claim; if it does not hold, the work continues. Once the claim is confirmed, the coordinator writes the issue in `specs/issues/<slug>.md` from the two reports, following `spec`'s issue rules; this is the one file a coordinator writes itself.

That coordinator then reports the blocker to its own coordinator with the issue, the work completed, and its gap ids still open; each coordinator passes the report upward the same way, and the root reports it to the user. Pause work that depends on resolving the blocker, and let independent tasks finish and verify their results under `fanout`. There is no interactive decision queue for changing read-only contracts during a build. Preserve committed, verified work; a later user-invoked run resumes from the recomputed gap after resolution.

## Completion criteria

Verification must establish:

- Every gap id recorded at the start of the run is resolved: each id to add is tagged in a test, and each id to remove is gone from the tests.
- Every test added or changed to close a gap id genuinely asserts its requirement. Id presence and a passing test alone are insufficient.
- The implementation realizes the requirements behind the gap ids, including replacement of superseded behavior, without changing design or the sub-project's `AGENTS.md`.
- Every declared gate exits zero, in the declared order, with nothing skipped or suppressed.
- Every commit follows the declared convention, names the gap ids it closes, and was made with every declared gate passing.

Have one sub-agent measure the final gap and another run the gates in the declared order, each against the same commit; a fresh sub-agent verifies each result.

A run halted by a confirmed blocker cannot meet these criteria and does not claim to. It ends as a partial result: committed, verified work is kept, and the issue stays open for the user.

The root's final report to the user names the ids closed, the commit hashes, the gate results and the commit they ran against, any issues, and the gap ids still open. Detailed evidence stays in commit messages, test output, and scratch files under the [handoff convention](../handoff/SKILL.md#scratch-file-convention), not in coordinator context.
