# D09-on-a-space

What accounts contributes when it runs on a space and in a sandbox: what ships, what the manifest says and what a space takes from it, the nginx fragment `cli.NginxConf` and the file under `etc/` that copies it, the working directory, the environment file and the services file entries it uses, the host command a space gives it and the precondition that command stands on, the in-memory host a sandbox gives it, the walk the delivery makes in the sandbox, the trust accounts places in what reaches it, and the finish line. Almost every step between a checkout and an agent's tool call belongs to someone else: devctl builds and ships the release, opsctl installs it, initializes the host and generates nginx, auth decides every credential, the host's nginx turns away every request without one and sets the identity headers, the gateway relays tool calls, telemetry keeps the trail, home lists the app, and opsctl, run through sudo, makes every change on the host. accounts' own share is what `D01-layout-and-run-seam` to `D08-trail` state, plus the manifest's contents and the fragment, which this document states. Outcomes cited by number are the decisions document /home/mgreenly/Documents/ikigenba-accounts.md.

This document states three things: the contents of `cli.Manifest`, which `D02-cli` declares; the constant `cli.NginxConf`, which `D01-layout-and-run-seam` assigns to it; and that `etc/nginx.conf` holds the fragment's bytes. Everything else it says of a space or a sandbox is behaviour the other documents own; the table under Verification names the owner of each outcome.

## What ships

The release holds four members of accounts' directory: `bin/accounts`, `etc/manifest.toml`, `etc/nginx.conf` and `share/icon.svg`, the last Tabler's `users` outline, human-authored and already in the tree; `devctl build` packs it beside `bin/` and `etc/`, and home and the banner's trail show it. The two files under `etc/` are what the root package's `Etc` embeds (`D01-layout-and-run-seam`, R-C7K6-723O): `etc/manifest.toml` holds the bytes of `cli.Manifest` (`D02-cli`) and `etc/nginx.conf` the bytes of `cli.NginxConf` (this document). The templates and the migration ship inside the binary (`D01-layout-and-run-seam`, R-C6C9-TACZ, R-C8S2-KTUD; the migration's bytes `D04-store`, R-PDCM-AJ5R), and the database is not in the release: accounts creates `state/accounts.db` under its working directory on its first start (`D01-layout-and-run-seam`, R-CIJ9-MZRX, R-CJR6-0RIM). The tarball, its member list and its name are devctl's, and no test opens the icon or reads the checkout, so the release's members and the icon are outside what a requirement can state; a developer checks them with `tar`.

## The manifest

The manifest is a program-parsed file: opsctl, the sandbox, devctl and home read it, so its text is contract and this document states it byte for byte (R-E307-S9VA), and bounds the description so that TOML reads it as itself (R-E484-61LZ). `D02-cli` declares the constant `cli.Manifest`, says that `accounts manifest` prints it and that `etc/manifest.toml` holds the same bytes.

In order, it says: the app is `accounts`; its description is `pages.Description` (`D06-pages`), the line the about screen shows and the host publishes in its services file for the gateway's instructions, spelled between the quotes so that the two cannot drift, which is why the requirement also keeps the constant free of anything TOML would read as other than itself; it is not the default app (`default = false`); it is an MCP service (`mcp = true`), so the gateway offers the thirteen tools (`D07-tools`); it admits no guests (`guests = false`); it holds no secrets (`secrets = []`), since it calls nothing outside the suite and authenticates to nothing; a `[database]` table naming the `sqlite` engine and `state/accounts.db`, which the host keeps and replicates; a `[resources]` table whose only line is `memory_max = "128M"`, which bounds the unit; and, last, a `[home]` table whose only line is `group = "core"`.

**Why `core`.** home draws its `core` group apart from the `application` group. accounts is platform infrastructure: it manages the space's host itself, the accounts, groups and folders people reach the space by, as repos holds the suite's repositories and connections the suite's grants, and both of those sit in `core`. It is not a business application like ledger, candidates or campaigns, whose subject is a business's own work. A business's folder lives on the host accounts manages, not inside accounts. `[resources]` names no `slice`, so the unit runs in the apps' slice like repos and connections: `core` in `[home]` is where home lists it, not where systemd places it.

**What the manifest leaves out.** No `[env]` table: accounts' only setting of its own, `ACCOUNTS_HOST_COMMAND`, must stay unset on a host so that the default applies, and unset in a sandbox so that the in-memory host is chosen (below); a value written from the manifest into the environment would override both. `DRAIN_SECONDS` is the space's, as for every app. No `[events]` and nothing about the bus: accounts emits no bus event and accepts none (`D01-layout-and-run-seam`), and the manifest format has no place for either. No port: accounts serves on the socket the host passes it. Nothing names a space, a release or a host name.

**What `guests = false` means.** The host's nginx (opsctl's) and the sandbox's put every request to accounts' host name through auth's `/check`, to the pages, to `/_appkit/` and to `/mcp` alike, and admit only one that carries a credential: a page asked without one is sent to sign in by nginx and auth, and `/mcp` without one is answered `401`, before anything reaches accounts. The pages still send a guest to sign in themselves, through `identity.Optional`, should one reach the socket (`D03-serve`, `D06-pages`), and `/mcp` is wrapped by `identity.Require` (`D03-serve`). So every check the walk makes with a credential is answered by accounts, and every one without by nginx and auth.

## The nginx fragment

`cli.NginxConf` holds back the suite's two internal bus paths, `/events` and `/declarations`, at the public name with two exact-match locations answering 404, the pair most of the suite's apps carry. accounts serves neither as a route of its own (`D03-serve`): the events app asks every enabled service on the host for `/declarations` over its socket and takes an answer other than 200 as declaring nothing, and accounts accepts no delivery. The fragment makes the public answer at those two paths the suite's, whatever accounts' socket answers there, so a later release that joins the bus exposes nothing by the change. Nothing else is held back: `/mcp` is guarded by nginx's own locations, and every other path is gated by nginx under `guests = false` and guards itself besides. opsctl and the sandbox include `etc/nginx.conf` inside accounts' server.

## On a space

accounts on a space is the same binary on the same terms as under a test: a socket passed as descriptor 3 (`/run/ikigenba/accounts.sock`), `/var/opt/ikigenba/accounts` as its working directory, so that its database is `/var/opt/ikigenba/accounts/state/accounts.db`, its environment from `/etc/opt/ikigenba/accounts/env`, which opsctl writes and which holds `DRAIN_SECONDS` and neither `ACCOUNTS_HOST_COMMAND` nor `IKIGENBA_SANDBOX`, and `IKIGENBA_SERVICES` naming the host's services file. Who supplies each is opsctl's; what accounts does with them is `D01-layout-and-run-seam`'s and `D03-serve`'s. It is served at `accounts.<host>`, the space's own host name; nothing names a space, and the DNS name is the space's (outcome 23).

Of the services file it uses `auth`, for the banner's links and a guest's sign-in address (`D06-pages`), and `telemetry`, appkit's sink for the trail (`D08-trail`). It reaches no sibling and nothing outside the suite (`D01-layout-and-run-seam`, R-CNEV-62QP); it needs no outbound access and holds no secret. A missing entry never refuses the start: without `auth` the addresses are built on the request's scheme and host, and without `telemetry` the writer behaves as appkit's does. The host must run telemetry and auth.

**The host command.** With `ACCOUNTS_HOST_COMMAND` unset and no `IKIGENBA_SANDBOX`, as on every space, accounts runs `host.DefaultCommand` (`D05-host`, R-HQO5-SWWO, R-I432-0E2B; `D01-layout-and-run-seam`, R-DAKY-FPTW, R-PJ40-PVCP): `sudo -n` with the installed `opsctl` at `/usr/local/bin/opsctl`, the path opsctl's published README says activating a release links it at, and the arguments of each command, the grammar, outputs and exit statuses `D05-host` relies on. Both are reached only through their published interfaces, as the design format's "Depending on an external tool" allows, and no test runs either: every test runs a stub in their place (accounts/AGENTS.md, "The host is a stub").

**The precondition: the sudo rule.** accounts relies on the host having been initialized by an opsctl that lays down a sudo rule letting the unprivileged account every app runs as run `opsctl group …` and `opsctl user …` as root without a password, and nothing else (outcomes 16 and 22). The rule is opsctl's to write and the host's to hold; accounts never writes, reads or checks it, and no requirement here states it, since only a real sudo could observe it and no test runs one. What accounts does without it follows from sudo's documented behaviour: sudo(8) says `-n` makes sudo exit with an error rather than prompt when a password would be needed, and that sudo exits 1 when there is a configuration or permission problem or it cannot run the command. So on a host with no rule every changing tool and every list answers failed, `D05-host`'s `*host.FailedError` for status 1 (R-IGA1-U3H9), and nothing is recorded (`D07-tools`); it is never mistaken for a refusal, which only opsctl's statuses 2 and 4 carry. The unit must also leave sudo able to gain privilege, so opsctl's unit for accounts must not set `NoNewPrivileges`; that too is opsctl's.

**Backup and restore.** The host's backup archives `state/` whole, so `accounts.db` is taken with it: the groups and users accounts created, with their created times and creators, and no key, membership, state or secret (`D04-store`). The host's accounts, homes and folders are the host's, not accounts'. After a restore the record may hold a group or user the host no longer has, which no list shows, or miss one the host has, which lists show with no created time (`D04-store`, `D07-tools`).

**Releases.** accounts ships in the next suite release (outcome 25). There is no previous release of accounts, so the first migration has no predecessor to run beside; the rolling rule for later releases is `D04-store`'s (R-XKOP-IEAG).

## In a sandbox

The sandbox builds accounts from the worktree, gives it `DRAIN_SECONDS` and `IKIGENBA_SANDBOX` (set to the sandbox's name, never in production), includes `etc/nginx.conf` in accounts' server, and, the manifest refusing guests, routes every request through auth's `/check`, as the host does. With `ACCOUNTS_HOST_COMMAND` unset and `IKIGENBA_SANDBOX` present, accounts reaches the in-memory host `host.NewMemory()` returns (`D01-layout-and-run-seam`, R-CPUN-XM83, R-DAKY-FPTW; `D05-host`, R-HXZK-3JCU), so no sudo and no opsctl run on the developer's machine. That host follows opsctl's interface (names, refusals, states) and holds nothing when the run starts; every user's last sign-in is never (R-IZSF-YFCD). The record, under the sandbox's data directory, survives `down` and `up`; the in-memory host does not, so after a restart the record's groups and users are not listed until made again, and are then listed with the first run's created times (`D04-store`, R-OXHX-BIIQ, R-P4TB-M4YW).

An agent is curl on the developer's machine: the pages at `http://accounts.<sandbox>.localhost:<port>/` with `Authorization: Bearer $(sandbox token)`, and the tools through the gateway or at accounts' `/mcp` with the same token. Every step signs in; a step without the token is answered by nginx and auth, never by accounts.

### The walk

With the sandbox up from the worktree, the delivery walks, as the developer, with the stored token, through the gateway's tools or accounts' `/mcp` and with curl. Keys are fresh ed25519 OpenSSH public keys the delivery generates for the walk.

1. Before any change: home, `GET /`, `/tools` and `/about` with the token and without it, `POST /mcp` without a credential, `/events` and `/declarations` at the public name, and the gateway's `describe` of accounts.
2. `groups` and `users`: none of either.
3. `create_group` `acme`; again, refused as a name taken; `create_group` with a name that is not valid; `group` `acme`.
4. `add_user` `alice` to `acme` with a key; `add_user` with a key that is not ed25519; `add_user` `bob` to `acme`; `add_user` with the name `acme`.
5. `add_key` a second key to `alice`; `remove_key` both, the second answering that no key is left; `add_key` one back.
6. `create_group` `beta`; `add_member` `alice` to `beta`; `remove_member` `alice` from `beta`; `remove_group` `beta`.
7. `remove_group` `acme`, refused while it has members.
8. `delete_user` `bob`, refused while active; `lock_user` `bob`; `delete_user` `bob`; `lock_user` then `unlock_user` `alice`.
9. `groups` and `users` with `limit` 1, following `next` to the end; `group` `acme`.
10. `GET /` again, `/tools`, `/about` and an unknown path, with the token.
11. telemetry's `catalog` and `search` for accounts.

The real chain through sudo and opsctl is not in the sandbox; it is walked on the space at the finish line.

## Verification

Each row is an outcome the walk checks and the documents that own it. A row naming nginx, auth, the sandbox, home, telemetry or the gateway is that service's behaviour, observed and not required here. Ids are cited where this document's inputs fix them; the rest are cited by document.

| Outcome the checks rely on | Owner |
|---|---|
| home lists accounts under core, with its icon and description | `[home] group = "core"` and the description in the manifest (R-E307-S9VA, R-E484-61LZ; `cli.Manifest` `D02-cli`; `pages.Description` `D06-pages`); the icon in `share/` (What ships); the listing: home and the sandbox's or opsctl's services file |
| `GET /`, `/tools` and `/about` with the token answer 200 with the page drawn for the developer, the release in the footer; an unknown path answers accounts' not-found page | `D06-pages`; routing `D03-serve`; the footer's release `D01-layout-and-run-seam` (R-PHW4-C3M0) |
| the same paths without a credential are sent to sign in by nginx and auth, and nothing reaches accounts | `guests = false` (R-E307-S9VA); nginx and auth's `/check`; the pages' own redirect `D06-pages`, `D03-serve` |
| `POST /mcp` without a credential answers 401 and nothing reaches accounts | nginx and auth; were it to reach accounts, `D03-serve` |
| `/events` and `/declarations` at the public name answer 404, with or without the token | the fragment (R-M6FC-GN0B, R-M7N8-UER0), included by the sandbox's and opsctl's nginx |
| the gateway's `describe` lists the thirteen tools in catalog order with their effects, and a call's result carries accounts' `serverInfo` | `D07-tools`; mounting `D03-serve`; `D01-layout-and-run-seam` (R-DJ49-440R); the relay: the gateway |
| the sandbox's tools reach the in-memory host, empty at the start | `D01-layout-and-run-seam` (R-CPUN-XM83); `D05-host` (R-IOTC-IHO4) |
| `create_group` makes the group; again it is refused as a name taken with the host's diagnostic and records nothing; an invalid name is refused with the host's diagnostic | `D07-tools`; `D04-store` (R-XEL7-LJKZ); `D05-host` (R-9VCK-2CEE, R-D9PT-VML3) |
| `add_user` makes the user active in the group with the key; a bad key and a taken name are refused with the host's diagnostic | `D07-tools`; `D04-store` (R-XFT3-ZBBO); `D05-host` (R-IYKJ-KNLO, R-IR95-A15I) |
| `add_key` and `remove_key` change the user's keys; removing the last answers that no key is left and keeps the user | `D07-tools`; `D05-host` (R-J10C-C732, R-J288-PYTR) |
| `add_member`, `remove_member` and `remove_group` of an empty group succeed; `remove_group` of a group with members is refused | `D07-tools`; `D04-store` (R-OZXQ-3204); `D05-host` (R-J3G5-3QKG, R-SXVL-IZ6F) |
| `delete_user` of an active user is refused; lock, then delete succeeds; lock and unlock change the state and keep groups and keys | `D07-tools`; `D04-store` (R-P794-DOGA); `D05-host` (R-SZ3H-WQX4, R-J4O1-HIB5) |
| `groups` and `users` page by name with `next`; `group` lists its members sorted | `D07-tools`; `D05-host` (R-IUWU-FCDL, R-IZSF-YFCD, R-IXCN-6VUZ) |
| `GET /` shows the groups with their members and created times and the users with their state and an empty last sign-in | `D06-pages`; `D01-layout-and-run-seam` (R-PGO7-YBVB) |
| telemetry's `catalog` and `search` for accounts show one change event per changing call, naming the tool, the developer, the group or user and the result, and none for a read | `D08-trail`; which call records `D07-tools` |
| accounts reaches no sibling: telemetry shows no `sibling.called` | `D01-layout-and-run-seam` (R-CNEV-62QP); `D08-trail` |

That a test proves each owner's requirement over a stub host and fakes on loopback sockets, and never over a real sudo, opsctl, telemetry, nginx or gateway, is the run's; the walk is the delivery's, and no requirement states what the real services answer.

## Trust

accounts believes the identity it is sent, as every sibling does: `X-User-Id`, `X-User-Email` and `X-Request-Id` as nginx or the gateway set them on a request to its socket (`D03-serve`). It asks auth nothing. Every signed-in user of the space reads and changes every group and user, as themselves (outcome 1), and every change is recorded in the trail under the user id the request names (`D08-trail`). A process that can reach accounts' socket can therefore name any user and make any change a tool makes; every service on the host is trusted, and nginx and the gateway are the only doors from outside, nginx admitting no request without a credential.

What opsctl answers is believed: its lists are the truth the tools and the page show, and its first diagnostic line is relayed unaltered (`D05-host`). The privilege accounts uses is exactly what the sudo rule grants: opsctl's group and user commands, which validate every name and key themselves, so accounts holds no privilege of its own and does no validation of its own. Every app on a host runs as the same account, so the rule is reachable from any app's process; that is the host's arrangement and the same trust every service on the host already has. accounts holds no secret; its database holds names, times and creators, and public keys never enter it.

## The finish line

The delivery carries accounts to the space the decisions document names, after the sandbox walk, as its Finish line settles it: build (the gap closed and the gates pass), the sandbox walk above, land onto `main`, tag the next suite release, push, apply the infra change, and deploy, the host initialized by the release's opsctl so the sudo rule is in place before the first change. On the space, with the space token and through the gateway: `create_group` and `add_user` with the owner's test key succeed and `GET /` on `accounts.<host>` and home show both; the rest of the walk, `lock_user`, `unlock_user` and `delete_user` included, is repeated against the real host. This is where the real chain through sudo and opsctl is proved; no test proves it.

## Recorded decisions

- **The manifest's contents are stated here,** byte for byte, and `cli.Manifest`, its printing and `etc/manifest.toml`'s equality with it are `D02-cli`'s. It is ledger's manifest with accounts' name, its database path and `[home] group = "core"` in place of `application`.
- **`[home] group = "core"`,** the brief's settled point 8: accounts is platform infrastructure managing the space's host, like repos and connections, not a business application. `[resources]` keeps the default `apps` slice.
- **No `[env]`,** so that the default host command applies on a space and the in-memory host in a sandbox; a manifest value would override both.
- **`guests = false`** (settled point 7): accounts has no guest route.
- **`memory_max = "128M"`,** as ledger and connections.
- **`cli.NginxConf` is the suite's held-back pair,** `/events` and `/declarations`, though accounts serves neither, so the public answer at those paths is the suite's and a later bus membership exposes nothing. The alternative, an empty fragment, would have the same effect today; `D01-layout-and-run-seam` requires the file to exist either way.
- **The host command default and the sandbox switch are `D01-layout-and-run-seam`'s and `D05-host`'s,** cited here, not restated. The sudo rule is an external precondition, stated in prose and never tested.
- No release, space or host name is named in the manifest, the fragment or any requirement.

## Consumer tasks

An operator initializes the host with the release's opsctl, which lays down the sudo rule, and installs the release; opsctl reads `etc/manifest.toml`, writes `DRAIN_SECONDS` into `/etc/opt/ikigenba/accounts/env`, generates accounts' server with auth's `/check` in front of every path and `etc/nginx.conf` included, adds accounts' entry with its icon and the `core` group to the services file, and starts the unit; the start creates and migrates the database with no step of the operator's. An agent then calls `create_group` through the gateway, and accounts runs `sudo -n` with opsctl's `group add`.

A test compares `cli.Manifest` with the string it builds from the stated lines and `pages.Description`; another checks `pages.Description` with `utf8.ValidString` and rune by rune for what R-E484-61LZ excludes. A test reads `nginx.conf` from the root package's `Etc()` with `fs.ReadFile` and compares it with `cli.NginxConf`; it imports `cli` and references the constant, so it compiles only if the constant exists.

## REQUIREMENTS

- R-E307-S9VA: The value of `cli.Manifest` (`D02-cli`) MUST be exactly `app = "accounts"\ndescription = "` followed by the text of `pages.Description` (`D06-pages`) followed by `"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[database]\nengine = "sqlite"\npath = "state/accounts.db"\n\n[resources]\nmemory_max = "128M"\n\n[home]\ngroup = "core"\n`, each `\n` denoting one line feed and each `"` one double quote.
- R-E484-61LZ: `pages.Description` (`D06-pages`) MUST be non-empty, be valid UTF-8 as the standard library's `utf8.ValidString` reports, and hold no `"`, no `\` and no rune for which the standard library's `unicode.IsControl` is true; so that, written between the quotes of the manifest's description line (R-E307-S9VA), its TOML value is exactly `pages.Description`.
- R-M6FC-GN0B: The `internal/cli` package MUST export `const NginxConf = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"`, each `\n` denoting one line feed.
- R-M7N8-UER0: The contents of the file `nginx.conf` in the file system the root package's `Etc` returns (`D01-layout-and-run-seam`) MUST be exactly the bytes of `cli.NginxConf`.
