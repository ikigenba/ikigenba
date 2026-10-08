# Stories — update

`update`, the tool that changes the schedule of one of the caller's triggers. It takes, in this order, `slug`, a string, the slug of one of the caller's triggers, and `when`, a string, the schedule the trigger fires on from now; both are required, and nothing else about a trigger can be changed: its slug, its id, its owner, and its `created` are fixed at create (`S06`), and its status changes only by `pause` and `resume` (`S09`). `when` is held to the rule `S05` and `S06` state for `create`, read in UTC, kept exactly as sent and answered back as sent. A `when` sent with the string the trigger already has is not a change; any other string is, even one that names the same slots. The checks run in this order — the trigger, that it exists and is the caller's; then `when` — and the first that fails is the whole answer, one line naming it. On an active trigger the new schedule takes effect at once: `next` becomes the first slot of the new schedule after the call, and the slot the old schedule called for next is not fired. On a paused trigger the schedule is changed and the trigger stays paused, with no `next`; `resume` (`S09`) starts it on the new schedule. `last_fired` is kept either way: it records a fire that happened, and the trigger still fires only a slot later than it (`S11`). It answers the trigger object `show` answers (`S07`), as it is after the call.

The actor, the request shape, the result envelope, and the fixture are those of `S06`: `S06`'s shared fixture, in which the caller `u_7f3a9c21` (`mg@example.com`) owns `hourly` (`crn_3f9a1c7e5b2d8046`, `@hourly`, active, created `2026-09-20T08:00:00Z`, last fired `2026-10-05T09:00:00Z`, next `2026-10-05T10:00:00Z`) and `weekly_digest` (`crn_5c7b9e2f4a6d1038`, `0 8 * * 1`, paused, created `2026-09-01T12:00:00Z`, last fired `2026-09-28T08:00:00Z`), and `u_2b8e1d04` (`ann@example.com`) owns `month_end` and `nightly_backup` (`crn_8d2e6b4a1f7c3095`, `30 2 * * *`, active); it is now `2026-10-05T09:32:00Z`. `update` is of kind `additive`, and its `tool.called` is as `S05` fixes it. `update` emits nothing to the event bus and records no event of its own: an update that changes the schedule and one that changes nothing leave the same trail, the request's `request.started`, `tool.called`, and `request.finished`. A refusal changes nothing. cron writes nothing to stderr for any answer in this group.

## A model changes the schedule of an active trigger

`hourly` fires on the hour. The model moves it to a quarter to the hour. The change takes effect at once: `next` is the first slot of the new schedule after the call, `09:45`, not the `10:00` the old schedule called for, and `last_fired` still records the fire at `09:00`. Nothing fires by the change.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"update","arguments":{"slug":"hourly","when":"45 * * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"crn_3f9a1c7e5b2d8046","slug":"hourly","when":"45 * * * *","owner":"mg@example.com","status":"active","created":"2026-09-20T08:00:00Z","last_fired":"2026-10-05T09:00:00Z","next":"2026-10-05T09:45:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly. `id`, `slug`, `owner`, `status`, `created`, and `last_fired` are unchanged.

Preconditions:

- The preamble's: `hourly` is active on `@hourly`, next `2026-10-05T10:00:00Z`.

Postconditions:

- `show` with `hourly` (`S07`) answers the object above.
- `hourly` fires next at `2026-10-05T09:45:00Z`, emitting `cron.hourly.fired` with `scheduled` `2026-10-05T09:45:00Z` and `when` `45 * * * *` (`S11`); it does not fire at `2026-10-05T10:00:00Z`.
- Nothing was emitted to the event bus.
- telemetry has received the request's three events, in this order, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"update"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A model changes the schedule of a paused trigger

`weekly_digest` is paused. Changing its schedule does not start it: it stays paused, with no `next`, and fires on the new schedule only once it is resumed (`S09`). The model need not resume a trigger to change its schedule, or pause it again after.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"update","arguments":{"slug":"weekly_digest","when":"0 9 * * 1"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"crn_5c7b9e2f4a6d1038","slug":"weekly_digest","when":"0 9 * * 1","owner":"mg@example.com","status":"paused","created":"2026-09-01T12:00:00Z","last_fired":"2026-09-28T08:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly. It has no `next`.

Preconditions:

- The preamble's: `weekly_digest` is paused on `0 8 * * 1`.

Postconditions:

- `weekly_digest` is paused on `0 9 * * 1`, and does not fire.
- A `resume` of `weekly_digest` after the call starts it on `0 9 * * 1` (`S09`).
- Nothing was emitted to the event bus. telemetry has received the request's `request.started`, `tool.called` with `kind` `additive` and `outcome` `ok`, and `request.finished`, and no other event.

## A model updates a trigger to the schedule it already has

The `when` sent is the one the trigger already has, so there is nothing to change. That is not a refusal: a model retrying an update that already happened gets the answer it would have got the first time. `next` is as it was. The comparison is of the string as sent, so `0 * * * *` sent to `hourly` is a change even though it names the same slots as `@hourly`: it is answered with `when` `0 * * * *` and, as any update, emits and records no event of its own.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"update","arguments":{"slug":"hourly","when":"@hourly"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"crn_3f9a1c7e5b2d8046","slug":"hourly","when":"@hourly","owner":"mg@example.com","status":"active","created":"2026-09-20T08:00:00Z","last_fired":"2026-10-05T09:00:00Z","next":"2026-10-05T10:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `hourly` is active on `@hourly`.

Postconditions:

- Nothing has changed: `hourly` fires next at `2026-10-05T10:00:00Z`.
- Nothing was emitted to the event bus. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"update"}}
  ```

## A model updates a trigger with a schedule cron does not allow

`when` is held to the rule `S05` and `S06` state for `create`: exactly one of the five shorthands, or exactly five fields separated by single spaces. `@every 15m` is neither, and is refused quoting it as sent, as `@annually`, `@midnight`, `@reboot`, `@DAILY`, or a schedule with a leading or trailing space would be. The trigger keeps the schedule it had.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"update","arguments":{"slug":"hourly","when":"@every 15m"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid when '@every 15m'
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `hourly` is active on `@hourly` and fires next at `2026-10-05T10:00:00Z`.
- The request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model calls update without a schedule

`update` exists to change the schedule, so `when` is required: a call naming only the trigger asks for nothing and is refused as its arguments are read, before the trigger is looked up (`S05`), so the model is not left believing it changed something.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"update","arguments":{"slug":"hourly"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
when: missing required field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`); the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"additive","outcome":"invalid_arguments","tool":"update"}}
  ```

## A model updates another user's trigger

Only a trigger's owner changes its schedule. Another user's trigger gets exactly the answer a trigger that does not exist gets, though `list` and `show` (`S07`) show it to the caller.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"update","arguments":{"slug":"nightly_backup","when":"0 3 * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no trigger named 'nightly_backup'
```

Preconditions:

- The preamble's: `nightly_backup` is `u_2b8e1d04`'s, active on `30 2 * * *`.

Postconditions:

- Nothing has changed: `nightly_backup` is active on `30 2 * * *` and fires next at `2026-10-06T02:30:00Z`.
- The request's `tool.called` has `kind` `additive` and `outcome` `error`, as for a trigger that does not exist.

## A model updates a trigger that does not exist

A slug no trigger has is refused, quoting it as sent. The trigger is looked up before `when` is looked at, so the same answer comes whatever `when` the call sent, `@every 15m` included.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"update","arguments":{"slug":"daily_report","when":"@daily"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no trigger named 'daily_report'
```

Preconditions:

- The preamble's: no trigger is named `daily_report`.

Postconditions:

- Nothing has changed; no trigger was created.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"update"}}
  ```

## A model sends arguments update does not take

A field of the wrong JSON type, or one the tool does not have, is refused as the arguments are read, every offence in one answer: the tool's fields first, in the order of its input schema, then each unknown field in the order it was sent (`S05`). Here the model sent `when` as a number, tried to pause the trigger through `update`, and tried to rename it, none of which `update` does: a trigger's status changes by `pause` and `resume` (`S09`), and its slug is fixed at create, since it is the middle word of every event it emits.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: update

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"update","arguments":{"slug":"hourly","when":60,"status":"paused","new_slug":"every_hour"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
when: expected string, got number
status: unknown field
new_slug: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `hourly` is still named `hourly`, active on `@hourly`.
- Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`); the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"additive","outcome":"invalid_arguments","tool":"update"}}
  ```
