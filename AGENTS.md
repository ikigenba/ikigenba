# metaspot

This directory contains the Terraform configuration for the AWS accounts you have access to:

- `bootstrap/` — per-account state backend bootstrap (local state)
- `test/` — `test` AWS account (213629091798), profile `test`
- `prod/` — `prod` AWS account (853624428511), profile `prod`

The apex `metaspot.org` Route53 zone lives in a separate `mgmt` account; each env owns its own delegated subtree (`test.metaspot.org`, `prod.metaspot.org`).

## devlog/

Each subfolder has a `devlog/` directory containing a dated history of changes made to that environment — what was created, and the reasoning behind the decisions. Read the relevant `devlog/` entries before making changes to an environment so you understand the existing state and the rationale behind it. When you make a meaningful change, add a new dated entry.
