---
name: audit-spec
description: Audit test adequacy for matched requirement ids using fanout. Verify findings, remove inadequate coverage tags, and file issues for untestable requirements. Human-gated; never self-invoked.
---

# Audit test adequacy

Only the user starts this operation. Invoke it directly; load
[fanout](../fanout/SKILL.md) for execution and verification and
[spec](../spec/SKILL.md) for ids, the canonical gap, project ground, and issues.
The root is a fanout coordinator. This skill supplies the goal below; fanout
owns the agent roles, decomposition, ownership, verification, capacity
handling, and repairs. It does not authorize starting `build-spec`.

## Goal and authority

Establish whether tests genuinely verify each id present in both design and
the declared test-file set at the start of the audit. The mechanical gap
establishes presence only; audit establishes adequacy.

Resolve the selected sub-project and confirm its absolute directory holds
`specs/` and project ground in `AGENTS.md`; ask if the project is unidentified.
Read applicable ancestor guidance. Supply that directory, this skill, the
audit scope, and completion criteria to the fanout assignments. Never
substitute repository-wide guidance for project ground.

Design and ground are read-only. Writes are limited to removing inadequate
test tags or tests and filing evidenced issues under `specs/issues/`.
No implementation changes, replacement tests, requirement edits, or minted
ids. Preserve unrelated test assertions and coverage tags. Evidence and
working inventories belong in external scratch material under the
[handoff convention](../handoff/SKILL.md#scratch-file-convention).

## Audit work

Delegate inventory of the initial matched ids and canonical gap, identifying
the artifact state. Partition adequacy work by requirements and overlapping
tests, with explicit ownership of shared files. Existing unmatched ids remain
build work; do not widen the audit to implement them.

For each assigned id, inspect its requirement, all tagged tests contributing
to its coverage, and relevant existing implementation or fixtures needed to
judge their assertions. Determine whether tests exercise and assert the
required behavior, rather than relying on bare literals, irrelevant assertions,
or skips that conceal failure. Assess their combined coverage: several tests
may together prove a requirement even when no single test proves it all.
Report the evidence for each finding.

Every audit result receives fresh independent verification, including an
"adequate" verdict. For a proposed removal the verifier challenges the
finding by trying to establish that the existing test is adequate. No removal
lands before that finding passes verification.

Route verified findings:

- **Coverage incomplete, requirement testable:** an owning leaf removes that
  requirement's tags from the declared test set so the id reopens. Preserve
  useful partial assertions and other ids for the subsequent build to use.
- **Coverage complete, but a tag is irrelevant:** remove only the unsupported
  tag; retain tags contributing to the verified complete coverage.
- **Requirement untestable or contract cannot be satisfied:** file an evidenced
  issue under spec's rules. Validate it as a blocker and use fanout's halt
  behavior; the audit cannot redesign the contract.
- **Test adequate:** retain it and record the verified evidence.

Do not repair tests here. Delete a test only when doing so loses no useful
assertions or other coverage. Verify every applied removal against its
confirmed finding. Removing an irrelevant tag does not reopen an id still
adequately covered elsewhere; report the actual net gap change.

## Completion criteria

Verification must establish:

- Every initially matched id has a supported adequacy verdict, or a specific
  evidenced blocker explaining why the audit is incomplete.
- Adequate verdicts and proposed removals were independently challenged.
- Applied removals match confirmed findings and preserve unrelated assertions
  and adequate coverage; tests were not silently repaired.
- Design, ground, and implementation remain unchanged.
- A final canonical gap measurement reports the actual net effect of edits
  against an identified artifact state.

Delegate final gap measurement and any checks needed for the applied removals
as bounded assignments. The root accepts verified evidence; it does not
inspect all tests or repeat the audit. If no ids are matched, report the
verified inventory without mutations.

Report audited and retained ids, removed tags/tests and reasons, newly opened
ids, the remaining gap, evidence locations, and issues. A confirmed blocker
halts delegation and active descendants under fanout; report completed and
remaining work without claiming a complete audit. The user may next invoke
`build-spec` to close reopened ids; do not invoke it automatically.
