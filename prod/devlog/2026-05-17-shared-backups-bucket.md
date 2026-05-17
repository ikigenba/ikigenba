# 2026-05-17 — shared prefix-isolated backups bucket

## What

Added one env-wide S3 bucket, `aws_s3_bucket.backups`
(`metaspot-prod-backups-853624428511`), to `shared.tf`: Block Public Access
(all four), `BucketOwnerEnforced` ownership, default SSE-S3 (AES256), and a
bucket policy denying non-TLS access. No versioning.

Each server writes only under its own key prefix `<node>/`. Enforcement is
per-server in `<name>.tf`: `biz` got `aws_iam_role_policy.biz_backups`
(`backups-rw`) on its existing role — object R/W/Delete on
`<bucket>/biz/*`, and `s3:ListBucket` on the bucket conditioned to
`s3:prefix = ["biz/*"]`. The instance role is now **standard for every
server** (it carries the backups grant always); the `app-config` secrets
grant became the opt-in extra on the same role. `METASPOT_BACKUP_BUCKET`
was added to the env-file template and written to `biz` out-of-band;
the prefix is derived as `$METASPOT_NODE/`, not stored.

`terraform apply`: 6 added, 0 changed, 0 destroyed.

## Why these choices

**Shared bucket + per-prefix IAM, not a bucket per server.** A bucket per
server would multiply lifecycle/policy/encryption config and bump against
account bucket limits for no security gain over tightly-scoped IAM. One
bucket with per-server prefixes is the standard multi-tenant pattern.

**The isolation is the `s3:ListBucket` prefix condition + absence of any
bucket-wide grant.** `GetObject` scoped to `<node>/*` is the obvious half;
the half people miss is that `s3:ListBucket` is a *bucket*-level action — an
unconditioned list grant leaks every other server's keys even when object
reads are scoped. The policy conditions list on `s3:prefix`, and no
statement anywhere grants `s3:*`/objects bucket-wide, so a server provably
cannot see or touch another's prefix. This holds because there is one app
per box and the role arrives via the instance profile (no role assumption
across boxes).

**SSE-S3, not per-server KMS.** Consistent with the explicit "Layer-1
isolation only, no customer-managed KMS until justified" stance from the
ralph-scoops secrets devlog. SSE-S3 is free; isolation between servers is
IAM, not crypto. A per-server CMK ($1/key/mo + key-policy management) would
add a backstop against a future IAM mistake — deferred until there's a
concrete reason, same as for `app-config`. This is documented as a known
limitation, not an oversight.

**No versioning.** Chosen deliberately: backups here are
write-then-restore, and versioning's value (undo accidental
overwrite/delete) was judged not worth the cost/complexity for the current
single-box footprint. Revisit if backups become business-critical or
multi-box churn makes accidental clobber likely.

**Instance role promoted to standard.** Previously a role existed only when
secrets = yes. Backups need a role on every box, so rather than gate it,
every server now always has a role/profile; secrets is just an additional
`aws_iam_role_policy`. Simpler mental model and removes the "no role at all"
branch from the standard.

**Account-ID-suffixed bucket name.** `metaspot-prod-backups-853624428511`
is globally unique without a random suffix, and the suffix is the same
account ID already documented and present in `/etc/metaspot/env`.
