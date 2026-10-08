# Stories — delete

`delete`, the tool that removes one of the caller's triggers for good. Its one argument is `slug`, required, a string, the slug of one of the caller's triggers. The trigger's record goes, whatever its status, and with it its schedule: a slot that falls due after the call fires nothing for it. It answers `{"deleted":true,"id":"<trigger id>"}`. There is no undo; once the call has answered, `list` (`S07`) no longer holds the trigger, the page at `/` has no row for it (`S03`), and `show`, `update`, `pause`, `resume`, and `delete` with its slug are refused with `no trigger named '<slug>'`. Its slug is free at once for any user's `create` (`S06`): ids are drawn at random and are not derived from the slug; a trigger created with the slug later is a new trigger with its own id. It refuses a slug that no trigger has, or that names another user's trigger, with `no trigger named '<slug>'`, quoting it as sent; only a trigger's owner deletes it, though `list` and `show` (`S07`) show it to everyone. `delete` is of kind `destructive`.

The actor, the request shape, the result envelope, and the fixture are those of `S06`: `S06`'s shared fixture, in which the caller `u_7f3a9c21` (`mg@example.com`) owns `hourly` (`crn_3f9a1c7e5b2d8046`, `@hourly`, active, created `2026-09-20T08:00:00Z`, last fired `2026-10-05T09:00:00Z`, next `2026-10-05T10:00:00Z`) and `weekly_digest` (`crn_5c7b9e2f4a6d1038`, `0 8 * * 1`, paused, created `2026-09-01T12:00:00Z`, last fired `2026-09-28T08:00:00Z`), and `u_2b8e1d04` (`ann@example.com`) owns `month_end` and `nightly_backup` (`crn_8d2e6b4a1f7c3095`, `30 2 * * *`, active, next `2026-10-06T02:30:00Z`); it is now `2026-10-05T09:32:00Z`, and telemetry and the events app take every event. Its `tool.called` is as `S05` fixes it. A delete that removes a trigger emits `cron.<slug>.deleted` to the event bus, with the attributes `trigger`, the trigger's id, and `when`, the schedule it had, under the caller's user id and the request's id, and records the same event, by the same name and with the same attributes, in the trail, between the request's `request.started` and its `tool.called`. The event the bus receives is shown as the JSON object it receives, where `<event-id>` is the id cron gives it, `evt_` and 16 lowercase hexadecimal digits, `<time>` is when cron emitted it, UTC to the microsecond, and `cause` and `depth` are as `S06` tells; no request here carries `X-Event-Cause` or `X-Event-Depth`, so `cause` is empty and `depth` is 0. A refusal removes nothing, emits nothing, and records no `cron.*` event. The owner's email is in no event. cron writes nothing to stderr for any answer in this group.

## A model deletes a trigger

The ordinary case: `hourly` is active and due to fire at `10:00`. Once it is deleted it never fires again: the last event it emits is `cron.hourly.deleted`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete","arguments":{"slug":"hourly"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"deleted":true,"id":"crn_3f9a1c7e5b2d8046"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's.

Postconditions:

- cron no longer holds `crn_3f9a1c7e5b2d8046`. `list` (`S07`) answers `month_end`, `nightly_backup`, and `weekly_digest`; `show`, `update`, `pause`, `resume`, and `delete` with `hourly` are refused with `no trigger named 'hourly'`.
- No `cron.hourly.fired` is emitted at `2026-10-05T10:00:00Z` or after (`S11`).
- The slug `hourly` is free: any user may create a trigger named `hourly` (`S06`), which is a new trigger with its own id and no `last_fired`.
- The events app has received one event from cron, where `<request-id>` is the request's id (`S02`):

  ```
  {"id":"<event-id>","time":"<time>","service":"cron","event":"cron.hourly.deleted","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"trigger":"crn_3f9a1c7e5b2d8046","when":"@hourly"},"cause":"","depth":0}
  ```

- telemetry has received the request's four events, in this order:

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"cron","event":"cron.hourly.deleted","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"trigger":"crn_3f9a1c7e5b2d8046","when":"@hourly"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"ok","tool":"delete"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A model deletes a paused trigger

A model need not resume a trigger before deleting it. `weekly_digest` is paused, and it goes as an active trigger does; its `cron.weekly_digest.deleted` carries the schedule it had.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete","arguments":{"slug":"weekly_digest"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"deleted":true,"id":"crn_5c7b9e2f4a6d1038"}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's: `weekly_digest` is paused.

Postconditions:

- cron no longer holds `crn_5c7b9e2f4a6d1038`. `list` (`S07`) answers `hourly`, `month_end`, and `nightly_backup`; `resume` with `weekly_digest` is refused with `no trigger named 'weekly_digest'`, as are `show`, `update`, `pause`, and `delete`.
- The slug `weekly_digest` is free.
- The events app has received one event from cron, where `<request-id>` is the request's id (`S02`):

  ```
  {"id":"<event-id>","time":"<time>","service":"cron","event":"cron.weekly_digest.deleted","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"trigger":"crn_5c7b9e2f4a6d1038","when":"0 8 * * 1"},"cause":"","depth":0}
  ```

- telemetry has received `cron.weekly_digest.deleted` with attributes `{"trigger":"crn_5c7b9e2f4a6d1038","when":"0 8 * * 1"}` before the request's `tool.called`, whose `kind` is `destructive` and `outcome` `ok`.

## A model deletes another user's trigger

Only a trigger's owner deletes it. Another user's trigger gets exactly the answer a trigger that does not exist gets, and goes on firing.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"delete","arguments":{"slug":"nightly_backup"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no trigger named 'nightly_backup'
```

Preconditions:

- The preamble's: `nightly_backup` is `u_2b8e1d04`'s and active.

Postconditions:

- Nothing was removed: `nightly_backup` is active and fires next at `2026-10-06T02:30:00Z`.
- Nothing was emitted to the event bus. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`):

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"destructive","outcome":"error","tool":"delete"}}
  ```

## A model deletes a trigger that does not exist

A slug no trigger has is refused, quoting it as sent.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"delete","arguments":{"slug":"daily_report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no trigger named 'daily_report'
```

Preconditions:

- The preamble's: no trigger is named `daily_report`.

Postconditions:

- Nothing was removed.
- Nothing was emitted to the event bus. The request's `tool.called` has `kind` `destructive` and `outcome` `error`.

## A model deletes a trigger that is already gone

A second delete of the same trigger finds nothing, and is refused as for any trigger that does not exist: delete is not quietly repeated, so a model learns the first call did what it asked rather than that something else did.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
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

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
no trigger named 'hourly'
```

Preconditions:

- The preamble's, after `A model deletes a trigger`.

Postconditions:

- Nothing has changed. cron emitted no second `cron.hourly.deleted` and recorded none; the request's `tool.called` has `outcome` `error`.

## A model calls delete without saying which trigger

`slug` is required and is the only argument. A call that leaves it out, or sends a field the tool does not have, is refused as its arguments are read, every offence in one answer (`S05`). Here the model named the trigger by its id under a key the tool does not have.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"delete","arguments":{"id":"crn_3f9a1c7e5b2d8046"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
slug: missing required field
id: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- Nothing was removed: `hourly` is active and fires next at `2026-10-05T10:00:00Z`.
- Nothing was emitted to the event bus. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event, where `<request-id>` is the request's id (`S02`); the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"destructive","outcome":"invalid_arguments","tool":"delete"}}
  ```
