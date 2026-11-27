# D09-on-a-space

This document covers what accounts contributes when it runs on a space and in a sandbox:

- what ships
- what the manifest says and what a space takes from it
- the nginx fragment `cli.NginxConf` and the file under `etc/` that copies it
- the working directory, the environment and the services file entries it uses
- the preconditions the hosts feature stands on
- the memory cloud and simulated hosts a sandbox gives it
- the walk the delivery makes in the sandbox
- the trust accounts places in what reaches it
- the finish line

Outcomes cited as outcome N are those of /home/mgreenly/Documents/ikigenba-ssh-hosts.md, and log entries those of /home/mgreenly/Documents/ikigenba-ssh-hosts-decisions.md. Self-service outcome N is outcome N of /home/mgreenly/Documents/ikigenba-self-service-accounts.md. Host-names outcome N is outcome N of /home/mgreenly/Documents/ikigenba-host-names.md, and host-names log entries those of /home/mgreenly/Documents/ikigenba-host-names-decisions.md. Host-numbers outcome N is outcome N of /home/mgreenly/Documents/ikigenba-host-numbers-and-delete.md, and host-numbers log entries those of /home/mgreenly/Documents/ikigenba-host-numbers-and-delete-decisions.md. Host-backups outcome N is outcome N of /home/mgreenly/Documents/ikigenba-host-backups.md, and host-backups log entries those of /home/mgreenly/Documents/ikigenba-host-backups-decisions.md.

Almost every step between a checkout and a running host belongs to someone else:

- devctl builds and ships the release, `accountsd` included, and `devctl hosts set` gives one space its AWS rights.
- infra holds the zone, the launch template whose user data installs `accountsd`, the hosts' role and profile, and the operator policy.
- opsctl installs the release, writes the environment and generates nginx.
- auth decides every person's credential.
- AWS runs the hosts and answers the daemons' identity.
- telemetry keeps the trail.

accounts' own share is what the other documents of this design state, plus three things this document states:

- the contents of `cli.Manifest`, which `D02-cli` declares
- the constant `cli.NginxConf`
- that `etc/nginx.conf` holds the fragment's bytes

## What ships

The release holds five members of accounts' directory:

- `bin/accounts`.
- `bin/accountsd`, the daemon. `devctl build` builds every `cmd/<x>` other than `cmd/accounts` static for linux/arm64 and ships it as `accounts/bin/<x>` (log D5). On a space it lands at `/opt/ikigenba/current/accounts/bin/accountsd`, beside `bin/accounts`, which is where accounts finds it (`D01-layout-and-run-seam`).
- `etc/manifest.toml`, holding the bytes of `cli.Manifest`.
- `etc/nginx.conf`, holding the bytes of `cli.NginxConf`.
- `share/icon.svg`, unchanged.

The two files under `etc/` are what the root package's `Etc` embeds (`D01-layout-and-run-seam`, R-C7K6-723O). The templates and the migrations ship inside the binary (`D01-layout-and-run-seam`, R-7QD1-Z5PQ and R-20U1-QZJH; the migrations' bytes are `D04-store`'s). The database is not in the release: accounts creates `state/accounts.db` on its first start, and in a sandbox also `state/sandbox-cloud.json` (`D01-layout-and-run-seam`, R-ESG4-8UGU and R-Y9CP-KR8R).

The tarball, its member list and its name are devctl's, and no test reads the checkout. So the release's members are outside what a requirement can state; a developer checks them with `tar`.

opsctl's `activate` used to refuse a `bin/` holding more than `<app>`. Under log D11 it now accepts extra regular files there; that is opsctl's change, already landed, and a precondition of this release.

## The manifest

opsctl, the sandbox, devctl and home read the manifest, so its text is contract and this document states it byte for byte (R-DJ7M-H4AO). It also bounds the description so that TOML reads it as itself (R-APH7-IHIV).

In order, the manifest says:

- the app is `accounts`
- its description is `pages.Description` (`D06-pages`, new words)
- it is not the default app
- it is an MCP service, so the gateway offers the thirty-six tools (`D07-tools`, R-GPMX-YWDP)
- it admits guests (`guests = true`)
- it holds no secrets (`secrets = []`)
- a `[database]` table names `sqlite` and `state/accounts.db`
- a `[resources]` table holds only `memory_max = "256M"`
- a `[home]` table holds only `group = "core"`

**Why `guests = true`** (coordinator decision Q1). A daemon on a host has no person's credential. It must reach `/accountsd`, to download the binary at first boot and on self-update, and `/accountsd/v1/*`, to long-poll and report (outcome 21).

- Under `guests = true`, opsctl's nginx and the sandbox's ask auth's `/check/open` on `location /`. That subrequest admits a request with no credential and passes it on with no identity headers.
- `/mcp` keeps the strict `/check`, so an agent without a credential is still turned away by nginx and auth with 401.
- A page asked without a credential now reaches accounts. `identity.Optional` and `D06-pages` send that guest to sign in, as before.
- The enrollment page, `/join?token=<token>`, rides the same `location /` and answers the guest itself (`D03-serve`, `D18-enrollment`): a person holding an invite link has no credential on the space (self-service outcome 11). It needs nothing more of the manifest or the fragment.
- The daemon routes take no person's identity at all (`D03-serve`). `/accountsd` serves the release's binary to anyone, and `/accountsd/v1/*` demands the presigned identity header that `D13-daemon-endpoints` verifies (`D12-host-identity`).

**Departure (recorded, flagged to the caller).** Outcome 21 says the daemon long-polls on "a path nginx passes without auth's subrequest". Under `guests = true` nginx still makes a subrequest, `/check/open`, which admits anyone. The daemon paths therefore depend on auth being up. While auth is down a host keeps its last applied state and sign-ins keep working (outcome 22), so nothing is lost but freshness.

The alternative was a fragment `location` that proxies `/accountsd` to accounts' socket without `auth_request`. It is not portable:

- opsctl proxies to `unix:/run/ikigenba/accounts.sock`.
- The sandbox proxies to an `upstream` of its own naming.
- A fragment can name neither without encoding a sibling's internals.

Giving fragments a portable proxy target would be two sibling changes (log D13, Q1 (b)). Switching to it later changes only this document's manifest and fragment.

**Other departures:**

- accounts' previous manifest had `guests = false`. The precedent for `guests = true` is webhooks (`/in/<slug>`) and sites.
- `memory_max` moves from `128M` (ledger's and connections' value, and accounts' before) to `256M`. The reasons are the AWS SDK's clients, up to `daemonapi.MaxWaiters` waiting long polls each holding a goroutine and a watch, and report bodies of up to `protocol.MaxReportBytes`.

**What the manifest leaves out:**

- **No `[env]`.** accounts' settings are the space's:
  - `DRAIN_SECONDS`
  - `AWS_REGION` and the four `IKIGENBA_HOSTS_*` keys, which opsctl writes only when its configuration names them (log D9) and reserves, so a manifest naming one is refused
  - `IKIGENBA_SANDBOX`, the sandbox's
- **`secrets = []`.** accounts' AWS credentials come from the space host's instance role, through the SDK's default chain. `devctl hosts set` attaches the operator policy to that role (outcome 5).
- **No `[events]`.** accounts emits nothing on the bus and accepts nothing (`D08-trail`).
- **No port and no name.** Nothing names a space, a release, a host name, the zone or the template.

**Why `core`.** home draws its `core` group apart from `application`. accounts is platform infrastructure, now operating the business's SSH hosts, like repos and connections. `[resources]` names no `slice`, so the unit runs in the apps' slice.

## The nginx fragment

`cli.NginxConf` holds back the suite's two internal bus paths, `/events` and `/declarations`, at the public name, with two exact-match locations answering 404. That is unchanged.

Nothing else is held back or added:

- `/accountsd` and `/accountsd/…` ride `location /` with `/check/open`, so the fragment needs no location for them, and so does `/join`.
- `/mcp` is guarded by nginx's own locations.

`protocol.LongPoll` (45 s) sits under nginx's default 60 s `proxy_read_timeout`, which opsctl's generated configuration leaves unset. So a long poll needs no timeout directive in the fragment either. opsctl and the sandbox include `etc/nginx.conf` inside accounts' server.

## On a space

accounts runs on the same terms as under a test:

- a socket passed as descriptor 3 (`/run/ikigenba/accounts.sock`)
- `/var/opt/ikigenba/accounts` as its working directory
- its environment from `/etc/opt/ikigenba/accounts/env`, which opsctl writes

That environment holds `DRAIN_SECONDS`, `IKIGENBA_SERVICES`, `IKIGENBA_COMMIT` and `IKIGENBA_RELEASE`. On the space that operates hosts it also holds `AWS_REGION`, `IKIGENBA_HOSTS_DOMAIN`, `IKIGENBA_HOSTS_ZONE_ID`, `IKIGENBA_HOSTS_LAUNCH_TEMPLATE` and `IKIGENBA_HOSTS_INSTANCE_PROFILE`. It never holds `IKIGENBA_SANDBOX`.

With all five hosts keys present accounts runs in AWS mode. With any one missing it runs unconfigured: the tools refuse saying so, the page says so, and the engine does not run (outcome 4; `D01-layout-and-run-seam`). Every other space ships the same binary and runs unconfigured.

Of the services file accounts uses three entries:

- `auth`, for the banner and sign-in
- `telemetry`, for the trail
- `accounts`, its own entry, whose URL is the **accounts URL** tagged on every instance it launches and the **binding** its verifier expects (`D01-layout-and-run-seam`)

On a space that URL is `https://accounts.<space host>`.

**Preconditions of the hosts feature.** They are stated in prose only; no test can observe them.

- **infra applied** (log step 2, D7, D8). It provides:
  - The hosted zone for the hosts domain.
  - The hosts' launch template. Its user data reads the instance's `AccountsURL` tag through IMDS, downloads `<AccountsURL>/accountsd` (arm64 ELF, over HTTPS, no redirect), writes `ACCOUNTS_URL` to `/etc/accountsd.env`, and enables `accountsd.service` with `Restart=always`.
  - The security group.
  - The hosts' role and instance profile, named alike, which `D12-host-identity` relies on: the verifier expects the assumed-role ARN under the profile's name.
  - The operator policy holding the tag contract `D05-cloud` and `D10-operations` follow. AWS denies any call outside it.
  - In that policy, and in the space boundary that carries the same statements, `ec2:DeleteVolume` on volumes tagged `Kind=host-data` and nothing wider (host-numbers Sequence step 1; infra commit e69e0506, applied 2026-11-25, host-numbers log P1). A delete told not to keep the data (`delete_data`, `D10-operations`) and `delete_kept_volume` (`D07-tools`) need it; without it AWS answers `UnauthorizedOperation`, which fails the delete or returns the kept volume to `kept` with that reason (`D05-cloud`).
  - In that policy, the Route 53 change right on the hosts zone, which already allows a host's record (host-names "What was observed": the operator role already allows the record), so the host-names release needs no new right. Precedent, not proof, for its scope is infra's `iam.tf`, which limits it to records of type A whose names match `*.<hosts domain>` and are not one of the site names Terraform keeps there; the exact scope is checked at the finish line (Route 53 accepts the host's record under the operator policy). accounts writes records for accounts and for hosts and nothing else: `<account>.<hosts domain>` (`D10-operations`) and `<host>.<hosts domain>` (`D10-operations`). Host names, `host` followed by digits, are reserved for accounts and users (`D17-names-keys-sizes`), so the two never collide; neither form is one of the site names.
- **`devctl hosts set`** on the operating space (outcome 5), which gives its role the operator policy.
- **opsctl writes the five keys** (log D9) and accepts extra files in `bin/` (log D11).
  - The host backups bucket and the backup role (host-backups Sequence step 1; infra commit `b9ab479a`). The bucket is the one `settings.Hosts.BackupBucket()` names (`D01-layout-and-run-seam`): versioned, TLS only, encrypted by S3, its policy denying the backup role's sessions any version delete, its one lifecycle rule expiring hidden versions after 35 days and aborting unfinished uploads. The role is the one `settings.Hosts.BackupRole()` names: it trusts the account, and allows get, put, delete and abort-multipart on the bucket's objects and list and get-location on the bucket. The hosts' role has no right on the bucket.
  - In the operator policy, and in the boundary that carries it, `sts:AssumeRole` on the backup role, and list, list versions, delete and delete versions on the bucket. accounts assumes the role with a session policy for each credential set it issues (`D21-backup-access`), and lists and deletes under its own role for `create_account`'s name check, `delete_account_backups` and a delete's `delete_backups` step (`D07-tools`, `D10-operations`). The boundary is within a few characters of IAM's limit, and this release adds nothing to it.
- **The space's accounts unit can reach IMDS and AWS's regional endpoints** for EC2, STS, S3 and Route 53. Its unit hardening is opsctl's, and the finish line checks it.
- **The hosts can reach S3's regional endpoint and the release assets accountsd installs restic and Litestream from** (`D15-machine`), over HTTPS. The finish line checks it by `mg` reaching healthy.

The host-names release adds no precondition. The hosts domain and zone are the ones accounts already writes account records in, and the operator role already allows a host's record (host-names "What was observed"). Its infra change is comments only (host-names log P3), so infra needs no new apply for it.

The host-numbers release adds one precondition, the `ec2:DeleteVolume` right above, applied before the deploy. Nothing else changes for the operator: no new key, no new setting, no opsctl change.

The host-backups release adds one precondition, infra's `b9ab479a` applied before the deploy: the bucket, the backup role and the operator policy's rights on both, above. Nothing else changes for the operator: accounts derives the bucket and the role from the hosts settings it already has, so there is no new key, setting, secret or opsctl change, and the manifest is unchanged.

**Backup and restore.** The host's backup archives `state/` whole, so `accounts.db` goes with it: the hosts, operations, accounts, users, keys and latest reports (`D04-store`). After a restore the record may lag AWS. Operations resume against what AWS holds, by their recorded steps and lookups by tag (`D10-operations`). A resource no restored record explains is reported as unexplained, never deleted (`D10-operations`), and is never listed or deleted as a kept volume, which only the record names (`D04-store`). A kept volume the restored record lists that was deleted since counts as done when deleted again. For a host that is running, stopping, stopped, starting or resizing and has an EIP, the engine's scan puts back its own A record if it is missing, and a record that is not the host's is left alone and shown as the host's reason (`D10-operations`). This is the suite's backup of accounts' own `state/`, and the host-backups release leaves it as it is (host-backups outcome 48); the hosts' backups are another thing, the files and databases of the accounts on the hosts, which the hosts' daemons write to the host backups bucket and which the space's backup neither takes nor restores.

**Releases: expand only** (outcome 27). This release adds `0002_hosts.sql` and stops reading or writing the old `groups` and `users` tables of `0001`, but drops nothing (`D04-store`). It stops calling opsctl: no `sudo`, no `opsctl group|user` (`D01-layout-and-run-seam`, R-ETO0-MM7J). It changes nothing in opsctl. The SSH bans release adds `0003_bans.sql`, new tables and indexes only, and the release before it runs on that schema (`D04-store`). The self-service release adds `0004_invites.sql` the same way: an admin flag is a row of a new table rather than a new column of the users' table, so the release before it runs on that schema unchanged (`D04-store`); a user enrolled through an invite is an ordinary user to the old release, and the admin flag never reaches a host, so the daemon protocol does not change. The host-names release adds `0005_host_records.sql` the same way, a new table only, so the release before it runs on that schema (`D04-store`). Its persisted operation steps follow the same rule: a create's own record steps are named `host_record` and `wait_host_record`, which the release before it does not know and so never runs as an account's steps, and a host that release deletes keeps its record only until the scan removes it (`D10-operations`). It adds `domain` to the desired state and `hostname` to the report in the same release, a recorded departure from expand then contract (`D11-protocol`; host-names log P2): the daemon ships inside accounts and updates itself to the digest accounts names, both sides ignore members they do not know, and a daemon given no domain leaves the hostname alone, so an old and new mix works in both directions and a rollback never renames a host (host-names outcome 6). The host-numbers release adds `0006_kept_volumes.sql` the same way, new tables and indexes only, and the release before it runs on that schema (`D04-store`): a delete's choice and a kept volume live only in the new tables, which the release before never reads; a host number it reuses is one the release before never gives again, since that release names a host by its counter, which is above every number given; and a delete told not to keep the data that the release before finds at a step it knows goes on to keep the data, the safe side, while one at `delete_data` or `wait_data_deleted`, step names it does not know, waits and times out until the roll-forward, which resumes or restarts it (`D10-operations`; host-numbers log P3).

The host-backups release adds `0007_host_backups.sql` the same way: it only creates tables and indexes, names no table an earlier file created, and declares no foreign key, so the release before it runs on that schema and its vacate of a reused host name, which renames only `hosts`, `operations` and `host_records`, is never blocked by a key from a new table; every host-scoped row of the new tables carries the host's `seq`, so a host of a reused name never inherits them (`D04-store`). The new operation kinds, close, open, replace and restore, are kept only in the new table `backup_operations`, which the release before never reads, so across a rollback such an operation pauses at its step and resumes after the roll-forward instead of failing its host (`D04-store`, `D10-operations`). The one existing kind with a new step, a delete's `delete_backups`, stays in `operations` under the host-numbers precedent above: the release before does not know the step, so it waits and fails after `StepTimeout`, and `retry_host` after the roll-forward finishes it. It adds `backup` to the desired state and `backups`, `host_backup` and `restores` to the report in the same release, a recorded departure from expand then contract like `domain` (`D11-protocol`): `backup` is read by the daemon of the same release that sends it, which updates itself to the digest accounts names within a cycle; both sides ignore members they do not know; and a daemon given no `backup` runs no backup, stopping its Litestream units and reporting no credentials, so absence means no backups and an old and new mix is safe in both directions (`D14-accountsd`, `D15-machine`).

Its costs during a rollback are accepted and recorded, on the precedent of `delete_data` above (host-backups plan pick 16):

- The release before shows a new host state (`closing`, `opening`, `replacing`, `restoring`) as a word on its pages. Its `host` tool refuses with `store.Unreachable` a host whose current operation is a new kind, since that operation is not in the table it reads; its `retry_host` refuses such a failed host as no host; its `stop_host` and `start_host` refuse it by state; and its `resize_host` and `delete_host` could act on it, since it sees no running host-kind operation.
- A closed host reopens: the release before sends its users active. The closed time stays in the new table, and the roll-forward locks them again.
- A replace caught before its old instance is retired has changed nothing a daemon sees. A replace from backup caught after the retire and before admission is sent its accounts and users by the release before, whose daemon formats the new volume and seeds new folders; after the roll-forward the per-account emptiness check refuses to restore over them, the replace fails, and the operator replaces from backup again onto a fresh volume, the seeded one kept with cause `replaced` (`D15-machine`, `D10-operations`).
- A kept volume of a replace is an ordinary kept volume to the release before, listed and deletable, with no cause shown.
- A daemon of the release before under the new accounts (a failed self-update) reports no backups, so its accounts read health `none`; a close on its host never completes, and `open_host` cancels it; a partial restore stays pending.

The next release contracts. It drops accounts' old tables, and opsctl removes the shell group and its sshd rules, the pooled limits, the sudo rule for accounts, and its `group` and `user` commands. The daemon protocol changes by expand then contract too (`D11-protocol`), the host-names release's one-release addition being the recorded exception.

## In a sandbox

The sandbox builds accounts from the worktree and gives it `DRAIN_SECONDS` and `IKIGENBA_SANDBOX`, and no hosts key. It also builds `cmd/accountsd` into `<data>/bin/accountsd` beside `bin/accounts`, for the developer's architecture and still an ELF, and starts nothing for it (log D11, D12). It includes `etc/nginx.conf` in accounts' server. Because the manifest admits guests, it routes `location /` through auth's `/check/open` and `/mcp` through `/check`, as a space does.

Sandbox mode (`D01-layout-and-run-seam`):

- accounts runs on `D16-sandbox`'s memory cloud, kept in `state/sandbox-cloud.json` under its app directory.
- The memory cloud enforces the tag contract and takes observable time over each transition.
- One simulated daemon (`D16-sandbox`) runs in process for every running instance. It speaks the real protocol, through the real verifier, to accounts' own handler, and applies to an in-memory machine.

No AWS call and no real host change is made on the developer's machine.

What survives a restart:

- The record and the memory cloud survive `down`, `up` and an accounts restart, so an interrupted operation resumes against what the fake already made.
- Simulated machines do not survive. Their next cycle re-applies the whole desired state, the hostname included, as a real daemon does after a reboot.
- `sandbox wipe` removes everything.

An agent is curl on the developer's machine:

- the pages at `http://accounts.<sandbox>.localhost:<port>/`, with `Authorization: Bearer $(sandbox token)`
- the tools through the gateway or at accounts' `/mcp`, with the same token

### The walk

The sandbox has no real machine: it shows a host's record in the memory cloud and the hostname a simulated host was asked to take and reports, never a real hostname (host-names "What was observed"). The sandbox is up from the worktree. The delivery walks it as the developer, with the stored token, through the gateway's tools or accounts' `/mcp`, and with curl. Keys are fresh ed25519 OpenSSH public keys the delivery generates.

The timings are `D16-sandbox`'s: about 40 s of `provisioning`, 5 s for a DNS change, and 3 s per apply.

1. **Before any change.**
   - home lists accounts.
   - `GET /`, `/tools` and `/about` are fetched with the token, and without it (sent to sign in).
   - `POST /mcp` without a credential gets 401 from nginx.
   - `/events` and `/declarations` answer 404.
   - The gateway's `describe` of accounts lists the thirty-six tools (`D07-tools`, R-GPMX-YWDP), `kept_volumes` and `delete_kept_volume` right after `delete_host`, and `close_host`, `open_host`, `replace_host`, `backup_status`, `restore_points`, `restore_database`, `restore_files` and `delete_account_backups` right after `disallow_ip`.
   - `/about` shows the backup settings, the defaults (`D04-store`), and `backup_status` lists no account.
   - `kept_volumes` lists none.
   - `curl` of `/accountsd` without a token gets bytes equal to `<data>/bin/accountsd`.
   - `GET /accountsd/v1/state` without the identity header is answered 401 by accounts.
2. **Create a host.**
   - `hosts` lists none.
   - `create_host` with no argument answers `host01`, `provisioning`; its answer is unchanged and carries no DNS name.
   - `host` shows `host01` and `dns_name` `host01.hosts.test`, `/hosts/host01` shows the host page with the DNS name, and the landing row shows the DNS name and health `none`.
   - Once the host's address is made, `state/sandbox-cloud.json` holds one A record at `host01.hosts.test` whose one value is the host's EIP; the launch follows the record's change, about 5 s.
   - After about 40 s the host is `running`, and within a cycle its health is `sync` with a full report on `/hosts/host01`, which shows the reported hostname `host01.hosts.test`; the `host` tool's `report` carries `hostname` `host01.hosts.test`. `hosts` lists `host01` with its `dns_name`.
3. **Place an account.**
   - `create_account` `acme` on `host01` answers `pending` and the DNS name `acme.hosts.test`. It turns `active` after the DNS change and one apply.
   - With no other step, within about a minute of `acme` turning `active`, `backup_status` lists `acme` on `host01` with health `healthy`, a snapshot time and no error, and `/hosts/host01` shows its backup row (host-backups outcome 2).
   - `create_account` `www` is refused as reserved.
   - `acme` again is refused as taken.
4. **Add users.**
   - `add_user` `bugs` and `daffy` to `acme` with keys: each answers `pending: true`, then `false` once applied.
   - `add_user` a third user under the name `acme` is refused as taken.
   - `add_key` and then `remove_key` on `bugs`.
5. **Lock, unlock and delete.**
   - `lock_user` `daffy` (pending, then applied), then `unlock_user`.
   - `delete_user` `daffy` while active is refused.
   - `lock_user` then `delete_user` `daffy`: `removing`, then gone from `users`.
   - `add_user` `daffy` again succeeds.
6. **Admins and invites** (self-service outcomes 1 to 15). The link is opened with curl and no bearer token, no credential at all, as the person invited would open it in a browser.
   - `create_invite` `acme` answers an invite with `admin` false, an `expires` 72 hours after its `created`, and a `link` that is the sandbox's accounts URL followed by `/join?token=` and the token. `list_invites` `acme` shows it without the link, and the landing row of `acme` counts one invite.
   - `GET` of the link shows the form naming `acme` and `acme.hosts.test`; a second `GET` shows it again, so opening uses nothing.
   - A `POST` naming `bugs` (taken), `www` (reserved) and `Bugs` (malformed) each get the one name answer, a `POST` with a bad key the key answer, and the link still shows the form after each.
   - A `POST` naming `elmer` with a fresh key answers that the user is created, with `elmer`, `acme` and `acme.hosts.test`. `users` shows `elmer` pending, then applied, not an admin. The link then answers the unusable page, 404.
   - Reuse: a second invite's link, submitted with `elmer` again, gets the name answer and stays usable.
   - Revoke: `revoke_invite` of that second invite answers it; its link then answers the same unusable page as a used one, and `revoke_invite` again is refused.
   - Admin: `create_invite` `acme` with `admin` true, then a `POST` naming `porky`: `users` shows `porky` an admin. `set_admin` `acme` `porky` false, then true, each shows on the landing's users list.
   - Rate limit: a burst of `POST`s to a usable link from the developer's machine, all from one address, ends in 429.
7. **Tools on the box** (self-service outcomes 16 to 26; the finish line's items 4 to 9, in the sandbox). The delivery drives the stdio server as `D16-sandbox` says: the sandbox's `accountsd` run under the name `accountsd-mcp`, with `ACCOUNTSD_SOCKET` naming a user's socket from `state/box/sockets.txt`, piped JSON-RPC lines or registered in `claude`.
   - As `porky`, an admin: `tools/list` offers the eleven box tools; `users` lists `acme`'s users; `create_invite` answers a link like the central one, and enrolling `taz` through it makes an ordinary user.
   - As `elmer`, not an admin: `tools/list` offers `keys`, `add_key` and `remove_key` only, and an admin tool is refused with the only-an-admin answer. `add_key` a second key, `remove_key` the first, then `remove_key` the last is refused with the last-key answer. `keys`, and centrally `users`, show each change once applied.
   - As `porky`: `lock_user` `elmer` is applied and `elmer`'s socket is gone; `unlock_user` brings both back. Locking, deleting and demoting `porky` are each refused with the cannot-yourself answer. `set_admin` `elmer` true, and `elmer`'s `create_invite` answers; false, and it is refused.
   - `revoke_invite` of a box-made invite answers it, and its link then answers the unusable page.
8. **Stop and start.**
   - `stop_host` `host01`: `stopping`, then `stopped`. Health keeps its last word.
   - `start_host`: `starting`, then `running`, with the same EIP and data volume and health back to `sync`.
   - The record at `host01.hosts.test` is the same one throughout, and the report after the start carries the same hostname.
9. **Resize.** `resize_host` `host01` to type `t4g.large` with `data_gb` 60 answers `restarts: true`, then `resizing`, then `running` with the new type and size, the same record and the same reported hostname.
10. **Backups and partial restores** (host-backups outcomes 15 to 23 and 37 to 41). The delivery writes lines to `state/sandbox-backups` under accounts' app directory (`D16-sandbox`); each line is gone at the sim's next look.
   - `host01 acme database data/app.db`: within a pass, `restore_points` `acme` lists `data/app.db`, healthy, with its oldest, written and synced times, beside the snapshot times newest first.
   - `restore_database` `acme` `data/app.db` at a time between its oldest time and now answers `pending`; it is `done` once applied, and the host page's restores list shows its copy beside the original, named for that time (`data/app.restored-<T>.db`). The same tool for a path `restore_points` does not list is refused as no such database, and for a time before the oldest as not restorable.
   - `restore_files` `acme` at a snapshot time `restore_points` lists answers `pending`, then `done` with its copy under `restored/<T>/`; with `user` `bugs`, under `bugs`'s home. At a time no snapshot has it is refused as no such snapshot.
   - `host01 acme fail`: within a pass `backup_status` shows `acme` `error`, and the landing's `host01` row and the host page flag it. `host01 acme heal`: `healthy` again after the next `Backup` pass (D14's `BackupTick`) and the report that follows it, within about two minutes, since a snapshot that failed is tried again then and not at the schedule's next (D15). `stale` needs the threshold's wait and is not walked; unit tests prove it with an injected clock.
11. **Close and open** (host-backups outcomes 24 to 27).
   - `close_host` `host01` answers `closing`, then the host is `running` with a `closed` time on `host` and the host page, and `backup_status` shows `acme`'s final backup `done`. While it closes and once closed, the simulated machine holds `acme`'s users locked, so their sockets leave `state/box/sockets.txt`, while `users` still answers each with its own stored state.
   - `close_host` `host01` again answers the host with no operation and changes nothing.
   - `open_host` `host01`: `opening`, then `running` with no `closed` time; the users are applied unlocked and their sockets are back.
12. **Replace** (host-backups outcomes 28 to 36).
   - `replace_host` `host01` `from` `volume`: `replacing`, then `running`, with the same name, EIP, record and data volume and a new instance; the users are applied as before, and `state/sandbox-seeds` gains no line for `acme`.
   - `replace_host` `host01` `from` `volume` with `data_gb` is refused as keeping the volume's size.
   - `replace_host` `host01` `from` `backup`: `replacing`, then `restoring` with `acme`'s restore pending on the host page and in `backup_status`, then `done`, then `running` once its users are admitted and applied, with the same name, EIP and record, a new instance and a new data volume; `restore_points` still lists `data/app.db`; `state/sandbox-seeds` gains no line for `acme`; and `kept_volumes` lists the old data volume with cause `replaced`.
13. **Restart mid-operation.** `create_host` again (`host02`). During its `provisioning`, run `systemctl --user restart sandbox-<name>-accounts`. Then `resize_host` `host01`'s root to 30 and restart again during `resizing`. Each operation resumes from its recorded step and settles. After it settles, the delivery counts the instances, volumes and addresses tagged `Host=<name>` in `state/sandbox-cloud.json` and expects exactly one instance, one data volume, one root volume and one address per host, and exactly one A record at each host's DNS name, holding its EIP; `host02`'s report carries `host02.hosts.test`. An extra resource the interrupted attempt made carries the running operation's `Operation` tag, so the stray scan treats it as explained; telemetry's lack of `resource.unexplained` alone proves nothing here.
14. **Removal guards.**
   - `delete_host` `host01` is refused while `acme` is on it.
   - `remove_account` `acme` is refused while it has users. Lock and delete them, then `remove_account` `acme`: `removing`, then gone. `backup_status` lists `acme` as `removed`, its backups left in place (host-backups outcome 43).
   - `create_account` `acme` on `host01` is refused: the name still has backups. `delete_account_backups` of `acme` answers `deleting`, and `acme` leaves `backup_status` once its keys are gone from the memory cloud. `create_account` `acme` is then accepted; `delete_account_backups` `acme` is now refused, since the account exists; and `remove_account` `acme` again, which has no users (host-backups outcomes 44 and 45).
   - `delete_host` `host01` with no `keep_data`: `deleting`, its `on_delete` `keep` in the answer, on `host` and on the host page, then hidden from `hosts` and the landing page. Its data volume stays in the memory cloud, tagged `Orphaned=true`, and its record at `host01.hosts.test` is gone from it, as `acme.hosts.test` is.
   - `delete_host` `host02` with `keep_data` false is refused while it is not closed (host-backups outcome 26). `close_host` `host02`, then `delete_host` `host02` with `keep_data` false: `deleting` with `on_delete` `delete`, then hidden. Restart accounts while it is `deleting` (its terminate takes 10 s); it resumes and finishes. The memory cloud then holds no volume, address or record of `host02`, no instance of it that is not terminated, and no key under `hosts/host02/` (host-backups outcome 46).
15. **Host numbers** (host-numbers outcomes 1 to 3). With no host live, `create_host` answers `host01` again, then `host02`. Delete `host01` with `keep_data` false and `lost` true, which needs no close, and, once it is hidden, `create_host` answers `host01` a third time, with a new record, instance, address and empty data volume, while `host02` keeps its own. While a host is still `deleting`, `create_host` skips its number. `host` for the new `host01` answers its own create as its operation, no report until its first, an empty `reason` and `on_delete`, and its own new `instance_id` and `data_volume`, none of the earlier `host01`'s.
16. **Kept volumes** (host-numbers outcomes 14 to 16).
   - `kept_volumes` lists the volume the replace kept in step 12, with cause `replaced`, and the first `host01`'s volume from step 14, with cause `deleted`, each with its id, the host's name, its size and when it was kept; the landing's kept-volumes list shows the same. `delete_kept_volume` of the replaced one answers `deleting`, then it is gone.
   - `delete_kept_volume` of the first `host01`'s volume answers `deleting`, then within about 10 s (5 s of `deleting` in the memory cloud and the engine's 5 s poll) it is gone from `kept_volumes` and from the memory cloud. Of it again, of a live host's data volume, and of an id the cloud never made, each is refused as no such kept volume.
   - Delete `host02` with no `keep_data`, then `delete_kept_volume` of its volume, and restart accounts while it is `deleting`: after the restart it finishes, and `kept_volumes` lists none.
   - `close_host` the remaining `host01`, then delete it with `keep_data` false: the memory cloud holds no host, no host volume and no key under `hosts/`.
17. **Telemetry.** telemetry's `catalog` and `search` for accounts show:
    - one `change.finished` per changing call, a box call's naming `ssh:<user>` as its user
    - a box-made invite's `invite.created` naming `ssh:<user>`, and the `invite.used` of its enrollment
    - `operation.started` and `operation.finished` for each operation, closes, opens, replaces and restores included, a delete's `data` saying `keep` or `delete`
    - `volume.changed` for each kept volume's change of state, `kept` to `deleting` under the caller of `delete_kept_volume` and `deleting` to `deleted` under the same caller
    - `host.changed` for each host state change
    - `account.changed` for each account state change
    - `invite.created`, `invite.revoked` and `invite.used` for each invite made, revoked and used, and `admin.changed` for each flag `set_admin` changed
    - nothing for a read, and nothing for a refused enrollment
    - no token, in any event: a `request.started` of a link shows the path `/join` alone, with no query
    - no credential, no restore path and no host's backup error text in any event (`D08-trail`)

Silence (`host.silent`) is not part of the walk, because a running sandbox host always reports. Unit tests prove it with an injected clock.

Neither is a taken host name: the walk has no way to put a record at a host's name before its create, since every name below the domain the walk can write is an account's and host names are reserved. Unit tests over the memory cloud prove a create failing with the record conflict and the scan healing a missing record and showing a conflicting one as the host's reason (`D10-operations`).

The real chain, AWS, real hosts, SSH and the space's nginx, is not in the sandbox. It is walked at the finish line.

## Verification

Each row is an outcome the walk checks and the documents that own it. A row naming nginx, auth, the sandbox, home, telemetry or the gateway is that service's behaviour, observed and not required here.

| Outcome the checks rely on | Owner |
|---|---|
| home lists accounts under core with its icon and description | the manifest (R-DJ7M-H4AO, R-APH7-IHIV; `D02-cli`; `D06-pages`); home |
| pages answer 200 for the developer and send a guest to sign in; `/mcp` without a credential is 401 | `D06-pages`, `D03-serve`; `guests = true` (R-DJ7M-H4AO); nginx and auth |
| `/events` and `/declarations` answer 404 | the fragment (R-M6FC-GN0B, R-M7N8-UER0) |
| `/accountsd` serves the binary to anyone; `/accountsd/v1/*` refuses a request without identity | `D13-daemon-endpoints`; routing `D03-serve` (R-GIAD-HJV5); binary path `D01-layout-and-run-seam` (R-Z1EE-DHAQ) |
| the sandbox runs on the memory cloud and simulated daemons, persisted across restarts | `D01-layout-and-run-seam` (R-YRN7-BBD6, R-YYYL-LXTC); `D03-serve` (R-GY52-GKI6); `D16-sandbox` |
| host states, health and the report page | `D10-operations`, `D04-store`, `D06-pages`, `D07-tools` |
| a host's DNS name on the page and in the tools, its record made at create, kept through stop, start and resize, and deleted with it | `D06-pages`, `D07-tools`, `D10-operations`, `D04-store`; the memory cloud `D16-sandbox` |
| a host is asked to take its DNS name and reports the hostname it has | `D11-protocol`, `D13-daemon-endpoints`, `D15-machine`; the domain's wiring `D03-serve`; the simulated host `D16-sandbox` |
| account placement, DNS name, reserved and taken names, host names reserved for accounts and users | `D07-tools`, `D17-names-keys-sizes`, `D04-store`, `D10-operations` |
| users pending, then applied; lock, unlock, delete, re-add | `D07-tools`, `D04-store`, `D13-daemon-endpoints`, `D14-accountsd`, `D15-machine` |
| invites made, listed, counted on the landing, revoked; the admin flag set and shown | `D07-tools`, `D04-store`, `D06-pages` |
| the enrollment page answers a guest: form, refusals, created user, unusable link, rate limit | `D18-enrollment`; routing `D03-serve` (R-HX9C-M9PO, R-FUH5-FVTH); template data `D06-pages`; names and keys `D17-names-keys-sizes` |
| the box tools: the tool list fits the tier, each user's own keys, an admin's users and invites, the box refusals, a box change landing as desired state | `D19-box-tools`, `D04-store`; the box path `D11-protocol` (R-YFOX-ORCE), `D13-daemon-endpoints`; wiring `D03-serve` (R-Z3DU-2772); the host side `D14-accountsd`, `D20-box-mcp`; the sandbox's sockets `D16-sandbox` |
| a box change's events name `ssh:<user>`, and a box-made invite's creator | `D08-trail`, `D19-box-tools`, `D04-store` |
| stop, start, resize with `restarts` | `D07-tools`, `D10-operations` |
| restart mid-operation resumes, with no duplicates and no strays | `D03-serve` (R-GT9G-XHJE, R-KRUV-BTB7); `D10-operations`; `D16-sandbox` |
| removal guards, unplace, delete and orphaned volume | `D04-store`, `D07-tools`, `D10-operations` |
| a new host takes the lowest free number, and a reused name carries nothing of the earlier host | `D04-store`, `D07-tools`, `D10-operations` |
| a delete with `keep_data` false leaves nothing of the host, resumes after a restart, and shows its choice | `D07-tools`, `D04-store`, `D10-operations`, `D06-pages`, `D08-trail`; the seam `D05-cloud` (R-7KQ3-HPFX); the memory cloud `D16-sandbox` (R-7JI7-3XP8, R-QO4W-BTK6, R-L5PS-7HX9) |
| kept volumes listed, on the page and by tool, and deleted, resuming after a restart | `D04-store`, `D07-tools`, `D10-operations`, `D06-pages`, `D08-trail`; the memory cloud `D16-sandbox` |
| trail events, and no token in the trail | `D08-trail`; emitters `D07-tools`, `D10-operations`, `D13-daemon-endpoints`, `D18-enrollment` |
| an account is backed up with no further step: health, snapshots, databases, the flag, the settings shown | `D04-store`, `D07-tools`, `D06-pages`; the credentials `D21-backup-access`, `D13-daemon-endpoints`, wiring `D03-serve` (R-PLAM-5U68); the seam `D05-cloud`; the host side `D11-protocol`, `D14-accountsd`, `D15-machine`; the sandbox's backups file `D16-sandbox`, wiring `D03-serve` (R-PMII-JLWX) |
| partial restores: database and files, refusals, copies named beside the original | `D07-tools`, `D04-store`, `D10-operations`, `D17-names-keys-sizes`, `D15-machine`, `D06-pages` |
| close and open: users locked and unlocked, the final backups, the closed time | `D07-tools`, `D04-store`, `D10-operations`, `D06-pages`, `D15-machine` |
| replace keeping the volume or from backup: same name and address, restored, admitted, not seeded again, the old volume kept as `replaced` | `D07-tools`, `D04-store`, `D10-operations`, `D05-cloud`, `D15-machine`; the memory cloud and machines `D16-sandbox` |
| a removed account's backups kept, the name refused until they are deleted; a host's key backups deleted with its data; a data-deleting delete needs a closed host or `lost` | `D07-tools`, `D04-store`, `D10-operations`, `D05-cloud`; the memory cloud `D16-sandbox`; wiring `D03-serve` (R-3W54-93QJ) |
| accounts reaches no sibling and runs no program | `D01-layout-and-run-seam` (R-7SSU-QP74, R-ETO0-MM7J) |

## Trust

- **People.** accounts believes the identity nginx sets for a person (`X-User-Id`, `X-User-Email`, `X-Request-Id`), as every sibling does. Every signed-in user of the space reads and changes every host, account and user, as themselves, and every change is recorded under the caller (`D08-trail`). That includes the admin flag, which any signed-in user may set or clear on any user (`set_admin`), and invites, which any signed-in user may make, admin invites included (self-service outcomes 4 and 10). The admin flag grants nothing on the space: it is what the box tools' admin tier checks (`D19-box-tools`). `guests = true` lets a request without a credential reach accounts' socket. The pages send it to sign in, `/mcp` never sees it, and only the daemon routes and the enrollment page, `/join?token=<token>`, answer it.
- **Invite holders.** The enrollment page believes nothing about who is asking: its one authority is the token in the link (`D18-enrollment`). Whoever holds an unused, unexpired, unrevoked link can create one user of its account, with the admin flag the invite carries, once; a signed-in person opening it is treated as a guest. The token is drawn from the run's random source and kept only as its hash (`D04-store`), answered once by `create_invite` and never by any other tool, page or event (`D07-tools`, `D08-trail`). A refused name, an unusable link and a rate-limited submission each say no more than their one answer (self-service outcome 13), and submissions are limited per address, the address being nginx's `X-Real-IP`, which opsctl's nginx and the sandbox's set from the connection. The token does reach the space's nginx access log, which is opsctl's and outside accounts: an accepted risk, bounded by single use, 72 hours and revocation (`D18-enrollment`).
- **Daemons.**
  - A daemon is believed only after its presigned `sts:GetCallerIdentity` request has been executed by accounts and answered by STS:
    - for accounts' own AWS account
    - for the hosts' role
    - with an instance id that maps to exactly one live host
  - The request is fresh, never replayed, and bound to accounts' binding (`D12-host-identity`).
  - The session name is trusted as the instance id because the hosts' role trusts only `ec2.amazonaws.com` (infra).
  - Customer users cannot obtain the role's credentials, because the host blocks IMDS for every uid but root (`D15-machine`).
  - What a forged report could do is bounded to its own host (`D04-store`).
- **Hosts' users.** A user on a host reaches the box tools through the host's daemon, which forwards to `/accountsd/v1/box` under the host's verified identity with the caller's uid (`D13-daemon-endpoints`). accounts resolves the verified host and the uid to an active user of an account placed on that host and never reads a name or an account from the host. Each call acts as that user, `ssh:<name>`, within that user's account only, and an admin tool checks in the same transaction as its write that the user is still an active admin (`D19-box-tools`, `D04-store`).
  - Accepted risk (self-service outcome 20): the host becomes a write path into accounts. Root on a host can forward any uid, so it can act as any admin of the accounts placed on that host. It could already do anything to those accounts' files and sessions. accounts never lets a host touch an account placed elsewhere.
- **The binary.** `/accountsd` serves, to anyone, the release's own daemon binary. It holds no secret: the daemon learns everything else from the verified protocol.
- **Backup credentials.** accounts assumes the backup role and hands each host, in its state answer, one credential set per active account on it and one for its own key backups, each limited by a session policy to that one prefix and expiring within the hour (`D21-backup-access`, `D13-daemon-endpoints`). They are never stored by accounts and never reach the trail (`D08-trail`); on the host only root can read them (`D15-machine`); the bucket's policy denies them any version delete, so a host can only hide an object, which stays for 35 days (infra); and they cannot make a daemon's identity header (`D12-host-identity`). Root on a host can read or hide the backups of the accounts placed on it, which it could already read and destroy on the host; it never reaches another host's accounts.
- **AWS.** accounts holds no secret of its own. Its credentials are the space role's, which the operator policy limits to the tag contract and to A records below the hosts domain, so AWS itself refuses anything outside it. Within that, accounts writes only accounts' and hosts' own records, creates a record only where none is, and deletes only one that is exactly its own (`D10-operations`). The policy allows deleting only a volume tagged `Kind=host-data`; within that, accounts deletes only a data volume its record explains, a host's own on a delete told not to keep it or a kept volume a caller names, never a stray (`D10-operations`, `D04-store`); a host's own only once the host holds no account that is not being removed (`D07-tools`). An AWS error's message text never enters the trail, only its service, operation, subject and code (`D05-cloud`).

## The finish line

The delivery carries accounts to `us`, as the decisions document's Finish line and Sequence settle it:

1. Build: the gap closed and the gates pass, including the `accountsd` arm64 build.
2. The sandbox walk above.
3. Land onto `main`, tag the next suite release, push, and deploy to `us`.

The preconditions above come first: infra applied, `devctl hosts set us`, opsctl's five keys set and `init` run.

On the space, with the space token and through the gateway, the delivery makes the eight checks of the decisions document's Finish line:

1. `host01` provisions to `running` and `sync`, with a full report.
2. `acme.<hosts domain>` resolves to `host01`'s EIP.
3. `bugs` and `daffy` show pending, then applied.
4. SSH works as each of them, with shared `/srv/acme` and private homes; `ec2-user` and passwords are refused.
5. Lock, unlock, delete and re-add.
6. Stop, start and resize keep the same EIP, data and host key.
7. Restarting accounts during a create and a resize, and restarting the daemon during an apply, resumes and settles with no orphaned resource.
8. SSM reaches the host, and `acme` and `bugs` are gone from `us`.

The self-service release's finish line is its decisions document's, and it adds no check to these.

The host-names release adds the eight checks of its own Finish line (host-names log P4, P5). They run on `us` after the next suite release is deployed, with no one having touched `host04` by hand, on `host04` (the one existing host, carrying `mgd`) and on a disposable host created and deleted through the gateway:

1. On `host04`, `hostname` prints `host04.metaspot.org`, and accounts shows the name as applied: the `host` tool's `report` and the host page's report carry `hostname` `host04.metaspot.org`, and the host's health is `sync`. This proves that an existing host takes the name through the daemon's normal update.
2. `host04.metaspot.org` resolves, from the developer's machine, to `host04`'s EIP, which the scan made with no operation run (`D10-operations`). `mgd.metaspot.org` resolves as before.
3. An existing user of `mgd` signs in as `<user>@mgd.metaspot.org` with no host key warning, and sudo is refused as before (host-names log P1) and the accounts tools work for them as before.
4. A new host is created through the gateway. The `host` tool shows its DNS name. When it first reports in sync its hostname is its full name, and that name resolves to its EIP.
5. The new host is rebooted, then stopped and started. The hostname and the record are the same after each.
6. Creating an account named like a host, such as `host09`, is refused (`D17-names-keys-sizes`).
7. The new host is deleted. Its record is gone and its name no longer resolves. The data volume `delete_host` keeps is deleted by hand (host-names log P4).
8. The `hosts` and `host` tools and the page show each host's DNS name.

The report gives what each check answered and the ids of every AWS resource left running.

The host-numbers release adds the nine checks of its own Finish line. They run on `us` after the next suite release is deployed and the `ec2:DeleteVolume` right is applied, starting with no host running and the kept data volumes the host-names delivery left (host-numbers Prerequisites), through the gateway:

1. `create_host` answers `host01`, though the database has given out higher numbers before (host-numbers outcome 4); it runs, reaches `sync`, and `host01.metaspot.org` resolves to its EIP.
2. A second `create_host` answers `host02`.
3. `delete_host` `host01` with `keep_data` false. Its instance, address, DNS record and data volume are gone, and the AWS account holds no volume tagged `Host=host01`.
4. `create_host` answers `host01` again: in sync, with a new empty data volume and no SSH or DNS trace of the earlier one.
5. accounts is restarted during a `keep_data` false delete; the delete resumes and finishes with nothing left behind.
6. `delete_host` `host02` with no `keep_data`. Its data volume remains, tagged `Orphaned=true`.
7. `kept_volumes` lists `host02`'s volume and those the host-names delivery left that the record explains (host-numbers log P2: a volume the record does not explain stays a stray, is reported and is not deleted by hand); `delete_kept_volume` of each, and the list is empty.
8. `delete_host` of the remaining `host01` with `keep_data` false. The AWS account holds no host and no host volume the record explains.
9. A full setup is left standing for the owner: `create_host` answers `host01`; `mgd` is placed on it; `mike` is added as its admin with a key generated at `~/.ssh/id_ed25519_host01_mike`; a `mgd-mike` entry is added to `~/.ssh/config` (host-numbers log P5); `ssh mike@mgd.metaspot.org` works from the developer's machine.

The report gives what each tool answered and the ids of every AWS resource left running.

The host-backups release adds the twelve checks of its own Finish line. They run on `us` after the next suite release is deployed and infra's `b9ab479a` is applied, through the gateway, on a throwaway host and a throwaway account on it with a user the delivery creates, and on `mg` on `host01`, which the delivery only lets start being backed up (it does not close, replace, delete, stop or resize `host01`, nor restore into `mg`):

1. After `create_account`, with no other step, `backup_status` shows the account healthy.
2. A file in the account folder, a file in the user's home and the folder's git history are in a snapshot; a row written to a database under `data/` is streamed within seconds.
3. A row deleted at a known time is present in a copy `restore_database` makes as of just before it.
4. A deleted file comes back under `restored/` from an earlier snapshot through `restore_files`, with its owner and permissions.
5. `close_host` ends the user's session, refuses a new sign-in and reports every final backup done; `open_host` lets the user back in.
6. `replace_host` keeping the volume brings the host back with the same name, address and host key, and all its data.
7. `replace_host` from backup, the old data volume kept, brings the host back with files, homes, owners, permissions, ACLs, git history and databases, and the same host key, so signing in gives no warning; the account is not seeded again.
8. The host's credentials cannot read or write another account's prefix and cannot remove a hidden copy; the user cannot read the credentials.
9. With backups deliberately broken, the account is flagged.
10. After `remove_account`, `create_account` refuses the name until `delete_account_backups` has run.
11. `mg` is backed up and shows healthy, and `ssh mike@mg.metaspot.org` still works.
12. The throwaway host, account, user and kept volume are removed, and what was added to this machine's SSH setup for them is removed.

The report gives what each tool answered and the evidence for each item.

No test can make the following checks, so they are made here:

- an SSH session's processes sit in `acct-<account>-<user>.slice` with the slice's properties: on `host01`, `/proc/<pid>/cgroup` of a signed-in session's shell, and of a background child it started, names a cgroup at or below `/acct.slice/<AccountSlice(a)>/<PersonSlice(a, u)>/`, `a` the account and `u` the user, in every one of at least 30 sign-ins with and without a terminal, with no process of theirs left in a logind `session-<n>.scope`, so the limits of the account's and the person's slice files bound it (`D15-machine`; the PAM mechanism is unproven until here, coordinator decision Q2; the seam requirements prove only the files, the link and `Session`'s command)
- IMDS is unreachable as a customer user and reachable as root (`D15-machine`)
- `getent passwd` and `getent group` on the launched host list no name outside `valid.SystemNames` (`D17-names-keys-sizes`) other than the accounts and users accounts placed, so the reserved system names cover every account and group the image, SSM and EC2 create
- real STS accepts the daemons' presigned headers bound to the binding
- the space's nginx passes `/accountsd` under `guests = true`
- opsctl activates a release whose `bin/` holds `accountsd`
- the accounts unit reaches IMDS and AWS
- on the hosts' image, `hostnamectl set-hostname` with a full dotted name sets the static hostname that `/etc/hostname` holds, and the cloud-init drop-in holding `preserve_hostname: true` keeps it across a reboot (`D15-machine`; host-names checks 1, 4 and 5). This mechanism is checked only here. The scratch host of host-names "What was observed" showed, in its own terms, that a full dotted name set as the static hostname worked on the hosts' image and survived a reboot and a stop and start, and that with cloud-init told to preserve the hostname it survived a reboot after cloud-init's saved state was removed; the finish line does not repeat the loss of saved state
- Route 53 accepts the host's record under the operator policy, and the name resolves (host-names checks 2 and 4)
- EC2 accepts `DeleteVolume` of a volume tagged `Kind=host-data` under the operator policy, and the volume is gone from `DescribeVolumes` once deleted (host-numbers checks 3, 7 and 8); the memory cloud's `deleting` and its refusals only mirror this (`D16-sandbox`)
- under the operator role, `DeleteVolume` and `DescribeVolumes` of a volume that no longer exists answer `InvalidVolume.NotFound` and not `UnauthorizedOperation`; the observation `D05-cloud` cites was made with the owner's SSO session (host-numbers checks 3, 5 and 7)
- real STS answers `AssumeRole` of the backup role with an inline session policy, from accounts' role, with credentials expiring within the hour; a role it may not assume answers `AccessDenied` or `AccessDeniedException` (`D05-cloud`, `D21-backup-access`; host-backups check 1)
- STS's `PackedPolicySize` for the longest session policy accounts writes, an account policy for a 32-byte slug and the host policy for the longest host name, is under 100 percent: AWS reports the packed size only in its answer and it cannot be computed locally, while the plaintext bound is `D21-backup-access`'s requirement (host-backups checks 1 and 6)
- a real credential set issued for the longest policy, its `AccessKeyID`, `SecretAccessKey` and `SessionToken` together, is within `daemonapi.MaxCredentialBytes` (`D13-daemon-endpoints`), since the state answer zeroes a larger set and AWS publishes no bound on a session token (host-backups check 1)
- real S3 accepts restic's `init`, `backup`, `forget --prune`, `check`, `snapshots` and `restore`, and Litestream's `replicate`, `ltx`, `status` and `restore`, under issued credentials whose `s3:ListBucket` is conditioned on `s3:prefix`, and refuses them another account's prefix and any version delete (`D15-machine`; host-backups checks 2 to 4, 6 to 8)
- Litestream picks up renewed credentials at each restart of its unit, and a database streamed across several reissues stays synced (`D15-machine`; host-backups check 2)
- `DeleteObjects` answers a per-key error in the shape `D05-cloud` maps, and S3 answers the codes of `D05-cloud`'s class table as it lists them (`NoSuchBucket`, `SlowDown`, `RequestTimeout`, `AccessDenied`, `ExpiredToken`), under accounts' role, for `create_account`'s name check, `delete_account_backups` and a delete's `delete_backups` (host-backups checks 10 and 12)
- restic converges when a run outlives its one-hour credentials: an interrupted backup's uploaded data is reused by the next run, which restic 0.20.0's documents do not state (`D15-machine`; host-backups plan pick 17). It is checked by `mg`, an existing account with real data, reaching healthy (host-backups check 11); if it does not, the delivery stops and reports
- a whole-host restore of the throwaway host, its account's files and databases, finishes within one credential lifetime, or resumes across runs, before `RestoreAttempts` fail it (`D15-machine`; host-backups check 7)

## Recorded decisions

- **The manifest's contents are stated here,** byte for byte. `cli.Manifest`, its printing and `etc/manifest.toml`'s equality with it are `D02-cli`'s.
- **`guests = true`** (coordinator decision Q1). This is a departure from outcome 21's wording and from accounts' previous `guests = false`. The precedent is webhooks and sites. The reasons are above.
- **`memory_max = "256M"`.** A departure from `128M` (ledger, connections, accounts before), for the reasons above.
- **`secrets = []` and no `[env]`.** Credentials come from the instance role. The hosts keys are opsctl's to write, and reserved.
- **`[home] group = "core"`** is kept.
- **`cli.NginxConf` is unchanged.** The daemon paths need no location of their own under `guests = true`. The box path, `/accountsd/v1/box`, is one of them: it rides `location /` like every `/accountsd/` path, which the route table hands to the daemon endpoints (R-GIAD-HJV5), and its largest body, `protocol.MaxBoxRequest` (64 KiB), is under nginx's documented default `client_max_body_size` of 1m, which the fragment does not set. The manifest is unchanged too. A fragment that proxied them past auth would need a proxy target that is not portable across opsctl and the sandbox.
- **What ships, the preconditions and the finish line are prose.** Only a real release, a real AWS account and a real host could observe them, and no permitted test reaches those.
- No release, space, host name, zone or template is named in the manifest, the fragment or any requirement.
- **The host-names release adds no requirement here.** Its walk and its checks are prose, as the rest of the finish line is, and none of the four requirements below is changed by it: the manifest is unchanged, since the release adds no tool, no setting and no secret, and so is the fragment.
- **The operator policy needs no change for host names.** The operator role already allows a host's record (host-names "What was observed"). infra's `iam.tf` is precedent for why: its right on the hosts zone is A records matching `*.<hosts domain>`, less the site names, which would admit `<host>.<hosts domain>` as it admits `<account>.<hosts domain>`. The exact scope is left to the finish line's check that Route 53 accepts the host's record under the operator policy. The comments in infra that said only account names are written are corrected by the delivery (host-names log P3).
- **The host-numbers release adds no requirement here.** Its walk, its precondition and its checks are prose. The manifest is unchanged: two new tools change what the gateway lists, not the manifest's text, and there is no new setting or secret. The fragment is unchanged.
- **The host-backups release adds no requirement here.** Its preconditions, its compatibility with the release before, its walk and its checks are prose. The manifest is unchanged (R-DJ7M-H4AO): eight new tools change what the gateway lists, not the manifest's text; there is no new setting, secret, environment key or event; and `memory_max = "256M"` holds, since the S3 and STS clients are small and every new operation holds a page, one report body or one credential set per account at a time. The fragment is unchanged. The tool count, thirty-six, is stated here only in prose; `D07-tools` states it as a requirement (R-GPMX-YWDP).
- **Rollback costs of the host-backups release are accepted** (host-backups plan pick 16), on the precedent of the host-numbers release's `delete_data`; they are listed under Releases above. Rejected: keeping the new kinds in `operations`, which would fail a host whose hours-long restore a rollback caught.
- **The protocol's `backup` member is a recorded departure, like `domain`** (`D11-protocol`): absence means no backups, and the daemon ships with accounts.
- **The operator policy gains `ec2:DeleteVolume` on `Kind=host-data` volumes, and nothing wider** (host-numbers Sequence step 1, log P1). One right serves both a delete told not to keep the data and `delete_kept_volume`. It overturns the ssh-hosts rule that no tool deletes a data volume (host-numbers outcome 15), as the reuse of host numbers overturns the rule that a host name is never reused (host-numbers outcome 6): a host's number is free again once its delete has finished (`D04-store`).

## Consumer usage

An operator's path, in order (infra applied first, `b9ab479a` for the host-backups release):

1. `devctl hosts set us`.
2. `opsctl config set` of `aws.region` and the four `hosts.*` keys.
3. Deploy. opsctl writes the env file with the five keys, generates accounts' server with `/check/open` on `location /` and `etc/nginx.conf` included, and starts the unit.
4. accounts starts in AWS mode.

An agent then calls `create_host` through the gateway (task 1). An instance's first boot fetches `https://accounts.<space host>/accountsd` through `location /` (task 2).

Tests of this document:

- One compares `cli.Manifest` with the string it builds from the stated lines and `pages.Description`.
- One checks `pages.Description` with `utf8.ValidString` and rune by rune.
- One reads `nginx.conf` from `accounts.Etc()` with `fs.ReadFile` and compares it with `cli.NginxConf`.

## REQUIREMENTS

- R-DJ7M-H4AO: The value of `cli.Manifest` (`D02-cli`) MUST be exactly `app = "accounts"\ndescription = "` followed by the text of `pages.Description` (`D06-pages`) followed by `"\ndefault = false\nmcp = true\nguests = true\nsecrets = []\n\n[database]\nengine = "sqlite"\npath = "state/accounts.db"\n\n[resources]\nmemory_max = "256M"\n\n[home]\ngroup = "core"\n`, each `\n` denoting one line feed and each `"` one double quote.
- R-APH7-IHIV: `pages.Description` (`D06-pages`) MUST be non-empty, be valid UTF-8 as the standard library's `utf8.ValidString` reports, and hold no `"`, no `\` and no rune for which the standard library's `unicode.IsControl` is true; so that, written between the quotes of the manifest's description line (R-DJ7M-H4AO), its TOML value is exactly `pages.Description`.
- R-M6FC-GN0B: The `internal/cli` package MUST export `const NginxConf = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"`, each `\n` denoting one line feed.
- R-M7N8-UER0: The contents of the file `nginx.conf` in the file system the root package's `Etc` returns (`D01-layout-and-run-seam`) MUST be exactly the bytes of `cli.NginxConf`.
