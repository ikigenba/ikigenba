# Stories — mcp endpoint

The bus offered to models: events' MCP interface at `/mcp`, the one route that answers MCP clients rather than browsers or siblings. It is the exact path `/mcp`; a path beneath it, `/mcp/tools` say, is a path that names nothing and is answered with events' not-found page (`S03`). `/mcp` takes only POST, each POST carries one JSON-RPC request, and each request is answered with one `application/json` body: there are no sessions and no streams, and events keeps nothing from one request to the next. It offers five tools and nothing else, in this order: `catalog`, which shows every event name or pattern the suite's services declare that they emit, and every name the log holds, who emits and accepts each, and how many the log retains (`S08`); `search`, which lists the retained log, newest first, filtered and paged (`S09`); `subscribers`, which shows each subscriber's status, reason, cursor, lag and since (`S10`); `skip`, which moves a paused subscriber past the event it is stuck on and resumes its deliveries (`S12`); and `resume`, which clears a subscriber's pause and retries the event it is stuck on (`S12`). There is no tool that emits an event: only a service emits, at `/emit` (`S07`). Every signed-in user sees the same bus: no tool narrows what it shows to the caller. Each tool's arguments, its result, and its failures are told in its own group; what they share is fixed here.

On a host, a model does not reach `/mcp` directly: it reaches events' tools through the mcp gateway, naming the service `events` and the tool, and the gateway calls events at its socket on the model's behalf (below, and `S17`). events is one of the suite's MCP services, so the host's services file carries `"mcp": true` for it and the gateway's `describe` lists its five tools with their kinds: `catalog`, `search` and `subscribers` are of kind `read` and run with the gateway's `call`; `skip` is of kind `destructive` and `resume` of kind `additive`, and both run with `mutate`. The actor in this group is a model working through an MCP client, the client itself, or the gateway standing in for either. Each request is shown as the HTTP request events receives on a running events (`S02`), started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment, the host's services file, which holds the suite's services file (`S03`) unless a story says otherwise, and whose `telemetry` entry names the telemetry service, which takes every event (`S02`). `/mcp` is behind the same identity rule as every route of events but `/emit`, and events serves nothing to guests: nginx sets `X-User-Id` and `X-User-Email` and, on a host with an authenticator, challenges a request with no credential at `/mcp`, never passing it to events; the gateway forwards both headers (`S02`); and a request without `X-User-Id` is answered 500 before anything else is looked at. The requests below carry both headers by hand, and only the missing-header story carries neither. `/mcp` answers the same whatever `Host` names: the requests below carry `Host: events.sbx.ikigenba.dev`, as nginx passes it, but for the gateway's, which sends `Host: backend` to every service.

A client speaking the protocol revision `2026-07-28` sends, on every request, the headers `Content-Type: application/json`, `MCP-Protocol-Version: 2026-07-28`, and `Mcp-Method` equal to the request's `method`, and for a `tools/call` also `Mcp-Name` equal to the tool's name; and its `params` carry `_meta` with `io.modelcontextprotocol/protocolVersion` `2026-07-28` and `io.modelcontextprotocol/clientCapabilities` `{}`. Every successful answer on that revision is status 200, and its `result`, besides what each story fixes, carries `resultType` `"complete"` and `_meta` whose `io.modelcontextprotocol/serverInfo` is `{"name":"events","version":"<display>"}`, where `<display>` is the string `events --version` prints under the environment events was started with (`S01`), the empty string when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`; a `tools/list` or `server/discover` result also carries `ttlMs` `0` and `cacheScope` `"private"`. Those members are not repeated in this group or in `S08` to `S12`. A client speaking an earlier revision, `2025-11-25` or `2025-06-18`, opens with `initialize` and is served the same tools, and its calls get the same tool results, `isError` results included, without those members; a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200 on the earlier revisions. How the transport answers a request that is not well-formed MCP — a wrong `Content-Type`, a body over 1 MiB, a foreign `Origin`, mismatched headers, an unknown method — is the platform's, the same for every app, and this group does not restate it.

A tool's successful result carries the answer twice: as `structuredContent`, a JSON object, and as one text content block whose text is that same object encoded compactly, with no white space between its tokens. A tool that cannot do what it was asked answers status 200 with a result whose `isError` is `true`, with no `structuredContent` and a `content` array of exactly one text block saying why. Arguments refused as they are read against the tool's input schema — a field missing, of the wrong JSON type, or one the tool does not have — are reported with the platform's wording: the line `invalid arguments:` followed by one line per offence, each `<field>: <reason>` (`limit: expected integer, got string`, `bogus: unknown field`), separated by LF with no LF after the last; the tool's fields come first, in the order of its input schema, then each unknown field in the order it was sent. Arguments that pass that reading but that events refuses, or a call it cannot carry out as things stand, are refused with one line in events' own words, told in the group of the tool that meets it (`S08` to `S12`): among them `no subscriber '<service>'` and `'<service>' is not paused` (`S12`), `<service>` quoted exactly as it was sent. A refusal is one line, unless a value it quotes, as sent, itself holds a line break. Every tool, when events cannot read or write its log, refuses with exactly `cannot reach the log; try again later`, quoting nothing of the underlying error. A filter that matches nothing is not a failure: it is an empty result. Nothing changes in the log, in any subscriber's cursor or status, or in any delivery by a call that is refused.

Every request to `/mcp` is recorded in events' trail as every request is, by its `request.started` and `request.finished` (`S02`, `S03`). A `tools/call` that reaches one of the five tools and is answered with a `result`, an `isError` result included, also records `tool.called`, after anything the tool recorded and before the request's `request.finished`, under the caller's request id and user (`S14`). Its attributes are `tool`, the tool's name; `kind`, the tool's kind as the gateway's `describe` lists it, `read` for `catalog`, `search` and `subscribers`, `destructive` for `skip`, and `additive` for `resume`; `outcome`, `ok` for a result with no `isError`, `invalid_arguments` when the arguments were refused as they were read against the input schema, and `error` for every other refusal; and `duration_us`, how long the tool took, in whole microseconds, 0 when the arguments were refused as they were read and the tool never ran. The arguments themselves and the text of a refusal are never recorded. A call answered with a protocol error, such as an unknown tool, reached no tool and records no `tool.called`; nor does any other method.

A response body below is laid out for reading: its white space is not fixed. The order of members within a tool, where a story shows one, is fixed as shown; elsewhere the order of members is not. A response block shows the status line and the headers the story fixes; a header it does not show is not fixed. No answer in this group or in `S08` to `S12` earns a line on stderr, the missing-header 500 included; only an event of its trail events cannot deliver to telemetry reaches stderr (`S02`).

## An MCP client lists events' tools

A client lists the tools before offering them to a model. What it gets is everything the model is told about each tool: its name, a description written for the model, the shape of its arguments and its answer, and how careful the client must be before calling it. The description's first line is a one-line summary a catalogue can show on its own, the same line the landing page shows beside the tool's name (`S03`). `catalog`, `search` and `subscribers` change nothing, so they are marked read-only and not destructive. `skip` gives up a subscriber's delivery of the event it is stuck on for good: that event is never delivered to it again, so `skip` is marked destructive. `resume` only tries the same event again, and takes nothing away, so it is marked neither read-only nor destructive. None is open-world: each acts on the platform's own data.

Request:

```
POST /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
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

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `nextCursor` and whose `tools` is an array of exactly five tools, in this order, each an object with the members `name`, `description`, `inputSchema`, `outputSchema`, and `annotations`, in that order:

- `catalog`, whose `description` begins with the line `Every event the suite emits, who emits and accepts it, counts and last seen.`, whose `inputSchema` is `{"type": "object", "properties": {"service": {"type": "string"}, "event": {"type": "string"}}, "additionalProperties": false}`, and whose `annotations` are `{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}`;
- `search`, whose `description` begins with the line `The retained log, newest first, filtered by service, event, user, request id, cause or attributes.`, whose `inputSchema` is `{"type": "object", "properties": {"since": {"type": "string"}, "until": {"type": "string"}, "services": {"type": "array", "items": {"type": "string"}}, "events": {"type": "array", "items": {"type": "string"}}, "user": {"type": "string"}, "request_id": {"type": "string"}, "cause": {"type": "string"}, "attrs": {"type": "object"}, "limit": {"type": "integer"}, "cursor": {"type": "string"}}, "additionalProperties": false}`, every argument optional, and whose `annotations` are `{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}`;
- `subscribers`, whose `description` begins with the line `Each subscriber's status, reason, cursor and lag.`, whose `inputSchema` is `{"type": "object", "additionalProperties": false}`, since it takes no arguments, and whose `annotations` are `{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}`;
- `skip`, whose `description` begins with the line `Skip the event a paused subscriber is stuck on and resume it.`, whose `inputSchema` is `{"type": "object", "properties": {"service": {"type": "string"}}, "required": ["service"], "additionalProperties": false}`, and whose `annotations` are `{"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}`;
- `resume`, whose `description` begins with the line `Retry the event a paused subscriber is stuck on.`, whose `inputSchema` is `{"type": "object", "properties": {"service": {"type": "string"}}, "required": ["service"], "additionalProperties": false}`, and whose `annotations` are `{"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false}`.

Each input property may also carry a `description` of its own, written for the model, which this story does not fix. The order of the properties in each `inputSchema` is fixed as shown. What each argument does, `limit`'s range of 1 to 500 among it, is told in the tool's own group (`S08` to `S12`); no story fixes the rest of a description or any tool's `outputSchema`. The schemas carry no `$schema` member.

Preconditions:

- events is serving, and telemetry takes every event.

Postconditions:

- Nothing has changed. The log was not read.
- events wrote nothing to stderr. telemetry has received the request's two events, under user `u_7f3a9c21` and the id events gave the request, and no `tool.called`, since no tool ran:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"<request-id>","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

  `request_bytes` is the length of the request's body and `response_bytes` the length of the response's.

## A client asks events what it is for

A client tells its model what each server is for through the server's instructions. events' instructions are its own description, as the host's services file gives it, so they are written once, in events' manifest, and never anywhere else. events reads the file afresh for every request that asks, as it does for the launcher (`S03`), so a rewrite of the file shows in the next answer without a restart.

Request:

```
POST /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
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

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has exactly these members besides the `resultType`, `_meta`, `ttlMs`, and `cacheScope` every such result carries, taking its instructions from the description of the services file's entry named `events`:

```
{
  "supportedVersions": ["2026-07-28", "2025-11-25", "2025-06-18"],
  "capabilities": {"tools": {}},
  "instructions": "The suite's internal event bus"
}
```

Preconditions:

- events is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file, whose entry named `events` has the description `The suite's internal event bus`.

Postconditions:

- Nothing has changed. The services file is as it was.
- events wrote nothing to stderr.

## A client asks events what it is for on a host with no services file

With no services file there is no description to give, so the answer has no instructions at all rather than empty ones, and is otherwise the same. The answer is the same when the file named is missing, cannot be read, or has no entry named `events`. The tools are offered all the same.

Request:

```
POST /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
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

- events is serving, started with `IKIGENBA_SERVICES` unset.

Postconditions:

- Nothing has changed.
- events wrote nothing to stderr about the missing instructions. With no services file it has no telemetry to send to (`S02`), so stderr holds the request's two events, `request.started` and `request.finished`, each as an `events: undelivered event: <event>` line, and nothing else for this request.

## A client speaking an earlier revision opens with initialize

Many clients in the field still speak `2025-11-25` or `2025-06-18`, which open with `initialize` and send no `_meta`. events serves them as every app does: `initialize` echoes the revision the client asked for when it is one of those two, and answers `2025-11-25` otherwise. The instructions are the ones `server/discover` gives. events keeps no session, so the client's following `notifications/initialized` is answered `202` with an empty body, and the client may list or call tools with or without having sent `initialize`.

Request:

```
POST /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
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
  "serverInfo": {"name": "events", "version": "<display>"},
  "instructions": "The suite's internal event bus"
}
```

The response carries no `Mcp-Session-Id` header.

Preconditions:

- events is serving, started with `IKIGENBA_SERVICES=/run/ikigenba/services.json` in its environment.
- `/run/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed.
- events wrote nothing to stderr.

## A client speaking an earlier revision lists the tools

A client on an earlier revision is offered the same five tools, with the same names, descriptions, schemas, and annotations, and its calls get the same tool results, `isError` results included, as on `2026-07-28`. Only the envelope differs: results carry no `resultType`, `_meta`, `ttlMs`, or `cacheScope`, and a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200, not 400.

Request:

```
POST /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2025-11-25

{"jsonrpc":"2.0","id":2,"method":"tools/list"}
```

```
POST /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
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

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has exactly one member, `tools`, the array of `An MCP client lists events' tools`, member for member.

Preconditions:

- events is serving.

Postconditions:

- Nothing has changed.
- events wrote nothing to stderr.

## A browser opens /mcp

`/mcp` is for MCP clients, which only POST. A GET, from a browser or a client hoping for a stream, is refused with an empty body: events offers no stream and no page at this address, and its landing page is at `/` (`S03`). A DELETE, from a client ending a session it believes it has, is refused the same way, since there are no sessions, and so is any other method that is not POST.

Request:

```
GET /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
DELETE /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
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

- events is serving.

Postconditions:

- Nothing has changed.
- events wrote nothing to stderr.

## A request to /mcp arrives without the identity headers

`/mcp` follows the rule every route of events but `/emit` follows (`S03`): nginx sets `X-User-Id` on every request it forwards and the gateway forwards the one it received, so a request without it says nginx or the gateway is misconfigured, a server fault answered 500. The identity check runs before anything about MCP is looked at, so the answer is the same plain text every route gives, not a JSON-RPC response, and no tool runs, whatever the body asked for. An `X-User-Id` header whose value is empty is answered the same way.

Request:

```
POST /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/list

{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the line `identity header missing`, ending with LF. A `tools/call` of any of the five tools with no identity is answered the same way, and so is a `GET /mcp`, not with the 405 of `A browser opens /mcp`.

Preconditions:

- events is serving, and telemetry takes every event.
- The request carries no `X-User-Id` header and no `X-Request-Id` header.

Postconditions:

- Nothing has changed. The log was not read, and no subscriber's cursor or status moved.
- events wrote nothing to stderr about the 500. telemetry has received the request's two events, with an empty user, under the id events gave the request (`S02`); no tool ran, so there is no `tool.called`:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"<request-id>","user":"","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"<request-id>","user":"","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":500}}
  ```

## A model calls a tool events does not have

events has five tools. A call naming any other — a tool to emit an event, say, which events does not offer, since only a service emits, at `/emit` (`S07`) — is not a tool's refusal but a protocol error: there is no tool to answer it.

Request:

```
POST /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: emit

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"emit","arguments":{"event":"repo.pushed"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
```

Status 400. The body is a JSON-RPC response with `id` 5 and no `result`, whose `error` has `code` `-32602` and `message` `Unknown tool: emit`.

Preconditions:

- events is serving, and telemetry takes every event.

Postconditions:

- Nothing has changed. No event was added to the log, and nothing was delivered.
- events wrote nothing to stderr. No tool ran, so events recorded no `tool.called`: telemetry has received only the request's `request.started` and its `request.finished`, whose `status` is 400, under user `u_7f3a9c21`.

## A model calls a tool while events cannot read its log

Every tool answers from the log, so a tool that cannot read it has nothing true to say. It does not answer as if the bus were empty or had no subscribers, which would tell the model every service had stopped; it refuses, in the same words whichever of the five tools was called, and quotes nothing of the database's own error. The model can tell this from a refusal of its arguments and may try again later. events keeps serving; a page opened meanwhile is answered as `S03` tells.

Request:

```
POST /mcp HTTP/1.1
Host: events.sbx.ikigenba.dev
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: subscribers

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"subscribers","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
cannot reach the log; try again later
```

A `catalog`, `search`, `skip`, or `resume` call whose arguments pass the reading against its input schema is refused with the same text, and a `skip` or `resume` so refused has changed nothing: no subscriber's cursor or status moved, and nothing was delivered or skipped. A call whose arguments are refused as they are read against the input schema is refused with the platform's `invalid arguments:` wording, as above.

Preconditions:

- events is serving, and telemetry takes every event.
- events' database cannot be read: `state/events.db` has become unreadable since events opened it, its storage failing reads, say.

Postconditions:

- Nothing has changed.
- events wrote nothing to stderr. telemetry has received the request's three events, under user `u_7f3a9c21` and request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"events","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"error","tool":"subscribers"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

- events is still serving.

## The mcp gateway calls events over its socket

The platform's MCP gateway offers the tools of every service whose entry in the services file is marked for MCP, and events' is. A model asks the gateway to `call` the service `events` and a read tool, or to `mutate` with `skip` or `resume`, and the gateway calls events directly on its socket, not through nginx, on behalf of the caller it is serving: it speaks `2026-07-28`, names no service in `Host`, sending `Host: backend` as it does to every service, and forwards that caller's `X-User-Id`, `X-User-Email`, and `X-Request-Id`, as any sibling does (`S02`). events answers the gateway exactly as it answers a client through nginx; it cannot tell the two apart and does not try. Here a developer on the host, as the `ikigenba` user, stands in for the gateway's `call` of `subscribers`; what the gateway does with the answer is the gateway's, told in its own stories.

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
Mcp-Name: subscribers

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"subscribers","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, and whose `structuredContent` and text block are the ones a client reaching `/mcp` through nginx gets for the same call at the same moment (`S10`).

Preconditions:

- events is deployed and active on the host, serving on `/run/ikigenba/events.sock`.
- The host's services file lists events with `"mcp": true`, the `url` `https://events.sbx.ikigenba.dev`, and the socket `/run/ikigenba/events.sock`, and lists the telemetry service, which takes every event.
- The caller runs as the `ikigenba` user, which can reach the socket.

Postconditions:

- Nothing has changed. No subscriber's cursor or status moved.
- events wrote nothing to stderr. telemetry has received the request's three events, under user `u_7f3a9c21` and request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`:

  ```
  {"time":"<time>","service":"events","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  {"time":"<time>","service":"events","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"subscribers"}}
  {"time":"<time>","service":"events","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```
