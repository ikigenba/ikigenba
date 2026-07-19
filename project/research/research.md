# Research — external ground truth

Non-contractual evidence design references instead of re-deriving. Facts here
were verified live against the `int` AWS account on 2026-07-18 unless noted.

## AWS SSM Parameter Store (us-east-2)

- **Standard-tier parameters cap at 4,096 bytes** of value. The suite's former
  shared secrets blob (`/ikigenba/int/app-config`, one JSON object keyed by
  app) measured 3,995 bytes on 2026-07-18 — one more key of any size would
  have failed the next write. Advanced tier raises the cap to 8 KB but costs
  $0.05/parameter/month and only defers the wall.
- **Standard-tier parameters are free** and the per-account quota (10,000
  parameters per region) is far above the suite's needs. Fourteen per-app
  parameters replace the one blob at zero cost; the largest single app's
  secret set (github, whose RSA App private key PEM dominates) is ~1.8 KB —
  comfortable Standard-tier headroom per app.
- **`put-parameter --overwrite` creates the parameter when it does not
  exist.** This is what lets per-app parameter *existence* be script-managed:
  the first push creates, later pushes overwrite, and no Terraform resource
  per parameter is needed.
- **Parameter names are hierarchical by `/`, and a leaf may coexist with
  children.** `/ikigenba/int/app-config` (the old blob) and
  `/ikigenba/int/app-config/<app>` (the per-app parameters) are simultaneously
  valid, which made the migration's coexistence window possible.
  `get-parameters-by-path --path /ikigenba/int/app-config` enumerates the
  children without the leaf.
- **IAM can grant by path pattern**: a `Resource` of the parameter ARN plus
  `<arn>/*` covers the blob and all per-app children in one statement.
  Applied to the int instance role's `app-config` policy on 2026-07-18
  (metaspot commit `e771ff3`).

## The live migration (completed 2026-07-18, recorded as context)

The per-app split is **already live** on `int.ikigenba.com`: the launcher
(`metaspot/templates/ikigenba-launch`, installed out-of-band on the box)
fetches `/ikigenba/${IKIGENBA_ENV}/app-config/<app>` directly and hard-fails
on any fetch error including `ParameterNotFound`; all fourteen per-app
parameters are seeded (secret-less apps hold an explicit `{}`); all fourteen
services restarted healthy, with `github` proving end-to-end secret flow
(`github_auth: ok`). The old blob parameter remains in place, unread, as a
rollback path until the repo-side cleanup (the pending plan phases) lands.
The blob-era launcher is preserved on-box as
`/usr/local/bin/ikigenba-launch.blob-era`.
