# 2026-04-04 — Bootstrap state backends for test and prod

## What

Created S3 state backends in the `test` (213629091798) and `prod` (853624428511) AWS accounts to hold Terraform state for the per-env root modules.

Resources created by `bootstrap/test` and `bootstrap/prod` (6 resources each, via the shared `modules/state-backend` module):

- `aws_s3_bucket` — `metaspot-test-tfstate-213629091798` / `metaspot-prod-tfstate-853624428511`
- `aws_s3_bucket_versioning` — enabled
- `aws_s3_bucket_server_side_encryption_configuration` — SSE-S3 (AES256)
- `aws_s3_bucket_public_access_block` — all four blocks on
- `aws_s3_bucket_lifecycle_configuration` — noncurrent version expiration at 90 days
- `aws_s3_bucket_policy` — deny all actions when `aws:SecureTransport` is false

Region: `us-east-2` (Ohio).

## Why these choices

**Per-account state backends, not a central one.** Each environment's state lives in its own account. This matches the goal of keeping `test` and `prod` entirely separated — losing access to one env cannot affect the other, and SCPs can be applied per account without worrying about cross-account state reads.

**Shared `state-backend` module, separate root configs.** The module is reused by both `bootstrap/test` and `bootstrap/prod` to avoid duplicating resource definitions, while the root configs remain independent (separate providers, separate state). State separation was the hard requirement; module DRY is free on top of it.

**S3 native locking, no DynamoDB.** Terraform 1.10+ and the AWS provider support `use_lockfile = true` on the S3 backend, replacing the need for a DynamoDB lock table. One less resource per account, one less thing to bootstrap, one less thing to pay for. The local Terraform binary is 1.14.8, which supports this.

**SSE-S3 (AES256), not KMS.** The state bucket holds Terraform metadata; SSE-S3 provides encryption at rest without the operational overhead of a customer-managed KMS key. Can be upgraded later if a concrete need appears.

**TLS-only bucket policy.** A deny statement on `aws:SecureTransport = false` blocks any plaintext access, guarding against accidental misconfiguration.

**Versioning + 90-day noncurrent expiration.** Versioning protects against accidental state corruption/deletion; the lifecycle rule prevents unbounded version accumulation.

**Bucket names include the account ID.** S3 bucket names are globally unique; including the account ID guarantees uniqueness and makes ownership obvious from the name alone.

**Bootstrap uses a local backend.** Bootstrap cannot reference the bucket it is about to create, so its own state is stored locally for now. The `.gitignore` excludes `*.tfstate`.

## Provider and tagging

- Terraform `>= 1.10`
- AWS provider `~> 5.70` (resolved to 5.100.0)
- Default tags applied by the provider: `Project=metaspot`, `Environment={test|prod}`, `ManagedBy=terraform`, `Component=bootstrap`
