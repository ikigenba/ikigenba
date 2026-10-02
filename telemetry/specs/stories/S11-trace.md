# Stories — trace

`trace`, the tool that tells one request's story: every record in the trail that carries a request id, from every service the request touched, oldest first. Its one argument, `request_id`, is required: the id as a string, matched exactly as given, in whatever shape it has, the 32 lowercase hexadecimal digits nginx and the suite's services use (dummy's `S2`) or anything else a service recorded; the empty string answers every record that carries no request id. The result is `{"records":[...]}`, each record exactly as it was ingested (`S06`), `{"time","service","event","request_id","user","attrs"}` in that order, ordered by `time` oldest first; records with the same `time` are in the order telemetry received them, so the sequence reads as the services lived it, auth's check before the gateway's forward before the backend's execution. The result has no page: a request's records are few, and all of them come. An id no retained record carries answers `{"records":[]}`, not an error, since an id the agent found in a log may be older than the retention window (`S07`) or belong to a request that reached no service. `request_id` left out is refused as every tool of the suite refuses a missing field, with `invalid arguments:` and `request_id: missing required field` (`S05`).

The actor, the request shape, the result envelope, the fixture, and the preconditions are those of `S08`: an agent reaches telemetry through the gateway's `call` with `service` `telemetry` and `tool` `trace`, under request id `e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c`, and the trail holds exactly the twenty-five fixture records `S08` lists. Every story is read-only and adds to the trail only telemetry's and the gateway's records of the call (`S08`, `S12`, mcp's `S08`), whose `tool.called` names `tool=trace`; telemetry writes nothing to stderr for any answer in this group.

## An agent traces a request across the services it touched

The gateway call of 2026-10-02 14:03 (mcp's `S08`): auth's check of the agent's token, then the gateway's request, inside it dummy's answer to `tools/list`, the gateway's first `sibling.called`, dummy's answer to `tools/call` with its `tool.called`, the gateway's second `sibling.called`, its own `tool.called`, and its `request.finished`. Thirteen records, three services, one id.

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

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"trace","args":{"request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is telemetry's answer: no `isError` member, a `structuredContent` of

```
{"records":[
  {"time":"2026-10-02T14:03:07.100000Z","service":"auth","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"method":"GET","path":"/check"}},
  {"time":"2026-10-02T14:03:07.104000Z","service":"auth","event":"check.allowed","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"credential":"token","host":"mcp.sbx.ikigenba.dev","method":"POST","outcome":"allowed","path":"/mcp","token":"tok_01J9Q4R8ZT6M3VXK2A7HB5NWCD"}},
  {"time":"2026-10-02T14:03:07.104500Z","service":"auth","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"","attrs":{"duration_us":4500,"status":200}},
  {"time":"2026-10-02T14:03:07.110000Z","service":"mcp","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}},
  {"time":"2026-10-02T14:03:07.112000Z","service":"dummy","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}},
  {"time":"2026-10-02T14:03:07.113000Z","service":"dummy","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":1000,"status":200}},
  {"time":"2026-10-02T14:03:07.113500Z","service":"mcp","event":"sibling.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":2000,"method":"POST","path":"/mcp","status":200,"target":"dummy"}},
  {"time":"2026-10-02T14:03:07.115000Z","service":"dummy","event":"request.started","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}},
  {"time":"2026-10-02T14:03:07.116000Z","service":"dummy","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":800,"kind":"read","outcome":"ok","tool":"list_widgets"}},
  {"time":"2026-10-02T14:03:07.116500Z","service":"dummy","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":1500,"status":200}},
  {"time":"2026-10-02T14:03:07.117000Z","service":"mcp","event":"sibling.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":2500,"method":"POST","path":"/mcp","status":200,"target":"dummy"}},
  {"time":"2026-10-02T14:03:07.117500Z","service":"mcp","event":"tool.called","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":7000,"kind":"read","outcome":"ok","tool":"call"}},
  {"time":"2026-10-02T14:03:07.118000Z","service":"mcp","event":"request.finished","request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59","user":"u_7f3a9c21","attrs":{"duration_us":8000,"status":200}}]}
```

and a `content` array of one text block whose text is that object encoded compactly. No record of another request is in it, and nothing of this call's own: the call runs under its own id.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`).

## An agent traces a call it made to telemetry

telemetry records its own requests like any service (`S12`), so a tool call an agent made earlier through the gateway is traceable: the gateway's records and telemetry's lie side by side under the id the gateway forwarded. Here the agent earlier read the catalog (`S08`) under `e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c`, and now traces that id under a new one.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: f0e1d2c3b4a5968778695a4b3c2d1e0f
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"trace","args":{"request_id":"e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[
  {"time":"<time>","service":"mcp","event":"request.started","request_id":"e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}},
  {"time":"<time>","service":"telemetry","event":"request.started","request_id":"e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}},
  {"time":"<time>","service":"telemetry","event":"request.finished","request_id":"e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"status":200}},
  {"time":"<time>","service":"mcp","event":"sibling.called","request_id":"e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"method":"POST","path":"/mcp","status":200,"target":"telemetry"}},
  {"time":"<time>","service":"telemetry","event":"request.started","request_id":"e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}},
  {"time":"<time>","service":"telemetry","event":"tool.called","request_id":"e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"catalog"}},
  {"time":"<time>","service":"telemetry","event":"request.finished","request_id":"e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"status":200}},
  {"time":"<time>","service":"mcp","event":"sibling.called","request_id":"e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"method":"POST","path":"/mcp","status":200,"target":"telemetry"}},
  {"time":"<time>","service":"mcp","event":"tool.called","request_id":"e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"kind":"read","outcome":"ok","tool":"call"}},
  {"time":"<time>","service":"mcp","event":"request.finished","request_id":"e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c","user":"u_7f3a9c21","attrs":{"duration_us":<n>,"status":200}}]}
```

and a `content` array of one text block whose text is that object encoded compactly. Each `<time>` is the record's time as stored, ascending down the list; each `<n>` a whole number of microseconds. The first telemetry pair is its answer to the gateway's `tools/list`, the second its answer to the `tools/call`, with the `tool.called` between. The order shown is one the trail can hold; what is fixed is each service's own order, mcp's `request.started` first and `request.finished` last, as `S12` tells, and the third and fourth records, like the seventh and eighth, may be the other way round, since each service stamps its own time.

Preconditions:

- `S08`'s, and the agent has already made the call of `An agent reads the whole catalog` (`S08`) under request id `e4b1c7d9a2f3485e9c0d1b2a3f4e5d6c`, the only call under that id, which added mcp's records of it (mcp's `S08`) and telemetry's own (`S12`) to the trail.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`), under `f0e1d2c3b4a5968778695a4b3c2d1e0f`.

## An agent traces a request whose records share a time

Two records of one request can carry the same microsecond. They keep the order telemetry received them in, which is the order the service recorded them, so a `tool.called` stays before the `request.finished` that followed it; `search` (`S09`) answers the same two the other way round.

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

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"trace","args":{"request_id":"b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[
  {"time":"2026-10-02T14:20:31.400000Z","service":"dummy","event":"request.started","request_id":"b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}},
  {"time":"2026-10-02T14:20:31.400900Z","service":"dummy","event":"tool.called","request_id":"b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3","user":"u_7f3a9c21","attrs":{"duration_us":700,"kind":"read","outcome":"ok","tool":"list_widgets"}},
  {"time":"2026-10-02T14:20:31.400900Z","service":"dummy","event":"request.finished","request_id":"b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3","user":"u_7f3a9c21","attrs":{"duration_us":900,"status":200}}]}
```

and a `content` array of one text block whose text is that object encoded compactly.

Preconditions:

- `S08`'s, and the trail also holds those three records of dummy's, ingested in that order (`S06`).

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`).

## An agent traces a request id no record carries

An id from a log line, a header, or a guess that matches nothing: an empty trace, so the agent can tell "never recorded" from "failed to ask".

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

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"trace","args":{"request_id":"d41d8cd98f00b204e9800998ecf8427e"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`).

## An agent leaves out the request id

`request_id` is the one argument and it is required; without it there is nothing to trace, and the arguments are refused as they are read, before the tool runs.

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

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"trace","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
request_id: missing required field
```

Preconditions:

- `S08`'s.

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`), whose `tool.called` has `outcome=invalid_arguments` and `duration_us` 0, since the tool never ran.

## An agent traces a request the sweep has removed

A request older than the retention window is gone from the trail once the sweep has run (`S07`), and a trace of it is empty: the same answer as for an id that never existed, since telemetry keeps no memory of what it removed.

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

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"call","arguments":{"service":"telemetry","tool":"trace","args":{"request_id":"3f9c2a7be1d04c6a8b5e0f1d2c3b4a59"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has no `isError` member, a `structuredContent` of

```
{"records":[]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- `S08`'s, except that it is 2026-10-20: every fixture record is more than 15 days old, `RETENTION_DAYS` is unset, and the sweep has run since the newest of them aged past the window (`S07`).

Postconditions:

- Nothing has changed but the trail, which gained only telemetry's and the gateway's records of the call (`S08`). The fixture records were removed by the sweep, not by this call.
