# Stories — on a space

cron reached through a space: the file `S14` describes, deployed with `devctl deploy`, installed by `opsctl`, and answered by nginx at `cron.<space>` over TLS, so on the space `sbx.ikigenba.dev` cron answers at `cron.sbx.ikigenba.dev`. nginx on the space proxies to cron's socket, `/run/ikigenba/cron.sock` (`S02`), and includes cron's own `etc/nginx.conf` in that server (`S14`), so `/events` and `/declarations` at the public name answer 404 while the same paths on the socket stay open to the events app. cron's manifest declares `guests = false` (`S01`), so the space's nginx asks auth's `/check` for every path of cron: a request with no credential never reaches cron, and is sent to sign in at a page or challenged at `/mcp` and the paths under `/mcp/` by nginx itself (opsctl's `S5-nginx.md`, `A browser reaches a wired app outside /mcp without signing in` and `An MCP client reaches a wired app without a credential`), one whose credential auth refuses gets auth's 403 and never reaches cron, and one with a session or a token auth honors is passed with the caller's `X-User-Id` and `X-User-Email`. Every request nginx passes carries `X-Forwarded-Proto: https` and the `X-Request-Id` nginx gave it, the same id its `/check` subrequest carried, so auth's check event and cron's records of the request share one request id. The host's services file is `/var/lib/ikigenba/services.json`, which lists cron with `url` `https://cron.sbx.ikigenba.dev`, its socket, and marked for MCP since its manifest has `mcp = true`, so the MCP gateway offers cron's seven tools through `https://mcp.sbx.ikigenba.dev/mcp` (mcp's `S11-on-a-space.md`); it lists the events app too, to which cron emits its events (`S02`). cron runs as `/opt/cron/bin/cron` with `/opt/cron` as its working directory, so its database is `/opt/cron/state/cron.db`, which the host keeps and replicates as it does auth's; that is opsctl's doing and is named here only by its effect. `/opt/cron/etc/env` carries the space's `DRAIN_SECONDS` and `IKIGENBA_SERVICES` and nothing of cron's own, since the manifest has no `[env]`. cron runs as the service `ikigenba-cron.service`, with the 128 MiB ceiling its manifest's `[resources]` declares (`S01`; opsctl's `S7-apps.md`). A deploy restarts cron, and a slot that falls due between the old cron exiting and the new one starting is gone (`S11`, `S13`).

The stories prove the whole path from checkout to browser, curl, and agent, and nothing about cron that the earlier groups do not already say. devctl and opsctl are named only by their published commands. The database on the space holds the space's triggers (`S06`) — `hourly` and `weekly_digest`, owned by `u_7f3a9c21`, `mg@example.com`, and `month_end` and `nightly_backup`, owned by `u_2b8e1d04`, `ann@example.com` — and no trigger named `crm_sync`. A guest is curl with no cookie and no `Authorization` header; a signed-in caller sends the token `ikp_<token>` (auth's `S5-tokens.md`), owned by `u_7f3a9c21`, as `Authorization: Bearer ikp_<token>`. The MCP requests are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05` fixes; the members every result carries on that revision are not repeated. Trail events are named here by their attributes, as `S12` records them in full. The token's secret is in no record any story below leaves behind: not in the trail, not in any bus event, and not in nginx's logs on the host.

## A user on a space reaches cron's landing page

A signed-in user asks for cron's own name and gets the page of every trigger in the space and the tools that manage them.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://cron.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03`): an HTML page whose title is `cron`, whose banner's profile link is titled `mg@example.com`, the email of the token's owner, and leads to `https://auth.sbx.ikigenba.dev/`, whose banner's `Sign out` button is in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout`, whose banner carries the launcher button `Services`, since the host's services file lists cron with its icon, and whose visible text carries the heading `cron`; the summary `Triggers that emit events on the suite's event bus on a schedule.`; the table of every trigger in the space, `hourly`, `month_end`, `nightly_backup`, and `weekly_digest`, in that order, each with its id, schedule, owner's email, status, last fire, and next slot, `hourly` and `weekly_digest` marked `yours` and the other two not, `month_end` with no last fire and `weekly_digest`, which is paused, with no next slot; the heading `MCP tools` and the seven tool names `list`, `show`, `create`, `update`, `pause`, `resume`, and `delete`; and a link `About cron` to `/about`; and whose footer reads `cron <display>`, where `<display>` is whatever display string the host's environment gives cron: the string the deployed binary's `cron --version` prints under that same environment (`S01`), and empty when the host sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`. Its stylesheet is `https://cron.sbx.ikigenba.dev/_appkit/theme.css` (`S04`): a browser showing the page requests its style from cron's own host and from no other origin. Its button feedback script is `https://cron.sbx.ikigenba.dev/_appkit/feedback.js` and its icon `https://cron.sbx.ikigenba.dev/_appkit/favicon.svg`, both from the same host.

Preconditions:

- The space `sbx.ikigenba.dev` exists in account `602773793009`, its instance is `running`, and `opsctl` is installed on it.
- `devctl build cron`, run in a clean tree at the commit `<sha>`, wrote `cron/dist/cron-<sha>.tar.xz` (`S14`). No tag is needed.
- `devctl --account 602773793009 deploy sbx.ikigenba.dev cron/dist/cron-<sha>.tar.xz` exited 0.
- `devctl --account 602773793009 space status sbx.ikigenba.dev` shows cron's service and socket `active`, in the layout devctl's and opsctl's stories own.
- auth, events, and telemetry are deployed and active on the space through their own chains.
- `ikp_<token>`, whose id is `<token-id>`, is a token auth honors, owned by `u_7f3a9c21`, `mg@example.com`.
- The host sets `IKIGENBA_SERVICES` in cron's environment to the path of its services file, and that file lists cron with its icon, from `share/icon.svg` (`S14`).

Postconditions:

- Nothing has changed but the trail. No trigger fired for the request, and nothing was emitted to the bus.
- The trail holds, under the id nginx gave the request and user `u_7f3a9c21`: auth's `check.allowed` with `outcome=allowed`, `credential=token`, `host=cron.sbx.ikigenba.dev`, `path=/`, and `token=<token-id>`; then cron's `request.started` with `method=GET` and `path=/`, and `request.finished` with `status=200`. The response set no cookie.

## A guest on a space asks for cron's landing page

cron serves nothing to guests, so the space's nginx sends a guest who opens cron's own name to sign in, carrying the URL it asked for, and the request never reaches cron. Once signed in, auth brings the browser back to the landing page.

Request:

```
$ curl -si https://cron.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 302
location: https://auth.sbx.ikigenba.dev/?return=https://cron.sbx.ikigenba.dev/
```

Status 302. The body is not fixed, and the response carries no `www-authenticate` header. `https://cron.sbx.ikigenba.dev/about` answers the same, with the `return` ending `/about`.

Preconditions:

- cron is deployed and active on `sbx.ikigenba.dev`, as in `A user on a space reaches cron's landing page`.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/cron.sock`, and cron recorded no event; auth recorded `check.refused` with `outcome=unauthenticated` and `credential=none` (auth's `S4-check.md`).

## An MCP client on a space reaches cron's /mcp without a credential

nginx keeps the strict `/check` on `/mcp` and everything under it, and answers an MCP client with no credential with the bearer challenge it gives at any app (opsctl's `S5-nginx.md`, `An MCP client reaches a wired app without a credential`), rather than a redirect it could not follow; cron never sees the request.

Request:

```
$ curl -si -X POST https://cron.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 401
content-type: text/plain
www-authenticate: Bearer realm="ikigenba", resource_metadata="https://mcp.sbx.ikigenba.dev/.well-known/oauth-protected-resource"
```

Status 401. The body is the host's nginx's one line `authentication required: send Authorization: Bearer <token>`, ending in a newline, where `<token>` is those seven characters as written, not a value filled in.

Preconditions:

- cron is deployed and active on `sbx.ikigenba.dev`, as in `A user on a space reaches cron's landing page`.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/cron.sock`, and cron recorded no event; auth recorded `check.refused` with `outcome=unauthenticated` and `credential=none`.

## An agent asks a space for cron's events and declarations paths

`/events` and `/declarations` are meant only for the events app on the socket (`S02`). At cron's public name the space's nginx answers each 404 for every method, because of the fragment cron ships (`S14`); a credential makes no difference, since the answer is nginx's and the request never reaches cron. Every form below is answered the same way.

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' https://cron.sbx.ikigenba.dev/events
```

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://cron.sbx.ikigenba.dev/events
```

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' https://cron.sbx.ikigenba.dev/declarations
```

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://cron.sbx.ikigenba.dev/declarations
```

Response:

```
HTTP/2 404
```

Status 404. The body is not fixed. A request to either path with no credential is answered 404 as well; this story does not fix which of the space's refusals comes first, only that neither reaches cron.

Preconditions:

- cron is deployed and active on `sbx.ikigenba.dev`, as in `A user on a space reaches cron's landing page`, so the host's nginx includes `/opt/cron/etc/nginx.conf` in cron's server.
- The agent holds `ikp_<token>`, a token auth honors, owned by `u_7f3a9c21`.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/cron.sock`: cron recorded no event for any of the requests. The public 404 closes the paths to the outside only; what cron answers on them on its socket is `S02`'s.

## An agent on a space creates a trigger through the gateway and shows it

The expected path for an agent: it asks the gateway to run cron's `create` with a slug and a schedule, then `show` to see the trigger as cron keeps it. `create` is an additive tool (`S05`), so the agent calls it with the gateway's `mutate` tool (mcp's `S09-mutate.md`); `show` is a read tool, called with the gateway's `call` (mcp's `S08-call.md`). The gateway reaches cron directly on `/run/ikigenba/cron.sock` and relays its answer, forwarding the caller and the request id nginx gave the agent's request, so the trigger is `u_7f3a9c21`'s and its `cron.crm_sync.created` carries that request's id (`S06`).

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"cron","tool":"create","args":{"slug":"crm_sync","when":"*/15 * * * *"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call","arguments":{"service":"cron","tool":"show","args":{"slug":"crm_sync"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 200
content-type: application/json
```

Status 200, for each. The first body is a JSON-RPC response with `id` 1 whose `result` is cron's answer to `create` (`S06`), relayed: no `isError` member, a `structuredContent` of

```
{"id":"<id>","slug":"crm_sync","when":"*/15 * * * *","owner":"mg@example.com","status":"active","created":"<created>","next":"<next>"}
```

and a `content` array of one text block holding that same object encoded compactly; it has no `last_fired`, since the trigger has never fired. The second, sent before `<next>`, is a JSON-RPC response with `id` 2 whose `result` is cron's answer to `show` (`S07`), relayed, with the same `structuredContent`. `<id>` is `crn_` followed by 16 lowercase hexadecimal digits, the same in both; `<created>` is the time of the create, RFC 3339 UTC to the second; and `<next>` is the first quarter hour after it, RFC 3339 UTC to the second. Sent after `<next>`, `show` answers the trigger with `last_fired` `<next>` and `next` the quarter hour after it (`S11`). Each result's `io.modelcontextprotocol/serverInfo` is the gateway's, `{"name":"mcp","version":"<mcp display>"}`, where `<mcp display>` is the gateway's own display string, not cron's.

Preconditions:

- cron is deployed and active on `sbx.ikigenba.dev`, as in `A user on a space reaches cron's landing page`, and the host's services file lists `cron` enabled, marked for MCP, with the socket `/run/ikigenba/cron.sock`.
- mcp is deployed and active on the space through its own `S11-on-a-space.md` chain, serving `https://mcp.sbx.ikigenba.dev/mcp`.
- The agent holds `ikp_<token>`, owned by `u_7f3a9c21`.
- No trigger in the space has the slug `crm_sync`.

Postconditions:

- `u_7f3a9c21` owns a new trigger `crm_sync` with id `<id>`, `when` `*/15 * * * *`, active, in `/opt/cron/state/cron.db`.
- The landing page lists `crm_sync` among the triggers, marked `yours` for `u_7f3a9c21` (`S03`).
- The events app has taken the create's `cron.crm_sync.created`, with `service` `cron`, `attrs` exactly `trigger` `<id>` and `when` `*/15 * * * *`, `request_id` the id nginx gave the first request, `user` `u_7f3a9c21`, `cause` empty, and `depth` 0 (`S02`, `S06`).
- mcp wrote nothing to stderr, and neither did cron.
- The trail holds, under the id nginx gave each request and user `u_7f3a9c21`, auth's `check.allowed` with `credential=token`, the gateway's events for the call, and cron's own: for the first, `cron.crm_sync.created` with `trigger=<id>` and `when=*/15 * * * *`, then `tool.called` with `tool=create`, `kind=additive`, and `outcome=ok`; for the second, `tool.called` with `tool=show`, `kind=read`, and `outcome=ok`. No event holds the owner's email or the token.
