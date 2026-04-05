# 2026-04-04 — test environment initial setup

## What

Created the Terraform root module for the `test` environment (account 213629091798) and applied it.

Resources created:

- `aws_route53_zone.env` — hosted zone `test.metaspot.org` (zone ID `Z07571552VE2BCWKNR2NN`)

Nameservers assigned by Route53:

```
ns-1078.awsdns-06.org
ns-160.awsdns-20.com
ns-1934.awsdns-49.co.uk
ns-741.awsdns-28.net
```

Delegation records were created in the `metaspot.org` zone (`Z03917382H7JCX6JDFEP4`) in the `mgmt` account via the AWS CLI (`aws route53 change-resource-record-sets --profile mgmt`). Public resolution of `test.metaspot.org` NS via `8.8.8.8` was verified after creation.

The delegation records in `mgmt` were created directly via CLI and are not currently managed by any Terraform.

## Why these choices

**Subdomain delegation pattern.** The apex `metaspot.org` zone remains in `mgmt`, and each environment owns its own subtree (`test.metaspot.org` here). This keeps DNS blast radius contained — a mistake inside the env cannot affect the apex or sibling environments — and allows ACM DNS validation to work naturally inside the account that owns the records.

**Backend configured for this account's S3 bucket.**

```
bucket       = "metaspot-test-tfstate-213629091798"
key          = "test/terraform.tfstate"
region       = "us-east-2"
profile      = "test"
encrypt      = true
use_lockfile = true
```

State and lock file both live in test's own account. No DynamoDB lock table — native S3 locking via `use_lockfile = true`.

**Default tags.** Provider applies `Project=metaspot`, `Environment=test`, `ManagedBy=terraform` to all taggable resources automatically.
