# infra

The Terraform for the one AWS account this repository manages: one root
domain, one hosted zone, and the substrate every space is built on. Nothing
per-space is here; devctl creates and destroys spaces against this substrate.

**This module is not spec-governed.** There is no `specs/` here, no requirement
ids, and none of the spec operations apply. It is hand-maintained: changes are
made only on direct instruction. An agent may `init`, `validate`, `fmt`, and
`plan` freely; **it runs `terraform apply` only when directly instructed to**,
and it never imports, moves, or edits state.

Nothing about the AWS organisation, the registration of the domain, the
`mgmt` account, or any other account or domain is managed or changed from
here; those live elsewhere (the standalone `metaspot` repository) and are out
of scope.

Every path below is relative to this directory (`infra/`).

## The root

`terraform.tfvars.json` is the single statement of the root: it holds
`domain` and `region` and nothing else. Terraform loads it natively from this
directory; devctl walks up from its working directory to find the same file
and takes both values from it. It is the one committed var file (`.gitignore`
exempts it). Changing the domain there renames every resource below.

The AWS profile is named after the domain: `profile = var.domain`. No profile
name is checked in. The backend block cannot take variables, so every
Terraform command runs with `AWS_PROFILE` set to the domain, and the backend's
region literal duplicates the tfvars value for the same reason.

Every resource is named after the domain, so devctl needs no configuration to
find them: the hosted zone, the launch template, the permissions boundary, the
key pair, the backup bucket, and the budget are all `<domain>`; the security
group is `<domain>-<suffix>` (a prefix, because it is replaced
create-before-destroy). Default tags on everything: `Domain = <domain>`,
`ManagedBy = "terraform"`. The account id appears nowhere in the
configuration; the boundary reads it from STS.

## Layout

- `terraform.tfvars.json` — the root (above).
- `variables.tf` — `domain`, `region`, and the STS caller-identity lookup.
- `providers.tf` — the backend and provider. Backend: S3 bucket
  `metaspot-dev-tfstate-295229566359`, key `295229566359/terraform.tfstate`,
  region `us-east-2`, `use_lockfile = true`. The bucket and key predate the
  single-root layout and keep their names so the state never moves. The
  backend block is the whole configuration; `terraform init` with no
  arguments is correct.
- `locals.tf` — the knobs: AMI, instance type, root volume, bucket expiry,
  admin SSH CIDR, budget amount and email.
- `dns.tf` — the hosted zone, `prevent_destroy`, delegated at the registrar
  from the mgmt account. It holds no records of its own.
- `network.tf` — the default-VPC lookup and the space security group: 80 and
  443 from anywhere, 22 from the admin CIDR.
- `ssh.tf` — the key pair; only the public half. The private key is the
  operator's (`~/.ssh/id_ed25519_ikigenba_dev`).
- `launch.tf` — the launch template every space is launched from. No instance
  profile: devctl passes the per-space profile at launch.
- `iam.tf` — the permissions boundary (below).
- `backups.tf` — the backup bucket, its access block, ownership, encryption,
  transport policy, and the one lifecycle rule (`backup_expiry_days`).
- `budget.tf` — the monthly cost budget; it only notifies and changes nothing.
- `outputs.tf` — the zone's name servers, for the registrar. devctl reads no
  outputs.
- `templates/space-first-boot.sh` — the launch template's user data. Packages
  only: nginx, certbot, awscli-2, and jq from the distribution, and litestream
  from its GitHub release RPM, pinned by version and sha256 in the script. The
  host learns nothing about itself here. Changes affect new launches only.
- `bootstrap/` — the state-backend root; calls `bootstrap/modules/state-backend/`
  to create the tfstate bucket above. Same default tags plus
  `Component = "bootstrap"`. It takes the root's two values by
  `-var-file=../terraform.tfvars.json`, since Terraform loads a tfvars file
  only from its own root. **Local state**: `terraform.tfstate` sits beside
  its `.tf` files and is committed, because it is the only record that
  Terraform owns the state bucket. A fresh clone must plan `No changes` here;
  if `plan` proposes creating the bucket, the state file is missing — stop
  and restore it rather than apply. The state holds only the bucket and its
  configuration resources, nothing secret.

`.terraform/` directories and provider caches are not tracked; `init` recreates
them. `.terraform.lock.hcl` files are tracked.

## Spaces and apps

A space is one label under the root (`sbx1.<domain>`), one EC2 instance
launched from the launch template by devctl, never by Terraform. An app is one
label under a space (`foo.sbx1.<domain>`). There is no nesting, no grouping,
and no apex space: the apex `<domain>` is an A record devctl points at one
app on one space (`devctl apex set`), and `*.<domain>` is never written.

Every space holds one Elastic IP, allocated by devctl at create and released
at destroy; devctl writes it as the `<space>` and `*.<space>` A records. The
launch template still assigns a transient public address, because first boot
fetches its packages before the Elastic IP is associated. Elastic IPs are an
account quota (`EC2-VPC Elastic IPs`, `L-0263D0A3`), five per region by
default; that is the space cap, and devctl fails create at its address step
when it is exhausted. The quota is not Terraform's to manage.

Spaces are registered by the `Space=<space domain>` tag on the instance, its
volumes, and its Elastic IP; it is the only registry tag.

A space's secrets are one SSM SecureString per app at `/<space domain>/<app>`,
a flat JSON object. devctl writes them from the operator's machine; the host
reads them through its instance role. Backups go to the one bucket under
`<space label>/<app>/...`; the bucket name has dots, so clients address it
path-style. Backup periods are opsctl configuration devctl sets at create from
its own defaults; nothing here holds them. Objects expire after
`backup_expiry_days`.

devctl creates the instance profile and role for each space at launch. The
role carries the boundary and an inline policy devctl renders itself, which
narrows the boundary to the space's own SSM path `/<space domain>/*`, its own
bucket prefix `<space label>/`, and its own DNS names in the zone. When the
space holds the apex, devctl regenerates that policy to also allow the TXT
record at `_acme-challenge.<domain>`.

### The boundary

`iam.tf` is the ceiling: nothing granted in a space's inline policy can exceed
it.

- SSM read (`GetParameter`, `GetParameters`, `GetParametersByPath`) on
  `parameter/*.<domain>/*`: every space's every app, nothing else.
- KMS decrypt via SSM in the region.
- S3 get, put, and list on the backup bucket.
- Route 53 `ChangeResourceRecordSets` on the one zone, record types `A` and
  `TXT` only, plus `ListResourceRecordSets` and `GetChange`.

### Hosts write, never delete

No space role holds `s3:DeleteObject` or `ssm:PutParameter`/`DeleteParameter`,
and the boundary does not grant them, so no inline policy can. Bucket expiry
is the only way a backup is deleted from the host's side; litestream runs
with its own retention off for that reason. devctl is the only writer of
secrets, and `space destroy --delete-secrets` / `--delete-backups` are the
only deletes. Host DNS writes are `A` and `TXT` only, so no host can write an
NS record. Accepted caveat: IAM cannot express "one label deep", so a space's
`*.<space>` reach is what the per-space policy states, and the zone is
protected against destruction in Terraform (`prevent_destroy`), as is the
state bucket.

## Credentials

The profile is named after the domain, under `sso-session metaspot` in
`~/.aws/config`. AWS CLI v2 is required (`sso-session` stanzas are v2 only).
The operator fills the `<placeholders>`:

```
[sso-session metaspot]
sso_start_url = <SSO start URL>
sso_region = <SSO region>
sso_registration_scopes = sso:account:access

[profile ikigenba.dev]
sso_session = metaspot
sso_account_id = <account id>
sso_role_name = <role name>
region = us-east-2
```

Every Terraform command needs a live session and the profile selected. This
is the precondition for the gates:

```
aws sso login --sso-session metaspot
export AWS_PROFILE=ikigenba.dev
```

## Gates

There is no build run here; these are the operator's checks. Run them after
every change. Each must exit 0, and each `plan` must report `No changes` unless
the change was meant to produce one.

1. `terraform fmt -check -recursive` — from `infra/`.
2. In `infra/`:
   `terraform init -input=false`, then
   `terraform validate`, then
   `terraform plan -input=false -detailed-exitcode` (exit 0 is `No changes`;
   exit 2 is a pending change to read before anyone applies it).
3. In `infra/bootstrap/`: the same three commands, with
   `-var-file=../terraform.tfvars.json` on `plan`.

Terraform 1.10+ is required (`required_version`); the AWS provider is pinned
`~> 5.70` and locked by `.terraform.lock.hcl`.
