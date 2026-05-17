# 2026-05-17 — AMI pins become documentation, not a replacement trigger

## What

Bumped the `ami` pin on both prod instances (`aws_instance.dnd`,
`aws_instance.ralph_scoops`) from `ami-0cf8dce2cda56aa67`
(AL2023 2023.11.20260413) to `ami-0f5b1543e7f934f48`
(AL2023 2023.11.20260514, kernel-6.18), and added
`lifecycle { ignore_changes = [ami] }` to both instances.

This follows the in-place release upgrade recorded earlier today — the pin
now matches the release the running boxes were upgraded to.

## Why these choices

**Pin tracks reality, but in-place upgrade is the patching mechanism.**
Patching is done by `dnf upgrade --releasever=...` on the live instances, not
by rolling AMIs. Changing `ami` on an existing `aws_instance` forces
destroy+recreate, which terminates a live prod box (loses local OS state and
service config; the SSM-stored Anthropic key survives and the EIP just
re-associates). So `ignore_changes = [ami]` makes the pin inert for existing
instances. It still applies on genuine from-scratch creation (`-replace`, new
resource, or an externally-terminated instance Terraform rebuilds), so a
clean rebuild starts from a patched image instead of a year-old one.

**Convention going forward.** Every in-place release upgrade should also bump
the AMI pin and add a devlog note, so the documented base image keeps tracking
the release the fleet actually runs. The pin is documentation of intent; the
devlog is the audit trail.

**kernel-6.18 variant, not kernel-default.** The new pin was resolved from the
SSM public parameter `al2023-ami-kernel-6.18-x86_64`, not
`al2023-ami-kernel-default-x86_64`. The "default" parameter currently points
at a kernel-6.1 image; the previous pin and the running instances are on the
6.18 kernel lineage. Taking "latest default" would have silently regressed the
rebuild baseline from 6.18 to 6.1. Parity with what's actually running beats
"newest generic."

## Tradeoffs accepted / deferred

- **Manual discipline, not enforced.** Nothing verifies the pinned AMI matches
  the running release; the comment + devlog are the only coupling. A future
  upgrade that skips the pin bump will silently leave rebuilds on a stale
  image. Automated enforcement deferred — not worth it for two instances.
- **The ignore is bidirectional.** If we ever genuinely want Terraform to roll
  a new AMI onto existing instances, the `ignore_changes` line must be removed
  temporarily or `terraform apply -replace=...` used explicitly. Accepted: the
  destructive path should require an explicit, deliberate action.
