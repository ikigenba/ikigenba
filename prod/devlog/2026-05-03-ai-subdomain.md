# 2026-05-03 — ai.metaspot.org subdomain

## What

Added a second hosted zone to the prod account: `ai.metaspot.org` (zone ID `Z0474191SV1EFVTT4ZNO`). The prod account now owns two top-level subzones of `metaspot.org`:

- `prod.metaspot.org` — canonical namespace for resources in this account
- `ai.metaspot.org` — alias namespace; records here will be CNAMEs pointing into `prod.metaspot.org`

Nameservers assigned by Route53 for `ai.metaspot.org`:

```
ns-345.awsdns-43.com
ns-682.awsdns-21.net
ns-1323.awsdns-37.org
ns-1797.awsdns-32.co.uk
```

Delegation `NS` recordset created in the mgmt `metaspot.org` zone (`Z03917382H7JCX6JDFEP4`) via `aws route53 change-resource-record-sets --profile mgmt`. Public resolution via `8.8.8.8` confirmed after the change reached `INSYNC`.

The mgmt-side delegation records — both this one and the earlier `prod.metaspot.org` one — are still unmanaged by Terraform. They will be imported into a future mgmt root module rather than recreated.

## Why these choices

**Two zones in one account, not nested.** `ai.metaspot.org` is a sibling of `prod.metaspot.org` rather than `ai.prod.metaspot.org`. The user-facing name should be short (`foo.ai.metaspot.org`) without leaking the environment label, while internal/canonical names stay under `prod.metaspot.org` so DNS still reflects which account owns the underlying resource.

**CNAMEs, not Route53 aliases, for the indirection.** R53 alias records can only target supported AWS resources or other records *in the same zone* — they cannot alias across hosted zones. So the `ai.* → prod.*` mapping will be per-name `CNAME` records. There is no wildcard form that substitutes the label, so each alias is an explicit one-line entry (likely driven by a `for_each` map in Terraform once the first one lands).

**Delegation done via CLI, matching the existing precedent.** The `prod.metaspot.org` delegation in `mgmt` was also created via CLI (see `2026-04-04-initial-setup.md`). Keeping the same path here avoids partial Terraform ownership of the mgmt zone; both delegations will be imported together when the mgmt zone moves under Terraform.
