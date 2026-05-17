# 2026-05-17 — dnd and ralph-scoops torn down; biz is the standard

## What

Destroyed all `dnd` and `ralph-scoops` infrastructure (14 resources):
both instances, security groups, EIPs, all four Route53 records, the
`ralph-scoops` IAM role/policy/instance-profile, and the
`/metaspot/prod/anthropic-api-key` SecureString. `terraform apply` reported
`0 added, 0 changed, 14 destroyed`.

`prod/dnd.tf`, `prod/ralph-scoops.tf`, and the now-empty `prod/main.tf` were
deleted. The fleet's shared primitives were first relocated into a new
`prod/shared.tf`: `data.aws_vpc.default`, `data.aws_subnets.default`,
`aws_key_pair.ai4mgreenly`, and the `aws_route53_zone.env` /
`aws_route53_zone.ai` zones (the last two moved out of `main.tf`).
Dnd/ralph outputs were stripped from `outputs.tf`. The standard docs in
`AGENTS.md` (mirrored to `CLAUDE.md`) now point at `biz.tf` as the canonical
reference and `shared.tf` for the shared blocks.

`biz` is now the sole server and the standard reference. `app-config.tf`
(the opt-in secrets blob) and `biz` were untouched.

## Why these choices

**Relocate shared blocks before deleting, not after.** `dnd.tf`
historically owned the VPC/subnet data sources and the fleet SSH key, and
`main.tf` owned the DNS zones — all of which `biz` depends on. Deleting
those files wholesale would have planned `aws_key_pair.ai4mgreenly` for
destruction (breaking `biz`'s `key_name`) and orphaned the zones. Moving a
block between `.tf` files does not change its Terraform address, so the
relocation is a pure state no-op — the plan confirmed `0 to change` for
everything kept, proving nothing shared was touched. This is the safe way to
honour "destroy dnd/ralph, keep biz": the SSH key and zones are *fleet*
infra, not dnd/ralph infra, even though they happened to live in those files.

**`shared.tf` as the home for env-wide primitives.** Co-locating the things
every server implicitly needs (network lookups, SSH key, DNS zones) means a
future single-server teardown can never again take fleet infra with it.
`app-config.tf` deliberately stays separate: it's the opt-in secrets-standard
component with its own devlog, not always-on plumbing.

**The Anthropic key died with ralph-scoops.** `/metaspot/prod/anthropic-api-key`
and its dedicated per-ARN IAM role were ralph-scoops-specific. They are gone;
the surviving secrets mechanism is the generic `/metaspot/prod/app-config`
blob in `app-config.tf`. The AGENTS.md note about ralph-scoops keeping a
per-ARN setup was removed — there is no longer an exception to document.

## Tradeoffs accepted

- **Irreversible.** Instances (and any unmanaged local OS/service state on
  them), the two EIP allocations, and the Anthropic key value are gone. This
  was explicit and intended — shrinking to a single canonical box.
- **biz's user_data still says `dnd`/`ralph` are siblings? No** — `biz`'s
  `/etc/metaspot/env` only ever described `biz` itself, so nothing on the
  surviving box references the removed hosts.
