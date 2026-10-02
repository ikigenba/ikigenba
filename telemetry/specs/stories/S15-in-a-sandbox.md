# Stories — in a sandbox

telemetry reached through a sandbox: the local runner a developer brings up from a worktree, which serves every app of the checkout in plain HTTP behind its own nginx on a loopback port, each at `http://<app>.<name>.localhost:<port>`. The stories share one sandbox: `wip`, on port `7400`, holding `auth`, `dummy`, `mcp`, and `telemetry`, all active, with telemetry at `http://telemetry.wip.localhost:7400`, the gateway at `http://mcp.wip.localhost:7400`, and dummy at `http://dummy.wip.localhost:7400`. Browsers and curl resolve every name under `localhost` to the loopback address. The sandbox gives telemetry the environment of a host, `DRAIN_SECONDS`, `IKIGENBA_SERVICES`, and `RETENTION_DAYS=15` from its manifest's `[env]` (`S07-retention.md`), and variables a host never sets, none of which telemetry reads. Its working directory is the sandbox's own for the app, so its database is `<sandbox data>/apps/telemetry/state/telemetry.db`, created on the first `up` and kept across `down` and `up` until `wipe`. The sandbox's services file lists telemetry with its socket under the developer's runtime directory, so every app of the sandbox delivers its events there and the trail in a sandbox is as complete as on a host: auth's check events, dummy's requests, the gateway's forwards, and telemetry's own records (`S12-own-events.md`). The sandbox's nginx includes telemetry's `etc/nginx.conf` from the checkout in telemetry's server, as a host's does, so `/ingest` at telemetry's sandbox name answers 404 while the socket takes every sibling's events. Every request to an app other than `auth` is first put to auth's `/check`, so an agent sends the sandbox's bearer token, the one `sandbox token` prints, as `Authorization: Bearer <token>`; the sandbox's nginx, its routing through `/check`, and the token are the sandbox's own (a separate sub-project), named here only by their observable effect, as `S14-on-a-space.md` names the space's nginx. Every request below is a `$ curl -si` line to the sandbox; responses pass through the sandbox's nginx over HTTP/1.1. The MCP requests are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05-mcp.md` fixes; the members every result carries on that revision are not repeated.

## An agent reaches telemetry in a sandbox

An agent working in the worktree opens telemetry's own name in the sandbox with the stored token and gets the landing page, as on a space.

Request:

```
$ curl -si -H "Authorization: Bearer $(sandbox token)" http://telemetry.wip.localhost:7400/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03-landing.md`): an HTML page whose title is `telemetry`, whose visible text carries the heading `telemetry`, the heading `MCP tools`, the four tool names `catalog`, `search`, `count`, and `trace`, and the link `About telemetry` to `/about`, whose banner's profile link is titled with the email of the token's owner and leads to `http://auth.wip.localhost:7400/`, and whose footer reads `telemetry v<semver>`. Its banner carries the launcher button `Services`, since the sandbox's services file lists telemetry with its icon.

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token a human created at `http://auth.wip.localhost:7400/` and stored with `sandbox token set`.
- auth's `/check` answers 200 for that token, naming its owner.

Postconditions:

- Nothing has changed but the trail: telemetry records `request.started` with `method=GET` and `path=/`, then `request.finished` with `status=200`, under the id the sandbox's nginx gave the request and the token's owner; auth records `check.allowed` for the same id with `host=telemetry.wip.localhost` and `path=/` (auth's `S4-check.md`).

## An agent asks a sandbox for the ingest path

As on a space, `/ingest` at telemetry's public name answers 404 for every method, because the sandbox includes telemetry's `etc/nginx.conf` in its server. Both forms below are answered the same way.

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -d '{"time":"2026-10-02T14:03:07.123456Z","service":"dummy","event":"service.started","request_id":"","user":"","attrs":{"version":"v0.3.0"}}' http://telemetry.wip.localhost:7400/ingest
```

```
$ curl -si -H "Authorization: Bearer $(sandbox token)" http://telemetry.wip.localhost:7400/ingest
```

Response:

```
HTTP/1.1 404 Not Found
```

Status 404. The body is not fixed. A request to the same path with no token is answered 404 as well; this story does not fix which of the sandbox's refusals comes first, only that neither reaches telemetry.

Preconditions:

- The sandbox above is up from an `up` run with `telemetry/etc/nginx.conf` in the checkout, and `sandbox token` prints a token auth accepts.

Postconditions:

- Nothing has changed. No record was stored, and telemetry records no event for either request: nothing reached its socket.

## An agent finds a sibling's events in the sandbox's trail

The point of running telemetry in a sandbox: what every app of the checkout does there is in one trail, read with the same tools through the sandbox's gateway, with the sandbox's token. The agent has just fetched dummy's panel with that token and now asks telemetry for dummy's newest records.

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"services":["dummy"],"events":["request.started","request.finished"],"limit":2}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is telemetry's answer to `search` (`S09-search.md`), relayed by the gateway: no `isError` member, a `structuredContent` of

```
{"records":[{"time":"<time>","service":"dummy","event":"request.finished","request_id":"<request-id>","user":"<user-id>","attrs":{"duration_us":<n>,"status":200}},{"time":"<time>","service":"dummy","event":"request.started","request_id":"<request-id>","user":"<user-id>","attrs":{"method":"GET","path":"/widgets"}}],"cursor":"<cursor>"}
```

and a `content` array of one text block holding that same object encoded compactly. `<request-id>` is the id the sandbox's nginx gave the panel request, the same in both records; `<user-id>` is the token's owner; `cursor` is present when dummy has recorded more than two such events in the sandbox, and absent otherwise. The result's `io.modelcontextprotocol/serverInfo` is the gateway's, `{"name":"mcp","version":"v<semver>"}`.

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token auth accepts, whose owner is the user `<user-id>`.
- The agent's most recent request to dummy was `curl -si -H "Authorization: Bearer $(sandbox token)" http://dummy.wip.localhost:7400/widgets`, answered 200, and dummy has made no request since; dummy posted that request's two events to telemetry's socket in the sandbox (dummy's `S2-serve.md`).

Postconditions:

- Nothing has changed in dummy's records.
- A `trace` of `<request-id>` through the same gateway answers five records, oldest first: auth's `request.started` with `method=GET` and `path=/check`, its `check.allowed` for the panel request with `host=dummy.wip.localhost` and `path=/widgets` (auth's `S4-check.md`), and its `request.finished` with `status=200`, the check event under user `<user-id>` and the two request events under no user, since the sandbox's nginx names none on its subrequest; then dummy's two records.
- The trail holds the gateway's events for this call and telemetry's own, under the id nginx gave it, as `S14-on-a-space.md` tells for a space.
- `sandbox logs telemetry` shows no line from telemetry: it wrote nothing to stderr.
