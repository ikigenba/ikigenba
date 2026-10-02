# Stories — search

`search`, the tool that lists the records matching a filter, newest first, a page at a time. Its arguments are all optional. The filters: `since`, an RFC 3339 time, keeps records whose `time` is at or after it; `until`, an RFC 3339 time, keeps records whose `time` is before it; `services`, an array of service names, keeps records of any of them; `events`, an array of event names, keeps records of any of them; `user`, a string, keeps records whose `user` is exactly it; `request_id`, a string, keeps records whose `request_id` is exactly it; `attrs`, an object whose values are strings, numbers, or booleans, keeps records that carry every one of its keys with exactly that value, a number matching a number and a string a string. A filter left out is unbounded; the filters given are ANDed. The page: `limit`, a whole number from 1 to 500, 50 when left out, is the most records one answer holds; `cursor`, the value a previous answer gave, continues from where that answer stopped, with the same filters. A time is compared as an instant, so `since` written in another offset means the same thing as in UTC, and a `since` that is not before `until` matches nothing. Every record is answered exactly as it was ingested (`S06`): `{"time","service","event","request_id","user","attrs"}`, members in that order, `attrs` sorted by key. Records are newest first; records with the same `time` are in the reverse of the order telemetry received them, so a search reads the trail backwards exactly. Only retained records are found (`S07`).

The result is `{"records":[...]}` and, when more records match than the page holds, a second member, `"cursor":"<cursor>"`, an opaque string; passing it back as `cursor`, with the same filters, answers the next page, and the last page has no `cursor` member. A page carries `limit` records, or fewer only on the last page. A refusal the tool makes is an `isError` result with one text block: `since is not an RFC 3339 time: '<value>'` and `until is not an RFC 3339 time: '<value>'` for a time the tool cannot read; `limit must be between 1 and 500, got <n>` for a limit outside the range; `cursor is not one search issued` for a cursor that no search answered. An empty `cursor` is the same as leaving it out: the first page. A filter that matches nothing is an empty page, `{"records":[]}`, never an error.

The actor, the request shape, the result envelope, the fixture, and the preconditions are those of `S08`: an agent reaches telemetry through the gateway's `call` with `service` `telemetry` and `tool` `search`, under request id `e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c`, and the trail holds exactly the twenty-five fixture records `S08` lists, which these stories name by their request ids: `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`, the gateway call of 2026-10-02 14:03 (thirteen records, auth's three first, mcp's `request.finished` last); `9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f`, the panel visit of 2026-10-02 13:41 (five); `c7d1e3f5a9b2c4d6e8f0a1b3c5d7e9f2`, the panel visit of 2026-09-30 (five); and the two `service.started` records of 2026-09-30, which carry no request id. Every story is read-only and adds to the trail only telemetry's and the gateway's records of the call (`S08`, `S12`, mcp's `S08`), whose `tool.called` names `tool=search`; telemetry writes nothing to stderr for any answer in this group.

## An agent searches by request id

The usual first move after a `trace` (`S11`) is unnecessary: the same records, newest first, with a page size the agent chooses. Here the panel visit of 2026-10-02.

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

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"request_id":"9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is telemetry's answer: no `isError` member, a `structuredContent` of

```
{"records":[
  {"time":"2026-10-02T13:41:52.214000Z","service":"dummy","event":"request.finished","request_id":"9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f","user":"u_1e9b4d07","attrs":{"duration_us":4000,"status":200}},
  {"time":"2026-10-02T13:41:52.210000Z","service":"dummy","event":"request.started","request_id":"9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f","user":"u_1e9b4d07","attrs":{"method":"GET","path":"/widgets"}},
  {"time":"2026-10-02T13:41:52.203500Z","service":"auth","event":"request.finished","request_id":"9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f","user":"","attrs":{"duration_us":3500,"status":200}},
  {"time":"2026-10-02T13:41:52.203000Z","service":"auth","event":"check.allowed","request_id":"9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f","user":"u_1e9b4d07","attrs":{"credential":"session","host":"dummy.sbx.ikigenba.dev","method":"GET","outcome":"allowed","path":"/widgets"}},
  {"time":"2026-10-02T13:41:52.200000Z","service":"auth","event":"request.started","request_id":"9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f","user":"","attrs":{"method":"GET","path":"/check"}}]}
```

and a `content` array of one text block whose text is that object encoded compactly. There is no `cursor`: five records match and the page holds fifty.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`).

## An agent searches by user

Everything one user did, across services and days, newest first: both panel visits of `u_1e9b4d07`. auth's `request.started` and `request.finished` for its `/check` name no user (`S08`), so only `check.allowed` of auth's three is the user's.

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

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"user":"u_1e9b4d07"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member and a `structuredContent` of `{"records":[...]}` holding six records and no `cursor`: of `9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f`, dummy's `request.finished` and `request.started` and auth's `check.allowed`, exactly as `An agent searches by request id` answers them, with auth's `request.finished` and `request.started` left out; then of `c7d1e3f5a9b2c4d6e8f0a1b3c5d7e9f2` the same three, newest first, `2026-09-30T09:12:44.530000Z` dummy `request.finished`, `2026-09-30T09:12:44.510000Z` dummy `request.started`, and `2026-09-30T09:12:44.503000Z` auth `check.allowed`, each record as `S08` lists it. The `content` array is one text block whose text is that object encoded compactly.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`).

## An agent searches by services and events

`services` and `events` each take several names and match any of them; together they match records of any named service carrying any named event. Here the gateway's calls to backends, bounded with `until` so the gateway's own records of this call, which it posts under `e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c` as it goes (mcp's `S08`), stay out of the answer.

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

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"services":["mcp"],"events":["sibling.called","tool.called"],"until":"2026-10-02T14:30:00Z"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[
  {"time":"2026-10-02T14:03:07.117500Z","service":"mcp","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":7000,"kind":"read","outcome":"ok","tool":"call"}},
  {"time":"2026-10-02T14:03:07.117000Z","service":"mcp","event":"sibling.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":2500,"method":"POST","path":"/mcp","status":200,"target":"dummy"}},
  {"time":"2026-10-02T14:03:07.113500Z","service":"mcp","event":"sibling.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":2000,"method":"POST","path":"/mcp","status":200,"target":"dummy"}}]}
```

and a `content` array of one text block whose text is that object encoded compactly. dummy's `tool.called` is not in it: dummy is not in `services`.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`).

## An agent searches by an attribute's value

`attrs` finds the records whose attributes hold the given keys with the given values, whatever service or event recorded them: the one request answered 500. A number is matched as a number, so `{"status":"500"}` would match nothing.

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

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"attrs":{"status":500}}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[{"time":"2026-09-30T09:12:44.530000Z","service":"dummy","event":"request.finished","request_id":"c7d1e3f5a9b2c4d6e8f0a1b3c5d7e9f2","user":"u_1e9b4d07","attrs":{"duration_us":20000,"status":500}}]}
```

and a `content` array of one text block whose text is exactly that line. With `{"status":500,"duration_us":4000}` nothing matches, since no one record carries both values, and the answer is `{"records":[]}`.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`).

## An agent searches a time range

`since` is inclusive and `until` exclusive, so hour-wide ranges tile without overlap. The 13:00 hour of 2026-10-02 holds the panel visit and nothing else.

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

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"since":"2026-10-02T13:00:00Z","until":"2026-10-02T14:00:00Z"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member and a `structuredContent` of `{"records":[...]}` holding the five records of `9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f`, exactly as `An agent searches by request id` answers them, and no `cursor`; the `content` array is one text block whose text is that object encoded compactly. With `since` `2026-10-02T14:03:07.118000Z` and `until` `2026-10-02T14:30:00Z` the answer is the one record at that instant, mcp's `request.finished`; with `until` `2026-10-02T14:03:07.100000Z` alone, the twelve records before auth's `request.started` of 14:03.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`).

## An agent searches with no filter and gets the first page

No filter but an `until` before the agent's own calls (`S08`): the newest fifty records of the trail before it and a cursor, since more match. The trail here is bigger than the fixture.

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

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"until":"2026-10-02T14:30:00Z"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member and a `structuredContent` of `{"records":[...],"cursor":"<cursor>"}`: fifty records, newest first, the thirteen of `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59`, then the five of `9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f`, then the thirty-two newest of 2026-10-01, and `<cursor>`, an opaque string. The `content` array is one text block whose text is that object encoded compactly. Passing `{"cursor":"<cursor>"}` answers the next fifty, all of 2026-10-01, with another `cursor`; the third page holds the last twenty-five, the eighteen oldest of 2026-10-01 then the seven of 2026-09-30, and no `cursor`.

Preconditions:

- `S08`'s, except that the trail holds, besides the fixture, one hundred more records: dummy's `request.started` and `request.finished` for fifty panel visits of 2026-10-01, one a minute from `2026-10-01T10:00:00.000000Z`, each under a request id of its own and user `u_1e9b4d07`.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`).

## An agent pages through a search to its end

`limit` sets the page; the cursor carries the search on, with the same filters, here the `until` that keeps the agent's own calls out (`S08`). Three pages of ten cover the fixture: the third holds the five left and no cursor, which is how the agent knows it has everything.

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

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"until":"2026-10-02T14:30:00Z","limit":10}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

then, with the `<cursor>` the first answer gave,

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"until":"2026-10-02T14:30:00Z","limit":10,"cursor":"<cursor>"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

and once more with the `<cursor>` the second answer gave, as `id` 9.

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, three times. The first body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member and a `structuredContent` of `{"records":[...],"cursor":"<cursor>"}` holding the ten newest records: those of `3f9c2a7be1d04c6a8b5e0f1d2c3b4a59` from mcp's `request.finished` at `2026-10-02T14:03:07.118000Z` down to mcp's `request.started` at `2026-10-02T14:03:07.110000Z`. The second, `id` 8, holds the next ten with another `cursor`: auth's three of 14:03 newest first, the five of `9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f`, then dummy's `request.finished` and `request.started` of 2026-09-30. The third, `id` 9, has no `cursor` and a `structuredContent` of

```
{"records":[
  {"time":"2026-09-30T09:12:44.503500Z","service":"auth","event":"request.finished","request_id":"c7d1e3f5a9b2c4d6e8f0a1b3c5d7e9f2","user":"","attrs":{"duration_us":3500,"status":200}},
  {"time":"2026-09-30T09:12:44.503000Z","service":"auth","event":"check.allowed","request_id":"c7d1e3f5a9b2c4d6e8f0a1b3c5d7e9f2","user":"u_1e9b4d07","attrs":{"credential":"session","host":"dummy.sbx.ikigenba.dev","method":"GET","outcome":"allowed","path":"/widgets"}},
  {"time":"2026-09-30T09:12:44.500000Z","service":"auth","event":"request.started","request_id":"c7d1e3f5a9b2c4d6e8f0a1b3c5d7e9f2","user":"","attrs":{"method":"GET","path":"/check"}},
  {"time":"2026-09-30T08:00:02.000000Z","service":"dummy","event":"service.started","request_id":"","user":"","attrs":{"version":"v<semver>"}},
  {"time":"2026-09-30T08:00:00.000000Z","service":"telemetry","event":"service.started","request_id":"","user":"","attrs":{"version":"v<semver>"}}]}
```

Each `content` array is one text block whose text is its object encoded compactly. No record is on two pages and none is skipped: the three pages together are the fixture, newest first.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the three calls (`S08`).

## An agent asks for a page outside the range

`limit` is 1 to 500; 0 and 501 are refused alike, each with the number given.

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

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"limit":0}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
limit must be between 1 and 500, got 0
```

With `"limit":501` the text is `limit must be between 1 and 500, got 501`.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`), whose `tool.called` has `outcome=error`.

## An agent gives a time the tool cannot read

`since` and `until` are RFC 3339 or nothing; a date alone, a bare word, or a local time with no offset is refused, naming the argument and quoting the value.

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

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"since":"yesterday"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
since is not an RFC 3339 time: 'yesterday'
```

With `{"until":"2026-10-02 14:00"}` the text is `until is not an RFC 3339 time: '2026-10-02 14:00'`.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`), whose `tool.called` has `outcome=error`.

## An agent passes a cursor no search issued

A cursor is only what a `search` answer gave; a made-up one, or one damaged in transit, is refused rather than guessed at.

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

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"cursor":"page2"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
cursor is not one search issued
```

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`), whose `tool.called` has `outcome=error`.

## An agent gives a range that ends before it starts

`since` not before `until` is a range holding no instant. It is not an error: it matches nothing, as a range between two quiet hours would.

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

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"since":"2026-10-02T14:00:00Z","until":"2026-10-02T13:00:00Z"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[]}
```

and a `content` array of one text block whose text is exactly that line. `since` equal to `until` answers the same.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`).

## An agent's filters match nothing

A well-formed filter that no record satisfies is an empty page; the agent learns the trail has nothing of the kind, and can widen the filter or ask `catalog` (`S08`) what there is.

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

{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"search","args":{"services":["dummy"],"events":["check.allowed"]}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 14 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`).
