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

Outcomes cited as outcome N are those of /home/mgreenly/Documents/ikigenba-ssh-hosts.md, and log entries those of /home/mgreenly/Documents/ikigenba-ssh-hosts-decisions.md. Self-service outcome N is outcome N of /home/mgreenly/Documents/ikigenba-self-service-accounts.md.

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

The two files under `etc/` are what the root package's `Etc` embeds (`D01-layout-and-run-seam`, R-C7K6-723O). The templates and the migrations ship inside the binary (`D01-layout-and-run-seam`, R-7QD1-Z5PQ and R-7RKY-CXGF; the migrations' bytes are `D04-store`'s). The database is not in the release: accounts creates `state/accounts.db` on its first start, and in a sandbox also `state/sandbox-cloud.json` (`D01-layout-and-run-seam`, R-ESG4-8UGU and R-Y9CP-KR8R).

The tarball, its member list and its name are devctl's, and no test reads the checkout. So the release's members are outside what a requirement can state; a developer checks them with `tar`.

opsctl's `activate` used to refuse a `bin/` holding more than `<app>`. Under log D11 it now accepts extra regular files there; that is opsctl's change, already landed, and a precondition of this release.

## The manifest

opsctl, the sandbox, devctl and home read the manifest, so its text is contract and this document states it byte for byte (R-DJ7M-H4AO). It also bounds the description so that TOML reads it as itself (R-APH7-IHIV).

In order, the manifest says:

- the app is `accounts`
- its description is `pages.Description` (`D06-pages`, new words)
- it is not the default app
- it is an MCP service, so the gateway offers the twenty-six tools (`D07-tools`)
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
- **`devctl hosts set`** on the operating space (outcome 5), which gives its role the operator policy.
- **opsctl writes the five keys** (log D9) and accepts extra files in `bin/` (log D11).
- **The space's accounts unit can reach IMDS and AWS's regional endpoints** for EC2, STS and Route 53. Its unit hardening is opsctl's, and the finish line checks it.

**Backup and restore.** The host's backup archives `state/` whole, so `accounts.db` goes with it: the hosts, operations, accounts, users, keys and latest reports (`D04-store`). After a restore the record may lag AWS. Operations resume against what AWS holds, by their recorded steps and lookups by tag (`D10-operations`). A resource no restored record explains is reported as unexplained, never deleted (`D10-operations`).

**Releases: expand only** (outcome 27). This release adds `0002_hosts.sql` and stops reading or writing the old `groups` and `users` tables of `0001`, but drops nothing (`D04-store`). It stops calling opsctl: no `sudo`, no `opsctl group|user` (`D01-layout-and-run-seam`, R-ETO0-MM7J). It changes nothing in opsctl. The SSH bans release adds `0003_bans.sql`, new tables and indexes only, and the release before it runs on that schema (`D04-store`). The self-service release adds `0004_invites.sql` the same way: an admin flag is a row of a new table rather than a new column of the users' table, so the release before it runs on that schema unchanged (`D04-store`); a user enrolled through an invite is an ordinary user to the old release, and the admin flag never reaches a host, so the daemon protocol does not change.

The next release contracts. It drops accounts' old tables, and opsctl removes the shell group and its sshd rules, the pooled limits, the sudo rule for accounts, and its `group` and `user` commands. The daemon protocol changes by expand then contract too (`D11-protocol`).

## In a sandbox

The sandbox builds accounts from the worktree and gives it `DRAIN_SECONDS` and `IKIGENBA_SANDBOX`, and no hosts key. It also builds `cmd/accountsd` into `<data>/bin/accountsd` beside `bin/accounts`, for the developer's architecture and still an ELF, and starts nothing for it (log D11, D12). It includes `etc/nginx.conf` in accounts' server. Because the manifest admits guests, it routes `location /` through auth's `/check/open` and `/mcp` through `/check`, as a space does.

Sandbox mode (`D01-layout-and-run-seam`):

- accounts runs on `D16-sandbox`'s memory cloud, kept in `state/sandbox-cloud.json` under its app directory.
- The memory cloud enforces the tag contract and takes observable time over each transition.
- One simulated daemon (`D16-sandbox`) runs in process for every running instance. It speaks the real protocol, through the real verifier, to accounts' own handler, and applies to an in-memory machine.

No AWS call and no real host change is made on the developer's machine.

What survives a restart:

- The record and the memory cloud survive `down`, `up` and an accounts restart, so an interrupted operation resumes against what the fake already made.
- Simulated machines do not survive. Their next cycle re-applies the whole desired state, as a real daemon does after a reboot.
- `sandbox wipe` removes everything.

An agent is curl on the developer's machine:

- the pages at `http://accounts.<sandbox>.localhost:<port>/`, with `Authorization: Bearer $(sandbox token)`
- the tools through the gateway or at accounts' `/mcp`, with the same token

### The walk

The sandbox is up from the worktree. The delivery walks it as the developer, with the stored token, through the gateway's tools or accounts' `/mcp`, and with curl. Keys are fresh ed25519 OpenSSH public keys the delivery generates.

The timings are `D16-sandbox`'s: about 40 s of `provisioning`, 5 s for a DNS change, and 3 s per apply.

1. **Before any change.**
   - home lists accounts.
   - `GET /`, `/tools` and `/about` are fetched with the token, and without it (sent to sign in).
   - `POST /mcp` without a credential gets 401 from nginx.
   - `/events` and `/declarations` answer 404.
   - The gateway's `describe` of accounts lists the twenty-six tools.
   - `curl` of `/accountsd` without a token gets bytes equal to `<data>/bin/accountsd`.
   - `GET /accountsd/v1/state` without the identity header is answered 401 by accounts.
2. **Create a host.**
   - `hosts` lists none.
   - `create_host` with no argument answers `host01`, `provisioning`.
   - `host` shows `host01`, `/hosts/host01` shows the host page, and the landing row shows health `none`.
   - After about 40 s the host is `running`, and within a cycle its health is `sync` with a full report on `/hosts/host01`.
3. **Place an account.**
   - `create_account` `acme` on `host01` answers `pending` and the DNS name `acme.hosts.test`. It turns `active` after the DNS change and one apply.
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
9. **Resize.** `resize_host` `host01` to type `t4g.large` with `data_gb` 60 answers `restarts: true`, then `resizing`, then `running` with the new type and size.
10. **Restart mid-operation.** `create_host` again (`host02`). During its `provisioning`, run `systemctl --user restart sandbox-<name>-accounts`. Then `resize_host` `host01`'s root to 30 and restart again during `resizing`. Each operation resumes from its recorded step and settles. After it settles, the delivery counts the instances, volumes and addresses tagged `Host=<name>` in `state/sandbox-cloud.json` and expects exactly one instance, one data volume, one root volume and one address per host. An extra resource the interrupted attempt made carries the running operation's `Operation` tag, so the stray scan treats it as explained; telemetry's lack of `resource.unexplained` alone proves nothing here.
11. **Removal guards.**
   - `delete_host` `host01` is refused while `acme` is on it.
   - `remove_account` `acme` is refused while it has users. Lock and delete them, then `remove_account` `acme`: `removing`, then gone.
   - `delete_host` `host02` and `host01`: `deleting`, then hidden from `hosts` and the landing page. Their data volumes stay in the memory cloud, tagged `Orphaned=true`.
12. **Telemetry.** telemetry's `catalog` and `search` for accounts show:
    - one `change.finished` per changing call, a box call's naming `ssh:<user>` as its user
    - a box-made invite's `invite.created` naming `ssh:<user>`, and the `invite.used` of its enrollment
    - `operation.started` and `operation.finished` for each operation
    - `host.changed` for each host state change
    - `account.changed` for each account state change
    - `invite.created`, `invite.revoked` and `invite.used` for each invite made, revoked and used, and `admin.changed` for each flag `set_admin` changed
    - nothing for a read, and nothing for a refused enrollment
    - no token, in any event: a `request.started` of a link shows the path `/join` alone, with no query

Silence (`host.silent`) is not part of the walk, because a running sandbox host always reports. Unit tests prove it with an injected clock.

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
| account placement, DNS name, reserved and taken names | `D07-tools`, `D17-names-keys-sizes`, `D04-store`, `D10-operations` |
| users pending, then applied; lock, unlock, delete, re-add | `D07-tools`, `D04-store`, `D13-daemon-endpoints`, `D14-accountsd`, `D15-machine` |
| invites made, listed, counted on the landing, revoked; the admin flag set and shown | `D07-tools`, `D04-store`, `D06-pages` |
| the enrollment page answers a guest: form, refusals, created user, unusable link, rate limit | `D18-enrollment`; routing `D03-serve` (R-HX9C-M9PO, R-FUH5-FVTH); template data `D06-pages`; names and keys `D17-names-keys-sizes` |
| the box tools: the tool list fits the tier, each user's own keys, an admin's users and invites, the box refusals, a box change landing as desired state | `D19-box-tools`, `D04-store`; the box path `D11-protocol` (R-YFOX-ORCE), `D13-daemon-endpoints`; wiring `D03-serve` (R-AIMF-81PV); the host side `D14-accountsd`, `D20-box-mcp`; the sandbox's sockets `D16-sandbox` |
| a box change's events name `ssh:<user>`, and a box-made invite's creator | `D08-trail`, `D19-box-tools`, `D04-store` |
| stop, start, resize with `restarts` | `D07-tools`, `D10-operations` |
| restart mid-operation resumes, with no duplicates and no strays | `D03-serve` (R-GT9G-XHJE, R-KRUV-BTB7); `D10-operations`; `D16-sandbox` |
| removal guards, unplace, delete and orphaned volume | `D04-store`, `D07-tools`, `D10-operations` |
| trail events, and no token in the trail | `D08-trail`; emitters `D07-tools`, `D10-operations`, `D13-daemon-endpoints`, `D18-enrollment` |
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
- **AWS.** accounts holds no secret of its own. Its credentials are the space role's, which the operator policy limits to the tag contract, so AWS itself refuses anything outside it. An AWS error's message text never enters the trail, only its service, operation, subject and code (`D05-cloud`).

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

No test can make the following checks, so they are made here:

- an SSH session's processes sit in `acct-<account>-<user>.slice` with the slice's properties: on `host01`, `/proc/<pid>/cgroup` of a signed-in session's shell, and of a background child it started, names a cgroup at or below `/acct.slice/<AccountSlice(a)>/<PersonSlice(a, u)>/`, `a` the account and `u` the user, in every one of at least 30 sign-ins with and without a terminal, with no process of theirs left in a logind `session-<n>.scope`, so the limits of the account's and the person's slice files bound it (`D15-machine`; the PAM mechanism is unproven until here, coordinator decision Q2; the seam requirements prove only the files, the link and `Session`'s command)
- IMDS is unreachable as a customer user and reachable as root (`D15-machine`)
- `getent passwd` and `getent group` on the launched host list no name outside `valid.SystemNames` (`D17-names-keys-sizes`) other than the accounts and users accounts placed, so the reserved system names cover every account and group the image, SSM and EC2 create
- real STS accepts the daemons' presigned headers bound to the binding
- the space's nginx passes `/accountsd` under `guests = true`
- opsctl activates a release whose `bin/` holds `accountsd`
- the accounts unit reaches IMDS and AWS

## Recorded decisions

- **The manifest's contents are stated here,** byte for byte. `cli.Manifest`, its printing and `etc/manifest.toml`'s equality with it are `D02-cli`'s.
- **`guests = true`** (coordinator decision Q1). This is a departure from outcome 21's wording and from accounts' previous `guests = false`. The precedent is webhooks and sites. The reasons are above.
- **`memory_max = "256M"`.** A departure from `128M` (ledger, connections, accounts before), for the reasons above.
- **`secrets = []` and no `[env]`.** Credentials come from the instance role. The hosts keys are opsctl's to write, and reserved.
- **`[home] group = "core"`** is kept.
- **`cli.NginxConf` is unchanged.** The daemon paths need no location of their own under `guests = true`. The box path, `/accountsd/v1/box`, is one of them: it rides `location /` like every `/accountsd/` path, which the route table hands to the daemon endpoints (R-GIAD-HJV5), and its largest body, `protocol.MaxBoxRequest` (64 KiB), is under nginx's documented default `client_max_body_size` of 1m, which the fragment does not set. The manifest is unchanged too. A fragment that proxied them past auth would need a proxy target that is not portable across opsctl and the sandbox.
- **What ships, the preconditions and the finish line are prose.** Only a real release, a real AWS account and a real host could observe them, and no permitted test reaches those.
- No release, space, host name, zone or template is named in the manifest, the fragment or any requirement.

## Consumer usage

An operator's path, in order:

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
