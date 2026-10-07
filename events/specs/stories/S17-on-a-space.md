# Stories — on a space

events reached through a space: the file `S16` describes, deployed with `devctl deploy`, installed by `opsctl`, and answered by nginx at `events.<space>` over TLS, so on the space `sbx.ikigenba.dev` events answers at `events.sbx.ikigenba.dev`. nginx on the space proxies to events' socket, `/run/ikigenba/events.sock` (`S02`), and includes events' own `etc/nginx.conf` in that server (`S16`), so `/emit` at the public name answers 404 while the same path on the socket takes every sibling's events (`S07`). events' manifest declares no `guests` (`S02`), so the space's nginx asks auth's `/check` for every path of events: a request with no credential never reaches events, and is sent to sign in at a page or challenged at `/mcp`, by nginx itself; one with a session or a token auth honors is passed with the caller's `X-User-Id` and `X-User-Email`. Every request nginx passes carries `X-Forwarded-Proto: https` and the `X-Request-Id` nginx gave it, the same id its `/check` subrequest carried. The host's services file is `/var/lib/ikigenba/services.json`, which opsctl writes and names in every app's environment as `IKIGENBA_SERVICES`; it lists events under the name `events`, with `url` `https://events.sbx.ikigenba.dev`, the socket `/run/ikigenba/events.sock`, and marked for MCP since its manifest has `mcp = true`. That entry is how every producer on the host finds the bus: a service that emits looks up the entry named `events` and posts to its socket. The same file is how events finds the services it asks for declarations and delivers to (`S06`, `S11`), each at its own socket, and the MCP gateway offers events' five tools through `https://mcp.sbx.ikigenba.dev/mcp`. events runs as `/opt/events/bin/events` with `/opt/events` as its working directory, so its log is `/opt/events/state/events.db`, the database the manifest's `[database]` table declares (`S16`); the host keeps it across releases and replicates it continuously, which is opsctl's doing and is named here only by its effect. `/opt/events/etc/env` carries the six settings of the manifest's `[env]` beside the space's `DRAIN_SECONDS` and `IKIGENBA_SERVICES`. The stories prove the whole path from checkout to browser, curl, and agent, and nothing about events that the earlier groups do not already say. devctl and opsctl are named only by the commands they offer. repos and scripts are deployed and active on the space; repos declares that it emits `repo.pushed` and scripts that it accepts every event (`S06`), so scripts is a subscriber (`S10`). A guest is curl with no cookie and no `Authorization` header; a signed-in caller sends the token `ikp_<token>`, whose id is `<token-id>`, owned by `u_7f3a9c21`, `mg@example.com`, as `Authorization: Bearer ikp_<token>`. The MCP requests are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05` fixes; the members every result carries on that revision are not repeated. Trail records are named by their attributes, as `S14` records them.

## A user on a space reaches events' landing page

A signed-in user asks for events' own name and sees where every subscriber is in the log, and the tools agents use on the bus.

Request:

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://events.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03`): an HTML page whose title is `events`, whose banner's profile link is titled `mg@example.com`, the email of the token's owner, and leads to `https://auth.sbx.ikigenba.dev/`, whose banner's `Sign out` button is in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout`, whose banner carries the launcher button `Services`, since the host's services file lists events with its icon, from `share/icon.svg` (`S16`), and whose visible text carries the heading `events`; the heading `Subscribers`; the heading `MCP tools` and the five tool names `catalog`, `search`, `subscribers`, `skip`, and `resume`; and a link `About events` to `/about`; and whose footer reads `events v<semver>`, the version the deployed binary's `events --version` prints (`S01`), the same one `space status` reports for events. Its stylesheet is `https://events.sbx.ikigenba.dev/_appkit/theme.css` (`S04`): a browser showing the page requests its style from events' own host and from no other origin. Its button feedback script is `https://events.sbx.ikigenba.dev/_appkit/feedback.js` and its icon `https://events.sbx.ikigenba.dev/_appkit/favicon.svg`, both from the same host.

Preconditions:

- The space `sbx.ikigenba.dev` exists in account `602773793009`, its instance is `running`, and `opsctl` is installed on it.
- A tag `events/v<semver>` points at the commit `devctl build events` was run at, and it wrote `events/dist/events-v<semver>.tar.xz`.
- `devctl --account 602773793009 deploy sbx.ikigenba.dev events/dist/events-v<semver>.tar.xz` exited 0.
- `devctl --account 602773793009 space status sbx.ikigenba.dev` shows `events v<semver> active active -`.
- auth, telemetry, repos, and scripts are deployed and active on the space through their own chains.
- `ikp_<token>` is a token auth honors, owned by `u_7f3a9c21`, `mg@example.com`.

Postconditions:

- Nothing has changed but the trail.
- The trail holds, under the id nginx gave the request and user `u_7f3a9c21`: auth's `check.allowed` with `outcome=allowed`, `credential=token`, `host=events.sbx.ikigenba.dev`, `path=/`, and `token=<token-id>`; then events' `request.started` with `method=GET` and `path=/`, and `request.finished` with `status=200`. The response set no cookie.

## A guest on a space asks for events' landing page

events serves nothing to guests, so the space's nginx sends a guest who opens events' own name to sign in, carrying the URL it asked for, and the request never reaches events.

Request:

```
$ curl -si https://events.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 302
location: https://auth.sbx.ikigenba.dev/?return=https://events.sbx.ikigenba.dev/
```

Status 302. The body is not fixed, and the response carries no `www-authenticate` header. `https://events.sbx.ikigenba.dev/about` answers the same, with the `return` ending `/about`.

Preconditions:

- events `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A user on a space reaches events' landing page`.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/events.sock`, and events recorded nothing; auth recorded `check.refused` with `outcome=unauthenticated` and `credential=none`.

## An MCP client on a space reaches events' /mcp without a credential

nginx answers an MCP client with no credential with the bearer challenge it gives at any app, rather than a redirect it could not follow; events never sees the request.

Request:

```
$ curl -si -X POST https://events.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 401
content-type: text/plain
www-authenticate: Bearer realm="ikigenba"
```

Status 401. The body is the host's nginx's one line `authentication required: send Authorization: Bearer <token>`.

Preconditions:

- events `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A user on a space reaches events' landing page`.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/events.sock`, and events recorded nothing; auth recorded `check.refused` with `outcome=unauthenticated` and `credential=none`.

## An agent asks a space for events' emit path

`/emit` is meant only for events' siblings on the socket: an event on the bus is something other services act on, so nothing outside the host may put one there. At events' public name the space's nginx answers it 404 for every method, because of the fragment events ships (`S16`); a credential makes no difference, since the answer is nginx's and the request never reaches events. Both forms below are answered the same way.

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -d '{"id":"evt_1d6f3a8c5e2b9047","time":"2026-10-06T09:14:02.123456Z","service":"repos","event":"repo.pushed","request_id":"","user":"","attrs":{"new":"b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37","old":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0}' https://events.sbx.ikigenba.dev/emit
```

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://events.sbx.ikigenba.dev/emit
```

Response:

```
HTTP/2 404
```

Status 404. The body is not fixed. A request to the same path with no credential is answered 404 as well; this story does not fix which of the space's refusals comes first, only that neither reaches events.

Preconditions:

- events `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A user on a space reaches events' landing page`, so the host's nginx includes `/opt/events/etc/nginx.conf` in events' server.
- The agent holds `ikp_<token>`, a token auth honors, owned by `u_7f3a9c21`.
- events holds no event `evt_1d6f3a8c5e2b9047`.

Postconditions:

- Nothing has changed. Nothing reached `/run/ikigenba/events.sock`: events stored no event, recorded nothing for either request, and delivered nothing.
- The same event posted on `/run/ikigenba/events.sock` by repos is stored, as `S07` tells: the public 404 closes the path to the outside only.

## An agent on a space finds a push in events' log through the gateway

A developer pushes to a repository in repos on the space. repos finds the bus in the services file, as the entry named `events`, and emits the push's `repo.pushed` to that entry's socket; events stores it. An agent connected to the space's one MCP endpoint finds it with events' `search`, a read tool, which it calls with the gateway's `call`; the gateway reaches events directly on `/run/ikigenba/events.sock` and relays its answer, forwarding the caller and the request id nginx gave the agent's request.

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"services":["repos"],"events":["repo.pushed"],"limit":1}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 200
content-type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is events' answer to `search` (`S09`), relayed: no `isError` member, and a `structuredContent` listing exactly one event, the newest `repo.pushed` from repos, the developer's push. Its `id` begins `evt_`; its `service` is `repos` and its `event` `repo.pushed`; its `attrs` are `repo` `<rep>`, `ref` `refs/heads/main`, `old` `<old>`, and `new` `<new>`; its `request_id` is `<push-request>` and its `user` `u_7f3a9c21`; its `cause` is empty and its `depth` 0; and it carries the `seq` and `received` events gave it. The result's `io.modelcontextprotocol/serverInfo` is the gateway's, not events'.

Preconditions:

- events `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A user on a space reaches events' landing page`, and the host's services file lists `events` enabled, with the socket `/run/ikigenba/events.sock`.
- mcp is deployed and active on the space, serving `https://mcp.sbx.ikigenba.dev/mcp`.
- `u_7f3a9c21` owns, in repos, the repository `notes`, `<rep>`, and pushed to it with git a commit `<new>` that moved `refs/heads/main` from `<old>`, the push's request carrying the id `<push-request>` nginx gave it; that is the newest push on the space.
- The agent holds `ikp_<token>`.

Postconditions:

- Nothing has changed in events' log but what repos' emit added before the call: the call itself changed nothing.
- The trail holds events' `event.accepted` with `event` the event's id and `cause` empty (`S14`), and, under the id nginx gave the agent's request and user `u_7f3a9c21`, events' `request.started` with `method=POST` and `path=/mcp`, `tool.called` with `tool=search`, `kind=read`, and `outcome=ok`, and `request.finished` with `status=200`.
- events wrote nothing to stderr.

## An agent on a space checks the subscribers through the gateway

An agent that wants to know whether the services that react to events are keeping up asks events' `subscribers`, a read tool, through the gateway's `call`.

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 200
content-type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` is events' answer to `subscribers` (`S10`), relayed: no `isError` member, and a `structuredContent` listing scripts with `service` `scripts`, `status` `ok`, `cursor` the `seq` of the developer's push from the story above, and `lag` 0.

Preconditions:

- Everything `An agent on a space finds a push in events' log through the gateway` requires holds, and nothing has been emitted since that push.
- scripts answered the push's delivery ok.

Postconditions:

- Nothing has changed but the trail, which holds, under the id nginx gave the agent's request and user `u_7f3a9c21`, events' `request.started`, `tool.called` with `tool=subscribers`, `kind=read`, and `outcome=ok`, and `request.finished` with `status=200`.

## An operator restores events on a space

A restore puts events back as its replica holds it, and the log, the cursors, and everything else events keeps in its database come back together, as of one moment: the newest point the replica holds. Two things follow, and both are accepted. Events accepted after that point are gone from the log: their producers were answered 204 and kept no copy, so no one emits them again and no subscriber is delivered them again. And every subscriber's cursor is back where it stood at that point, so events delivers again, in `seq` order, each event the subscriber had finished since: the same events, with the same `id`, which a subscriber recognizes by that `id` and need not act on twice. The restore stops events' socket for its length, so an emit made meanwhile finds no socket and is not taken by events; whether it reaches the log later is up to the producer that made it.

Command:

```
$ sudo opsctl restore events
```

Output:

```
source: ok (events/<tarball>, <size>)
stop: ok (ikigenba-events.socket, ikigenba-events.service, litestream.service)
files: ok (/opt/events/etc, /opt/events/state, <n> files)
db: ok (/opt/events/state/events.db, newest <time>)
litestream: ok (unchanged)
start: ok (litestream.service, ikigenba-events.socket, ikigenba-events.service)
```

`<tarball>` is the name of the newest files backup of events, `<size>` its size, `<n>` how many files it held, and `<time>` the newest point the replica of events' database holds, the point the log and the cursors are restored to.

Exits 0. The lines are on stdout; stderr is empty.

Preconditions:

- events `v<semver>` is deployed and active on `sbx.ikigenba.dev`, as in `A user on a space reaches events' landing page`, and the host has replicated `/opt/events/state/events.db`.
- `aws.region` and `backup.s3_uri` are set in opsctl's configuration, and the host's role can read under that prefix.
- `<backup.s3_uri>events/` holds at least one files backup of events, of `/opt/events/etc/` and `/opt/events/state/`.
- The newest point the replica holds has the log through `seq` 1040, and scripts `ok` with its cursor at 1032.
- Since that point, and before the restore, events accepted the events with `seq` 1041 to 1046, delivered every event through 1046 to scripts, and scripts answered each ok, so `subscribers` answered scripts' cursor 1046 and lag 0.
- scripts answers every delivery ok.

Postconditions:

- events is serving again on `/run/ikigenba/events.sock`, over the restored `/opt/events/state/events.db`.
- `search` (`S09`) finds no event whose `seq` is above 1040 from before the restore: the events once at 1041 to 1046 are gone and are never delivered again. The `seq` numbers they had may be given again, to the next events events accepts, so a `seq` names an event only within one history of the log; a consumer recognizes an event by its `id`.
- scripts has been delivered again, in `seq` order, the events at 1033 to 1040, each with the `id`, `seq`, and `received` it had before; once it has answered them, `subscribers` (`S10`) answers scripts `ok` with its cursor at 1040 and lag 0.
- The trail holds this events' `service.started` after the restore, and an `event.delivered` with `service=scripts` for each of the deliveries made again, each naming its event's id under `event` (`S14`).
