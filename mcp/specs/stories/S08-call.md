# Stories — call

`call`, the gateway tool that runs a read tool of a service and returns what that tool answered. Its arguments are `service`, required, a service's name as `services` lists it; `tool`, required, a tool's name as `describe` lists it (`S07`); and `args`, optional, an object holding the tool's own arguments. `args` left out is `{}`. The gateway passes `args` to the backend untouched: it does not hold them to the tool's input schema, which is the backend's to enforce. Only a tool whose kind is `read` runs here (`S07`); a write tool runs with `mutate` (`S09`). When the backend answers the call, its result is the answer, relayed verbatim — its `content`, its `structuredContent`, its `isError`, every member it has — except for the envelope members the gateway's own server sets (`S05`): `resultType`, and `_meta`, whose `io.modelcontextprotocol/serverInfo` is the gateway's `{"name":"mcp","version":"v<semver>"}`, never the backend's; on the earlier revisions those are absent, as for every result.

The hop to a backend is the same for `describe`, `call`, and `mutate`. The gateway goes straight to the socket the service's entry in the services file names, never through the space's nginx, and speaks revision `2026-07-28`, carrying the caller it serves: the `X-User-Id`, `X-User-Email`, and `X-Request-Id` it received, each sent only when the caller's request had it. It asks the backend for its `tools/list` afresh on every gateway call, keeping no copy from one call to the next, and for `call` and `mutate` then sends one `tools/call`. One budget of 50 seconds, counted from the moment the gateway call arrives, covers every backend request that gateway call makes, so every answer comes before nginx's 60 seconds run out. When the client goes away before the answer, the backend request in flight is abandoned with it.

The checks run in this order, and the first that refuses ends the call: a service the connection does not reach; a service that is unavailable; the backend's `tools/list` and whatever goes wrong with it; a tool the backend does not have; a tool of the wrong kind; and last the backend's `tools/call` and whatever goes wrong with it. A refusal is answered status 200 with a result whose `isError` is `true`, no `structuredContent`, and one text block saying what the model should do next (`S05`). A refusal made before the backend is asked contacts no backend and writes nothing to stderr.

The gateway writes one line to stderr for every request it makes to a backend, when that request ends, whatever the outcome; this is its one exception to a healthy app's silence (`S02`). The line is `mcp: request <id>: <service> tools/list: <outcome>` for a `tools/list` and `mcp: request <id>: <service> tools/call <tool>: <outcome>` for a `tools/call`, where `<id>` is the caller's `X-Request-Id`, or `-` when it sent none. `<outcome>` is one of: `ok`, a `tools/list` answered with tools or a `tools/call` answered with a result that is not an error; `tool error`, a result whose `isError` is `true`; `rpc error <code>: <message>`, a JSON-RPC error, with any CR or LF in its message replaced by a space; `unreachable`, nothing accepted the connection; `timed out`, the budget ran out; `cancelled`, the client went away; and `bad response (status <n>)`, an answer that is not MCP, `<n>` the HTTP status received, written `bad response` alone when the connection broke before any status arrived.

The actor is a model working through an MCP client. Each request is the HTTP request the client sends to a running mcp (`S02`), on revision `2026-07-28`, with the headers and `_meta` `S05` fixes and `Mcp-Name: call`; every answer also carries the envelope members `S05` fixes. Every request carries the caller's `X-User-Id`, `X-User-Email` and `X-Request-Id` by hand unless a story says otherwise. `IKIGENBA_SERVICES` names `/var/lib/ikigenba/services.json`, which holds the suite's services file (`S05`) unless a story says otherwise. dummy is the backend these stories reach, serving on `/run/ikigenba/dummy.sock` with its two tools and its fixture widgets `alpha`, `beta`, and `gamma` exactly as dummy's `S9` defines them. Stories about a backend that misbehaves use the hypothetical backend `reports` of `S07`, added to the file after `notes`, whose one tool, `build_report`, is a read tool that takes no arguments.

## A model calls a read tool

The ordinary case: a model runs a read tool it has described, and gets the backend's answer as if it had called the backend itself.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call","arguments":{"service":"dummy","tool":"list_widgets","args":{}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is dummy's answer to `list_widgets` (dummy's `S9`): no `isError` member, a `structuredContent` of

```
{"widgets":[{"name":"alpha","count":3,"status":"active"},{"name":"beta","count":0,"status":"paused"},{"name":"gamma","count":12,"status":"retired"}]}
```

and a `content` array of one text block whose text is exactly that line. Its `_meta` names the gateway, `{"name":"mcp","version":"v<semver>"}`, not dummy.

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`, its widgets the fixture set as it started.

Postconditions:

- Nothing has changed. dummy received one `tools/list`, then one `tools/call` of `list_widgets` with the arguments `{}`.
- mcp wrote two lines to stderr, in this order:

  ```
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: ok
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/call list_widgets: ok
  ```

## A model calls a tool and leaves out its arguments

A tool that takes no arguments needs no `args`. Left out, `args` is `{}`, so this is the same call as the one above and gets the same answer.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call","arguments":{"service":"dummy","tool":"list_widgets"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` is the one of `A model calls a read tool`, member for member.

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`, its widgets the fixture set as it started.

Postconditions:

- Nothing has changed. The `tools/call` dummy received carried the arguments `{}`.
- mcp wrote the two lines of `A model calls a read tool` to stderr.

## A backend sees the caller the gateway serves

The gateway calls a backend on behalf of the person whose request it is serving, so the backend answers for that person, exactly as it would had the person's client called it through nginx. The backend gets the `X-User-Id`, `X-User-Email`, and `X-Request-Id` of the request the gateway received, so anything it writes about the request carries the same id as the gateway's lines.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"call","arguments":{"service":"dummy","tool":"list_widgets"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` is the one of `A model calls a read tool`.

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`.

Postconditions:

- Nothing has changed.
- dummy received both requests, the `tools/list` and the `tools/call`, on `/run/ikigenba/dummy.sock`, not through nginx, each a `2026-07-28` request carrying the headers

  ```
  X-User-Id: u_7f3a9c21
  X-User-Email: mg@example.com
  X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
  MCP-Protocol-Version: 2026-07-28
  ```

  as in dummy's `S9` story `The mcp gateway calls dummy over its socket`.
- mcp wrote the two lines of `A model calls a read tool` to stderr.

## A backend sees a caller that sent no request id

The gateway forwards what its caller sent and makes nothing up: a request that reached it without `X-Request-Id` reaches the backend without one too, and the gateway's lines about it carry `-` for the id.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"call","arguments":{"service":"dummy","tool":"list_widgets"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` is the one of `A model calls a read tool`.

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`.
- The request carries no `X-Request-Id` header.

Postconditions:

- Nothing has changed.
- dummy received both requests carrying `X-User-Id: u_7f3a9c21` and `X-User-Email: mg@example.com` and no `X-Request-Id` header.
- mcp wrote two lines to stderr, in this order:

  ```
  mcp: request -: dummy tools/list: ok
  mcp: request -: dummy tools/call list_widgets: ok
  ```

## A backend refuses the arguments a model passed

The gateway does not check `args`; the backend does, and its refusal is its answer, relayed verbatim like any other, so the model reads the backend's own words. `list_widgets` takes no arguments, so dummy refuses any it is sent as an unknown field (dummy's `S9`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"call","arguments":{"service":"dummy","tool":"list_widgets","args":{"x":1}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` is dummy's: `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
x: unknown field
```

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`.

Postconditions:

- Nothing has changed. dummy received the `tools/call` with the arguments `{"x":1}`, as the model sent them.
- mcp wrote two lines to stderr, in this order:

  ```
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: ok
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/call list_widgets: tool error
  ```

## A model calls a write tool

`call` is marked read-only, so a client may run it without asking its user first; it must never change anything. A write tool is refused before it runs, and the model is told which gateway tool does run it.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"call","arguments":{"service":"dummy","tool":"create_widget","args":{"name":"delta","count":7,"status":"active"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Tool create_widget of service dummy is a write tool. Use mutate to run it.
```

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`, its widgets the fixture set as it started.

Postconditions:

- Nothing has changed. dummy received one `tools/list` and no `tools/call`; no widget named `delta` exists.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: ok`.

## A model calls a tool of a service the gateway does not know

A name that `services` would not list on this connection is refused before any backend is asked, as for `describe` (`S07`): here a name in no entry of the file.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"call","arguments":{"service":"weather","tool":"forecast"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Unknown service: weather. Call services to see the services you can use.
```

Preconditions:

- mcp is serving, with the suite's services file, which has no entry named `weather`.

Postconditions:

- Nothing has changed. No backend was contacted.
- mcp wrote nothing to stderr.

## A model calls a tool of a service that is disabled

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"call","arguments":{"service":"notes","tool":"search_notes","args":{"query":"groceries"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service notes is unavailable: disabled. Do not retry; call services to see the services you can use.
```

Preconditions:

- mcp is serving, with the suite's services file, whose `notes` entry has `"enabled": false`.

Postconditions:

- Nothing has changed. Nothing was sent to `/run/ikigenba/notes.sock`.
- mcp wrote nothing to stderr.

## A model calls a tool of a scoped service that is not installed

The scope names `auth`, whose entry is not an MCP service, so `services` lists it as unavailable, reason `not installed` (`S05`), and nothing is sent to its socket, though it has one.

Request:

```
POST /mcp/auth,dummy HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"call","arguments":{"service":"auth","tool":"list_tokens"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service auth is unavailable: not installed. Do not retry; call services to see the services you can use.
```

Preconditions:

- mcp is serving, with the suite's services file, whose `auth` entry has `"mcp": false`.

Postconditions:

- Nothing has changed. Nothing was sent to `/run/ikigenba/auth.sock`.
- mcp wrote nothing to stderr.

## A model calls a tool of a service whose backend is not running

Nothing answers on the service's socket, so the gateway cannot even learn its tools, and the model is told to try again later.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"call","arguments":{"service":"dummy","tool":"list_widgets"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 10 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service dummy could not be reached. Retry later.
```

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is not serving: nothing accepts connections on `/run/ikigenba/dummy.sock`.

Postconditions:

- Nothing has changed.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: unreachable`. Had the backend stopped between the `tools/list` and the `tools/call`, the answer would be the same and the second line would end `tools/call list_widgets: unreachable`.

## A model calls a tool the service does not have

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"call","arguments":{"service":"dummy","tool":"count_widgets"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service dummy has no tool count_widgets. Call describe with service dummy to see its tools.
```

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`, with its two tools and no other.

Postconditions:

- Nothing has changed. dummy received one `tools/list` and no `tools/call`.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: ok`.

## A model calls a tool of a backend that does not list its tools in time

The backend accepts the connection but has not answered its `tools/list` when the call's 50 seconds run out. No tool has run, so the model is told to try again later.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"call","arguments":{"service":"reports","tool":"build_report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, 50 seconds after the request arrived. The body is a JSON-RPC response with `id` 12 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service reports did not answer within 50 s. Retry later.
```

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- `reports` accepts connections on `/run/ikigenba/reports.sock` but does not answer a `tools/list` within 50 seconds.

Postconditions:

- Nothing has changed. `reports` was sent no `tools/call`.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: timed out`.

## A model calls a tool that does not finish in time

The backend lists its tools, but the tool itself has not answered when the call's 50 seconds run out; the time the `tools/list` took counts against the same 50 seconds. The tool may have done its work and only been slow to say so, so the model is told the call may have completed rather than invited to retry.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"call","arguments":{"service":"reports","tool":"build_report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, 50 seconds after the request arrived. The body is a JSON-RPC response with `id` 13 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service reports did not answer within 50 s; the call may still have completed.
```

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- `reports` is serving on `/run/ikigenba/reports.sock`; it answers `tools/list` at once, offering `build_report`, but does not answer a `tools/call` of `build_report` within 50 seconds.

Postconditions:

- Nothing has changed in mcp. The gateway abandoned its `tools/call` to `reports`.
- mcp wrote two lines to stderr, in this order:

  ```
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: ok
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/call build_report: timed out
  ```

## A model calls a tool and the backend answers with an error

The backend answers the `tools/call` with a JSON-RPC error rather than a result. The gateway passes the backend's message on unaltered, after naming the service it came from. A JSON-RPC error in answer to the `tools/list` is answered with the same text, as in `S07`, and its line names `tools/list`. Here `reports` answers with `code` `-32603` and `message` `report store is offline`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"call","arguments":{"service":"reports","tool":"build_report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 14 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service reports answered with an error: report store is offline
```

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- `reports` is serving on `/run/ikigenba/reports.sock`; it answers `tools/list` offering `build_report`, and a `tools/call` of `build_report` with a JSON-RPC error whose `code` is `-32603` and whose `message` is `report store is offline`.

Postconditions:

- Nothing has changed in mcp.
- mcp wrote two lines to stderr, in this order:

  ```
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: ok
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/call build_report: rpc error -32603: report store is offline
  ```

## A model calls a tool and the backend does not answer as MCP

The backend lists its tools, but answers the `tools/call` with something the gateway cannot read as MCP: here an HTTP `502` with a one-line plain-text body and no JSON-RPC response. The tool may have run before the answer went wrong, so the model is told the call may have completed. An unreadable answer to the `tools/list` is answered as in `S07`, `Service reports gave an answer the gateway could not read. Retry later.`, since no tool has run, and its line names `tools/list`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"call","arguments":{"service":"reports","tool":"build_report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 15 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service reports gave an answer the gateway could not read; the call may still have completed.
```

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- `reports` is serving on `/run/ikigenba/reports.sock`; it answers `tools/list` offering `build_report`, and a `tools/call` of `build_report` with status `502`, `Content-Type: text/plain`, and a one-line body.

Postconditions:

- Nothing has changed in mcp.
- mcp wrote two lines to stderr, in this order:

  ```
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: ok
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/call build_report: bad response (status 502)
  ```

## A model passes arguments that are not an object

A tool's arguments are a JSON object, so `args` must be one. Anything else is refused with the platform's wording (`S05`) before anything else is looked at, and no backend is asked.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"call","arguments":{"service":"dummy","tool":"list_widgets","args":"all"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 16 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
args: expected object, got string
```

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`.

Postconditions:

- Nothing has changed. dummy was not contacted.
- mcp wrote nothing to stderr.

## A client gives up while the backend works

A client that stops waiting — its user cancelled, or it timed out on its own — closes its connection. The gateway abandons the backend request in flight at once rather than holding it open for an answer no one will read.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: call

{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"call","arguments":{"service":"reports","tool":"build_report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

None: the client closed its connection 10 seconds after sending the request, before any answer was written.

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- `reports` is serving on `/run/ikigenba/reports.sock`; it answers `tools/list` at once, offering `build_report`, and takes longer than 10 seconds to answer a `tools/call` of `build_report`.

Postconditions:

- Nothing has changed in mcp. When the client went away, the gateway abandoned its `tools/call` to `reports`, closing that connection, without waiting for the 50 seconds to run out.
- mcp wrote two lines to stderr, in this order, the second as soon as the client went away:

  ```
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: ok
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/call build_report: cancelled
  ```
