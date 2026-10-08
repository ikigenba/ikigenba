# Stories — mcp endpoint

The trail offered to models: telemetry's MCP interface at `/mcp`, the one route that answers MCP clients rather than browsers or siblings. It is the exact path `/mcp`; a path beneath it, `/mcp/tools` say, is a path that does not exist and is answered as `S03` answers one. `/mcp` takes only POST, each POST carries one JSON-RPC request, and each request is answered with one `application/json` body: there are no sessions and no streams, and telemetry keeps nothing from one request to the next. It offers four tools and nothing else, in this order: `catalog`, which shows the services in the trail, the events each records, and the attribute keys each carries (`S08`); `search`, which lists the records that match a filter, newest first and paged (`S09`); `count`, which counts the records that match a filter, in total or grouped (`S10`); and `trace`, which lists every record of one request, from every service, oldest first (`S11`). All four read the trail and change nothing; there is no tool that adds to the trail or takes from it, since siblings write the trail by posting to `/ingest` (`S06`) and retention takes from it (`S07`). Each tool's arguments, its result, and its failures are told in its own group; what they share is fixed here. A record in any result is the event as it was ingested, an object whose members are, in this order, `time`, `service`, `event`, `request_id`, `user`, and `attrs`.

On a host, a model does not reach `/mcp` directly: it reaches telemetry's tools through the mcp gateway's `call`, naming the service `telemetry` and the tool, and the gateway calls telemetry at its socket on the model's behalf (below, and `S14`). telemetry's manifest marks it an MCP service (`S01`), so the host's services file carries `"mcp": true` for it and the gateway's `describe` lists its four tools, each of kind `read`, which is why `call` runs them and `mutate` never does. The actor in this group is a model working through an MCP client, the client itself, or the gateway standing in for either. Each request is shown as the HTTP request telemetry receives on a running telemetry (`S02`), started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment, the host's services file, which holds the suite's services file (`S03`) unless a story says otherwise. `/mcp` is behind the same identity rule as every route but `/ingest`: the gate sets `X-User-Id` and `X-User-Email`, the gateway forwards them (`S02`), and a request without `X-User-Id` is answered 500 before anything else is looked at. The requests below carry both headers by hand, and only the missing-header story carries neither.

A client speaking the protocol revision `2026-07-28` sends, on every request, the headers `Content-Type: application/json`, `MCP-Protocol-Version: 2026-07-28`, and `Mcp-Method` equal to the request's `method`, and for a `tools/call` also `Mcp-Name` equal to the tool's name; and its `params` carry `_meta` with `io.modelcontextprotocol/protocolVersion` `2026-07-28` and `io.modelcontextprotocol/clientCapabilities` `{}`. Every successful answer on that revision is status 200, and its `result`, besides what each story fixes, carries `resultType` `"complete"` and `_meta` whose `io.modelcontextprotocol/serverInfo` is `{"name":"telemetry","version":"<display>"}`, where `<display>` is the string `telemetry --version` prints under the environment telemetry was started with (`S01`), the empty string when that environment sets neither `IKIGENBA_COMMIT` nor `IKIGENBA_RELEASE`; a `tools/list` or `server/discover` result also carries `ttlMs` `0` and `cacheScope` `"private"`. Those members are not repeated in this group or in `S08` to `S11`. A client speaking an earlier revision, `2025-11-25` or `2025-06-18`, opens with `initialize` and is served the same tools, and its calls get the same tool results, `isError` results included, without those members; a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200 on the earlier revisions. How the transport answers a request that is not well-formed MCP — a wrong `Content-Type`, a body over 1 MiB, a foreign `Origin`, mismatched headers, an unknown method — is the platform's, the same for every app, and this group does not restate it.

A tool's successful result carries the answer twice: as `structuredContent`, a JSON object, and as one text content block whose text is that same object encoded compactly, with no white space between its tokens. A tool that cannot do what it was asked — a time that is not RFC 3339, a limit out of range, a cursor it did not issue, a `by` it does not know — answers status 200 with a result whose `isError` is `true`, with no `structuredContent` and a `content` array of exactly one text block saying why, in the words its own group fixes. A tool that refuses its arguments as they are read against its input schema answers the same way with the platform's wording: the line `invalid arguments:` followed by one line per offence, each `<field>: <reason>` (`request_id: missing required field`, `limit: expected integer, got string`, `bogus: unknown field`), separated by LF with no LF after the last. A filter that matches nothing is not a failure: it is an empty result.

Every request to `/mcp` is recorded in telemetry's own trail as every request but an ingest is, by its `request.started` and `request.finished` (`S02`). A `tools/call` that reaches one of the four tools and is answered with a `result`, an `isError` result included, also records `tool.called`, before the request's `request.finished`, under the caller's request id and user. Its attributes are `tool`, the tool's name; `kind`, `read` for all four; `outcome`, which says how the call was answered: `ok` for a result with no `isError`, `invalid_arguments` when the arguments were refused as they were read against the input schema, and `error` when they passed that but the tool could not do what it was asked; and `duration_us`, how long the tool took, in whole microseconds, 0 when the arguments were refused as they were read and the tool never ran. The arguments themselves, and the text of a refusal, are never recorded. A call answered with a protocol error, such as an unknown tool, reached no tool and records no `tool.called`; nor does any other method. A tool reads the trail as it is in the store when the call runs. telemetry stores its own events off the request's path, in order, shortly after they happen (`S02`, `S12`), so whether the `request.started` of the request that runs the tool is already there is not fixed; what a tool never finds is its own `tool.called`, recorded after it has answered.

A response body below is laid out for reading: its white space is not fixed. The order of members within a tool and within its schemas is fixed as shown; elsewhere the order of members is not. A response block shows the status line and the headers the story fixes; a header it does not show is not fixed. No answer in this group earns a line on stderr, the missing-header 500 included (`S02`, `S03`).

## An MCP client lists telemetry's tools

A client lists the tools before offering them to a model. What it gets is everything the model is told about each tool: its name, a description written for the model, the shape of its arguments and its answer, and how careful the client must be before calling it. The description's first line is a one-line summary a catalogue can show on its own, the same line the landing page shows beside the tool's name (`S03`). All four only read the trail, so all four are marked read-only and none destructive; none reaches outside the platform's own data, so none is open-world.

Request:

```
POST /mcp HTTP/1.1
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

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `nextCursor` and whose `tools` is an array of exactly four tools, in this order:

```
[
  {
    "name": "catalog",
    "description": "The services, the events each records, and the attribute keys each carries.\n\nWithout arguments, every service in the trail in name order, each with its events in name order, and for each event how many records it has, when the latest was recorded, and the attribute keys its records carry. Pass service or event, or both, to narrow it. Call it first to learn what search and count can filter on.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "service": {"type": "string"},
        "event": {"type": "string"}
      },
      "additionalProperties": false
    },
    "outputSchema": <the catalog output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "search",
    "description": "The records that match a filter, newest first.\n\nEvery argument is optional and they combine: since and until bound the time (RFC 3339, since inclusive, until exclusive), services and events take any of the names given, user and request_id match exactly, and attrs is an object whose every pair a record must carry. limit is 1 to 500, 50 when left out. When more records match, the result carries a cursor; pass it back with the same filters for the next page.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "since": {"type": "string"},
        "until": {"type": "string"},
        "services": {"type": "array", "items": {"type": "string"}},
        "events": {"type": "array", "items": {"type": "string"}},
        "user": {"type": "string"},
        "request_id": {"type": "string"},
        "attrs": {"type": "object"},
        "limit": {"type": "integer"},
        "cursor": {"type": "string"}
      },
      "additionalProperties": false
    },
    "outputSchema": <the search output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "count",
    "description": "How many records match a filter, grouped by a field or a time bucket.\n\nTakes the filters of search. Without by, the total. With by, one of service, event, user, request_id, minute, hour, day, or attrs.<key>, the total and one group per value with its count.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "since": {"type": "string"},
        "until": {"type": "string"},
        "services": {"type": "array", "items": {"type": "string"}},
        "events": {"type": "array", "items": {"type": "string"}},
        "user": {"type": "string"},
        "request_id": {"type": "string"},
        "attrs": {"type": "object"},
        "by": {"type": "string"}
      },
      "additionalProperties": false
    },
    "outputSchema": <the count output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "trace",
    "description": "Every record of one request, from every service it touched, oldest first.\n\nPass the request id; the result is the records of that id from every service, in the order they happened. An id the trail does not hold gives no records, not an error.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "request_id": {"type": "string"}
      },
      "required": ["request_id"],
      "additionalProperties": false
    },
    "outputSchema": <the trace output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  }
]
```

Each input property also carries a `description` of its own, written for the model, which this story does not fix. Only `trace` marks an argument required. The output schemas are not quoted whole. `search`'s and `trace`'s each describe an object closed to other members whose `records` property is an array of records, each an object with the properties, in this order, `time`, a string; `service`, a string; `event`, a string; `request_id`, a string; `user`, a string; and `attrs`, an object; `search`'s also has `cursor`, a string. `count`'s describes an object with `total`, an integer, and `groups`, an array of objects with `key`, a string, and `count`, an integer. `catalog`'s describes an object whose one property, `services`, is an array of objects with `service`, a string, and `events`, an array of objects with `event`, a string, `count`, an integer, `last_seen`, a string, and `attrs`, an array of strings. Which members each output schema marks required, and which carry a description, are not fixed here. The schemas carry no `$schema` member.

Preconditions:

- telemetry is serving.

Postconditions:

- Nothing has changed but the trail.
- telemetry wrote nothing to stderr. The trail holds the request's two events, under user `u_7f3a9c21` and a request id telemetry made up for it, and no `tool.called`, since no tool ran:

  ```
  request.started method=POST path=/mcp
  request.finished status=200
  ```

## A client asks telemetry what it is for

A client tells its model what each server is for through the server's instructions. telemetry's instructions are its own description, as the host's services file gives it, so they are written once, in telemetry's manifest (`S01`), and never anywhere else. telemetry reads the file afresh for every request that asks, as it does for the launcher (`S03`), so a rewrite of the file shows in the next answer without a restart.

Request:

```
POST /mcp HTTP/1.1
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
  "instructions": "The suite's trail of events"
}
```

Preconditions:

- telemetry is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file, whose entry named `telemetry` has the description `The suite's trail of events`.

Postconditions:

- Nothing has changed but the trail. The services file is as it was.
- telemetry wrote nothing to stderr.

## A client asks telemetry what it is for on a host with no services file

With no services file there is no description to give, so the answer has no instructions at all rather than empty ones, and is otherwise the same. The answer is the same when the file named is missing, cannot be read, or has no entry named `telemetry`. The tools are offered all the same, and telemetry's trail is kept all the same: unlike a sibling, telemetry needs no services file to record its own events (`S02`).

Request:

```
POST /mcp HTTP/1.1
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

- telemetry is serving, started with `IKIGENBA_SERVICES` unset.

Postconditions:

- Nothing has changed but the trail.
- telemetry wrote nothing to stderr about the missing instructions. The trail holds the request's two events, `request.started` with `method=POST path=/mcp` and `request.finished` with `status=200`, under user `u_7f3a9c21` and a request id telemetry made up for it, and nothing else for this request.

## A client speaking an earlier revision opens with initialize

Many clients in the field still speak `2025-11-25` or `2025-06-18`, which open with `initialize` and send no `_meta`. telemetry serves them as every app does: `initialize` echoes the revision the client asked for when it is one of those two, and answers `2025-11-25` otherwise. The instructions are the ones `server/discover` gives. telemetry keeps no session, so the client's following `notifications/initialized` is answered `202` with an empty body, and the client may list or call tools with or without having sent `initialize`.

Request:

```
POST /mcp HTTP/1.1
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
  "serverInfo": {"name": "telemetry", "version": "<display>"},
  "instructions": "The suite's trail of events"
}
```

The response carries no `Mcp-Session-Id` header.

Preconditions:

- telemetry is serving, started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment.
- `/var/lib/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed but the trail.
- telemetry wrote nothing to stderr.

## A client speaking an earlier revision lists the tools

A client on an earlier revision is offered the same four tools, with the same names, descriptions, schemas, and annotations, and its calls get the same tool results, `isError` results included, as on `2026-07-28`. Only the envelope differs: results carry no `resultType`, `_meta`, `ttlMs`, or `cacheScope`, and a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200, not 400.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2025-11-25

{"jsonrpc":"2.0","id":2,"method":"tools/list"}
```

```
POST /mcp HTTP/1.1
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

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has exactly one member, `tools`, the array of `An MCP client lists telemetry's tools`, member for member.

Preconditions:

- telemetry is serving.

Postconditions:

- Nothing has changed but the trail.
- telemetry wrote nothing to stderr.

## A browser opens /mcp

`/mcp` is for MCP clients, which only POST. A GET, from a browser or a client hoping for a stream, is refused with an empty body: telemetry offers no stream and no page at this address, and its landing page is at `/` (`S03`). A DELETE, from a client ending a session it believes it has, is refused the same way, since there are no sessions, and so is any other method that is not POST.

Request:

```
GET /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
DELETE /mcp HTTP/1.1
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

- telemetry is serving.

Postconditions:

- Nothing has changed but the trail.
- telemetry wrote nothing to stderr.

## A request to /mcp arrives without the identity headers

`/mcp` follows the rule every route but `/ingest` follows: the gate sets `X-User-Id` on every request it forwards and the gateway forwards the one it received, so a request without it says the gate or the gateway is misconfigured, a server fault answered 500. The identity check runs before anything about MCP is looked at, so the answer is the same plain text every route gives, not a JSON-RPC response, and no tool runs, whatever the body asked for.

Request:

```
POST /mcp HTTP/1.1
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: trace

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"trace","arguments":{"request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the line `identity header missing`, ending with LF.

Preconditions:

- telemetry is serving.
- The request carries no `X-User-Id` header and no `X-Request-Id` header.

Postconditions:

- Nothing has changed but the trail. No record of the trail was read.
- telemetry wrote nothing to stderr about the 500. Its trail records the request as it records every request but an ingest (`S02`), with an empty user, under the id telemetry gave the request; no tool ran, so there is no `tool.called`:

  ```
  request.started method=POST path=/mcp
  request.finished status=500
  ```

## A model calls a tool telemetry does not have

telemetry has four tools. A call naming any other — a tool to delete records, say, which telemetry does not offer — is not a tool's refusal but a protocol error: there is no tool to answer it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: delete_records

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"delete_records","arguments":{"request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
```

Status 400. The body is a JSON-RPC response with `id` 5 and no `result`, whose `error` has `code` `-32602` and `message` `Unknown tool: delete_records`.

Preconditions:

- telemetry is serving.

Postconditions:

- Nothing has changed but the trail. Every record the trail held before is as it was.
- telemetry wrote nothing to stderr. No tool ran, so the trail holds no `tool.called` for the request, only its two events, under user `u_7f3a9c21` and a request id telemetry made up for it:

  ```
  request.started method=POST path=/mcp
  request.finished status=400
  ```

## A model calls a tool while telemetry cannot read its trail

Every tool answers from the trail, so a tool that cannot read it has nothing true to say. It does not answer as if the trail were empty, which would tell the model nothing happened; it refuses, in the same words whichever of the four tools was called, and quotes nothing of the database's own error. The model can tell this from a refusal of its arguments and may try again later. telemetry keeps serving.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: catalog

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"catalog","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
cannot read the trail
```

A `search`, `count` or `trace` call whose arguments the tool would otherwise answer is refused with the same text. A call the tool refuses whatever the trail holds — arguments refused as they are read against the input schema, a time that is not RFC 3339, a `limit` out of range, a cursor no search issued, a `by` it does not know — is refused as its own group says.

Preconditions:

- telemetry is serving on the socket it was passed.
- telemetry's database cannot be read: `state/telemetry.db` has become unreadable since telemetry opened it, its storage failing reads, say.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's records of the call, its `tool.called` with `outcome=error` among them, under user `u_7f3a9c21` and request id `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`. When the database cannot be written either, those records go to stderr as `undelivered event` lines instead (`S12`), and telemetry writes nothing else to stderr.
- telemetry is still serving.

## The mcp gateway calls telemetry over its socket

The platform's MCP gateway offers the tools of every service whose entry in the services file is marked for MCP, and telemetry's is. A model asks the gateway to `call` the service `telemetry` and the tool it wants, and the gateway calls telemetry directly on its socket, not through nginx, on behalf of the caller it is serving: it speaks `2026-07-28` and forwards that caller's `X-User-Id`, `X-User-Email`, and `X-Request-Id`, as any sibling does (`S02`). telemetry answers the gateway exactly as it answers a client through nginx; it cannot tell the two apart and does not try. Here a developer on the host, as the `ikigenba` user, stands in for the gateway; what the gateway does with the answer is the gateway's, told in its own stories.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
Accept: application/json, text/event-stream
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: catalog

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"catalog","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member and whose `structuredContent` is an object whose one member, `services`, is an array of every service in the trail, in name order, each with its events, as `S08` tells, with one text content block holding the same object encoded compactly.

Preconditions:

- telemetry is deployed and active on the host, serving on `/run/ikigenba/telemetry.sock`.
- The host's services file lists telemetry with `"mcp": true` and the socket `/run/ikigenba/telemetry.sock`.
- The caller runs as the `ikigenba` user, which can reach the socket.
- The trail holds no record whose request id is `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`.

Postconditions:

- Nothing has changed but the trail.
- telemetry wrote nothing to stderr.
- The trail holds telemetry's three events for the call, under the id and the user the developer sent as the gateway would forward them, and nothing else under that id, since no gateway made the call; a `trace` of `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` answers those three, in this order (`S11`, `S12`):

  ```
  request.started method=POST path=/mcp
  tool.called tool=catalog kind=read outcome=ok
  request.finished status=200
  ```
