# Auditing test adequacy ($audit-spec)

See `../SKILL.md` for the id rules and the canonical tag/grep.

The mechanical gap only checks id presence. Audit judges **adequacy**: read every requirement and the test(s) tagged with its id, and decide whether the test genuinely verifies the requirement — no bare literals, no skips laundering a failure, assertions that actually exercise the behavior.

Route each finding by the in-role/out-of-role line:

- **In-role** (test inadequate, but the requirement is fine and testable): strip the id tag from (or delete) the inadequate test. That re-opens the id in the next gap, so the loop rebuilds it — and verify catches what the first test missed. Because stripping is destructive and judgement-based, also write a short report of what was stripped and why (git holds the reversal).
- **Out-of-role** (the requirement itself cannot really be tested, or the design/seam is wrong): file an issue. It needs a design change, not another build turn.

## Filing an issue

`specs/issues/` is the escalation channel for friction that cannot be resolved in-role — a wrong seam, contradictory requirements, a missing dependency, broken tooling. It is distinct from build-loop feedback, which is a gap the builder can close within the current contract.

- One markdown file per issue, id-named with `idgen -p I` → `specs/issues/I-XXXX-XXXX-<slug>.md`.
- Contents: filing context, the requirement id(s) involved, the friction, why it is unresolvable in-role, evidence (conflicting ids, failing command output), and a suggested resolution.
- An issue must carry proof; a vague "cannot proceed" issue is invalid.
- Resolve by deleting the file (git holds history). The gate is simply whether `specs/issues/` is empty.
- Any open issue halts the loop at gather.
