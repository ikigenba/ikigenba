# D09-on-a-space

What sessions contributes when it runs on a space and in a sandbox: what ships, what the manifest says and what a space takes from it, the nginx fragment `cli.NginxConf` and the file under `etc/` that copies it, the working directory, the environment file and the services file entries it uses, the walk the delivery makes in the sandbox, the trust sessions places in what reaches it, and the finish line. Almost every step between a checkout and an agent's tool call belongs to someone else: devctl builds and ships the release, opsctl installs it and generates nginx, auth decides every credential, the host's nginx turns away every request without one and sets the identity headers, the gateway relays tool calls, telemetry keeps the trail, home lists the app, and the `turns` command, a sibling with its own design, reads each person's session files and pushes their turns through the gateway. sessions' own share is what `D01-layout-and-run-seam` to `D08-trail` state, plus the manifest's contents and the fragment, which this document states. Outcomes cited by number are the decisions document /home/mgreenly/Documents/ikigenba-sessions.md; decisions cited as D-numbers are its log, /home/mgreenly/Documents/ikigenba-sessions-decisions.md.

This document states three things: the contents of `cli.Manifest`, which `D02-cli` declares; the constant `cli.NginxConf`, which `D01-layout-and-run-seam` assigns to it; and that `etc/nginx.conf` holds the fragment's bytes. Everything else it says of a space or a sandbox is behaviour the other documents own; the table under Verification names the owner of each outcome.

## What ships

The release holds four members of sessions' directory: `bin/sessions`, `etc/manifest.toml`, `etc/nginx.conf` and `share/icon.svg`, the last Tabler's `clock-play` outline, human-authored and already in the tree; `devctl build` packs it beside `bin/` and `etc/`, and home and the banner's trail show it. The two files under `etc/` are what the root package's `Etc` embeds (`D01-layout-and-run-seam`): `etc/manifest.toml` holds the bytes of `cli.Manifest` (`D02-cli`) and `etc/nginx.conf` the bytes of `cli.NginxConf` (this document). The templates and the migration ship inside the binary (`D01-layout-and-run-seam`, `Assets` and `Migrations`; the migration's bytes, with the seeded rules, prices and away limit, `D04-store`), and the database is not in the release: sessions creates `state/sessions.db` under its working directory on its first start (`D01-layout-and-run-seam`). The tarball, its member list and its name are devctl's, and no test opens the icon or reads the checkout, so the release's members and the icon are outside what a requirement can state; a developer checks them with `tar`.

The `turns` command does not ship with sessions: devctl packs it beside opsctl as a tool the release carries (D5, D12), and nothing of it is in sessions' directory or this design.

## The manifest

The manifest is a program-parsed file: opsctl, the sandbox, devctl and home read it, so its text is contract and this document states it byte for byte, and bounds the description so that TOML reads it as itself (both under REQUIREMENTS). `D02-cli` declares the constant `cli.Manifest`, says that `sessions manifest` prints it and that `etc/manifest.toml` holds the same bytes.

In order, it says: the app is `sessions`; its description is `pages.Description` (`D06-pages`), the line the about screen shows and the host publishes in its services file for the gateway's instructions, spelled between the quotes so that the two cannot drift, which is why the requirement also keeps the constant free of anything TOML would read as other than itself; it is not the default app (`default = false`); it is an MCP service (`mcp = true`), so the gateway offers its tools (`D07-tools`), and the `turns` command reaches `push_turns` through the gateway's relay as any agent does (D4); it admits no guests (`guests = false`); it holds no secrets (`secrets = []`), since it calls nothing outside the suite and authenticates to nothing; a `[database]` table naming the `sqlite` engine and `state/sessions.db`, which the host keeps and replicates; a `[resources]` table whose only line is `memory_max = "128M"`, which bounds the unit; and, last, a `[home]` table whose only line is `group = "core"`.

**Why `core`.** home draws its `core` group apart from the `application` group, and D3 settled `core` for sessions. sessions is the suite's own record of how the space is worked through agents, filled by a command the suite ships and installs on every host, its timer enabled for each account created there once the release is in place and pushing only once that person stores a token (D5, D6, D14, P2), as telemetry is the suite's record of its requests; both sit in `core`. It is not a business application like ledger, candidates or campaigns, whose subject is a business's own records and dealings with others. `[resources]` names no `slice`, so the unit runs in the apps' slice like accounts and connections: `core` in `[home]` is where home lists it, not where systemd places it.

**Why `128M`.** sessions' work is bounded: a push carries at most `MaxPushTurns` turns over at most `MaxPushSessions` sessions (`D07-tools`), a session's tree is read in one bounded query, and a recompute after a rule, price or away-limit change walks the root sessions in pages of `BatchSize` (`D04-store`), never holding the whole table. That is the shape of ledger's, accounts' and connections' work, which run in `128M`.

**What the manifest leaves out.** No `[env]` table: sessions has no setting of its own beyond the space's `DRAIN_SECONDS` (`D01-layout-and-run-seam`), and the away limit, the rules and the prices are records in its database, changed through tools (`D07-tools`), never environment. No `[events]` and nothing about the bus: sessions emits no bus event and accepts none (`D01-layout-and-run-seam`), and the manifest format has no place for either. No port: sessions serves on the socket the host passes it. Nothing names a space, a release or a host name.

**What `guests = false` means.** The host's nginx (opsctl's) and the sandbox's put every request to sessions' host name through auth's `/check`, to the pages, to `/_appkit/` and to `/mcp` alike, and admit only one that carries a credential: a page asked without one is sent to sign in by nginx and auth, and `/mcp` without one is answered `401`, before anything reaches sessions. The pages still send a guest to sign in themselves, through `identity.Optional`, should one reach the socket (`D03-serve`, `D06-pages`), and `/mcp` is wrapped by `identity.Require` (`D03-serve`). So every check the walk makes with a credential is answered by sessions, and every one without by nginx and auth. A pusher with no token, as on a host whose person has stored none (P2), is turned away at the gateway and changes nothing here.

## The nginx fragment

`cli.NginxConf` holds back the suite's two internal bus paths, `/events` and `/declarations`, at the public name with two exact-match locations answering 404, the pair most of the suite's apps carry. sessions serves neither as a route of its own (`D03-serve`): the events app asks every enabled service on the host for `/declarations` over its socket and takes an answer other than 200 as declaring nothing, and sessions accepts no delivery. The fragment makes the public answer at those two paths the suite's, whatever sessions' socket answers there, so a later release that joins the bus, say to emit a figure when a week closes, exposes nothing by the change. Nothing else is held back: `/mcp` is guarded by nginx's own locations, and every other path is gated by nginx under `guests = false` and guards itself besides. sessions has no path of its own that must be public or must be hidden: no webhook, no tracking link, no download. opsctl and the sandbox include `etc/nginx.conf` inside sessions' server.

## On a space

sessions on a space is the same binary on the same terms as under a test: a socket passed as descriptor 3 (`/run/ikigenba/sessions.sock`), `/var/opt/ikigenba/sessions` as its working directory, so that its database is `/var/opt/ikigenba/sessions/state/sessions.db`, its environment from `/etc/opt/ikigenba/sessions/env`, which opsctl writes and which holds `DRAIN_SECONDS`, and `IKIGENBA_SERVICES` naming the host's services file. Who supplies each is opsctl's; what sessions does with them is `D01-layout-and-run-seam`'s and `D03-serve`'s. It is served at `sessions.<host>`, the space's own host name; nothing names a space, and the DNS name is the space's.

Of the services file it uses `auth`, for the banner's links and a guest's sign-in address (`D06-pages`), and `telemetry`, appkit's sink for the trail (`D08-trail`). It reaches no sibling and nothing outside the suite (`D01-layout-and-run-seam`); it needs no outbound access and holds no secret. A missing entry never refuses the start: without `auth` the addresses are built on the request's scheme and host, and without `telemetry` the writer behaves as appkit's does. The host must run telemetry and auth, and the gateway for the `turns` command to reach it.

**What reaches it.** Turns arrive only in a `push_turns` call, from the `turns` command on a person's laptop or on the host itself, relayed by the gateway with that person's personal access token (outcomes 12 and 13, D4), after a `get_latest_turns` call tells the pusher what is already held; sessions reads no file of any agent tool and never learns which machine a push came from beyond the host the push names, the short hostname kept as given. A session is known by its tool and its id together (`D04-store`), so two tools' sessions never collide on an id. Agents read the figures through the same gateway, and every signed-in person reads the landing page (P6, D1).

**Backup and restore.** The host's backup archives `state/` whole, so `sessions.db` is taken with it: the sessions and their turns, the rules, the prices, the away limit and the stored summaries (`D04-store`). It holds no text of any prompt, answer, thought, tool call or tool result, save each session's first command and its argument (outcome 2), and no secret. After a restore the summaries are those the database held, consistent with its turns and tables, and a pusher resumes from the `latest` that `get_latest_turns` answers, so turns pushed after the backup are pushed again on its next run.

**Releases.** sessions ships in the next suite release (outcome 17). There is no previous release of sessions, so the first migration has no predecessor to run beside; the rolling rule for later releases is `D04-store`'s.

## In a sandbox

The sandbox builds sessions from the worktree, gives it `DRAIN_SECONDS` and `IKIGENBA_SANDBOX` (set to the sandbox's name, never in production; nothing in sessions turns on it, since it has no host and no stand-in to choose), includes `etc/nginx.conf` in sessions' server, and, the manifest refusing guests, routes every request through auth's `/check`, as the host does. The database, under the sandbox's data directory, survives `down` and `up`, and starts with the migration's seeded rules, prices and away limit (`D04-store`).

An agent is curl on the developer's machine: the pages at `http://sessions.<sandbox>.localhost:<port>/` with `Authorization: Bearer $(sandbox token)`, and the tools through the gateway or at sessions' `/mcp` with the same token. Every step signs in; a step without the token is answered by nginx and auth, never by sessions. The `turns` command's own walk against the sandbox is its delivery's, not this one's.

### The walk

With the sandbox up from the worktree, the delivery walks, as the developer, with the stored token, through the gateway's tools or sessions' `/mcp` and with curl. Sessions, hosts, directories and moments are ones the delivery composes for the walk; no real session file is read.

1. Before any change: home, `GET /`, `/tools` and `/about` with the token and without it, `POST /mcp` without a credential, `/events` and `/declarations` at the public name, and the gateway's `describe` of sessions.
2. `list_rules`, `list_prices` and `get_away_limit`: the seeded rules, prices and limit; `list_sessions`: none; `get_latest_turns` for the two sessions step 3 pushes: neither has a `latest`; `total_sessions` by week for the current week grouped by kind: one period with no groups.
3. `push_turns` with two entries: a root `claude` session, interactive, on host `ld01` in the directory `/mnt/projects/ikigenba/sessions`, which the seeded `build` rule for `/mnt/projects/ikigenba` matches, with a `/deliver` command, carrying person, agent and tool turns over an hour with one gap longer than the away limit and a priced model; and a `codex` session, headless, whose parent is the pair of the root's tool and id, with agent turns of an unpriced model. The answer gives each entry's tool, id and `latest`; `get_latest_turns` for the same two pairs answers the same, and for a `grok` session with the root's id, none. Then a push entry with no turns for a pair never pushed: no `latest`, and nothing written.
4. The same batch again: the same answer and the same figures. One agent turn again with larger counts: the tokens and dollars move, and nothing else.
5. `push_turns` refused, each with nothing written: an unknown tool, an unknown whose, a relative directory, a parent equal to the entry's own pair, a parent with an empty tool or id, a negative count, a turn past `MaxPushTurns`; and `get_latest_turns` refused for an empty list, an unknown tool, an empty id and more than `MaxLatestSessions` entries.
6. `list_sessions`: one row, the root, kind `build`, one subagent, attention with the long gap's person turn capped at the away limit, wall, agent time, the four token sums and their total, dollars and the subagent's tokens unpriced; filtered by `kind`, `tool`, `person`, `host` and `command`; a second root pushed under another directory that no rule matches, then `limit` 1 following `next` to the end.
7. `total_sessions` by week for the current week grouped by `kind`, `person`, `tool` and `command`; by month; a range of weeks, oldest first with empty weeks included; `to` before `from` refused; more than `MaxPeriods` refused.
8. `add_rule` on `ld01` for `/mnt/projects/ikigenba/sessions` naming `plan`: the root moves to `plan` with no push; again, refused as held; `add_rule` naming a kind outside the six, refused; `remove_rule` it: back to `build`; again, refused as absent.
9. `set_price` for the subagent's model: unpriced falls to zero and dollars rise; `remove_price` it: back; again, refused as absent.
10. `set_away_limit` to 5: attention falls; to 0 and to 1441, refused; back to 15.
11. `GET /` again, `/tools`, `/about` and an unknown path, with the token.
12. telemetry's `catalog` and `search` for sessions.

A push for a session held under another person needs a second person's token, which the sandbox does not hold; it is proved by tests, and on the space only if a second person pushes.

## Verification

Each row is an outcome the walk checks and the documents that own it. A row naming nginx, auth, the sandbox, home, telemetry or the gateway is that service's behaviour, observed and not required here. Owners are cited by document.

| Outcome the checks rely on | Owner |
|---|---|
| home lists sessions under core, with its icon and description | `[home] group = "core"` and the description in the manifest (this document; `cli.Manifest` `D02-cli`; `pages.Description` `D06-pages`); the icon in `share/` (What ships); the listing: home and the sandbox's or opsctl's services file |
| `GET /`, `/tools` and `/about` with the token answer 200 with the page drawn for the developer, the release in the footer; an unknown path answers sessions' not-found page | `D06-pages`; routing `D03-serve`; the footer's release `D01-layout-and-run-seam` |
| the same paths without a credential are sent to sign in by nginx and auth, and nothing reaches sessions | `guests = false` (this document's manifest); nginx and auth's `/check`; the pages' own redirect `D06-pages`, `D03-serve` |
| `POST /mcp` without a credential answers 401 and nothing reaches sessions | nginx and auth; were it to reach sessions, `D03-serve` |
| `/events` and `/declarations` at the public name answer 404, with or without the token | the fragment `cli.NginxConf` and `etc/nginx.conf` (this document), included by the sandbox's and opsctl's nginx |
| the gateway's `describe` lists sessions' tools in catalog order with their effects, and a call's result carries sessions' `serverInfo` | `D07-tools`; mounting `D03-serve`; `D01-layout-and-run-seam`; the relay: the gateway |
| a fresh database answers the seeded rules, prices and away limit, and no session | `D04-store`; `D07-tools` |
| a push and `get_latest_turns` answer each pair's latest moment, a repeated push changes nothing, a re-pushed turn's counts are corrected, and an invalid batch is refused whole with nothing written | `D07-tools`; `D04-store` |
| a listed root carries its kind, attention under the away limit, wall, agent time, tokens, dollars and unpriced tokens, with its subagent rolled up; filters and paging hold | `D07-tools`; `D04-store`; `D05-figures` |
| totals by week and month, grouped by kind, person, tool and command, oldest first with empty periods, and their refusals | `D07-tools`; `D04-store`; `D05-figures` |
| a rule added or removed moves the matching sessions' kind with no push; a price set or removed moves dollars and unpriced; an away limit set moves attention; each refusal leaves everything as it was | `D07-tools`; `D04-store`; `D05-figures` |
| `GET /` shows nine weeks by kind with the untagged share and the away limit, and nothing per person | `D06-pages`; `D04-store`; `D01-layout-and-run-seam` |
| telemetry's `catalog` and `search` for sessions show one event per write, carrying no directory of a session, no command argument and no text of any turn, and none for a read | `D08-trail`; which call records `D07-tools` |
| sessions reaches no sibling: telemetry shows no `sibling.called` | `D01-layout-and-run-seam`; `D08-trail` |

That a test proves each owner's requirement over fakes on loopback sockets, and never over a real telemetry, nginx or gateway, is the run's; the walk is the delivery's, and no requirement states what the real services answer.

## Trust

sessions believes the identity it is sent, as every sibling does: `X-User-Id`, `X-User-Email` and `X-Request-Id` as nginx or the gateway set them on a request to its socket (`D03-serve`). It asks auth nothing. Every signed-in user of the space reads every person's sessions and changes the rules, the prices and the away limit, as themselves (D1), and every change is recorded in the trail under the user id the request names (`D08-trail`). A session is the person's whose credential first pushed it, and a push for it under another user id is refused (`D04-store`), so one person cannot add turns to another's session through the gateway. A process that can reach sessions' socket can therefore name any user and push or change anything a tool does; every service on the host is trusted, and nginx and the gateway are the only doors from outside, nginx admitting no request without a credential.

What a push says is believed: the host, the directory, the tool, the moments, the models and the counts are the pusher's reading of the person's own files, and sessions checks only their form (`D07-tools`). sessions holds no secret; its database holds turns without content, each session's first command and argument, and the tables.

## The finish line

The delivery carries sessions to `us` as the decisions document's Finish line settles it: build (the gap closed and the gates pass), the sandbox walk above, land onto `main`, tag the next suite release, push, and deploy. With `turns` installed on the owner's laptop and every session on it pushed, through the gateway with the space token: `list_sessions` and `total_sessions` for the week of 2026-10-13 answer the figures of the `/deliver` session rsi cycle one measured, with its Codex subagents rolled up, its wall agreeing with the cycle file's; a second push changes nothing; `add_rule` moves the matching sessions to their kind without a push; `set_away_limit` moves the figures; and `GET /` on `sessions.<host>` and home show the current week by kind. This is where the real pushes from session files are proved; no test proves them.

## Recorded decisions

- **The manifest's contents are stated here,** byte for byte, and `cli.Manifest`, its printing and `etc/manifest.toml`'s equality with it are `D02-cli`'s. It is accounts' manifest with sessions' name and database path.
- **`[home] group = "core"`** (D3): sessions is the suite's own record of how the space is worked, like telemetry, not a business application. `[resources]` keeps the default `apps` slice.
- **No `[env]`:** the away limit, rules and prices are records changed through tools, and `DRAIN_SECONDS` is the space's.
- **`guests = false`:** sessions has no guest route; the pusher signs in with a personal token (outcome 13).
- **`secrets = []`:** sessions calls nothing outside the suite.
- **`memory_max = "128M"`,** as accounts, ledger and connections: every unit of work is bounded by a push's limits or a recompute's page.
- **`cli.NginxConf` is the suite's held-back pair,** `/events` and `/declarations`, though sessions serves neither, so the public answer at those paths is the suite's and a later bus membership exposes nothing. The alternative, an empty fragment, would have the same effect today; `D01-layout-and-run-seam` requires the file to exist either way.
- **Departure from accounts:** no host command, no sudo rule, no in-memory host and no reading of `IKIGENBA_SANDBOX`; sessions changes nothing on the host, so its sandbox and its space differ only in their data.
- No release, space or host name is named in the manifest, the fragment or any requirement.

## Consumer tasks

An operator installs the release; opsctl reads `etc/manifest.toml`, writes `DRAIN_SECONDS` into `/etc/opt/ikigenba/sessions/env`, generates sessions' server with auth's `/check` in front of every path and `etc/nginx.conf` included, adds sessions' entry with its icon and the `core` group to the services file, and starts the unit; the start creates and migrates the database, seeded rules, prices and away limit included, with no step of the operator's. A person's `turns` timer then calls `get_latest_turns` through the gateway's `call` and `push_turns` through its `mutate`, and an agent calls `total_sessions` through `call`.

A test compares `cli.Manifest` with the string it builds from the stated lines and `pages.Description`; another checks `pages.Description` with `utf8.ValidString` and rune by rune for what the description's requirement excludes. A test reads `nginx.conf` from the root package's `Etc()` with `fs.ReadFile` and compares it with `cli.NginxConf`; it imports `cli` and references the constant, so it compiles only if the constant exists.

## REQUIREMENTS

- R-G06B-CRJ9: The value of `cli.Manifest` (`D02-cli`) MUST be exactly `app = "sessions"\ndescription = "` followed by the text of `pages.Description` (`D06-pages`) followed by `"\ndefault = false\nmcp = true\nguests = false\nsecrets = []\n\n[database]\nengine = "sqlite"\npath = "state/sessions.db"\n\n[resources]\nmemory_max = "128M"\n\n[home]\ngroup = "core"\n`, each `\n` denoting one line feed and each `"` one double quote.
- R-G1E7-QJ9Y: `pages.Description` (`D06-pages`) MUST be non-empty, be valid UTF-8 as the standard library's `utf8.ValidString` reports, and hold no `"`, no `\` and no rune for which the standard library's `unicode.IsControl` is true; so that, written between the quotes of the manifest's description line (the manifest's requirement above), its TOML value is exactly `pages.Description`.
- R-G3U0-I2RC: The `internal/cli` package MUST export `const NginxConf = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"`, each `\n` denoting one line feed.
- R-G51W-VUI1: The contents of the file `nginx.conf` in the file system the root package's `Etc` returns (`D01-layout-and-run-seam`) MUST be exactly the bytes of `cli.NginxConf`.
