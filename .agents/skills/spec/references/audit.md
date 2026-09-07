# Auditing test adequacy (audit-spec)

See `../SKILL.md` for the id rules and the canonical tag/grep. Audit shares its
shape with `build-spec`: it reads across the whole design and its tests, so it
is run by delegation for the same reason — context is the scarce resource, and
an audit that reads every requirement and every tagged test in one context has
already lost the memory it needs to judge the next one. Load the `build-spec`
skill for the roles; audit uses them the same way, on the same scopes.

The mechanical gap only checks id presence. Audit judges **adequacy**: for each
requirement, read the test(s) tagged with its id and decide whether the test
genuinely verifies the requirement — no bare literals, no skips laundering a
failure, assertions that actually exercise the behavior.

## Delegation

The audit is partitioned the way the build is, and for the same reason.

A **coordinator** holds the design set, partitions it into scopes — one design
document, or a cluster of ids within one document whose tests overlap — and
delegates each scope to a fresh **auditor** (never a fork; a fork inherits the
context being protected). It holds about six children; a design with more scopes
than that is split among sub-coordinators, so no single context accumulates a
dozen scopes' worth of requirements and tests. The coordinator never reads a
test itself.

An **auditor** holds one scope. It reads that scope's requirements and the
test(s) tagged with their ids, and nothing more — a fact about another seam
comes from a targeted grep, not from reading its document. For each id it judges
adequacy and routes the finding (below). It returns a short report and nothing
else: ids audited, ids it proposes to strip and why, and issues filed by path.

A strip is destructive and judgement-based, so — as in `build-spec`, where no
work is accepted on the word of the agent that did it — every proposed strip is
confirmed by a fresh **verifier** before it lands. The verifier reads the same
requirement and test the auditor did and tries to prove the test is in fact
adequate; it returns keep or strip with evidence. Only a strip that survives
this is applied. An "adequate" verdict needs no such check — nothing destructive
follows from it. A confirmed strip is applied within the scope that owns the
test (the auditor's scope), never by the coordinator.

Because a strip re-opens an id, the coordinator reruns the canonical gap greps
after its children return, so its report shows the audit's net effect on the
gap. The next `build-spec` run rebuilds every re-opened id.

## Routing a finding

Route each finding by the in-role/out-of-role line:

- **In-role** (test inadequate, but the requirement is fine and testable): strip
  the id tag from (or delete) the inadequate test — once a fresh verifier
  confirms it. That re-opens the id in the next gap, so the next `build-spec`
  run rebuilds it. Because stripping is destructive and judgement-based, the
  auditor records what was stripped and why in its report (git holds the
  reversal).
- **Out-of-role** (the requirement itself cannot really be tested, or the
  design/seam is wrong): file an issue. It needs a design change, not another
  build turn.

## Filing an issue

`specs/issues/` is the escalation channel for friction that cannot be resolved in-role — a wrong seam, contradictory requirements, a missing dependency, broken tooling. It is distinct from a gap a builder can close within the current contract.

- One markdown file per issue, named `specs/issues/<slug>.md`. Issues carry no minted id; nothing outside `draft-spec` invokes `idgen`.
- Contents: filing context, the requirement id(s) involved, the friction, why it is unresolvable in-role, evidence (conflicting ids, failing command output), and a suggested resolution.
- An issue must carry proof; a vague "cannot proceed" issue is invalid.
- Resolve by deleting the file (git holds history). The gate is simply whether `specs/issues/` is empty.
- Any open issue halts the build run.
