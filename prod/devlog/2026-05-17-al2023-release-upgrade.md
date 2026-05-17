# 2026-05-17 — AL2023 release upgrade on both prod instances

## What

Upgraded both prod EC2 instances from AL2023 release `2023.11.20260413` to
`2023.11.20260514`, then rebooted.

- `dnd-web` (`i-035553a87822218d1`, 3.132.0.167)
- `ralph-scoops` (`i-0c86bf8d7d9152afd`, 3.146.222.40)

Both: `sudo dnf upgrade --releasever=2023.11.20260514 -y`, then reboot.

Changes pulled in: new kernel `kernel6.18-6.18.25-57.109` (running, was
`6.18.20-20.229`), glibc, microcode_ctl, python3 stack, selinux-policy, tzdata
2026b, vim 9.2.240, dnf/yum. `needs-restarting -r` flagged glibc + microcode
→ reboot required.

Post-reboot verification on both: release `2023.11.20260514`, kernel
`6.18.25-57.109`, `nginx` active; on ralph-scoops `ralph-scoops.service` also
active; no further reboot pending.

This is an operational/maintenance change only — no Terraform was modified
(AMI is still pinned to `ami-0cf8dce2cda56aa67` in `dnd.tf` /
`ralph-scoops.tf`; a future instance replacement starts from the older image
and would re-run this upgrade, or the AMI pin should be bumped).

## Why / notes

**Standard `dnf upgrade -y` is a no-op here.** AL2023 pins a `releasever`;
within a pinned release the boxes were already fully patched. Moving forward
requires an explicit `--releasever=<version>`. Latest available version comes
from `dnf check-release-update` (its listing goes to stdout but the command
exits non-zero when updates exist — don't discard it or gate on exit code).

**SSH access** (also recorded in `AGENTS.md`): key
`~/.ssh/id_ed25519_ai4mgreenly` (matches `aws_key_pair.ai4mgreenly`), user
`ec2-user`.
