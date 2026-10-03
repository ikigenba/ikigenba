# infra

infra is the Terraform for the one AWS account: root domain, hosted zone and substrate. Nothing per-space is here; devctl creates and destroys spaces against this substrate. The AWS organisation, the domain's registration and the `mgmt` account are managed from the standalone `metaspot` repository, not here. There is no `specs/` here and no requirement ids: none of the spec operations apply, and changes are made only on explicit, direct instruction from a human. An agent may `init`, `validate`, `fmt` and `plan` freely; it runs `terraform apply` only on explicit, direct instruction, and it never imports, moves or edits state. Every path below is relative to `infra/`.

## Layout

- `terraform.tfvars.json` is the root: `domain` and `region`, nothing else (see The root).
- `variables.tf` declares `domain`, `region` and the STS caller-identity lookup. `providers.tf` holds the backend and provider: S3 bucket `metaspot-dev-tfstate-295229566359`, key `295229566359/terraform.tfstate`, region `us-east-2`, `encrypt = true`, `use_lockfile = true`. The bucket and key predate the single-root layout and keep their names so the state never moves; the backend block is the whole configuration, so `terraform init` with no arguments is correct.
- `locals.tf` is the knobs: AMI, instance type, root volume, bucket expiry, admin SSH CIDR, budget amount and email.
- `dns.tf` is the hosted zone, `prevent_destroy`, delegated at the registrar from the mgmt account; it holds no records of its own. `network.tf` is the default-VPC lookup and the space security group: 80 and 443 from anywhere, 22 from the admin CIDR. `ssh.tf` is the key pair, public half only; the private key is the operator's (`~/.ssh/id_ed25519_ikigenba_dev`).
- `launch.tf` is the launch template every space is launched from, with no instance profile: devctl passes the per-space profile at launch. `templates/space-first-boot.sh` is its user data: packages only (nginx, certbot, awscli-2, jq and git from the distribution, litestream from its GitHub release RPM pinned by version and sha256 in the script); the host learns nothing about itself here, and changes affect new launches only.
- `iam.tf` is the permissions boundary (see The boundary). `backups.tf` is the backup bucket with its access block, ownership, encryption, transport policy and one lifecycle rule (`backup_expiry_days`). `budget.tf` is the monthly cost budget; it only notifies. `outputs.tf` is the zone's name servers, for the registrar; devctl reads no outputs.
- `bootstrap/` is the state-backend root; it calls `bootstrap/modules/state-backend/` to create the tfstate bucket, with the same default tags plus `Component = "bootstrap"`, and takes the root's two values by `-var-file=../terraform.tfvars.json`, since Terraform loads a tfvars file only from its own root. Its state is local and committed (`terraform.tfstate` beside its `.tf` files), because it is the only record that Terraform owns the state bucket; it holds nothing secret. A fresh clone must plan `No changes` here; if `plan` proposes creating the bucket, the state file is missing: stop and restore it rather than apply.
- `.terraform/` directories and provider caches are untracked; `init` recreates them. `.terraform.lock.hcl` files are tracked.

## The root

`terraform.tfvars.json` is the single statement of the root. Terraform loads it natively from this directory, and devctl walks up from its working directory to the same file and takes both values from it. It is the one committed var file (`.gitignore` exempts it); changing the domain there renames every resource below.

The AWS profile is named after the domain (`profile = var.domain`); no profile name is checked in. A backend block takes no variables, so every Terraform command runs with `AWS_PROFILE` set to the domain, and the backend's region literal duplicates the tfvars value for the same reason.

Every resource is named after the domain, so devctl needs no configuration to find them: the hosted zone, launch template, permissions boundary, key pair, backup bucket and budget are all `<domain>`, and the security group is `<domain>-<suffix>`, a prefix, because it is replaced create-before-destroy. Everything carries the default tags `Domain = <domain>` and `ManagedBy = "terraform"`. The account id appears nowhere in the configuration; the boundary reads it from STS.

## Spaces and apps

A space is one label under the root (`sbx1.<domain>`): one EC2 instance launched from the launch template by devctl, never by Terraform. An app is one label under a space (`foo.sbx1.<domain>`). There is no nesting, no grouping and no apex space: the apex `<domain>` is an A record devctl points at one app on one space (`devctl apex set`), and `*.<domain>` is never written.

Every space holds one Elastic IP, allocated by devctl at create and released at destroy, which devctl writes as the `<space>` and `*.<space>` A records. The launch template still assigns a transient public address, because first boot fetches its packages before the Elastic IP is associated. Elastic IPs are an account quota (`EC2-VPC Elastic IPs`, `L-0263D0A3`), five per region by default; that is the space cap, devctl fails create at its address step when it is exhausted, and the quota is not Terraform's to manage. Spaces are registered by the `Space=<space domain>` tag on the instance, its volumes and its Elastic IP; it is the only registry tag.

A space's secrets are one SSM SecureString per app at `/<space domain>/<app>`, a flat JSON object: devctl writes them from the operator's machine, and the host reads them through its instance role. Backups go to the one bucket under `<space label>/<app>/...`; the bucket name has dots, so clients address it path-style. Backup periods are opsctl configuration that devctl sets at create from its own defaults; nothing here holds them. Objects expire after `backup_expiry_days`.

devctl creates each space's instance profile and role at launch. The role carries the boundary and an inline policy devctl renders, which narrows the boundary to the space's own SSM path `/<space domain>/*`, its own bucket prefix `<space label>/` and its own DNS names in the zone. When the space holds the apex, devctl regenerates that policy to also allow the TXT record at `_acme-challenge.<domain>`.

### The boundary

`iam.tf` is the ceiling: nothing granted in a space's inline policy can exceed it.

- SSM read (`GetParameter`, `GetParameters`, `GetParametersByPath`) on `parameter/*.<domain>/*`: every space's every app, nothing else.
- KMS `Decrypt` and `GenerateDataKey` via SSM in the region.
- S3 get, put and list on the backup bucket.
- Route 53 `ChangeResourceRecordSets` on the one zone, record types `A` and `TXT` only, plus `ListResourceRecordSets` and `GetChange`.

### Hosts write, never delete

No space role holds `s3:DeleteObject` or `ssm:PutParameter`/`DeleteParameter`, and the boundary does not grant them, so no inline policy can. Bucket expiry is the only way a backup is deleted from the host's side, which is why litestream runs with its own retention off. devctl is the only writer of secrets, and `space destroy --delete-secrets` / `--delete-backups` are the only deletes. Host DNS writes are `A` and `TXT` only, so no host can write an NS record. Accepted caveat: IAM cannot express "one label deep", so a space's `*.<space>` reach is what the per-space policy states; the zone and the state bucket are protected against destruction in Terraform (`prevent_destroy`).

## Toolchain

- Terraform 1.10 or later (`required_version`), with the `hashicorp/aws` provider at `~> 5.70`; `.terraform.lock.hcl` pins it.
- AWS CLI v2, for the SSO session (`sso-session` stanzas are v2 only).
- Prefer what Terraform and the AWS provider offer; adding a provider or module source needs human approval.

## Operator setup

The profile is named after the domain, under `sso-session metaspot` in `~/.aws/config`. The operator fills the `<placeholders>`:

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

Every Terraform command needs a live session and the profile selected:

```
aws sso login --sso-session metaspot
export AWS_PROFILE=ikigenba.dev
```

## Gates

There are no gates.
