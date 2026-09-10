# metaspot

This directory contains the Terraform configuration for the AWS accounts you have access to. The topology is symmetric: one directory per AWS account, profile name = directory name = the environment it owns.

- `bootstrap/<name>/` — per-account state backend bootstrap (local state)
- `mgmt/` — `mgmt` AWS account (132801647717), profile `mgmt`. Owns the apex `metaspot.org` zone, NS delegations to every child account, and the `aws_organizations_account` + IIC assignment resources for the org. No servers. Also the home for the registered-domain portfolio. Registration for `ikigenba.dev` remains here while its authoritative DNS is delegated at the registrar to the `dev` account.
- `dev/` — `dev` AWS account (295229566359), profile `dev`. Owns the `ikigenba.dev` authoritative zone, host, and backup bucket.
- `prod/` — `prod` AWS account (853624428511), profile `prod`. Owns `prod.metaspot.org`.
- `test/` — `test` AWS account (213629091798), profile `test`. Owns `test.metaspot.org`.
- `sandbox/` — `sandbox` AWS account (654596473544), profile `sandbox`. Owns `sandbox.metaspot.org`.
- `ai/` — `ai` AWS account (417780655767), profile `ai`. Owns `ai.metaspot.org`.

`mgmt` is the AWS Organizations management account; all other accounts are members. IAM Identity Center is enabled in `mgmt` and grants `AdministratorAccess` to the `Administrators` group on every member account. One `sso-session metaspot` covers every profile in `~/.aws/config`.

**Adding a new account** (customer or otherwise) follows the same pattern as `ai`: a new entry in `mgmt/accounts.tf` (creates the AWS account + IIC assignment), a new `bootstrap/<name>/`, a new `<name>/` root with its `<name>.metaspot.org` zone, and a new NS delegation in `mgmt/delegations.tf`. Email convention: `mgreenly+<name>@gmail.com`. Bucket naming: tfstate `metaspot-<name>-tfstate-<accountid>`, backups `<name>-metaspot-org-<accountid>`. Default region: `us-east-2`.

**`ikigenba.com` — second customer apex.** `ikigenba.com` is the customer-facing product domain and plays the exact role `metaspot.org` plays internally: `mgmt` owns the apex zone (`mgmt/ikigenba.tf`) and delegates `<account>.ikigenba.com` out to per-customer member accounts via NS records. Onboarding a customer under it is the same `ai`-style pattern above, except the child root creates a `<account>.ikigenba.com` zone (instead of, or in addition to, `<account>.metaspot.org`) and the NS delegation goes in `mgmt/ikigenba.tf` rather than `mgmt/delegations.tf`. **`ikigenba.dev`** is the developer environment: `mgmt` owns its registration, while registrar nameservers delegate the apex to the `dev` account's hosted zone. The former `mgmt` hosted zone remains temporarily with matching apex and wildcard records so cached delegation continues to resolve during the handoff; remove it after the old NS TTL has expired.

## SSH access

The `prod` EC2 instances are all Amazon Linux 2023, and the key below works on every one of them.

The `dev` host uses `~/.ssh/id_ed25519_ikigenba_dev` with user `ec2-user`; the
`Host ikigenba.dev dev` entry in `~/.ssh/config` pins it to the host's Elastic
IP and selects that key.

- **Key:** `~/.ssh/id_ed25519_ai4mgreenly` — matches the Terraform `aws_key_pair.ai4mgreenly` (public key comment `claude@logic-refinery.com`).
- **User:** `ec2-user`
- **Package manager:** `dnf` (e.g. system update: `sudo dnf upgrade -y`). Note AL2023 pins a release version; moving releases needs an explicit `--releasever=`.

Example: `ssh -i ~/.ssh/id_ed25519_ai4mgreenly ec2-user@<public-ip>`

## Creating a server

"Create a server" means: in the target account root, produce `<account>.tf` per this spec. **One server per account.** The box is named after the account and answers on the apex `<account>.metaspot.org`. Multiple services run on the box (each with a name unique on that box); nginx routes them by **path** (`<account>.metaspot.org/<svc>/`) under that one apex host (see Service layer). The **dashboard** is the apex/default app and owns auth, the install landing page, and the nginx apex block. Do **not** ask how to build it, only the three questions below.

- Own security group: ingress **80 + 443 from `0.0.0.0/0`** (public HTTP/HTTPS — port 80 is required for Let's Encrypt's HTTP-01 challenge and nginx's 80→443 redirect, see Service layer) and **22 from the admin IP only** (`216.173.146.119/32`, hardcoded per SG — update everywhere it appears when the IP changes). All egress. `name_prefix` (not `name`) + `lifecycle { create_before_destroy = true }` so a description/rule change replaces the SG without a `DependencyViolation` hang.
- `aws_instance`: AL2023, `ami` pinned literally (resolve current via SSM `/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64` once, then hardcode). Reuse the shared `data.aws_vpc.default`, `data.aws_subnets.default`, `aws_key_pair.ai4mgreenly`, and the `aws_route53_zone.env` zone from `shared.tf` — one SSH key for the whole fleet. Explicit `root_block_device { volume_type = "gp3", volume_size = <N> }`. `user_data = templatefile("${path.module}/../templates/ikigenba-env.sh.tftpl", { ..., launcher_source = file("${path.module}/../templates/ikigenba-launch") })` (see node-identity section), `user_data_replace_on_change = false`, and `lifecycle { ignore_changes = [ami, user_data] }`.
- `aws_eip`.
- A record `<account>.metaspot.org` → EIP (zone `aws_route53_zone.env`). A wildcard `*.<account>.metaspot.org` → EIP may still be created but is **vestigial under path routing** — services answer on the apex host under paths, not on per-service subdomains.
- `aws_iam_role` + `aws_iam_instance_profile` (**always** — every server has a role), attached via `iam_instance_profile`. Always carries two policies: bucket-wide `backups-rw` (see Backups) and the env-wide `app-config` SSM grant. Both are unconditional — the launcher (installed at first boot) requires the SSM grant to function, so it is part of the baseline, not optional.
- `<name>_public_ip` and `<name>_ssh` outputs in `outputs.tf`.

### The only two questions (use the defaults; ask nothing else)

1. **Instance size?** — default **`t3.micro`**.
2. **Root volume size?** — default **10 GiB** (gp3).

The old "secrets in Parameter Store?" question is gone: every server gets the `app-config` SSM path grant unconditionally because the platform launcher requires it. Every app is seeded with its own `/…/app-config/<app>` parameter before first deploy — real keys when it has secrets, an explicit `{}` when it has none. The grant is cheap; the uniformity is worth more than the rare exemption.

### Node identity and platform launcher (first-boot baseline)

Every server's `user_data` is rendered from the top-level `templates/ikigenba-env.sh.tftpl` and does three things at first boot:

1. **Writes `/etc/metaspot/env`** — a flat `KEY=value` file (no `export`), both `source`-able from bash and valid as a systemd `EnvironmentFile=`. **Non-secret identity/topology only**, set per box: `METASPOT_ENV`, `METASPOT_NODE`, `METASPOT_FQDN`, `METASPOT_DOMAIN`, `METASPOT_DNS_ZONE`, `METASPOT_AWS_ACCOUNT_ID`, `METASPOT_AWS_REGION`, `METASPOT_BACKUP_BUCKET`. `METASPOT_DOMAIN` is the customer subdomain (e.g. `acme.metaspot.org`) — services mount under paths on it (`${METASPOT_DOMAIN}/<app>/`), not on per-app subdomains.
2. **Installs `/usr/local/bin/ikigenba-launch`** (mode `0755`, root:root). Source lives at `templates/ikigenba-launch` in the repo and is injected into the templatefile call via `launcher_source = file("${path.module}/../templates/ikigenba-launch")`. The launcher is platform, not application — every app's systemd unit `ExecStart`s it; no app's `bin/setup` touches `/usr/local/bin/`.
3. **Installs the launcher's runtime deps** — `dnf install -y awscli-2 jq` (AL2023 ships neither). Note the package name is `awscli-2`, not `aws-cli` (which doesn't exist on AL2023 and silently breaks the whole transaction under `-q`).

No secrets in user_data (user_data is world-readable via IMDS); no public IP (Terraform dependency cycle, and it's available from IMDS at runtime) — fetch volatile facts from IMDS.

`user_data` is the **first-boot baseline only**: cloud-init does not re-run it on reboot, and `ignore_changes = [user_data]` keeps a code change from ever force-replacing a live box — exactly the AMI-pin discipline. Consequence: changing the template (env vars or launcher) does **not** update running boxes. Existing instances are brought into line by writing `/etc/metaspot/env` and/or `/usr/local/bin/ikigenba-launch` **out-of-band**, never by reboot.

### Backups (per-account bucket, per-app prefix by convention)

One bucket per account `aws_s3_bucket.backups` in each account's `shared.tf`, named `<account>-metaspot-org-<accountid>` (e.g. `acme-metaspot-org-417780655767`): Block Public Access on, `BucketOwnerEnforced`, default **SSE-S3 (AES256)**, bucket policy denying non-TLS. No versioning.

The instance role carries a single bucket-wide `backups-rw` grant in `<account>.tf`: `s3:GetObject`/`PutObject`/`DeleteObject`/`ListBucket` on the whole bucket. Per-app prefix isolation **is not IAM-enforced** — it can't be cleanly, since all apps on the box share the one instance role. Apps write under `<app>/` by **convention** so backups stay legible and so two apps on the same box don't clobber each other. It's all one customer on one box; the practical blast radius is itself.

When an app needs to back up logically shared state (e.g. the whole `/etc/letsencrypt` tree, which holds certs for every app on the box), it captures the full tree under **its own `<app>/` prefix** rather than inventing a shared prefix — storage is cheap and each app's tarball stays self-sufficient for restore. The "per-app prefix is convention, not IAM" caveat still applies.

### Secrets / Parameter Store convention

**One parameter per app**, hostname-independent: `/metaspot/<env>/app-config/<app>` (`SecureString`), each a flat JSON object `{ "KEY": "value", … }`. A secret-less app holds an explicit `{}` — "no secrets" is a seeded state, not an absence. **No Terraform parameter resources at all**: existence is script-managed (the app repo's push tool creates on first `put-parameter --overwrite`), so adding an app to a box needs no apply. (The `int` account, on the `ikigenba.com` apex, uses the same shape under `/ikigenba/int/app-config/<app>` — migrated 2026-07-18; its former shared-blob parameter is deleted.)

The instance role's `app-config` policy grants, on the hardcoded path pattern `…parameter/<prefix>/<env>/app-config/*`: `ssm:GetParameter` + `ssm:PutParameter`, plus `kms:Decrypt` + `kms:GenerateDataKey` scoped by `kms:ViaService = ssm.us-east-2.amazonaws.com`. Unconditional — the launcher needs it.

Accepted tradeoff: all apps share the one instance role, so any app on the box can read or overwrite any app's parameter — per-app isolation is by path convention, not IAM (same posture as the backup bucket prefixes). A write is a plain overwrite of one app's own parameter; there is no shared blob and no read-modify-write.

### Service layer (the app on the box)

How a deployed app sits on a server. **Per-app, not per-box** — a server may host multiple apps, each independently deployed. App names must be unique on a given box. For an app `<app>`:

**Path routing is the model** (it supersedes the old subdomain-per-service / host-header design). Services are pure REST + MCP APIs with **no UI**, bound to loopback, mounted under paths on one apex host; the **dashboard** is the apex/default app and the suite's OAuth authorization server. The two authoritative specs are `docs/path-routing-architecture.md` (topology + auth contract) and `docs/connector-and-install.md` (client-side connector/plugin/install). The bullets below are the platform summary; those docs win on detail.

- **Layout.** Install root `/opt/<app>/`; data/work dirs nest under it. Dedicated system user `<app>` (`useradd --system --home-dir /opt/<app> --shell /usr/sbin/nologin`) owns its tree. No login shell, never shared.
- **Entrypoint convention.** Every app exposes a single executable at `/opt/<app>/bin/run` regardless of runtime. Go ships the binary there. Node/Python ship a short shell wrapper that execs `node server.js` / `python -m app`. This is what makes the systemd unit identical across all apps — the runtime difference lives in the app, not the platform.
- **Manifest (on-box, runtime).** Every app ships `etc/manifest.env` — flat `KEY=value`, sourceable from bash:
  ```
  PORT=8123
  MOUNT=/crm/
  DEFAULT=false
  ```
  `PORT` is the **loopback** port the app listens on — services bind `127.0.0.1` only; nginx is the sole trust boundary (see auth, below). `MOUNT` is the path prefix the service answers on under the apex host (`<account>.metaspot.org/crm/`). `DEFAULT=true` means this app also answers on the bare apex `<account>.metaspot.org/` — this is the **dashboard's** role; **at most one app per box may set it.** Two defaults = nginx conflict, by design.
- **Workstation routing (`etc/deploy.env`).** Sibling to `manifest.env` in the repo, but **workstation-only** — never shipped to the box. Sourced by every `bin/*` script before any `ssh`. Flat `KEY=value`, committed (non-secret routing). Required keys:
  ```
  ACCOUNT=<account-name>            # determines DOMAIN and HOST: <account>.metaspot.org
  SSH_USER=ec2-user
  SSH_KEY=~/.ssh/id_ed25519_ai4mgreenly
  CERTBOT_EMAIL=<address>
  ```
  Scripts derive `DOMAIN="${ACCOUNT}.metaspot.org"` and `HOST="${HOST:-${ACCOUNT}.metaspot.org}"` from `ACCOUNT`. `HOST` is overridable via env for unusual targets but normally inferred. This convention is what lets the seven `bin/*` scripts be byte-identical across apps — all app- and account-specific routing lives in `deploy.env` (account) and `manifest.env` (app) plus the `${APP}` literal at the top of each script.
- **Seven scripts, shipped in the app repo, run from a workstation — each script ssh's into the box itself.** That's why they exist as scripts rather than as "just use `systemctl`": the operational surface is the same seven verbs invoked locally, no manual ssh required. The runtime-specific work lives in `bin/build`; the other six are byte-identical across apps.
  - `bin/build` (**per-app**) — produces `build/${APP}` as the deploy artifact. Go: `go build -o build/${APP} ./...`. Node: `npm run build` and place the bundle/entry at `build/${APP}`. Python: a thin wrapper script at `build/${APP}` that execs the interpreter. This is the **one** place runtime variation is allowed; everything else is generic. `build/` is gitignored.
  - `bin/setup` — one-time, idempotent box prep (root via `ssh -t … sudo bash -s`): `dnf install` of the app's own runtime deps only (e.g. `nginx`, `certbot`). The launcher's deps (`awscli-2`, `jq`) are already present from instance bootstrap — `bin/setup` does not install them. Then the app user + dirs and the systemd unit; `systemctl enable` but does not start. The launcher is **not** touched — it's a platform binary already in place. **nginx work differs by role** (see nginx, below): the **dashboard's** `bin/setup` owns the single apex `server` block, the one apex TLS cert (HTTP-01) + renewal timer, the ACME-challenge location, the `/_authn` internal location, and the `include` of the locations dir. A **service's** `bin/setup` only writes its `location` fragment to `/etc/nginx/conf.d/locations/<app>.conf` and reloads nginx — no vhost, no cert.
  - `bin/deploy` — repeatable: calls `./bin/build`, `bin/stop`, `rsync --rsync-path="sudo rsync"` `build/${APP}` to `/opt/${APP}/bin/run`, `chown` to `${APP}:${APP}`, `bin/start`. The box never compiles. Brief downtime during rsync is accepted; if it ever matters, layer in an `error_page` maintenance fallback later.
  - `bin/start` — `systemctl start ${APP}`.
  - `bin/stop` — `systemctl stop ${APP}`. nginx (if present) will serve 502 until restarted; this is fine.
  - `bin/backup` — push state to `s3://$METASPOT_BACKUP_BUCKET/${APP}/` by convention (the role's grant is bucket-wide; see Backups).
  - `bin/restore` — symmetric pull from the same prefix.
- **systemd unit, written by `bin/setup`.** Identical shape for every app — the entrypoint convention is what allows this:
  ```
  [Service]
  Type=simple
  User=<app>
  WorkingDirectory=/opt/<app>
  EnvironmentFile=/etc/metaspot/env
  ExecStart=/usr/local/bin/ikigenba-launch <app>
  Restart=on-failure
  ```
  Plus `After=network-online.target`, `Wants=network-online.target`, `WantedBy=multi-user.target`. **No leading `-` on `EnvironmentFile=`** — a missing identity file must fail the unit, not let it run misidentified. Logs go stdout/stderr → journald; no app-managed log files.
- **Secrets — shared launcher.** `/usr/local/bin/ikigenba-launch <app>` is installed by **instance bootstrap** (see Node identity), not by any app's `bin/setup`. Apps just `ExecStart` it. On every start the launcher: sources `/etc/metaspot/env`, sources `/opt/<app>/etc/current/manifest.env` (non-secret defaults), fetches the app's own parameter `/metaspot/<env>/app-config/<app>` (a flat `{KEY: value}` JSON object) via the instance role, exports each `KEY=value` (SSM wins over manifest on collision), then `exec`s `/opt/<app>/bin/run`. **Hard-fails** if SSM is unreachable, the grant is missing, or the parameter doesn't exist — `ParameterNotFound` included, since every app is seeded before first deploy (secret-less apps hold an explicit `{}`), so a missing parameter means never-seeded and the app must not start. Secret lives only in the launched process's environment — never on disk, never an `EnvironmentFile`. Rotation = re-push the app's parameter, `systemctl restart <app>`.
- **nginx — path routing + edge auth.** One apex `server` block (listen 443, `server_name <account>.metaspot.org`), owned by the **dashboard**, which `include`s `/etc/nginx/conf.d/locations/*.conf`. Each service ships `etc/nginx.conf` as a **`location` fragment** (placeholders `__APP__`, `__MOUNT__`, `__PORT__`), substituted by its `bin/setup` and dropped into that locations dir. The fragment `proxy_pass`es to `http://127.0.0.1:<port>/` with a **trailing slash so the `<mount>` prefix is stripped** — the service sees root-relative paths and stays mount-agnostic. Every service location runs `auth_request /_authn` against the dashboard's introspection endpoint; the dashboard validates the opaque bearer token and returns identity headers (`X-Owner-Email`, `X-Client-Id`) that nginx sets **authoritatively** (any inbound copies cleared first, so a client can't spoof identity). The one unauthenticated service route is its static `/<mount>/.well-known/oauth-protected-resource` doc. A service with no fragment is reachable only locally — fine for workers/batch. Full contract: `docs/path-routing-architecture.md`.
- **TLS — one apex cert, owned by the dashboard.** A single Let's Encrypt cert for `<account>.metaspot.org` via `certbot certonly --webroot -w /var/lib/letsencrypt`, issued and renewed by the **dashboard's** `bin/setup` with a persisted `--deploy-hook "systemctl reload nginx"`. Services do **not** issue certs — they mount under the apex host the dashboard already terminates TLS for. Port 80 in the SG carries the challenge plus the 80→443 redirect.
- **Identity & auth (suite-wide).** An external IdP (Google) authenticates the human; the **dashboard mints its own opaque tokens** and is the suite's OAuth authorization server. nginx introspects every request via `auth_request` (cached, loopback) so revocation from the dashboard is instant. Services carry **zero token logic** — they trust the identity headers nginx injects and use them for audit. Every service MCP exposes a no-side-effect `<svc>_whoami` tool so the client connect skill can prove the chain end to end. Audit is per-service. Details: `docs/path-routing-architecture.md`.
- **Connector plugin (client side).** **One suite plugin per box** (not per service) bundles the skills for every service on the box plus a single `connect`/doctor skill that wires up each service's MCP connector individually. It is an **internal** plugin — distributed from a private repo or, simplest, **served by the dashboard** off the box; never the public Claude catalog. Its home is a `plugin/` subfolder of the **dashboard** repo, which also exposes the box's service inventory the connect skill reads. Each service's remote MCP connector lands in the customer's project `.mcp.json` (per-customer URL + OAuth); the dashboard landing page serves the one-paste install snippet. Details: `docs/connector-and-install.md`.

**Why this shape.** The fixed seven-verb surface (`build/setup/deploy/start/stop/backup/restore`) means every app on every box presents the same operational interface, invoked from a workstation with no manual ssh. Runtime variation is quarantined to `bin/build`; the other six are byte-identical across apps because `etc/deploy.env` (account routing) and `etc/manifest.env` (app config) absorb all the per-app/per-account differences. The `/opt/<app>/bin/run` convention pushes runtime variation into the build so the systemd unit can be identical platform-wide. The launcher pattern keeps secrets off disk and turns rotation into a restart. Path routing under one apex host means shipping a new service needs zero Terraform and zero DNS change — just a new `location` fragment; edge `auth_request` keeps every service free of token logic; and one apex cert replaces per-service certs.
