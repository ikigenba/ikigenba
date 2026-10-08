# Stories — catalog

`catalog`, the first of telemetry's four read tools (`S05`): what the trail holds, by name. It answers the services that have records in the trail, the events each of them has recorded, and for each event how many records it has, when its newest record is from, and which attribute keys its records carry. It is how an agent learns what it can filter on before it reaches for `search` (`S09`) or `count` (`S10`). Its arguments are `service`, optional, a service's name, and `event`, optional, an event's name; both are exact names, matched as given, and either narrows the answer to that name; an empty name is the same as leaving the argument out. Its result is `{"services":[...]}`: one entry per service, `{"service":"<name>","events":[...]}`, sorted by service name, each holding one entry per event, `{"event":"<name>","count":<n>,"last_seen":"<time>","attrs":["<key>",...]}`, sorted by event name. `count` is how many records of that service and event the trail holds; `last_seen` is the `time` of the newest of them, as stored; `attrs` is every attribute key that appears on any of them, each key once, sorted. Only retained records count: a record the sweep has removed (`S07`) is in no entry, and a service or event with no retained record has none. A filter that matches no record answers `{"services":[]}`, not an error.

The actor is an agent working through an MCP client, reaching telemetry the way agents do: through the MCP gateway's `call` (mcp's `S08`), with `service` `telemetry`, `tool` `catalog`, and the tool's own arguments as `args`. Each request is the HTTP request the client sends to a running mcp, on revision `2026-07-28`, with the headers and `_meta` that revision fixes and `Mcp-Name: call`, carrying the caller's `X-User-Id`, `X-User-Email`, and `X-Request-Id` by hand. The request id is `e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c`, an id of this call's own, not one the fixture holds. The gateway forwards those headers and the `tools/call` to telemetry on `/run/ikigenba/telemetry.sock` and relays telemetry's result verbatim, `isError` included, under its own `resultType` and `_meta`; a client that reaches telemetry's `/mcp` directly with the same `tools/call`, as a developer on the host can, gets the same `result` member for member, with telemetry's own `_meta` (`S05`). A successful result carries the answer twice: as `structuredContent`, a JSON object, and as one text content block whose text is that object encoded compactly, with no white space between its tokens. A refusal is status 200 with `isError` `true`, no `structuredContent`, and one text block saying why; arguments that do not fit the tool's input schema, an unknown field say, are refused as every tool of the suite refuses them, with `invalid arguments:` and one `<field>: <reason>` line per offence (`S05`). A response body below is laid out for reading: its white space is not fixed. The order of members within a result is fixed as shown; elsewhere the order of members is not. A response block shows the status line and the headers the story fixes; a header it does not show is not fixed.

The trail fixture is the same in this group and in `S09`, `S10`, and `S11`: twenty-five records, exactly these, in the order telemetry received them. They are the start of telemetry and dummy on 2026-09-30; a user's visit to dummy's panel that day under request id `c7d1e3f5a9b2c4d6e8f0a1b3c5d7e9f2`, which dummy answered 500; the same user's visit on 2026-10-02 under `9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f`; and an agent's call of dummy's `list_widgets` through the gateway with a token, under `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`, the request mcp's `S08` shows. Every attribute is as the recording service sent it (dummy's `S2`, auth's `S4`, mcp's `S08`). auth's `request.started` and `request.finished` for its `/check` carry no user: nginx's subrequest names none, and a request event carries the user its `X-User-Id` names, empty when there is none (auth's `S2`); only `check.allowed` names the user the check resolved to (auth's `S4`).

```
{"time":"2026-09-30T08:00:00.000000Z","service":"telemetry","event":"service.started","request_id":"","user":"","attrs":{"version":"c604e32"}}
{"time":"2026-09-30T08:00:02.000000Z","service":"dummy","event":"service.started","request_id":"","user":"","attrs":{"version":"c604e32"}}
{"time":"2026-09-30T09:12:44.500000Z","service":"auth","event":"request.started","request_id":"c7d1e3f5a9b2c4d6e8f0a1b3c5d7e9f2","user":"","attrs":{"method":"GET","path":"/check"}}
{"time":"2026-09-30T09:12:44.503000Z","service":"auth","event":"check.allowed","request_id":"c7d1e3f5a9b2c4d6e8f0a1b3c5d7e9f2","user":"u_1e9b4d07","attrs":{"credential":"session","host":"dummy.sbx.ikigenba.dev","method":"GET","outcome":"allowed","path":"/widgets"}}
{"time":"2026-09-30T09:12:44.503500Z","service":"auth","event":"request.finished","request_id":"c7d1e3f5a9b2c4d6e8f0a1b3c5d7e9f2","user":"","attrs":{"duration_us":3500,"status":200}}
{"time":"2026-09-30T09:12:44.510000Z","service":"dummy","event":"request.started","request_id":"c7d1e3f5a9b2c4d6e8f0a1b3c5d7e9f2","user":"u_1e9b4d07","attrs":{"method":"GET","path":"/widgets"}}
{"time":"2026-09-30T09:12:44.530000Z","service":"dummy","event":"request.finished","request_id":"c7d1e3f5a9b2c4d6e8f0a1b3c5d7e9f2","user":"u_1e9b4d07","attrs":{"duration_us":20000,"status":500}}
{"time":"2026-10-02T13:41:52.200000Z","service":"auth","event":"request.started","request_id":"9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f","user":"","attrs":{"method":"GET","path":"/check"}}
{"time":"2026-10-02T13:41:52.203000Z","service":"auth","event":"check.allowed","request_id":"9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f","user":"u_1e9b4d07","attrs":{"credential":"session","host":"dummy.sbx.ikigenba.dev","method":"GET","outcome":"allowed","path":"/widgets"}}
{"time":"2026-10-02T13:41:52.203500Z","service":"auth","event":"request.finished","request_id":"9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f","user":"","attrs":{"duration_us":3500,"status":200}}
{"time":"2026-10-02T13:41:52.210000Z","service":"dummy","event":"request.started","request_id":"9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f","user":"u_1e9b4d07","attrs":{"method":"GET","path":"/widgets"}}
{"time":"2026-10-02T13:41:52.214000Z","service":"dummy","event":"request.finished","request_id":"9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f","user":"u_1e9b4d07","attrs":{"duration_us":4000,"status":200}}
{"time":"2026-10-02T14:03:07.100000Z","service":"auth","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/check"}}
{"time":"2026-10-02T14:03:07.104000Z","service":"auth","event":"check.allowed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"credential":"token","host":"mcp.sbx.ikigenba.dev","method":"POST","outcome":"allowed","path":"/mcp","token":"tok_01J9Q4R8ZT6M3VXK2A7HB5NWCD"}}
{"time":"2026-10-02T14:03:07.104500Z","service":"auth","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":4500,"status":200}}
{"time":"2026-10-02T14:03:07.110000Z","service":"mcp","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
{"time":"2026-10-02T14:03:07.112000Z","service":"dummy","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
{"time":"2026-10-02T14:03:07.113000Z","service":"dummy","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":1000,"status":200}}
{"time":"2026-10-02T14:03:07.113500Z","service":"mcp","event":"sibling.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":2000,"method":"POST","path":"/mcp","status":200,"target":"dummy"}}
{"time":"2026-10-02T14:03:07.115000Z","service":"dummy","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
{"time":"2026-10-02T14:03:07.116000Z","service":"dummy","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":800,"kind":"read","outcome":"ok","tool":"list_widgets"}}
{"time":"2026-10-02T14:03:07.116500Z","service":"dummy","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":1500,"status":200}}
{"time":"2026-10-02T14:03:07.117000Z","service":"mcp","event":"sibling.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":2500,"method":"POST","path":"/mcp","status":200,"target":"dummy"}}
{"time":"2026-10-02T14:03:07.117500Z","service":"mcp","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":7000,"kind":"read","outcome":"ok","tool":"call"}}
{"time":"2026-10-02T14:03:07.118000Z","service":"mcp","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":8000,"status":200}}
```

Every story's preconditions are the same unless it says otherwise: mcp is serving, with the suite's services file, whose `telemetry` entry is marked for MCP and names the socket `/run/ikigenba/telemetry.sock`; telemetry is serving on that socket with `RETENTION_DAYS` unset, and its trail holds exactly the fixture. It is `2026-10-02T15:00:00Z` when the agent's first call is made, and every call in this group and in `S09` to `S11` is made after that, so every record the fixture holds is before that instant and every record the calls add is at or after it. Every story is read-only: nothing in the trail changes but the records telemetry keeps of its own requests (`S12`) and the gateway's records of the call (mcp's `S08`); telemetry's own are under the call's own request id and user: `request.started method=POST path=/mcp` and `request.finished status=200` for each of the gateway's two requests, and between the second pair `tool.called tool=catalog kind=read outcome=ok`, or `outcome=error` for a refusal the tool made, or `outcome=invalid_arguments` for arguments refused as they were read. The gateway adds its own records under the same id (mcp's `S08`). telemetry stores its own records off the request's path, shortly after each event, and a tool answers from what is in the store when it runs, so whether the records of the call in progress, and the gateway's records of the hop just before it, are already in the trail is not fixed (`S12`); a story whose answer could take them in bounds the call with `until` before `2026-10-02T15:00:00Z`, or says which of its entries are not fixed. telemetry writes nothing to stderr for any answer in this group.

## An agent reads the whole catalog

No arguments: the whole trail, by service and event. `check.allowed` carries `token` on one of its three records and not on the other two, and the key is listed once. `telemetry` lists itself, since its own records are in the trail like any other's: its `service.started` from the fixture, exactly as shown, and `request.started`, `request.finished`, and `tool.called` from the agent's own calls through the gateway, which the fixture does not hold, so their `count` and `last_seen` are not fixed; `<n>` is at least 1 and `<time>` is at or after `2026-10-02T15:00:00Z`. `mcp`'s entry is the same way: the gateway records `request.started`, two `sibling.called`, `tool.called`, and `request.finished` for every call the agent makes through it (mcp's `S08`), so each of its four events holds the fixture's records and the agent's own calls' as well, and their `count` and `last_seen` are not fixed either; `<n>` is more than the fixture's 1, 1, 2, and 1, and `<time>` is at or after `2026-10-02T15:00:00Z`. The attribute keys of both are fixed: the agent's calls add records of the same shape, except that their `request.finished` also carries `request_bytes` and `response_bytes` (`S02`), which the fixture's records predate, so both list those keys. Every other service's entry is exact.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"catalog","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is telemetry's answer: no `isError` member, a `structuredContent` of

```
{"services":[
  {"service":"auth","events":[
    {"event":"check.allowed","count":3,"last_seen":"2026-10-02T14:03:07.104000Z","attrs":["credential","host","method","outcome","path","token"]},
    {"event":"request.finished","count":3,"last_seen":"2026-10-02T14:03:07.104500Z","attrs":["duration_us","status"]},
    {"event":"request.started","count":3,"last_seen":"2026-10-02T14:03:07.100000Z","attrs":["method","path"]}]},
  {"service":"dummy","events":[
    {"event":"request.finished","count":4,"last_seen":"2026-10-02T14:03:07.116500Z","attrs":["duration_us","status"]},
    {"event":"request.started","count":4,"last_seen":"2026-10-02T14:03:07.115000Z","attrs":["method","path"]},
    {"event":"service.started","count":1,"last_seen":"2026-09-30T08:00:02.000000Z","attrs":["version"]},
    {"event":"tool.called","count":1,"last_seen":"2026-10-02T14:03:07.116000Z","attrs":["duration_us","kind","outcome","tool"]}]},
  {"service":"mcp","events":[
    {"event":"request.finished","count":<n>,"last_seen":"<time>","attrs":["duration_us","request_bytes","response_bytes","status"]},
    {"event":"request.started","count":<n>,"last_seen":"<time>","attrs":["method","path"]},
    {"event":"sibling.called","count":<n>,"last_seen":"<time>","attrs":["duration_us","method","path","status","target"]},
    {"event":"tool.called","count":<n>,"last_seen":"<time>","attrs":["duration_us","kind","outcome","tool"]}]},
  {"service":"telemetry","events":[
    {"event":"request.finished","count":<n>,"last_seen":"<time>","attrs":["duration_us","request_bytes","response_bytes","status"]},
    {"event":"request.started","count":<n>,"last_seen":"<time>","attrs":["method","path"]},
    {"event":"service.started","count":1,"last_seen":"2026-09-30T08:00:00.000000Z","attrs":["version"]},
    {"event":"tool.called","count":<n>,"last_seen":"<time>","attrs":["duration_us","kind","outcome","tool"]}]}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's, and the agent has already made at least one call of a telemetry tool through the gateway since `2026-10-02T15:00:00Z`, under a request id other than `e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c`, so telemetry's `request.started`, `request.finished`, and `tool.called` for it, and the gateway's five records of it, are in the trail.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (preamble).

## An agent reads the catalog of one service

`service` narrows the answer to that service's entry; the entry is as it would be in the whole catalog.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"catalog","args":{"service":"dummy"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"services":[
  {"service":"dummy","events":[
    {"event":"request.finished","count":4,"last_seen":"2026-10-02T14:03:07.116500Z","attrs":["duration_us","status"]},
    {"event":"request.started","count":4,"last_seen":"2026-10-02T14:03:07.115000Z","attrs":["method","path"]},
    {"event":"service.started","count":1,"last_seen":"2026-09-30T08:00:02.000000Z","attrs":["version"]},
    {"event":"tool.called","count":1,"last_seen":"2026-10-02T14:03:07.116000Z","attrs":["duration_us","kind","outcome","tool"]}]}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (preamble).

## An agent reads the catalog of one event across services

`event` keeps, in every service's entry, only that event; a service with no record of it has no entry. `service.started` is recorded once each by telemetry and dummy in the fixture and by nothing else, and a tool call through the gateway adds no `service.started` to any service (mcp's `S08`, `S12`), so the answer is exact: auth and mcp have no entry.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"catalog","args":{"event":"service.started"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"services":[
  {"service":"dummy","events":[
    {"event":"service.started","count":1,"last_seen":"2026-09-30T08:00:02.000000Z","attrs":["version"]}]},
  {"service":"telemetry","events":[
    {"event":"service.started","count":1,"last_seen":"2026-09-30T08:00:00.000000Z","attrs":["version"]}]}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (preamble).

## An agent reads the catalog of one event of one service

Both filters together: one service's entry holding one event. `check.allowed` is auth's alone, and the agent's calls through the gateway add none (auth is not on their path), so the answer is exact; `token`, carried by one of the three records, is listed once beside the keys all three carry.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"catalog","args":{"service":"auth","event":"check.allowed"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"services":[{"service":"auth","events":[{"event":"check.allowed","count":3,"last_seen":"2026-10-02T14:03:07.104000Z","attrs":["credential","host","method","outcome","path","token"]}]}]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (preamble).

## An agent reads the catalog of a service that has no records

A name nothing in the trail carries, a service never deployed or whose records the sweep has removed (`S07`), is an empty catalog, not an error: the agent learns there is nothing to search. An `event` nothing recorded answers the same way.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"catalog","args":{"service":"widgets"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"services":[]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- The preamble's.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (preamble).
