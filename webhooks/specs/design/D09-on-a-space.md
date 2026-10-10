# D09-on-a-space

What webhooks contributes when it runs on a space and in a sandbox, and the one guarantee no earlier document owns whole: no secret, no body and no sender header but the kept ones reaches anything webhooks records or writes beyond its database.

## What ships

The release file has four members: `bin/webhooks`, `etc/manifest.toml`, `etc/nginx.conf` and `share/icon.svg`, the last Tabler's `webhook` outline, human-authored. The manifest's lines carry what a space asks of it (`D02-cli`): `guests = true`, so the host's nginx passes `location /` without a credential through auth's `/check/open` while `/mcp`, `/api` and the git paths still require one; `mcp = true`; `[env]` with `WEBHOOKS_RETENTION_DAYS = "2"`; `[database]` naming `state/webhooks.db`, which the host keeps and replicates; `[resources]`; and `[home]`, which puts webhooks in the `core` group of home's landing page. The fragment raises nginx's body limit at webhooks' name above the ingress's cap and answers `/events` and `/declarations` 404.

## On a space

| Outcome | Owner |
|---|---|
| serve on the inherited socket, `READY=1`, the database under `/opt/webhooks/state/` | `D01-layout-and-run-seam`, `D03-serve`; the units and the directory: opsctl |
| a sender posts to `https://webhooks.<host>/in/<slug>` with no credential | `guests = true` (`D02-cli`); nginx's `/check/open` subrequest and auth's guest `check.allowed`: opsctl and auth |
| a sender's `Authorization` header is judged by auth, which refuses one it does not honor with 403 before webhooks sees it | auth; the reason the bearer scheme uses `X-Webhook-Secret` (`D05-ingress`) |
| an oversized body is answered 413 by webhooks, not by nginx | the fragment's `client_max_body_size` (`D02-cli`); the cap (`D05-ingress`) |
| `X-User-Id`, `X-User-Email` and `X-Request-Id` set by nginx, never by the sender | opsctl's nginx; the ingress ignores the first two (`D05-ingress`) |
| the tools through the gateway, `url` from the services file's entry | `D08-tools`; the file: opsctl |
| `webhook.<slug>.received` taken by the events app and delivered to scripts, a run per delivery | `D07-trail`; the broker: events; the run: scripts |
| deliveries gone after the window, the window from `[env]` | `D03-serve`, `D04-store`; writing `etc/env`: opsctl |

## In a sandbox

The sandbox builds webhooks from the worktree, gives it `WEBHOOKS_RETENTION_DAYS` from the manifest's `[env]`, routes `location /` through auth's `/check/open` because the manifest has `guests = true`, and includes `etc/nginx.conf` in webhooks' server. A sender is curl on the developer's machine posting to `http://webhooks.<sandbox>.localhost:<port>/in/<slug>`; the sandbox is not reachable from GitHub, so a real GitHub delivery waits for a deployed space.

## The guarantee

A **marked** value is one a test chooses long, of letters and digits that no hexadecimal id or run of digits can match, and sends nowhere else: a secret webhooks minted (the test controls `Rand`, so it knows the secret), a body, a signature, a sender header value, and a credential in an `Authorization` header. Over a sequence that creates webhooks of both schemes, accepts and refuses deliveries, rotates, fetches a delivery, deletes, and asks the pages, no marked value appears, in any case, in any trail event, any bus event, or anything written to standard error. The secret appears only in the `create` and `rotate` results, the body only in the `delivery` result.

## REQUIREMENTS

- R-0ZXT-P6KS: Over a run of `cli.Run` with capturing sinks and a `Rand` the test controls that serves `create` for a `bearer` and a `github-hmac` webhook, accepted and refused deliveries to each carrying a marked body, marked kept and unkept header values, a marked wrong secret, a marked `Authorization` credential and, for the `github-hmac` one, a signature, a `rotate`, a `delivery` and a `delete`, no event the `Sink` is handed, no bus event the `EventSink` is handed, and nothing written to `Stderr` MUST contain, ignoring ASCII case, any run of 8 or more characters of a minted secret, a body, a signature, the `Authorization` credential, or an unkept header value; and the only events that contain the value of a kept header MUST be `received` events, under `content_type` or `type`.
- R-12DM-GQ26: In that same run, the secret a `create` or `rotate` answered MUST appear in no result of a later `list`, `show` or `delivery` call and in no page body, and a delivery's body MUST appear in no tool result but `delivery`'s and in no page body.
