# Stories — on a space

telemetry reached through a space: the file `S13-package.md` describes, deployed with `devctl deploy`, installed by `opsctl`, and answered by nginx at `telemetry.<space>` over TLS. A space is one label under the root domain and an app is `<app>.<space>`, so telemetry on the space `sbx.ikigenba.dev` answers at `telemetry.sbx.ikigenba.dev`. nginx on the space proxies to telemetry's socket, `/run/ikigenba/telemetry.sock` (`S02-serve.md`), and includes telemetry's own `etc/nginx.conf` in that server, so `/ingest` at the public name answers 404 while the same path on the socket takes every sibling's events (`S06-ingest.md`). The space authenticates every request before it reaches telemetry and passes the caller on in `X-User-Id` and `X-User-Email`, with the request's id in `X-Request-Id`; telemetry's pages and `/mcp` have no unauthenticated case, so a request that arrives at all is one of a known caller. The host's services file is `/var/lib/ikigenba/services.json`, which opsctl writes and names in every app's environment; it lists telemetry, marked for MCP since its manifest has `mcp = true`, with that socket, so every sibling finds it and posts its events there, and the MCP gateway offers telemetry's four tools through `https://mcp.<space>/mcp` (mcp's `S11`). telemetry runs as `/opt/telemetry/bin/telemetry` with `/opt/telemetry` as its working directory, so its database is `/opt/telemetry/state/telemetry.db`, the one the manifest's `[database]` table declares (`S01-bootstrap.md`); the host keeps and replicates a declared database as it does auth's, which is opsctl's doing and is named here only by its effect. `/opt/telemetry/etc/env` carries `RETENTION_DAYS` from the manifest's `[env]` (`S07-retention.md`) beside the space's `DRAIN_SECONDS` and `IKIGENBA_SERVICES`. The stories prove the whole path from checkout to browser and agent and nothing about telemetry that the earlier groups do not already say. devctl and opsctl are named only by their published commands. The MCP requests below are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05-mcp.md` fixes; the members every result carries on that revision are not repeated.

## A visitor reaches telemetry's landing page on a space

The visitor asks for the landing page over TLS at telemetry's hostname on the space. The gate in front of telemetry authenticates the request and hands telemetry the caller's identity, and telemetry renders the page for that caller.

Request:

```
$ curl -si https://telemetry.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03-landing.md`): an HTML page whose title is `telemetry`, whose banner's profile link is titled with the email address of the caller the gate authenticated, whose visible text carries the heading `telemetry`, the heading `MCP tools`, and the four tool names `catalog`, `search`, `count`, and `trace`, and a link `About telemetry` to `/about`, and whose footer reads `telemetry <display>`, where `<display>` is whatever display string the host's environment gives telemetry: the string the deployed binary's `telemetry --version` prints under that same environment (`S01-bootstrap.md`), and empty when the host sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`. Its stylesheet is `https://telemetry.sbx.ikigenba.dev/_appkit/theme.css`, and the fonts that stylesheet loads are under the same `https://telemetry.sbx.ikigenba.dev/_appkit/` (`S04-assets.md`): a browser showing the page requests its style from telemetry's own host and from no other origin. Its button feedback script is `https://telemetry.sbx.ikigenba.dev/_appkit/feedback.js`, and its icon, the suite's favicon a browser shows on the page's tab, is `https://telemetry.sbx.ikigenba.dev/_appkit/favicon.svg`, both from the same host. In the banner, the profile link leads to `https://auth.sbx.ikigenba.dev/`, and the `Sign out` button is in a form that POSTs to `https://auth.sbx.ikigenba.dev/logout` (`S03-landing.md`); submitting it signs the visitor out of the space, as auth's stories tell.

Preconditions:

- The space `sbx.ikigenba.dev` exists in account `602773793009`, its instance is `running`, and `opsctl` is installed on it.
- `devctl build telemetry`, run in a clean tree at the commit `<sha>`, wrote `telemetry/dist/telemetry-<sha>.tar.xz` (`S13-package.md`). No tag is needed.
- `devctl --account 602773793009 deploy sbx.ikigenba.dev telemetry/dist/telemetry-<sha>.tar.xz` exited 0.
- `devctl --account 602773793009 space status sbx.ikigenba.dev` shows telemetry's service and socket `active`, in the layout devctl's and opsctl's stories own.
- `/opt/telemetry/state/telemetry.db` is the database telemetry opened, created on its first start on this space or kept from an earlier deploy.
- The space routes `telemetry.sbx.ikigenba.dev` through its authenticating gate: the gate admits the request and sets `X-User-Id` and `X-User-Email` on what it passes to telemetry, and refuses a request it cannot authenticate before telemetry sees it.
- The caller holds a credential the gate accepts, and the email that credential names is the one the page's profile link is titled with.

Postconditions:

- Nothing has changed but the trail: telemetry records `request.started` with `method=GET` and `path=/`, then `request.finished` with `status=200`, under the id nginx gave the request and the user the gate named (`S12-own-events.md`).

## A visitor on a space opens the service launcher

On a space the host sets `IKIGENBA_SERVICES` in telemetry's environment to the path of its services file (`S02-serve.md`). That file lists every service installed on the host, and telemetry's entry carries an icon because telemetry's package ships `share/icon.svg` (`S13-package.md`), which is what puts telemetry in the launcher (`S03-landing.md`). So the page a visitor reaches on a space carries the launcher in its banner, and telemetry is one of the services it offers. The launcher's text and behavior are `S03-landing.md`'s; this story fixes only what the visitor sees on a space.

Request:

```
$ curl -si https://telemetry.sbx.ikigenba.dev/
```

Response:

```
HTTP/2 200
content-type: text/html; charset=utf-8
```

Status 200. The body is the landing page of the story above, and its banner carries the launcher button labelled `Services` (`S03-landing.md`). In a browser, pressing the button opens a list of the space's services with a search box labelled `Find a service`; each entry shows a service's icon and name, as `S03-landing.md` tells. telemetry's own entry is in the list and is marked as the current page. The launcher's script is `https://telemetry.sbx.ikigenba.dev/_appkit/launcher.js` (`S04-assets.md`), so the launcher, like the style, needs nothing from any other origin.

Preconditions:

- Everything the story above requires holds: telemetry is deployed and active on `sbx.ikigenba.dev`, and the caller holds a credential the gate accepts.
- `telemetry/dist/telemetry-<sha>.tar.xz` holds `share/icon.svg` (`S13-package.md`).
- The host sets `IKIGENBA_SERVICES` in telemetry's environment to the path of its services file, and that file lists telemetry with its icon.

Postconditions:

- Nothing has changed but telemetry's own two records of the request, as in the story above.

## An agent asks a space for the ingest path

`/ingest` is meant only for siblings on the socket. At telemetry's public name the space's nginx answers it 404 for every method, because of the fragment telemetry ships (`S13-package.md`); a credential makes no difference, since the answer is nginx's and the request never reaches telemetry. Both forms below are answered the same way.

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -d '{"time":"2026-10-02T14:03:07.123456Z","service":"dummy","event":"service.started","request_id":"","user":"","attrs":{"version":"c604e32"}}' https://telemetry.sbx.ikigenba.dev/ingest
```

```
$ curl -si -H 'Authorization: Bearer ikp_<token>' https://telemetry.sbx.ikigenba.dev/ingest
```

Response:

```
HTTP/2 404
```

Status 404. The body is not fixed. A request to the same path with no credential is answered 404 as well; this story does not fix which of the space's refusals comes first, only that neither reaches telemetry.

Preconditions:

- telemetry is deployed and active on `sbx.ikigenba.dev`, as in `A visitor reaches telemetry's landing page on a space`, so the host's nginx includes `/opt/telemetry/etc/nginx.conf` in telemetry's server.
- The agent holds a valid token `ikp_<token>` (auth's `S5-tokens.md`) the gate accepts.

Postconditions:

- Nothing has changed. No record was stored, and telemetry records no event for either request: the request never reached its socket.
- The same event posted on `/run/ikigenba/telemetry.sock` by a process on the host is stored, as `S06-ingest.md` tells: the public 404 closes the path to the outside only.

## An agent on a space searches the trail through the gateway

This is what the trail is for: an agent connected to the space's one MCP endpoint reads the trail with telemetry's tools. The agent names the service and the tool, and the gateway reaches telemetry directly on `/run/ikigenba/telemetry.sock`, lists its tools to learn that `search` is a read tool, calls it on the caller's behalf, and relays telemetry's answer (mcp's `S08`). telemetry sees the call as one from the caller, with the same `X-Request-Id` nginx gave the request to the gateway, so the call is itself in the trail (`S12-own-events.md`).

Request:

```
$ curl -si -X POST -H 'Authorization: Bearer ikp_<token>' -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"services":["dummy"],"events":["request.finished"],"limit":2}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' https://mcp.sbx.ikigenba.dev/mcp
```

Response:

```
HTTP/2 200
content-type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is telemetry's answer to `search` (`S09-search.md`), relayed: it has no `isError` member, its `structuredContent` is an object whose members are `records`, an array of the two newest `request.finished` records from `dummy`, newest first, each the event as dummy posted it (`S06-ingest.md`), and, when more than two match, `cursor`, an opaque string; and its `content` is one text block holding that same object encoded compactly. The result's `io.modelcontextprotocol/serverInfo` is the gateway's, `{"name":"mcp","version":"<mcp-display>"}`, where `<mcp-display>` is the gateway's own display string, as mcp's stories tell, not telemetry's.

Preconditions:

- telemetry is deployed and active on `sbx.ikigenba.dev`, as in `A visitor reaches telemetry's landing page on a space`, and the host's services file lists `telemetry` enabled, marked for MCP, with the socket `/run/ikigenba/telemetry.sock`.
- mcp is deployed and active on the space through its own `S11` chain, serving `https://mcp.sbx.ikigenba.dev/mcp`.
- dummy is deployed and active on the space, and has answered at least two requests since the start of the retention window, each of which it recorded as a `request.finished` posted to telemetry's socket.
- The agent holds a valid token `ikp_<token>` (auth's `S5-tokens.md`) the gate accepts, whose owner is the user `<user-id>`.

Postconditions:

- Nothing has changed in dummy's records.
- mcp wrote nothing to stderr, and neither did telemetry.
- The trail holds, under `<id>`, the `X-Request-Id` nginx set on the request: auth's three records for the gate's check of the token, `request.started` with `method=GET` and `path=/check`, `check.allowed` with `credential=token`, `host=mcp.sbx.ikigenba.dev`, `method=POST`, and `path=/mcp`, and `request.finished` with `status=200` (auth's `S4-check.md`), the check event under user `<user-id>` and the two request events under no user, since nginx's subrequest names none; the gateway's five events for the request, posted to `/ingest`; and telemetry's own five for the gateway's two hops, stored directly, all ten under user `<user-id>`. A `trace` of `<id>` shows them together, oldest first: auth's three, then the gateway's and telemetry's as `S12-own-events.md` fixes them, each service's records in its own order, mcp's `request.started` first and its `request.finished` last among them, and the relative order of a hop's telemetry `request.finished` and mcp's `sibling.called` not fixed.
