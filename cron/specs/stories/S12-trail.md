# Stories — trail

What cron's trail holds, and what it never holds. cron records what it does as events it sends to the platform's telemetry service; how it finds telemetry, and what it does with an event telemetry cannot take, is told in `S02`. An operator, or an agent working for one, follows what happened from one thing they know — a request id, a user, a trigger's id, or a time. The stories show each event as the JSON object telemetry receives:

```
{"time":"<time>","service":"cron","event":"<event>","request_id":"<request-id>","user":"<user>","attrs":{<attributes>}}
```

`<time>` is when cron recorded the event, in UTC to the microsecond, as `2026-10-05T09:14:02.123456Z`; `service` is always `cron`; `<request-id>` and `<user>` are the id of the request that caused the event and the caller's `X-User-Id`. Both are empty for a start or a stop, which no request caused. A fire is caused by no request: it carries the request id cron made for that fire alone and the trigger's owner as its user (`S11`), and is never empty in either. `attrs` holds the event's attributes, flat, their keys in alphabetical order. A trigger is named in an attribute by its id, under the key `trigger`, so its trail can be followed by that id from its creation to its deletion; its slug is the middle word of its `cron.*` events' names. No event carries a user's email, a request's query, a tool's arguments beyond the `when` a trigger's events carry, or the text of a refusal.

cron records these events and no others:

- `service.started`, once cron is serving, with `version`; and `service.stopping`, with `reason`, `SIGTERM` or `SIGINT`, its last event except the `event.lost` of a bus event dropped when a stop's drain ends and the `request.finished` of a request cut off at the drain deadline, which follow it (`S02`, `S13`);
- `request.started`, as each request arrives, with `method` and `path`; and `request.finished`, once its answer is complete, with `status`, `duration_us`, `request_bytes`, and `response_bytes`, shown as `<n>` and `<bytes>` unless a story fixes them (`S02`);
- `tool.called`, for each call of one of its seven tools answered with a result, with `tool`, `kind`, `outcome`, and `duration_us` (`S05`);
- `cron.<slug>.created`, `cron.<slug>.paused`, `cron.<slug>.resumed`, and `cron.<slug>.deleted`, one for each `create`, `pause`, `resume`, and `delete` that changes a trigger, where `<slug>` is the trigger's slug, each with `trigger`, its id, and `when`, its schedule as it stands, exactly as it was sent (`S06`, `S09`, `S10`); `update` changes a trigger too but records no event of its own, only its `tool.called` (`S08`), and a `pause` of a paused trigger or a `resume` of an active one changes nothing and records none (`S09`);
- `cron.<slug>.fired`, for each fire, with `scheduled`, the slot it fired for; `trigger`; and `when` (`S11`);
- `event.lost`, when cron drops an event it emitted to the event bus because the bus did not take it within its retry window, or before a stop's drain ended, carrying the dropped event's `id` among its attributes (`S11`, `S13`).

Each `cron.*` event is also the event cron emits on the event bus, under the same name, with the same request id, user, and attributes; on the bus it has three members more, its own `id`, `cause`, and `depth` (`S11`), which the trail does not carry. A request's events carry its request id and its caller and come in this order: `request.started`, then its `cron.*` event if it changed a trigger, then its `tool.called` if it called a tool, then `request.finished`. A tool call that is refused records no `cron.*` event. A request that changes no trigger — a page, `/_appkit/`, a tool that only reads, an `update`, a refused call — records only its `request.started`, its `tool.called` if it called a tool, and its `request.finished`. A fire records only its `cron.<slug>.fired`, with no request around it. Unless a story says otherwise, cron runs on the host with the suite's services file (`S03`), whose `telemetry` entry takes every event and whose `events` entry, the event bus, takes every event; the time is `2026-10-05T09:32:00Z`; cron's database holds `S06`'s triggers, in which the caller `u_7f3a9c21` (`mg@example.com`) owns `hourly` (`crn_3f9a1c7e5b2d8046`, `@hourly`, active, last fired `2026-10-05T09:00:00Z`) and `weekly_digest` (`crn_5c7b9e2f4a6d1038`, `0 8 * * 1`, paused, last fired `2026-09-28T08:00:00Z`), and `u_2b8e1d04` (`ann@example.com`) owns `month_end` and `nightly_backup`; and requests come through nginx or the gateway as `S05` shows them, with `X-Request-Id` `7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9` unless a story fixes another.

## An operator follows a trigger's creation

`create` (`S06`) records `cron.<slug>.created`, named for the new trigger's slug, with its id and its schedule. The id is how the rest of the trigger's trail is found, whatever later becomes of the slug.

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create","arguments":{"slug":"crm_sync","when":"*/15 * * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","slug":"crm_sync","when":"*/15 * * * *","owner":"mg@example.com","status":"active","created":"2026-10-05T09:32:00Z","next":"2026-10-05T09:45:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the id cron gave the trigger, `crn_` and 16 lowercase hexadecimal digits.

Preconditions:

- The preamble's: no trigger has the slug `crm_sync`.

Postconditions:

- cron's database holds `crm_sync`, as `S06` tells.
- telemetry has received the request's four events, in this order:

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"cron","event":"cron.crm_sync.created","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"trigger":"<id>","when":"*/15 * * * *"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"create"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `mg@example.com` is in none of them.
- The event bus has received the same `cron.crm_sync.created`, under the same request id and user, with the same attributes.

## An operator follows a trigger being paused

`pause` (`S09`) records `cron.<slug>.paused`, with the trigger's id and its schedule. A `pause` of a trigger already paused changes nothing and records no `cron.*` event, only its `tool.called` (`S09`).

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: pause

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"pause","arguments":{"slug":"hourly"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"crn_3f9a1c7e5b2d8046","slug":"hourly","when":"@hourly","owner":"mg@example.com","status":"paused","created":"2026-09-20T08:00:00Z","last_fired":"2026-10-05T09:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `hourly` is the caller's and active.

Postconditions:

- `hourly` is paused (`S09`).
- Between the request's `request.started` and its `request.finished`, telemetry has received, in this order:

  ```
  {"time":"<time>","service":"cron","event":"cron.hourly.paused","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"trigger":"crn_3f9a1c7e5b2d8046","when":"@hourly"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"pause"}}
  ```

- The event bus has received the same `cron.hourly.paused`.

## An operator follows a trigger being resumed

`resume` (`S09`) records `cron.<slug>.resumed`, with the trigger's id and its schedule. Its slots missed while it was paused are not made up, so nothing fires at the resume, and the trail holds no `cron.*.fired` for them. A `resume` of a trigger already active changes nothing and records no `cron.*` event (`S09`).

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: resume

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"resume","arguments":{"slug":"weekly_digest"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"crn_5c7b9e2f4a6d1038","slug":"weekly_digest","when":"0 8 * * 1","owner":"mg@example.com","status":"active","created":"2026-09-01T12:00:00Z","last_fired":"2026-09-28T08:00:00Z","next":"2026-10-12T08:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `weekly_digest` is the caller's and paused; its slot `2026-10-05T08:00:00Z` passed while it was paused.

Postconditions:

- `weekly_digest` is active, and fires next at `2026-10-12T08:00:00Z` (`S09`).
- Between the request's `request.started` and its `request.finished`, telemetry has received, in this order, and no `cron.weekly_digest.fired`:

  ```
  {"time":"<time>","service":"cron","event":"cron.weekly_digest.resumed","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"trigger":"crn_5c7b9e2f4a6d1038","when":"0 8 * * 1"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"resume"}}
  ```

- The event bus has received the same `cron.weekly_digest.resumed`.

## An operator finds no trigger event for an update

`update` (`S08`) changes a trigger's schedule and records no `cron.*` event, and emits nothing on the bus: the trail shows only that the tool was called. The schedule it had is in the `when` of the trigger's earlier events; the one it has now is read from the trigger itself, with `show` (`S07`), and is in the `when` of every later event of the trigger.

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"update","arguments":{"slug":"hourly","when":"30 * * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"crn_3f9a1c7e5b2d8046","slug":"hourly","when":"30 * * * *","owner":"mg@example.com","status":"active","created":"2026-09-20T08:00:00Z","last_fired":"2026-10-05T09:00:00Z","next":"2026-10-05T10:30:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `hourly` is the caller's, active, with `when` `@hourly`.

Postconditions:

- `hourly`'s `when` is `30 * * * *` (`S08`).
- telemetry has received the request's three events, in this order, and no `cron.*` event:

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"update"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `30 * * * *` is in none of them.
- The event bus has received nothing from cron for the call.

## An operator follows a trigger's deletion

`delete` (`S10`) records `cron.<slug>.deleted`, with the trigger's id and the schedule it had. The trigger's earlier events still name that id, so its whole trail, from `cron.<slug>.created` through its fires to `cron.<slug>.deleted`, can be followed after it is gone. Ids are drawn at random and are not derived from the slug; a trigger created with the slug later is a new trigger with its own id.

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"delete","arguments":{"slug":"hourly"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"deleted":true,"id":"crn_3f9a1c7e5b2d8046"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `hourly` is the caller's.

Postconditions:

- cron's database holds no `hourly`, as `S10` tells.
- Between the request's `request.started` and its `request.finished`, telemetry has received, in this order:

  ```
  {"time":"<time>","service":"cron","event":"cron.hourly.deleted","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"trigger":"crn_3f9a1c7e5b2d8046","when":"@hourly"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"delete"}}
  ```

- The event bus has received the same `cron.hourly.deleted`.
- No later event is for the deleted trigger (`S11`).

## An operator follows a fire

A fire (`S11`) is in the trail as one event, `cron.<slug>.fired`, with no request around it: no `request.started`, no `request.finished`, and no `tool.called`. It carries the request id cron made for that fire and no other, so a trace of that id finds the fire alone, and the trigger's owner as its user, whoever created or last changed the trigger. A run a script makes on that event is told in scripts' own trail.

Command:

```
$ cron
```

Output:

```
```

Does not exit. Nothing is on stdout or stderr.

Preconditions:

- The preamble's: cron has been serving since `2026-10-05T09:32:00Z`, and `hourly` is active with `next` `2026-10-05T10:00:00Z`.
- The clock passes `2026-10-05T10:00:00Z`.

Postconditions:

- telemetry has received, at the fire, exactly one event from cron:

  ```
  {"time":"<time>","service":"cron","event":"cron.hourly.fired","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"scheduled":"2026-10-05T10:00:00Z","trigger":"crn_3f9a1c7e5b2d8046","when":"@hourly"}}
  ```

  `<request-id>` is 32 lowercase hexadecimal digits, made for this fire; no other event in cron's trail carries it. `mg@example.com` is not in it.
- The event bus has received the same `cron.hourly.fired`, under the same request id and user (`S11`).

## An operator finds no trigger event for a refused call

A call that is refused changed no trigger, so the trail says only that the call was made and refused: its `tool.called` with `outcome` `error`, and no `cron.*` event. Here `ann@example.com` tries to pause `mg@example.com`'s `hourly`, which only its owner may pause; the slug she sent and the text of the refusal are not recorded. Every other refusal of every tool (`S05` to `S10`) records no `cron.*` event either, and its `tool.called`, if it records one, is as `S05` fixes it.

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
X-Request-Id: 7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: pause

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"pause","arguments":{"slug":"hourly"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no trigger named 'hourly'
```

Preconditions:

- The preamble's: `hourly` is `u_7f3a9c21`'s, not the caller's, and active.

Postconditions:

- Nothing has changed: `hourly` is active, as it was (`S09`).
- telemetry has received the request's three events, in this order, and no `cron.*` event:

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_2b8e1d04","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_2b8e1d04","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"error","tool":"pause"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9","user":"u_2b8e1d04","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `hourly` and `crn_3f9a1c7e5b2d8046` are in none of them.
- The event bus has received nothing from cron for the call.

## An operator follows a user's visit to the landing page

A page records only the request's own pair: there is no event for viewing the triggers, though the page shows every one (`S03`).

Request:

```
GET /?from=launcher HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: a7e3c1f9b5d2084e6c8a0f2d4b6e9c13
```

Response:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
```

Status 200. The body is the landing page (`S03`), an HTML page whose `h1` reads `cron`.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed but the trail. telemetry has received the request's two events, in this order:

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"a7e3c1f9b5d2084e6c8a0f2d4b6e9c13","user":"u_7f3a9c21","attrs":{"method":"GET","path":"/"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"a7e3c1f9b5d2084e6c8a0f2d4b6e9c13","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":0,"response_bytes":<bytes>,"status":200}}
  ```

  `from=launcher` and `mg@example.com` are in neither.
- The event bus has received nothing from cron.

## An operator follows cron's start

A start records `service.started`, with `<display>` as its `version`, and nothing for the triggers: starting fires nothing and makes up no slot (`S11`), so no `cron.*` event comes with it. A new value there is how a deploy shows in the trail.

Command:

```
$ sudo systemctl start ikigenba-cron.service
```

Output:

```
```

Exits 0. Nothing is on stdout or stderr.

Preconditions:

- `opsctl install` has installed cron, and `ikigenba-cron.socket` is active (`S02`); `ikigenba-cron.service` is not running.
- `/opt/cron/state/cron.db` holds `S06`'s triggers.

Postconditions:

- cron is serving on `/run/ikigenba/cron.sock`.
- telemetry has received one event from cron, with no request id and no user, whose `version` is `<display>`, the string `cron --version` prints under the environment the host gives cron (`S01`), the empty string when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`, and no `cron.*` event:

  ```
  {"time":"<time>","service":"cron","event":"service.started","request_id":"","user":"","attrs":{"version":"<display>"}}
  ```

## An operator follows cron's stop

A stop records `service.stopping`, with the signal that stopped it, once every request it accepted has finished, or at the drain deadline for any still running; it is cron's last event, except the `event.lost` of a bus event dropped when the drain ends and the `request.finished` of a request cut off at the drain deadline, which follow it (`S02`, `S13`). It records nothing for the triggers: a stop changes no trigger, and a slot that comes once cron has been told to stop does not fire, so it records no `cron.*.fired` either (`S13`). A `service.started` with no `service.stopping` before the next one is how the trail shows a cron that died rather than stopped.

Command:

```
$ kill -TERM <pid>
```

Output:

```
```

cron exits 0. Nothing is on stdout or stderr.

Preconditions:

- cron is serving as process `<pid>`, on the socket it was passed, over `S06`'s triggers; no request is running at the signal and none arrives after it, and cron holds no event the bus has not taken.

Postconditions:

- telemetry has received, last of cron's events, with no request id and no user:

  ```
  {"time":"<time>","service":"cron","event":"service.stopping","request_id":"","user":"","attrs":{"reason":"SIGTERM"}}
  ```

  With `SIGINT` the `reason` is `SIGINT`. No `cron.*` event came with it.
