# Stories — count

`count`, the tool that says how many records match a filter, and how they split. It takes the filters of `search` (`S09`), with the same meaning: `since` inclusive and `until` exclusive, `services` and `events` any-of, `user` and `request_id` exact, `attrs` every pair equal; all optional, ANDed, unbounded when left out. It takes one more, `by`, optional: how to group what matched. `by` is a field, `service`, `event`, `user`, or `request_id`; a time bucket, `minute`, `hour`, or `day`; or an attribute, `attrs.<key>`. Without `by` the result is `{"total":<n>}`. With `by` it is `{"total":<n>,"groups":[{"key":<value>,"count":<n>},...]}`, where `total` is the same number as without `by`. For a field, every matching record is in exactly one group, the group's `key` is the field's value as stored, the empty string included, and the groups are ordered by `count` descending, then `key` ascending. For an attribute, a record that carries the key is in the group of its value, as stored, a string, number, or boolean, and a record without the key is in `total` but in no group; the groups are ordered the same way. For a time bucket, `key` is the bucket's start, RFC 3339 UTC without a fraction, `2026-10-02T14:00:00Z` say, a record is in the bucket its `time` falls in, a bucket with no record is left out, and the buckets are oldest first, so the agent reads them as a timeline. Only retained records are counted (`S07`). A `by` that is none of these is refused with `by must be service, event, user, request_id, minute, hour, day, or attrs.<key>, got '<value>'`; a time the tool cannot read with `since is not an RFC 3339 time: '<value>'` or the same for `until`, as `search` refuses them. A filter that matches nothing is `{"total":0}`, with `"groups":[]` when `by` was given.

The actor, the request shape, the result envelope, the fixture, and the preconditions are those of `S08`: an agent reaches telemetry through the gateway's `call` with `service` `telemetry` and `tool` `count`, under request id `e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c`, and the trail holds exactly the twenty-five fixture records `S08` lists. Every story is read-only and adds to the trail only telemetry's own records of the call (`S08`, `S12`), whose `tool.called` names `tool=count`; telemetry writes nothing to stderr for any answer in this group.

## An agent counts the whole trail

How many records there are, bounded with `until` before the agent's own calls (`S08`) so the count is the fixture's; every story below that counts the whole fixture is bounded the same way.

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

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"count","args":{"until":"2026-10-02T14:30:00Z"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is telemetry's answer: no `isError` member, a `structuredContent` of

```
{"total":25}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but telemetry's own records of the call (`S08`).

## An agent counts by service

Which service is the noisiest: groups by `count` descending, then by name.

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

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"count","args":{"until":"2026-10-02T14:30:00Z","by":"service"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"total":25,"groups":[{"key":"dummy","count":10},{"key":"auth","count":9},{"key":"mcp","count":5},{"key":"telemetry","count":1}]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but telemetry's own records of the call (`S08`).

## An agent counts by event

Ties are broken by key: `request.finished` before `request.started` at eight each, and the three events with two records in name order.

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

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"count","args":{"until":"2026-10-02T14:30:00Z","by":"event"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"total":25,"groups":[{"key":"request.finished","count":8},{"key":"request.started","count":8},{"key":"check.allowed","count":3},{"key":"service.started","count":2},{"key":"sibling.called","count":2},{"key":"tool.called","count":2}]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but telemetry's own records of the call (`S08`).

## An agent counts by user

The two `service.started` records carry no user, and neither do auth's six `request.started` and `request.finished` for its `/check` (`S08`); they are the group whose key is the empty string, so the groups still add up to `total`.

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

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"count","args":{"until":"2026-10-02T14:30:00Z","by":"user"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"total":25,"groups":[{"key":"u_7f3a9c21","count":11},{"key":"","count":8},{"key":"u_1e9b4d07","count":6}]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but telemetry's own records of the call (`S08`).

## An agent counts by request id

Which request made the most records: the gateway call, then the two panel visits at five each in id order, then the records with no request id.

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

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"count","args":{"until":"2026-10-02T14:30:00Z","by":"request_id"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has no `isError` member, a `structuredContent` of

```
{"total":25,"groups":[{"key":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","count":13},{"key":"9b2e4d6f8a1c3e5b7d9f0a2c4e6b8d1f","count":5},{"key":"c7d1e3f5a9b2c4d6e8f0a1b3c5d7e9f2","count":5},{"key":"","count":2}]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but telemetry's own records of the call (`S08`).

## An agent counts by an attribute

`attrs.outcome` is carried by `check.allowed` and `tool.called` only; the other twenty records are in `total` and in no group. An attribute whose values are numbers, `attrs.status`, has number keys.

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

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"count","args":{"until":"2026-10-02T14:30:00Z","by":"attrs.outcome"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"total":25,"groups":[{"key":"allowed","count":3},{"key":"ok","count":2}]}
```

and a `content` array of one text block whose text is exactly that line. With `"by":"attrs.status"` the `structuredContent` is

```
{"total":25,"groups":[{"key":200,"count":9},{"key":500,"count":1}]}
```

the nine being the seven `request.finished` answered 200 and the two `sibling.called`. With `"by":"attrs.widget"`, a key no record carries, it is `{"total":25,"groups":[]}`.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but telemetry's own records of the call (`S08`).

## An agent counts by hour

The timeline of the trail: one bucket per hour that has a record, oldest first, the quiet hours between left out.

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

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"count","args":{"until":"2026-10-02T14:30:00Z","by":"hour"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has no `isError` member, a `structuredContent` of

```
{"total":25,"groups":[{"key":"2026-09-30T08:00:00Z","count":2},{"key":"2026-09-30T09:00:00Z","count":5},{"key":"2026-10-02T13:00:00Z","count":5},{"key":"2026-10-02T14:00:00Z","count":13}]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but telemetry's own records of the call (`S08`).

## An agent counts by day and by minute

`day` buckets start at midnight UTC; `minute` buckets at the minute. The minute count here is narrowed to 2026-10-02 with `since`, the way an agent closes in on a spike.

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

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"count","args":{"until":"2026-10-02T14:30:00Z","by":"day"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"count","args":{"since":"2026-10-02T00:00:00Z","until":"2026-10-02T14:30:00Z","by":"minute"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, for each. The first body is a JSON-RPC response with `id` 8 whose `result` has no `isError` member, a `structuredContent` of

```
{"total":25,"groups":[{"key":"2026-09-30T00:00:00Z","count":7},{"key":"2026-10-02T00:00:00Z","count":18}]}
```

and a `content` array of one text block whose text is exactly that line. The second, `id` 9, has a `structuredContent` of

```
{"total":18,"groups":[{"key":"2026-10-02T13:41:00Z","count":5},{"key":"2026-10-02T14:03:00Z","count":13}]}
```

and the same kind of `content`.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but telemetry's own records of the two calls (`S08`).

## An agent narrows a count with filters

The filters are `search`'s and narrow the count the same way, with or without `by`: the requests dummy answered 500, and the records of one user by service, where auth's one is its `check.allowed`, since its `/check` request records name no user (`S08`).

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

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"count","args":{"services":["dummy"],"events":["request.finished"],"attrs":{"status":500}}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"count","args":{"user":"u_1e9b4d07","until":"2026-10-01T00:00:00Z","by":"service"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, for each. The first body is a JSON-RPC response with `id` 10 whose `result` has no `isError` member, a `structuredContent` of

```
{"total":1}
```

and a `content` array of one text block whose text is exactly that line. The second, `id` 11, has a `structuredContent` of

```
{"total":3,"groups":[{"key":"dummy","count":2},{"key":"auth","count":1}]}
```

and the same kind of `content`. With filters that match nothing, `{"services":["dummy"],"events":["check.allowed"]}` say, the answer is `{"total":0}`, or `{"total":0,"groups":[]}` when `by` is given.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but telemetry's own records of the two calls (`S08`).

## An agent groups by something the tool cannot group by

`by` is one of the seven names or `attrs.<key>`; anything else, a path or a bare attribute name without `attrs.`, is refused, naming what is allowed.

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

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"count","args":{"by":"path"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
by must be service, event, user, request_id, minute, hour, day, or attrs.<key>, got 'path'
```

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but telemetry's own records of the call (`S08`), whose `tool.called` has `outcome=error`.

## An agent gives a time the tool cannot read

The time filters are refused exactly as `search` refuses them (`S09`).

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

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"count","args":{"until":"2026-10-02 14:00","by":"hour"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
until is not an RFC 3339 time: '2026-10-02 14:00'
```

With `{"since":"yesterday"}` the text is `since is not an RFC 3339 time: 'yesterday'`.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but telemetry's own records of the call (`S08`), whose `tool.called` has `outcome=error`.
