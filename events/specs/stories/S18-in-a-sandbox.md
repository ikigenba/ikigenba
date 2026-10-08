# Stories — in a sandbox

events reached through a sandbox: the local runner a developer brings up from a worktree, which serves every app of the checkout in plain HTTP behind its own nginx on a loopback port, each at `http://<app>.<name>.localhost:<port>`. The stories share one sandbox: `wip`, on port `7400`, holding `auth`, `dummy`, `events`, `mcp`, `repos`, `scripts`, `sites`, and `telemetry`, all active, with events at `http://events.wip.localhost:7400`, repos at `http://repos.wip.localhost:7400`, auth at `http://auth.wip.localhost:7400`, and the gateway at `http://mcp.wip.localhost:7400`. Browsers, curl, and git resolve every name under `localhost` to the loopback address. The sandbox gives events the environment of a host, `DRAIN_SECONDS`, `IKIGENBA_SERVICES`, and the six settings of its manifest's `[env]` at their manifest values, and variables a host never sets, none of which events reads. Its working directory is the sandbox's own for the app, so its log is `<data>/apps/events/state/events.db`, created on the first `up` and kept across `down` and `up` until `wipe`. The sandbox's services file lists events under the name `events` with its socket under the developer's runtime directory, so every app of the sandbox that emits finds the bus there, as on a host, and events finds there every service it asks for declarations and delivers to (`S06`, `S11`). The sandbox's nginx includes events' `etc/nginx.conf` from the checkout in events' server, as a host's does (`S16`), so `/emit` at events' sandbox name answers 404 while the same path on events' socket takes every sibling's events. events' manifest declares no `guests`, so the sandbox's nginx puts every request to events to auth's `/check`: a request with no credential never reaches events, and is sent to sign in at a page or challenged at `/mcp`. An agent sends the sandbox's token, the one `sandbox token` prints, as `Authorization: Bearer <token>`, and git sends it as the Basic password after the sandbox's nginx challenges it on a git path. The sandbox's nginx, its routing through `/check`, and the token are the sandbox's own (a separate sub-project), named here only by their observable effect. Every request below is made to the sandbox; responses pass through the sandbox's nginx over HTTP/1.1. The developer's shell has the token in `IKIGENBA_TOKEN` and a git credential helper installed once that answers with it:

```
$ export IKIGENBA_TOKEN="$(sandbox token)"
$ git config --global credential.http://*.wip.localhost:7400.helper '!f() { test "$1" = get && printf "username=token\npassword=%s\n" "$IKIGENBA_TOKEN"; }; f'
```

repos declares that it emits `repo.pushed` with the attributes `repo`, `ref`, `old`, and `new`, and scripts that it accepts every event (`S06`), so scripts is a subscriber (`S10`). The MCP requests are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05` fixes; the members every result carries on that revision are not repeated. Trail records are named by their attributes, as `S14` records them.

## An agent reaches events' landing page in a sandbox

An agent working in the worktree opens events' own name in the sandbox with the stored token and gets the landing page, as on a space.

Request:

```
$ curl -si -H "Authorization: Bearer $(sandbox token)" http://events.wip.localhost:7400/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03`): an HTML page whose title is `events`, whose visible text carries the heading `events`, the heading `Subscribers`, the heading `MCP tools` and the five tool names `catalog`, `search`, `subscribers`, `skip`, and `resume`, and the link `About events` to `/about`, whose banner's profile link is titled with the email of the token's owner and leads to `http://auth.wip.localhost:7400/`, and whose footer reads `events <display>`, where `<display>` is the first seven characters of the worktree's commit, followed by `-dirty` when the worktree has uncommitted changes, since the sandbox gives events that commit as `IKIGENBA_COMMIT` and sets no `IKIGENBA_RELEASE` (`S01`). Its banner carries the launcher button `Services`, since the sandbox's services file lists events with its icon.

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token a human created at `http://auth.wip.localhost:7400/` and stored with `sandbox token set`.
- auth's `/check` answers 200 for that token, naming its owner.

Postconditions:

- Nothing has changed but the trail: events records `request.started` with `method=GET` and `path=/`, then `request.finished` with `status=200`, under the id the sandbox's nginx gave the request and the token's owner; auth records `check.allowed` for the same id with `host=events.wip.localhost` and `path=/`.

## A developer in a sandbox pushes to a repository and an agent finds the push in events' log

The whole path of an event in a sandbox: the developer pushes a commit to a repository in repos with plain git, repos emits the push's `repo.pushed` to events, and an agent finds it with events' `search` through the gateway's `call`. Nothing but the push puts it there.

Command:

```
$ git clone -q http://repos.wip.localhost:7400/notes.git
$ git -C notes rev-parse HEAD
$ git -C notes commit -q --allow-empty -m 'Touch'
$ git -C notes push -q origin main
$ git -C notes rev-parse HEAD
```

Output:

```
<old>
<new>
```

Each command exits 0 and writes nothing to stderr. `<old>` is the 40 lowercase hexadecimal digits `notes`' `main` named before the push, and `<new>` those of the commit just pushed. Then the agent searches events' log:

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"services":["repos"],"events":["repo.pushed"],"limit":1}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is events' answer to `search` (`S09`), relayed by the gateway: no `isError` member, and a `structuredContent` listing exactly one event, the push's. Its `id` begins `evt_`; its `service` is `repos` and its `event` `repo.pushed`; its `attrs` are exactly `repo` `<rep>`, `ref` `refs/heads/main`, `old` `<old>`, and `new` `<new>`; its `request_id` is the id the sandbox's nginx gave the `POST /notes.git/git-receive-pack`; its `user` is `<user-id>`; its `cause` is empty and its `depth` 0; and it carries the `seq` and `received` events gave it.

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token auth accepts, owned by `<user-id>`.
- `<user-id>` owns, in repos, the repository `notes`, `<rep>`, whose `main` is at `<old>`.
- The developer's shell is as the opening paragraph says, and the current directory holds nothing named `notes`.

Postconditions:

- `notes`' `refs/heads/main` is `<new>`, and events' log holds the push's event as the answer shows it.
- The trail holds events' `event.accepted` with `event` the push's event id and `cause` empty (`S14`), and, under the id the sandbox's nginx gave the agent's request and user `<user-id>`, events' `request.started`, `tool.called` with `tool=search`, `kind=read`, and `outcome=ok`, and `request.finished` with `status=200`.
- `sandbox logs events` shows no line from events: it wrote nothing to stderr.

## An agent in a sandbox watches scripts take a push

With scripts holding a script subscribed to `repo.pushed`, a push is delivered to scripts, and the agent sees scripts' place in the log move past it with events' `subscribers`, through the gateway's `call`, once before the push and once after. What scripts does with the delivery is scripts' own.

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"subscribers","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is events' answer to `subscribers` (`S10`), relayed: no `isError` member, and a `structuredContent` listing scripts with `service` `scripts`, `status` `ok`, `cursor` `<before>`, and `lag` 0. Then the developer pushes a commit to `notes` with git, as the story above does, and once events has delivered the push's event, the agent makes the same call again, with `id` 2, and gets an answer that lists scripts with `status` `ok`, `cursor` now the `seq` of the push's event, a number above `<before>`, and `lag` 0.

Preconditions:

- The sandbox above is up, with events and scripts active, and `sandbox token` prints a token auth accepts, owned by `<user-id>`.
- `<user-id>` owns, in repos, the repository `notes`, and in scripts a script subscribed to `repo.pushed`.
- scripts is `ok` with lag 0, and nothing but the push below is emitted between the two calls.
- scripts answers the delivery of the push's event ok.
- The developer's shell is as the opening paragraph says, and the current directory holds nothing named `notes`.

Postconditions:

- events delivered the push's event to scripts; scripts' cursor is the push's event's `seq`.
- The trail holds events' `event.accepted` with `event` the push's event id, a `sibling.called` with `target=scripts`, `method=POST`, and `path=/events` for the delivery, and `event.delivered` with `event` the push's event id and `service=scripts` (`S14`), and, for each call, under its own id and user `<user-id>`, events' `request.started`, `tool.called` with `tool=subscribers`, `kind=read`, and `outcome=ok`, and `request.finished` with `status=200`.
- `sandbox logs events` shows no line from events.

## A guest in a sandbox asks for events' landing page

events serves nothing to guests, in a sandbox as on a space. The sandbox's nginx sends a guest who opens events' own name to sign in at the sandbox's auth, carrying the URL it asked for, and the request never reaches events.

Request:

```
$ curl -si http://events.wip.localhost:7400/
```

Response:

```
HTTP/1.1 302 Moved Temporarily
Location: http://auth.wip.localhost:7400/?return=http://events.wip.localhost:7400/
```

Status 302. The response carries no `WWW-Authenticate` header; the body is not fixed. `http://events.wip.localhost:7400/about` answers the same, with the `return` ending `/about`, and `curl -si -X POST http://events.wip.localhost:7400/mcp` is answered `401` with `WWW-Authenticate: Bearer realm="ikigenba"`.

Preconditions:

- The sandbox above is up.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached events' socket, and events recorded nothing.

## An agent asks a sandbox for events' emit path

As on a space (`S17`), `/emit` at events' public name answers 404 for every method, because the sandbox includes events' `etc/nginx.conf` in its server. Both forms below are answered the same way.

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -d '{"id":"evt_2e7a4c9f6b3d0158","time":"2026-10-06T09:14:02.123456Z","service":"repos","event":"repo.pushed","request_id":"","user":"","attrs":{"new":"b07d2e3f5a8c1964e0d7b3a2f9c6e5d48a1b0c37","old":"e4f1c9a7d2b85306f41c0e9a3d7b2c8e5f16a04d","ref":"refs/heads/main","repo":"rep_9c2e4b7a1d3f8e05"},"cause":"","depth":0}' http://events.wip.localhost:7400/emit
```

```
$ curl -si -H "Authorization: Bearer $(sandbox token)" http://events.wip.localhost:7400/emit
```

Response:

```
HTTP/1.1 404 Not Found
```

Status 404. The body is not fixed. A request to the same path with no token is answered 404 as well; this story does not fix which of the sandbox's refusals comes first, only that neither reaches events.

Preconditions:

- The sandbox above is up from an `up` run with `events/etc/nginx.conf` in the checkout, and `sandbox token` prints a token auth accepts.
- events holds no event `evt_2e7a4c9f6b3d0158`.

Postconditions:

- Nothing has changed. Nothing reached events' socket: events stored no event, recorded nothing for either request, and delivered nothing.
