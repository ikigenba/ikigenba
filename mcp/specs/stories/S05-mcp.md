# Stories — mcp endpoint

The gateway's MCP interface: `/mcp`, where an MCP client reaches every one of the suite's MCP services through four tools, and `/mcp/<scope>`, the same interface narrowed to the services the scope names. `/mcp` takes only POST, each POST carries one JSON-RPC request, and each request is answered with one `application/json` body: there are no sessions and no streams, and the gateway keeps nothing from one request to the next. It offers four tools and nothing else, in this order, whatever the scope: `services`, which lists the services the connection reaches (`S06`); `describe`, which shows a service's tools (`S07`); `call`, which runs a read tool (`S08`); and `mutate`, which runs a write tool (`S09`). The gateway's catalogue, its MCP services, is read from the services file: it takes its path from `IKIGENBA_SERVICES`, which it reads once, when it starts (`S02`), and it reads the file itself afresh for every request, so a rewrite of the file shows in the next answer without a restart. The file is the one the connect page's launcher reads (`S03`), under the same rules: an entry that lacks one of `name`, `url`, `description`, `socket`, `enabled` and `mcp`, or holds one of the wrong kind, is left out while the rest are used, and no variable, no file, a file that cannot be read, or a file that is not such an object is a file that lists no services, reported nowhere. The MCP services are the entries whose `mcp` is `true`, except any entry named `mcp`, which is the gateway itself and is never one of them even if it says `true`; they are taken in bytewise order of their names. An MCP service whose `enabled` is `false` is listed but unavailable, for the reason `disabled`.

`/mcp` reaches every MCP service. `/mcp/<a>,<b>` reaches exactly the services named, in whatever order the path gives them; they are listed in name order. A scope is one or more names separated by commas, each name 1 to 63 ASCII letters, digits, and hyphens, beginning and ending with a letter or digit, and no name twice. A name is matched against the file exactly, letter case counting. A scoped name that is not in the file, whose entry has `mcp` `false`, or that is `mcp` itself is listed all the same, as unavailable for the reason `not installed`. A path beneath `/mcp` that is not a well-formed scope does not exist and is answered 404, as any path the gateway does not serve is (`S03`).

The actor is a model working through an MCP client, or the client itself. Each request is shown as the HTTP request the client sends to a running gateway (`S02`), started with `IKIGENBA_SERVICES=/var/lib/ikigenba/services.json` in its environment, the host's services file, which holds the suite's services file below unless a story says otherwise. mcp serves guests its other paths (`S03`), but not `/mcp` or any path beneath it: nginx keeps its strict check there (opsctl's `S5-nginx.md`), so a request reaches them only with the caller nginx authenticated in `X-User-Id` and `X-User-Email`, and a request without `X-User-Id` is answered 500 before anything else, the method or the scope, is looked at. The requests below carry both headers by hand, and only the missing-header story carries neither.

The suite's services file:

```
{
  "services": [
    { "name": "auth", "url": "https://auth.sbx.ikigenba.dev", "description": "", "socket": "/run/ikigenba/auth.sock", "enabled": true, "mcp": false },
    { "name": "dummy", "url": "https://dummy.sbx.ikigenba.dev", "description": "Demo widgets to list and create", "socket": "/run/ikigenba/dummy.sock", "enabled": true, "mcp": true },
    { "name": "mcp", "url": "https://mcp.sbx.ikigenba.dev", "description": "Connect AI assistants to your services", "socket": "/run/ikigenba/mcp.sock", "enabled": true, "mcp": false },
    { "name": "notes", "url": "https://notes.sbx.ikigenba.dev", "description": "Notes to keep and search", "socket": "/run/ikigenba/notes.sock", "enabled": false, "mcp": true }
  ]
}
```

Its MCP services are `dummy`, available, and `notes`, unavailable because it is `disabled`; `auth` and `mcp` are not among them.

A client speaking the protocol revision `2026-07-28` sends, on every request, the headers `Content-Type: application/json`, `MCP-Protocol-Version: 2026-07-28`, and `Mcp-Method` equal to the request's `method`, and for a `tools/call` also `Mcp-Name` equal to the tool's name; and its `params` carry `_meta` with `io.modelcontextprotocol/protocolVersion` `2026-07-28` and `io.modelcontextprotocol/clientCapabilities` `{}`. Every successful answer on that revision is status 200, and its `result`, besides what each story fixes, carries `resultType` `"complete"` and `_meta` whose `io.modelcontextprotocol/serverInfo` is `{"name":"mcp","version":"v<semver>"}`, where `v<semver>` is the version `mcp --version` prints (`S01`); a `tools/list` or `server/discover` result also carries `ttlMs` `0` and `cacheScope` `"private"`. Those members are not repeated in this group or in `S06` to `S09`. A client speaking an earlier revision, `2025-11-25` or `2025-06-18`, opens with `initialize` and is served the same tools, and its calls get the same tool results, `isError` results included, without those members; a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200 on the earlier revisions. How the transport answers a request that is not well-formed MCP — a wrong `Content-Type`, a body over 1 MiB, a foreign `Origin`, mismatched headers, an unknown method — is the platform's, the same for every app, and this group does not restate it.

Each gateway tool's arguments, its result, and its failures are told in its own group, `S06` to `S09`; what they share is fixed here. A successful result of the gateway's own making carries the answer twice: as `structuredContent`, a JSON object, and as one text content block whose text is that same object encoded compactly, with no white space between its tokens. A result the gateway relays from a backend is the backend's, as `S08` and `S09` tell. A tool that cannot do what it was asked answers status 200 with a result whose `isError` is `true`, with no `structuredContent` and a `content` array of exactly one text block saying why and what the model can do next. A tool that refuses its arguments answers that way with the platform's wording: the line `invalid arguments:` followed by one line per offence, each `<field>: <reason>` (`service: missing required field`, `args: expected object, got string`, `bogus: unknown field`), separated by LF with no LF after the last.

Every call of one of the four tools that is answered with a result, an `isError` result included, adds one `tool.called` event to the trail (`S02`) once the tool has done its work and before the answer goes out, under the caller's request id and user, so in the trail it falls between the request's `request.started` and its `request.finished`. Its `tool` is the gateway tool's name, `services`, `describe`, `call`, or `mutate`, never a backend tool's; its `kind` is `read` for `services`, `describe`, and `call`, and `destructive` for `mutate`, as each tool is marked to the client; and its `outcome` is `ok` when the tool answered with a result that is not an error, `error` when it answered with an `isError` result, a refusal of the gateway's own or a backend's result relayed with `isError` `true`, and `invalid_arguments` when the gateway refused its own arguments (`service`, `tool`, `args`) with the platform's wording; a backend's refusal of the `args` it was passed is relayed, and is `error`. A request answered with a JSON-RPC error, an unknown tool say, reached no tool and adds no `tool.called`; its `request.finished` records it. The backend a gateway call reaches records its own `tool.called` for the tool it ran, in its own trail and under the same request id, so a trace of the request shows both the forward and the execution.

A response body below is laid out for reading: its white space is not fixed. The order of members within a tool and within its schemas, and within a tool's result where a story shows one, is fixed as shown; elsewhere the order of members is not. A response block shows the status line and the headers the story fixes; a header it does not show is not fixed. No answer in this group or in `S06` contacts a backend, and the gateway writes nothing to stderr about any answer in this group or in `S06` to `S09`, the missing-header 500 included; only an event it cannot deliver reaches stderr (`S02`); every request adds `request.started` and `request.finished` to the trail, as on every route (`S03`), and what a backend request of `describe`, `call`, or `mutate` adds is told in `S08`.

## An MCP client lists the gateway's tools

A client lists the tools before offering them to a model. The gateway offers four, however many services it reaches, so a model's view of the suite stays small: it learns the services with `services`, a service's tools with `describe`, and runs them with `call` and `mutate`. The description's first line is a one-line summary a catalogue can show on its own. `services`, `describe`, and `call` change nothing through the gateway, so they are marked read-only; `mutate` runs a tool that may change or remove data, so it is always marked destructive. None reaches outside the platform's own services, so none is open-world.

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
    "name": "services",
    "description": "List the services this connection reaches, and whether each is available.\n\nAn unavailable service says why: disabled or not installed. Call describe to see a service's tools.",
    "inputSchema": {"type": "object", "additionalProperties": false},
    "outputSchema": <the services output schema>,
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "describe",
    "description": "Show a service's tools, or one tool's full description and schemas.\n\nWithout tool, lists each tool of the service with its one-line summary and its kind: a read tool runs with call, a write tool with mutate. With tool, gives that tool's full description, its input schema, its output schema when it has one, and its kind. Call describe before call or mutate.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "service": {"type": "string", "description": "The service's name, as services lists it."},
        "tool": {"type": "string", "description": "A tool's name, as describe lists it. Leave it out to list the service's tools."}
      },
      "required": ["service"],
      "additionalProperties": false
    },
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "call",
    "description": "Run a read tool of a service and return its result.\n\nName the service and the tool as describe shows them, and pass the tool's arguments in args, an object matching its input schema ({} when left out). Only a tool of kind read runs here; a write tool runs with mutate.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "service": {"type": "string", "description": "The service's name, as services lists it."},
        "tool": {"type": "string", "description": "The tool's name, as describe lists it."},
        "args": {"type": "object", "description": "The tool's arguments, matching its input schema. Leave it out for {}."}
      },
      "required": ["service", "tool"],
      "additionalProperties": false
    },
    "annotations": {"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
  },
  {
    "name": "mutate",
    "description": "Run a write tool of a service and return its result.\n\nName the service and the tool as describe shows them, and pass the tool's arguments in args, an object matching its input schema ({} when left out). Only a tool of kind write runs here; a read tool runs with call. A write tool may change or remove data.",
    "inputSchema": {
      "type": "object",
      "properties": {
        "service": {"type": "string", "description": "The service's name, as services lists it."},
        "tool": {"type": "string", "description": "The tool's name, as describe lists it."},
        "args": {"type": "object", "description": "The tool's arguments, matching its input schema. Leave it out for {}."}
      },
      "required": ["service", "tool"],
      "additionalProperties": false
    },
    "annotations": {"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}
  }
]
```

`services`'s output schema is not quoted whole: it describes an object closed to other members whose one property, `services`, is an array of objects with the properties `name`, a string; `description`, a string; `available`, a boolean; and `reason`, a string. Which members it marks required, and which carry a description, are not fixed here. Whether `describe` carries an output schema is not fixed here either; `call` and `mutate` carry none, since what they return is the backend tool's own result.

Preconditions:

- The gateway is serving.

Postconditions:

- Nothing has changed.
- No backend was contacted, and the gateway wrote nothing to stderr.

## A client on a scoped endpoint is offered the same four tools

The scope narrows which services the tools reach, not which tools there are. A client pointed at `/mcp/dummy` is offered exactly the four tools of `An MCP client lists the gateway's tools`, and so is a client pointed at a scope that reaches nothing available, such as `/mcp/ghost`, where no service named `ghost` is installed.

Request:

```
POST /mcp/dummy HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/list

{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp/ghost HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/list

{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has no `nextCursor` and whose `tools` is the array of `An MCP client lists the gateway's tools`, member for member.

Preconditions:

- The gateway is serving.
- The services file has no entry named `ghost`.

Postconditions:

- Nothing has changed.
- No backend was contacted, and the gateway wrote nothing to stderr.

## A client asks the gateway what it reaches

A client tells its model what each server is for through the server's instructions. The gateway's instructions are made for each request from the services the connection reaches: two lines joined by LF, with no LF after the second. The first names exactly the services `services` would list, unavailable ones included, in name order, joined by `, `; the second tells the model how to go on. The instructions are always present.

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

Status 200. The body is a JSON-RPC response with `id` 3 whose `result` has exactly these members besides the `resultType`, `_meta`, `ttlMs`, and `cacheScope` every such result carries:

```
{
  "supportedVersions": ["2026-07-28", "2025-11-25", "2025-06-18"],
  "capabilities": {"tools": {}},
  "instructions": "This server reaches these services: dummy, notes.\nCall services to see which are available, and describe before call or mutate."
}
```

Preconditions:

- The gateway is serving.
- `/var/lib/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed. The services file is as it was.
- No backend was contacted, and the gateway wrote nothing to stderr.

## A client asks what a scoped endpoint reaches

A scoped endpoint's instructions name the scope's services, whether or not they are installed, in name order, whatever order the path gave them in. Here the scope names `notes`, `dummy`, and `ghost`, and the file has no entry named `ghost`.

Request:

```
POST /mcp/notes,dummy,ghost HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: server/discover

{"jsonrpc":"2.0","id":4,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is the answer of `A client asks the gateway what it reaches`, with `id` 4, except that `instructions` is:

```
"This server reaches these services: dummy, ghost, notes.\nCall services to see which are available, and describe before call or mutate."
```

Preconditions:

- The gateway is serving.
- `/var/lib/ikigenba/services.json` holds the suite's services file, which has no entry named `ghost`.

Postconditions:

- Nothing has changed.
- No backend was contacted, and the gateway wrote nothing to stderr.

## A client asks what the gateway reaches on a host with no MCP services

With no services file, `/mcp` reaches no services, and the first line says so; the instructions are still present and the four tools are still offered. The answer is the same when the file named is missing, cannot be read, is not a services file, or lists no MCP service. A scoped endpoint still names its scope: `/mcp/dummy` on this host reaches `dummy`, listed as not installed, and its first line is `This server reaches these services: dummy.`.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: server/discover

{"jsonrpc":"2.0","id":5,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 200 OK
Content-Type: application/json
```

Status 200. The body is the answer of `A client asks the gateway what it reaches`, with `id` 5, except that `instructions` is:

```
"This server reaches no services.\nCall services to see which are available, and describe before call or mutate."
```

Preconditions:

- The gateway is serving, started with `IKIGENBA_SERVICES` unset.

Postconditions:

- Nothing has changed.
- The gateway wrote nothing to stderr about the services file. Without one, though, it cannot find the telemetry service either, so no event of the request reaches the trail, and stderr holds one `undelivered event` line for each (`S02`), in this order, where `<id>` is the request id mcp made up for the request, each `<time>` is when mcp recorded that event, each `<us>` a duration in whole microseconds, and each `<bytes>` a count of body bytes:

  ```
  mcp: undelivered event: {"time":"<time>","service":"mcp","event":"request.started","request_id":"<id>","user":"u_7f3a9c21","attrs":{"method":"POST","path":"/mcp"}}
  mcp: undelivered event: {"time":"<time>","service":"mcp","event":"request.finished","request_id":"<id>","user":"u_7f3a9c21","attrs":{"duration_us":<us>,"request_bytes":<bytes>,"response_bytes":<bytes>,"status":200}}
  ```

## A client speaking an earlier revision opens with initialize

Many clients in the field still speak `2025-11-25` or `2025-06-18`, which open with `initialize` and send no `_meta`. The gateway serves them as every app does: `initialize` echoes the revision the client asked for when it is one of those two, and answers `2025-11-25` otherwise. The instructions are the ones `server/discover` gives for the same path. The gateway keeps no session, so the client's following `notifications/initialized` is answered `202` with an empty body, and the client may list or call tools with or without having sent `initialize`.

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
  "serverInfo": {"name": "mcp", "version": "v<semver>"},
  "instructions": "This server reaches these services: dummy, notes.\nCall services to see which are available, and describe before call or mutate."
}
```

The response carries no `Mcp-Session-Id` header.

Preconditions:

- The gateway is serving.
- `/var/lib/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed.
- No backend was contacted, and the gateway wrote nothing to stderr.

## A client speaking an earlier revision lists the tools

A client on an earlier revision is offered the same four tools, with the same names, descriptions, schemas, and annotations, and its calls get the same tool results as on `2026-07-28`. Only the envelope differs: results carry no `resultType`, `_meta`, `ttlMs`, or `cacheScope`, and a protocol error, such as an unknown tool, carries the same `code` and `message` but is answered with status 200, not 400.

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

Status 200. The body is a JSON-RPC response with `id` 2 whose `result` has exactly one member, `tools`, the array of `An MCP client lists the gateway's tools`, member for member.

Preconditions:

- The gateway is serving.

Postconditions:

- Nothing has changed.
- No backend was contacted, and the gateway wrote nothing to stderr.

## A client opens a malformed scope

A scope that breaks the grammar names no services the gateway could reach, so the path does not exist: the answer is the gateway's plain-text 404, whatever the method and whatever the body, never a JSON-RPC response. The scope is malformed when it is empty (`/mcp/`), when an element is empty (`/mcp/a,,b`, `/mcp/,dummy`, or a trailing comma, `/mcp/dummy,`), when a name appears twice (`/mcp/dummy,dummy`), or when an element is not a name: a character other than an ASCII letter, digit, or hyphen (`/mcp/du_mmy`, and `/mcp/dummy/`, whose element holds a slash), a hyphen first or last (`/mcp/-dummy`), or more than 63 characters. A well-formed name the file does not hold is not malformed: `/mcp/Dummy` is a scope, reaching `Dummy`, which is not installed because names match exactly.

Request:

```
POST /mcp/ HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/list

{"jsonrpc":"2.0","id":6,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp/a,,b HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/list

{"jsonrpc":"2.0","id":6,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp/dummy, HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/list

{"jsonrpc":"2.0","id":6,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp/dummy,dummy HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/list

{"jsonrpc":"2.0","id":6,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp/du_mmy HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/list

{"jsonrpc":"2.0","id":6,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp/dummy/ HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/list

{"jsonrpc":"2.0","id":6,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
GET /mcp/dummy,dummy HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

```
DELETE /mcp/ HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
```

Response:

```
HTTP/1.1 404 Not Found
Content-Type: text/plain; charset=utf-8
```

Status 404. The body is exactly the line `not found`, ending with LF.

Preconditions:

- The gateway is serving.

Postconditions:

- Nothing has changed.
- No tool ran, no backend was contacted, and the gateway wrote nothing to stderr.

## A browser opens /mcp

`/mcp` is for MCP clients, which only POST. A GET, from a browser or a client hoping for a stream, is refused with an empty body: the gateway offers no stream and no page at this address, and its connect page is at `/` (`S03`). A DELETE, from a client ending a session it believes it has, is refused the same way, since there are no sessions, and so is any other method that is not POST. A well-formed scoped path is answered the same as `/mcp`.

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

```
GET /mcp/dummy,notes HTTP/1.1
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

- The gateway is serving.

Postconditions:

- Nothing has changed.
- The gateway wrote nothing to stderr.

## A request to /mcp arrives without the identity headers

Every other route of mcp serves guests, but `/mcp` does not: every tool works for its caller. nginx never lets a request without a credential through to `/mcp`, so a request without `X-User-Id` here says the gate or a sibling is misconfigured, a server fault answered 500. The identity check runs before anything else is looked at, the scope included, so the answer is plain text, not a JSON-RPC response and not the 404 of a malformed scope, and no tool runs, whatever the body asked for. Without `X-User-Id` the gateway has no caller to forward to a backend, and it contacts none.

Request:

```
POST /mcp HTTP/1.1
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: services

{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"services","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

```
POST /mcp/a,,b HTTP/1.1
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/list

{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain; charset=utf-8
```

Status 500. The body is exactly the line `identity header missing`, ending with LF.

Preconditions:

- The gateway is serving.
- The request carries no `X-User-Id` header and no `X-Request-Id` header.

Postconditions:

- Nothing has changed. No backend was contacted, and no tool ran, so the trail holds no `tool.called`.
- The gateway wrote nothing to stderr. The trail holds two events for each request, with an empty user and a request id mcp made up for it (`S02`); for the first request:

  ```
  request.started method=POST path=/mcp
  request.finished status=500
  ```

  and for the second the same, with `path=/mcp/a,,b`.

## A model calls a tool the gateway does not have

The gateway has four tools. A call naming any other is not a tool's refusal but a protocol error: there is no tool to answer it. A model that has learned a service's tools and calls one of them by its own name, here dummy's `list_widgets`, gets that answer too; a service's tools are run through `call` and `mutate`, never directly.

Request:

```
POST /mcp HTTP/1.1
X-User-Id: u_7f3a9c21
X-User-Email: mg@example.com
Content-Type: application/json
MCP-Protocol-Version: 2026-07-28
Mcp-Method: tools/call
Mcp-Name: list_widgets

{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"list_widgets","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}
```

Response:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json
```

Status 400. The body is a JSON-RPC response with `id` 8 and no `result`, whose `error` has `code` `-32602` and `message` `Unknown tool: list_widgets`.

Preconditions:

- The gateway is serving.
- `/var/lib/ikigenba/services.json` holds the suite's services file.

Postconditions:

- Nothing has changed. No backend was contacted; dummy received no request.
- The gateway wrote nothing to stderr. No tool ran, so the trail holds no `tool.called` for the request, only its two events, under user `u_7f3a9c21` and a request id mcp made up for it:

  ```
  request.started method=POST path=/mcp
  request.finished status=400
  ```
