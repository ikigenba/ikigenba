# 2026-05-17 — biz SG hardening + replace-safe SG pattern (crash recovery)

## What

Tightened `aws_security_group.biz` to the new fleet standard: **443 from
`0.0.0.0/0`** (public HTTPS), **22 from `216.173.146.119/32` only** (admin IP),
**port 80 removed**, all egress kept. The standard prose in `AGENTS.md` /
`CLAUDE.md` was updated to match earlier the same day.

In doing so the SG resource was switched from `name = "biz"` to
`name_prefix = "biz-"` and given `lifecycle { create_before_destroy = true }`.
Live SG is now `sg-0f2a145d6fe34670c`; the old `sg-045aa41d7f44e56ed` is
destroyed. `aws_instance.biz` was updated **in-place** (same instance id, EIP
`18.224.112.178` and DNS unchanged). `Apply complete: 1 added, 1 changed,
1 destroyed`, ~6s total.

This entry also records a crash recovery: the prior session's first attempt at
this change hung for ~20 minutes and was force-killed, leaving an orphaned S3
state lock.

## Why these choices

**An SG `description` is immutable, so editing it forces a replace.** The
description was changed to accurately describe the new rules ("Allow public
HTTPS and admin-only SSH"). AWS has no modify-description API, so Terraform
must destroy+recreate the SG. That replacement — not the ingress edits, which
are in-place mutable — is the entire source of the difficulty here.

**`create_before_destroy` + `name_prefix`, not the default order.** The first
attempt used a fixed `name = "biz"` with the default replace order
(destroy-then-create). Terraform tried to delete the old SG while it was still
attached to the running instance → `DependencyViolation` → the AWS provider
silently retries deletion for ~15 min before erroring. Piped through
`grep | tail` (which only flushes at process exit) the apply produced zero
output, looked hung, and the session was killed mid-flight. The fix is the
canonical SG-replacement pattern: `create_before_destroy = true` so Terraform
builds the new SG and repoints the instance *before* deleting the old one, and
`name_prefix` because a fixed name would collide with the still-existing old SG
during the create step. With both, the same change applied cleanly in seconds.
`biz.tf` is the canonical "mirror this" reference, so every future server
inherits the replace-safe pattern; the rationale is also inline in `biz.tf`.

**Recovering the orphaned lock.** The killed apply left an S3-backend lock
(`f2801d25-...`, OperationTypeApply, created 15:59:59 — the exact crashed-apply
start time). With no terraform process alive and the lock ID/owner matching the
dead apply unambiguously, `terraform force-unlock <id>` was the correct
recovery — it removes only the lock, never state. A post-unlock read-only plan
showed the *same* `1 add / 1 change / 1 destroy`, proving the killed apply had
mutated nothing (it died on the failing destroy, before creating anything);
state and AWS reality were still consistent with the pre-change SG.

## Tradeoffs / notes

- **`name_prefix` means generated SG names** (`biz-2026051716...`) instead of a
  human-friendly fixed name. Accepted: replace-safety on a live prod box beats
  a tidy console name, and resources are still found by tag/`Name`.
- **The 20-min "hang" was a tooling-visibility failure, not a Terraform bug.**
  Lesson carried forward: never pipe a long prod `apply` through
  `grep | tail` — stream full output to a file so progress (and a real hang)
  is observable. This apply was run that way.
- `216.173.146.119/32` is this shared box's egress IP; SSH from here was
  verified still working after the change. If the admin source IP ever changes,
  update it everywhere it appears (per the `AGENTS.md` SG note).
