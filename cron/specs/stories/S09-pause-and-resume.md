# Stories — pause and resume

`pause` and `resume`, the two tools that stop one of the caller's triggers firing and start it again. Each takes one argument, `slug`, required, a string, the slug of one of the caller's triggers. `pause` makes an active trigger `paused`: it fires no more, and it has no `next` until it is resumed. `resume` makes a paused trigger `active` again: its `next` is the first slot of its schedule after the call, and it fires from that slot on; a slot that fell due while it was paused is not made up, not even the latest one. `last_fired` is kept across both. Pausing a trigger that is already paused, or resuming one that is already active, is not a change and not a refusal: the call answers the trigger as it is. Each refuses a slug that no trigger has, or that names another user's trigger, with `no trigger named '<slug>'`, quoting it as sent; only a trigger's owner pauses or resumes it, though `list` and `show` (`S07`) show it to everyone. Each answers the trigger object `show` answers (`S07`), as it is after the call. A paused trigger is one its owner stopped; it has nothing to do with a subscriber the event bus holds back after failed deliveries, which the bus calls paused too, and neither tool touches the bus's subscribers.

The actor, the request shape, the result envelope, and the fixture are those of `S06`: `S06`'s shared fixture, in which the caller `u_7f3a9c21` (`mg@example.com`) owns `hourly` (`crn_3f9a1c7e5b2d8046`, `@hourly`, active, created `2026-09-20T08:00:00Z`, last fired `2026-10-05T09:00:00Z`, next `2026-10-05T10:00:00Z`) and `weekly_digest` (`crn_5c7b9e2f4a6d1038`, `0 8 * * 1`, paused, created `2026-09-01T12:00:00Z`, last fired `2026-09-28T08:00:00Z`), and `u_2b8e1d04` (`ann@example.com`) owns `month_end` (`crn_1a4f8c6e9b3d7025`, `@monthly`, active, next `2026-11-01T00:00:00Z`) and `nightly_backup` (`crn_8d2e6b4a1f7c3095`, `30 2 * * *`, active, next `2026-10-06T02:30:00Z`); it is now `2026-10-05T09:32:00Z`, a Monday, and telemetry and the events app take every event. `pause` is of kind `destructive` and `resume` of kind `additive`, and their `tool.called` is as `S05` fixes it. A `pause` that pauses a trigger emits `cron.<slug>.paused` to the event bus, and a `resume` that resumes one emits `cron.<slug>.resumed`, each with the attributes `trigger`, the trigger's id, and `when`, its schedule, under the caller's user id and the request's id, and records the same event, by the same name and with the same attributes, in the trail, between the request's `request.started` and its `tool.called`. The event the bus receives is shown as the JSON object it receives, where `<event-id>` is the id cron gives it, `evt_` and 16 lowercase hexadecimal digits, `<time>` is when cron emitted it, UTC to the microsecond, and `cause` and `depth` are as `S06` tells; no request here carries `X-Event-Cause` or `X-Event-Depth`, so `cause` is empty and `depth` is 0. A call that changes nothing, and a refusal, emit nothing and record no `cron.*` event. The owner's email is in no event. cron writes nothing to stderr for any answer in this group.

## A model pauses an active trigger

`hourly` fires every hour. The model stops it. It is paused at once: the `10:00` slot it was due to fire next is not fired, nor any slot after, until it is resumed. Its schedule and `last_fired` are kept, so a `resume` picks it up where its schedule says.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: pause

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pause","arguments":{"slug":"hourly"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"crn_3f9a1c7e5b2d8046","slug":"hourly","when":"@hourly","owner":"mg@example.com","status":"paused","created":"2026-09-20T08:00:00Z","last_fired":"2026-10-05T09:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly. It has no `next`.

Preconditions:

- The preamble's: `hourly` is active, next `2026-10-05T10:00:00Z`.

Postconditions:

- `show` with `hourly` (`S07`) answers the object above.
- `hourly` does not fire at `2026-10-05T10:00:00Z` or at any slot after, until it is resumed: cron emits no `cron.hourly.fired` (`S11`).
- The events app has received one event from cron, where `<request-id>` is the request's id (`S02`):

  ```
  {"id":"<event-id>","time":"<time>","service":"cron","event":"cron.hourly.paused","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"trigger":"crn_3f9a1c7e5b2d8046","when":"@hourly"},"cause":"","depth":0}
  ```

- telemetry has received the request's four events, in this order:

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"cron","event":"cron.hourly.paused","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"trigger":"crn_3f9a1c7e5b2d8046","when":"@hourly"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"pause"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A model pauses a trigger that is already paused

`weekly_digest` is paused already, so there is nothing to stop. That is not a refusal: a model retrying a pause that already happened gets the answer it would have got the first time.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: pause

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"pause","arguments":{"slug":"weekly_digest"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"crn_5c7b9e2f4a6d1038","slug":"weekly_digest","when":"0 8 * * 1","owner":"mg@example.com","status":"paused","created":"2026-09-01T12:00:00Z","last_fired":"2026-09-28T08:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: `weekly_digest` is paused.

Postconditions:

- Nothing has changed.
- Nothing was emitted to the event bus. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"pause"}}
  ```

## A model resumes a paused trigger

`weekly_digest` fires on Mondays at `08:00`, last fired on `2026-09-28`, and was paused after. The model starts it again at `09:32` on a Monday. This morning's `08:00` slot fell due while it was paused and is not made up: nothing fires by the call, and its `next` is the first slot after the call, next Monday's.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
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

and a `content` array of one text block whose text is that object encoded compactly. `last_fired` is still the last fire before the pause.

Preconditions:

- The preamble's: `weekly_digest` is paused, last fired `2026-09-28T08:00:00Z`.

Postconditions:

- `show` with `weekly_digest` (`S07`) answers the object above.
- No `cron.weekly_digest.fired` was emitted by the call, for `2026-10-05T08:00:00Z` or any other slot that fell due while it was paused. `weekly_digest` fires next at `2026-10-12T08:00:00Z` (`S11`).
- The events app has received one event from cron, where `<request-id>` is the request's id (`S02`):

  ```
  {"id":"<event-id>","time":"<time>","service":"cron","event":"cron.weekly_digest.resumed","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"trigger":"crn_5c7b9e2f4a6d1038","when":"0 8 * * 1"},"cause":"","depth":0}
  ```

- telemetry has received the request's four events, in this order:

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"cron","event":"cron.weekly_digest.resumed","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"trigger":"crn_5c7b9e2f4a6d1038","when":"0 8 * * 1"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"resume"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A model resumes a trigger that is already active

`hourly` is active, so there is nothing to start. That is not a refusal: the call answers the trigger as it is, and its `next` is the slot it was already due to fire, not one recomputed by the call.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: resume

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"resume","arguments":{"slug":"hourly"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

- The preamble's: `hourly` is active.

Postconditions:

- Nothing has changed: `hourly` fires next at `2026-10-05T10:00:00Z`.
- Nothing was emitted to the event bus. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"resume"}}
  ```

## A model pauses another user's trigger

Only a trigger's owner pauses it. Another user's trigger gets exactly the answer a trigger that does not exist gets, and goes on firing.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: pause

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"pause","arguments":{"slug":"nightly_backup"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no trigger named 'nightly_backup'
```

Preconditions:

- The preamble's: `nightly_backup` is `u_2b8e1d04`'s and active.

Postconditions:

- Nothing has changed: `nightly_backup` is active and fires next at `2026-10-06T02:30:00Z`.
- Nothing was emitted to the event bus. The request's `tool.called` has `kind` `destructive` and `outcome` `error`, as for a trigger that does not exist.

## A model resumes another user's trigger

Only a trigger's owner resumes it, and another user's trigger gets exactly the answer a trigger that does not exist gets, whatever its status. `month_end` is active; the answer would be the same were it paused.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: resume

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"resume","arguments":{"slug":"month_end"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no trigger named 'month_end'
```

Preconditions:

- The preamble's: `month_end` is `u_2b8e1d04`'s and active.

Postconditions:

- Nothing has changed: `month_end` is active and fires next at `2026-11-01T00:00:00Z`.
- Nothing was emitted to the event bus. The request's `tool.called` has `kind` `additive` and `outcome` `error`, as for a trigger that does not exist.

## A model pauses or resumes a trigger that does not exist

A slug no trigger has is refused by either tool, quoting it as sent.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: pause

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"pause","arguments":{"slug":"daily_report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: resume

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"resume","arguments":{"slug":"daily_report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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
- Nothing was emitted to the event bus. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`), `<tool>` is the tool called, and `<kind>` is `destructive` for `pause` and `additive` for `resume`:

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"<kind>","outcome":"error","tool":"<tool>"}}
  ```

## A model calls pause or resume without saying which trigger

`slug` is required and is the only argument. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer (`S05`). Here the model named the trigger by its id under a key the tool does not have.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: pause

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"pause","arguments":{"id":"crn_3f9a1c7e5b2d8046"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: resume

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"resume","arguments":{"id":"crn_5c7b9e2f4a6d1038"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
slug: missing required field
id: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed: `hourly` is still active and `weekly_digest` still paused.
- Nothing was emitted to the event bus. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`), `<tool>` is the tool called, and `<kind>` is `destructive` for `pause` and `additive` for `resume`; the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"<kind>","outcome":"invalid_arguments","tool":"<tool>"}}
  ```
