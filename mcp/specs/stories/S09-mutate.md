# Stories — mutate

`mutate`, the gateway tool that runs a write tool of a service and returns what that tool answered. It is `call` (`S08`) for the other kind of tool: the same arguments, `service` and `tool`, required, and `args`, optional, `{}` when left out and passed to the backend untouched; the same hop to the backend, the same 50-second budget, the same cancellation, and the same line on stderr for every backend request (`S08`); the same checks in the same order; and the backend's result relayed verbatim, `isError` included, under the gateway's own envelope (`S05`). Only a tool whose kind is `write` runs here (`S07`); a read tool runs with `call`. A client is told that `mutate` is destructive (`S05`), so it can ask its user before running one, whatever the tool does.

The actor is a model working through an MCP client. Each request is the HTTP request the client sends to a running mcp (`S02`), on revision `2026-07-28`, with the headers and `_meta` `S05` fixes and `Mcp-Name: mutate`; every answer also carries the envelope members `S05` fixes. Every request carries the caller's `X-User-Id`, `X-User-Email` and `X-Request-Id` by hand. `IKIGENBA_SERVICES` names `/var/lib/ikigenba/services.json`, which holds the suite's services file (`S05`) unless a story says otherwise. dummy is the backend these stories reach, serving on `/run/ikigenba/dummy.sock` with its two tools and its fixture widgets `alpha`, `beta`, and `gamma` exactly as dummy's `S9` defines them. Stories about a backend that misbehaves use the hypothetical backend `reports` of `S07`, added to the file after `notes`, offering here two tools in this order: `build_report`, the read tool of `S07`, and `archive_report`, a write tool, annotated `readOnlyHint` `false` and `destructiveHint` `true`, that takes a report's `name`.

## A model creates a widget through the gateway

The ordinary case: a model runs a write tool it has described, and gets the backend's answer as if it had called the backend itself. The change is the backend's, made for the caller the gateway serves (`S08`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"dummy","tool":"create_widget","args":{"name":"delta","count":7,"status":"active"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` is dummy's answer to `create_widget` (dummy's `S9`): no `isError` member, a `structuredContent` of

```
{"name":"delta","count":7,"status":"active"}
```

and a `content` array of one text block whose text is exactly that line. Its `_meta` names the gateway, `{"name":"mcp","version":"v<semver>"}`, not dummy.

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`, its widgets the fixture set as it started, so no widget is named `delta`.

Postconditions:

- dummy holds a widget named `delta`, count 7, status `active`, last, after `gamma`; a `call` of `list_widgets` (`S08`) and dummy's own panel show it from the next request on.
- mcp wrote two lines to stderr, in this order:

  ```
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: ok
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/call create_widget: ok
  ```

## A model mutates with a read tool

A model that runs a read tool with `mutate` is told to use `call`, so the client's user is not asked to approve, as a change, something that changes nothing. The tool is not run.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"dummy","tool":"list_widgets"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Tool list_widgets of service dummy is a read tool. Use call to run it.
```

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`.

Postconditions:

- Nothing has changed. dummy received one `tools/list` and no `tools/call`.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: ok`.

## A backend refuses the change a model asked for

The gateway does not check `args`; the backend holds them to its own rules, and its refusal is its answer, relayed verbatim, so the model learns every offence in the backend's own words and can fix them in one retry. Here the name is taken and the count negative, and dummy names both (dummy's `S9`).

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"dummy","tool":"create_widget","args":{"name":"alpha","count":-4,"status":"retired"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` is dummy's: `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
name: that name is already taken
count: the count cannot be negative
```

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`, its widgets the fixture set as it started, so a widget named `alpha` exists.

Postconditions:

- No widget was created. dummy's widgets are unchanged; `alpha` still has count 3 and status `active`.
- mcp wrote two lines to stderr, in this order:

  ```
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: ok
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/call create_widget: tool error
  ```

## A model mutates and the tool does not finish in time

The backend lists its tools, but the write tool has not answered when the call's 50 seconds run out (`S08`). The change may have been made and only the answer been slow, so the model is told the call may have completed rather than invited to retry: running a write tool twice may make the change twice.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"reports","tool":"archive_report","args":{"name":"q3"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, 50 seconds after the request arrived. The body is a JSON-RPC response with `id` 4 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service reports did not answer within 50 s; the call may still have completed.
```

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- `reports` is serving on `/run/ikigenba/reports.sock`; it answers `tools/list` at once, offering its two tools, but does not answer a `tools/call` of `archive_report` within 50 seconds.

Postconditions:

- Nothing has changed in mcp. The gateway abandoned its `tools/call` to `reports`; whether `reports` archived `q3` is not known to the gateway.
- mcp wrote two lines to stderr, in this order:

  ```
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: ok
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/call archive_report: timed out
  ```

## A model mutates through a service the gateway does not know

As for `call` (`S08`), a name that `services` would not list on this connection is refused before any backend is asked.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"weather","tool":"set_alert","args":{"city":"Oslo"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 5 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Unknown service: weather. Call services to see the services you can use.
```

Preconditions:

- mcp is serving, with the suite's services file, which has no entry named `weather`.

Postconditions:

- Nothing has changed. No backend was contacted.
- mcp wrote nothing to stderr.

## A model mutates through a service that is disabled

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"notes","tool":"add_note","args":{"text":"buy milk"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service notes is unavailable: disabled. Do not retry; call services to see the services you can use.
```

Preconditions:

- mcp is serving, with the suite's services file, whose `notes` entry has `"enabled": false`.

Postconditions:

- Nothing has changed. Nothing was sent to `/run/ikigenba/notes.sock`.
- mcp wrote nothing to stderr.

## A model mutates through a scoped service that is not installed

The scope names `weather`, which is in no entry of the file, so `services` lists it as unavailable, reason `not installed` (`S05`).

Request:

```
POST /mcp/dummy,weather HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"weather","tool":"set_alert","args":{"city":"Oslo"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service weather is unavailable: not installed. Do not retry; call services to see the services you can use.
```

Preconditions:

- mcp is serving, with the suite's services file, which has no entry named `weather`.

Postconditions:

- Nothing has changed. No backend was contacted.
- mcp wrote nothing to stderr.

## A model mutates through a service whose backend is not running

Nothing answers on the service's socket, so nothing was changed, and the model is told to try again later.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"dummy","tool":"create_widget","args":{"name":"delta","count":7,"status":"active"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 8 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service dummy could not be reached. Retry later.
```

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is not serving: nothing accepts connections on `/run/ikigenba/dummy.sock`.

Postconditions:

- Nothing has changed.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: unreachable`.

## A model mutates with a tool the service does not have

dummy offers no tool to remove a widget (dummy's `S9`), so a model that tries is told how to see the tools there are.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"dummy","tool":"delete_widget","args":{"name":"alpha"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service dummy has no tool delete_widget. Call describe with service dummy to see its tools.
```

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`, with its two tools and no other.

Postconditions:

- Nothing has changed. dummy received one `tools/list` and no `tools/call`; `alpha` still exists.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: ok`.

## A model mutates through a backend that does not list its tools in time

The backend has not answered its `tools/list` when the call's 50 seconds run out. No tool has run, so the model is told to try again later.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"reports","tool":"archive_report","args":{"name":"q3"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, 50 seconds after the request arrived. The body is a JSON-RPC response with `id` 10 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service reports did not answer within 50 s. Retry later.
```

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- `reports` accepts connections on `/run/ikigenba/reports.sock` but does not answer a `tools/list` within 50 seconds.

Postconditions:

- Nothing has changed. `reports` was sent no `tools/call`.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: timed out`.

## A model mutates and the backend answers with an error

The backend answers the `tools/call` with a JSON-RPC error; the gateway passes its message on unaltered, after naming the service, as for `call` (`S08`). A JSON-RPC error in answer to the `tools/list` is answered with the same text, as in `S07`, and its line names `tools/list`. Here `reports` answers with `code` `-32603` and `message` `report store is offline`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"reports","tool":"archive_report","args":{"name":"q3"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 11 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service reports answered with an error: report store is offline
```

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- `reports` is serving on `/run/ikigenba/reports.sock`; it answers `tools/list` offering its two tools, and a `tools/call` of `archive_report` with a JSON-RPC error whose `code` is `-32603` and whose `message` is `report store is offline`.

Postconditions:

- Nothing has changed in mcp.
- mcp wrote two lines to stderr, in this order:

  ```
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: ok
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/call archive_report: rpc error -32603: report store is offline
  ```

## A model mutates and the backend does not answer as MCP

The backend answers the `tools/call` with something the gateway cannot read as MCP: here an HTTP `502` with a one-line plain-text body. The change may have been made before the answer went wrong, so the model is told the call may have completed. An unreadable answer to the `tools/list` is answered as in `S07`, `Service reports gave an answer the gateway could not read. Retry later.`, since no tool has run, and its line names `tools/list`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"reports","tool":"archive_report","args":{"name":"q3"}},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service reports gave an answer the gateway could not read; the call may still have completed.
```

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- `reports` is serving on `/run/ikigenba/reports.sock`; it answers `tools/list` offering its two tools, and a `tools/call` of `archive_report` with status `502`, `Content-Type: text/plain`, and a one-line body.

Postconditions:

- Nothing has changed in mcp; whether `reports` archived `q3` is not known to the gateway.
- mcp wrote two lines to stderr, in this order:

  ```
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: ok
  mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/call archive_report: bad response (status 502)
  ```

## A model mutates with arguments that are not an object

`args` must be a JSON object, as for `call` (`S08`); anything else is refused with the platform's wording (`S05`) before any backend is asked.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: mutate

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"mutate","arguments":{"service":"dummy","tool":"create_widget","args":"delta"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
args: expected object, got string
```

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`, its widgets the fixture set as it started.

Postconditions:

- Nothing has changed. dummy was not contacted; no widget named `delta` exists.
- mcp wrote nothing to stderr.
