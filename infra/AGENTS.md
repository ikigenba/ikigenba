# infra

The Terraform for the Ikigenba AWS footprint: the organisation, its member
accounts, the registered-domain portfolio and its hosted zones, the per-account
state buckets, and the one host in each environment account. It came into this
monorepo from the standalone `metaspot` repository with its history intact;
resource names, tags (`Project = "metaspot"`), bucket names, and backends are
unchanged, and renaming any of them would churn live resources for no gain.

**This module is not spec-governed.** There is no `specs/` here, no requirement
ids, and none of the four spec operations (`draft-spec`, `check-spec`,
`build-spec`, `audit-spec`) apply. Like `opsctl/bootstrap.md`, it is
hand-maintained: changes are made only on direct instruction, and `terraform
apply` is always run by a human. An agent may `init`, `validate`, `fmt`, and
`plan`; it never applies, imports, moves, or edits state.

Every path below is relative to this directory (`infra/`).

## Topology

One directory per AWS account; profile name = directory name = the
environment it owns. Region `us-east-2` everywhere.

- `mgmt/` — `mgmt` account `132801647717`, the AWS Organizations management
  account. Owns the member accounts (`accounts.tf`), the IAM Identity Center
  assignment that grants `AdministratorAccess` to the `Administrators` group on
  each admin member, and every registered domain and its hosted zone:
  `metaspot.org`, `metaspot.net`, `ikigenba.com`, `ikigenba.dev`
  (registration only, see below), `michaelgreenly.com`, `michaelgreenly.dev`,
  `logic-refinery.{com,io,net,tv}`, `ikigai-group.io`. Parked apexes point at
  the `int` box (`parked.tf`); `*.metaspot.org` also points there
  (`metaspot-org.tf`), and `metaspot-org-dns01.tf` is the cross-account role
  the `int` box assumes for that zone's DNS-01 challenges. NS delegation of
  `int.ikigenba.com` lives in `ikigenba.tf`. No servers.
- `dev/` — `dev` account `295229566359`. Owns the authoritative `ikigenba.dev`
  zone (delegated at the registrar from `mgmt`), one host answering at
  `ikigenba.dev` and `*.ikigenba.dev`, its instance role, and the backup
  bucket `ikigenba-dev-295229566359`.
- `int/` — `int` account `704229156466`. Owns the `int.ikigenba.com` zone
  (delegated from `ikigenba.com` in `mgmt`), one host answering at
  `int.ikigenba.com` and `*.int.ikigenba.com`, its instance role, and the
  backup bucket `int-ikigenba-com-704229156466`.
- `bootstrap/{mgmt,dev,int}/` — per-account state-backend roots, each calling
  `bootstrap/modules/state-backend/` to create that account's tfstate bucket.
  **Local state**, see below.
- `templates/` — `ikigenba-env.sh.tftpl` (first-boot user data) and
  `ikigenba-launch` (the platform launcher it installs).
- `docs/` — the path-routing and connector architecture documents the service
  layer refers to.

The `prod`, `test`, `sandbox`, and `ai` accounts that earlier versions of this
document listed were closed. Their hosted zones, delegations, and roots are
gone; only the empty `bootstrap/{prod,test,sandbox}/` directories remain
(each holding a stale, gitignored `terraform.tfstate`), and cleaning them up is
a separate concern. The `mgmt/ikigenba.tf` `ikigenba.dev` zone is transitional:
it answers identically to the `dev` zone while the old registrar delegation
ages out of caches, and is removed after that soak.

`TODO.md` and `CASEFILE.md` are operator notes carried over unchanged.

**Cross-account values are hardcoded literals.** NS delegation records, the
parked-apex EIP, the `int` instance-role ARN in `metaspot-org-dns01.tf`, and
every AMI are copied in by hand from the other root's outputs or the console.
This module never wires a `terraform_remote_state` cross-account read.

## Credentials and state

One `sso-session metaspot` in `~/.aws/config` covers the `mgmt`, `dev`, and
`int` profiles. Every Terraform command needs a live session:

```
aws sso login --sso-session metaspot
```

`mgmt/`, `dev/`, and `int/` keep state in S3, bucket
`metaspot-<name>-tfstate-<accountid>`, key `<name>/terraform.tfstate`, with
`use_lockfile = true` (no DynamoDB table). The backend block in each root's
`providers.tf` is the whole configuration; `terraform init` with no arguments
is correct, and `-migrate-state` and `-reconfigure` are never needed.

The three `bootstrap/<name>/` roots have no backend: their state is a
`terraform.tfstate` beside their `.tf` files, gitignored, and copied by hand
when this module moves between checkouts. A missing bootstrap state file means
`plan` will propose creating a bucket that already exists; stop and copy the
file rather than apply.

`.terraform/` directories and provider caches are not tracked; `init` recreates
them. `.terraform.lock.hcl` files are tracked.

## Gates

There is no build run here, so these are the operator's checks. Run them
after every change and before every apply; each must exit 0 and each `plan`
must report `No changes` unless the change was meant to produce one. The SSO
login above is the precondition.

1. `terraform fmt -check -recursive` — from `infra/`.
2. In each of `mgmt/`, `dev/`, `int/`, `bootstrap/mgmt/`, `bootstrap/dev/`,
   `bootstrap/int/`:
   `terraform init -input=false`, then `terraform validate`, then
   `terraform plan -input=false -detailed-exitcode` (exit 0 is `No changes`;
   exit 2 is a pending change to read before anyone applies it).

Terraform 1.10+ is required (`required_version`); the AWS provider is pinned
`~> 5.70` per root and locked by `.terraform.lock.hcl`.

## Adding an account

Follows the pattern `dev/` and `int/` use: a new entry in `mgmt/accounts.tf`
(creates the account and the Identity Center assignment), a new
`bootstrap/<name>/` (apply it with local state first, then keep its state
file), a new `<name>/` root with its own zone and backend, and an NS
delegation in `mgmt/ikigenba.tf` (for `<name>.ikigenba.com`) or
`mgmt/delegations.tf` (for `<name>.metaspot.org`). Account email:
`mgreenly+<name>@gmail.com`. Bucket names: tfstate
`metaspot-<name>-tfstate-<accountid>`; backups named after the zone with
dashes for dots plus the account id.

## SSH access

Both hosts are Amazon Linux 2023, user `ec2-user`, package manager `dnf`
(AL2023 pins a release; moving releases needs an explicit `--releasever=`).
One key pair per account, declared in that root's `shared.tf`:

- `dev`: `~/.ssh/id_ed25519_ikigenba_dev`, key pair `ikigenba_dev`. The
  `Host ikigenba.dev dev` entry in `~/.ssh/config` pins the Elastic IP and
  selects the key.
- `int`: `~/.ssh/id_ed25519_int_ikigenba_com`, key pair `int_ikigenba_com`.

Each root's `<name>_ssh` output prints the exact command.

## Creating a server

"Create a server" means: in the target account root, produce `<name>.tf` per
this standard. **One server per account.** The box is named after the account
and answers on the zone apex from `shared.tf` (`aws_route53_zone.env`).
Multiple services run on the box and nginx routes them by **path** under that
one apex host (see Service layer). Do not ask how to build it; only the two
questions below.

- Own security group: ingress **80 and 443 from `0.0.0.0/0`** (port 80 carries
  the Let's Encrypt HTTP-01 challenge and the 80→443 redirect) and **22 from
  the admin IP only** (`208.118.151.172/32`, hardcoded per SG; update every
  occurrence when it changes). All egress. `name_prefix` plus
  `lifecycle { create_before_destroy = true }` so a rule change replaces the
  SG without a `DependencyViolation` hang.
- `aws_instance`: AL2023, `ami` pinned literally (resolve once via SSM
  `/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64`,
  then hardcode with the date). Reuse `data.aws_vpc.default`,
  `data.aws_subnets.default`, the account's `aws_key_pair`, and
  `aws_route53_zone.env` from `shared.tf`. Explicit
  `root_block_device { volume_type = "gp3", volume_size = <N> }`.
  `user_data` rendered from `templates/ikigenba-env.sh.tftpl` with
  `launcher_source = file("${path.module}/../templates/ikigenba-launch")`
  (see Node identity), `user_data_replace_on_change = false`, and
  `lifecycle { ignore_changes = [ami, user_data] }`.
- `aws_eip`, and A records for the apex and `*.<apex>` in
  `aws_route53_zone.env` pointing at it.
- `aws_iam_role` plus `aws_iam_instance_profile` (**always**), attached via
  `iam_instance_profile`, carrying three inline policies: bucket-wide
  `backups-rw` (see Backups), `dns-rw` on the account's own zone (record
  changes and `route53:GetChange`), and the env-wide `app-config` SSM grant
  (see Secrets). All unconditional; the launcher needs the SSM grant to run.
- `<name>_public_ip` and `<name>_ssh` outputs in `outputs.tf`, beside
  `hosted_zone_id` and `hosted_zone_name_servers`.

### The only two questions (use the defaults; ask nothing else)

1. **Instance size?** — default `t3.micro` (both current hosts are `t3.small`).
2. **Root volume size?** — default 10 GiB gp3.

### Node identity and platform launcher (first-boot baseline)

Every server's `user_data` is rendered from `templates/ikigenba-env.sh.tftpl`
and does three things at first boot:

1. **Writes `/etc/ikigenba/env`**, a flat `KEY=value` file (no `export`),
   `source`-able from bash and valid as a systemd `EnvironmentFile=`.
   Non-secret identity and topology only: `IKIGENBA_ENV`, `IKIGENBA_NODE`,
   `IKIGENBA_FQDN`, `IKIGENBA_DOMAIN`, `IKIGENBA_ROOT=/opt`,
   `IKIGENBA_DNS_ZONE`, `IKIGENBA_AWS_ACCOUNT_ID`, `IKIGENBA_AWS_REGION`,
   `IKIGENBA_BACKUP_BUCKET`.
2. **Installs `/usr/local/bin/ikigenba-launch`** (mode `0755`, root:root) from
   `templates/ikigenba-launch`. The launcher is platform, not application:
   every app's systemd unit `ExecStart`s it, and no app's setup touches
   `/usr/local/bin/`.
3. **Installs the launcher's runtime deps**: `dnf install -y awscli-2 jq`
   (the package is `awscli-2`, not `aws-cli`). The `dev` and `int` roots
   append `dnf install -y -q nginx certbot` and `systemctl enable nginx`.

No secrets in user data (it is world-readable via IMDS) and no public IP in it
(a dependency cycle, and IMDS has it at runtime).

`user_data` is the **first-boot baseline only**: cloud-init does not re-run it
on reboot, and `ignore_changes = [user_data]` keeps a template change from
force-replacing a live box, the same discipline as the AMI pin. A template
change therefore does **not** update running boxes; bring them into line by
writing `/etc/ikigenba/env` or `/usr/local/bin/ikigenba-launch` out of band.

### Backups

One bucket per account, `aws_s3_bucket.backups` in `shared.tf`, named after
the zone (`ikigenba-dev-295229566359`, `int-ikigenba-com-704229156466`):
Block Public Access on, `BucketOwnerEnforced`, default SSE-S3 (AES256), a
bucket policy denying non-TLS transport, no versioning.

The instance role's `backups-rw` policy grants `s3:GetObject`, `PutObject`,
`DeleteObject`, and `ListBucket` on the whole bucket. Per-app prefix isolation
is **not IAM-enforced**; apps write under `<app>/` by convention, and an app
that backs up shared state (the whole `/etc/letsencrypt` tree, say) captures
it under its own prefix so each tarball restores on its own.

### Secrets (Parameter Store)

**One parameter per app**, hostname-independent:
`/ikigenba/<env>/app-config/<app>` (`SecureString`), a flat JSON object
`{ "KEY": "value", … }`. A secret-less app holds an explicit `{}`; "no
secrets" is a seeded state, not an absence. **No Terraform parameter
resources**: existence is script-managed (the app's push tool creates on first
`put-parameter --overwrite`), so adding an app needs no apply.

The instance role's `app-config` policy grants `ssm:GetParameter` and
`ssm:PutParameter` on `…parameter/ikigenba/<env>/app-config/*`, plus
`kms:Decrypt` and `kms:GenerateDataKey` scoped by
`kms:ViaService = ssm.us-east-2.amazonaws.com`. All apps on a box share the
one role, so isolation between their parameters is by path convention, the
same posture as the backup prefixes.

### Service layer (the app on the box)

Per-app, not per-box: a server hosts several independently deployed apps with
names unique on that box. **Path routing is the model**: services are REST and
MCP APIs with no UI, bound to loopback, mounted under paths on the one apex
host; the **dashboard** is the apex/default app and the suite's OAuth
authorization server. `docs/path-routing-architecture.md` (topology and auth
contract) and `docs/connector-and-install.md` (client-side connector and
install) are authoritative; this is the platform summary.

- **Layout.** Install root `/opt/<app>/`, owned by a dedicated system user
  `<app>` (`useradd --system --home-dir /opt/<app> --shell /usr/sbin/nologin`).
- **Entrypoint.** Every app exposes one executable at `/opt/<app>/bin/run`
  regardless of runtime, so the systemd unit is identical across apps.
- **Manifest.** `etc/manifest.env`, flat `KEY=value`: `PORT` (the loopback
  port), `MOUNT` (the path prefix under the apex host), `DEFAULT` (`true` for
  the one app that also answers on the bare apex; two defaults is an nginx
  conflict by design).
- **Workstation routing.** `etc/deploy.env`, committed but never shipped:
  `ACCOUNT`, `SSH_USER`, `SSH_KEY`, `CERTBOT_EMAIL`. Scripts derive the domain
  and host from `ACCOUNT`.
- **Seven scripts** in the app repo, each ssh-ing into the box itself:
  `bin/build` (the one per-app script, producing `build/${APP}`),
  `bin/setup` (idempotent box prep as root: runtime deps, user, dirs, unit,
  `systemctl enable`; the dashboard's setup also owns the apex `server`
  block, the apex cert and renewal timer, the ACME location, the `/_authn`
  location, and the `include` of the locations dir; a service's setup only
  writes `/etc/nginx/conf.d/locations/<app>.conf` and reloads nginx),
  `bin/deploy` (build, stop, rsync to `/opt/${APP}/bin/run`, start),
  `bin/start`, `bin/stop`, `bin/backup` (to
  `s3://$IKIGENBA_BACKUP_BUCKET/${APP}/`), `bin/restore`.
- **systemd unit**, written by `bin/setup`, identical for every app:
  `Type=simple`, `User=<app>`, `WorkingDirectory=/opt/<app>`,
  `EnvironmentFile=/etc/ikigenba/env` (no leading `-`; a missing identity file
  must fail the unit), `ExecStart=/usr/local/bin/ikigenba-launch <app>`,
  `Restart=on-failure`, `After=`/`Wants=network-online.target`,
  `WantedBy=multi-user.target`. Logs go to journald.
- **Launcher.** On every start `ikigenba-launch <app>` sources
  `/etc/ikigenba/env`, sources `/opt/<app>/etc/current/manifest.env`, fetches
  `/ikigenba/${IKIGENBA_ENV}/app-config/<app>` via the instance role, exports
  each key (SSM wins over the manifest), and `exec`s `/opt/<app>/bin/run`.
  It **hard-fails** if SSM is unreachable, the grant is missing, or the
  parameter does not exist. Secrets live only in the launched process's
  environment. Rotation is re-push the parameter and `systemctl restart`.
- **nginx.** One apex `server` block (443, owned by the dashboard) includes
  `/etc/nginx/conf.d/locations/*.conf`. Each service ships `etc/nginx.conf`
  as a `location` fragment (`__APP__`, `__MOUNT__`, `__PORT__`) that
  `proxy_pass`es to `http://127.0.0.1:<port>/` with a trailing slash so the
  mount prefix is stripped. Every service location runs `auth_request
  /_authn` against the dashboard; nginx sets `X-Owner-Email` and
  `X-Client-Id` authoritatively after clearing inbound copies. The one
  unauthenticated service route is `/<mount>/.well-known/oauth-protected-resource`.
- **TLS.** One Let's Encrypt cert per apex host, issued and renewed by the
  dashboard's `bin/setup` via `certbot certonly --webroot` with a
  `--deploy-hook "systemctl reload nginx"`. Services issue no certs.
- **Identity.** An external IdP (Google) authenticates the human; the
  dashboard mints opaque tokens and introspects them for nginx. Services carry
  zero token logic and expose a no-side-effect `<svc>_whoami` MCP tool.
- **Connector plugin.** One suite plugin per box, served by the dashboard,
  bundling the skills for every service plus a `connect` skill that wires each
  service's MCP connector into the customer's `.mcp.json`.

**Why this shape.** The fixed seven-verb surface means every app on every box
presents the same operational interface from a workstation. Runtime variation
is quarantined to `bin/build`; `deploy.env` and `manifest.env` absorb the
per-account and per-app differences; the launcher keeps secrets off disk and
turns rotation into a restart; and path routing under one apex host means a
new service needs zero Terraform and zero DNS change.
