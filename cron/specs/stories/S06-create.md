# Stories — create

`create`, the tool that adds a trigger: a slug and a schedule, which emits an event on the suite's event bus each time its schedule comes due (`S11`). It takes, in this order, `slug` and `when`, both required strings. A slug is 1 to 64 characters: a lowercase ASCII letter, then lowercase letters and digits, with single underscores between them, so no `-`, no upper-case letter, no leading digit or underscore, no trailing underscore, and never two underscores together; it is taken as sent, never trimmed or folded. No slug is reserved: no trigger has a page of its own. Slugs are unique across the space, not per owner, because a slug is the middle word of the names of the events a trigger emits, and two triggers with one slug would emit events no subscriber could tell apart; so a slug is taken when any trigger, the caller's or another user's, has it. `when` is accepted when it is exactly one of the five descriptors `@hourly`, `@daily`, `@weekly`, `@monthly`, and `@yearly`, or when it is exactly five fields, minute, hour, day of month, month, and day of week, separated by single spaces with nothing before the first or after the last, that a standard five-field cron parser accepts, such as `*/15 * * * *`; names such as `MON` and `JAN`, ranges, lists, and steps are all allowed within a field. Anything else is refused: there is no seconds field, no `@every`, `@reboot`, `@annually`, or `@midnight`, a descriptor is written in lower case only, and a schedule with a leading, trailing, or doubled space is refused, not trimmed. `when` is kept exactly as sent, so `0 9 * * MON` and `0 9 * * 1` are different schedules to cron even though they name the same slots. Every schedule is read in UTC; a trigger has no time zone of its own. The arguments are checked in this order — the slug against the rule, then whether it is taken; then `when` — and the first that fails is the whole answer, one line naming it. The result is the trigger object (`S05`), members in this order: `id`, `crn_` and 16 lowercase hexadecimal digits, minted now (ids are drawn at random and are not derived from the slug; a trigger created with the slug later is a new trigger with its own id, `S10`); `slug`; `when`, exactly as sent; `owner`, the caller's `X-User-Email`; `status`, `active`; `created`, the time of the call, RFC 3339 UTC to the second; and `next`, the first slot of the schedule strictly after the call, so a trigger created exactly on one of its slots first fires at the slot after it. A new trigger has never fired, so `last_fired` is absent. The owner is the caller, always: only they may later update, pause, resume, or delete it (`S08`, `S09`, `S10`), though every user can list and show it (`S07`).

The actor is a model working through an MCP client, or the gateway's `mutate` on its behalf; the request shape, the result envelope, the `invalid arguments:` wording, `tool.called`, the lifecycle events, and the trail are as `S05` fixes them. Each request is the HTTP request a running cron (`S02`) receives on `/mcp`, carrying `X-User-Id` and `X-User-Email` by hand, on revision `2026-07-28`; unless a story says otherwise it carries no `X-Request-Id`, so cron gives the request an id of its own (`S02`), shown as `<request-id>`, and no `X-Event-Cause` or `X-Event-Depth`. telemetry takes every event, and the events app takes every event cron emits.

The shared fixture, which `S05` and every later group assume unless a story says otherwise: there are two users, `u_7f3a9c21`, `mg@example.com`, the usual caller, and `u_2b8e1d04`, `ann@example.com`. It is now `2026-10-05T09:32:00Z`, a Monday. The space holds four triggers, each created by its owner's `create` with the email shown:

| slug | id | when | owner | status | created | last_fired | next |
|---|---|---|---|---|---|---|---|
| `hourly` | `crn_3f9a1c7e5b2d8046` | `@hourly` | `mg@example.com` | `active` | `2026-09-20T08:00:00Z` | `2026-10-05T09:00:00Z` | `2026-10-05T10:00:00Z` |
| `month_end` | `crn_1a4f8c6e9b3d7025` | `@monthly` | `ann@example.com` | `active` | `2026-10-04T10:00:00Z` | (never fired) | `2026-11-01T00:00:00Z` |
| `nightly_backup` | `crn_8d2e6b4a1f7c3095` | `30 2 * * *` | `ann@example.com` | `active` | `2026-09-28T17:15:00Z` | `2026-10-05T02:30:00Z` | `2026-10-06T02:30:00Z` |
| `weekly_digest` | `crn_5c7b9e2f4a6d1038` | `0 8 * * 1` | `mg@example.com` | `paused` | `2026-09-01T12:00:00Z` | `2026-09-28T08:00:00Z` | (none: paused) |

So `u_7f3a9c21` owns `hourly` and `weekly_digest`, and `u_2b8e1d04` owns `month_end` and `nightly_backup`. No trigger has the slug `crm_sync`; a story that creates a trigger makes `crm_sync`, with `when` `*/15 * * * *`, unless it says otherwise. `create` is of kind `additive`; a call that makes a trigger records `cron.<slug>.created`, with `trigger`, the new id, and `when`, as sent, before its `tool.called`, and emits that event to the event bus (`S05`). A refusal creates nothing, records no `cron.*` event, and emits nothing. cron writes nothing to stderr for any answer in this group.

## A model creates a trigger

The ordinary case: a slug and a five-field schedule, every fifteen minutes. The trigger is active at once, and the answer says when it first fires: the first quarter hour after the call. Nothing has fired yet, so the answer has no `last_fired`. Creating the trigger is itself an event on the bus, `cron.crm_sync.created`, so a service that cares when triggers come and go can follow them; each fire is a separate event, `cron.crm_sync.fired` (`S11`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
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

and a `content` array of one text block whose text is that object encoded compactly. `<id>` is the id cron minted, `crn_` and 16 lowercase hexadecimal digits, different from every other trigger's; `created` is the time of the call.

Preconditions:

- The preamble's: no trigger has the slug `crm_sync`, and the call is made at `2026-10-05T09:32:00Z`.

Postconditions:

- cron holds a fifth trigger: id `<id>`, slug `crm_sync`, `when` `*/15 * * * *`, owner `u_7f3a9c21` (`mg@example.com`), `active`, created `2026-10-05T09:32:00Z`, never fired, next `2026-10-05T09:45:00Z`. `list` (`S07`) answers `crm_sync`, `hourly`, `month_end`, `nightly_backup`, `weekly_digest`, in that order; `show` with `crm_sync` answers what `create` answered; the landing page shows it with the `yours` badge to `mg@example.com` and without it to `ann@example.com` (`S03`).
- Its first fire is the slot `2026-10-05T09:45:00Z`, emitting `cron.crm_sync.fired` (`S11`). The other four triggers are as they were.
- telemetry has received the request's four events, in this order:

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"cron","event":"cron.crm_sync.created","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"trigger":"<id>","when":"*/15 * * * *"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"ok","tool":"create"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  The email `mg@example.com` is in none of them.

- The events app has received one event from cron, the bus's nine-member event:

  ```
  {"id":"<event-id>","time":"<time>","service":"cron","event":"cron.crm_sync.created","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"trigger":"<id>","when":"*/15 * * * *"},"cause":"","depth":0}
  ```

  `<event-id>` is `evt_` and 16 lowercase hexadecimal digits, which cron gave it; `<time>` is when cron emitted it, UTC to the microsecond; `<request-id>` and `<id>` are those above. The request carried no `X-Event-Cause` or `X-Event-Depth`, so `cause` is empty and `depth` is `0`.

## A model creates a trigger with a descriptor

A descriptor names a common schedule in one word: `@hourly` fires at the top of every hour, `@daily` at midnight UTC, `@weekly` at midnight UTC between Saturday and Sunday, `@monthly` at midnight UTC on the first of the month, and `@yearly` at midnight UTC on the first of January. The descriptor is kept and answered back as sent, never rewritten as the five fields it stands for. These five, in lower case, are the only descriptors: `@annually` and `@midnight`, which some cron dialects accept, and `@DAILY` are refused as any `when` that is not allowed is (below). Five fields may name days and months, so `0 9 * * MON-FRI` is accepted as well and kept exactly so.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create","arguments":{"slug":"daily_report","when":"@daily"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","slug":"daily_report","when":"@daily","owner":"mg@example.com","status":"active","created":"2026-10-05T09:32:00Z","next":"2026-10-06T00:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: no trigger has the slug `daily_report`.

Postconditions:

- cron holds `daily_report`, owner `u_7f3a9c21`, `when` `@daily`, `active`, never fired, next `2026-10-06T00:00:00Z`.
- telemetry has received `cron.daily_report.created` with attributes `{"trigger":"<id>","when":"@daily"}` before the request's `tool.called`, whose `kind` is `additive` and `outcome` `ok`; the events app has received the same event, with `cause` empty and `depth` `0`.

## A model creates a trigger with an event as the cause

A call a script makes in reaction to an event names that event, so the trigger's `created` event can be traced back to it through `cause`, and the bus's limit on `depth` can stop a loop of reactions. Here a script run that an event started calls cron through the gateway, and its request carries the event's id `evt_8c3f1a6e2d9b4075` as the cause, at depth `1`. When a request carries both `X-Event-Cause`, an event id, `evt_` followed by 16 lowercase hexadecimal digits, and `X-Event-Depth`, a non-negative whole decimal number, the event has that id as its `cause` and that number plus one as its `depth` (`S05`). Neither header changes the answer or the trail's record.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Event-Cause: evt_8c3f1a6e2d9b4075
X-Event-Depth: 1
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"create","arguments":{"slug":"follow_up","when":"0 9 * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","slug":"follow_up","when":"0 9 * * *","owner":"mg@example.com","status":"active","created":"2026-10-05T09:32:00Z","next":"2026-10-06T09:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: no trigger has the slug `follow_up`, and the call is made at `2026-10-05T09:32:00Z`.

Postconditions:

- cron holds `follow_up`, owner `u_7f3a9c21`, `when` `0 9 * * *`, `active`, never fired, next `2026-10-06T09:00:00Z`.
- telemetry has received the request's four events, exactly as in `A model creates a trigger`, with `cron.follow_up.created` and its attributes `{"trigger":"<id>","when":"0 9 * * *"}` in place of `crm_sync`'s, and nothing of either header.
- The events app has received one event from cron:

  ```
  {"id":"<event-id>","time":"<time>","service":"cron","event":"cron.follow_up.created","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"trigger":"<id>","when":"0 9 * * *"},"cause":"evt_8c3f1a6e2d9b4075","depth":2}
  ```

## A model creates a trigger with a malformed cause header

The two headers count only together and only well-formed: when either is absent or malformed, cron ignores the pair as a whole, `cause` is empty, `depth` is `0`, and the call is otherwise unaffected (`S05`). Each form below sends the headers in one such way: a cause without a depth, a depth without a cause, a cause whose hexadecimal digits are not lowercase, a negative depth, and a depth that is not a number. They behave identically.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Event-Cause: evt_8c3f1a6e2d9b4075
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"create","arguments":{"slug":"follow_up","when":"0 9 * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Event-Depth: 1
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"create","arguments":{"slug":"follow_up","when":"0 9 * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Event-Cause: evt_8C3F1A6E2D9B4075
X-Event-Depth: 1
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"create","arguments":{"slug":"follow_up","when":"0 9 * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Event-Cause: evt_8c3f1a6e2d9b4075
X-Event-Depth: -1
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"create","arguments":{"slug":"follow_up","when":"0 9 * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Event-Cause: evt_8c3f1a6e2d9b4075
X-Event-Depth: one
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"create","arguments":{"slug":"follow_up","when":"0 9 * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has no `isError` member, a `structuredContent` of

```
{"id":"<id>","slug":"follow_up","when":"0 9 * * *","owner":"mg@example.com","status":"active","created":"2026-10-05T09:32:00Z","next":"2026-10-06T09:00:00Z"}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's: no trigger has the slug `follow_up`, and the call is made at `2026-10-05T09:32:00Z`.

Postconditions:

- cron holds `follow_up`, as in `A model creates a trigger with an event as the cause`.
- telemetry has received the request's four events, exactly as in `A model creates a trigger with an event as the cause`.
- The events app has received one event from cron:

  ```
  {"id":"<event-id>","time":"<time>","service":"cron","event":"cron.follow_up.created","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"trigger":"<id>","when":"0 9 * * *"},"cause":"","depth":0}
  ```

## A model creates a trigger with a slug another user's trigger has

Slugs are unique across the space, not per owner: `month_end` is `ann@example.com`'s trigger, and its events are named `cron.month_end.*`, so the caller cannot have a `month_end` of its own. The answer says only that the slug is taken; whose it is, the caller can see with `show` (`S07`). The slug is checked before `when`, so a taken slug is the answer even when `when` is not allowed either.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create","arguments":{"slug":"month_end","when":"0 0 1 * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
a trigger named 'month_end' already exists
```

Preconditions:

- The preamble's: `month_end` is `u_2b8e1d04`'s.

Postconditions:

- No trigger was created. `month_end` is as it was, still `u_2b8e1d04`'s, `@monthly`, next `2026-11-01T00:00:00Z`.
- cron recorded no `cron.*` event and emitted nothing to the event bus. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event:

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"create"}}
  ```

## A model creates a trigger with a slug its own trigger has

A taken slug is refused, never read as a request to change the trigger that has it, and the existing trigger is untouched, whatever `when` the call names. A model that means to change a trigger's schedule calls `update` (`S08`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create","arguments":{"slug":"hourly","when":"30 * * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
a trigger named 'hourly' already exists
```

Preconditions:

- The preamble's: the caller owns `hourly`.

Postconditions:

- No trigger was created. `hourly` is unchanged: same id, `crn_3f9a1c7e5b2d8046`, `when` `@hourly`, last fired `2026-10-05T09:00:00Z`, next `2026-10-05T10:00:00Z`.
- cron recorded no `cron.*` event and emitted nothing to the event bus; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model creates a trigger with a slug that is not allowed

A slug is held to one rule, and every way of breaking it gets the same line, quoting the slug as sent: a `-`, as here; an upper-case letter, as in `CRM_Sync`; a first character that is not a letter, as in `9am_report` or `_crm`; two underscores together, as in `crm__sync`; a last character `_`, as in `crm_`; any other character outside `a`-`z`, `0`-`9`, and `_`, a `.` or a space say; the empty slug; and more than 64 characters, the letter `a` written 65 times say. Nothing is trimmed or folded, so `CRM_Sync` and ` crm_sync` are refused rather than read as `crm_sync`. The slug is checked first, so a slug that is not allowed is the answer even when `when` is not allowed either.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"create","arguments":{"slug":"crm-sync","when":"*/15 * * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid slug 'crm-sync'
```

The same call with `CRM_Sync` is answered `invalid slug 'CRM_Sync'`, with `9am_report` `invalid slug '9am_report'`, with `crm__sync` `invalid slug 'crm__sync'`, and with 65 `a`s `invalid slug '<the 65 a's>'`, quoting all 65.

Preconditions:

- The preamble's.

Postconditions:

- No trigger was created.
- cron recorded no `cron.*` event and emitted nothing to the event bus. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event:

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"additive","outcome":"error","tool":"create"}}
  ```

## A model creates a trigger with a schedule that is not allowed

`when` is five cron fields separated by single spaces or one of the five lower-case descriptors, and nothing else. A sixth field for seconds, as here, is refused, as is `@every 5m`, `@reboot`, `@annually`, `@midnight`, `@DAILY`, a word that is no schedule at all such as `bogus`, the empty string, and five good fields padded with a space before or after them or with two spaces between two of them; each gets the same line, quoting `when` exactly as sent, padding included. Nothing is trimmed or folded. `when` is checked only once the slug has passed and is free.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"create","arguments":{"slug":"crm_sync","when":"0 */15 * * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid when '0 */15 * * * *'
```

The same call with `@every 5m` is answered `invalid when '@every 5m'`, with `@reboot` `invalid when '@reboot'`, with `@DAILY` `invalid when '@DAILY'`, with `bogus` `invalid when 'bogus'`, with ` */15 * * * *` (a leading space) `invalid when ' */15 * * * *'`, with `*/15 * * * * ` (a trailing space) `invalid when '*/15 * * * * '`, and with `*/15  * * * *` (two spaces after the first field) `invalid when '*/15  * * * *'`.

Preconditions:

- The preamble's: no trigger has the slug `crm_sync`.

Postconditions:

- No trigger was created; `crm_sync` is still free.
- cron recorded no `cron.*` event and emitted nothing to the event bus; the request's `tool.called` has `kind` `additive` and `outcome` `error`.

## A model calls create without a schedule

`slug` and `when` are both required. A call that leaves one out is refused as its arguments are read, every offence in one answer, before any rule of cron's is looked at (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"create","arguments":{"slug":"crm_sync"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
when: missing required field
```

Preconditions:

- The preamble's.

Postconditions:

- No trigger was created.
- cron recorded no `cron.*` event and emitted nothing to the event bus. Between the request's `request.started` and its `request.finished`, whose `status` is 200, telemetry has received one event; the tool never ran, so its `duration_us` is 0:

  ```
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":0,"kind":"additive","outcome":"invalid_arguments","tool":"create"}}
  ```

## A model calls create without a slug

A model that names the trigger in a field create does not have, `name` here, has left `slug` out: both offences are in one answer, the missing field first, since the tool's own fields come before unknown ones (`S05`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"create","arguments":{"name":"crm_sync","when":"*/15 * * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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
name: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- No trigger was created.
- cron recorded no `cron.*` event and emitted nothing to the event bus; the request's `tool.called` has `kind` `additive`, `outcome` `invalid_arguments`, and `duration_us` 0.

## A model sends arguments create does not take

A field of the wrong JSON type, or one the tool does not have, is refused as the arguments are read, every offence in one answer: the tool's fields first, in the order of its input schema, then each unknown field in the order it was sent (`S05`). Here the model sent `when` as a number, meaning every 15 minutes, and tried to name the trigger's owner, which is always the caller.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"create","arguments":{"slug":"crm_sync","when":15,"owner":"ann@example.com"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
when: expected string, got number
owner: unknown field
```

Preconditions:

- The preamble's.

Postconditions:

- No trigger was created.
- cron recorded no `cron.*` event and emitted nothing to the event bus; the request's `tool.called` has `kind` `additive`, `outcome` `invalid_arguments`, and `duration_us` 0.
