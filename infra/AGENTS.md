# infra

infra is the Terraform for the one AWS account: root domain, hosted zone and substrate. Nothing per-space is here; devctl creates and destroys spaces against this substrate. The AWS organisation, the domain's registration and the `mgmt` account are managed from the standalone `metaspot` repository, not here. There is no `specs/` here and no requirement ids: none of the spec operations apply, and changes are made only on explicit, direct instruction from a human. An agent may `init`, `validate`, `fmt` and `plan` freely; it runs `terraform apply` only on explicit, direct instruction, and it never imports, moves or edits state. Every path below is relative to `infra/`.

## Layout

- `terraform.tfvars.json` is the root: `domain` and `region`, nothing else (see The root).
- `variables.tf` declares `domain`, `region` and the STS caller-identity lookup. `providers.tf` holds the backend and provider: S3 bucket `metaspot-dev-tfstate-295229566359`, key `295229566359/terraform.tfstate`, region `us-east-2`, `encrypt = true`, `use_lockfile = true`. The bucket and key predate the single-root layout and keep their names so the state never moves; the backend block is the whole configuration, so `terraform init` with no arguments is correct.
- `locals.tf` is the knobs: AMI, instance type, root volume, bucket expiry, budget amount and email, and the hosts domain with a host's default type and root volume.
- `dns.tf` is the root hosted zone, `prevent_destroy`, delegated at the registrar from the mgmt account; it holds no records of its own. It also holds the hosts zone (`metaspot.org`, see SSH hosts), likewise `prevent_destroy` and delegated from mgmt, with the records that moved with it from mgmt: apex A, MX and TXT, and the `dnd` and `eastmark` A records, the A records pointing at `us`'s Elastic IP as a literal. `network.tf` is the default-VPC lookup and the space security group: 80, 443 and 22 from anywhere; on the host, fail2ban and the sshd rules opsctl owns do the limiting. It also holds the hosts' security group: 22 from anywhere and nothing else. `ssh.tf` is the key pair, public half only; the private key is the operator's (`~/.ssh/id_ed25519_ikigenba_dev`).
- `launch.tf` is the launch template every space is launched from (and the hosts' template, see SSH hosts), with no instance profile: devctl passes the per-space profile at launch. `templates/space-first-boot.sh` is its user data: packages only (nginx, certbot, awscli-2, jq, git and python3.12 from the distribution, litestream from its GitHub release RPM pinned by version and sha256 in the script); the host learns nothing about itself here, and changes affect new launches only.
- `iam.tf` is the permissions boundary (see The boundary), and the hosts' role, instance profile and operator policy (see SSH hosts). `backups.tf` is the backup bucket with its access block, ownership, encryption, transport policy and one lifecycle rule (`backup_expiry_days`). `budget.tf` is the monthly cost budget; it only notifies. `outputs.tf` is each zone's name servers, for the registrar; devctl reads no outputs.
- `bootstrap/` is the state-backend root; it calls `bootstrap/modules/state-backend/` to create the tfstate bucket, with the same default tags plus `Component = "bootstrap"`, and takes the root's two values by `-var-file=../terraform.tfvars.json`, since Terraform loads a tfvars file only from its own root. Its state is local and committed (`terraform.tfstate` beside its `.tf` files), because it is the only record that Terraform owns the state bucket; it holds nothing secret. A fresh clone must plan `No changes` here; if `plan` proposes creating the bucket, the state file is missing: stop and restore it rather than apply.
- `.terraform/` directories and provider caches are untracked; `init` recreates them. `.terraform.lock.hcl` files are tracked.

## The root

`terraform.tfvars.json` is the single statement of the root. Terraform loads it natively from this directory, and devctl walks up from its working directory to the same file and takes both values from it. It is the one committed var file (`.gitignore` exempts it); changing the domain there renames every resource below.

The AWS profile is named after the domain (`profile = var.domain`); no profile name is checked in. A backend block takes no variables, so every Terraform command runs with `AWS_PROFILE` set to the domain, and the backend's region literal duplicates the tfvars value for the same reason.

Every resource is named after the domain, so devctl needs no configuration to find them: the hosted zone, launch template, permissions boundary, key pair, backup bucket and budget are all `<domain>`, and the security group is `<domain>-<suffix>`, a prefix, because it is replaced create-before-destroy. The SSH hosts' resources are `<domain>-hosts` (launch template, role, instance profile), `<domain>-hosts-<suffix>` (security group) and `<domain>-hosts-operator` (policy); only the hosts zone is named for its own domain. Everything carries the default tags `Domain = <domain>` and `ManagedBy = "terraform"`. The account id appears nowhere in the configuration; the boundary reads it from STS.

## Spaces and apps

A space is one label under the root (`sbx1.<domain>`): one EC2 instance launched from the launch template by devctl, never by Terraform. An app is one label under a space (`foo.sbx1.<domain>`). There is no nesting, no grouping and no apex space: the apex `<domain>` is an A record devctl points at one app on one space (`devctl apex set`), and `*.<domain>` is never written.

Every space holds one Elastic IP, allocated by devctl at create and released at destroy, which devctl writes as the `<space>` and `*.<space>` A records. The launch template still assigns a transient public address, because first boot fetches its packages before the Elastic IP is associated. Elastic IPs are an account quota (`EC2-VPC Elastic IPs`, `L-0263D0A3`), five per region by default; that is the space cap, devctl fails create at its address step when it is exhausted, and the quota is not Terraform's to manage. Spaces are registered by the `Space=<space domain>` tag on the instance, its volumes and its Elastic IP; it is the only registry tag.

A space's secrets are one SSM SecureString per app at `/<space domain>/<app>`, a flat JSON object: devctl writes them from the operator's machine, and the host reads them through its instance role. Backups go to the one bucket under `<space label>/<app>/...`; the bucket name has dots, so clients address it path-style. Backup periods are opsctl configuration that devctl sets at create from its own defaults; nothing here holds them. Objects expire after `backup_expiry_days`.

devctl creates each space's instance profile and role at launch. The role carries the boundary and an inline policy devctl renders, which narrows the boundary to the space's own SSM path `/<space domain>/*`, its own bucket prefix `<space label>/` and its own DNS names in the zone. When the space holds the apex, devctl regenerates that policy to also allow the TXT record at `_acme-challenge.<domain>`.

### The boundary

`iam.tf` is the ceiling: nothing granted in a space's inline policy can exceed it.

- SSM read (`GetParameter`, `GetParameters`, `GetParametersByPath`) on `parameter/*.<domain>/*`: every space's every app, nothing else.
- KMS `Decrypt` and `GenerateDataKey` via SSM in the region.
- S3 get, put, delete and list on the backup bucket.
- Route 53 `ChangeResourceRecordSets` on the one zone, record types `A` and `TXT` only, plus `ListResourceRecordSets` and `GetChange`.
- The statements of `<domain>-hosts-operator`, verbatim, so the one space it is attached to can use them; no space's inline policy grants them.
- Session Manager: `ssm:UpdateInstanceInformation` on the account's instances in the region, and the `ssmmessages` and `ec2messages` channel actions, which take no resource. These are the session actions of AWS's `AmazonSSMManagedInstanceCore`, without its broader `ssm` reads, so the SSM agent on a host can register and carry a session and an operator signed in through SSO reaches the host without sshd. A space gets them only when its inline policy grants them too.

### What hosts may delete

The boundary grants `s3:DeleteObject` on the backup bucket so that litestream's own retention can prune a space's replicas; each space's inline policy narrows it to the space's own prefix. Bucket expiry stays as the backstop for anything retention leaves behind. No space role holds `ssm:PutParameter`/`DeleteParameter`, and the boundary does not grant them, so no inline policy can. devctl is the only writer of secrets, and `space destroy --delete-secrets` / `--delete-backups` are the only deletes beyond a host's own pruning. Host DNS writes are `A` and `TXT` only, so no host can write an NS record. Accepted caveat: IAM cannot express "one label deep", so a space's `*.<space>` reach is what the per-space policy states; the zone and the state bucket are protected against destruction in Terraform (`prevent_destroy`).

## SSH hosts

accounts on the one hosts-operator space (`devctl hosts set`) runs EC2 hosts for SSH users in the hosts zone; Terraform holds only their static pieces.

- The launch template `<domain>-hosts`: the space's image, `t4g.medium`, a 20 GB gp3 root, IMDSv2 required with instance tags readable, credits unlimited, the hosts' security group, a transient public address, no key pair and no instance profile: accounts passes the hosts' profile and every tag at launch, and creates each host's data volume apart from the instance. Its user data, `templates/hosts-first-boot.sh`, reads the instance tag `AccountsURL` from IMDS, downloads `<AccountsURL>/accountsd` to `/usr/local/bin/accountsd`, retrying both until they succeed, and enables `accountsd.service` with `ACCOUNTS_URL` in `/etc/accountsd.env`; the daemon does the rest.
- The role and instance profile `<domain>-hosts`: Session Manager only, the same statements as the boundary's, no boundary of its own. The daemon proves who it is with a presigned `sts:GetCallerIdentity`, which needs no permission.
- `<domain>-hosts-operator`, attached by devctl to the operator space's role. Every write is scoped by the `Kind` tag accounts sets in the creating call: `Kind=host` on an instance and the root volume and network interface it launches with, and on an Elastic IP; `Kind=host-data` on a data volume. It launches only from the hosts' template, with the hosts' security group, an Amazon image and a `t4g`, `m7g`, `c7g` or `r7g` type, and a resize stays in those families; it passes only the hosts' role; it stops, starts, terminates, modifies, attaches, detaches, associates and releases only what carries a hosts `Kind`, and deletes no volume; it adds later tags only to what already carries one, never `Kind`, `Space` or `Domain`, and never tags anything `Space`, devctl's registry tag. In the hosts zone it changes only `A` records below the apex, never `dnd` or `eastmark`.

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
