# metaspot

This directory contains the Terraform configuration for the AWS accounts you have access to:

- `bootstrap/` — per-account state backend bootstrap (local state)
- `test/` — `test` AWS account (213629091798), profile `test`
- `prod/` — `prod` AWS account (853624428511), profile `prod`

The apex `metaspot.org` Route53 zone lives in a separate `mgmt` account; each env owns its own delegated subtree (`test.metaspot.org`, `prod.metaspot.org`).

## SSH access

The `prod` EC2 instances are all Amazon Linux 2023, and the key below works on every one of them.

- **Key:** `~/.ssh/id_ed25519_ai4mgreenly` — matches the Terraform `aws_key_pair.ai4mgreenly` (public key comment `claude@logic-refinery.com`).
- **User:** `ec2-user`
- **Package manager:** `dnf` (e.g. system update: `sudo dnf upgrade -y`). Note AL2023 pins a release version; moving releases needs an explicit `--releasever=`.

Example: `ssh -i ~/.ssh/id_ed25519_ai4mgreenly ec2-user@<public-ip>`

## Creating a server

"Create a server" (in `prod/`) means: produce `<name>.tf` mirroring `biz.tf` (the canonical reference) — do **not** ask how to build it, only the three questions below.

- Own security group: ingress **80 + 443 from `0.0.0.0/0`** (public HTTP/HTTPS — port 80 is required for Let's Encrypt's HTTP-01 challenge and nginx's 80→443 redirect, see Service layer) and **22 from the admin IP only** (`216.173.146.119/32`, hardcoded per SG — update everywhere it appears when the IP changes). All egress. `name_prefix` (not `name`) + `lifecycle { create_before_destroy = true }` so a description/rule change replaces the SG without a `DependencyViolation` hang.
- `aws_instance`: AL2023, `ami` pinned to the current fleet pin (latest `prod/devlog` AMI entry). Reuse the shared `data.aws_vpc.default`, `data.aws_subnets.default`, `aws_key_pair.ai4mgreenly`, and the `aws_route53_zone.env` / `aws_route53_zone.ai` zones from `shared.tf` — one SSH key for the whole fleet. Explicit `root_block_device { volume_type = "gp3", volume_size = <N> }`. `user_data = templatefile("${path.module}/templates/metaspot-env.sh.tftpl", {...})` (see node-identity section), `user_data_replace_on_change = false`, and `lifecycle { ignore_changes = [ami, user_data] }`.
- `aws_eip`.
- A record `<name>.prod.metaspot.org` → EIP (canonical zone `aws_route53_zone.env`).
- CNAME `<name>.ai.metaspot.org` → the A record (alias zone `aws_route53_zone.ai`).
- `aws_iam_role` + `aws_iam_instance_profile` (**always** — every server has a role), attached via `iam_instance_profile`. Always carries the `backups-rw` policy (see Backups); the `app-config` grant is added only if secrets = yes.
- `<name>_public_ip` and `<name>_ssh` outputs in `outputs.tf`.
- A new dated `prod/devlog/` entry.

### The only three questions (use the defaults; ask nothing else)

1. **Secrets in Parameter Store?** — default **no**. The IAM role/profile always exists (for backups); "yes" only adds the `app-config` grant to it.
2. **Instance size?** — default **`t3.micro`**.
3. **Root volume size?** — default **10 GiB** (gp3).

### Node identity (`/etc/metaspot/env`)

Every server's `user_data` writes `/etc/metaspot/env` from `prod/templates/metaspot-env.sh.tftpl` — a flat `KEY=value` file (no `export`), both `source`-able from bash and valid as a systemd `EnvironmentFile=`. It carries **non-secret identity/topology only**, set per box:

`METASPOT_ENV`, `METASPOT_NODE`, `METASPOT_FQDN`, `METASPOT_ALIAS_FQDN`, `METASPOT_DNS_ZONE`, `METASPOT_AI_ZONE`, `METASPOT_AWS_ACCOUNT_ID`, `METASPOT_AWS_REGION`, `METASPOT_BACKUP_BUCKET`. The server's backup prefix is just `$METASPOT_NODE/` — not stored separately.

No secrets (user_data is world-readable via IMDS); no public IP (Terraform dependency cycle, and it's available from IMDS at runtime) — fetch volatile facts from IMDS.

`user_data` is the **first-boot baseline only**: cloud-init does not re-run it on reboot, and `ignore_changes = [user_data]` keeps a code change from ever force-replacing a live box — exactly the AMI-pin discipline. Consequence: changing the template does **not** update running boxes. Existing instances are brought into line by writing `/etc/metaspot/env` **out-of-band** (deterministic per box), never by reboot.

### Backups (shared S3 bucket, prefix-isolated)

One env-wide bucket `aws_s3_bucket.backups` in `shared.tf` (`metaspot-prod-backups-853624428511`): Block Public Access on, `BucketOwnerEnforced`, default **SSE-S3 (AES256)**, bucket policy denying non-TLS. No versioning.

Every server writes only under its own key prefix `<node>/`. Isolation is enforced **per-server in `<name>.tf`** by the `backups-rw` `aws_iam_role_policy`: `s3:GetObject`/`PutObject`/`DeleteObject` on `<bucket>/<node>/*`, plus `s3:ListBucket` on the bucket **conditioned** with `s3:prefix = ["<node>/*"]`. There is **no** bucket-wide object grant anywhere — that, plus the prefix-conditioned list, is what makes one server unable to read or write another's.

This is **IAM isolation, not cryptographic isolation**: all prefixes share one SSE-S3 key. A per-server KMS CMK would add a backstop at ~$1/key/mo — deferred, same stance as the secrets layer.

### Secrets / Parameter Store convention

One generic, hostname-independent parameter per env: `/metaspot/<env>/app-config` (`SecureString`). Terraform owns only its *existence* (placeholder value, `lifecycle { ignore_changes = [value] }`); the content is never in Terraform state. Services populate and read it via a checked-in helper script against that hardcoded path — every box uses the same path; nothing derives from the DNS name.

When secrets = yes, the server's `.tf` adds **only** an `aws_iam_role_policy` on its (always-present) role granting, on that single `app-config` ARN: `ssm:GetParameter` + `ssm:PutParameter`, plus `kms:Decrypt` + `kms:GenerateDataKey` scoped by `kms:ViaService = ssm.us-east-2.amazonaws.com`. No per-server parameter resource.

Accepted tradeoffs: any secrets-enabled box can read and overwrite the whole shared blob, so scripts must read-modify-write a single JSON key, not blind-overwrite.

### Service layer (the app on the box)

How a deployed app is expected to sit on a server. **Per-app, not per-box** — a server may host more than one service. For an app `<app>`:

- **Layout.** Install root `/opt/<app>`; the app's data/work dirs nest under it. A dedicated **system user `<app>`** (`useradd --system --home-dir /opt/<app> --shell /usr/sbin/nologin`) owns its own tree. No login shell; never a shared user.
- **Four scripts, shipped in the app repo, run from a workstation.** Every app exposes exactly this operational surface — nothing app-specific in the names:
  - `bin/setup` — one-time, **idempotent** box prep, root via `ssh -t … sudo bash -s`: `dnf install` (incl. `aws-cli`, `jq`, `nginx`, `certbot`), the app user + dirs, the launcher, the systemd unit, the nginx vhost, Let's Encrypt issuance + auto-renew timer; `systemctl enable` but **does not start** (code arrives via `deploy`).
  - `bin/deploy` — repeatable: build the artifact **off-box**, `rsync --rsync-path="sudo rsync"` it into place, append-only data sync, `chown` to `<app>`, `systemctl restart`, print status. The box never compiles anything.
  - `bin/backup` — push the app's state to the shared backups bucket under **its own `$METASPOT_NODE/` prefix only** (bucket from `/etc/metaspot/env`'s `METASPOT_BACKUP_BUCKET`; the `backups-rw` grant is prefix-scoped — see Backups).
  - `bin/restore` — pull that same prefix back. Symmetric with `backup`; IAM makes a box physically unable to touch another's prefix.
- **systemd unit** `/etc/systemd/system/<app>.service`: `Type=simple`, `User=<app>`, `WorkingDirectory=/opt/<app>`, `ExecStart=/usr/local/bin/<app>-launch`, `Restart=on-failure`, `After`/`Wants=network-online.target`, `WantedBy=multi-user.target`, and `EnvironmentFile=/etc/metaspot/env` — **no leading `-`**: a missing identity file must fail the unit, not let it run misidentified. Logs: the app writes only to stdout/stderr → journald (`journalctl -u <app>`); no app-managed log files, no logrotate.
- **Secrets: the launcher pattern, deliberately not systemd-native.** `/usr/local/bin/<app>-launch` runs as the app user (via the unit's `User=`) and on **every start**: fetches the shared `/metaspot/<env>/app-config` `SecureString` via the instance role (`aws ssm get-parameter --with-decryption`), `jq`s out this app's own JSON key, exports it, runs a **fail-fast smoke test** that the secret actually works, then `exec`s the binary. The secret exists only in the launched process's environment — never written to disk (not even tmpfs), never an `EnvironmentFile`, no `ExecStartPre` ordering. Rotation is "update the blob, `systemctl restart`". (`aws-cli`/`jq` are `bin/setup` packages; requires secrets = yes for the SSM grant.)
- **TLS / web.** nginx terminates TLS on 443 and reverse-proxies to the app's loopback-only listener; certbot owns the 443 server block and the 80→443 redirect. **This is why the SG opens 80** — Let's Encrypt's HTTP-01 validator has no fixed CIDR. SSH stays admin-only; that is the kept security win.

**Why this shape.** It is `ralph-scoops`'s proven `provision`/`deploy` split, generalised: renamed to the fixed `bin/{setup,deploy,backup,restore}` quartet so every app presents the same four verbs, and made **per-app** (not a fixed `/opt/app`) so one box can host several. The launcher was chosen over an `EnvironmentFile`/`ExecStartPre` or systemd-credentials approach because it keeps the secret off disk entirely and turns rotation into a restart — and it is the pattern already battle-tested in prod. `backup`/`restore` are first-class, not ad-hoc, because the prefix-isolated bucket only delivers its safety guarantee if every app uses it the same disciplined way.

## devlog/

Each subfolder has a `devlog/` directory containing a dated history of changes made to that environment — what was created, and the reasoning behind the decisions. Read the relevant `devlog/` entries before making changes to an environment so you understand the existing state and the rationale behind it. When you make a meaningful change, add a new dated entry.
