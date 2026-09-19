---
name: build-spec
description: Close the mechanical id gap between design and tests using fanout, with verified changes and passing gates. Human-gated; never self-invoked. Existing matched ids belong to audit-spec.
---

# Close the gap

Only the user starts this operation. Invoke it directly; load
[fanout](../fanout/SKILL.md) for execution and verification and
[spec](../spec/SKILL.md) for ids, the canonical gap, project ground, and issues.
The root is a fanout coordinator. This skill supplies the goal below; fanout
owns the agent roles, decomposition, ownership, verification, capacity
handling, and repairs. It does not authorize starting `audit-spec`.

## Goal and authority

Close the mechanical gap between design ids and tagged test ids. Verify the
work performed to close it and pass the project's gates. Adequacy of ids
already matched before the run belongs to `audit-spec`, not this operation.

Run `pwd` and confirm the working directory holds both `specs/` and its own
`AGENTS.md`. If not, stop and report; do not substitute a git root, worktree
root, or ancestor's ground. Give every assignment this same absolute
directory, this skill, applicable guidance, assigned gap ids, and criteria.

Design and project ground are read-only. Missing design or ground prevents
the run. Source and test changes must close specific gap ids; issue files
are the escalation channel defined by spec. Never mint ids, run `idgen`,
or require it as a build tool. Never alter a contract to make work pass.
Do not add aliases, shims, or forwarding layers preserving superseded shapes.

## Establish the work

Delegate inventory of the canonical gap using the ground's declared test-file
set. Record the initial adds, removals, already-matched ids, and artifact state
for scope control. Check for open issues and concrete required ground;
any open issue halts the run under spec's rules.

An independently verified empty gap ends the run with id counts: no source
review, test adequacy inspection, gate execution, edits, or commits.
Verification of this result checks only the declared sets and mechanical gap
calculation.

For a nonempty gap, partition work by design seam and overlapping source/test
changes. Paired additions and removals from a replacement land together so
the superseded shape is retired. Shared files, integration, commits, and gate
execution need explicit owners and ordering; a passing gate must describe an
identifiable integrated artifact state, not concurrent unfinished edits.

Implementers read their assigned requirements and relevant code and tests;
targeted lookups resolve adjacent contracts. Use the toolchain, test-file set,
exact ordered gates, and commit convention declared in the project ground.
Commit in green phases naming the gap ids and following repository attribution.
Only stage the phase's owned changes.

## Completion criteria

Verification must establish:

- Design and test id sets agree; all initial adds and removals are resolved.
- Every test added or changed to close a gap id genuinely asserts its
  requirement. Id presence and a passing test alone are insufficient.
- The implementation realizes the assigned contract, including replacement
  of superseded behavior, without changing design or ground.
- Every declared gate exits zero, in the declared order, with nothing skipped
  or suppressed.
- Changes are committed in green phases under the declared convention, and
  each commit names the gap ids it closes.

Delegate final integrated gap measurement and ordered gate execution as
bounded assignments, with independent verification of their evidence and
artifact state. Coordinators accept verified reports rather than rereading
code or rerunning the whole project themselves. Later changes invalidate
affected checks under fanout's rules.

Reports identify ids closed, commit hashes, gate results, checked artifact
state, evidence locations, issues, and remaining work. Keep detailed evidence
in the artifacts or external scratch material, not coordinator context.

## Blockers and handoff

File genuine blockers in `specs/issues/<slug>.md` under spec's issue rules:
contradictory or unsatisfiable requirements, false dependency facts, unavailable
required tooling, or gates that cannot pass within the contract. Include ids,
commands and output, or quoted contradictions. Difficulty and size require
decomposition, not issues. Assign validation of a blocker claim; invalid
issues are removed by an authorized leaf and the work resumes.

A confirmed blocker halts the tree under fanout. There is no interactive
decision queue for changing read-only contracts during a build. Report the
issue, completed phases, and remaining gap. Preserve committed, verified work;
a later user-invoked run resumes from the recomputed gap after resolution.
