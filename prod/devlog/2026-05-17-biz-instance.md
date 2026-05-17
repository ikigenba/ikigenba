# 2026-05-17 — biz instance

## What

Added a third EC2 instance to the prod account, `biz`, following the
plain `dnd.tf` pattern (no SSM/IAM, unlike `ralph-scoops`).

- `aws_instance.biz` — t3.micro, AL2023 (`ami-0f5b1543e7f934f48`,
  2023.11.20260514, kernel-6.18), default 8 GiB gp3 root, default VPC
- `aws_eip.biz` — Elastic IP
- `aws_security_group.biz` — own SG, opens 22/80/443 to `0.0.0.0/0`, all egress
- Reuses `aws_key_pair.ai4mgreenly` and the shared `data.aws_vpc.default` /
  `data.aws_subnets.default` declared in `dnd.tf` — same SSH key as the
  other two boxes
- New `biz_public_ip` and `biz_ssh` outputs

DNS:

- `biz.prod.metaspot.org` — A → EIP (in `prod.metaspot.org`, the canonical zone)
- `biz.ai.metaspot.org` — CNAME → `biz.prod.metaspot.org` (in `ai.metaspot.org`,
  the alias zone)

## Why these choices

**dnd pattern, not ralph-scoops.** `biz` was described as a plain
instance + static IP + DNS with no Anthropic API key requirement, so it
mirrors `dnd` (SG + instance + EIP + two DNS records) rather than
`ralph-scoops` (which additionally carries the SSM SecureString parameter
and a dedicated IAM role/instance-profile to read it). If `biz` later needs
the Anthropic key, lift the SSM/IAM block from `ralph-scoops.tf` then.

**Two zones, CNAME alias.** Same convention as the other two: the canonical
A record lives in `prod.metaspot.org`; `ai.metaspot.org` carries a CNAME
alias pointing back at the canonical name. R53 alias records cannot cross
hosted zones, so the indirection has to be a CNAME.

**Separate security group.** Even though the ingress rules are currently
identical to `dnd` and `ralph-scoops`, each box keeps its own SG so future
ingress changes to one workload don't couple to the others — same reasoning
recorded in the ralph-scoops devlog.

**AMI pinned + `ignore_changes = [ami]`.** Carried over verbatim from the
AMI-pin-strategy decision: the pin documents the intended base image for a
from-scratch rebuild, but in-place `dnf` upgrades are the patching
mechanism, so an AMI bump must not force-replace a live box. For a brand new
instance the pin is simply the launch image; the ignore matters once it's
running.

**Same SSH key.** Explicitly reuses `aws_key_pair.ai4mgreenly`
(`~/.ssh/id_ed25519_ai4mgreenly`) so there is one key across the prod fleet.
