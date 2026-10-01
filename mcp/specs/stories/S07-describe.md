# Stories — describe

`describe`, the gateway tool a model calls to learn what a service can do before it runs anything there. Its arguments are `service`, required, a service's name as `services` lists it, and `tool`, optional, a tool's name as `describe` lists it. With `service` only, it answers `{"service":<name>,"tools":[...]}`: each of the backend's tools, in the backend's order, as `{"name":<name>,"summary":<summary>,"kind":<kind>}`, where the summary is the backend's description up to its first LF (the whole description when it has none). With `tool` too, it answers `{"service":<name>,"tool":{"name":...,"description":...,"kind":...,"inputSchema":...,"outputSchema":...}}`: the backend's full description and input schema, unaltered, its output schema only when the backend's tool has one, and the kind. A tool's kind is `read` when the backend annotates it `readOnlyHint` `true`, and `write` otherwise, a tool with no annotations included; a read tool runs with `call` (`S08`), a write tool with `mutate` (`S09`). The answer carries the object twice, as `structuredContent` and as one text block holding it compactly encoded, the convention every tool result of the gateway follows (`S05`).

`describe` holds nothing: every call asks the backend for its `tools/list` afresh, over the hop `S08` describes — straight to the socket the services file names, forwarding the caller — within the one 50-second budget of `S08`, and writes one line to stderr for that backend request in the format `S08` fixes. The checks run in this order: a service the connection does not reach, then a service that is unavailable, then the backend's `tools/list` and whatever goes wrong with it, then, when `tool` is given, a tool the backend does not have. A check that refuses ends the call: it is answered status 200 with a result whose `isError` is `true`, no `structuredContent`, and one text block saying what the model should do next. A refusal made before the backend is asked contacts no backend and writes nothing to stderr.

The actor is a model working through an MCP client. Each request is the HTTP request the client sends to a running mcp (`S02`), on revision `2026-07-28`, with the headers and `_meta` `S05` fixes and `Mcp-Name: describe`; every successful answer also carries the envelope members `S05` fixes, not repeated below. Every request carries the caller's `X-User-Id`, `X-User-Email` and `X-Request-Id` by hand. Unless a story says otherwise, `IKIGENBA_SERVICES` names `/var/lib/ikigenba/services.json`, which holds the suite's services file (`S05`): `dummy`, an available MCP service; `notes`, an MCP service that is disabled; and `auth` and `mcp`, which are not MCP services.

dummy is the backend these stories reach, serving on `/run/ikigenba/dummy.sock` with its two tools exactly as dummy's `S9` defines them: `list_widgets`, annotated read-only, and `create_widget`, annotated neither read-only nor destructive. Stories about a backend that misbehaves use a second, hypothetical backend, `reports`, added to the file after `notes` as `{ "name": "reports", "url": "https://reports.sbx.ikigenba.dev", "description": "Reports built from your data", "socket": "/run/ikigenba/reports.sock", "enabled": true, "mcp": true }`; when it answers, it offers one tool, `build_report`, description `Build a report from your data.`, input schema `{"type":"object","additionalProperties":false}`, no output schema, annotated `readOnlyHint` `true`.

A response body below is laid out for reading: its white space is not fixed.

## A model lists a service's tools

Before running anything, a model asks what a service offers. Each tool comes with only its one-line summary and its kind, enough to choose a tool and to know whether `call` or `mutate` runs it; the full description and schemas are one more `describe` away.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"describe","arguments":{"service":"dummy"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 1 whose `result` has no `isError` member, a `structuredContent` of

```
{"service":"dummy","tools":[{"name":"list_widgets","summary":"List the widgets, oldest first.","kind":"read"},{"name":"create_widget","summary":"Create a widget and return it.","kind":"write"}]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`.

Postconditions:

- Nothing has changed. dummy received one `tools/list` and no `tools/call`.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: ok`.

## A model reads one tool's full description and schemas

Before it runs a tool, a model reads everything the backend tells about it: the whole description, which carries the tool's rules, and the schemas its arguments and answer follow. The gateway passes the backend's description and schemas on unaltered, member for member, so the model reads what the backend wrote.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"describe","arguments":{"service":"dummy","tool":"create_widget"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `isError` member, a `structuredContent` of

```
{
  "service": "dummy",
  "tool": {
    "name": "create_widget",
    "description": "Create a widget and return it.\n\nThe name is trimmed of surrounding white space and must then be 1 to 40 characters and not already taken (letter case counts). The count is a whole number, zero or more. Every rule the arguments break is reported in one error, and nothing is created unless all of them hold.",
    "kind": "write",
    "inputSchema": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "The widget's name: 1 to 40 characters after trimming, unique."},
        "count": {"type": "integer", "description": "How many: a whole number, zero or more."},
        "status": {"type": "string", "enum": ["active", "paused", "retired"], "description": "The widget's status."}
      },
      "required": ["name", "count", "status"],
      "additionalProperties": false
    },
    "outputSchema": <create_widget's output schema>
  }
}
```

and a `content` array of one text block whose text is that same object encoded compactly. The description, the input schema, and the output schema are exactly those of `create_widget` in dummy's `tools/list` (dummy's `S9`), member for member; the output schema is not quoted whole here, as dummy's `S9` does not quote it. A tool whose backend gives no output schema, such as `reports`'s `build_report`, is described with no `outputSchema` member.

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`.

Postconditions:

- Nothing has changed. dummy received one `tools/list` and no `tools/call`; no widget was created.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: ok`.

## A model reads a tool that has no output schema

A backend need not give a tool an output schema. The gateway adds none of its own: the tool is described with no `outputSchema` member at all, rather than an empty or made-up one.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"describe","arguments":{"service":"reports","tool":"build_report"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has no `isError` member, a `structuredContent` of

```
{"service":"reports","tool":{"name":"build_report","description":"Build a report from your data.","kind":"read","inputSchema":{"type":"object","additionalProperties":false}}}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- `reports` is serving on `/run/ikigenba/reports.sock`, offering `build_report` alone.

Postconditions:

- Nothing has changed. `reports` received one `tools/list` and no `tools/call`.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: ok`.

## A model lists a tool whose backend makes no claim about it

A tool is `read` only when its backend says so, by annotating it `readOnlyHint` `true`. A tool with no annotations at all makes no such promise, so the gateway treats it as one that may change data: its kind is `write`, and it runs only with `mutate`. Here `reports` offers, after `build_report`, a second tool, `export_report`, description `Export a report as a file.`, input schema `{"type":"object","additionalProperties":false}`, and no `annotations` member.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"describe","arguments":{"service":"reports"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 4 whose `result` has no `isError` member, a `structuredContent` of

```
{"service":"reports","tools":[{"name":"build_report","summary":"Build a report from your data.","kind":"read"},{"name":"export_report","summary":"Export a report as a file.","kind":"write"}]}
```

and a `content` array of one text block whose text is exactly that line.

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- `reports` is serving on `/run/ikigenba/reports.sock`, offering `build_report` and then `export_report`, the latter with no annotations.

Postconditions:

- Nothing has changed. `reports` received one `tools/list` and no `tools/call`.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: ok`.

## A model sees a tool a backend gained a moment ago

The gateway keeps no copy of a backend's tools, so a backend redeployed with a new tool shows it on the very next `describe`, with no restart of mcp and nothing to expire. Here `reports` answers first with its one tool, then is redeployed with a release that adds a second tool, `list_reports`, description `List the reports built so far.`, annotated `readOnlyHint` `true`, and the same request is sent again.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"describe","arguments":{"service":"reports"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, both times. The first body is a JSON-RPC response with `id` 5 whose `result` has a `structuredContent` of

```
{"service":"reports","tools":[{"name":"build_report","summary":"Build a report from your data.","kind":"read"}]}
```

and the second, sent after the redeploy, one of

```
{"service":"reports","tools":[{"name":"build_report","summary":"Build a report from your data.","kind":"read"},{"name":"list_reports","summary":"List the reports built so far.","kind":"read"}]}
```

each with no `isError` member and a `content` array of one text block whose text is exactly its line.

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry, and was not restarted between the two requests.
- `reports` is serving on `/run/ikigenba/reports.sock`, offering `build_report` alone; between the two requests it is redeployed and then offers `build_report` and `list_reports`, in that order.

Postconditions:

- Nothing has changed in mcp. `reports` received one `tools/list` for each request.
- mcp wrote one line to stderr for each request: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: ok`.

## A model describes a service the gateway does not know

A name that `services` would not list on this connection is not a service the model can use, so the model is told to look at what it can use. On `/mcp` that is every name outside the MCP services: a name in no entry of the file, as here, and equally a name whose entry is not an MCP service, such as `auth`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"describe","arguments":{"service":"weather"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 6 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Unknown service: weather. Call services to see the services you can use.
```

Preconditions:

- mcp is serving, with the suite's services file, which has no entry named `weather`.

Postconditions:

- Nothing has changed. No backend was contacted.
- mcp wrote nothing to stderr.

## A model describes a service outside its connection's scope

A connection scoped to some services reaches only those (`S05`). A service the scope leaves out is unknown on that connection even though it is installed, available, and reachable on `/mcp`: the scope is the whole world the model sees.

Request:

```
POST /mcp/notes HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"describe","arguments":{"service":"dummy"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 7 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Unknown service: dummy. Call services to see the services you can use.
```

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`.

Postconditions:

- Nothing has changed. dummy was not contacted.
- mcp wrote nothing to stderr.

## A model describes a service that is disabled

A service the file marks disabled is listed by `services` as unavailable, reason `disabled`, so the model knows of it but cannot use it, and asking again will not help. The gateway does not try the backend.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"describe","arguments":{"service":"notes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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

## A model describes a scoped service that is not installed

A connection's scope may name a service the host does not have: a name in no entry of the file, as here, or one whose entry is not an MCP service, such as `auth`. `services` lists it as unavailable, reason `not installed` (`S05`), so describing it is refused the same way as a disabled one, with that reason.

Request:

```
POST /mcp/dummy,weather HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"describe","arguments":{"service":"weather"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 9 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service weather is unavailable: not installed. Do not retry; call services to see the services you can use.
```

Preconditions:

- mcp is serving, with the suite's services file, which has no entry named `weather`.

Postconditions:

- Nothing has changed. No backend was contacted.
- mcp wrote nothing to stderr.

## A model describes a service whose backend is not running

The file lists the service as available, but nothing answers on its socket: the backend is stopped, or between restarts. That is likely to pass, so the model is told to try again later.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"describe","arguments":{"service":"dummy"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
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
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: unreachable`.

## A model describes a service whose backend does not answer in time

The backend accepts the connection but has not answered its `tools/list` when the call's 50 seconds (`S08`) run out. The gateway stops waiting and answers then, so the model gets an answer before the space's nginx would give up on the request.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"describe","arguments":{"service":"reports"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200, 50 seconds after the request arrived. The body is a JSON-RPC response with `id` 11 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service reports did not answer within 50 s. Retry later.
```

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- `reports` accepts connections on `/run/ikigenba/reports.sock` but does not answer a `tools/list` within 50 seconds.

Postconditions:

- Nothing has changed.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: timed out`.

## A model describes a service whose backend answers with an error

The backend answers the `tools/list`, but with a JSON-RPC error rather than its tools. The gateway passes the backend's message on unaltered, after naming the service it came from. Here `reports` answers with `code` `-32603` and `message` `report store is offline`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"describe","arguments":{"service":"reports"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 12 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service reports answered with an error: report store is offline
```

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- `reports` is serving on `/run/ikigenba/reports.sock` and answers `tools/list` with a JSON-RPC error whose `code` is `-32603` and whose `message` is `report store is offline`.

Postconditions:

- Nothing has changed.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: rpc error -32603: report store is offline`.

## A model describes a service whose backend does not answer as MCP

Something answers on the backend's socket, but not with an MCP answer the gateway can read: here an HTTP `502` with a one-line plain-text body and no JSON-RPC response. A non-JSON body, a broken protocol answer, or a connection that breaks after connecting is answered the same way. The model is told to try again later.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"describe","arguments":{"service":"reports"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 13 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service reports gave an answer the gateway could not read. Retry later.
```

Preconditions:

- mcp is serving, with the suite's services file plus the `reports` entry.
- What listens on `/run/ikigenba/reports.sock` answers every request with status `502`, `Content-Type: text/plain`, and a one-line body.

Postconditions:

- Nothing has changed.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: reports tools/list: bad response (status 502)`. Had the connection broken before any status arrived, the line would end `bad response`.

## A model describes a tool the service does not have

The service answers, but has no tool of that name: the model misspelled it, or the backend dropped it in a release since the model last looked. The model is told how to see the tools there are.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"describe","arguments":{"service":"dummy","tool":"delete_widget"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 14 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
Service dummy has no tool delete_widget. Call describe with service dummy to see its tools.
```

Preconditions:

- mcp is serving, with the suite's services file.
- dummy is serving on `/run/ikigenba/dummy.sock`, with its two tools and no other.

Postconditions:

- Nothing has changed. dummy received one `tools/list` and no `tools/call`.
- mcp wrote one line to stderr: `mcp: request 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59: dummy tools/list: ok`.

## A model describes without naming a service

`service` is required. Arguments that do not fit `describe`'s input schema are refused before anything else is looked at, in the platform's wording for a tool's arguments, the same as dummy's tools use (dummy's `S9`): the line `invalid arguments:`, then one `<field>: <reason>` line per offence.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
X-Request-Id: 3f9c2a7be1d04c6a8b5e0f1d2c3b4a59
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: describe

{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"describe","arguments":{"tool":"create_widget"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 15 whose `result` has `isError` `true`, no `structuredContent`, and a `content` array of one text block whose text is exactly:

```
invalid arguments:
service: missing required field
```

Preconditions:

- mcp is serving, with the suite's services file.

Postconditions:

- Nothing has changed. No backend was contacted.
- mcp wrote nothing to stderr.
