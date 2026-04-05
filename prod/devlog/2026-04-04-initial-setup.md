# 2026-04-04 — prod environment initial setup

## What

Created the Terraform root module for the `prod` environment (account 853624428511) and applied it.

Resources created:

- `aws_route53_zone.env` — hosted zone `prod.metaspot.org` (zone ID `Z07667062ZOXQHPQCQRLC`)

Nameservers assigned by Route53:

```
ns-1519.awsdns-61.org
ns-1727.awsdns-23.co.uk
ns-4.awsdns-00.com
ns-759.awsdns-30.net
```

Delegation records were created in the `metaspot.org` zone (`Z03917382H7JCX6JDFEP4`) in the `mgmt` account via the AWS CLI (`aws route53 change-resource-record-sets --profile mgmt`). Public resolution of `prod.metaspot.org` NS via `8.8.8.8` was verified after creation.

The delegation records in `mgmt` were created directly via CLI and are not currently managed by any Terraform.

## Why these choices

**Subdomain delegation pattern.** The apex `metaspot.org` zone remains in `mgmt`, and each environment owns its own subtree (`prod.metaspot.org` here). This keeps DNS blast radius contained — a mistake inside the env cannot affect the apex or sibling environments — and allows ACM DNS validation to work naturally inside the account that owns the records.

**Backend configured for this account's S3 bucket.**

```
bucket       = "metaspot-prod-tfstate-853624428511"
key          = "prod/terraform.tfstate"
region       = "us-east-2"
profile      = "prod"
encrypt      = true
use_lockfile = true
```

State and lock file both live in prod's own account. No DynamoDB lock table — native S3 locking via `use_lockfile = true`.

**Default tags.** Provider applies `Project=metaspot`, `Environment=prod`, `ManagedBy=terraform` to all taggable resources automatically.
