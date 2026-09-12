# infra

The Terraform for the AWS accounts this repository manages. Every account
root has the same shape — a hosted zone, a backup bucket, a security group, a
key pair, a launch template, a permissions boundary, a cost budget, and the
`/ikigenba/account` properties entry — and accounts differ only in the values
of their `locals` and properties. Each account is referred to by its ID
everywhere here — directory names, state key, profile, tags:

- **`295229566359`** — the `ikigenba.dev` account: the authoritative
  `ikigenba.dev` hosted zone, the host answering at `ikigenba.dev`, and the
  backup bucket; full substrate.
- **`602773793009`** — the `sbx.ikigenba.dev` account. Created
  2026-09-10; full substrate. Owns the `sbx.ikigenba.dev` hosted zone,
  delegated from the `ikigenba.dev` zone.

New things — buckets, roots, tags — use the `ikigenba-`
name and `Project = "ikigenba"`. Legacy names in `295229566359`
(`metaspot-dev-tfstate-295229566359`, `Project = "metaspot"`, the `dev` in
resource names, the key pair, the SSM path, the host's ssh alias) came in with
that lineage and stay as they are — renaming them would churn live resources
for no gain; the `dev` there is a host/resource name, not the account's.
Every account root carries a monthly cost budget (`budget.tf`) with email
notifications; it only notifies and changes nothing.

Nothing about the AWS organisation, the registration of `ikigenba.dev`, the
`mgmt` account, the `int` account, or any other account or domain is managed
or changed from here; those live elsewhere (the standalone `metaspot`
repository) and are out of scope.

**This module is not spec-governed.** There is no `specs/` here, no requirement
ids, and none of the spec operations apply. It is hand-maintained: changes are
made only on direct instruction. An agent may `init`, `validate`, `fmt`, and
`plan` freely; **it runs `terraform apply` only when directly instructed to**,
and it never imports, moves, or edits state.

Every path below is relative to this directory (`infra/`). Region `us-east-2`.

## Layout

- `295229566359/` — the `ikigenba.dev` account root. The account's `domain`
  is `ikigenba.dev`; it owns the authoritative `ikigenba.dev` hosted zone
  (`shared.tf`; delegated at the registrar from the management account, which
  is out of scope here), and spaces are `<name>.ikigenba.dev` by default,
  written from this account. The zone also carries the NS record delegating
  `sbx.ikigenba.dev` to `602773793009` (`aws_route53_record.sbx_ns` in
  `shared.tf`); its name servers are hardcoded literals by policy —
  cross-account values are never read via `terraform_remote_state`. Also owns
  the `dev` host (`dev.tf`): one instance answering at `ikigenba.dev` and
  `*.ikigenba.dev`, its `dev-` security group, instance role and profile, and
  Elastic IP. And the substrate every space is built on and nothing per-space:
  the `ikigenba-space` launch template (`launch.tf`), the
  `ikigenba-space-boundary` permissions boundary (`iam.tf`; its Route 53
  statement covers every hosted zone in the account — the per-space policy
  narrows to one), the backup bucket `ikigenba-dev-295229566359` (declared in
  `shared.tf`, shared with the `dev` host; its 30-day expiry rule is in
  `backups.tf`), the `ikigenba-space-` security group (`network.tf`; the
  default-VPC lookups it uses are in `shared.tf`), the `ikigenba` key pair
  (`ssh.tf`, alongside the legacy `ikigenba_dev` pair in `shared.tf` — same
  public key), the Parameter Store entry `/ikigenba/account` (`account.tf`),
  the monthly cost budget `ikigenba-monthly` (`budget.tf`; its amount and
  email are the `budget_monthly_usd` and `budget_email` locals), and the
  account's knobs in `locals.tf`. See "Spaces" below. Backend: S3 bucket
  `metaspot-dev-tfstate-295229566359`, key `295229566359/terraform.tfstate`,
  profile `295229566359`, region `us-east-2`, `use_lockfile = true`. Default
  tags: `Project = "metaspot"`, `Account = "295229566359"`,
  `ManagedBy = "terraform"` — legacy, and every resource in this root,
  the space substrate included, carries them. The backend block in
  `providers.tf` is the whole configuration; `terraform init` with no arguments
  is correct.
- `bootstrap/295229566359/` — the state-backend root for the account; calls
  `bootstrap/modules/state-backend/` to create the tfstate bucket above. Same
  default tags plus `Component = "bootstrap"`.
  **Local state**: `terraform.tfstate` sits beside its `.tf` files, gitignored,
  and is present in this worktree. A missing bootstrap state file makes `plan`
  propose creating a bucket that already exists — stop and restore the file
  rather than apply.
- `602773793009/` — the `sbx.ikigenba.dev` account root. The account's
  `domain` is `sbx.ikigenba.dev`; it owns the `sbx.ikigenba.dev` hosted zone
  (`dns.tf`), delegated by an NS record in the `ikigenba.dev` zone in
  `295229566359`, and spaces are `<name>.sbx.ikigenba.dev` by default, written
  from this account. The zone's name servers are the `sbx_name_servers`
  output. Also owns the substrate every space is built on and nothing
  per-space: the `ikigenba-space` launch template (`launch.tf`), the
  `ikigenba-space-boundary` permissions boundary (`iam.tf`; its Route 53
  statement covers every hosted zone in the account — the per-space policy
  narrows to one), the backup bucket `sbx-ikigenba-dev-602773793009`
  (`backups.tf`), the
  `ikigenba-space-` security group and default-VPC lookups (`network.tf`), the
  `ikigenba` key pair (`ssh.tf`), the Parameter Store entry `/ikigenba/account`
  (`account.tf`), the monthly cost budget `ikigenba-monthly` (`budget.tf`; its
  amount and email are the `budget_monthly_usd` and `budget_email` locals),
  and the account's knobs in `locals.tf`. See "Spaces" below. Backend: S3
  bucket `ikigenba-tfstate-602773793009`, key `602773793009/terraform.tfstate`,
  profile `602773793009`, region `us-east-2`, `use_lockfile = true`. Default
  tags: `Project = "ikigenba"`, `Account = "602773793009"`,
  `ManagedBy = "terraform"`. As above, the backend
  block in `providers.tf` is the whole configuration.
- `bootstrap/602773793009/` — the state-backend root for `602773793009`;
  calls `bootstrap/modules/state-backend/` to create
  `ikigenba-tfstate-602773793009`. Same default tags plus
  `Component = "bootstrap"`. **Local state**, same rule as above: its
  `terraform.tfstate` is gitignored and present in this worktree; if it is
  missing, stop and restore it rather than apply.
- `bootstrap/modules/state-backend/` — the module both `bootstrap/*/` roots
  source.
- `templates/` — `ikigenba-env.sh.tftpl` (the first-boot user data) and
  `ikigenba-launch` (the platform launcher it installs). The `dev` host was
  created with these; `295229566359/dev.tf` renders `user_data` from them.
  Also `space-first-boot.sh`, the user data of the `ikigenba-space` launch
  template (packages only), and `space-role-policy.json`, the per-space role
  policy the operator-side tool renders; Terraform never reads the latter.

`.terraform/` directories and provider caches are not tracked; `init` recreates
them. `.terraform.lock.hcl` files are tracked.

## Spaces

A space is one EC2 instance in an account, launched from that account's
`ikigenba-space` launch template by an operator-side tool, not by Terraform.
The contract below is the same in both accounts; only these per-account
values differ:

| account | `domain` | `backup_expiry_days` | `budget_monthly_usd` | `deploy_from_main_only` | `delete_secrets_on_destroy` | `delete_backups_on_destroy` |
|---|---|---|---|---|---|---|
| `295229566359` | `ikigenba.dev` | 30 | 75 | `true` | `false` | `false` |
| `602773793009` | `sbx.ikigenba.dev` | 7 | 50 | `false` | `true` | `true` |

A space has a *domain*: by default `<name>.<account domain>`, where the
account domain is the `domain` property at `/ikigenba/account`, or one given
explicitly — any name whose hosted zone is in the account, the apex of such a
zone included. The tool finds the zone by longest-suffix match of the domain
over the account's hosted zones.

The tool creates the instance profile `ikigenba-space-<name>` at launch; its
role carries the `ikigenba-space-boundary` permissions boundary and an inline
policy rendered from `templates/space-role-policy.json`, which has five
literal placeholders: `<name>`, `<domain>`, `<zone_id>` (the zone found
above), `<account_id>` (the account the space is created in), and `<bucket>`
(the account's backup bucket; `/ikigenba/account` supplies it as
`backup_bucket`, and the boundary ARN as `permissions_boundary_arn`). The
boundary is the ceiling; the inline policy narrows it to the space's own SSM
path `/ikigenba/<name>/*`, its own bucket prefix `<name>/`, and its own DNS
names `<domain>` and `*.<domain>` in its one zone.

Spaces are registered by the `Space=<name>` tag; the instance also carries
`Domain=<fqdn>`. There is no per-space Terraform. A space holds no Elastic IP:
its public address is the one the launch template assigns, and it is written
into the space's zone as the `<domain>` and `*.<domain>` A records with
TTL 60.

The account's properties for the tool live at Parameter Store
`/ikigenba/account`, written only by Terraform (`account.tf`); `account` is a
reserved space name for that reason. `sbx` (in `295229566359` it is the
delegation to `602773793009`, not a space) and the manifest service names are
reserved too: a tool must refuse them. Objects in the backup bucket expire after
`backup_expiry_days` (`locals.tf`; see the table above). Launch-template
changes affect new launches only.

## Credentials

Profiles `295229566359` and `602773793009` are under `sso-session metaspot` in
`~/.aws/config`. Every Terraform command needs a live session — this is the
precondition for the gates:

```
aws sso login --sso-session metaspot
```

## Gates

There is no build run here; these are the operator's checks. Run them after
every change. Each must exit 0, and each `plan` must report `No changes` unless
the change was meant to produce one.

1. `terraform fmt -check -recursive` — from `infra/`.
2. In each of `295229566359/`, `602773793009/`, `bootstrap/295229566359/`,
   and `bootstrap/602773793009/`:
   `terraform init -input=false`, then
   `terraform validate`, then
   `terraform plan -input=false -detailed-exitcode` (exit 0 is `No changes`;
   exit 2 is a pending change to read before anyone applies it).

Terraform 1.10+ is required (`required_version`); the AWS provider is pinned
`~> 5.70` and locked by `.terraform.lock.hcl`.

## SSH access

The `dev` host is Amazon Linux 2023, user `ec2-user`. The key pair `ikigenba_dev`
is declared in `295229566359/shared.tf`; the private key is
`~/.ssh/id_ed25519_ikigenba_dev`. The `Host ikigenba.dev dev` entry in
`~/.ssh/config` pins the Elastic IP and selects the key. The `dev_ssh` output
prints the exact command.
