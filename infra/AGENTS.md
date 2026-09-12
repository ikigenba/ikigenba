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
  is out of scope here), and every space's domain ends in `ikigenba.dev`
  (`ikigenba.dev` itself for the apex space), written from this account. The
  zone also carries the NS record delegating `sbx.ikigenba.dev` to
  `602773793009` (`aws_route53_record.sbx_ns` in `shared.tf`); its name
  servers are hardcoded literals by policy — cross-account values are never
  read via `terraform_remote_state`. Also owns
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
  **Local state**: `terraform.tfstate` (and its `.backup`) sits beside its
  `.tf` files and is committed — `.gitignore` names the bootstrap state files
  explicitly. It is committed because it is the only record that Terraform
  owns the state bucket: a fresh clone must plan `No changes` here, and a
  missing bootstrap state file would make `plan` propose creating a bucket
  that already exists — if that happens, stop and restore the file rather
  than apply. The state holds only the bucket and its configuration
  resources, nothing secret.
- `602773793009/` — the `sbx.ikigenba.dev` account root. The account's
  `domain` is `sbx.ikigenba.dev`; it owns the `sbx.ikigenba.dev` hosted zone
  (`dns.tf`), delegated by an NS record in the `ikigenba.dev` zone in
  `295229566359`, and every space's domain ends in `sbx.ikigenba.dev`
  (`sbx.ikigenba.dev` itself for the apex space), written from this account.
  The zone's name servers are the `sbx_name_servers` output. Also owns the
  substrate every space is built on and nothing
  per-space: the `ikigenba-space` launch template (`launch.tf`), the
  `ikigenba-space-boundary` permissions boundary (`iam.tf`; its Route 53
  statement covers every hosted zone in the account — the per-space policy
  narrows to one), the backup bucket `sbx-ikigenba-dev-602773793009`
  (`backups.tf`; the one exception to the `ikigenba-` naming rule — it
  predates the rule, and renaming a bucket is a destroy and recreate), the
  `ikigenba-space-` security group and default-VPC lookup (`network.tf`), the
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
  `terraform.tfstate` is committed; if `plan` proposes creating the bucket,
  the state is missing — stop and restore it rather than apply.
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

| account | `domain` | `backup_expiry_days` | `backup_full_seconds` | `backup_incremental_seconds` | `backup_wal_seconds` | `budget_monthly_usd` | `deploy_from_main_only` | `delete_secrets_on_destroy` | `delete_backups_on_destroy` |
|---|---|---|---|---|---|---|---|---|---|
| `295229566359` | `ikigenba.dev` | 30 | 604800 | 86400 | 900 | 75 | `true` | `false` | `false` |
| `602773793009` | `sbx.ikigenba.dev` | 7 | 0 | 0 | 0 | 50 | `false` | `true` | `true` |

A space has one identifier: its full domain — `foo.sbx.ikigenba.dev`, say, or
the account domain itself for the apex space. The tool creates a space with
`<domain> --account <account>`; the domain must end in the account's `domain`
property at `/ikigenba/account` (equal to it is the apex space), and there is
no separate name. The tool finds the zone by longest-suffix match of the
domain over the account's hosted zones.

The tool creates the instance profile `ikigenba-space-<domain>` at launch; its
role carries the `ikigenba-space-boundary` permissions boundary and an inline
policy rendered from `templates/space-role-policy.json`, which has four
literal placeholders the tool substitutes at create time (Terraform never
reads the file, and the file carries no comment of its own — IAM's policy
grammar allows only `Version`, `Id`, and `Statement`):

- `<domain>` — the space's one identifier, its full domain (`foo.sbx.ikigenba.dev`,
  say, or the account domain itself for the apex space); it must end in the
  `domain` property at `/ikigenba/account`. That one value is the SSM path
  segment (`/ikigenba/<domain>/*`), the bucket prefix (`<domain>/`), the role
  name suffix, and the two DNS names the space owns, `<domain>` and
  `*.<domain>`.
- `<zone_id>` — the hosted zone found by longest-suffix match of `<domain>`
  over the account's hosted zones.
- `<account_id>` — the account the space is created in.
- `<bucket>` — the account's backup bucket, the `backup_bucket` property at
  `/ikigenba/account`; the role's permissions boundary ARN is the
  `permissions_boundary_arn` property there.

The boundary is the ceiling: nothing granted in the inline policy can exceed
it. The inline policy narrows it to the space's own SSM path
`/ikigenba/<domain>/*` (read only), its own bucket prefix `<domain>/` (get,
put, list), and its own DNS names `<domain>` and `*.<domain>` in its one zone
(record types `A` and `TXT` only).

Spaces are registered by the `Space=<domain>` tag, the only registry tag.
There is no per-space Terraform. A space holds no Elastic IP: its public
address is the one the launch template assigns, and it is written into the
space's zone as the `<domain>` and `*.<domain>` A records with TTL 60.

A space's secrets are one SSM SecureString per app: `/ikigenba/<domain>/<app>`
holds a flat JSON object of that app's secrets. The values come from the
operator's machine at space create — the platform generates nothing — and the
host reads the entry through its instance role at app start. The host can
only read it; the operator-side tool is the only writer (see "Hosts write,
never delete").

Backups are the account's three periods, in seconds, with `0` meaning never:
`backup_full_seconds`, `backup_incremental_seconds`, and `backup_wal_seconds`
(`locals.tf`, published in `/ikigenba/account`; see the table above). Every
service in a space uses them, and the tool hands the three to the host at
create. `602773793009` never backs up — its data is seed data — and its
`delete_backups_on_destroy` and bucket expiry are unchanged by that.

The account's properties for the tool live at Parameter Store
`/ikigenba/account`, written only by Terraform (`account.tf`); no space's
path can collide with it, since every space's domain ends in the account
domain. Reserved domains, which a tool must refuse — the general rule is
that a domain is refused if the account's zone does not serve it:

- `sbx.<account domain>` exactly, and any domain ending in
  `.sbx.<account domain>`, when created in the account that delegates it away
  (in `295229566359`, `sbx.ikigenba.dev` is the NS delegation to
  `602773793009`, not a space; records under it in the `ikigenba.dev` zone
  would be shadowed by the delegation and never served).
- any `<app>.<existing space domain>` — apps answer at `<app>.<space domain>`,
  and a space there would shadow one.

Objects in the backup bucket expire after `backup_expiry_days` (`locals.tf`;
see the table above). Launch-template changes affect new launches only.

### Hosts write, never delete

No host role — space roles and the `dev` host alike — holds `s3:DeleteObject`
or `ssm:PutParameter`/`ssm:DeleteParameter`, and the `ikigenba-space-boundary`
does not grant them, so no inline policy can. Bucket expiry
(`backup_expiry_days`) is the only way a backup is deleted; the operator-side
tool is the only writer of secrets. Host DNS writes are limited to record
types `A` and `TXT` (boundary, space policy, and the space template's
`ChangeResourceRecordSets` condition), so no host can rewrite an NS
delegation. Accepted caveat: IAM cannot express "one label deep", so the apex
space's `*.<account domain>` reach covers sibling spaces' `A` records too; a
hosted zone is protected against destruction in Terraform
(`prevent_destroy`), as is each state bucket. The `dev` host is the one
exception to the DNS narrowing: it writes the whole `ikigenba.dev` zone, as
it always has.

## Credentials

Profiles `295229566359` and `602773793009` are under `sso-session metaspot` in
`~/.aws/config`. AWS CLI v2 is required (`sso-session` stanzas are v2 only).
The operator fills the `<placeholders>`:

```
[sso-session metaspot]
sso_start_url = <SSO start URL>
sso_region = <SSO region>
sso_registration_scopes = sso:account:access

[profile 295229566359]
sso_session = metaspot
sso_account_id = 295229566359
sso_role_name = <role name>
region = us-east-2

[profile 602773793009]
sso_session = metaspot
sso_account_id = 602773793009
sso_role_name = <role name>
region = us-east-2
```

Every Terraform command needs a live session — this is the precondition for
the gates:

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
`~/.ssh/id_ed25519_ikigenba_dev`. The key was generated by the operator with
`ssh-keygen -t ed25519`; only the public half is in the repo (`ssh.tf` in each
account root, and the legacy pair in `shared.tf`). The `Host ikigenba.dev dev` entry in
`~/.ssh/config` pins the Elastic IP and selects the key. The `dev_ssh` output
prints the exact command.
