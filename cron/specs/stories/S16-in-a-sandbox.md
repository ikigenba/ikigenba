# Stories — in a sandbox

cron reached through a sandbox: the local runner a developer brings up from a worktree, which serves every app of the checkout in plain HTTP behind its own nginx on a loopback port, each at `http://<app>.<name>.localhost:<port>`. The stories share one sandbox: `wip`, on port `7400`, holding `auth`, `cron`, `dummy`, `events`, `mcp`, `repos`, `scripts`, `sites`, and `telemetry`, all active, with cron at `http://cron.wip.localhost:7400`, auth at `http://auth.wip.localhost:7400`, and the gateway at `http://mcp.wip.localhost:7400`. Browsers and curl resolve every name under `localhost` to the loopback address. The sandbox gives cron the environment of a host, `DRAIN_SECONDS` and `IKIGENBA_SERVICES`, `IKIGENBA_COMMIT`, the worktree's commit, from which cron builds the display string it shows as its version (`S01`, `S02`), and other variables a host never sets, none of which cron reads. cron's working directory is the sandbox's own for it, `<data>/apps/cron/`, where `<data>` is the sandbox's data directory (sandbox's `S3-apps.md`), so its database is `<data>/apps/cron/state/cron.db`, created on the first `up` and kept across `down` and `up` until `wipe`. The sandbox's services file lists the events app, so cron emits its events there as on a host (`S02`), and the events app delivers them to the sandbox's scripts, which accepts every event.

cron's manifest declares `guests = false`, so the sandbox's nginx puts every request to cron to auth's `/check`: a request with no credential never reaches cron, and is sent to sign in at a page or challenged at `/mcp` (sandbox's `S4-routing.md`). The sandbox's nginx includes cron's `etc/nginx.conf` from the checkout in cron's server, as a host's does (`S14`), so `/events` and `/declarations` at cron's sandbox name answer 404 while the same paths on cron's socket stay open to the events app. Every request the sandbox's nginx passes carries `X-Forwarded-Proto: http` and an `X-Request-Id` nginx made. An agent sends the sandbox's token, the one `sandbox token` prints, as `Authorization: Bearer <token>`. The sandbox's nginx, its routing through `/check`, and the token are the sandbox's own (a separate sub-project), named here only by their observable effect. Every request below is made to the sandbox; responses pass through the sandbox's nginx over HTTP/1.1. The MCP requests are made with the protocol revision `2026-07-28` and carry the headers and `_meta` `S05` fixes; the members every result carries on that revision are not repeated. As on a space (`S15`), the token is in no record any story below leaves behind.

## An agent reaches cron's landing page in a sandbox

An agent working in the worktree opens cron's own name in the sandbox with the stored token and gets the landing page, as on a space.

Request:

```
$ curl -si -H "Authorization: Bearer $(sandbox token)" http://cron.wip.localhost:7400/
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03`): an HTML page whose title is `cron`, whose visible text carries the heading `cron`, the summary `Triggers that emit events on the suite's event bus on a schedule.`, the heading `MCP tools` and the seven tool names `list`, `show`, `create`, `update`, `pause`, `resume`, and `delete`, and the link `About cron` to `/about`, whose banner's profile link is titled with the email of the token's owner and leads to `http://auth.wip.localhost:7400/`, and whose footer reads `cron <display>`, where `<display>` is the worktree's short commit, followed by `-dirty` when the worktree is modified: the sandbox tells cron the worktree's commit in `IKIGENBA_COMMIT` and sets no `IKIGENBA_RELEASE` (sandbox's `S3-apps.md`; `S01`). Its banner carries the launcher button `Services`, since the sandbox's services file lists cron with its icon. With no trigger in the sandbox yet, the triggers section reads `No triggers yet` and `A trigger an agent creates with the create tool shows up here.`

Preconditions:

- The sandbox above is up, and `sandbox token` prints a token a human created at `http://auth.wip.localhost:7400/` and stored with `sandbox token set`.
- auth's `/check` answers 200 for that token, naming its owner.
- cron's database holds no trigger.

Postconditions:

- Nothing has changed but the trail: cron records `request.started` with `method=GET` and `path=/`, then `request.finished` with `status=200`, under the id the sandbox's nginx gave the request and the token's owner; auth records `check.allowed` for the same id with `host=cron.wip.localhost` and `path=/` (auth's `S4-check.md`).

## An agent in a sandbox makes a trigger run a script every minute

The whole loop cron is for, in a sandbox: an agent creates a trigger that fires every minute, and a script it has subscribed to every trigger's fire runs each time. cron never knows about the script: cron emits `cron.tick.fired` to the events app (`S11`), the events app delivers it to scripts, and scripts starts a run of every script subscribed to a pattern it matches, with the whole event as the run's input (scripts' `S27-event-runs.md`). Then the agent pauses the trigger and the runs stop, resumes it and they start again, and deletes it. cron's write tools are tools the gateway runs with `mutate` (mcp's `S09-mutate.md`), and its read tools, scripts' `runs` and `result`, and events' `search` are tools it runs with `call` (mcp's `S08-call.md`). The script `on-tick` runs the repository `tick-watch`, whose `main` holds one file, `main.py`, which prints what it was given:

```
import json
import os

with open(os.environ["IKIGENBA_INPUT"]) as f:
    event = json.load(f)
a = event["attrs"]
print(event["event"], event["service"], a["trigger"], a["when"], a["scheduled"])
```

First the agent creates the trigger and lists the triggers:

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"cron","tool":"create","args":{"slug":"tick","when":"* * * * *"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call","arguments":{"service":"cron","tool":"list","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, for each. The first body is a JSON-RPC response with `id` 1 whose `result` is cron's answer to `create` (`S06`), relayed by the gateway: no `isError` member, a `structuredContent` of

```
{"id":"<id>","slug":"tick","when":"* * * * *","owner":"<email>","status":"active","created":"<created>","next":"<next>"}
```

and a `content` array of one text block holding that same object encoded compactly. The second, sent before `<next>`, is a JSON-RPC response with `id` 2 whose `result` is cron's answer to `list` (`S07`), relayed, with a `structuredContent` of

```
{"triggers":[{"id":"<id>","slug":"tick","when":"* * * * *","owner":"<email>","status":"active","next":"<next>"}]}
```

`<id>` is `crn_` followed by 16 lowercase hexadecimal digits, the same throughout; `<email>` is the email of the token's owner; `<created>` is the time of the create; and `<next>` is the first whole minute after it, each RFC 3339 UTC to the second. At `<next>` the trigger fires, and once the events app has delivered its event and the run has ended, the agent lists `on-tick`'s runs and reads the newest:

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"call","arguments":{"service":"scripts","tool":"runs","args":{"name":"on-tick"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"call","arguments":{"service":"scripts","tool":"result","args":{"run":"<run>"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, for each. The first body's `result` is scripts' answer to `runs`, relayed, listing every run `on-tick` has had, newest first; the newest, `<run>`, has `trigger` `event` and `event` `<event-id>`, the id of the fire's event, beginning `evt_`, and is `exited` with exit code 0. A run begins within a minute of `<created>`. The second body's `result` is scripts' answer to `result`, relayed, for `<run>`: `user` `<user-id>`, the script's owner; `trigger` `event`; `event` `<event-id>`; `status` `exited`; `exit_code` 0; and `stdout` the one line `cron.tick.fired cron <id> * * * * * <scheduled>` and a newline, `<scheduled>` being the slot the fire was for, RFC 3339 UTC to the second: `<next>` for the first fire, and a whole minute for every fire. Each further minute the trigger fires again, and `on-tick` gets one more run for each fire. Then the agent pauses the trigger:

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"cron","tool":"pause","args":{"slug":"tick"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body's `result` is cron's answer to `pause` (`S09`), relayed, with a `structuredContent` of

```
{"id":"<id>","slug":"tick","when":"* * * * *","owner":"<email>","status":"paused","created":"<created>","last_fired":"<last>"}
```

`<last>` being the `scheduled` of the trigger's latest fire; a paused trigger has no `next`. While the trigger is paused it fires no more: `runs` for `on-tick`, asked two minutes later, lists exactly the runs it listed just after the pause, and no more. Then the agent resumes it:

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"cron","tool":"resume","args":{"slug":"tick"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body's `result` is cron's answer to `resume` (`S09`), relayed, with a `structuredContent` of

```
{"id":"<id>","slug":"tick","when":"* * * * *","owner":"<email>","status":"active","created":"<created>","last_fired":"<last>","next":"<resumed-next>"}
```

`<resumed-next>` being the first whole minute after the resume. The slots that passed while the trigger was paused are not fired; at `<resumed-next>` it fires again, and `on-tick` gets a new run whose `stdout` names `<resumed-next>` as the slot. Then the agent deletes the trigger, finds its deletion in events' log, and lists the triggers:

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: mutate' -d '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"cron","tool":"delete","args":{"slug":"tick"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"call","arguments":{"service":"events","tool":"search","args":{"services":["cron"],"events":["cron.tick.deleted"],"limit":1}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" -H 'Content-Type: application/json' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: call' -d '{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"call","arguments":{"service":"cron","tool":"list","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' http://mcp.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, for each. The first body's `result` is cron's answer to `delete` (`S10`), relayed, with a `structuredContent` of `{"deleted":true,"id":"<id>"}`. The second body's `result` is events' answer to `search` (events' `S09-search.md`), relayed, listing exactly one event: its `id` begins `evt_`; its `service` is `cron` and its `event` `cron.tick.deleted`; its `attrs` are exactly `trigger` `<id>` and `when` `* * * * *`; its `request_id` is the id the sandbox's nginx gave the `delete` request; its `user` is `<user-id>`; its `cause` is empty and its `depth` 0. The third body's `result` is cron's answer to `list`, relayed, with a `structuredContent` of `{"triggers":[]}`.

Preconditions:

- The sandbox above is up, with events and scripts active, and `sandbox token` prints a token auth accepts, whose owner is the user `<user-id>`, `<email>`.
- `<user-id>` owns, in repos, the repository `tick-watch`, whose `main` holds `main.py` above and nothing else, and, in scripts, the script `on-tick`, running `tick-watch` at `main`, subscribed to `cron.*.fired` and to nothing else, with no run yet (scripts' `S25-in-a-sandbox.md`, `S26-subscriptions.md`).
- cron holds no trigger, and no other script in the sandbox is subscribed to a pattern a `cron` event matches.
- No run is running or queued in scripts, so each fire's run starts as soon as its event is delivered.

Postconditions:

- cron holds no trigger: `<data>/apps/cron/state/cron.db` has no row for `<id>`, and the slug `tick` is free; a trigger created with the slug `tick` later is a new trigger with its own id (`S06`). No fire follows the deletion, and `on-tick` gets no run after it.
- Every run `on-tick` has is `exited` with exit code 0, and each is for one fire: its `input.json` holds that fire's event, byte for byte as the events app delivered it, an object of eleven members: `id`, beginning `evt_`; `time`; `service` `cron`; `event` `cron.tick.fired`; `request_id`, the id cron minted for the fire, 32 lowercase hexadecimal digits; `user` `<user-id>`, the trigger's owner; `attrs`, holding exactly `scheduled`, the fire's slot, `trigger` `<id>`, and `when` `* * * * *`; `cause` empty; `depth` 0; `seq`; and `received`. No two runs are for the same slot, and no run is for a slot that passed while the trigger was paused.
- The events app's log holds, from cron, `cron.tick.created`, one `cron.tick.fired` for each fire before the pause, `cron.tick.paused`, `cron.tick.resumed`, one `cron.tick.fired` for each fire after it, and `cron.tick.deleted`, in that order.
- The trail holds, under the id the sandbox's nginx gave each gateway request and user `<user-id>`: cron's `cron.tick.created`, then `tool.called` with `tool=create`, `kind=additive`, and `outcome=ok`; cron's `tool.called` with `tool=list` and `kind=read`; cron's `cron.tick.paused`, then `tool.called` with `tool=pause` and `kind=destructive`; cron's `cron.tick.resumed`, then `tool.called` with `tool=resume` and `kind=additive`; cron's `cron.tick.deleted`, then `tool.called` with `tool=delete` and `kind=destructive`, each with `trigger=<id>` and `when=* * * * *` on the trigger event and `outcome=ok` on the `tool.called`. Each fire's `cron.tick.fired`, with `scheduled`, `trigger=<id>`, and `when=* * * * *`, is under the request id cron minted for it and user `<user-id>` (`S11`, `S12`). No event holds `<email>` or the token.
- `sandbox logs cron` shows no line from cron: it wrote nothing to stderr.

## A guest in a sandbox asks for cron's landing page

cron serves nothing to guests, in a sandbox as on a space. The sandbox's nginx sends a guest who opens cron's own name to sign in at the sandbox's auth, carrying the URL it asked for, and the request never reaches cron.

Request:

```
$ curl -si http://cron.wip.localhost:7400/
```

Response:

```
HTTP/1.1 302 Moved Temporarily
Location: http://auth.wip.localhost:7400/?return=http://cron.wip.localhost:7400/
```

Status 302. The response carries no `WWW-Authenticate` header; the body is not fixed. `http://cron.wip.localhost:7400/about` answers the same, with the `return` ending `/about`.

Preconditions:

- The sandbox above is up.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached cron's socket, and cron recorded no event.

## An MCP client in a sandbox reaches cron's /mcp without a credential

The sandbox's nginx answers an MCP client with no credential at cron's `/mcp` with the bearer challenge it gives at any app (sandbox's `S4-routing.md`), and cron never sees the request.

Request:

```
$ curl -si -X POST http://cron.wip.localhost:7400/mcp
```

Response:

```
HTTP/1.1 401 Unauthorized
Content-Type: text/plain
WWW-Authenticate: Bearer realm="ikigenba", resource_metadata="http://mcp.wip.localhost:7400/.well-known/oauth-protected-resource"
```

Status 401. The body is the one line `authentication required: send Authorization: Bearer <token>` ending in a newline, where `<token>` is those seven characters as written, not a value filled in.

Preconditions:

- The sandbox above is up.
- The request carries no cookie and no `Authorization` header, so auth's `/check` answers 401.

Postconditions:

- Nothing has changed. Nothing reached cron's socket, and cron recorded no event.

## An agent asks a sandbox for cron's events and declarations paths

As on a space (`S15`), `/events` and `/declarations` at cron's public name answer 404 for every method, because the sandbox includes cron's `etc/nginx.conf` in its server. Every form below is answered the same way.

Request:

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" http://cron.wip.localhost:7400/events
```

```
$ curl -si -H "Authorization: Bearer $(sandbox token)" http://cron.wip.localhost:7400/events
```

```
$ curl -si -X POST -H "Authorization: Bearer $(sandbox token)" http://cron.wip.localhost:7400/declarations
```

```
$ curl -si -H "Authorization: Bearer $(sandbox token)" http://cron.wip.localhost:7400/declarations
```

Response:

```
HTTP/1.1 404 Not Found
```

Status 404. The body is not fixed. A request to either path with no token is answered 404 as well; this story does not fix which of the sandbox's refusals comes first, only that neither reaches cron.

Preconditions:

- The sandbox above is up from an `up` run with `cron/etc/nginx.conf` in the checkout, and `sandbox token` prints a token auth accepts.

Postconditions:

- Nothing has changed. Nothing reached cron's socket: cron recorded no event for any of the requests.
