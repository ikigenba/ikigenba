# Stories — list and show

`list` and `show`, the two tools that tell a caller what triggers the space has. Both are of kind `read`, change nothing, emit nothing to the event bus, and see every trigger in the space, whoever owns it: a trigger is not private, and any signed-in user may list it, show it, and subscribe to its events. Only changing one is the owner's alone (`S08`, `S09`, `S10`). `list` takes no arguments and answers `{"triggers":[...]}`, one entry per trigger in the space, sorted by slug ascending, each with the members, in this order, `id`, `slug`, `when`, `owner`, `status`, `last_fired`, and `next`, under the rules `S05` fixes: `last_fired` is absent until the trigger has fired and `next` is absent while it is paused; a space with no triggers gets `{"triggers":[]}`. `show` takes one argument, `slug`, required, a string, the trigger's slug, looked up among every trigger in the space; a trigger's id is not its slug. It answers the trigger object `create` answers (`S06`), members in the same order — `id`, `slug`, `when`, `owner`, `status`, `created`, `last_fired`, `next` — under the same rules. `when` is the schedule exactly as it was sent; `owner` is the owner's email; `last_fired` is the slot of the trigger's latest fire, the `scheduled` time of its latest `cron.<slug>.fired` (`S11`), not the moment it was emitted; and `next` is the next slot it will fire. A `slug` that names no trigger is refused with exactly `no trigger named '<slug>'`, quoting the value as sent.

The actor, the request shape, the result envelope, and the fixture are those of `S06`, whose shared fixture holds, it being `2026-10-05T09:32:00Z`: the caller `u_7f3a9c21`'s `hourly` (`crn_3f9a1c7e5b2d8046`, `@hourly`, active, last fired `2026-10-05T09:00:00Z`, next `2026-10-05T10:00:00Z`) and `weekly_digest` (`crn_5c7b9e2f4a6d1038`, `0 8 * * 1`, paused, last fired `2026-09-28T08:00:00Z`), and `u_2b8e1d04`'s `month_end` (`crn_1a4f8c6e9b3d7025`, `@monthly`, active, never fired, next `2026-11-01T00:00:00Z`) and `nightly_backup` (`crn_8d2e6b4a1f7c3095`, `30 2 * * *`, active, last fired `2026-10-05T02:30:00Z`, next `2026-10-06T02:30:00Z`). Every story is read-only: nothing changes but the trail, which gains the request's `request.started`, its `tool.called` with `tool` `list` or `show`, `kind` `read`, and the outcome `S05` fixes, and its `request.finished`; neither tool records a `cron.*` event, and neither touches a trigger's schedule, so every active trigger fires at its next slot as before. cron writes nothing to stderr for any answer in this group.

## A model lists the triggers

The ordinary case: every trigger in the space, by slug, the caller's and `ann@example.com`'s alike, each with its schedule, its owner, its status, and when it last fired and fires next. `month_end` has never fired, so its entry has no `last_fired`; `weekly_digest` is paused, so its entry has no `next`, though it keeps the `last_fired` of the fire before it was paused. A model tells its own triggers from others' by `owner`. A call may leave `arguments` out or send `{}`; both are the same call.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"triggers":[{"id":"crn_3f9a1c7e5b2d8046","slug":"hourly","when":"@hourly","owner":"mg@example.com","status":"active","last_fired":"2026-10-05T09:00:00Z","next":"2026-10-05T10:00:00Z"},{"id":"crn_1a4f8c6e9b3d7025","slug":"month_end","when":"@monthly","owner":"ann@example.com","status":"active","next":"2026-11-01T00:00:00Z"},{"id":"crn_8d2e6b4a1f7c3095","slug":"nightly_backup","when":"30 2 * * *","owner":"ann@example.com","status":"active","last_fired":"2026-10-05T02:30:00Z","next":"2026-10-06T02:30:00Z"},{"id":"crn_5c7b9e2f4a6d1038","slug":"weekly_digest","when":"0 8 * * 1","owner":"mg@example.com","status":"paused","last_fired":"2026-09-28T08:00:00Z"}]}
```

and a `content` array of one text block whose text is exactly that object encoded compactly. `ann@example.com`, calling `list` as `u_2b8e1d04`, gets the same answer, member for member.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed, and nothing was emitted to the event bus.
- telemetry has received the request's three events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"list"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  No trigger's id or slug is in them.

## A model lists the triggers when there are none

A space with no triggers gets an empty list, not an error.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"triggers":[]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- cron holds no trigger: none has been created, or every one has been deleted (`S10`).

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `list`, `kind` `read`, and `outcome` `ok`.

## A model sends list an argument

`list` takes no arguments, so anything sent to it is an unknown field, refused as its arguments are read (`S05`). A model that believes it narrowed the listing to one trigger must learn that it did not; `show` answers one trigger.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list","arguments":{"slug":"hourly"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
slug: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `list`, `kind` `read`, `outcome` `invalid_arguments`, and `duration_us` 0.

## A model shows a trigger that has fired

The ordinary case, and how a model learns everything cron holds about one trigger: its schedule, its owner, when it was created, the slot it last fired for, and the slot it fires next. `hourly` last fired for the slot `2026-10-05T09:00:00Z`; `last_fired` is that slot, the `scheduled` of its latest `cron.hourly.fired`, even if the event went out a moment later (`S11`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"show","arguments":{"slug":"hourly"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"crn_3f9a1c7e5b2d8046","slug":"hourly","when":"@hourly","owner":"mg@example.com","status":"active","created":"2026-09-20T08:00:00Z","last_fired":"2026-10-05T09:00:00Z","next":"2026-10-05T10:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. `hourly` fires at `2026-10-05T10:00:00Z` as before.
- telemetry has received the request's `request.started`, then `{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"show"}` as the attributes of its `tool.called`, then its `request.finished` with `status` 200. Neither the id nor the slug of `hourly` is in them.

## A model shows a trigger that has never fired

A trigger whose first slot has not come yet has no `last_fired`; it has everything else. Here `ann@example.com` shows her own `month_end`, created the day before, whose first slot is the first of next month.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_2b8e1d04
X-User-Email: ann@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"show","arguments":{"slug":"month_end"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"crn_1a4f8c6e9b3d7025","slug":"month_end","when":"@monthly","owner":"ann@example.com","status":"active","created":"2026-10-04T10:00:00Z","next":"2026-11-01T00:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's; the caller is `u_2b8e1d04` (`ann@example.com`), and `month_end` has never fired.

Postconditions:

- Nothing has changed. The request's `tool.called`, under user `u_2b8e1d04`, has `tool` `show`, `kind` `read`, and `outcome` `ok`.

## A model shows a paused trigger

A paused trigger fires nothing until it is resumed, so it has no next slot and its answer has no `next`; its `status` says why. It keeps its schedule and the `last_fired` of the fire before it was paused. Resumed, it would fire again from the first slot after the resume, the slots it missed while paused never made up (`S09`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"show","arguments":{"slug":"weekly_digest"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"crn_5c7b9e2f4a6d1038","slug":"weekly_digest","when":"0 8 * * 1","owner":"mg@example.com","status":"paused","created":"2026-09-01T12:00:00Z","last_fired":"2026-09-28T08:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `weekly_digest` is paused, so its slot `2026-10-05T08:00:00Z` did not fire.

Postconditions:

- Nothing has changed. `weekly_digest` is still paused; showing it neither resumes it nor fires it.

## A model shows another user's trigger

Every trigger is visible to every signed-in user, so `show` answers another user's trigger as it answers the caller's own, owner and all. A model that finds a trigger it wants to react to subscribes to its events without asking its owner; it cannot change it, since `update`, `pause`, `resume`, and `delete` refuse a trigger the caller does not own as one that does not exist (`S08`, `S09`, `S10`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"show","arguments":{"slug":"nightly_backup"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"crn_8d2e6b4a1f7c3095","slug":"nightly_backup","when":"30 2 * * *","owner":"ann@example.com","status":"active","created":"2026-09-28T17:15:00Z","last_fired":"2026-10-05T02:30:00Z","next":"2026-10-06T02:30:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `nightly_backup` is `u_2b8e1d04`'s, and the caller is `u_7f3a9c21`.

Postconditions:

- Nothing has changed. The request's `tool.called`, under user `u_7f3a9c21`, has `tool` `show`, `kind` `read`, and `outcome` `ok`.

## A model shows a trigger that does not exist

A slug that names no trigger is refused, quoting it as sent. The same answer comes for a slug that breaks the rule for slugs, since it can name nothing, and for a trigger's id, which is not its slug: `crn_3f9a1c7e5b2d8046` gets `no trigger named 'crn_3f9a1c7e5b2d8046'`. A trigger that has been deleted is answered the same way (`S10`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"show","arguments":{"slug":"cleanup"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no trigger named 'cleanup'
```

Preconditions:

- The preamble's: no trigger has the slug `cleanup`.

Postconditions:

- Nothing has changed.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"error","tool":"show"}}
  ```

## A model calls show without saying which trigger

`slug` is required. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer (`S05`). Here the model named the trigger `name`, which `show` does not take.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: show

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"show","arguments":{"name":"hourly"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
slug: missing required field
name: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed. The request's `tool.called` has `tool` `show`, `kind` `read`, `outcome` `invalid_arguments`, and `duration_us` 0.
