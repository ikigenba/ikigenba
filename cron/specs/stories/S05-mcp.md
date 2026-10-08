# Stories — mcp endpoint

The triggers offered to models: cron's MCP interface at `/mcp`, the one route that answers MCP clients rather than browsers. It is the exact path `/mcp`; a path beneath it, `/mcp/tools` say, is a path cron does not serve and is answered not found, as any such path is (`S03`). `/mcp` takes only POST, each POST carries one JSON-RPC request, and each request is answered with one `application/json` body: there are no sessions and no streams, and cron keeps nothing from one request to the next. It offers seven tools and nothing else, in this order: `list`, which lists every trigger in the space (`S07`); `show`, which shows one trigger with its schedule, its owner, and when it last fired and fires next (`S07`); `create`, which creates a trigger that emits an event on a schedule (`S06`); `update`, which changes the schedule of a trigger the caller owns (`S08`); `pause`, which stops a trigger the caller owns from firing (`S09`); `resume`, which starts a paused trigger the caller owns firing again, from its next slot (`S09`); and `delete`, which deletes a trigger the caller owns (`S10`). A trigger fires on its own, never through a tool: each slot its schedule calls for, cron emits `cron.<slug>.fired` on the suite's event bus (`S11`), and what reacts to that event is whatever service the bus delivers it to, never cron's concern. Each tool's arguments, its result, and its failures are told in its own group; what they share is fixed here.

A trigger has an id, `crn_` and 16 lowercase hexadecimal digits, which cron gives it when it is created and which is never reused; a slug, its identity and the middle word of every event name it emits, 1 to 64 characters, a lowercase ASCII letter, then lowercase letters and digits with single underscores between them, unique across the space (`S06`); a schedule, `when`, exactly one of `@hourly`, `@daily`, `@weekly`, `@monthly`, and `@yearly`, or exactly five fields separated by single spaces, with nothing before the first or after the last, that a standard five-field cron parser accepts, and nothing else (`S06`), kept exactly as it was sent and answered back as sent, so an `update` to a different string is a change even when it names the same slots (`S08`); an owner, the user who created it; and a status, `active` or `paused` (`S09`). A trigger's `paused` is its own: it has nothing to do with a subscriber the events app has paused. Every signed-in user sees every trigger: `list` lists them all and `show` shows any of them, whoever owns it. A tool that changes one trigger, `update`, `pause`, `resume`, or `delete`, takes it as `slug` and acts only on a trigger the caller owns: to every caller, another user's trigger is, for those four tools, indistinguishable from one that does not exist (`no trigger named '<slug>'`), though `list` and `show` show it and its slug is taken (`S06`). Every time in a tool's result is RFC 3339 UTC to the second, `2026-10-05T09:32:00Z` say, and every schedule is read in UTC. A member that does not apply is left out, never sent as `null`.

A trigger object, as `show`, `create`, `update`, `pause`, and `resume` return it, has these members in this order: `id`; `slug`; `when`; `owner`, the owner's email, the `X-User-Email` sent with the `create` that made it; `status`; `created`; `last_fired`, the `scheduled` time of its latest fire (`S11`), absent until it has fired; and `next`, the next slot it will fire, absent while it is paused; a trigger created exactly on a slot of its schedule is due first at the next slot strictly after its creation, never at that slot. `create`, `update`, `pause`, and `resume` answer the trigger as it stands after the call; `delete` answers `{"deleted":true,"id":"<id>"}`, the id of the trigger it deleted, and nothing else (`S10`). An entry of `list`'s `triggers` has the same members in the same order but `created`, under the same rules.

On a host, a model does not reach `/mcp` directly: it reaches cron's tools through the mcp gateway, naming the service `cron` and the tool, and the gateway calls cron at its socket on the model's behalf (below, and `S15`). cron's manifest marks it an MCP service (`S01`), so the host's services file carries `"mcp": true` for it and the gateway's `describe` lists its seven tools with their kinds: `list` and `show` are of kind `read` and run with the gateway's `call`; `create`, `update`, and `resume` are `additive`, and `pause` and `delete` are `destructive`, and those five run with `mutate`. The actor in this group is a model working through an MCP client, the client itself, or the gateway standing in for either. Each request is shown as the HTTP request cron receives on a running cron (`S02`), started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment, the host's services file, which holds the suite's services file (`S03`) unless a story says otherwise, whose `telemetry` entry names the telemetry service, which takes every event, and whose `events` entry names the events app, which takes every event cron emits (`S02`). `/mcp` is behind the same identity rule as every route of cron, which serves nothing to guests: nginx sets `X-User-Id` and `X-User-Email` and, on a host with an authenticator, challenges a request with no credential at `/mcp`, never passing it to cron (opsctl's `S5-nginx.md`); the gateway forwards both headers (`S02`); and a request without `X-User-Id` is answered 500 before anything else is looked at. The requests below carry both headers by hand, and only the missing-header story carries neither. `/mcp` answers the same whatever `Host` names: the requests below carry `Host: cron.sbx.ikigenba.dev`, as nginx passes it, but for the gateway's, which sends `Host: backend` to every service.

A client speaking the protocol revision `2026-07-28` sends, on every request, the headers `Content-Type: application/json`, `MCP-Protocol-Version: 2026-07-28`, and `Mcp-Method` equal to the request's `method`, and for a `tools/call` also `Mcp-Name` equal to the tool's name; and its `params` carry `_meta` with `io.modelcontextprotocol/protocolVersion` `2026-07-28` and `io.modelcontextprotocol/clientCapabilities` `{}`. Every successful answer on that revision is status 200, and its `result`, besides what each story fixes, carries `resultType` `"complete"` and `_meta` whose `io.modelcontextprotocol/serverInfo` is `{"name":"cron","version":"<display>"}`, where `<display>` is the string `cron --version` prints under the environment cron was started with (`S01`), the empty string when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`; a `tools/list` or `server/discover` result also carries `ttlMs` `0` and `cacheScope` `"private"`. Those members are not repeated in this group or in `S06` to `S10`. A client speaking an earlier revision, `2025-11-25` or `2025-06-18`, opens with `initialize` and is served the same tools, and its calls get the same tool results, `isError` results included, without those members; a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200 on the earlier revisions. How the transport answers a request that is not well-formed MCP — a wrong `Content-Type`, a body over 1 MiB, a foreign `Origin`, mismatched headers, an unknown method — is the platform's, the same for every app, and this group does not restate it.

A tool's successful result carries the answer twice: as `structuredContent`, a JSON object, and as one text content block whose text is that same object encoded compactly, with no white space between its tokens. A tool that cannot do what it was asked answers status 200 with a result whose `isError` is `true`, with no `structuredContent` and a `content` array of exactly one text block saying why. The text takes one of three forms:

- Arguments refused as they are read against the tool's input schema — a field missing, of the wrong JSON type, or one the tool does not have — are reported with the platform's wording: the line `invalid arguments:` followed by one line per offence, each `<field>: <reason>` (`when: missing required field`, `when: expected string, got number`, `slug: unknown field`), separated by LF with no LF after the last; the tool's fields come first, in the order of its input schema, then each unknown field in the order it was sent.
- Arguments that pass that reading but that cron refuses, or a call it cannot carry out as things stand, are refused with one line in cron's own words, each told in the group of the tool that meets it: `invalid slug '<slug>'`, `a trigger named '<slug>' already exists`, `invalid when '<when>'`, and `no trigger named '<slug>'`. `<slug>` and `<when>` are quoted exactly as they were sent. No refusal is more than one line.
- Every tool, when cron cannot read or write its database, refuses with exactly `cannot reach the database; try again later`, quoting nothing of the underlying error.

Nothing is created, changed, paused, resumed, or deleted by a call that is refused, and a refused call records no `cron.*` event and emits nothing to the event bus.

Every request to `/mcp` is recorded in cron's trail as every request is, by its `request.started` and `request.finished` (`S02`, `S12`). A `tools/call` that reaches one of the seven tools and is answered with a `result`, an `isError` result included, also records `tool.called`, after anything the tool recorded and before the request's `request.finished`, under the caller's request id and user. Its attributes are `tool`, the tool's name; `kind`, `read` for `list` and `show`, `additive` for `create`, `update`, and `resume`, and `destructive` for `pause` and `delete`; `outcome`, `ok` for a result with no `isError`, `invalid_arguments` when the arguments were refused as they were read against the input schema, and `error` for every other refusal; and `duration_us`, how long the tool took, in whole microseconds, 0 when the arguments were refused as they were read and the tool never ran. The arguments themselves and the text of a refusal are never recorded in `tool.called`. A call answered with a protocol error, such as an unknown tool, reached no tool and records no `tool.called`; nor does any other method.

A call that creates, pauses, resumes, or deletes a trigger also records one lifecycle event before its `tool.called`, under the caller's request id and user: `cron.<slug>.created`, `cron.<slug>.paused`, `cron.<slug>.resumed`, or `cron.<slug>.deleted`, where `<slug>` is the trigger's slug, with exactly two attributes, `trigger`, the trigger's id, and `when`, its schedule as it stands (for `deleted`, as it stood). `update` records no event of its own, and neither does a `pause` of a trigger already paused or a `resume` of one already active, which change nothing (`S08`, `S09`); `list` and `show` record nothing of their own. The owner's email is in no event. cron also emits each lifecycle event to the suite's event bus, the events app, as the nine-member event the bus receives: `id`, the event's own id, `evt_` and 16 lowercase hexadecimal digits, which cron gives it; `time`, when cron emitted it, UTC to the microsecond; `service` `cron`; `event`, the same name; `request_id` and `user`, those of the call; `attrs`, the same two attributes; and `cause` and `depth`. The call sets `cause` and `depth` with two request headers: when it carries both `X-Event-Cause`, an event id, `evt_` followed by 16 lowercase hexadecimal digits, and `X-Event-Depth`, a non-negative whole decimal number, the event has that id as its `cause` and that number plus one as its `depth`; when either is absent or malformed, cron ignores the pair as a whole, `cause` is empty, `depth` is `0`, and the call is otherwise unaffected. Neither header changes the trail's record. The bus event is in addition to the trail's record, never instead of it. No answer waits for the bus: an event the events app cannot take is kept and sent again, and one it has not taken within the retry window is dropped and recorded as `event.lost` (`S12`).

A response body below is laid out for reading: its white space is not fixed. The order of members within a tool and within its schemas, and within a tool's result where a story shows one, is fixed as shown; elsewhere the order of members is not. A response block shows the status line and the headers the story fixes; a header it does not show is not fixed. No answer in this group or in `S06` to `S10` earns a line on stderr, the missing-header 500 included; only an event cron cannot deliver reaches stderr (`S02`).

## An MCP client lists cron's tools

A client lists the tools before offering them to a model. What it gets is everything the model is told about each tool: its name, a description written for the model, the shape of its arguments and its answer, and how careful the client must be before calling it. The description's first line is a one-line summary a catalogue can show on its own, the same line the landing page shows beside the tool's name (`S03`). `list` and `show` change nothing, so they are marked read-only; `create`, `update`, and `resume` add a trigger, change its schedule, or set it firing again, and what each sets can be changed back by calling another tool, so they are marked neither read-only nor destructive; `pause` stops a trigger firing, and the slots that pass while it is paused are never fired, and `delete` removes a trigger for good, so both are marked destructive. None is open-world: each acts on the platform's own data.

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/list

{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `nextCursor` and whose `tools` is an array of exactly seven tools, in this order:

```
[
  {
    "name": "list",
    "description": "Every trigger in the space, by slug.\n\nTakes no arguments. Every user's triggers are listed, not only yours. Each trigger has its id, slug, when (its schedule, as it was given), owner (the email of the user who created it), status (active or paused), last_fired (the slot it last fired for; absent when it has never fired), and next (the next slot it fires; absent while paused). Times are UTC. Use show for one trigger's created time.",
    "inputSchema": {"type": "object", "additionalProperties": false},
    "outputSchema": <the list output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "show",
    "description": "One trigger, with its schedule, its owner, and when it last fired and fires next.\n\nPass slug, the trigger's slug; any user's trigger can be shown. The result has its id, slug, when (its schedule, as it was given), owner (the email of the user who created it), status (active or paused), created, last_fired (the slot it last fired for; absent when it has never fired), and next (the next slot it fires; absent while paused). Times are UTC.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "slug": {"type": "string", "description": "The trigger's slug."}
      },
      "required": ["slug"],
      "additionalProperties": false
    },
    "outputSchema": <the trigger output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "create",
    "description": "Create a trigger that emits an event on a schedule.\n\nslug is 1 to 64 characters: a lowercase letter, then lowercase letters and digits, with single underscores between them; it must not already be a trigger's slug in the space, whoever owns that trigger. when is the schedule, read in UTC: five cron fields (minute hour day-of-month month day-of-week), such as */15 * * * * or 0 9 * * MON-FRI, separated by single spaces with nothing before or after, or exactly one of @hourly, @daily, @weekly, @monthly, and @yearly; there is no seconds field and no other descriptor. Each time the schedule comes due, the trigger emits cron.<slug>.fired on the suite's event bus, with attrs trigger (its id), when, and scheduled (the slot it fired for); a slot missed is never made up. Creating it emits cron.<slug>.created. The trigger is yours: only you can update, pause, resume, or delete it. The result is what show returns.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "slug": {"type": "string", "description": "The new trigger's slug: a lowercase letter, then lowercase letters and digits with single underscores between them, 1 to 64 characters, not already a trigger's slug in the space."},
        "when": {"type": "string", "description": "The schedule, read in UTC: five cron fields, or @hourly, @daily, @weekly, @monthly, or @yearly."}
      },
      "required": ["slug", "when"],
      "additionalProperties": false
    },
    "outputSchema": <the trigger output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "update",
    "description": "Change the schedule of a trigger you own.\n\nPass slug and when, the new schedule, under the rules of create. Only the schedule changes: the trigger keeps its id, slug, status, and last_fired, and its next slot follows the new schedule. No event is emitted. Another user's trigger is refused as one that does not exist. The result is what show returns.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "slug": {"type": "string", "description": "The trigger's slug."},
        "when": {"type": "string", "description": "The new schedule, under the rules of create."}
      },
      "required": ["slug", "when"],
      "additionalProperties": false
    },
    "outputSchema": <the trigger output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "pause",
    "description": "Stop a trigger you own from firing.\n\nPass slug. A paused trigger keeps its slug, its schedule, and last_fired, has no next, and fires nothing until you resume it; the slots that pass while it is paused are never fired. Pausing emits cron.<slug>.paused; pausing a trigger already paused changes nothing. Another user's trigger is refused as one that does not exist. The result is what show returns.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "slug": {"type": "string", "description": "The trigger's slug."}
      },
      "required": ["slug"],
      "additionalProperties": false
    },
    "outputSchema": <the trigger output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}
  },
  {
    "name": "resume",
    "description": "Start a paused trigger you own firing again, from its next slot.\n\nPass slug. The trigger fires again from the first slot of its schedule after the call; the slots it missed while paused are not made up. Resuming emits cron.<slug>.resumed; resuming a trigger already active changes nothing. Another user's trigger is refused as one that does not exist. The result is what show returns.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "slug": {"type": "string", "description": "The trigger's slug."}
      },
      "required": ["slug"],
      "additionalProperties": false
    },
    "outputSchema": <the trigger output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "delete",
    "description": "Delete a trigger you own.\n\nPass slug. The trigger never fires again, and deleting it emits cron.<slug>.deleted. Its slug is free for anyone to take, and a trigger created with it later gets a new id. Another user's trigger is refused as one that does not exist. The result is deleted, true, and the id of the deleted trigger.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "slug": {"type": "string", "description": "The trigger's slug."}
      },
      "required": ["slug"],
      "additionalProperties": false
    },
    "outputSchema": <the delete output schema>,
    "annotations": {"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}
  }
]
```

The output schemas are not quoted whole; each describes an object closed to other members, with its properties in the order given here, and each object it holds is closed to other members too. The trigger output schema, which `show`, `create`, `update`, `pause`, and `resume` all carry, describes one trigger: `id`, a string; `slug`, a string; `when`, a string; `owner`, a string; `status`, a string; `created`, a string; `last_fired`, a string, not always present; and `next`, a string, not always present. `list`'s has one property, `triggers`, an array of objects with `id`, `slug`, `when`, `owner`, `status`, `last_fired`, and `next`, as in the trigger output schema. `delete`'s has `deleted`, a boolean, and `id`, a string. Which members each output schema marks required, and which carry a description, are not fixed here. The schemas carry no `$schema` member.

Preconditions:

- cron is serving, and telemetry takes every event.

Postconditions:

- Nothing has changed. No trigger was read, and nothing was emitted to the event bus.
- cron wrote nothing to stderr. telemetry has received the request's two events, under user `u_7f3a9c21` and the id cron gave the request, and no `tool.called`, since no tool ran:

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `request_bytes` is the length of the request's body and `response_bytes` the length of the response's.

## A client asks cron what it is for

A client tells its model what each server is for through the server's instructions. cron's instructions are its own description, as the host's services file gives it, so they are written once, in cron's manifest (`S01`), and never anywhere else. cron reads the file afresh for every request that asks, as it does for the launcher (`S03`), so a rewrite of the file shows in the next answer without a restart.

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: server/discover

{"jsonrpc":"2.0","id":2,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has exactly these members besides the `resultType`, `_meta`, `ttlMs`, and `cacheScope` every such result carries:

```
{
  "supportedVersions": ["2026-07-28", "2025-11-25", "2025-06-18"],
  "capabilities": {"tools": {}},
  "instructions": "Triggers that emit events on a schedule"
}
```

Preconditions:

- cron is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file, whose entry named `cron` has the description `Triggers that emit events on a schedule`.

Postconditions:

- Nothing has changed. The services file is as it was.
- cron wrote nothing to stderr.

## A client asks cron what it is for on a host with no services file

With no services file there is no description to give, so the answer has no instructions at all rather than empty ones, and is otherwise the same. The answer is the same when the file named is missing, cannot be read, or has no entry named `cron`. The tools are offered all the same.

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: server/discover

{"jsonrpc":"2.0","id":3,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has `supportedVersions` `["2026-07-28","2025-11-25","2025-06-18"]`, `capabilities` `{"tools":{}}`, the members every such result carries, and no `instructions` member.

Preconditions:

- cron is serving, started with `IKIGENBA_SERVICES` unset.

Postconditions:

- Nothing has changed.
- cron wrote nothing to stderr about the missing instructions. With no services file it has no telemetry to send to (`S02`), so stderr holds the request's two events, `request.started` and `request.finished`, each as a `cron: undelivered event: <event>` line, and nothing else for this request.

## A client speaking an earlier revision opens with initialize

Many clients in the field still speak `2025-11-25` or `2025-06-18`, which open with `initialize` and send no `_meta`. cron serves them as every app does: `initialize` echoes the revision the client asked for when it is one of those two, and answers `2025-11-25` otherwise. The instructions are the ones `server/discover` gives. cron keeps no session, so the client's following `notifications/initialized` is answered `202` with an empty body, and the client may list or call tools with or without having sent `initialize`.

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json

{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"example-client","version":"1.0.0"}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has exactly these members:

```
{
  "protocolVersion": "2025-11-25",
  "capabilities": {"tools": {}},
  "serverInfo": {"name": "cron", "version": "<display>"},
  "instructions": "Triggers that emit events on a schedule"
}
```

The response carries no `Mcp-Session-Id` header.

Preconditions:

- cron is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed.
- cron wrote nothing to stderr.

## A client speaking an earlier revision lists the tools

A client on an earlier revision is offered the same seven tools, with the same names, descriptions, schemas, and annotations, and its calls get the same tool results, `isError` results included, as on `2026-07-28`. Only the envelope differs: results carry no `resultType`, `_meta`, `ttlMs`, or `cacheScope`, and a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200, not 400.

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2025-11-25

{"jsonrpc":"2.0","id":2,"method":"tools/list"}
```

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2025-06-18

{"jsonrpc":"2.0","id":2,"method":"tools/list"}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has exactly one member, `tools`, the array of `An MCP client lists cron's tools`, member for member.

Preconditions:

- cron is serving.

Postconditions:

- Nothing has changed.
- cron wrote nothing to stderr.

## A browser opens /mcp

`/mcp` is for MCP clients, which only POST. A GET, from a browser or a client hoping for a stream, is refused with an empty body: cron offers no stream and no page at this address, and its triggers are shown at `/` (`S03`). A DELETE, from a client ending a session it believes it has, is refused the same way, since there are no sessions, and so is any other method that is not POST.

Request:

```
GET /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
DELETE /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 405 Method Not Allowed
Allow: POST
```

Status 405. The body is empty.

Preconditions:

- cron is serving.

Postconditions:

- Nothing has changed.
- cron wrote nothing to stderr.

## A request to /mcp arrives without the identity headers

`/mcp` follows the rule every route of cron follows (`S03`): nginx sets `X-User-Id` on every request it forwards and the gateway forwards the one it received, so a request without it says nginx or the gateway is misconfigured, a server fault answered 500. The identity check runs before anything about MCP is looked at, so the answer is the same plain text every route gives, not a JSON-RPC response, and no tool runs, whatever the body asked for. Without a caller there is no one to own a new trigger and no owner to check a change against. An `X-User-Id` header whose value is empty is answered the same way.

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: create

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create","arguments":{"slug":"crm_sync","when":"*/15 * * * *"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the line `identity header missing`, ending with LF. A `GET /mcp` with no identity is answered the same way, not with the 405 of `A browser opens /mcp`.

Preconditions:

- cron is serving, and telemetry takes every event.
- No trigger has the slug `crm_sync`.
- The request carries no `X-User-Id` header and no `X-Request-Id` header.

Postconditions:

- Nothing has changed. No trigger was created, and nothing was emitted to the event bus.
- cron wrote nothing to stderr about the 500. telemetry has received the request's two events, with an empty user, under the id cron gave the request (`S02`); no tool ran, so there is no `tool.called` and no `cron.*` event:

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"<request-id>","user":"","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"<request-id>","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":500}}
  ```

## A model calls a tool cron does not have

cron has seven tools. A call naming any other — a tool to rename a trigger, say, which cron does not offer, since a slug is a trigger's identity and `update` changes only its schedule (`S08`) — is not a tool's refusal but a protocol error: there is no tool to answer it.

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: rename

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"rename","arguments":{"slug":"hourly","new_slug":"every_hour"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
```

Status 400. The body is a JSON-RPC response with `id` 5 and no `result`, whose `error` has `code` `-32602` and `message` `Unknown tool: rename`.

Preconditions:

- cron is serving, and telemetry takes every event.
- The caller owns the trigger `hourly`, `crn_3f9a1c7e5b2d8046` (`S06`).

Postconditions:

- Nothing has changed. `hourly` is as it was and fires at its next slot, and nothing was emitted to the event bus.
- cron wrote nothing to stderr. No tool ran, so cron recorded no `tool.called`: telemetry has received only the request's `request.started` and its `request.finished`, whose `status` is 400, under user `u_7f3a9c21`.

## A model calls a tool while cron cannot reach its database

Every tool answers from cron's database, so a tool that cannot read it has nothing true to say. It does not answer as if the space had no triggers, which would tell the model its triggers were gone; it refuses, in the same words whichever of the seven tools was called, and quotes nothing of the database's own error. The model can tell this from a refusal of its arguments and may try again later. cron keeps serving; a page opened meanwhile is answered as `S03` tells.

Request:

```
POST /mcp HTTP/1.1
Host: cron.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"list","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
cannot reach the database; try again later
```

A `show`, `create`, `update`, `pause`, `resume`, or `delete` call whose arguments the tool would otherwise act on is refused with the same text, and a `create`, `update`, `pause`, `resume`, or `delete` so refused has changed nothing, recorded no `cron.*` event, and emitted nothing to the event bus. A call the tool refuses whatever the database holds — arguments refused as they are read against the input schema, or a `create` whose slug breaks the rule for slugs — is refused as its own group says. A `create` whose `when` is not allowed is refused with `invalid when '<when>'` only once its slug has been found free, and an `update` whose `when` is not allowed only once its trigger has been found (`S06`, `S08`), so with the database out of reach both get the line above.

Preconditions:

- cron is serving, and telemetry takes every event.
- cron's database cannot be read: `state/cron.db` has become unreadable since cron opened it, its storage failing reads, say.

Postconditions:

- Nothing has changed.
- cron wrote nothing to stderr. telemetry has received the request's three events, under user `u_7f3a9c21` and request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`:

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"error","tool":"list"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

- cron is still serving.

## The mcp gateway calls cron over its socket

The platform's MCP gateway offers the tools of every service whose entry in the services file is marked for MCP, and cron's is. A model asks the gateway to `call` the service `cron` and a read tool, or to `mutate` with any other, and the gateway calls cron directly on its socket, not through nginx, on behalf of the caller it is serving: it speaks `2026-07-28`, names no service in `Host`, sending `Host: backend` as it does to every service, and forwards that caller's `X-User-Id`, `X-User-Email`, and `X-Request-Id`, as any sibling does (`S02`). cron answers the gateway exactly as it answers a client through nginx; it cannot tell the two apart and does not try, and the caller the gateway forwards is the one every tool works for and every owner check is made against. Here a developer on the host, as the `ikigenba` user, stands in for the gateway's `call` of `list`; what the gateway does with the answer is the gateway's, told in its own stories.

Request:

```
POST /mcp HTTP/1.1
Host: backend
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
Accept: application/json, text/event-stream
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

and a `content` array of one text block whose text is exactly that line, as `S07` tells. `ann@example.com`'s `month_end` and `nightly_backup` are in it beside the caller's own.

Preconditions:

- cron is deployed and active on the host, serving on `/run/ikigenba/cron.sock`.
- The host's services file lists cron with `"mcp": true`, the `url` `https://cron.sbx.ikigenba.dev`, and the socket `/run/ikigenba/cron.sock`, and lists the telemetry service, which takes every event.
- The caller runs as the `ikigenba` user, which can reach the socket.
- cron holds `S06`'s shared fixture: the triggers `hourly`, `month_end`, `nightly_backup`, and `weekly_digest`, and it is `2026-10-05T09:32:00Z`.

Postconditions:

- Nothing has changed. Every trigger fires at its next slot as before, and nothing was emitted to the event bus.
- cron wrote nothing to stderr.
- telemetry has received cron's three events for the call under the id the developer sent and the user it sent, as the gateway would forward them, so a trace of that id shows the gateway's forward and cron's execution together:

  ```
  {"time":"<time>","service":"cron","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"cron","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"list"}}
  {"time":"<time>","service":"cron","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  No trigger's id or slug is in them.
