# 2026-05-17 — biz gets app-config, materializing the secrets standard

## What

First materialization of the Parameter Store half of the "create a server"
standard (recorded in `AGENTS.md` earlier today).

- `aws_ssm_parameter.app_config` (`prod/app-config.tf`) — `/metaspot/prod/app-config`,
  `SecureString`, placeholder value `{}`, `lifecycle { ignore_changes = [value] }`.
  Env-level, declared once for prod.
- `biz.tf` gained `aws_iam_role.biz` + `aws_iam_role_policy.biz_app_config`
  + `aws_iam_instance_profile.biz`, and `iam_instance_profile` was attached to
  `aws_instance.biz`. The policy grants `ssm:GetParameter` + `ssm:PutParameter`
  on the single `app-config` ARN, plus `kms:Decrypt` + `kms:GenerateDataKey`
  scoped by `kms:ViaService = ssm.us-east-2.amazonaws.com`.

## Why these choices

**Shared blob + script-owned content, per the new standard.** The parameter
path is fixed and hostname-independent (`/metaspot/prod/app-config`), so any
service on any box reads/writes the same place; nothing derives from the DNS
name. Terraform guarantees the parameter exists and its shape but never its
content — the value is populated and read out-of-band, so secrets never enter
Terraform state or plan output. `{}` is the placeholder (rather than
ralph-scoops' `PLACEHOLDER-...` string) because the blob is JSON: a
read-modify-write consumer can parse it before anything has been written.

**Read *and* write, hence `GenerateDataKey`.** Unlike `ralph-scoops` (which
only reads its per-ARN secret and so only needs `kms:Decrypt`), the standard
lets services *populate* the blob. Writing a `SecureString` requires
`kms:GenerateDataKey` in addition to `kms:Decrypt`, both still constrained to
the SSM service via the `kms:ViaService` condition rather than a key ARN
(the AWS-managed `aws/ssm` key has no stable per-account ARN to pin).

**Per-server `.tf` adds IAM only.** The parameter is env-level plumbing in
`app-config.tf`; `biz.tf` only adds the role/policy/instance-profile that
gates access. This keeps the per-server file to the access decision and avoids
each new server re-declaring (and racing to own) the shared parameter.

## Tradeoffs accepted

Coarse access, as the shared-blob model dictates: `biz` (and any future
secrets-enabled box) can read *and overwrite the entire* `app-config`. IAM
cannot enforce single-key writes, so consumer scripts must read-modify-write
one JSON key, never blind-overwrite. `ralph-scoops` deliberately keeps its
existing per-ARN Anthropic-key setup; this standard governs new servers only.
The instance-profile attach to the already-running `biz` is an in-place
association, not a replacement.
