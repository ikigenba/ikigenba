---
name: build-spec
description: Close the mechanical id gap between design and tests using fanout, with verified changes and passing gates. Human-gated; never self-invoked.
---

# Close the gap

Only the user starts this operation; an agent never invokes it. Never start `audit-spec` from this operation.

Load two skills before starting:

- [fanout](../fanout/SKILL.md) governs how the run executes; the root is a fanout coordinator.
- [spec](../spec/SKILL.md) defines the `specs/` system: its files, their formats, and the rules that connect them.

This skill supplies the goal, authority, blocker channel, and completion criteria; fanout supplies the method.

## Goal and authority

Identify the sub-project before anything else:

1. If the user named a sub-project, use it.
2. Otherwise, if the working directory is inside a sub-project, use the nearest directory at or above it that holds `specs/`.
3. Otherwise, ask the user which sub-project to build.

Confirm the sub-project's absolute directory holds both `specs/` and the sub-project's `AGENTS.md`; if not, stop and report. Never use the repository root's `AGENTS.md` in its place. Read applicable ancestor guidance. Supply that directory, this skill, the assigned gap ids, and completion criteria to the fanout assignments.

Close the mechanical gap between design ids and tagged test ids. Verify the work performed to close it and pass the gates declared in the sub-project's `AGENTS.md`. Adequacy of ids already matched before the run belongs to `audit-spec`, not this operation.

Design and the sub-project's `AGENTS.md` are read-only. Source and test changes must close specific gap ids; issue files are the escalation channel defined by spec. Never mint ids or run `idgen`. Never alter a contract to make work pass. Do not add aliases, shims, or forwarding layers preserving superseded shapes.

## Build work

Missing design, an incomplete sub-project `AGENTS.md`, or any open issue under spec's rules halts the run. Delegate inventory of the canonical gap using the test files declared in the sub-project's `AGENTS.md`. Record the initial adds, removals, already-matched ids, and artifact state for scope control.

An independently verified empty gap ends the run with id counts: no source review, test adequacy inspection, gate execution, edits, or commits. Verification of this result checks only the declared sets and mechanical gap calculation.

For a nonempty gap, partition work by design seam and overlapping source/test changes. Paired additions and removals from a replacement land together so the superseded shape is retired. Shared files, integration, commits, and gate execution need explicit owners and ordering; a passing gate must describe an identifiable integrated artifact state, not concurrent unfinished edits.

Implementers read their assigned requirements and relevant code and tests; targeted lookups resolve adjacent contracts. Use the toolchain, test-file set, exact ordered gates, and commit convention declared in the sub-project's `AGENTS.md`. Commit in green phases naming the gap ids and following repository attribution. Only stage the phase's owned changes.

## Blockers and handoff

File genuine blockers in `specs/issues/<slug>.md` under spec's issue rules: contradictory or unsatisfiable requirements, false dependency facts, unavailable required tooling, or gates that cannot pass within the contract. Include ids, commands and output, or quoted contradictions. Difficulty and size require decomposition, not issues. Assign validation of a blocker claim; invalid issues are removed by an authorized leaf and the work resumes.

On a confirmed blocker, immediately report the issue, completed phases, and remaining gap. Pause work that depends on resolving the blocker, and let independent tasks finish and verify their results under fanout. There is no interactive decision queue for changing read-only contracts during a build. Preserve committed, verified work; a later user-invoked run resumes from the recomputed gap after resolution.

## Completion criteria

Verification must establish:

- Design and test id sets agree; all initial adds and removals are resolved.
- Every test added or changed to close a gap id genuinely asserts its requirement. Id presence and a passing test alone are insufficient.
- The implementation realizes the assigned contract, including replacement of superseded behavior, without changing design or the sub-project's `AGENTS.md`.
- Every declared gate exits zero, in the declared order, with nothing skipped or suppressed.
- Changes are committed in green phases under the declared convention, and each commit names the gap ids it closes.

Delegate final integrated gap measurement and ordered gate execution as bounded assignments, with independent verification of their evidence and artifact state. Coordinators accept verified reports rather than rereading code or rerunning the whole sub-project themselves. Later changes invalidate affected checks under fanout's rules.

Reports identify ids closed, commit hashes, gate results, checked artifact state, evidence locations, issues, and remaining work. Keep detailed evidence in the artifacts or external scratch material, not coordinator context.
