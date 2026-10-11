# D10-on-a-space

What artifacts contributes when it runs on a space and in a sandbox, and the one guarantee no earlier document owns whole: no byte of an artifact's body reaches anything artifacts records or writes beyond its blob directory and the answers that serve the bytes. It follows webhooks' on-a-space design.

## What ships

The release holds four members of artifacts' directory: `bin/artifacts`, `etc/manifest.toml`, `etc/nginx.conf` and `share/icon.svg`, the last Tabler's `archive` outline, human-authored. No test reads the checkout, so the release and the icon are outside what a requirement can state; a developer checks them with `tar`. The manifest's lines carry what a space asks of it (`D02-cli`): `guests = false`, so the host's nginx puts every request through auth's `/check` and admits only one with a credential, to the pages, to `/files/` and to `/mcp` alike; `mcp = true`, so the gateway offers its tools; `[env]` with `ARTIFACT_MAX_BYTES = "1073741824"`; `[database]` naming `state/artifacts.db`, which the host keeps and replicates; `[resources]` with `memory_max = "128M"`, enough because no body is ever held in memory; and `[home]`, which puts artifacts in the `core` group of home's landing page. The fragment is repos', verbatim: it lifts nginx's body limit and turns off request buffering, so a body streams through nginx uncapped and unbuffered, and artifacts' own cap answers 413; it gives a transfer an hour each way; and it answers `/events` and `/declarations` 404.

## On a space

artifacts on a space is the same binary serving on the same terms as under a test: a socket passed as descriptor 3 (`/run/ikigenba/artifacts.sock`), `/var/opt/ikigenba/artifacts` as its working directory, so its state is `/var/opt/ikigenba/artifacts/state/`, its settings from `/etc/opt/ikigenba/artifacts/env`, and `IKIGENBA_SERVICES` naming the host's services file.

| Outcome | Owner |
|---|---|
| serve on the inherited socket, `READY=1`, the database and `blobs/` under `state/` | `D01-layout-and-run-seam`, `D03-serve`; the unit and the directory: opsctl |
| a request with no credential never reaches artifacts; a page asked without one is sent to sign in, `/files/` and `/mcp` without one are refused | `guests = false` (`D02-cli`); nginx and auth's `/check`: opsctl and auth |
| `X-User-Id`, `X-User-Email` and `X-Request-Id` set by nginx on every proxied request; a bearer token from curl is a full identity | opsctl's nginx and auth; `identity.Require` (`D03-serve`) |
| a 1 GiB body streams through nginx, which neither buffers nor caps it; a body past the cap is answered 413 by artifacts | the fragment (`D02-cli`); the cap (`D04-store`, `D05-files`); the value from `[env]`: opsctl |
| the tools through the gateway, `url` from the services file's entry `artifacts` | `D08-tools`, `D05-files`; the file: opsctl |
| `import`'s plan names the `url` on the services file's entry `artifacts`, and the agent sends the `PUT` itself | `D09-import`, `D05-files`; the file: opsctl |
| `artifact.created`, `artifact.replaced` and `artifact.deleted` taken by the events app and delivered to subscribed scripts | `D07-trail`; the broker: events; the run: scripts |
| expired artifacts gone once before ready and then hourly | the sweeper (`D03-serve`, `D04-store`) |
| a backup holds the database and the blobs together | opsctl's service backup, which archives `state/` whole, so `artifacts.db` and `state/blobs/` are taken in one archive |
| the database is replicated | Litestream, under opsctl, replicates `state/artifacts.db` only; a database restored from the replica may name blobs the directory lacks or miss blobs it holds, which `Reconcile` removes at start, recording each removed artifact in the trail alone as `artifact.orphaned` (`D04-store`, `D07-trail`) |

## In a sandbox

The sandbox builds artifacts from the worktree, gives it `ARTIFACT_MAX_BYTES` from the manifest's `[env]`, puts every request through auth's `/check` because the manifest has `guests = false`, and includes `etc/nginx.conf` in artifacts' server. Its state is `<data>/apps/artifacts/state/`. An agent is curl on the developer's machine with the stored token, putting and getting at `http://artifacts.<sandbox>.localhost:<port>/files/<path>`, and the gateway for the tools, whose `import` plan names that same address.

## The guarantee

A **marked body** is one a test chooses of at least 16 ASCII letters and digits, every run of 8 consecutive characters holding a letter outside `a`–`f` and `A`–`F`, so that no hexadecimal id, digest or run of digits can contain 8 of its characters in any case, and that the test sends nowhere but as a body: not in a path, a header, a URL, a tool argument, a setting or the services file. Over a sequence that puts, replaces, gets, gets a range, asks `HEAD`, has puts refused for size, ownership and expiry, asks `import` for a plan, deletes by `DELETE` and by the tool, lists, shows and asks the pages, no 8 consecutive characters of any marked body appear, in any case, in any trail event, any bus event, or anything written to standard error; and none appear in any tool result or page body. A body's bytes reach only the blob directory and the answers to `GET` and range `GET` of its path.

## Recorded decisions

- The marked value is a body alone. artifacts mints no secret and keeps no sender header, so there is nothing else of webhooks' list to mark; the caller's credential never reaches artifacts at all, since nginx consumes it.
- Paths, content types and URLs are metadata and do travel on the trail and the bus (`D07-trail`, and the request middleware's path); they are not marked.

## Consumer tasks

A test builds a `cli.Process` with a temporary `Dir`, a `LookupEnv` over a map giving a small `ARTIFACT_MAX_BYTES`, capturing `Sink` and `EventSink`, a `Stderr` buffer, and its own `Now`, `After` and `Rand`; it drives `/files/<path>` with raw HTTP as two users, `/mcp` with `mcp.Client`, and the pages with raw `GET`s, then searches every captured event's JSON, every bus event's JSON, `Stderr`, every tool result's JSON and every page body for each 8-character run of each marked body, ignoring ASCII case.

## REQUIREMENTS

- R-OJS9-D6KZ: artifacts' design defines a **marked body** as a body a test sends of at least 16 ASCII letters and digits in which every run of 8 consecutive characters holds a letter outside `a`–`f` and `A`–`F`, and which occurs nowhere else in what the test hands artifacts — no path, header, URL, query, tool argument, value `LookupEnv` gives, or part of the services file; every requirement of artifacts' design that names a marked body MUST denote such a body.
- R-UYJE-SKZP: Over a run of `cli.Run` with capturing sinks and an `ARTIFACT_MAX_BYTES` the test chooses, that serves, with marked bodies (R-OJS9-D6KZ), a `PUT /files/<p>` creating an artifact, a `PUT` replacing it by its owner, a `GET`, a `GET` with a `Range` header, a `HEAD`, a `PUT` refused 413 for a body past the cap, a `PUT` refused 403 by another user, a `PUT` refused 400 for an `X-Artifact-Expires` in the past, an `import` call for that artifact's path, a `DELETE /files/<p>`, a `delete` call, `list` and `show` calls, and `GET`s of `/` and of an artifact's page, no event the `Sink` is handed, no bus event the `EventSink` is handed, and nothing written to `Stderr` MUST contain, ignoring ASCII case, any run of 8 or more consecutive characters of any marked body sent or answered in that run.
- R-UZRB-6CQE: In the run R-UYJE-SKZP states, no tool result `mcp.Client` returns, in any member of its JSON, and no body of an answer to a `GET` of `/`, of an artifact's page, of `/tools` or of `/about` MUST contain, ignoring ASCII case, any run of 8 or more consecutive characters of any marked body.
