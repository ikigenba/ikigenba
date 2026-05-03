# 2026-05-03 — ralph-scoops instance + Anthropic API key parameter

## What

Added a second EC2 instance to the prod account.

- `aws_instance.ralph_scoops` — t3.micro, AL2023 (`ami-0cf8dce2cda56aa67`), default 8 GiB gp3 root, in the default VPC
- `aws_eip.ralph_scoops` — Elastic IP (current value `3.146.222.40`)
- `aws_security_group.ralph_scoops` — own SG, opens 22/80/443 to `0.0.0.0/0`, all egress
- Reuses `aws_key_pair.ai4mgreenly` declared in `dnd.tf` (same SSH key as dnd-web)

DNS:

- `ralph-scoops.prod.metaspot.org` — A → EIP (in `prod.metaspot.org`, the canonical zone)
- `ralph-scoops.ai.metaspot.org` — CNAME → `ralph-scoops.prod.metaspot.org` (in `ai.metaspot.org`, the alias zone)

Anthropic API key storage:

- `aws_ssm_parameter.anthropic_api_key` — `/metaspot/prod/anthropic-api-key`, `SecureString`, encrypted with the AWS-managed `alias/aws/ssm` key
- Terraform owns the parameter's existence and shape but `lifecycle { ignore_changes = [value] }` keeps the real secret out of state. The real value was set out-of-band via `aws ssm put-parameter`.

IAM:

- `aws_iam_role.ralph_scoops` — assumed by EC2 service principal
- `aws_iam_role_policy.ralph_scoops_anthropic_key` — grants `ssm:GetParameter` on the single parameter ARN, plus `kms:Decrypt` scoped via the `kms:ViaService = ssm.us-east-2.amazonaws.com` condition
- `aws_iam_instance_profile.ralph_scoops` — attached to the instance via `iam_instance_profile`

## Why these choices

**Two zones, CNAME alias.** Followed the convention established earlier today: `prod.metaspot.org` is the canonical name (A record points at the actual EIP); `ai.metaspot.org` is the alias namespace (CNAME points at the canonical name). R53 alias records cannot cross hosted zones, so it has to be a CNAME.

**Parameter Store, not Secrets Manager.** Anthropic API keys have no programmatic rotation API, so Secrets Manager's rotation/versioning/replication features ($0.40/secret/mo) would be paid-for capabilities we cannot use. SecureString in Parameter Store is functionally equivalent for read-only consumption, and free under the standard tier.

**Layer-1 isolation only (IAM, no CMK).** A dedicated IAM role attached only to `ralph-scoops`, granting `ssm:GetParameter` on exactly one ARN, gives "only this instance can read it" with zero recurring cost. A customer-managed KMS key with a restrictive key policy would harden against future IAM mistakes elsewhere in the account, but at $1/mo per key and with only one sensitive parameter so far, it's not yet worth the dedicated chokepoint. Revisit when there are multiple sensitive parameters or multiple workloads.

**`kms:ViaService` condition instead of pinning the key ARN.** Using the AWS-managed `aws/ssm` key means we don't have a stable per-account ARN to reference cleanly. `kms:ViaService = ssm.us-east-2.amazonaws.com` constrains decryption to "decrypt only when SSM is the calling service," which is the meaningful guarantee — direct `kms:Decrypt` calls from the role outside an SSM context will fail.

**Placeholder value + `ignore_changes`.** Avoids the secret ever entering Terraform state or plan output. The parameter resource is the shape; the value is operational and lives only in SSM.

**Separate security group from `dnd`.** Even though the rules are currently identical, sharing the SG would couple the two workloads' future ingress changes. Cheap to keep them independent.
