# Story-to-design fidelity review — 2026-09-15

The seven story files are byte-for-byte identical to the snapshot taken at the
start of this process and to Git HEAD. Stories are the source of truth.
Reviewed all 94 scenarios across six story groups against the 13 design files,
with subagents reviewing local commands, host lifecycle, and remote commands.
Existing draft design renames and additions were preserved.

## Design corrections

| Area | Correction |
|---|---|
| Bootstrap | Require the version to be defined in source without build-time injection; correct the count of account-requiring commands. |
| Space namespace | Refuse nested spaces independently of app names, with the account-domain exception. |
| Cloud discovery | Preserve the required duplicate-domain ambiguity check. |
| Paths | Distinguish checkout-root app paths from deploy operands relative to the invocation directory. |
| Help | Match the original space, build, and restore help blocks. |
| Host summaries | Preserve actual backed-up app names, the selected host backup key, and renewed versus not-yet-due certificate results. |
| Lifecycle postconditions | Preserve tags on stop and make list/status read-only behavior explicit. |
| Retire and remove | Distinguish devctl cloud operations from the documented delegated host backup effects. |
| Host access | Require the space's SSM reads and certificate DNS permissions. |
| Dependencies | Pin the already approved direct module set in D01. |

See the [coverage map](devctl-stories.md),
[remote command review](design-fidelity-remote.md),
[host summary review](host-summary-resolution.md),
[cloud contract review](cloud-contract-resolution.md), and
[dependency baseline](dependency-baseline.md).

## Verification and limits

All 94 source headings have current design requirement references. Six complete
command help examples match exactly; the bootstrap help frame matches with the
commands added by later stories. Unchanged requirement IDs retain identical
text. Changed requirements have fresh IDs. See [validation](validation.md).

No source code, go.mod, commits or remote state were changed. No build run or
check-spec certification was performed. External producer observations still
needed for implementation are recorded in the
[external evidence note](../issues/external-contract-observations.md); they do
not change the story-defined outcomes.
